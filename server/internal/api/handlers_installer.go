package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/constants"
)

// InstallerConfig represents the configuration embedded in the installer
type InstallerConfig struct {
	ServerURL       string `json:"server_url"`
	GRPCEndpoint    string `json:"grpc_endpoint"`
	EnrollmentToken string `json:"enrollment_token"`
	OrganizationID  int    `json:"organization_id"`
	GeneratedAt     string `json:"generated_at"`
}

// Config marker for appending JSON to binary
const installerConfigMarker = "---SENTINEL-CONFIG---"

// generateInstallerDownloadHandler generates a customized installer with embedded configuration
// GET /api/agent/installer?platform=windows&arch=amd64
// Requires JWT authentication
func generateInstallerDownloadHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get query parameters
		platform := c.DefaultQuery("platform", "")
		arch := c.DefaultQuery("arch", "amd64")

		// Validate platform
		if platform == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "platform parameter is required",
				"supported_values":  []string{"windows", "linux-deb", "linux-rpm", "macos"},
			})
			return
		}

		platform = strings.ToLower(platform)
		arch = strings.ToLower(arch)

		// Validate platform values
		validPlatforms := map[string]bool{
			"windows":   true,
			"linux-deb": true,
			"linux-rpm": true,
			"macos":     true,
		}
		if !validPlatforms[platform] {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":            "invalid platform",
				"supported_values": []string{"windows", "linux-deb", "linux-rpm", "macos"},
			})
			return
		}

		// Validate arch values
		validArch := map[string]bool{
			"amd64": true,
			"arm64": true,
		}
		if !validArch[arch] {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":            "invalid architecture",
				"supported_values": []string{"amd64", "arm64"},
			})
			return
		}

		// Get user info from context for logging/tracking
		var userID *uuid.UUID
		if uid, exists := c.Get("userID"); exists {
			if u, ok := uid.(uuid.UUID); ok {
				userID = &u
			}
		}

		// Build server URL
		serverURL := services.Config.PublicURL
		if serverURL == "" {
			serverURL = services.Config.ServerURL
		}
		if serverURL == "" {
			// Derive from request
			scheme := c.GetHeader("X-Forwarded-Proto")
			if scheme == "" {
				if c.Request.TLS != nil {
					scheme = "https"
				} else {
					scheme = "https" // Default to https for production
				}
			}
			host := c.GetHeader("X-Forwarded-Host")
			if host == "" {
				host = c.Request.Host
			}
			serverURL = fmt.Sprintf("%s://%s", scheme, host)
		}

		// Build gRPC endpoint
		grpcEndpoint := buildGRPCEndpoint(services, c)

		// Generate or retrieve enrollment token for this organization
		enrollmentToken, err := getOrCreateEnrollmentToken(c, services, userID)
		if err != nil {
			log.Printf("[Installer] Failed to get enrollment token: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate enrollment token"})
			return
		}

		// Build the configuration to embed
		config := InstallerConfig{
			ServerURL:       serverURL,
			GRPCEndpoint:    grpcEndpoint,
			EnrollmentToken: enrollmentToken,
			OrganizationID:  constants.CurrentOrganizationID,
			GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		}

		// Get the base installer path
		installerPath := getBaseInstallerPath(platform, arch)
		if installerPath == "" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":    "Base installer not found for this platform/architecture",
				"platform": platform,
				"arch":     arch,
			})
			return
		}

		// Read the base installer
		installerData, err := os.ReadFile(installerPath)
		if err != nil {
			log.Printf("[Installer] Failed to read base installer %s: %v", installerPath, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read base installer"})
			return
		}

		// Inject configuration based on platform
		var outputData []byte
		var filename string
		var contentType string

		switch platform {
		case "windows":
			outputData, err = injectConfigWindows(installerData, config)
			filename = fmt.Sprintf("sentinel-setup-%s-%s.exe", platform, arch)
			contentType = "application/octet-stream"

		case "linux-deb":
			// TODO: For Linux DEB packages, configuration injection requires modifying
			// the package contents (adding a config file inside). For now, return the
			// base package with instructions to configure via command-line.
			outputData = installerData
			filename = fmt.Sprintf("sentinel-agent-%s.deb", arch)
			contentType = "application/vnd.debian.binary-package"
			log.Printf("[Installer] Linux DEB config injection not yet implemented - returning base package")

		case "linux-rpm":
			// TODO: For Linux RPM packages, configuration injection requires modifying
			// the package contents (adding a config file inside). For now, return the
			// base package with instructions to configure via command-line.
			outputData = installerData
			filename = fmt.Sprintf("sentinel-agent-%s.rpm", arch)
			contentType = "application/x-rpm"
			log.Printf("[Installer] Linux RPM config injection not yet implemented - returning base package")

		case "macos":
			// TODO: For macOS PKG packages, configuration injection requires modifying
			// the package contents (adding a config file inside). For now, return the
			// base package with instructions to configure via command-line.
			outputData = installerData
			filename = fmt.Sprintf("sentinel-agent-%s.pkg", arch)
			contentType = "application/octet-stream"
			log.Printf("[Installer] macOS PKG config injection not yet implemented - returning base package")

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported platform"})
			return
		}

		if err != nil {
			log.Printf("[Installer] Failed to inject config: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare installer"})
			return
		}

		// Log the download
		logInstallerDownload(c, services, platform, arch, userID)

		// Set response headers
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		c.Header("Content-Type", contentType)
		c.Header("Content-Length", fmt.Sprintf("%d", len(outputData)))
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("X-Sentinel-Config-Injected", fmt.Sprintf("%v", platform == "windows"))

		// Send the installer
		c.Data(http.StatusOK, contentType, outputData)
	}
}

// buildGRPCEndpoint constructs the gRPC endpoint URL
func buildGRPCEndpoint(services *Services, c *gin.Context) string {
	// Check if there's a specific gRPC endpoint configured
	if services.Config.GRPCPort > 0 {
		// Extract host from PublicURL or request
		host := ""
		if services.Config.PublicURL != "" {
			// Parse host from URL
			host = extractHostFromURL(services.Config.PublicURL)
		}
		if host == "" && services.Config.ServerURL != "" {
			host = extractHostFromURL(services.Config.ServerURL)
		}
		if host == "" {
			host = c.GetHeader("X-Forwarded-Host")
			if host == "" {
				host = c.Request.Host
			}
			// Remove port if present
			if idx := strings.Index(host, ":"); idx != -1 {
				host = host[:idx]
			}
		}
		return fmt.Sprintf("%s:%d", host, services.Config.GRPCPort)
	}

	// Default gRPC endpoint (same host, port 4444)
	host := extractHostFromURL(services.Config.PublicURL)
	if host == "" {
		host = "sentinelrmm.us"
	}
	return fmt.Sprintf("%s:4444", host)
}

// extractHostFromURL extracts the hostname from a URL
func extractHostFromURL(urlStr string) string {
	if urlStr == "" {
		return ""
	}
	// Remove protocol
	urlStr = strings.TrimPrefix(urlStr, "https://")
	urlStr = strings.TrimPrefix(urlStr, "http://")
	// Remove path
	if idx := strings.Index(urlStr, "/"); idx != -1 {
		urlStr = urlStr[:idx]
	}
	// Remove port
	if idx := strings.Index(urlStr, ":"); idx != -1 {
		urlStr = urlStr[:idx]
	}
	return urlStr
}

// getOrCreateEnrollmentToken gets an existing enrollment token or creates a new one
func getOrCreateEnrollmentToken(c *gin.Context, services *Services, userID *uuid.UUID) (string, error) {
	ctx := c.Request.Context()

	// First, try to find an active, non-expired token for installer downloads
	var existingToken string
	err := services.DB.Pool().QueryRow(ctx, `
		SELECT token FROM enrollment_tokens
		WHERE organization_id = $1
		  AND is_active = TRUE
		  AND (expires_at IS NULL OR expires_at > NOW())
		  AND name LIKE 'Installer Download%'
		ORDER BY created_at DESC
		LIMIT 1
	`, constants.CurrentOrganizationID).Scan(&existingToken)

	if err == nil && existingToken != "" {
		return existingToken, nil
	}

	// Create a new token
	newToken := uuid.New().String()
	expiresAt := time.Now().Add(30 * 24 * time.Hour) // 30-day validity

	_, err = services.DB.Pool().Exec(ctx, `
		INSERT INTO enrollment_tokens (
			organization_id, token, name, description, created_by,
			expires_at, is_active, is_legacy
		) VALUES ($1, $2, $3, $4, $5, $6, TRUE, FALSE)
	`, constants.CurrentOrganizationID,
		newToken,
		fmt.Sprintf("Installer Download - %s", time.Now().Format("2006-01-02")),
		"Auto-generated token for installer download API",
		userID,
		expiresAt)

	if err != nil {
		return "", fmt.Errorf("failed to create enrollment token: %w", err)
	}

	return newToken, nil
}

// getBaseInstallerPath returns the path to the base installer template
func getBaseInstallerPath(platform, arch string) string {
	// Map platform to directory structure
	var baseName string
	var extension string

	switch platform {
	case "windows":
		baseName = "sentinel-setup-base"
		extension = ".exe"
	case "linux-deb":
		baseName = "sentinel-agent-base-" + arch
		extension = ".deb"
	case "linux-rpm":
		baseName = "sentinel-agent-base-" + arch
		extension = ".rpm"
	case "macos":
		baseName = "sentinel-agent-base-" + arch
		extension = ".pkg"
	default:
		return ""
	}

	// Check multiple locations
	searchPaths := []string{
		filepath.Join("installers", platform, baseName+extension),
		filepath.Join("installers", baseName+extension),
		filepath.Join("release", "agent", baseName+extension),
		filepath.Join("/app/installers", platform, baseName+extension),
		filepath.Join("/app/installers", baseName+extension),
	}

	// For Windows, also check for template installer
	if platform == "windows" {
		searchPaths = append([]string{
			filepath.Join("installers", "sentinel-installer-template.exe"),
			filepath.Join("/app/installers", "sentinel-installer-template.exe"),
		}, searchPaths...)
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// injectConfigWindows appends JSON configuration to Windows EXE with marker
func injectConfigWindows(installerData []byte, config InstallerConfig) ([]byte, error) {
	// First, try the existing XYZCFG binary patching method (same as buildPatchedInstallerWithCode)
	// This is for installers that have the config block placeholder
	placeholder := make([]byte, 200)
	copy(placeholder[0:6], []byte("XYZCFG"))
	copy(placeholder[6:59], []byte("https://config-placeholder-url.local_________________"))
	copy(placeholder[59:112], []byte("token-placeholder-value-replace-me___________________"))
	copy(placeholder[112:121], []byte("CODE_____"))

	if bytes.Contains(installerData, placeholder) {
		// Use binary patching method
		patched := make([]byte, 200)
		copy(patched[0:6], []byte("XYZCFG"))
		copy(patched[6:59], []byte(padConfigString(config.ServerURL, 53)))
		copy(patched[59:112], []byte(padConfigString(config.EnrollmentToken, 53)))
		copy(patched[112:121], []byte(padConfigString("API_DL___", 9))) // Mark as API download

		result := bytes.Replace(installerData, placeholder, patched, 1)
		log.Printf("[Installer] Windows: Used XYZCFG binary patching method")
		return result, nil
	}

	// Fallback: Append JSON config with marker to end of binary
	// This method appends the config after the exe, which the installer can read
	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	// Create the config block: marker + JSON + newline
	configBlock := []byte(installerConfigMarker)
	configBlock = append(configBlock, configJSON...)
	configBlock = append(configBlock, '\n')

	// Append to installer
	result := make([]byte, len(installerData)+len(configBlock))
	copy(result, installerData)
	copy(result[len(installerData):], configBlock)

	log.Printf("[Installer] Windows: Appended config marker with %d bytes of config", len(configJSON))
	return result, nil
}

// padConfigString pads a string to exactly the specified length
func padConfigString(s string, length int) string {
	if len(s) >= length {
		return s[:length]
	}
	return s + strings.Repeat("_", length-len(s))
}

// logInstallerDownload logs the installer download for auditing
func logInstallerDownload(c *gin.Context, services *Services, platform, arch string, userID *uuid.UUID) {
	ctx := c.Request.Context()

	// Try to log to database (don't fail if this fails)
	_, err := services.DB.Pool().Exec(ctx, `
		INSERT INTO agent_downloads (
			organization_id, platform, architecture,
			ip_address, user_agent, downloaded_by, downloaded_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`, constants.CurrentOrganizationID, platform, arch,
		c.ClientIP(), c.Request.UserAgent(), userID)

	if err != nil {
		// Table might not exist yet, just log
		log.Printf("[Installer] Download logged: platform=%s arch=%s ip=%s user=%v (db log failed: %v)",
			platform, arch, c.ClientIP(), userID, err)
	} else {
		log.Printf("[Installer] Download logged: platform=%s arch=%s ip=%s user=%v",
			platform, arch, c.ClientIP(), userID)
	}
}
