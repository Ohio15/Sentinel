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
	cachedChecksums    map[string]string // "windows-amd64" -> sha256
	checksumCacheTime  time.Time

	// Wave 1 hotfix (incident df7a7ff8): release-readiness cache. Tracks whether
	// the version advertised by installers/version.json has a corresponding
	// agent_releases row. When false, heartbeat-ack callers must suppress
	// updateAvailable=true to avoid the 401 retry-storm we hit on the 2026-04-27
	// v1.77.10 deploy. Cache TTL 60s, separate mutex from versionCacheMutex to
	// avoid reentrant-lock issues with getAgentVersionFromFile.
	cachedReleaseStatus *AgentReleaseStatus
	releaseStatusMutex  sync.RWMutex
	releaseStatusTime   time.Time
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
	// Priority: mounted/updatable paths first, then baked-in fallbacks
	paths := []string{
		"installers/version.json",      // Mounted volume - deployable updates
		"release/agent/version.json",   // Release directory
		"agent/version.json",           // Baked into Docker image (fallback)
		"../agent/version.json",        // Legacy path
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

// AgentReleaseStatus captures whether the announced version is actually
// publishable from the server. Heartbeat-ack callers gate updateAvailable=true
// on HasReleaseRow — without it, the download endpoint will 401/404 and trigger
// the retry-storm pattern documented in incident df7a7ff8.
type AgentReleaseStatus struct {
	Version       string
	HasReleaseRow bool
}

// getAgentReleaseStatus returns cached release-readiness, refreshing every 60s.
// One indexed query per minute (EXISTS over agent_releases) — not per heartbeat.
// On DB error: HasReleaseRow=false (fail-closed → suppress announcement). Better
// to under-announce than to flood the fleet with download retries when the
// release isn't actually serveable.
func (r *Router) getAgentReleaseStatus(ctx context.Context) *AgentReleaseStatus {
	releaseStatusMutex.RLock()
	if cachedReleaseStatus != nil && time.Since(releaseStatusTime) < 60*time.Second {
		s := cachedReleaseStatus
		releaseStatusMutex.RUnlock()
		return s
	}
	releaseStatusMutex.RUnlock()

	// Compute outside the write lock so DB latency doesn't block other readers.
	version := getCurrentAgentVersion()
	hasRow := false
	if r != nil && r.db != nil {
		qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := r.db.Pool().QueryRow(qctx,
			`SELECT EXISTS(SELECT 1 FROM agent_releases WHERE version = $1)`, version,
		).Scan(&hasRow); err != nil {
			log.Printf("[ReleaseStatus] agent_releases lookup failed for %s: %v (suppressing updateAvailable)", version, err)
			hasRow = false
		}
	}
	fresh := &AgentReleaseStatus{Version: version, HasReleaseRow: hasRow}

	releaseStatusMutex.Lock()
	defer releaseStatusMutex.Unlock()
	// Another goroutine may have refreshed while we were querying — keep theirs.
	if cachedReleaseStatus != nil && time.Since(releaseStatusTime) < 60*time.Second {
		return cachedReleaseStatus
	}
	cachedReleaseStatus = fresh
	releaseStatusTime = time.Now()
	if !hasRow {
		log.Printf("[ReleaseStatus] No agent_releases row for advertised version %s — updateAvailable suppressed until release pipeline publishes", version)
	}
	return fresh
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

// supportedAgentTargets is the set of (platform, arch) tuples the update server
// is willing to serve. Actual filesystem lookup is delegated to the canonical
// resolver in installer_paths.go so /app/installers and other production roots
// are searched uniformly.
var supportedAgentTargets = map[string]struct{ Platform, Arch string }{
	"windows-amd64": {"windows", "amd64"},
	"windows-386":   {"windows", "386"},
	"windows-arm64": {"windows", "arm64"},
	"linux-amd64":   {"linux", "amd64"},
	"linux-arm64":   {"linux", "arm64"},
	"darwin-amd64":  {"darwin", "amd64"},
	"darwin-arm64":  {"darwin", "arm64"},
}

// resolveAgentBinary resolves the path to the agent binary for the given
// platform/arch key (e.g. "windows-amd64"). Returns "" if the target is
// unsupported or the binary is not present in any canonical search root.
func resolveAgentBinary(key string) string {
	t, ok := supportedAgentTargets[key]
	if !ok {
		return ""
	}
	return findPlatformBinary("agent", t.Platform, t.Arch)
}

// getCachedChecksum returns a cached SHA256 checksum for the given binary, recomputing every 60s.
func getCachedChecksum(platformKey, binaryPath string) string {
	versionCacheMutex.RLock()
	if cachedChecksums != nil && time.Since(checksumCacheTime) < 60*time.Second {
		cs := cachedChecksums[platformKey]
		versionCacheMutex.RUnlock()
		return cs
	}
	versionCacheMutex.RUnlock()

	versionCacheMutex.Lock()
	defer versionCacheMutex.Unlock()

	// Double-check after acquiring write lock
	if cachedChecksums != nil && time.Since(checksumCacheTime) < 60*time.Second {
		return cachedChecksums[platformKey]
	}

	checksum, err := calculateFileChecksum(binaryPath)
	if err != nil {
		return ""
	}
	if cachedChecksums == nil {
		cachedChecksums = make(map[string]string)
	}
	cachedChecksums[platformKey] = checksum
	checksumCacheTime = time.Now()
	return checksum
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

	// Wave 1.1 hotfix (incident df7a7ff8 follow-up): same release-status gate
	// as the heartbeat-ack path in handlers.go. Without this, agents polling via
	// updater.CheckForUpdate bypass the heartbeat gate and still 401-loop on
	// /api/agent/update/download. Suppress at the source: don't tell self-pollers
	// an update is available when the server can't actually serve it.
	releaseStatus := r.getAgentReleaseStatus(c.Request.Context())
	if !releaseStatus.HasReleaseRow {
		response.Available = false
		c.JSON(http.StatusOK, response)
		return
	}

	// Agents below v1.72.0 on Linux have broken self-update (hardcoded Windows paths,
	// no cross-filesystem fallback, broken restart). Telling them "update available"
	// just causes an endless download-fail loop. Return unavailable to stop the storm;
	// these agents require manual binary replacement or a relay update from a LAN peer.
	if platform == "linux" && currentVersion != "" && !isNewerVersion(currentVersion, "1.71.99") {
		log.Printf("[AgentVersion] Agent %s on Linux at v%s is below minimum self-update version (v1.72.0), suppressing update notification", currentVersion, currentVersion)
		response.Available = false
		c.JSON(http.StatusOK, response)
		return
	}

	// Find binary for this platform via canonical resolver
	key := fmt.Sprintf("%s-%s", platform, arch)
	binaryPath := resolveAgentBinary(key)
	if binaryPath == "" {
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

	// Get cached checksum (avoids re-hashing large binaries on every request)
	checksum := getCachedChecksum(key, binaryPath)

	// Build download URL using PublicURL (public-facing, no mTLS required)
	serverURL := r.config.PublicURL
	if serverURL == "" {
		serverURL = r.config.ServerURL
	}
	if serverURL == "" {
		serverURL = fmt.Sprintf("https://%s", c.Request.Host)
	}
	rawURL := fmt.Sprintf("%s/api/agent/update/download?platform=%s&arch=%s", serverURL, platform, arch)
	// Sign URL with 10-min TTL so a URL leaked via log infra becomes useless
	// quickly. Agents re-fetch /api/agent/version on every check, so the short
	// TTL is invisible to them.
	downloadURL := signAgentUpdateURL(rawURL, platform, arch, agentVersion.Version)

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
// downloadCooldown tracks recent downloads to prevent storm from old agents
// that spawn a new download goroutine on every heartbeat ack (~10s)
var downloadCooldown sync.Map // key: "ip-platform-arch" -> time.Time

func (r *Router) downloadAgentUpdate(c *gin.Context) {
	// Extend write deadline for large binary transfers over slow WAN links
	// Default server WriteTimeout (60s) is too short for 25-30MB files
	rc := http.NewResponseController(c.Writer)
	if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Minute)); err != nil {
		log.Printf("[AgentUpdate] Warning: failed to extend write deadline: %v", err)
	}

	platform := c.Query("platform")
	arch := c.Query("arch")

	// Per-agent download cooldown: one download per agent per platform per 5 minutes.
	// Use X-Agent-ID header (set by agent) with IP fallback for old agents.
	cooldownID := c.GetHeader("X-Agent-ID")
	if cooldownID == "" {
		cooldownID = c.ClientIP()
	}
	cooldownKey := fmt.Sprintf("%s-%s-%s", cooldownID, platform, arch)
	if lastDownload, ok := downloadCooldown.Load(cooldownKey); ok {
		if time.Since(lastDownload.(time.Time)) < 5*time.Minute {
			remaining := 5*time.Minute - time.Since(lastDownload.(time.Time))
			c.Header("Retry-After", fmt.Sprintf("%d", int(remaining.Seconds())))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":      "Download cooldown active, binary was recently served",
				"retryAfter": int(remaining.Seconds()),
			})
			return
		}
	}
	downloadCooldown.Store(cooldownKey, time.Now())

	// Normalize
	switch arch {
	case "x64", "x86_64":
		arch = "amd64"
	case "x86", "i386", "i686":
		arch = "386"
	}

	key := fmt.Sprintf("%s-%s", platform, arch)
	binaryPath := resolveAgentBinary(key)
	if binaryPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Binary not found for platform"})
		return
	}

	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Binary file not found"})
		return
	}

	// Verify HMAC-signed URL parameters. Backward-compatible: if no signing
	// secret is set the check is a no-op; if a sig IS present it must validate.
	// requireSig=false during rollout so agents holding stale (unsigned) URLs
	// don't get cut off; flip to true once the fleet is rotated.
	currentVersion := getCurrentAgentVersion()
	if err := verifyAgentUpdateURLSig(c.Request.URL.Query(), platform, arch, currentVersion, false); err != nil {
		log.Printf("[AgentUpdate] Rejected signed URL: %v (ip=%s ua=%q)", err, c.ClientIP(), c.Request.UserAgent())
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired download URL"})
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
	r.db.Pool().Exec(ctx, `
		INSERT INTO agent_updates (id, agent_id, from_version, to_version, platform, architecture, ip_address, status, created_at)
		VALUES ($1, $2, '', $3, $4, $5, $6, 'downloading', NOW())
	`, uuid.New(), agentID, getCurrentAgentVersion(), platform, arch, ipAddress)

	r.db.Pool().Exec(ctx, `
		UPDATE devices
		SET previous_agent_version = agent_version,
		    last_update_check = NOW()
		WHERE agent_id = $1
	`, agentID)
}

// listAgentVersions returns all available agent versions and their release info
func (r *Router) listAgentVersions(c *gin.Context) {
	agentVersion := getAgentVersionFromFile()

	rows, err := r.db.Pool().Query(c.Request.Context(), `
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

	rows, err := r.db.Pool().Query(c.Request.Context(), `
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
		sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	log.Printf("[UpdateStatus] agent=%s status=%s from=%s to=%s error=%q ip=%s",
		req.AgentID, req.Status, req.FromVersion, req.ToVersion, req.Error, c.ClientIP())

	// Validate status enum
	validStatuses := map[string]bool{
		"downloading": true, "staging": true, "applying": true,
		"completed": true, "failed": true, "rolled_back": true,
	}
	if !validStatuses[req.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status"})
		return
	}

	// Resolve agent: accept either hardware fingerprint (agent_id) or device UUID (id)
	var resolvedAgentID string
	err := r.db.Pool().QueryRow(c.Request.Context(),
		"SELECT agent_id FROM devices WHERE agent_id = $1 OR id::text = $1 LIMIT 1",
		req.AgentID).Scan(&resolvedAgentID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "Unknown agent"})
		return
	}
	req.AgentID = resolvedAgentID // Normalize to hardware fingerprint for remaining queries

	// Try to update existing record first
	result, err := r.db.Pool().Exec(c.Request.Context(), `
		UPDATE agent_updates
		SET status = $1::text, error_message = $2, completed_at = CASE WHEN $1::text IN ('completed', 'failed') THEN NOW() ELSE NULL END
		WHERE agent_id = $3 AND to_version = $4 AND status = 'downloading'
	`, req.Status, req.Error, req.AgentID, req.ToVersion)

	if err != nil {
		log.Printf("Failed to update agent_updates: %v", err)
		// Continue anyway - we don't want to block agent updates
	}

	// If no rows were updated, insert a new record
	if err == nil && result.RowsAffected() == 0 {
		r.db.Pool().Exec(c.Request.Context(), `
			INSERT INTO agent_updates (id, agent_id, from_version, to_version, platform, architecture, ip_address, status, error_message, created_at, completed_at)
			VALUES ($1, $2, $3, $4, '', '', $5, $6::text, $7, NOW(), CASE WHEN $6::text IN ('completed', 'failed') THEN NOW() ELSE NULL END)
			ON CONFLICT DO NOTHING
		`, uuid.New(), req.AgentID, req.FromVersion, req.ToVersion, c.ClientIP(), req.Status, req.Error)
	}

	// Phase 6 (v1.77.30): rollout outcome — flip the rollout_devices row from
	// 'dispatched' to 'succeeded' or 'failed' so the rollout ticker can finalise
	// the parent rollout. Intermediate statuses (downloading|staging|applying)
	// are progress signals, not terminal — leave the rollout row in 'dispatched'.
	if req.Status == "completed" || req.Status == "failed" || req.Status == "rolled_back" {
		var deviceUUID uuid.UUID
		if err := r.db.Pool().QueryRow(c.Request.Context(),
			`SELECT id FROM devices WHERE agent_id = $1`, req.AgentID).Scan(&deviceUUID); err == nil {
			success := req.Status == "completed"
			r.recordRolloutDeviceOutcome(c.Request.Context(), deviceUUID, success, req.Error)
		}
	}

	if req.Status == "completed" {
		r.db.Pool().Exec(c.Request.Context(), `
			UPDATE devices SET agent_version = $1, updated_at = NOW() WHERE agent_id = $2
		`, req.ToVersion, req.AgentID)

		// Auto-resolve any open update-related alerts for this device
		result, resolveErr := r.db.Pool().Exec(c.Request.Context(), `
			UPDATE alerts
			SET status = 'resolved', resolved_at = NOW()
			WHERE device_id = (SELECT id FROM devices WHERE agent_id = $1 LIMIT 1)
			  AND status = 'open'
			  AND (title LIKE '%Update Loop%' OR title LIKE '%Download Failed%' OR title LIKE '%Rolled Back%')
		`, req.AgentID)
		if resolveErr == nil && result.RowsAffected() > 0 {
			log.Printf("Auto-resolved update loop alert for agent %s after successful update to %s", req.AgentID, req.ToVersion)

			// Broadcast resolution to dashboards
			if r.hub != nil {
				var deviceID uuid.UUID
				var hostname string
				_ = r.db.Pool().QueryRow(c.Request.Context(),
					`SELECT id, COALESCE(hostname, '') FROM devices WHERE agent_id = $1`, req.AgentID,
				).Scan(&deviceID, &hostname)

				msg, _ := json.Marshal(map[string]interface{}{
					"type": "alert_resolved",
					"alert": map[string]interface{}{
						"deviceId": deviceID,
						"hostname": hostname,
						"title":    "Agent Update Loop Resolved",
						"status":   "resolved",
					},
				})
				r.hub.BroadcastToDashboards(msg)
			}
		}
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

// hasRecentUpdateFailure checks whether the given agent has a failed or rolled-back
// update to targetVersion within the last 30 minutes. Used to suppress update
// notifications so agents don't enter a download-fail-retry loop every heartbeat.
func (r *Router) hasRecentUpdateFailure(ctx context.Context, agentID, targetVersion string) bool {
	var failedAt time.Time
	err := r.db.Pool().QueryRow(ctx, `
		SELECT COALESCE(completed_at, created_at) FROM agent_updates
		WHERE agent_id = $1 AND to_version = $2 AND status IN ('failed', 'rolled_back')
		ORDER BY COALESCE(completed_at, created_at) DESC LIMIT 1
	`, agentID, targetVersion).Scan(&failedAt)
	return err == nil && time.Since(failedAt) < 30*time.Minute
}

func parseVersion(v string) [3]int {
	var parts [3]int
	split := strings.Split(strings.TrimPrefix(v, "v"), ".")
	for i := 0; i < 3 && i < len(split); i++ {
		parts[i], _ = strconv.Atoi(split[i])
	}
	return parts
}
