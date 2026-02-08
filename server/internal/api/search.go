package api

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/constants"
)

// SearchResult represents a single search result
type SearchResult struct {
	Type        string                 `json:"type"`
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Subtitle    string                 `json:"subtitle,omitempty"`
	Description string                 `json:"description,omitempty"`
	Icon        string                 `json:"icon,omitempty"`
	URL         string                 `json:"url"`
	Score       float64                `json:"score,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// SearchResponse contains all search results
type SearchResponse struct {
	Query   string         `json:"query"`
	Total   int            `json:"total"`
	Results []SearchResult `json:"results"`
	Timing  int64          `json:"timingMs"`
}

// globalSearchHandler performs unified search across all entities
func globalSearchHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := strings.TrimSpace(c.Query("q"))
		if query == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Search query required"})
			return
		}

		// Normalize query for searching
		searchPattern := "%" + strings.ToLower(query) + "%"

		start := time.Now()
		ctx := context.Background()
		results := []SearchResult{}

		// Search devices
		deviceResults := searchDevices(ctx, services, searchPattern, query)
		results = append(results, deviceResults...)

		// Search alerts
		alertResults := searchAlerts(ctx, services, searchPattern)
		results = append(results, alertResults...)

		// Search scripts
		scriptResults := searchScripts(ctx, services, searchPattern)
		results = append(results, scriptResults...)

		// Search users (if admin)
		role := c.GetString("role")
		if role == "admin" {
			userResults := searchUsers(ctx, services, searchPattern)
			results = append(results, userResults...)
		}

		// Search commands
		commandResults := searchCommands(ctx, services, searchPattern)
		results = append(results, commandResults...)

		// Sort by score (devices and exact matches first)
		sortSearchResults(results, query)

		// Limit results
		maxResults := 50
		if len(results) > maxResults {
			results = results[:maxResults]
		}

		elapsed := time.Since(start).Milliseconds()

		c.JSON(http.StatusOK, SearchResponse{
			Query:   query,
			Total:   len(results),
			Results: results,
			Timing:  elapsed,
		})
	}
}

func searchDevices(ctx context.Context, services *Services, pattern, query string) []SearchResult {
	results := []SearchResult{}

	rows, err := services.DB.Pool().Query(ctx, `
		SELECT id, hostname, COALESCE(os_type, ''), COALESCE(os_version, ''),
		       status, COALESCE(ip_address, ''), agent_id
		FROM devices
		WHERE organization_id = $1
		  AND (LOWER(hostname) LIKE $2 OR LOWER(ip_address) LIKE $2 OR LOWER(agent_id) LIKE $2)
		ORDER BY
			CASE WHEN LOWER(hostname) = LOWER($3) THEN 0
			     WHEN LOWER(hostname) LIKE LOWER($3) || '%' THEN 1
			     ELSE 2 END,
			hostname
		LIMIT 15
	`, constants.CurrentOrganizationID, pattern, query)
	if err != nil {
		log.Printf("[Search] Device search error: %v", err)
		return results
	}
	defer rows.Close()

	for rows.Next() {
		var id, hostname, osType, osVersion, status, ipAddr, agentID string
		if err := rows.Scan(&id, &hostname, &osType, &osVersion, &status, &ipAddr, &agentID); err != nil {
			continue
		}

		score := 1.0
		if strings.EqualFold(hostname, query) {
			score = 2.0 // Exact match
		}

		results = append(results, SearchResult{
			Type:        "device",
			ID:          id,
			Title:       hostname,
			Subtitle:    osType + " " + osVersion,
			Description: ipAddr,
			Icon:        getDeviceIcon(osType, status),
			URL:         "/devices/" + id,
			Score:       score,
			Metadata: map[string]interface{}{
				"status":  status,
				"osType":  osType,
				"agentId": agentID,
			},
		})
	}

	return results
}

func searchAlerts(ctx context.Context, services *Services, pattern string) []SearchResult {
	results := []SearchResult{}

	rows, err := services.DB.Pool().Query(ctx, `
		SELECT a.id, a.title, COALESCE(a.message, ''), a.severity, a.status,
		       COALESCE(d.hostname, 'Unknown'), a.created_at
		FROM alerts a
		JOIN devices d ON a.device_id = d.id
		WHERE d.organization_id = $1
		  AND (LOWER(a.title) LIKE $2 OR LOWER(a.message) LIKE $2 OR LOWER(d.hostname) LIKE $2)
		ORDER BY a.created_at DESC
		LIMIT 10
	`, constants.CurrentOrganizationID, pattern)
	if err != nil {
		log.Printf("[Search] Alert search error: %v", err)
		return results
	}
	defer rows.Close()

	for rows.Next() {
		var id, title, message, severity, status, hostname string
		var createdAt time.Time
		if err := rows.Scan(&id, &title, &message, &severity, &status, &hostname, &createdAt); err != nil {
			continue
		}

		results = append(results, SearchResult{
			Type:        "alert",
			ID:          id,
			Title:       title,
			Subtitle:    hostname + " - " + severity,
			Description: truncateString(message, 100),
			Icon:        getAlertIcon(severity),
			URL:         "/alerts/" + id,
			Score:       0.8,
			Metadata: map[string]interface{}{
				"severity":  severity,
				"status":    status,
				"createdAt": createdAt,
			},
		})
	}

	return results
}

func searchScripts(ctx context.Context, services *Services, pattern string) []SearchResult {
	results := []SearchResult{}

	rows, err := services.DB.Pool().Query(ctx, `
		SELECT id, name, COALESCE(description, ''), language, is_preset
		FROM scripts
		WHERE organization_id = $1
		  AND (LOWER(name) LIKE $2 OR LOWER(description) LIKE $2)
		ORDER BY name
		LIMIT 10
	`, constants.CurrentOrganizationID, pattern)
	if err != nil {
		log.Printf("[Search] Script search error: %v", err)
		return results
	}
	defer rows.Close()

	for rows.Next() {
		var id, name, description, language string
		var isPreset bool
		if err := rows.Scan(&id, &name, &description, &language, &isPreset); err != nil {
			continue
		}

		subtitle := language
		if isPreset {
			subtitle += " (preset)"
		}

		results = append(results, SearchResult{
			Type:        "script",
			ID:          id,
			Title:       name,
			Subtitle:    subtitle,
			Description: truncateString(description, 100),
			Icon:        "code",
			URL:         "/scripts/" + id,
			Score:       0.6,
			Metadata: map[string]interface{}{
				"language": language,
				"isPreset": isPreset,
			},
		})
	}

	return results
}

func searchUsers(ctx context.Context, services *Services, pattern string) []SearchResult {
	results := []SearchResult{}

	rows, err := services.DB.Pool().Query(ctx, `
		SELECT id, email, COALESCE(username, ''), COALESCE(name, ''), role
		FROM users
		WHERE organization_id = $1
		  AND (LOWER(email) LIKE $2 OR LOWER(username) LIKE $2 OR LOWER(name) LIKE $2)
		ORDER BY email
		LIMIT 10
	`, constants.CurrentOrganizationID, pattern)
	if err != nil {
		log.Printf("[Search] User search error: %v", err)
		return results
	}
	defer rows.Close()

	for rows.Next() {
		var id, email, username, name, role string
		if err := rows.Scan(&id, &email, &username, &name, &role); err != nil {
			continue
		}

		displayName := name
		if displayName == "" {
			displayName = email
		}

		results = append(results, SearchResult{
			Type:        "user",
			ID:          id,
			Title:       displayName,
			Subtitle:    email,
			Description: role,
			Icon:        "user",
			URL:         "/settings/users/" + id,
			Score:       0.5,
			Metadata: map[string]interface{}{
				"role":     role,
				"username": username,
			},
		})
	}

	return results
}

func searchCommands(ctx context.Context, services *Services, pattern string) []SearchResult {
	results := []SearchResult{}

	rows, err := services.DB.Pool().Query(ctx, `
		SELECT c.id, c.command, c.status, d.hostname, c.created_at
		FROM commands c
		JOIN devices d ON c.device_id = d.id
		WHERE d.organization_id = $1
		  AND (LOWER(c.command) LIKE $2 OR LOWER(d.hostname) LIKE $2)
		ORDER BY c.created_at DESC
		LIMIT 10
	`, constants.CurrentOrganizationID, pattern)
	if err != nil {
		log.Printf("[Search] Command search error: %v", err)
		return results
	}
	defer rows.Close()

	for rows.Next() {
		var id, command, status, hostname string
		var createdAt time.Time
		if err := rows.Scan(&id, &command, &status, &hostname, &createdAt); err != nil {
			continue
		}

		results = append(results, SearchResult{
			Type:        "command",
			ID:          id,
			Title:       truncateString(command, 50),
			Subtitle:    hostname,
			Description: status + " - " + createdAt.Format("Jan 2, 15:04"),
			Icon:        "terminal",
			URL:         "/commands/" + id,
			Score:       0.4,
			Metadata: map[string]interface{}{
				"status": status,
			},
		})
	}

	return results
}

// quickActionsHandler returns available quick actions for command palette
func quickActionsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := c.GetString("role")

		actions := []map[string]interface{}{
			{
				"id":       "new-device",
				"title":    "Add New Device",
				"subtitle": "Deploy agent to a new device",
				"icon":     "plus",
				"action":   "navigate",
				"url":      "/devices/add",
				"keywords": []string{"add", "new", "install", "agent", "deploy"},
			},
			{
				"id":       "run-script",
				"title":    "Run Script",
				"subtitle": "Execute a script on devices",
				"icon":     "play",
				"action":   "navigate",
				"url":      "/scripts",
				"keywords": []string{"execute", "run", "script", "powershell", "bash"},
			},
			{
				"id":       "view-alerts",
				"title":    "View Alerts",
				"subtitle": "See all active alerts",
				"icon":     "bell",
				"action":   "navigate",
				"url":      "/alerts",
				"keywords": []string{"alerts", "warnings", "critical", "notifications"},
			},
			{
				"id":       "export-data",
				"title":    "Export Data",
				"subtitle": "Download devices, alerts, or metrics as CSV/Excel",
				"icon":     "download",
				"action":   "modal",
				"modal":    "export",
				"keywords": []string{"export", "csv", "excel", "download", "report"},
			},
			{
				"id":       "generate-report",
				"title":    "Generate Report",
				"subtitle": "Create PDF security report",
				"icon":     "file-text",
				"action":   "navigate",
				"url":      "/reports",
				"keywords": []string{"report", "pdf", "security", "executive"},
			},
		}

		// Admin-only actions
		if role == "admin" {
			actions = append(actions, map[string]interface{}{
				"id":       "manage-users",
				"title":    "Manage Users",
				"subtitle": "Add or edit user accounts",
				"icon":     "users",
				"action":   "navigate",
				"url":      "/settings/users",
				"keywords": []string{"users", "accounts", "permissions", "roles"},
			})
			actions = append(actions, map[string]interface{}{
				"id":       "create-alert-rule",
				"title":    "Create Alert Rule",
				"subtitle": "Set up new monitoring alert",
				"icon":     "alert-triangle",
				"action":   "navigate",
				"url":      "/settings/alerts/new",
				"keywords": []string{"alert", "rule", "threshold", "monitor"},
			})
		}

		c.JSON(http.StatusOK, actions)
	}
}

// searchSuggestionsHandler returns search suggestions as user types
func searchSuggestionsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := strings.TrimSpace(c.Query("q"))
		if len(query) < 2 {
			c.JSON(http.StatusOK, []string{})
			return
		}

		ctx := context.Background()
		pattern := strings.ToLower(query) + "%"
		suggestions := []string{}

		// Get hostname suggestions
		rows, err := services.DB.Pool().Query(ctx, `
			SELECT DISTINCT hostname FROM devices
			WHERE organization_id = $1 AND LOWER(hostname) LIKE $2
			ORDER BY hostname LIMIT 5
		`, constants.CurrentOrganizationID, pattern)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var hostname string
				if rows.Scan(&hostname) == nil {
					suggestions = append(suggestions, hostname)
				}
			}
		}

		// Get script name suggestions
		rows, err = services.DB.Pool().Query(ctx, `
			SELECT DISTINCT name FROM scripts
			WHERE organization_id = $1 AND LOWER(name) LIKE $2
			ORDER BY name LIMIT 3
		`, constants.CurrentOrganizationID, pattern)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var name string
				if rows.Scan(&name) == nil {
					suggestions = append(suggestions, name)
				}
			}
		}

		c.JSON(http.StatusOK, suggestions)
	}
}

// Helper functions

func getDeviceIcon(osType, status string) string {
	if status == "offline" {
		return "monitor-off"
	}
	switch strings.ToLower(osType) {
	case "windows":
		return "monitor"
	case "linux":
		return "terminal"
	case "macos", "darwin":
		return "monitor"
	default:
		return "monitor"
	}
}

func getAlertIcon(severity string) string {
	switch severity {
	case "critical":
		return "alert-octagon"
	case "warning":
		return "alert-triangle"
	default:
		return "info"
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func sortSearchResults(results []SearchResult, query string) {
	// Simple bubble sort by score (for small result sets this is fine)
	for i := 0; i < len(results)-1; i++ {
		for j := 0; j < len(results)-i-1; j++ {
			if results[j].Score < results[j+1].Score {
				results[j], results[j+1] = results[j+1], results[j]
			}
		}
	}
}

// RecentSearchResult for search history
type RecentSearchResult struct {
	ID        uuid.UUID `json:"id"`
	Query     string    `json:"query"`
	ResultID  string    `json:"resultId,omitempty"`
	ResultType string   `json:"resultType,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}
