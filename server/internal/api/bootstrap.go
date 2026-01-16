package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// BootstrapAgentInfo contains agent version and binary information
type BootstrapAgentInfo struct {
	Version   string `json:"version"`
	Checksum  string `json:"checksum"`
	Size      int64  `json:"size"`
	Platform  string `json:"platform"`
	Arch      string `json:"arch"`
	UpdatedAt string `json:"updatedAt"`
}

// Bootstrap binary placeholders (must match agent/cmd/sentinel-bootstrap/main.go)
const (
	bootstrapServerPlaceholder = "SENTINEL_BOOTSTRAP_SERVER:________________________________________________________________:END"
	bootstrapTokenPlaceholder  = "SENTINEL_BOOTSTRAP_TOKEN:________________________________________________________________:END"
	agentServerPlaceholder     = "SENTINEL_EMBEDDED_SERVER:________________________________________________________________:END"
	agentTokenPlaceholder      = "SENTINEL_EMBEDDED_TOKEN:________________________________________________________________:END"
)

// getBootstrapAgentInfoHandler returns agent version info for a platform/arch
func getBootstrapAgentInfoHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		platform := c.Query("platform")
		arch := c.Query("arch")

		if platform == "" {
			platform = runtime.GOOS
		}
		if arch == "" {
			arch = runtime.GOARCH
		}

		// Normalize platform/arch
		platform = strings.ToLower(platform)
		arch = strings.ToLower(arch)

		// Get agent binary path
		agentPath := getAgentBinaryPath(services, platform, arch)
		if agentPath == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Agent binary not found for this platform"})
			return
		}

		// Check if file exists
		stat, err := os.Stat(agentPath)
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Agent binary not found"})
			return
		}


		// Get version from version.json if available
		version := getAgentVersion(services)

		info := BootstrapAgentInfo{
			Version:   version,
			Checksum:  "", // Skip - binary modified during download
			Size:      stat.Size(),
			Platform:  platform,
			Arch:      arch,
			UpdatedAt: stat.ModTime().UTC().Format(time.RFC3339),
		}

		c.JSON(http.StatusOK, info)
	}
}

// downloadBootstrapHandler serves the bootstrapper binary with embedded configuration
func downloadBootstrapHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		platform := c.Query("platform")
		arch := c.Query("arch")
		token := c.Query("token")

		if platform == "" {
			platform = "windows"
		}
		if arch == "" {
			arch = "amd64"
		}

		// Normalize
		platform = strings.ToLower(platform)
		arch = strings.ToLower(arch)

		// Token is optional for bootstrapper download (can be embedded or passed at runtime)
		// But we validate if provided
		if token != "" {
			if err := validateEnrollmentToken(c, services, token); err != nil {
				return // Error already sent
			}
		}

		// Get bootstrapper binary path
		bootstrapPath := getBootstrapBinaryPath(services, platform, arch)
		if bootstrapPath == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Bootstrapper not found for this platform"})
			return
		}

		// Read binary
		binaryData, err := os.ReadFile(bootstrapPath)
		if err != nil {
			log.Printf("Error reading bootstrapper: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read bootstrapper"})
			return
		}

		// Get server URL
		serverURL := services.Config.ServerURL
		if serverURL == "" {
			// Construct from request
			scheme := "http"
			if c.Request.TLS != nil {
				scheme = "https"
			}
			serverURL = fmt.Sprintf("%s://%s", scheme, c.Request.Host)
		}

		// Embed configuration
		binaryData = embedBootstrapConfig(binaryData, serverURL, token)

		// Set filename
		ext := ""
		if platform == "windows" {
			ext = ".exe"
		}
		filename := fmt.Sprintf("sentinel-bootstrap-%s-%s%s", platform, arch, ext)

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.Header("Content-Type", "application/octet-stream")
		c.Data(http.StatusOK, "application/octet-stream", binaryData)
	}
}

// downloadBootstrapAgentHandler serves the agent binary with embedded configuration
func downloadBootstrapAgentHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		platform := c.Query("platform")
		arch := c.Query("arch")
		token := c.Query("token")

		if platform == "" {
			platform = "windows"
		}
		if arch == "" {
			arch = "amd64"
		}

		// Normalize
		platform = strings.ToLower(platform)
		arch = strings.ToLower(arch)

		// Token validation (optional but recommended)
		if token != "" {
			if err := validateEnrollmentToken(c, services, token); err != nil {
				return // Error already sent
			}

			// Increment token use count
			incrementTokenUseCount(c, services, token)
		}

		// Get agent binary path
		agentPath := getAgentBinaryPath(services, platform, arch)
		if agentPath == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Agent not found for this platform"})
			return
		}

		// Read binary
		binaryData, err := os.ReadFile(agentPath)
		if err != nil {
			log.Printf("Error reading agent: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read agent"})
			return
		}

		// Get server URL
		serverURL := services.Config.ServerURL
		if serverURL == "" {
			scheme := "http"
			if c.Request.TLS != nil {
				scheme = "https"
			}
			serverURL = fmt.Sprintf("%s://%s", scheme, c.Request.Host)
		}

		// Embed configuration
		binaryData = embedAgentConfig(binaryData, serverURL, token)

		// Set filename
		ext := ""
		if platform == "windows" {
			ext = ".exe"
		}
		filename := fmt.Sprintf("sentinel-agent-%s-%s%s", platform, arch, ext)

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.Header("Content-Type", "application/octet-stream")
		c.Data(http.StatusOK, "application/octet-stream", binaryData)
	}
}

// Helper functions

func getBootstrapBinaryPath(services *Services, platform, arch string) string {
	// Check in multiple locations
	baseName := fmt.Sprintf("sentinel-bootstrap-%s-%s", platform, arch)
	if platform == "windows" {
		baseName += ".exe"
	}

	// Check in various directories (server deployment locations)
	paths := []string{
		filepath.Join("agent", baseName),
		filepath.Join("release", "agent", baseName),
		filepath.Join("installers", baseName),
		baseName,
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}

func getAgentBinaryPath(services *Services, platform, arch string) string {
	// Check in multiple locations
	baseName := fmt.Sprintf("sentinel-agent-%s-%s", platform, arch)
	if platform == "windows" {
		baseName += ".exe"
	}

	// Also check for standard names without platform suffix
	standardName := "sentinel-agent"
	if platform == "windows" {
		standardName += ".exe"
	}

	paths := []string{
		filepath.Join("release", "agent", baseName),
		filepath.Join("release", "agent", standardName),
		filepath.Join("agent", baseName),
		filepath.Join("installers", baseName),
		baseName,
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}

func getAgentVersion(services *Services) string {
	// Try to read from version.json
	paths := []string{
		filepath.Join("release", "agent", "version.json"),
		filepath.Join("agent", "version.json"),
		"version.json",
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}

		var versionInfo struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &versionInfo); err == nil && versionInfo.Version != "" {
			return versionInfo.Version
		}
	}

	return "1.0.0" // Default version
}

// NOTE: calculateFileChecksum is defined in agent_updates.go

func embedBootstrapConfig(binaryData []byte, serverURL, token string) []byte {
	// Embed server URL
	if serverURL != "" {
		paddedServer := padString(serverURL, 64)
		replacement := "SENTINEL_BOOTSTRAP_SERVER:" + paddedServer + ":END"
		binaryData = replaceInBinary(binaryData, bootstrapServerPlaceholder, replacement)
	}

	// Embed token
	if token != "" {
		paddedToken := padString(token, 64)
		replacement := "SENTINEL_BOOTSTRAP_TOKEN:" + paddedToken + ":END"
		binaryData = replaceInBinary(binaryData, bootstrapTokenPlaceholder, replacement)
	}

	return binaryData
}

func embedAgentConfig(binaryData []byte, serverURL, token string) []byte {
	// Embed server URL
	if serverURL != "" {
		paddedServer := padString(serverURL, 64)
		replacement := "SENTINEL_EMBEDDED_SERVER:" + paddedServer + ":END"
		binaryData = replaceInBinary(binaryData, agentServerPlaceholder, replacement)
	}

	// Embed token
	if token != "" {
		paddedToken := padString(token, 64)
		replacement := "SENTINEL_EMBEDDED_TOKEN:" + paddedToken + ":END"
		binaryData = replaceInBinary(binaryData, agentTokenPlaceholder, replacement)
	}

	return binaryData
}

func padString(s string, length int) string {
	if len(s) >= length {
		return s[:length]
	}
	return s + strings.Repeat("_", length-len(s))
}

func replaceInBinary(data []byte, old, new string) []byte {
	return []byte(strings.Replace(string(data), old, new, 1))
}

func validateEnrollmentToken(c *gin.Context, services *Services, token string) error {
	var tokenID uuid.UUID
	var isActive bool
	var expiresAt *time.Time
	var maxUses *int
	var useCount int

	err := services.DB.Pool().QueryRow(c.Request.Context(), `
		SELECT id, is_active, expires_at, max_uses, use_count
		FROM enrollment_tokens WHERE token = $1
	`, token).Scan(&tokenID, &isActive, &expiresAt, &maxUses, &useCount)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid enrollment token"})
		return err
	}

	if !isActive {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Enrollment token is disabled"})
		return fmt.Errorf("token disabled")
	}

	if expiresAt != nil && time.Now().After(*expiresAt) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Enrollment token has expired"})
		return fmt.Errorf("token expired")
	}

	if maxUses != nil && useCount >= *maxUses {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Enrollment token has reached maximum uses"})
		return fmt.Errorf("token max uses reached")
	}

	return nil
}

func incrementTokenUseCount(c *gin.Context, services *Services, token string) {
	_, err := services.DB.Pool().Exec(c.Request.Context(), `
		UPDATE enrollment_tokens SET use_count = use_count + 1 WHERE token = $1
	`, token)
	if err != nil {
		log.Printf("Error incrementing token use count: %v", err)
	}
}
