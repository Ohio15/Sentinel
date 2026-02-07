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

// downloadBootstrapWatchdogHandler serves the watchdog binary
func downloadBootstrapWatchdogHandler(services *Services) gin.HandlerFunc {
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

// generateConfiguredInstallerHandler generates a pre-configured install script with embedded server URL and token
// This is for the "Direct Download" feature in the web dashboard - returns a ready-to-run script
func generateConfiguredInstallerHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		platform := c.Param("platform")
		if platform == "" {
			platform = "windows"
		}
		platform = strings.ToLower(platform)

		// Get or create an enrollment token for this download
		// First, try to get an existing active default token
		var token string
		var tokenID uuid.UUID
		err := services.DB.Pool().QueryRow(c.Request.Context(), `
			SELECT id, token FROM enrollment_tokens
			WHERE is_active = true
			AND (expires_at IS NULL OR expires_at > NOW())
			AND (max_uses IS NULL OR use_count < max_uses)
			AND name = 'Direct Download Token'
			LIMIT 1
		`).Scan(&tokenID, &token)

		if err != nil {
			// No existing token, create a new one
			tokenID = uuid.New()
			token = uuid.New().String()
			expiresAt := time.Now().Add(24 * time.Hour) // 24-hour validity

			_, err = services.DB.Pool().Exec(c.Request.Context(), `
				INSERT INTO enrollment_tokens (id, name, token, is_active, expires_at, created_at)
				VALUES ($1, 'Direct Download Token', $2, true, $3, NOW())
			`, tokenID, token, expiresAt)
			if err != nil {
				log.Printf("Error creating enrollment token: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate enrollment token"})
				return
			}
		}

		// Get server URL
		serverURL := services.Config.ServerURL
		if serverURL == "" {
			scheme := "https"
			if c.Request.TLS == nil && !strings.Contains(c.Request.Host, "localhost") {
				// Try to detect from X-Forwarded-Proto header (when behind proxy)
				if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
					scheme = proto
				}
			}
			serverURL = fmt.Sprintf("%s://%s", scheme, c.Request.Host)
		}

		var script string
		var filename string
		var contentType string

		switch platform {
		case "windows":
			script = generateWindowsInstaller(serverURL, token)
			filename = "sentinel-install.ps1"
			contentType = "application/octet-stream"
		case "linux", "macos":
			script = generateLinuxInstaller(serverURL, token)
			filename = "sentinel-install.sh"
			contentType = "application/x-sh"
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid platform. Supported: windows, linux, macos"})
			return
		}

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.Header("Content-Type", contentType)
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Data(http.StatusOK, contentType, []byte(script))
	}
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
