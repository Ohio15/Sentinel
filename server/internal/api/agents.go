package api

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"log"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"github.com/sentinel/server/internal/constants"
	"github.com/sentinel/server/internal/models"
)

const agentVersion = "1.0.0"

// installerArtifactName returns the legacy packaged-agent filename for a given
// platform/arch key. Resolution to a real filesystem path is deferred to the
// canonical resolver in installer_paths.go so /app/installers and other
// production paths are picked up uniformly.
func installerArtifactName(key string) string {
	switch key {
	case "windows-x64":
		return fmt.Sprintf("sentinel-agent-%s-windows-x64.zip", agentVersion)
	case "linux-x64":
		return fmt.Sprintf("sentinel-agent-%s-linux-x64.tar.gz", agentVersion)
	case "linux-arm64":
		return fmt.Sprintf("sentinel-agent-%s-linux-arm64.tar.gz", agentVersion)
	case "macos-x64":
		return fmt.Sprintf("sentinel-agent-%s-macos-x64.tar.gz", agentVersion)
	case "macos-arm64":
		return fmt.Sprintf("sentinel-agent-%s-macos-arm64.tar.gz", agentVersion)
	default:
		return ""
	}
}

// listEnrollmentTokens returns all enrollment tokens
func (r *Router) listEnrollmentTokens(c *gin.Context) {
	rows, err := r.db.Pool().Query(c.Request.Context(), `
		SELECT id, token, name, description, created_by, expires_at, max_uses, use_count,
		       is_active, tags, metadata, created_at, updated_at
		FROM enrollment_tokens
		WHERE organization_id = $1
		ORDER BY created_at DESC
	`, constants.CurrentOrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tokens"})
		return
	}
	defer rows.Close()

	tokens := []models.EnrollmentToken{}
	for rows.Next() {
		var t models.EnrollmentToken
		err := rows.Scan(
			&t.ID, &t.Token, &t.Name, &t.Description, &t.CreatedBy, &t.ExpiresAt,
			&t.MaxUses, &t.UseCount, &t.IsActive, &t.Tags, &t.Metadata,
			&t.CreatedAt, &t.UpdatedAt,
		)
		if err != nil {
			log.Printf("Error scanning enrollment token row: %v", err)
			continue
		}
		// Mask the token for display (show only first 8 chars)
		if len(t.Token) > 8 {
			t.Token = t.Token[:8] + "..."
		}
		tokens = append(tokens, t)
	}

	c.JSON(http.StatusOK, tokens)
}

// createEnrollmentToken creates a new enrollment token
func (r *Router) createEnrollmentToken(c *gin.Context) {
	var req struct {
		Name        string            `json:"name" binding:"required"`
		Description string            `json:"description"`
		ExpiresAt   *time.Time        `json:"expiresAt"`
		MaxUses     *int              `json:"maxUses"`
		Tags        []string          `json:"tags"`
		Metadata    map[string]string `json:"metadata"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// Generate secure random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}
	token := hex.EncodeToString(tokenBytes)

	// CW-003: Hash the token with bcrypt before storage
	tokenHash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash token"})
		return
	}

	// Get user ID from context
	userID, _ := c.Get("userID")
	uid := userID.(uuid.UUID)

	var tokenID uuid.UUID
	err = r.db.Pool().QueryRow(c.Request.Context(), `
		INSERT INTO enrollment_tokens (token, token_hash, name, description, created_by, expires_at, max_uses, tags, metadata, is_legacy)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, FALSE)
		RETURNING id
	`, token, string(tokenHash), req.Name, req.Description, uid, req.ExpiresAt, req.MaxUses, req.Tags, req.Metadata).Scan(&tokenID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create token"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":    tokenID,
		"token": token, // Return full token only on creation
		"name":  req.Name,
	})
}

// getEnrollmentToken returns a specific enrollment token (with full token value)
func (r *Router) getEnrollmentToken(c *gin.Context) {
	id := c.Param("id")
	tokenID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid token ID"})
		return
	}

	var t models.EnrollmentToken
	err = r.db.Pool().QueryRow(c.Request.Context(), `
		SELECT id, token, name, description, created_by, expires_at, max_uses, use_count,
		       is_active, tags, metadata, created_at, updated_at
		FROM enrollment_tokens WHERE id = $1 AND organization_id = $2
		`, tokenID, constants.CurrentOrganizationID).Scan(
		&t.ID, &t.Token, &t.Name, &t.Description, &t.CreatedBy, &t.ExpiresAt,
		&t.MaxUses, &t.UseCount, &t.IsActive, &t.Tags, &t.Metadata,
		&t.CreatedAt, &t.UpdatedAt,
	)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Token not found"})
		return
	}

	c.JSON(http.StatusOK, t)
}

// updateEnrollmentToken updates an enrollment token
func (r *Router) updateEnrollmentToken(c *gin.Context) {
	id := c.Param("id")
	tokenID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid token ID"})
		return
	}

	var req struct {
		Name        *string           `json:"name"`
		Description *string           `json:"description"`
		IsActive    *bool             `json:"isActive"`
		ExpiresAt   *time.Time        `json:"expiresAt"`
		MaxUses     *int              `json:"maxUses"`
		Tags        []string          `json:"tags"`
		Metadata    map[string]string `json:"metadata"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	_, err = r.db.Pool().Exec(c.Request.Context(), `
		UPDATE enrollment_tokens SET
			name = COALESCE($1, name),
			description = COALESCE($2, description),
			is_active = COALESCE($3, is_active),
			expires_at = COALESCE($4, expires_at),
			max_uses = COALESCE($5, max_uses),
			tags = COALESCE($6, tags),
			metadata = COALESCE($7, metadata)
		WHERE id = $8
	`, req.Name, req.Description, req.IsActive, req.ExpiresAt, req.MaxUses, req.Tags, req.Metadata, tokenID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Token updated"})
}

// deleteEnrollmentToken deletes an enrollment token
func (r *Router) deleteEnrollmentToken(c *gin.Context) {
	id := c.Param("id")
	tokenID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid token ID"})
		return
	}

	_, err = r.db.Pool().Exec(c.Request.Context(), `DELETE FROM enrollment_tokens WHERE id = $1 AND organization_id = $2`, tokenID, constants.CurrentOrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Token deleted"})
}

// regenerateEnrollmentToken generates a new token value for an existing token
func (r *Router) regenerateEnrollmentToken(c *gin.Context) {
	id := c.Param("id")
	tokenID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid token ID"})
		return
	}

	// Generate new secure random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}
	newToken := hex.EncodeToString(tokenBytes)

	_, err = r.db.Pool().Exec(c.Request.Context(), `
		UPDATE enrollment_tokens SET token = $1, use_count = 0 WHERE id = $2
	`, newToken, tokenID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to regenerate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":    tokenID,
		"token": newToken,
	})
}

// listAgentInstallers returns available agent installers
func (r *Router) listAgentInstallers(c *gin.Context) {
	installers := []models.AgentInstaller{}

	for _, key := range []string{"windows-x64", "linux-x64", "linux-arm64", "macos-x64", "macos-arm64"} {
		filename := installerArtifactName(key)
		if filename == "" {
			continue
		}
		parts := strings.SplitN(key, "-", 2)
		if len(parts) != 2 {
			continue
		}

		installer := models.AgentInstaller{
			Platform:     parts[0],
			Architecture: parts[1],
			Filename:     filename,
			Version:      agentVersion,
			DownloadURL:  fmt.Sprintf("/api/agents/download/%s/%s", parts[0], parts[1]),
		}

		// Resolve via canonical search roots so /app/installers is included.
		if resolved := findArtifact(filename); resolved != "" {
			if info, err := os.Stat(resolved); err == nil {
				installer.Size = info.Size()
			}
		}

		installers = append(installers, installer)
	}

	c.JSON(http.StatusOK, installers)
}

// downloadAgentInstaller handles agent installer downloads with embedded config
func (r *Router) downloadAgentInstaller(c *gin.Context) {
	platform := c.Param("platform")
	arch := c.Param("arch")
	token := c.Query("token")

	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Enrollment token required"})
		return
	}

	// Validate token
	var tokenID uuid.UUID
	var isActive bool
	var expiresAt *time.Time
	var maxUses *int
	var useCount int
	var tags []string
	var metadata map[string]string

	err := r.db.Pool().QueryRow(c.Request.Context(), `
		SELECT id, is_active, expires_at, max_uses, use_count, tags, metadata
		FROM enrollment_tokens WHERE token = $1 AND organization_id = $2
		`, token, constants.CurrentOrganizationID).Scan(&tokenID, &isActive, &expiresAt, &maxUses, &useCount, &tags, &metadata)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid enrollment token"})
		return
	}

	if !isActive {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Enrollment token is disabled"})
		return
	}

	if expiresAt != nil && time.Now().After(*expiresAt) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Enrollment token has expired"})
		return
	}

	if maxUses != nil && useCount >= *maxUses {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Enrollment token has reached maximum uses"})
		return
	}

	// Find installer via canonical resolver so /app/installers and other
	// production-deployment roots are searched uniformly.
	key := fmt.Sprintf("%s-%s", platform, arch)
	filename := installerArtifactName(key)
	if filename == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Installer not found for this platform"})
		return
	}
	installerPath := findArtifact(filename)
	if installerPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Installer file not found"})
		return
	}

	// Log download
	if _, err := r.db.Pool().Exec(c.Request.Context(), `
		INSERT INTO agent_downloads (token_id, platform, architecture, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5)
	`, tokenID, platform, arch, c.ClientIP(), c.Request.UserAgent()); err != nil {
		log.Printf("Error logging agent download: %v", err)
	}

	// Increment use count
	if _, err := r.db.Pool().Exec(c.Request.Context(), `
		UPDATE enrollment_tokens SET use_count = use_count + 1 WHERE id = $1
	`, tokenID); err != nil {
		log.Printf("Error incrementing token use count: %v", err)
	}

	// Generate unique agent ID for this download
	agentID := uuid.New().String()

	// For Windows, we'll create a modified zip with config
	if platform == "windows" {
		r.serveWindowsInstaller(c, installerPath, token, agentID, tags, metadata)
		return
	}

	// For Linux/macOS, serve tarball with install script that includes config
	r.serveUnixInstaller(c, installerPath, platform, token, agentID, tags, metadata)
}

// serveWindowsInstaller creates a customized Windows installer package
func (r *Router) serveWindowsInstaller(c *gin.Context, installerPath, token, agentID string, tags []string, metadata map[string]string) {
	// Read original zip
	originalZip, err := os.ReadFile(installerPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read installer"})
		return
	}

	// Create new zip with config
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	// Read original zip contents
	reader, err := zip.NewReader(bytes.NewReader(originalZip), int64(len(originalZip)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process installer"})
		return
	}

	// Copy original files
	for _, file := range reader.File {
		fw, err := zipWriter.Create(file.Name)
		if err != nil {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			continue
		}
		io.Copy(fw, rc)
		rc.Close()
	}

	// Add config file
	serverURL := r.config.ServerURL
	if serverURL == "" {
		serverURL = fmt.Sprintf("http://%s", c.Request.Host)
	}

	// Sanitize all values before embedding in scripts/config
	safeToken, err := sanitizeForShellEmbed(token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid token: " + err.Error()})
		return
	}
	safeServerURL, err := sanitizeForShellEmbed(serverURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid server URL: " + err.Error()})
		return
	}
	safeAgentID, err := sanitizeForShellEmbed(agentID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid agent ID: " + err.Error()})
		return
	}

	configContent := fmt.Sprintf(`{
  "agent_id": "%s",
  "server_url": "%s",
  "enrollment_token": "%s",
  "heartbeat_interval": 30,
  "metrics_interval": 60
}`, safeAgentID, safeServerURL, safeToken)

	configWriter, _ := zipWriter.Create("config/agent.json")
	configWriter.Write([]byte(configContent))

	// Add a quick-install script
	installScript := fmt.Sprintf(`# Sentinel Agent Quick Install
# Run as Administrator: powershell -ExecutionPolicy Bypass -File install.ps1

$ErrorActionPreference = "Stop"

$ServerUrl = "%s"
$EnrollmentToken = "%s"
$AgentID = "%s"

# Copy binary
$InstallDir = "$env:ProgramFiles\Sentinel"
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
Copy-Item "sentinel-agent.exe" "$InstallDir\sentinel-agent.exe" -Force

# Create config
$ConfigDir = "$env:ProgramData\Sentinel"
New-Item -ItemType Directory -Path $ConfigDir -Force | Out-Null
Copy-Item "config\agent.json" "$ConfigDir\agent.json" -Force

# Create service
sc.exe create "Sentinel Agent" binPath= "$InstallDir\sentinel-agent.exe" start= auto
sc.exe description "Sentinel Agent" "Sentinel Remote Monitoring and Management Agent"
sc.exe failure "Sentinel Agent" reset= 86400 actions= restart/60000/restart/60000/restart/60000

# Start service
Start-Service -Name "Sentinel Agent"

Write-Host "Sentinel Agent installed successfully!"
Write-Host "Agent ID: %s"
`, safeServerURL, safeToken, safeAgentID, safeAgentID)

	installWriter, _ := zipWriter.Create("quick-install.ps1")
	installWriter.Write([]byte(installScript))

	zipWriter.Close()

	// Serve the modified zip
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=sentinel-agent-%s-windows-x64.zip", safeAgentID[:8]))
	c.Data(http.StatusOK, "application/zip", buf.Bytes())
}

// serveUnixInstaller creates a customized Unix installer package
func (r *Router) serveUnixInstaller(c *gin.Context, installerPath, platform, token, agentID string, tags []string, metadata map[string]string) {
	// For now, serve original file with installation instructions
	// In production, you'd create a self-extracting script with embedded config

	serverURL := r.config.ServerURL
	if serverURL == "" {
		serverURL = fmt.Sprintf("http://%s", c.Request.Host)
	}

	// Sanitize all values before embedding in shell scripts
	safeToken, err := sanitizeForShellEmbed(token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid token: " + err.Error()})
		return
	}
	safeServerURL, err := sanitizeForShellEmbed(serverURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid server URL: " + err.Error()})
		return
	}
	safeAgentID, err := sanitizeForShellEmbed(agentID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid agent ID: " + err.Error()})
		return
	}

	// Create a shell script that downloads and configures the agent
	var scriptExt string
	var installCmd string

	if platform == "linux" {
		scriptExt = "sh"
		installCmd = fmt.Sprintf(`#!/bin/bash
# Sentinel Agent Installer
# Generated for Agent ID: %s

set -e

SERVER_URL="%s"
ENROLLMENT_TOKEN="%s"
AGENT_ID="%s"

echo "Installing Sentinel Agent..."
echo "Agent ID: $AGENT_ID"

# Create directories
sudo mkdir -p /opt/sentinel
sudo mkdir -p /etc/sentinel

# Download agent (if not bundled)
# curl -H "X-Enrollment-Token: $ENROLLMENT_TOKEN" -o /tmp/sentinel-agent "$SERVER_URL/api/agents/binary/linux/x64"
# sudo mv /tmp/sentinel-agent /opt/sentinel/sentinel-agent
# sudo chmod +x /opt/sentinel/sentinel-agent

# Create config
sudo tee /etc/sentinel/agent.json > /dev/null << EOF
{
  "agent_id": "$AGENT_ID",
  "server_url": "$SERVER_URL",
  "enrollment_token": "$ENROLLMENT_TOKEN",
  "heartbeat_interval": 30,
  "metrics_interval": 60
}
EOF

sudo chmod 600 /etc/sentinel/agent.json

# Create systemd service
sudo tee /etc/systemd/system/sentinel-agent.service > /dev/null << EOF
[Unit]
Description=Sentinel RMM Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/sentinel/sentinel-agent
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable sentinel-agent
sudo systemctl start sentinel-agent

echo "Sentinel Agent installed and started!"
echo "Check status: sudo systemctl status sentinel-agent"
`, safeAgentID, safeServerURL, safeToken, safeAgentID)
	} else {
		// macOS
		scriptExt = "sh"
		installCmd = fmt.Sprintf(`#!/bin/bash
# Sentinel Agent Installer for macOS
# Generated for Agent ID: %s

set -e

SERVER_URL="%s"
ENROLLMENT_TOKEN="%s"
AGENT_ID="%s"

echo "Installing Sentinel Agent..."
echo "Agent ID: $AGENT_ID"

# Create directories
sudo mkdir -p /usr/local/bin
sudo mkdir -p "/Library/Application Support/Sentinel"

# Create config
sudo tee "/Library/Application Support/Sentinel/agent.json" > /dev/null << EOF
{
  "agent_id": "$AGENT_ID",
  "server_url": "$SERVER_URL",
  "enrollment_token": "$ENROLLMENT_TOKEN",
  "heartbeat_interval": 30,
  "metrics_interval": 60
}
EOF

sudo chmod 600 "/Library/Application Support/Sentinel/agent.json"

# Create launchd plist
sudo tee /Library/LaunchDaemons/io.sentinel.agent.plist > /dev/null << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>io.sentinel.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/sentinel-agent</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
EOF

sudo launchctl load /Library/LaunchDaemons/io.sentinel.agent.plist

echo "Sentinel Agent installed and started!"
`, safeAgentID, safeServerURL, safeToken, safeAgentID)
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=sentinel-agent-install-%s.%s", safeAgentID[:8], scriptExt))
	c.Data(http.StatusOK, "text/plain", []byte(installCmd))
}

// getAgentInstallScript returns a one-liner install script
func (r *Router) getAgentInstallScript(c *gin.Context) {
	platform := c.Param("platform")
	token := c.Query("token")

	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Enrollment token required"})
		return
	}

	serverURL := r.config.ServerURL
	if serverURL == "" {
		serverURL = fmt.Sprintf("http://%s", c.Request.Host)
	}

	// Sanitize token and serverURL before embedding in shell script
	safeToken, err := sanitizeForShellEmbed(token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid token: " + err.Error()})
		return
	}
	safeServerURL, err := sanitizeForShellEmbed(serverURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid server URL: " + err.Error()})
		return
	}

	var script string

	switch platform {
	case "windows":
		script = fmt.Sprintf(`# Run in PowerShell as Administrator
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$ProgressPreference = 'SilentlyContinue'
Invoke-WebRequest -Uri '%s/api/agents/download/windows/x64' -Headers @{'X-Enrollment-Token'='%s'} -OutFile sentinel-agent.zip
Expand-Archive sentinel-agent.zip -DestinationPath sentinel-agent -Force
cd sentinel-agent
.\quick-install.ps1
`, safeServerURL, safeToken)

	case "linux":
		script = fmt.Sprintf(`#!/bin/bash
curl -sSL -H 'X-Enrollment-Token: %s' '%s/api/agents/download/linux/x64' | sudo bash
`, safeToken, safeServerURL)

	case "macos":
		script = fmt.Sprintf(`#!/bin/bash
curl -sSL -H 'X-Enrollment-Token: %s' '%s/api/agents/download/macos/arm64' | sudo bash
`, safeToken, safeServerURL)

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid platform"})
		return
	}

	c.Header("Content-Type", "text/plain")
	c.String(http.StatusOK, script)
}
