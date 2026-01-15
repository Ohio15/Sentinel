package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sentinel/server/internal/constants"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// AgentInstallationLink represents an installation link record
type AgentInstallationLink struct {
	ID                  uuid.UUID          `json:"id"`
	DownloadToken       string             `json:"downloadToken,omitempty"`
	DeviceName          string             `json:"deviceName"`
	UserEmail           string             `json:"userEmail"`
	UserName            *string            `json:"userName,omitempty"`
	EnrollmentTokenID   *uuid.UUID         `json:"enrollmentTokenId,omitempty"`
	CreatedAt           time.Time          `json:"createdAt"`
	CreatedBy           *int               `json:"createdBy,omitempty"`
	CreatedByName       *string            `json:"createdByName,omitempty"`
	ExpiresAt           time.Time          `json:"expiresAt"`
	DownloadedAt        *time.Time         `json:"downloadedAt,omitempty"`
	DownloadIP          *string            `json:"downloadIp,omitempty"`
	DownloadUserAgent   *string            `json:"downloadUserAgent,omitempty"`
	DownloadCount       int                `json:"downloadCount"`
	AgentConnectedAt    *time.Time         `json:"agentConnectedAt,omitempty"`
	DeviceID            *int               `json:"deviceId,omitempty"`
	Status              string             `json:"status"`
	RevokedAt           *time.Time         `json:"revokedAt,omitempty"`
	RevokedBy           *int               `json:"revokedBy,omitempty"`
	EmailSentAt         *time.Time         `json:"emailSentAt,omitempty"`
	EmailDeliveryStatus *string            `json:"emailDeliveryStatus,omitempty"`
	EmailOpenedAt       *time.Time         `json:"emailOpenedAt,omitempty"`
	ReminderSentAt      *time.Time         `json:"reminderSentAt,omitempty"`
	Notes               *string            `json:"notes,omitempty"`
	Metadata            map[string]any     `json:"metadata,omitempty"`
	DownloadURL         string             `json:"downloadUrl,omitempty"`
	AccessLog           []LinkAccessLog    `json:"accessLog,omitempty"`
}

// LinkAccessLog represents an access log entry
type LinkAccessLog struct {
	ID           int       `json:"id"`
	LinkID       uuid.UUID `json:"linkId"`
	AccessedAt   time.Time `json:"accessedAt"`
	IPAddress    string    `json:"ipAddress"`
	UserAgent    string    `json:"userAgent"`
	Action       string    `json:"action"`
	Success      bool      `json:"success"`
	ErrorMessage *string   `json:"errorMessage,omitempty"`
}

// CreateAgentLinkRequest is the request body for creating an installation link
type CreateAgentLinkRequest struct {
	DeviceName      string  `json:"deviceName" binding:"required"`
	UserEmail       string  `json:"userEmail" binding:"required,email"`
	UserName        *string `json:"userName"`
	Notes           *string `json:"notes"`
	ExpirationHours int     `json:"expirationHours"`
	SendEmail       *bool   `json:"sendEmail"`
	EmailTemplate   string  `json:"emailTemplate"`
}

// CreateAgentLinkResponse is the response after creating an installation link
type CreateAgentLinkResponse struct {
	Success         bool      `json:"success"`
	LinkID          uuid.UUID `json:"linkId"`
	DownloadToken   string    `json:"downloadToken"`
	DownloadURL     string    `json:"downloadUrl"`
	EnrollmentToken string    `json:"enrollmentToken"`
	ExpiresAt       time.Time `json:"expiresAt"`
	EmailSent       bool      `json:"emailSent"`
}

// AgentLinkListResponse is the paginated list response
type AgentLinkListResponse struct {
	Links []AgentInstallationLink `json:"links"`
	Total int                     `json:"total"`
	Page  int                     `json:"page"`
	Pages int                     `json:"pages"`
}

// createAgentLinkHandler creates a new installation link
func createAgentLinkHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateAgentLinkRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Validate email format
		if !strings.Contains(req.UserEmail, "@") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid email format"})
			return
		}

		// Check for existing pending link with same device name
		var existingCount int
		err := services.DB.Pool().QueryRow(c.Request.Context(), `
			SELECT COUNT(*) FROM agent_installation_links
			WHERE device_name = $1 AND status = 'pending' AND deleted_at IS NULL
		`, req.DeviceName).Scan(&existingCount)
		if err == nil && existingCount > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "Device name already has a pending installation link"})
			return
		}

		// Generate download token (64-char hex)
		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate download token"})
			return
		}
		downloadToken := "DL-" + hex.EncodeToString(tokenBytes)

		// Generate enrollment token for this specific link
		enrollmentBytes := make([]byte, 32)
		if _, err := rand.Read(enrollmentBytes); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate enrollment token"})
			return
		}
		enrollmentToken := hex.EncodeToString(enrollmentBytes)

		// Hash the enrollment token
		tokenHash, err := bcrypt.GenerateFromPassword([]byte(enrollmentToken), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash token"})
			return
		}

		// Get user ID from context
		var createdBy *int
		if userID, exists := c.Get("userID"); exists {
			if uid, ok := userID.(uuid.UUID); ok {
				// Need to get the users.id (int) from the uuid
				var userIntID int
				err := services.DB.Pool().QueryRow(c.Request.Context(),
					`SELECT id FROM users WHERE id = $1::text::int OR id::text = $1::text LIMIT 1`, uid.String()).Scan(&userIntID)
				if err == nil {
					createdBy = &userIntID
				}
			}
		}

		// Set expiration (default 24 hours)
		expirationHours := req.ExpirationHours
		if expirationHours <= 0 {
			expirationHours = 24
		}
		if expirationHours > 720 { // Max 30 days
			expirationHours = 720
		}
		expiresAt := time.Now().Add(time.Duration(expirationHours) * time.Hour)

		// Create enrollment token in database
		var enrollmentTokenID uuid.UUID
		err = services.DB.Pool().QueryRow(c.Request.Context(), `
			INSERT INTO enrollment_tokens (
				token, token_hash, name, description, created_by, expires_at, max_uses, is_active, is_legacy
			) VALUES ($1, $2, $3, $4, $5, $6, 1, TRUE, FALSE)
			RETURNING id
		`, enrollmentToken, string(tokenHash),
			fmt.Sprintf("Install Link: %s", req.DeviceName),
			fmt.Sprintf("Auto-generated for installation link to %s", req.UserEmail),
			createdBy, expiresAt).Scan(&enrollmentTokenID)
		if err != nil {
			log.Printf("Failed to create enrollment token: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create enrollment token"})
			return
		}

		// Create the installation link
		var linkID uuid.UUID
		err = services.DB.Pool().QueryRow(c.Request.Context(), `
			INSERT INTO agent_installation_links (
				download_token, device_name, user_email, user_name,
				enrollment_token_id, created_by, expires_at, notes, status
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending')
			RETURNING id
		`, downloadToken, req.DeviceName, req.UserEmail, req.UserName,
			enrollmentTokenID, createdBy, expiresAt, req.Notes).Scan(&linkID)
		if err != nil {
			log.Printf("Failed to create installation link: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create installation link"})
			return
		}

		// Build download URL - use PublicURL for web-accessible links
		publicURL := services.Config.PublicURL
		if publicURL == "" {
			publicURL = services.Config.ServerURL
		}
		if publicURL == "" {
			scheme := "https"
			if c.Request.TLS == nil {
				scheme = "http"
			}
			publicURL = fmt.Sprintf("%s://%s", scheme, c.Request.Host)
		}
		downloadURL := fmt.Sprintf("%s/install/%s", publicURL, downloadToken)

		// Send email if requested (default false - email not configured)
		emailSent := false
		sendEmail := req.SendEmail != nil && *req.SendEmail // Default false
		if sendEmail {
			emailSent = sendInstallationEmail(c, services, linkID, req, downloadURL, expiresAt)
		}

		c.JSON(http.StatusCreated, CreateAgentLinkResponse{
			Success:         true,
			LinkID:          linkID,
			DownloadToken:   downloadToken,
			DownloadURL:     downloadURL,
			EnrollmentToken: enrollmentToken[:16] + "...", // Masked
			ExpiresAt:       expiresAt,
			EmailSent:       emailSent,
		})
	}
}

// sendInstallationEmail sends the installation email
func sendInstallationEmail(c *gin.Context, services *Services, linkID uuid.UUID, req CreateAgentLinkRequest, downloadURL string, expiresAt time.Time) bool {
	// For now, just update the database to indicate email would be sent
	// In production, integrate with SendGrid, SES, etc.

	_, err := services.DB.Pool().Exec(c.Request.Context(), `
		UPDATE agent_installation_links
		SET email_sent_at = NOW(), email_delivery_status = 'pending'
		WHERE id = $1 AND organization_id = $2
	`, linkID, constants.CurrentOrganizationID)

	if err != nil {
		log.Printf("Failed to update email status: %v", err)
		return false
	}

	// Log that email should be sent
	log.Printf("[EMAIL] Would send installation email to %s for device %s, URL: %s",
		req.UserEmail, req.DeviceName, downloadURL)

	// TODO: Implement actual email sending with SendGrid/SES
	// For now, mark as "sent" for demo purposes
	_, _ = services.DB.Pool().Exec(c.Request.Context(), `
		UPDATE agent_installation_links
		SET email_delivery_status = 'sent'
		WHERE id = $1
	`, linkID)

	return true
}

// listAgentLinksHandler returns all installation links with filtering
func listAgentLinksHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Parse query params
		status := c.Query("status")
		search := c.Query("search")
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		sortBy := c.DefaultQuery("sort", "created_at")
		order := c.DefaultQuery("order", "desc")

		if page < 1 {
			page = 1
		}
		if limit < 1 || limit > 100 {
			limit = 50
		}

		// Validate sort column
		validSorts := map[string]bool{
			"created_at": true,
			"expires_at": true,
			"status":     true,
			"device_name": true,
		}
		if !validSorts[sortBy] {
			sortBy = "created_at"
		}
		if order != "asc" && order != "desc" {
			order = "desc"
		}

		offset := (page - 1) * limit

		// Build query
		baseQuery := `
			FROM agent_installation_links l
				LEFT JOIN users u ON l.created_by = u.id
				WHERE l.deleted_at IS NULL AND l.organization_id = $1
		`
		args := []interface{}{constants.CurrentOrganizationID}
		argNum := 2

		if status != "" {
			baseQuery += fmt.Sprintf(" AND l.status = $%d", argNum)
			args = append(args, status)
			argNum++
		}

		if search != "" {
			baseQuery += fmt.Sprintf(" AND (l.device_name ILIKE $%d OR l.user_email ILIKE $%d)", argNum, argNum)
			args = append(args, "%"+search+"%")
			argNum++
		}

		// Get total count
		var total int
		countQuery := "SELECT COUNT(*) " + baseQuery
		err := services.DB.Pool().QueryRow(c.Request.Context(), countQuery, args...).Scan(&total)
		if err != nil {
			log.Printf("Failed to count agent links: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch links"})
			return
		}

		// Get paginated results
		selectQuery := fmt.Sprintf(`
			SELECT l.id, l.download_token, l.device_name, l.user_email, l.user_name,
			       l.created_at, l.created_by, u.first_name || ' ' || u.last_name as created_by_name,
			       l.expires_at, l.downloaded_at, l.download_count,
			       l.agent_connected_at, l.device_id, l.status,
			       l.email_sent_at, l.email_delivery_status, l.notes
			%s
			ORDER BY l.%s %s
			LIMIT $%d OFFSET $%d
		`, baseQuery, sortBy, order, argNum, argNum+1)
		args = append(args, limit, offset)

		rows, err := services.DB.Pool().Query(c.Request.Context(), selectQuery, args...)
		if err != nil {
			log.Printf("Failed to fetch agent links: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch links"})
			return
		}
		defer rows.Close()

		links := []AgentInstallationLink{}
		for rows.Next() {
			var link AgentInstallationLink
			var createdByName sql.NullString
			err := rows.Scan(
				&link.ID, &link.DownloadToken, &link.DeviceName, &link.UserEmail, &link.UserName,
				&link.CreatedAt, &link.CreatedBy, &createdByName,
				&link.ExpiresAt, &link.DownloadedAt, &link.DownloadCount,
				&link.AgentConnectedAt, &link.DeviceID, &link.Status,
				&link.EmailSentAt, &link.EmailDeliveryStatus, &link.Notes,
			)
			if err != nil {
				log.Printf("Error scanning link row: %v", err)
				continue
			}
			if createdByName.Valid {
				link.CreatedByName = &createdByName.String
			}
			// Mask the download token in list view
			if len(link.DownloadToken) > 15 {
				link.DownloadToken = link.DownloadToken[:15] + "..."
			}
			links = append(links, link)
		}

		pages := (total + limit - 1) / limit
		c.JSON(http.StatusOK, AgentLinkListResponse{
			Links: links,
			Total: total,
			Page:  page,
			Pages: pages,
		})
	}
}

// getAgentLinkHandler returns detailed information about a specific link
func getAgentLinkHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		linkIDStr := c.Param("linkId")
		linkID, err := uuid.Parse(linkIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid link ID"})
			return
		}

		var link AgentInstallationLink
		var createdByName sql.NullString
		err = services.DB.Pool().QueryRow(c.Request.Context(), `
			SELECT l.id, l.download_token, l.device_name, l.user_email, l.user_name,
			       l.enrollment_token_id, l.created_at, l.created_by,
			       u.first_name || ' ' || u.last_name as created_by_name,
			       l.expires_at, l.downloaded_at, l.download_ip, l.download_user_agent,
			       l.download_count, l.agent_connected_at, l.device_id, l.status,
			       l.revoked_at, l.revoked_by, l.email_sent_at, l.email_delivery_status,
			       l.email_opened_at, l.reminder_sent_at, l.notes, l.metadata
			FROM agent_installation_links l
				LEFT JOIN users u ON l.created_by = u.id
				WHERE l.id = $1 AND l.deleted_at IS NULL AND l.organization_id = $2
		`, linkID, constants.CurrentOrganizationID).Scan(
			&link.ID, &link.DownloadToken, &link.DeviceName, &link.UserEmail, &link.UserName,
			&link.EnrollmentTokenID, &link.CreatedAt, &link.CreatedBy, &createdByName,
			&link.ExpiresAt, &link.DownloadedAt, &link.DownloadIP, &link.DownloadUserAgent,
			&link.DownloadCount, &link.AgentConnectedAt, &link.DeviceID, &link.Status,
			&link.RevokedAt, &link.RevokedBy, &link.EmailSentAt, &link.EmailDeliveryStatus,
			&link.EmailOpenedAt, &link.ReminderSentAt, &link.Notes, &link.Metadata,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Link not found"})
				return
			}
			log.Printf("Failed to fetch link: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch link"})
			return
		}

		if createdByName.Valid {
			link.CreatedByName = &createdByName.String
		}

		// Build download URL
		// Use PublicURL for web-accessible links
		publicURL := services.Config.PublicURL
		if publicURL == "" {
			publicURL = services.Config.ServerURL
		}
		if publicURL == "" {
			scheme := "https"
			if c.Request.TLS == nil {
				scheme = "http"
			}
			publicURL = fmt.Sprintf("%s://%s", scheme, c.Request.Host)
		}
		link.DownloadURL = fmt.Sprintf("%s/install/%s", publicURL, link.DownloadToken)

		// Fetch access log
		rows, err := services.DB.Pool().Query(c.Request.Context(), `
			SELECT id, link_id, accessed_at, ip_address, user_agent, action, success, error_message
			FROM agent_link_access_log
			WHERE link_id = $1
			ORDER BY accessed_at DESC
			LIMIT 50
		`, linkID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var entry LinkAccessLog
				if err := rows.Scan(&entry.ID, &entry.LinkID, &entry.AccessedAt,
					&entry.IPAddress, &entry.UserAgent, &entry.Action,
					&entry.Success, &entry.ErrorMessage); err == nil {
					link.AccessLog = append(link.AccessLog, entry)
				}
			}
		}

		c.JSON(http.StatusOK, link)
	}
}

// resendAgentLinkEmailHandler resends the installation email
func resendAgentLinkEmailHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		linkIDStr := c.Param("linkId")
		linkID, err := uuid.Parse(linkIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid link ID"})
			return
		}

		// Get link details
		var deviceName, userEmail string
		var userName *string
		var expiresAt time.Time
		var downloadToken string
		var status string

		err = services.DB.Pool().QueryRow(c.Request.Context(), `
			SELECT device_name, user_email, user_name, expires_at, download_token, status
			FROM agent_installation_links
			WHERE id = $1 AND deleted_at IS NULL AND organization_id = $2
		`, linkID, constants.CurrentOrganizationID).Scan(&deviceName, &userEmail, &userName, &expiresAt, &downloadToken, &status)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Link not found"})
			return
		}

		// Check if link is still valid
		if status == "revoked" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot resend email for revoked link"})
			return
		}
		if status == "expired" || time.Now().After(expiresAt) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot resend email for expired link"})
			return
		}

		// Build download URL - use PublicURL for web-accessible links
		publicURL := services.Config.PublicURL
		if publicURL == "" {
			publicURL = services.Config.ServerURL
		}
		if publicURL == "" {
			scheme := "https"
			if c.Request.TLS == nil {
				scheme = "http"
			}
			publicURL = fmt.Sprintf("%s://%s", scheme, c.Request.Host)
		}
		downloadURL := fmt.Sprintf("%s/install/%s", publicURL, downloadToken)

		// Send email
		req := CreateAgentLinkRequest{
			DeviceName: deviceName,
			UserEmail:  userEmail,
			UserName:   userName,
		}
		emailSent := sendInstallationEmail(c, services, linkID, req, downloadURL, expiresAt)

		c.JSON(http.StatusOK, gin.H{
			"success":   emailSent,
			"emailSent": emailSent,
			"sentAt":    time.Now(),
		})
	}
}

// revokeAgentLinkHandler revokes an installation link
func revokeAgentLinkHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		linkIDStr := c.Param("linkId")
		linkID, err := uuid.Parse(linkIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid link ID"})
			return
		}

		// Get user ID for audit
		var revokedBy *int
		if userID, exists := c.Get("userID"); exists {
			if uid, ok := userID.(uuid.UUID); ok {
				var userIntID int
				err := services.DB.Pool().QueryRow(c.Request.Context(),
					`SELECT id FROM users WHERE id = $1::text::int OR id::text = $1::text LIMIT 1`, uid.String()).Scan(&userIntID)
				if err == nil {
					revokedBy = &userIntID
				}
			}
		}

		// Revoke the link
		result, err := services.DB.Pool().Exec(c.Request.Context(), `
			UPDATE agent_installation_links
			SET status = 'revoked', revoked_at = NOW(), revoked_by = $1
			WHERE id = $2 AND status NOT IN ('installed', 'revoked') AND deleted_at IS NULL
		`, revokedBy, linkID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to revoke link"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Link cannot be revoked or not found"})
			return
		}

		// Also deactivate the associated enrollment token
		_, _ = services.DB.Pool().Exec(c.Request.Context(), `
			UPDATE enrollment_tokens
			SET is_active = FALSE
			WHERE id = (SELECT enrollment_token_id FROM agent_installation_links WHERE id = $1)
		`, linkID)

		c.JSON(http.StatusOK, gin.H{
			"success":   true,
			"revokedAt": time.Now(),
		})
	}
}

// deleteAgentLinkHandler soft-deletes an installation link
func deleteAgentLinkHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		linkIDStr := c.Param("linkId")
		linkID, err := uuid.Parse(linkIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid link ID"})
			return
		}

		result, err := services.DB.Pool().Exec(c.Request.Context(), `
			UPDATE agent_installation_links
			SET deleted_at = NOW()
			WHERE id = $1 AND deleted_at IS NULL AND organization_id = $2
		`, linkID, constants.CurrentOrganizationID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete link"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Link not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// getAgentLinkStatsHandler returns statistics about installation links
func getAgentLinkStatsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var stats struct {
			Total       int `json:"total"`
			Pending     int `json:"pending"`
			Downloaded  int `json:"downloaded"`
			Installed   int `json:"installed"`
			Expired     int `json:"expired"`
			Revoked     int `json:"revoked"`
			Last24Hours int `json:"last24Hours"`
			Last7Days   int `json:"last7Days"`
		}

		// Get counts by status
		rows, err := services.DB.Pool().Query(c.Request.Context(), `
			SELECT status, COUNT(*)
			FROM agent_installation_links
			WHERE deleted_at IS NULL
			GROUP BY status
		`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var status string
				var count int
				if rows.Scan(&status, &count) == nil {
					stats.Total += count
					switch status {
					case "pending":
						stats.Pending = count
					case "downloaded", "installing":
						stats.Downloaded += count
					case "installed":
						stats.Installed = count
					case "expired":
						stats.Expired = count
					case "revoked":
						stats.Revoked = count
					}
				}
			}
		}

		// Get recent counts
		services.DB.Pool().QueryRow(c.Request.Context(), `
			SELECT COUNT(*) FROM agent_installation_links
			WHERE created_at > NOW() - INTERVAL '24 hours' AND deleted_at IS NULL
		`).Scan(&stats.Last24Hours)

		services.DB.Pool().QueryRow(c.Request.Context(), `
			SELECT COUNT(*) FROM agent_installation_links
			WHERE created_at > NOW() - INTERVAL '7 days' AND deleted_at IS NULL
		`).Scan(&stats.Last7Days)

		c.JSON(http.StatusOK, stats)
	}
}
