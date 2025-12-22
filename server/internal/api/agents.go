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
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/models"
)

const agentVersion = "1.0.0"

// Agent installer paths (relative to server binary or absolute)
var installerPaths = map[string]string{
	"windows-x64": "installers/sentinel-agent-1.0.0-windows-x64.zip",
	"linux-x64":   "installers/sentinel-agent-1.0.0-linux-x64.tar.gz",
	"linux-arm64": "installers/sentinel-agent-1.0.0-linux-arm64.tar.gz",
	"macos-x64":   "installers/sentinel-agent-1.0.0-macos-x64.tar.gz",
	"macos-arm64": "installers/sentinel-agent-1.0.0-macos-arm64.tar.gz",
}

// listEnrollmentTokens returns all enrollment tokens
func (r *Router) listEnrollmentTokens(c *gin.Context) {
	rows, err := r.db.Pool().Query(c.Request.Context(), `
		SELECT id, token, name, description, created_by, expires_at, max_uses, use_count,
		       is_active, tags, metadata, created_at, updated_at
		FROM enrollment_tokens
		ORDER BY created_at DESC
	`)
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Generate secure random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}
	token := hex.EncodeToString(tokenBytes)

	// Get user ID from context
	userID, _ := c.Get("userID")
	uid := userID.(uuid.UUID)

	var tokenID uuid.UUID
	err := r.db.Pool().QueryRow(c.Request.Context(), `
		INSERT INTO enrollment_tokens (token, name, description, created_by, expires_at, max_uses, tags, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, token, req.Name, req.Description, uid, req.ExpiresAt, req.MaxUses, req.Tags, req.Metadata).Scan(&tokenID)

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
		FROM enrollment_tokens WHERE id = $1
	`, tokenID).Scan(
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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

	_, err = r.db.Pool().Exec(c.Request.Context(), `DELETE FROM enrollment_tokens WHERE id = $1`, tokenID)
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

	for key, path := range installerPaths {
		parts := strings.Split(key, "-")
		if len(parts) != 2 {
			continue
		}

		installer := models.AgentInstaller{
			Platform:     parts[0],
			Architecture: parts[1],
			Filename:     filepath.Base(path),
			Version:      agentVersion,
			DownloadURL:  fmt.Sprintf("/api/agents/download/%s/%s", parts[0], parts[1]),
		}

		// Try to get file size
		if info, err := os.Stat(path); err == nil {
			installer.Size = info.Size()
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
		FROM enrollment_tokens WHERE token = $1
	`, token).Scan(&tokenID, &isActive, &expiresAt, &maxUses, &useCount, &tags, &metadata)

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

	// Find installer
	key := fmt.Sprintf("%s-%s", platform, arch)
	installerPath, ok := installerPaths[key]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Installer not found for this platform"})
		return
	}

	// Check if file exists
	if _, err := os.Stat(installerPath); os.IsNotExist(err) {
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

	configContent := fmt.Sprintf(`{
  "agent_id": "%s",
  "server_url": "%s",
  "enrollment_token": "%s",
  "heartbeat_interval": 30,
  "metrics_interval": 60
}`, agentID, serverURL, token)

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
`, serverURL, token, agentID, agentID)

	installWriter, _ := zipWriter.Create("quick-install.ps1")
	installWriter.Write([]byte(installScript))

	zipWriter.Close()

	// Serve the modified zip
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=sentinel-agent-%s-windows-x64.zip", agentID[:8]))
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
# curl -o /tmp/sentinel-agent "$SERVER_URL/api/agents/binary/linux/x64?token=$ENROLLMENT_TOKEN"
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
`, agentID, serverURL, token, agentID)
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
`, agentID, serverURL, token, agentID)
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=sentinel-agent-install-%s.%s", agentID[:8], scriptExt))
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

	var script string

	switch platform {
	case "windows":
		script = fmt.Sprintf(`# Run in PowerShell as Administrator
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$ProgressPreference = 'SilentlyContinue'
Invoke-WebRequest -Uri '%s/api/agents/download/windows/x64?token=%s' -OutFile sentinel-agent.zip
Expand-Archive sentinel-agent.zip -DestinationPath sentinel-agent -Force
cd sentinel-agent
.\quick-install.ps1
`, serverURL, token)

	case "linux":
		script = fmt.Sprintf(`#!/bin/bash
curl -sSL '%s/api/agents/download/linux/x64?token=%s' | sudo bash
`, serverURL, token)

	case "macos":
		script = fmt.Sprintf(`#!/bin/bash
curl -sSL '%s/api/agents/download/macos/arm64?token=%s' | sudo bash
`, serverURL, token)

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid platform"})
		return
	}

	c.Header("Content-Type", "text/plain")
	c.String(http.StatusOK, script)
}
