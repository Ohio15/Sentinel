package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// PublicLinkInfo is the response for validating a download token
type PublicLinkInfo struct {
	Valid               bool       `json:"valid"`
	DeviceName          string     `json:"deviceName,omitempty"`
	UserName            *string    `json:"userName,omitempty"`
	CompanyName         string     `json:"companyName,omitempty"`
	ExpiresAt           *time.Time `json:"expiresAt,omitempty"`
	Status              string     `json:"status,omitempty"`
	DownloadAvailable   bool       `json:"downloadAvailable"`
	AlreadyDownloaded   bool       `json:"alreadyDownloaded"`
	AlreadyInstalled    bool       `json:"alreadyInstalled"`
	DownloadCount       int        `json:"downloadCount,omitempty"`
	Error               string     `json:"error,omitempty"`
	Message             string     `json:"message,omitempty"`
	InstallInstructions string     `json:"installInstructions,omitempty"`
}

// PublicStatusResponse is the response for checking installation status
type PublicStatusResponse struct {
	Status         string     `json:"status"`
	AgentConnected bool       `json:"agentConnected"`
	ConnectedAt    *time.Time `json:"connectedAt,omitempty"`
	AgentVersion   string     `json:"agentVersion,omitempty"`
	DeviceID       *int       `json:"deviceId,omitempty"`
}

// validatePublicLinkHandler validates a download token and returns link info
func validatePublicLinkHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		downloadToken := c.Param("downloadToken")
		if downloadToken == "" {
			c.JSON(http.StatusBadRequest, PublicLinkInfo{
				Valid:   false,
				Error:   "not_found",
				Message: "Invalid installation link",
			})
			return
		}

		// Log access attempt
		logLinkAccess(services, downloadToken, c.ClientIP(), c.Request.UserAgent(), "view", true, nil)

		// Query the link
		var link AgentInstallationLink
		err := services.DB.Pool().QueryRow(c.Request.Context(), `
			SELECT id, device_name, user_name, expires_at, status,
			       downloaded_at, download_count, agent_connected_at, device_id
			FROM agent_installation_links
			WHERE download_token = $1 AND deleted_at IS NULL
		`, downloadToken).Scan(
			&link.ID, &link.DeviceName, &link.UserName, &link.ExpiresAt, &link.Status,
			&link.DownloadedAt, &link.DownloadCount, &link.AgentConnectedAt, &link.DeviceID,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				logLinkAccess(services, downloadToken, c.ClientIP(), c.Request.UserAgent(), "view", false, strPtr("Link not found"))
				c.JSON(http.StatusNotFound, PublicLinkInfo{
					Valid:   false,
					Error:   "not_found",
					Message: "This installation link was not found. Please contact your IT administrator for a new link.",
				})
				return
			}
			log.Printf("Failed to query link: %v", err)
			c.JSON(http.StatusInternalServerError, PublicLinkInfo{
				Valid:   false,
				Error:   "error",
				Message: "An error occurred. Please try again later.",
			})
			return
		}

		// Check if revoked
		if link.Status == "revoked" {
			c.JSON(http.StatusGone, PublicLinkInfo{
				Valid:   false,
				Error:   "revoked",
				Message: "This installation link has been cancelled by your administrator. Please contact IT if you believe this is an error.",
			})
			return
		}

		// Check if expired
		if time.Now().After(link.ExpiresAt) {
			// Update status if not already expired
			if link.Status != "expired" {
				services.DB.Pool().Exec(c.Request.Context(), `
					UPDATE agent_installation_links SET status = 'expired' WHERE id = $1
				`, link.ID)
			}
			c.JSON(http.StatusGone, PublicLinkInfo{
				Valid:   false,
				Error:   "expired",
				Message: "This installation link has expired. Please contact your IT administrator for a new link.",
			})
			return
		}

		// Get company name from settings
		companyName := "Your Organization"
		var settingsValue string
		if err := services.DB.Pool().QueryRow(c.Request.Context(),
			`SELECT value FROM settings WHERE key = 'companyName' LIMIT 1`).Scan(&settingsValue); err == nil && settingsValue != "" {
			companyName = settingsValue
		}

		response := PublicLinkInfo{
			Valid:               true,
			DeviceName:          link.DeviceName,
			UserName:            link.UserName,
			CompanyName:         companyName,
			ExpiresAt:           &link.ExpiresAt,
			Status:              link.Status,
			DownloadAvailable:   link.Status != "installed",
			AlreadyDownloaded:   link.DownloadedAt != nil,
			AlreadyInstalled:    link.AgentConnectedAt != nil,
			DownloadCount:       link.DownloadCount,
			InstallInstructions: getInstallInstructions(),
		}

		c.JSON(http.StatusOK, response)
	}
}

// getInstallInstructions returns the installation instructions HTML
func getInstallInstructions() string {
	return `<ol>
<li>Click the download button to get the installer package</li>
<li>Open the downloaded ZIP file and extract its contents</li>
<li>Run <strong>quick-install.ps1</strong> as Administrator</li>
<li>Wait for the installation to complete</li>
<li>The agent will connect automatically after installation</li>
</ol>`
}

// buildPatchedInstaller creates a patched installer EXE with embedded config.
//
// Format the deployed InnoSetup template expects (see
// installers/windows/sentinel-setup.iss ReadConfigViaPowerShell): a UTF-8
// marker `---SENTINEL-CONFIG---` somewhere in the LAST 10 KB of the EXE,
// followed immediately by UTF-8 JSON that must start with `{` and contain
// `"server_url"`. The PowerShell extractor reads everything from the byte
// after the marker through end-of-file as the config payload and writes it
// verbatim to C:\ProgramData\Sentinel\config.json (and {app}\config.json as
// a diagnostic copy). The agent then reads that file on first start to do
// initial enrollment.
//
// The previous implementation used bytes.Replace against
// `SENTINEL_CONFIG_SERVER:...:END` / `SENTINEL_CONFIG_TOKEN:...:END`
// placeholders that the current template was NOT built with — so the
// replace was always a silent no-op (returning a byte-identical unpatched
// installer) and every customer-facing install link silently failed to
// enroll. See GitHub issue #26 for the diagnosis.
func buildPatchedInstaller(serverURL, enrollmentToken string) ([]byte, error) {
	installerPath := findInstallerTemplate()
	if installerPath == "" {
		return nil, fmt.Errorf("installer template not found")
	}
	installerData, err := os.ReadFile(installerPath)
	if err != nil {
		return nil, fmt.Errorf("read installer template %s: %w", installerPath, err)
	}
	log.Printf("Using installer template from: %s", installerPath)

	// Minimal JSON payload — the agent's Config struct
	// (agent/internal/config/config.go) takes the rest from defaults / from
	// what the server returns at enrollment. We deliberately don't pre-set
	// agent_id, device_id, enrolled, etc. — those are populated by the
	// enrollment handshake.
	configJSON, err := json.Marshal(struct {
		ServerURL       string `json:"server_url"`
		EnrollmentToken string `json:"enrollment_token"`
	}{
		ServerURL:       serverURL,
		EnrollmentToken: enrollmentToken,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embedded install config: %w", err)
	}

	const marker = "---SENTINEL-CONFIG---"
	if len(configJSON)+len(marker) > 10000 {
		// The extractor searches only the last 10 KB. Refusing to ship a
		// payload too large to be found beats silently producing a
		// non-enrollable installer.
		return nil, fmt.Errorf("embedded config too large (%d bytes) — extractor searches last 10000 bytes only", len(configJSON)+len(marker))
	}

	patched := make([]byte, 0, len(installerData)+len(marker)+len(configJSON))
	patched = append(patched, installerData...)
	patched = append(patched, []byte(marker)...)
	patched = append(patched, configJSON...)
	return patched, nil
}

// downloadInstallerHandler serves the installer EXE for a download token
func downloadInstallerHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		downloadToken := c.Param("downloadToken")
		if downloadToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid download token"})
			return
		}

		// Query the link with enrollment token
		var link AgentInstallationLink
		var enrollmentToken string
		var enrollmentTokenIsLegacy bool
		err := services.DB.Pool().QueryRow(c.Request.Context(), `
			SELECT l.id, l.device_name, l.user_name, l.expires_at, l.status,
			       l.download_count, l.enrollment_token_id,
			       e.token, e.is_legacy
			FROM agent_installation_links l
			JOIN enrollment_tokens e ON l.enrollment_token_id = e.id
			WHERE l.download_token = $1 AND l.deleted_at IS NULL
		`, downloadToken).Scan(
			&link.ID, &link.DeviceName, &link.UserName, &link.ExpiresAt, &link.Status,
			&link.DownloadCount, &link.EnrollmentTokenID,
			&enrollmentToken, &enrollmentTokenIsLegacy,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				logLinkAccess(services, downloadToken, c.ClientIP(), c.Request.UserAgent(), "download", false, strPtr("Link not found"))
				c.JSON(http.StatusNotFound, gin.H{"error": "Installation link not found"})
				return
			}
			log.Printf("Failed to query link: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process download"})
			return
		}

		// Check if revoked
		if link.Status == "revoked" {
			logLinkAccess(services, downloadToken, c.ClientIP(), c.Request.UserAgent(), "download", false, strPtr("Link revoked"))
			c.JSON(http.StatusGone, gin.H{"error": "This installation link has been revoked"})
			return
		}

		// Check if expired
		if time.Now().After(link.ExpiresAt) {
			logLinkAccess(services, downloadToken, c.ClientIP(), c.Request.UserAgent(), "download", false, strPtr("Link expired"))
			c.JSON(http.StatusGone, gin.H{"error": "This installation link has expired"})
			return
		}

		// Rate limit: max 5 downloads per token. Atomic increment-with-guard so
		// two concurrent requests at count=4 can't both succeed and produce 6
		// downloads (TOCTOU race that the check-then-update form had).
		const maxDownloadsPerLink = 5
		var newCount int
		err = services.DB.Pool().QueryRow(c.Request.Context(), `
			UPDATE agent_installation_links
			SET downloaded_at = COALESCE(downloaded_at, NOW()),
			    download_ip = $1,
			    download_user_agent = $2,
			    download_count = download_count + 1,
			    status = CASE WHEN status = 'pending' THEN 'downloaded' ELSE status END
			WHERE id = $3 AND download_count < $4
			RETURNING download_count
		`, c.ClientIP(), c.Request.UserAgent(), link.ID, maxDownloadsPerLink).Scan(&newCount)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// Either the row no longer exists or the counter cap was hit.
				logLinkAccess(services, downloadToken, c.ClientIP(), c.Request.UserAgent(), "download", false, strPtr("Download limit exceeded"))
				c.JSON(http.StatusTooManyRequests, gin.H{"error": "Download limit exceeded. Contact your administrator."})
				return
			}
			log.Printf("Failed to update download tracking: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process download"})
			return
		}

		// Log access (only after the atomic increment succeeded)
		logLinkAccess(services, downloadToken, c.ClientIP(), c.Request.UserAgent(), "download", true, nil)

		// Get server URL for agent connection
		serverURL := services.Config.ServerURL
		if serverURL == "" {
			serverURL = fmt.Sprintf("%s://%s", detectRequestScheme(c), c.Request.Host)
		}

		// Build patched installer EXE
		installerData, err := buildPatchedInstaller(serverURL, enrollmentToken)
		if err != nil {
			log.Printf("Failed to build patched installer: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare installer"})
			return
		}

		// Serve the EXE file
		filename := fmt.Sprintf("SentinelAgent-Setup-%s.exe", sanitizeFilename(link.DeviceName))
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Length", fmt.Sprintf("%d", len(installerData)))
		c.Data(http.StatusOK, "application/octet-stream", installerData)
	}
}

// checkInstallationStatusHandler returns the installation status for polling
func checkInstallationStatusHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		downloadToken := c.Param("downloadToken")
		if downloadToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid download token"})
			return
		}

		var status string
		var agentConnectedAt *time.Time
		var deviceID *int

		err := services.DB.Pool().QueryRow(c.Request.Context(), `
			SELECT status, agent_connected_at, device_id
			FROM agent_installation_links
			WHERE download_token = $1 AND deleted_at IS NULL
		`, downloadToken).Scan(&status, &agentConnectedAt, &deviceID)

		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Link not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check status"})
			return
		}

		response := PublicStatusResponse{
			Status:         status,
			AgentConnected: agentConnectedAt != nil,
			ConnectedAt:    agentConnectedAt,
			DeviceID:       deviceID,
		}

		// Get agent version if connected
		if deviceID != nil {
			var agentVersion string
			if err := services.DB.Pool().QueryRow(c.Request.Context(),
				`SELECT COALESCE(agent_version, '') FROM devices WHERE id = $1`, *deviceID,
			).Scan(&agentVersion); err == nil {
				response.AgentVersion = agentVersion
			}
		}

		c.JSON(http.StatusOK, response)
	}
}

// logLinkAccess logs an access attempt to the access log
func logLinkAccess(services *Services, downloadToken, ipAddress, userAgent, action string, success bool, errorMessage *string) {
	// First get link ID
	var linkID uuid.UUID
	err := services.DB.Pool().QueryRow(context.Background(), `
		SELECT id FROM agent_installation_links WHERE download_token = $1
	`, downloadToken).Scan(&linkID)
	if err != nil {
		return // Can't log without link ID
	}

	_, _ = services.DB.Pool().Exec(context.Background(), `
		INSERT INTO agent_link_access_log (link_id, ip_address, user_agent, action, success, error_message)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, linkID, ipAddress, userAgent, action, success, errorMessage)
}

// sanitizeFilename removes/replaces characters that are unsafe for filenames
func sanitizeFilename(name string) string {
	// Replace spaces and special characters
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.ReplaceAll(name, ":", "")
	name = strings.ReplaceAll(name, "*", "")
	name = strings.ReplaceAll(name, "?", "")
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "<", "")
	name = strings.ReplaceAll(name, ">", "")
	name = strings.ReplaceAll(name, "|", "")

	// Limit length
	if len(name) > 50 {
		name = name[:50]
	}

	return name
}

// strPtr returns a pointer to a string
func strPtr(s string) *string {
	return &s
}

