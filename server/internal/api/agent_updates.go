package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AgentVersionFile represents the version.json file structure
type AgentVersionFile struct {
	Version       string   `json:"version"`
	ReleaseDate   string   `json:"releaseDate"`
	Changelog     string   `json:"changelog"`
	Platforms     []string `json:"platforms"`
	MinAppVersion string   `json:"minAppVersion"`
}

// Cached agent version info
var (
	cachedAgentVersion *AgentVersionFile
	versionCacheMutex  sync.RWMutex
	versionCacheTime   time.Time
)

// getAgentVersionFromFile reads the agent version from version.json
func getAgentVersionFromFile() *AgentVersionFile {
	versionCacheMutex.RLock()
	// Cache for 60 seconds
	if cachedAgentVersion != nil && time.Since(versionCacheTime) < 60*time.Second {
		defer versionCacheMutex.RUnlock()
		return cachedAgentVersion
	}
	versionCacheMutex.RUnlock()

	versionCacheMutex.Lock()
	defer versionCacheMutex.Unlock()

	// Check paths for version.json
	paths := []string{
		"agent/version.json",
		"../agent/version.json",
		"installers/version.json",
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil {
			var vf AgentVersionFile
			if err := json.Unmarshal(data, &vf); err == nil {
				cachedAgentVersion = &vf
				versionCacheTime = time.Now()
				log.Printf("Loaded agent version %s from %s", vf.Version, path)
				return cachedAgentVersion
			}
		}
	}

	// Fallback to default version
	log.Println("Warning: Could not load version.json, using default version")
	cachedAgentVersion = &AgentVersionFile{
		Version:     "1.12.0",
		ReleaseDate: time.Now().Format("2006-01-02"),
		Changelog:   "No changelog available",
		Platforms:   []string{"windows", "linux", "darwin"},
	}
	versionCacheTime = time.Now()
	return cachedAgentVersion
}

// getCurrentAgentVersion returns the current agent version string
func getCurrentAgentVersion() string {
	return getAgentVersionFromFile().Version
}

// AgentVersionInfo contains version information for auto-update
type AgentVersionInfo struct {
	Version     string `json:"version"`
	Platform    string `json:"platform"`
	Arch        string `json:"arch"`
	DownloadURL string `json:"downloadUrl"`
	Checksum    string `json:"checksum"`
	Size        int64  `json:"size"`
	ReleaseDate string `json:"releaseDate"`
	Changelog   string `json:"changelog"`
	Required    bool   `json:"required"`
}

// AgentUpdateResponse is returned by the version check endpoint
type AgentUpdateResponse struct {
	Available      bool              `json:"available"`
	CurrentVersion string            `json:"currentVersion"`
	LatestVersion  string            `json:"latestVersion"`
	VersionInfo    *AgentVersionInfo `json:"versionInfo,omitempty"`
}

// Agent binary paths by platform/arch
var agentBinaryPaths = map[string]string{
	"windows-amd64": "installers/sentinel-agent-windows-amd64.exe",
	"windows-386":   "installers/sentinel-agent-windows-386.exe",
	"linux-amd64":   "installers/sentinel-agent-linux-amd64",
	"linux-arm64":   "installers/sentinel-agent-linux-arm64",
	"darwin-amd64":  "installers/sentinel-agent-darwin-amd64",
	"darwin-arm64":  "installers/sentinel-agent-darwin-arm64",
}

// getAgentVersion handles version check requests from agents
func (r *Router) getAgentVersion(c *gin.Context) {
	platform := c.Query("platform")
	arch := c.Query("arch")
	currentVersion := c.Query("current")

	// Normalize platform names
	if platform == "" {
		platform = runtime.GOOS
	}
	if arch == "" {
		arch = runtime.GOARCH
	}

	// Map common arch names
	switch arch {
	case "x64", "x86_64":
		arch = "amd64"
	case "x86", "i386", "i686":
		arch = "386"
	}

	agentVersion := getAgentVersionFromFile()

	response := AgentUpdateResponse{
		CurrentVersion: currentVersion,
		LatestVersion:  agentVersion.Version,
	}

	// Compare versions
	if !isNewerVersion(agentVersion.Version, currentVersion) {
		response.Available = false
		c.JSON(http.StatusOK, response)
		return
	}

	// Find binary for this platform
	key := fmt.Sprintf("%s-%s", platform, arch)
	binaryPath, ok := agentBinaryPaths[key]
	if !ok {
		response.Available = false
		c.JSON(http.StatusOK, response)
		return
	}

	// Check if binary exists and get info
	info, err := os.Stat(binaryPath)
	if os.IsNotExist(err) {
		response.Available = false
		c.JSON(http.StatusOK, response)
		return
	}

	// Calculate checksum
	checksum, err := calculateFileChecksum(binaryPath)
	if err != nil {
		checksum = ""
	}

	// Build download URL
	serverURL := r.config.ServerURL
	if serverURL == "" {
		serverURL = fmt.Sprintf("http://%s", c.Request.Host)
	}
	downloadURL := fmt.Sprintf("%s/api/agent/update/download?platform=%s&arch=%s", serverURL, platform, arch)

	response.Available = true
	response.VersionInfo = &AgentVersionInfo{
		Version:     agentVersion.Version,
		Platform:    platform,
		Arch:        arch,
		DownloadURL: downloadURL,
		Checksum:    checksum,
		Size:        info.Size(),
		ReleaseDate: agentVersion.ReleaseDate,
		Changelog:   agentVersion.Changelog,
		Required:    false,
	}

	c.JSON(http.StatusOK, response)
}

// downloadAgentUpdate serves the agent binary for updates
func (r *Router) downloadAgentUpdate(c *gin.Context) {
	platform := c.Query("platform")
	arch := c.Query("arch")

	// Normalize
	switch arch {
	case "x64", "x86_64":
		arch = "amd64"
	case "x86", "i386", "i686":
		arch = "386"
	}

	key := fmt.Sprintf("%s-%s", platform, arch)
	binaryPath, ok := agentBinaryPaths[key]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Binary not found for platform"})
		return
	}

	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Binary file not found"})
		return
	}

	// Log update download
	agentID := c.Query("agent_id")
	if agentID != "" {
		r.logAgentUpdate(c.Request.Context(), agentID, platform, arch, c.ClientIP())
	}

	filename := filepath.Base(binaryPath)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Header("X-Agent-Version", getCurrentAgentVersion())
	c.File(binaryPath)
}

// logAgentUpdate records an update download in the database
func (r *Router) logAgentUpdate(ctx context.Context, agentID, platform, arch, ipAddress string) {
	r.db.Pool.Exec(ctx, `
		INSERT INTO agent_updates (id, agent_id, from_version, to_version, platform, architecture, ip_address, status, created_at)
		VALUES ($1, $2, '', $3, $4, $5, $6, 'downloading', NOW())
	`, uuid.New(), agentID, getCurrentAgentVersion(), platform, arch, ipAddress)

	r.db.Pool.Exec(ctx, `
		UPDATE devices
		SET previous_agent_version = agent_version,
		    last_update_check = NOW()
		WHERE agent_id = $1
	`, agentID)
}

// listAgentVersions returns all available agent versions and their release info
func (r *Router) listAgentVersions(c *gin.Context) {
	agentVersion := getAgentVersionFromFile()

	rows, err := r.db.Pool.Query(c.Request.Context(), `
		SELECT version, release_date, changelog, is_required, platforms
		FROM agent_releases
		ORDER BY release_date DESC
		LIMIT 20
	`)
	if err != nil {
		c.JSON(http.StatusOK, []map[string]interface{}{
			{
				"version":     agentVersion.Version,
				"releaseDate": agentVersion.ReleaseDate,
				"changelog":   agentVersion.Changelog,
				"isCurrent":   true,
			},
		})
		return
	}
	defer rows.Close()

	versions := []map[string]interface{}{}
	for rows.Next() {
		var version, changelog string
		var releaseDate time.Time
		var isRequired bool
		var platforms []string

		if err := rows.Scan(&version, &releaseDate, &changelog, &isRequired, &platforms); err != nil {
			continue
		}

		versions = append(versions, map[string]interface{}{
			"version":     version,
			"releaseDate": releaseDate.Format(time.RFC3339),
			"changelog":   changelog,
			"isRequired":  isRequired,
			"platforms":   platforms,
			"isCurrent":   version == agentVersion.Version,
		})
	}

	if len(versions) == 0 {
		versions = append(versions, map[string]interface{}{
			"version":     agentVersion.Version,
			"releaseDate": agentVersion.ReleaseDate,
			"changelog":   agentVersion.Changelog,
			"isCurrent":   true,
		})
	}

	c.JSON(http.StatusOK, versions)
}

// getDeviceVersionHistory returns version history for a specific device
func (r *Router) getDeviceVersionHistory(c *gin.Context) {
	deviceID := c.Param("id")

	rows, err := r.db.Pool.Query(c.Request.Context(), `
		SELECT au.id, au.from_version, au.to_version, au.status, au.error_message, au.created_at, au.completed_at
		FROM agent_updates au
		JOIN devices d ON d.agent_id = au.agent_id
		WHERE d.id = $1
		ORDER BY au.created_at DESC
		LIMIT 50
	`, deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch version history"})
		return
	}
	defer rows.Close()

	history := []map[string]interface{}{}
	for rows.Next() {
		var id uuid.UUID
		var fromVersion, toVersion, status string
		var errorMessage *string
		var createdAt time.Time
		var completedAt *time.Time

		if err := rows.Scan(&id, &fromVersion, &toVersion, &status, &errorMessage, &createdAt, &completedAt); err != nil {
			continue
		}

		entry := map[string]interface{}{
			"id":          id,
			"fromVersion": fromVersion,
			"toVersion":   toVersion,
			"status":      status,
			"createdAt":   createdAt.Format(time.RFC3339),
		}
		if errorMessage != nil {
			entry["errorMessage"] = *errorMessage
		}
		if completedAt != nil {
			entry["completedAt"] = completedAt.Format(time.RFC3339)
		}

		history = append(history, entry)
	}

	c.JSON(http.StatusOK, history)
}

// reportUpdateStatus allows agents to report update status
func (r *Router) reportUpdateStatus(c *gin.Context) {
	var req struct {
		AgentID     string `json:"agentId" binding:"required"`
		FromVersion string `json:"fromVersion"`
		ToVersion   string `json:"toVersion" binding:"required"`
		Status      string `json:"status" binding:"required"`
		Error       string `json:"error"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := r.db.Pool.Exec(c.Request.Context(), `
		UPDATE agent_updates
		SET status = $1, error_message = $2, completed_at = CASE WHEN $1 IN ('completed', 'failed') THEN NOW() ELSE NULL END
		WHERE agent_id = $3 AND to_version = $4 AND status = 'downloading'
	`, req.Status, req.Error, req.AgentID, req.ToVersion)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update status"})
		return
	}

	if req.Status == "completed" {
		r.db.Pool.Exec(c.Request.Context(), `
			UPDATE devices SET agent_version = $1, updated_at = NOW() WHERE agent_id = $2
		`, req.ToVersion, req.AgentID)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Status updated"})
}

// Helper functions

func calculateFileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func isNewerVersion(latest, current string) bool {
	if current == "" {
		return true
	}

	latestParts := parseVersion(latest)
	currentParts := parseVersion(current)

	for i := 0; i < 3; i++ {
		if latestParts[i] > currentParts[i] {
			return true
		}
		if latestParts[i] < currentParts[i] {
			return false
		}
	}
	return false
}

func parseVersion(v string) [3]int {
	var parts [3]int
	split := strings.Split(strings.TrimPrefix(v, "v"), ".")
	for i := 0; i < 3 && i < len(split); i++ {
		parts[i], _ = strconv.Atoi(split[i])
	}
	return parts
}
