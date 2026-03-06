package api

import (
	"bytes"
	"crypto/rand"
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
		// Extend write deadline for large binary transfers
		rc := http.NewResponseController(c.Writer)
		if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Minute)); err != nil {
			log.Printf("[Bootstrap] Warning: failed to extend write deadline: %v", err)
		}

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

// downloadBootstrapWatchdogHandler serves the watchdog binary
func downloadBootstrapWatchdogHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extend write deadline for large binary transfers
		rc := http.NewResponseController(c.Writer)
		if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Minute)); err != nil {
			log.Printf("[Bootstrap] Warning: failed to extend write deadline: %v", err)
		}

		platform := c.Query("platform")
		arch := c.Query("arch")

		if platform == "" {
			platform = "windows"
		}
		if arch == "" {
			arch = "amd64"
		}

		// Normalize
		platform = strings.ToLower(platform)
		arch = strings.ToLower(arch)

		// Get watchdog binary path
		watchdogPath := getWatchdogBinaryPath(services, platform, arch)
		if watchdogPath == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Watchdog not found for this platform"})
			return
		}

		// Read binary
		binaryData, err := os.ReadFile(watchdogPath)
		if err != nil {
			log.Printf("Error reading watchdog: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read watchdog"})
			return
		}

		// Set filename
		ext := ""
		if platform == "windows" {
			ext = ".exe"
		}
		filename := fmt.Sprintf("sentinel-watchdog-%s-%s%s", platform, arch, ext)

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.Header("Content-Type", "application/octet-stream")
		c.Data(http.StatusOK, "application/octet-stream", binaryData)
	}
}

func getWatchdogBinaryPath(services *Services, platform, arch string) string {
	// Check in multiple locations
	baseName := fmt.Sprintf("sentinel-watchdog-%s-%s", platform, arch)
	if platform == "windows" {
		baseName += ".exe"
	}

	paths := []string{
		filepath.Join("release", "agent", baseName),
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

// downloadBootstrapDesktopHelperHandler serves the desktop helper binary for WebRTC remote desktop
func downloadBootstrapDesktopHelperHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		platform := c.Query("platform")
		arch := c.Query("arch")

		if platform == "" {
			platform = "windows"
		}
		if arch == "" {
			arch = "amd64"
		}

		// Normalize
		platform = strings.ToLower(platform)
		arch = strings.ToLower(arch)

		// Only Windows is supported for desktop helper
		if platform != "windows" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Desktop helper only available for Windows"})
			return
		}

		// Get desktop helper binary path
		helperPath := getDesktopHelperBinaryPath(services, platform, arch)
		if helperPath == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Desktop helper not found for this platform"})
			return
		}

		// Read binary
		binaryData, err := os.ReadFile(helperPath)
		if err != nil {
			log.Printf("Error reading desktop helper: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read desktop helper"})
			return
		}

		filename := fmt.Sprintf("sentinel-desktop-%s-%s.exe", platform, arch)

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.Header("Content-Type", "application/octet-stream")
		c.Data(http.StatusOK, "application/octet-stream", binaryData)
	}
}

func getDesktopHelperBinaryPath(services *Services, platform, arch string) string {
	// Check in multiple locations
	baseName := fmt.Sprintf("sentinel-desktop-%s-%s", platform, arch)
	if platform == "windows" {
		baseName += ".exe"
	}

	paths := []string{
		filepath.Join("release", "agent", baseName),
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

// generateConfiguredInstallerHandler generates a pre-configured installer with embedded server URL, token, and code
// This is for the "Direct Download" feature in the web dashboard
// For Windows: returns a patched EXE installer with embedded config (same method as installation links)
// For Linux/macOS: returns a pre-configured shell script
func generateConfiguredInstallerHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		platform := c.Param("platform")
		if platform == "" {
			platform = "windows"
		}
		platform = strings.ToLower(platform)

		// Get user info from context for tracking
		var createdBy *uuid.UUID
		if userID, exists := c.Get("userID"); exists {
			if uid, ok := userID.(uuid.UUID); ok {
				createdBy = &uid
			}
		}

		// Generate a unique installation code (like ABCD-1234)
		installCode := generateDirectDownloadCode()

		// Create enrollment token for this download
		enrollmentToken := uuid.New().String()
		expiresAt := time.Now().Add(7 * 24 * time.Hour) // 7-day validity

		// Create enrollment token in database
		var enrollmentTokenID uuid.UUID
		err := services.DB.Pool().QueryRow(c.Request.Context(), `
			INSERT INTO enrollment_tokens (
				token, name, description, created_by, expires_at, max_uses, is_active, is_legacy
			) VALUES ($1, $2, $3, $4, $5, 1, TRUE, FALSE)
			RETURNING id
		`, enrollmentToken,
			fmt.Sprintf("Direct Download: %s", installCode),
			fmt.Sprintf("Auto-generated for direct download %s", installCode),
			createdBy, expiresAt).Scan(&enrollmentTokenID)
		if err != nil {
			log.Printf("Error creating enrollment token: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate enrollment token"})
			return
		}

		// Create installation link record for tracking (with the code)
		downloadToken := "DD-" + uuid.New().String()
		_, err = services.DB.Pool().Exec(c.Request.Context(), `
			INSERT INTO agent_installation_links (
				download_token, installation_code, device_name,
				enrollment_token_id, created_by, expires_at, notes, status
			) VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending')
		`, downloadToken, installCode, "Direct Download",
			enrollmentTokenID, createdBy, expiresAt, "Created via Direct Download")
		if err != nil {
			log.Printf("Error creating installation link: %v", err)
			// Continue anyway - the token still works
		}

		// Get server URL - use PUBLIC_URL if set, otherwise derive from request headers
		// Priority: PUBLIC_URL env > X-Forwarded headers > Request.Host
		serverURL := services.Config.PublicURL
		if serverURL == "" {
			serverURL = services.Config.ServerURL
		}
		if serverURL == "" {
			// Get scheme from X-Forwarded-Proto (set by Traefik)
			scheme := c.GetHeader("X-Forwarded-Proto")
			if scheme == "" {
				scheme = "https"
			}

			// Get host from X-Forwarded-Host (set by Traefik), fallback to Request.Host
			host := c.GetHeader("X-Forwarded-Host")
			if host == "" {
				host = c.Request.Host
			}

			// Remove port 8443 (mTLS) and use 4443 instead for public access
			if strings.HasSuffix(host, ":8443") {
				host = strings.TrimSuffix(host, ":8443") + ":4443"
			}

			// If still localhost, use the domain from config or a sensible default
			if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
				// Try to get from DOMAIN environment variable
				if domain := os.Getenv("DOMAIN"); domain != "" {
					host = domain + ":4443"
				}
			}

			serverURL = fmt.Sprintf("%s://%s", scheme, host)
		}
		log.Printf("[Installer] Using server URL: %s (X-Forwarded-Host: %s, Request.Host: %s)",
			serverURL, c.GetHeader("X-Forwarded-Host"), c.Request.Host)

		switch platform {
		case "windows":
			// Use the patched EXE method with embedded config AND installation code
			installerData, err := buildPatchedInstallerWithCode(serverURL, enrollmentToken, installCode)
			if err != nil {
				internalError(c, "Failed to prepare installer", err)
				return
			}

			c.Header("Content-Disposition", "attachment; filename=\"SentinelAgent-Setup.exe\"")
			c.Header("Content-Type", "application/octet-stream")
			c.Header("Content-Length", fmt.Sprintf("%d", len(installerData)))
			c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
			c.Data(http.StatusOK, "application/octet-stream", installerData)

		case "linux", "macos":
			// For Linux/macOS, use the shell script approach
			script := generateLinuxInstaller(serverURL, enrollmentToken)
			c.Header("Content-Disposition", "attachment; filename=sentinel-install.sh")
			c.Header("Content-Type", "application/x-sh")
			c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
			c.Data(http.StatusOK, "application/x-sh", []byte(script))

		case "synology":
			// For Synology NAS, use a specialized script for DSM
			script := generateSynologyInstaller(serverURL, enrollmentToken)
			c.Header("Content-Disposition", "attachment; filename=sentinel-synology-install.sh")
			c.Header("Content-Type", "application/x-sh")
			c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
			c.Data(http.StatusOK, "application/x-sh", []byte(script))

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid platform. Supported: windows, linux, macos, synology"})
			return
		}
	}
}

// generateDirectDownloadCode generates a random 8-character code in XXXX-XXXX format
func generateDirectDownloadCode() string {
	const chars = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // Excludes ambiguous: 0,O,1,I,L
	code := make([]byte, 8)
	for i := range code {
		randByte := make([]byte, 1)
		rand.Read(randByte)
		code[i] = chars[int(randByte[0])%len(chars)]
	}
	return string(code[0:4]) + "-" + string(code[4:8])
}

// buildPatchedInstallerWithCode creates a patched installer EXE with embedded config AND installation code
func buildPatchedInstallerWithCode(serverURL, enrollmentToken, installCode string) ([]byte, error) {
	// Find installer template
	installerPaths := []string{
		"installers/sentinel-installer-template.exe",
		"release/agent/sentinel-installer-template.exe",
		"../installers/sentinel-installer-template.exe",
		"/app/installers/sentinel-installer-template.exe",
	}

	var installerData []byte
	var err error
	for _, path := range installerPaths {
		installerData, err = os.ReadFile(path)
		if err == nil {
			log.Printf("Using installer template from: %s", path)
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("installer template not found: %w", err)
	}

	// Binary patch the placeholders
	// The installer has these embedded variables that get patched:
	// SENTINEL_EMBEDDED_SERVER:https://placeholder-server-url-padding-to-64-chars___:END (64 char value)
	// SENTINEL_EMBEDDED_TOKEN:placeholder-token-value-padding-to-64-characters_____:END (64 char value)
	// SENTINEL_EMBEDDED_CODE:_________:END (9 chars for XXXX-XXXX)

	// Fixed-size config block patching
	// The installer has a 200-byte config block starting with "XYZCFG" magic
	// Layout:
	//   [0:6]   = "XYZCFG" magic header (unchanged)
	//   [6:59]  = Server URL (53 bytes, underscore-padded)
	//   [59:112] = Token (53 bytes, underscore-padded)
	//   [112:121] = Code (9 bytes, underscore-padded)
	//   [121:200] = Reserved/padding (zeros)

	// Build the placeholder config block
	placeholder := make([]byte, 200)
	copy(placeholder[0:6], []byte("XYZCFG"))
	copy(placeholder[6:59], []byte("https://config-placeholder-url.local_________________"))
	copy(placeholder[59:112], []byte("token-placeholder-value-replace-me___________________"))
	copy(placeholder[112:121], []byte("CODE_____"))

	// Build the patched config block
	patched := make([]byte, 200)
	copy(patched[0:6], []byte("XYZCFG"))
	copy(patched[6:59], []byte(padRightStr(serverURL, 53, '_')))
	copy(patched[59:112], []byte(padRightStr(enrollmentToken, 53, '_')))
	copy(patched[112:121], []byte(padRightStr(installCode, 9, '_')))

	if bytes.Contains(installerData, placeholder) {
		installerData = bytes.Replace(installerData, placeholder, patched, 1)
		log.Printf("[Patch] Config block patched: server=%s, token=%s..., code=%s",
			serverURL, enrollmentToken[:min(8, len(enrollmentToken))], installCode)
	} else {
		log.Printf("[Patch] ERROR: Config block not found in binary! Looking for XYZCFG magic...")
		// Try to find the magic header to debug
		if idx := bytes.Index(installerData, []byte("XYZCFG")); idx >= 0 {
			log.Printf("[Patch] Found XYZCFG at offset %d, but full block didn't match", idx)
			log.Printf("[Patch] Actual bytes: %x", installerData[idx:idx+20])
		}
	}

	return installerData, nil
}

// padRightStr pads a string to the specified length with the given character
func padRightStr(s string, length int, pad rune) string {
	if len(s) >= length {
		return s[:length]
	}
	return s + strings.Repeat(string(pad), length-len(s))
}

// generateWindowsInstaller creates a PowerShell script with UAC auto-elevation and embedded config
func generateWindowsInstaller(serverURL, token string) string {
	// Note: Using string concatenation because Go raw strings can't contain backticks
	// PowerShell backticks are replaced with [char]96 or alternative syntax
	script := `#Requires -Version 5.1
# Sentinel Agent Installer - Pre-configured for direct download
# This script will automatically request administrator privileges if needed

param(
    [switch]$Silent,
    [switch]$Force
)

# Configuration (pre-embedded from server)
$Script:Server = "` + serverURL + `"
$Script:Token = "` + token + `"

# Check if running as administrator
function Test-Administrator {
    $currentUser = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($currentUser)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

# Self-elevate with UAC if not running as admin
if (-not (Test-Administrator)) {
    Write-Host "Requesting administrator privileges..." -ForegroundColor Yellow

    # Build argument list preserving parameters
    $arguments = "-NoProfile -ExecutionPolicy Bypass -File " + [char]34 + $PSCommandPath + [char]34
    if ($Silent) { $arguments += " -Silent" }
    if ($Force) { $arguments += " -Force" }

    try {
        $process = Start-Process -FilePath "powershell.exe" -ArgumentList $arguments -Verb RunAs -Wait -PassThru
        exit $process.ExitCode
    } catch {
        Write-Host "ERROR: Administrator privileges are required to install Sentinel Agent." -ForegroundColor Red
        Write-Host "Please right-click the script and select 'Run with PowerShell' or run from an elevated prompt." -ForegroundColor Yellow
        if (-not $Silent) {
            Write-Host ""
            Write-Host "Press any key to close..." -ForegroundColor Gray
            $null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown") 2>$null
        }
        exit 1
    }
}

# Set error handling
$ErrorActionPreference = "Stop"
trap {
    Write-Host ""
    Write-Host "ERROR: $_" -ForegroundColor Red
    if (-not $Silent) {
        Write-Host ""
        Write-Host "Press any key to close..." -ForegroundColor Gray
        $null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown") 2>$null
    }
    exit 1
}

# ASCII Banner
function Show-Banner {
    Write-Host ""
    Write-Host "  ____             _   _            _ " -ForegroundColor Cyan
    Write-Host " / ___|  ___ _ __ | |_(_)_ __   ___| |" -ForegroundColor Cyan
    Write-Host " \___ \ / _ \ '_ \| __| | '_ \ / _ \ |" -ForegroundColor Cyan
    Write-Host "  ___) |  __/ | | | |_| | | | |  __/ |" -ForegroundColor Cyan
    Write-Host " |____/ \___|_| |_|\__|_|_| |_|\___|_|" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "       Remote Monitoring & Management" -ForegroundColor DarkCyan
    Write-Host ""
}

function Write-Step { param([string]$Message); Write-Host "[*] $Message" -ForegroundColor Yellow }
function Write-Success { param([string]$Message); Write-Host "[+] $Message" -ForegroundColor Green }
function Write-InstallError { param([string]$Message); Write-Host "[!] $Message" -ForegroundColor Red }

# Main installation
if (-not $Silent) { Show-Banner }

Write-Success "Running with administrator privileges"
Write-Host "      Server: $Script:Server" -ForegroundColor Gray

# Download bootstrapper
Write-Step "Downloading Sentinel Agent..."
$bootstrapperUrl = "$Script:Server/api/bootstrap/download?platform=windows&arch=amd64&token=$Script:Token"
$bootstrapperPath = Join-Path $env:TEMP "sentinel-bootstrap.exe"

try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    $ProgressPreference = 'SilentlyContinue'

    $webClient = New-Object System.Net.WebClient
    $webClient.Headers.Add("User-Agent", "Sentinel-Installer/1.0")
    $webClient.DownloadFile($bootstrapperUrl, $bootstrapperPath)
} catch {
    $errorMsg = $_.Exception.Message
    if ($_.Exception.InnerException) {
        $errorMsg = $_.Exception.InnerException.Message
    }
    Write-InstallError "Download failed: $errorMsg"
    throw "Failed to download agent from $Script:Server"
}

if (-not (Test-Path $bootstrapperPath)) {
    throw "Download failed - installer file was not created"
}

$fileSize = (Get-Item $bootstrapperPath).Length
Write-Success "Downloaded agent ($([math]::Round($fileSize/1MB, 2)) MB)"

# Run installer
Write-Step "Installing Sentinel Agent..."

$installArgs = @("--server=$Script:Server", "--token=$Script:Token")
if ($Silent) { $installArgs += "--silent" }
if ($Force) { $installArgs += "--force" }

$process = Start-Process -FilePath $bootstrapperPath -ArgumentList $installArgs -Wait -PassThru -NoNewWindow

# Cleanup
Remove-Item $bootstrapperPath -Force -ErrorAction SilentlyContinue

if ($process.ExitCode -ne 0) {
    throw "Installer failed with exit code $($process.ExitCode)"
}

Write-Success "Agent installed successfully"

# Verify services
Start-Sleep -Seconds 2
$agentService = Get-Service -Name "SentinelAgent" -ErrorAction SilentlyContinue
$watchdogService = Get-Service -Name "SentinelWatchdog" -ErrorAction SilentlyContinue

if ($agentService -and $agentService.Status -eq "Running") {
    Write-Success "SentinelAgent service is running"
}
if ($watchdogService -and $watchdogService.Status -eq "Running") {
    Write-Success "SentinelWatchdog service is running"
}

# Completion message
Write-Host ""
Write-Host "  ================================================================" -ForegroundColor Green
Write-Host "  =          INSTALLATION COMPLETED SUCCESSFULLY                 =" -ForegroundColor Green
Write-Host "  ================================================================" -ForegroundColor Green
Write-Host ""
Write-Host "  The Sentinel Agent is now running and will start automatically" -ForegroundColor White
Write-Host "  when Windows boots." -ForegroundColor White
Write-Host ""

if (-not $Silent) {
    Write-Host "Press any key to close..." -ForegroundColor Cyan
    $null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown") 2>$null
}

exit 0
`
	return script
}

// generateLinuxInstaller creates a bash script with sudo handling and embedded config
func generateLinuxInstaller(serverURL, token string) string {
	return fmt.Sprintf(`#!/bin/bash
# Sentinel Agent Installer - Pre-configured for direct download
# This script will automatically request root privileges if needed

set -e

# Configuration (pre-embedded from server)
SERVER="%s"
TOKEN="%s"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${YELLOW}Requesting root privileges...${NC}"
    exec sudo "$0" "$@"
    exit $?
fi

echo ""
echo -e "${CYAN}  ____             _   _            _ ${NC}"
echo -e "${CYAN} / ___|  ___ _ __ | |_(_)_ __   ___| |${NC}"
echo -e "${CYAN} \\___ \\ / _ \\ '_ \\| __| | '_ \\ / _ \\ |${NC}"
echo -e "${CYAN}  ___) |  __/ | | | |_| | | | |  __/ |${NC}"
echo -e "${CYAN} |____/ \\___|_| |_|\\__|_|_| |_|\\___|_|${NC}"
echo ""
echo -e "${CYAN}       Remote Monitoring & Management${NC}"
echo ""

echo -e "${GREEN}[+]${NC} Running with root privileges"
echo -e "      Server: $SERVER"

# Detect architecture
ARCH=$(uname -m)
case $ARCH in
    x86_64) ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    armv7l) ARCH="arm" ;;
    *) echo -e "${RED}[!] Unsupported architecture: $ARCH${NC}"; exit 1 ;;
esac

# Create installation directory
INSTALL_DIR="/opt/sentinel"
mkdir -p "$INSTALL_DIR"

# Download agent
echo -e "${YELLOW}[*]${NC} Downloading Sentinel Agent..."
DOWNLOAD_URL="$SERVER/api/bootstrap/download?platform=linux&arch=$ARCH&token=$TOKEN"

if command -v curl &> /dev/null; then
    curl -sSL "$DOWNLOAD_URL" -o "$INSTALL_DIR/sentinel-agent"
elif command -v wget &> /dev/null; then
    wget -q "$DOWNLOAD_URL" -O "$INSTALL_DIR/sentinel-agent"
else
    echo -e "${RED}[!] Neither curl nor wget found. Please install one.${NC}"
    exit 1
fi

chmod +x "$INSTALL_DIR/sentinel-agent"
echo -e "${GREEN}[+]${NC} Downloaded agent"

# Create systemd service
echo -e "${YELLOW}[*]${NC} Creating systemd service..."
cat > /etc/systemd/system/sentinel-agent.service << EOF
[Unit]
Description=Sentinel RMM Agent
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/sentinel-agent --server=$SERVER
Restart=always
RestartSec=10
User=root

[Install]
WantedBy=multi-user.target
EOF

# Create log directory
mkdir -p /var/log/sentinel

# Enable and start service
systemctl daemon-reload
systemctl enable sentinel-agent
systemctl start sentinel-agent

echo -e "${GREEN}[+]${NC} Sentinel Agent service started"

# Verify
sleep 2
if systemctl is-active --quiet sentinel-agent; then
    echo ""
    echo -e "${GREEN}  ================================================================${NC}"
    echo -e "${GREEN}  =          INSTALLATION COMPLETED SUCCESSFULLY                 =${NC}"
    echo -e "${GREEN}  ================================================================${NC}"
    echo ""
    echo "  The Sentinel Agent is now running and will start automatically"
    echo "  when the system boots."
    echo ""
    exit 0
else
    echo -e "${RED}[!] Service failed to start. Check logs: journalctl -u sentinel-agent${NC}"
    exit 1
fi
`, serverURL, token)
}

// generateSynologyInstaller creates a bash script optimized for Synology NAS devices
func generateSynologyInstaller(serverURL, token string) string {
	return fmt.Sprintf(`#!/bin/bash
# Sentinel Agent Installer for Synology NAS
# This script installs the Sentinel RMM agent on Synology DSM
# Supports DSM 6.x and 7.x on ARM and Intel platforms

set -e

# Configuration (pre-embedded from server)
SERVER="%s"
TOKEN="%s"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

# Check if running on Synology
if [ ! -f /etc/synoinfo.conf ]; then
    echo -e "${RED}[!] This script is designed for Synology NAS devices.${NC}"
    echo -e "${YELLOW}    For standard Linux, please use the Linux installer.${NC}"
    exit 1
fi

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${YELLOW}Requesting root privileges...${NC}"
    exec sudo "$0" "$@"
    exit $?
fi

echo ""
echo -e "${CYAN}  ____             _   _            _ ${NC}"
echo -e "${CYAN} / ___|  ___ _ __ | |_(_)_ __   ___| |${NC}"
echo -e "${CYAN} \\___ \\ / _ \\ '_ \\| __| | '_ \\ / _ \\ |${NC}"
echo -e "${CYAN}  ___) |  __/ | | | |_| | | | |  __/ |${NC}"
echo -e "${CYAN} |____/ \\___|_| |_|\\__|_|_| |_|\\___|_|${NC}"
echo ""
echo -e "${CYAN}       Synology NAS Agent Installer${NC}"
echo ""

# Get Synology info
DSM_VERSION=$(cat /etc.defaults/VERSION 2>/dev/null | grep productversion | cut -d'"' -f2 || echo "Unknown")
MODEL=$(cat /etc/synoinfo.conf 2>/dev/null | grep upnpmodelname | cut -d'"' -f2 || echo "Unknown")
echo -e "${GREEN}[+]${NC} Detected Synology: $MODEL running DSM $DSM_VERSION"
echo -e "      Server: $SERVER"

# Detect architecture
ARCH=$(uname -m)
case $ARCH in
    x86_64)
        ARCH="amd64"
        ;;
    aarch64)
        ARCH="arm64"
        ;;
    armv7l|armv7)
        ARCH="arm"
        ;;
    *)
        echo -e "${RED}[!] Unsupported architecture: $ARCH${NC}"
        echo -e "${YELLOW}    Supported: x86_64, aarch64, armv7${NC}"
        exit 1
        ;;
esac
echo -e "${GREEN}[+]${NC} Architecture: $ARCH"

# Installation paths for Synology
# Use /volume1/@appstore for package-like installation
# Fallback to /opt/sentinel if volume1 doesn't exist
if [ -d "/volume1" ]; then
    INSTALL_DIR="/volume1/@appstore/SentinelAgent"
else
    INSTALL_DIR="/opt/sentinel"
fi

CONFIG_DIR="/var/packages/SentinelAgent/etc"
LOG_DIR="/var/log/sentinel"

echo -e "${YELLOW}[*]${NC} Installing to: $INSTALL_DIR"

# Create directories
mkdir -p "$INSTALL_DIR"
mkdir -p "$CONFIG_DIR"
mkdir -p "$LOG_DIR"

# Stop existing service if running
if [ -f "/usr/local/etc/rc.d/sentinel-agent.sh" ]; then
    echo -e "${YELLOW}[*]${NC} Stopping existing agent..."
    /usr/local/etc/rc.d/sentinel-agent.sh stop 2>/dev/null || true
fi

# Download agent binary
echo -e "${YELLOW}[*]${NC} Downloading Sentinel Agent..."
DOWNLOAD_URL="$SERVER/api/bootstrap/agent?platform=linux&arch=$ARCH&token=$TOKEN"

if command -v curl &> /dev/null; then
    HTTP_CODE=$(curl -sSL -w "%%{http_code}" "$DOWNLOAD_URL" -o "$INSTALL_DIR/sentinel-agent")
    if [ "$HTTP_CODE" != "200" ]; then
        echo -e "${RED}[!] Download failed with HTTP $HTTP_CODE${NC}"
        rm -f "$INSTALL_DIR/sentinel-agent"
        exit 1
    fi
elif command -v wget &> /dev/null; then
    wget -q "$DOWNLOAD_URL" -O "$INSTALL_DIR/sentinel-agent"
else
    echo -e "${RED}[!] Neither curl nor wget found.${NC}"
    exit 1
fi

chmod +x "$INSTALL_DIR/sentinel-agent"
echo -e "${GREEN}[+]${NC} Downloaded agent"

# Create configuration file
echo -e "${YELLOW}[*]${NC} Creating configuration..."
cat > "$CONFIG_DIR/config.json" << EOF
{
    "server_url": "$SERVER",
    "enrollment_token": "$TOKEN",
    "log_path": "$LOG_DIR",
    "device_type": "nas"
}
EOF
chmod 600 "$CONFIG_DIR/config.json"

# Create startup script for Synology (rc.d style)
echo -e "${YELLOW}[*]${NC} Creating startup script..."
cat > /usr/local/etc/rc.d/sentinel-agent.sh << 'RCEOF'
#!/bin/sh
# Sentinel Agent startup script for Synology DSM

DAEMON="$INSTALL_DIR/sentinel-agent"
PIDFILE="/var/run/sentinel-agent.pid"
LOGFILE="$LOG_DIR/agent.log"

start() {
    if [ -f "$PIDFILE" ] && kill -0 $(cat "$PIDFILE") 2>/dev/null; then
        echo "Sentinel Agent is already running"
        return 1
    fi
    echo "Starting Sentinel Agent..."
    nohup "$DAEMON" --server="$SERVER" >> "$LOGFILE" 2>&1 &
    echo $! > "$PIDFILE"
    sleep 2
    if kill -0 $(cat "$PIDFILE") 2>/dev/null; then
        echo "Sentinel Agent started (PID: $(cat $PIDFILE))"
    else
        echo "Failed to start Sentinel Agent"
        rm -f "$PIDFILE"
        return 1
    fi
}

stop() {
    if [ ! -f "$PIDFILE" ]; then
        echo "Sentinel Agent is not running"
        return 1
    fi
    echo "Stopping Sentinel Agent..."
    kill $(cat "$PIDFILE") 2>/dev/null
    rm -f "$PIDFILE"
    echo "Sentinel Agent stopped"
}

status() {
    if [ -f "$PIDFILE" ] && kill -0 $(cat "$PIDFILE") 2>/dev/null; then
        echo "Sentinel Agent is running (PID: $(cat $PIDFILE))"
        return 0
    else
        echo "Sentinel Agent is not running"
        return 1
    fi
}

case "$1" in
    start)
        start
        ;;
    stop)
        stop
        ;;
    restart)
        stop
        sleep 2
        start
        ;;
    status)
        status
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|status}"
        exit 1
        ;;
esac
RCEOF

# Replace variables in the rc script
sed -i "s|\$INSTALL_DIR|$INSTALL_DIR|g" /usr/local/etc/rc.d/sentinel-agent.sh
sed -i "s|\$LOG_DIR|$LOG_DIR|g" /usr/local/etc/rc.d/sentinel-agent.sh
sed -i "s|\$SERVER|$SERVER|g" /usr/local/etc/rc.d/sentinel-agent.sh

chmod +x /usr/local/etc/rc.d/sentinel-agent.sh

# Start the agent
echo -e "${YELLOW}[*]${NC} Starting Sentinel Agent..."
/usr/local/etc/rc.d/sentinel-agent.sh start

# Verify
sleep 3
if /usr/local/etc/rc.d/sentinel-agent.sh status > /dev/null 2>&1; then
    echo ""
    echo -e "${GREEN}  ================================================================${NC}"
    echo -e "${GREEN}  =     SYNOLOGY INSTALLATION COMPLETED SUCCESSFULLY            =${NC}"
    echo -e "${GREEN}  ================================================================${NC}"
    echo ""
    echo "  The Sentinel Agent is now running on your Synology NAS."
    echo ""
    echo "  Management commands:"
    echo "    Start:   /usr/local/etc/rc.d/sentinel-agent.sh start"
    echo "    Stop:    /usr/local/etc/rc.d/sentinel-agent.sh stop"
    echo "    Status:  /usr/local/etc/rc.d/sentinel-agent.sh status"
    echo ""
    echo "  Logs: $LOG_DIR/agent.log"
    echo ""
    echo -e "${YELLOW}  Note: The agent will automatically start on NAS reboot.${NC}"
    echo ""
    exit 0
else
    echo -e "${RED}[!] Agent failed to start. Check logs: $LOG_DIR/agent.log${NC}"
    exit 1
fi
`, serverURL, token)
}

// downloadBootstrapOpenH264Handler serves the OpenH264 DLL for video encoding
func downloadBootstrapOpenH264Handler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		platform := c.Query("platform")
		arch := c.Query("arch")

		if platform == "" {
			platform = "windows"
		}
		if arch == "" {
			arch = "amd64"
		}

		// Normalize
		platform = strings.ToLower(platform)
		arch = strings.ToLower(arch)

		// Only Windows x64 is supported
		if platform != "windows" || arch != "amd64" {
			c.JSON(http.StatusNotFound, gin.H{"error": "OpenH264 only available for Windows amd64"})
			return
		}

		// Get OpenH264 DLL path
		dllPath := getOpenH264Path(services)
		if dllPath == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "OpenH264 DLL not found"})
			return
		}

		// Read DLL
		dllData, err := os.ReadFile(dllPath)
		if err != nil {
			log.Printf("Error reading OpenH264 DLL: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read OpenH264 DLL"})
			return
		}

		c.Header("Content-Disposition", "attachment; filename=openh264-2.4.1-win64.dll")
		c.Header("Content-Type", "application/octet-stream")
		c.Data(http.StatusOK, "application/octet-stream", dllData)
	}
}

func getOpenH264Path(services *Services) string {
	paths := []string{
		filepath.Join("release", "agent", "openh264-2.4.1-win64.dll"),
		filepath.Join("agent", "openh264-2.4.1-win64.dll"),
		filepath.Join("installers", "openh264-2.4.1-win64.dll"),
		"openh264-2.4.1-win64.dll",
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}
