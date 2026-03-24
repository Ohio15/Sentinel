package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/constants"
)

// generateKillToken creates a 32-byte random token and returns (plaintext hex, sha256 hash).
func generateKillToken() (string, string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	plaintext := hex.EncodeToString(tokenBytes)
	hash := sha256.Sum256([]byte(plaintext))
	hashHex := hex.EncodeToString(hash[:])
	return plaintext, hashHex, nil
}

// generateKillTokenForDevice generates a new kill token for a device,
// stores the hash in the database, and returns the plaintext token.
func (r *Router) generateKillTokenForDevice(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()

	// Verify device exists
	var agentID string
	err = r.db.Pool().QueryRow(ctx,
		"SELECT agent_id FROM devices WHERE id = $1 AND organization_id = $2",
		id, constants.CurrentOrganizationID,
	).Scan(&agentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	// Generate the token
	plaintext, hashHex, err := generateKillToken()
	if err != nil {
		log.Printf("[KillToken] Failed to generate token for device %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate kill token"})
		return
	}

	// Store the hash
	_, err = r.db.Pool().Exec(ctx,
		"UPDATE devices SET kill_token_hash = $1, updated_at = NOW() WHERE id = $2 AND organization_id = $3",
		hashHex, id, constants.CurrentOrganizationID,
	)
	if err != nil {
		log.Printf("[KillToken] Failed to store token hash for device %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store kill token"})
		return
	}

	log.Printf("[KillToken] Generated kill token for device %s (agent %s)", id, agentID)

	c.JSON(http.StatusOK, gin.H{
		"killToken": plaintext,
		"deviceId":  id,
		"agentId":   agentID,
		"message":   "Kill token generated. Store this securely — it cannot be retrieved again.",
	})
}

// getEmergencyUninstallScript generates a PowerShell script with the kill token
// embedded that can be used for offline emergency uninstall.
func (r *Router) getEmergencyUninstallScript(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()

	// Verify device exists and has a kill token
	var agentID string
	var killTokenHash *string
	err = r.db.Pool().QueryRow(ctx,
		"SELECT agent_id, kill_token_hash FROM devices WHERE id = $1 AND organization_id = $2",
		id, constants.CurrentOrganizationID,
	).Scan(&agentID, &killTokenHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	if killTokenHash == nil || *killTokenHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "No kill token exists for this device",
			"message": "Generate a kill token first using POST /api/devices/:id/generate-kill-token",
		})
		return
	}

	// Generate a fresh token so the script has the plaintext
	plaintext, hashHex, err := generateKillToken()
	if err != nil {
		log.Printf("[KillToken] Failed to generate token for emergency script (device %s): %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate kill token"})
		return
	}

	// Update the stored hash with the new one
	_, err = r.db.Pool().Exec(ctx,
		"UPDATE devices SET kill_token_hash = $1, updated_at = NOW() WHERE id = $2 AND organization_id = $3",
		hashHex, id, constants.CurrentOrganizationID,
	)
	if err != nil {
		log.Printf("[KillToken] Failed to update token hash for emergency script (device %s): %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update kill token"})
		return
	}

	log.Printf("[KillToken] Generated emergency uninstall script for device %s (agent %s)", id, agentID)

	script := generateEmergencyUninstallPS1(plaintext, agentID, id.String())

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=emergency-uninstall-%s.ps1", agentID))
	c.String(http.StatusOK, script)
}

// generateEmergencyUninstallPS1 builds the PowerShell script content.
func generateEmergencyUninstallPS1(killToken, agentID, deviceID string) string {
	var sb strings.Builder
	sb.WriteString(`# Sentinel Emergency Uninstall Script
# Generated for Agent: ` + agentID + `
# Device ID: ` + deviceID + `
#
# This script performs an offline emergency uninstall of the Sentinel agent.
# It must be run as Administrator on the target machine.
#
# WARNING: This script will permanently remove the Sentinel agent and all its data.

#Requires -RunAsAdministrator

$ErrorActionPreference = "Continue"
$KillToken = "` + killToken + `"
$InstallDir = Join-Path $env:ProgramFiles "Sentinel Agent"
$DataDir = Join-Path $env:ProgramData "Sentinel"
$AgentExe = Join-Path $InstallDir "sentinel-agent.exe"
$ServiceNames = @("SentinelAgent", "SentinelWatchdog")

Write-Host "========================================" -ForegroundColor Yellow
Write-Host "  Sentinel Emergency Uninstall Script" -ForegroundColor Yellow
Write-Host "========================================" -ForegroundColor Yellow
Write-Host ""
Write-Host "Agent ID: ` + agentID + `"
Write-Host "Device ID: ` + deviceID + `"
Write-Host ""

# Step 1: Reset DACLs on install directory so we can modify files
Write-Host "[1/7] Resetting file permissions (DACLs)..." -ForegroundColor Cyan
if (Test-Path $InstallDir) {
    try {
        $acl = New-Object System.Security.AccessControl.DirectorySecurity
        $adminRule = New-Object System.Security.AccessControl.FileSystemAccessRule(
            "BUILTIN\Administrators", "FullControl", "ContainerInherit,ObjectInherit", "None", "Allow"
        )
        $systemRule = New-Object System.Security.AccessControl.FileSystemAccessRule(
            "NT AUTHORITY\SYSTEM", "FullControl", "ContainerInherit,ObjectInherit", "None", "Allow"
        )
        $acl.SetAccessRuleProtection($true, $false)
        $acl.AddAccessRule($adminRule)
        $acl.AddAccessRule($systemRule)

        # Take ownership first
        $adminGroup = New-Object System.Security.Principal.NTAccount("BUILTIN", "Administrators")
        $acl.SetOwner($adminGroup)

        Set-Acl -Path $InstallDir -AclObject $acl
        Get-ChildItem -Path $InstallDir -Recurse -Force | ForEach-Object {
            try { Set-Acl -Path $_.FullName -AclObject $acl } catch { }
        }
        Write-Host "  DACLs reset successfully." -ForegroundColor Green
    } catch {
        Write-Host "  Warning: Failed to reset DACLs: $_" -ForegroundColor Yellow
        # Try icacls as fallback
        icacls $InstallDir /reset /T /Q 2>$null
    }
} else {
    Write-Host "  Install directory not found, skipping." -ForegroundColor Yellow
}

# Step 2: Stop the watchdog first (it would restart the agent)
Write-Host "[2/7] Stopping Sentinel Watchdog..." -ForegroundColor Cyan
$watchdogSvc = Get-Service -Name "SentinelWatchdog" -ErrorAction SilentlyContinue
if ($watchdogSvc) {
    Stop-Service -Name "SentinelWatchdog" -Force -ErrorAction SilentlyContinue
    # Wait for it to actually stop
    $timeout = 15
    while ($timeout -gt 0 -and (Get-Service -Name "SentinelWatchdog" -ErrorAction SilentlyContinue).Status -eq "Running") {
        Start-Sleep -Seconds 1
        $timeout--
    }
    if ($timeout -le 0) {
        Write-Host "  Watchdog did not stop gracefully, killing process..." -ForegroundColor Yellow
        Get-Process -Name "sentinel-watchdog" -ErrorAction SilentlyContinue | Stop-Process -Force
    }
    Write-Host "  Watchdog stopped." -ForegroundColor Green
} else {
    Write-Host "  Watchdog service not found, skipping." -ForegroundColor Yellow
}

# Step 3: Stop the agent service
Write-Host "[3/7] Stopping Sentinel Agent..." -ForegroundColor Cyan
$agentSvc = Get-Service -Name "SentinelAgent" -ErrorAction SilentlyContinue
if ($agentSvc) {
    Stop-Service -Name "SentinelAgent" -Force -ErrorAction SilentlyContinue
    $timeout = 15
    while ($timeout -gt 0 -and (Get-Service -Name "SentinelAgent" -ErrorAction SilentlyContinue).Status -eq "Running") {
        Start-Sleep -Seconds 1
        $timeout--
    }
    if ($timeout -le 0) {
        Write-Host "  Agent did not stop gracefully, killing process..." -ForegroundColor Yellow
        Get-Process -Name "sentinel-agent" -ErrorAction SilentlyContinue | Stop-Process -Force
    }
    Write-Host "  Agent stopped." -ForegroundColor Green
} else {
    Write-Host "  Agent service not found, skipping." -ForegroundColor Yellow
}

# Step 4: Attempt graceful uninstall with kill token
Write-Host "[4/7] Attempting graceful uninstall with kill token..." -ForegroundColor Cyan
if (Test-Path $AgentExe) {
    try {
        $proc = Start-Process -FilePath $AgentExe -ArgumentList "--force-uninstall", "--kill-token=$KillToken" -Wait -PassThru -NoNewWindow -ErrorAction Stop
        if ($proc.ExitCode -eq 0) {
            Write-Host "  Graceful uninstall succeeded." -ForegroundColor Green
        } else {
            Write-Host "  Graceful uninstall returned exit code $($proc.ExitCode), continuing with manual cleanup..." -ForegroundColor Yellow
        }
    } catch {
        Write-Host "  Graceful uninstall failed: $_. Continuing with manual cleanup..." -ForegroundColor Yellow
    }
} else {
    Write-Host "  Agent executable not found, proceeding with manual cleanup..." -ForegroundColor Yellow
}

# Step 5: Delete Windows services
Write-Host "[5/7] Removing Windows services..." -ForegroundColor Cyan
foreach ($svcName in $ServiceNames) {
    $svc = Get-Service -Name $svcName -ErrorAction SilentlyContinue
    if ($svc) {
        sc.exe delete $svcName 2>$null
        Write-Host "  Deleted service: $svcName" -ForegroundColor Green
    } else {
        Write-Host "  Service $svcName not found, skipping." -ForegroundColor Yellow
    }
}

# Step 6: Clean up registry
Write-Host "[6/7] Cleaning up registry..." -ForegroundColor Cyan
$regPaths = @(
    "HKLM:\SOFTWARE\Sentinel",
    "HKLM:\SYSTEM\CurrentControlSet\Services\SentinelAgent",
    "HKLM:\SYSTEM\CurrentControlSet\Services\SentinelWatchdog"
)
foreach ($regPath in $regPaths) {
    if (Test-Path $regPath) {
        Remove-Item -Path $regPath -Recurse -Force -ErrorAction SilentlyContinue
        Write-Host "  Removed: $regPath" -ForegroundColor Green
    }
}

# Step 7: Delete files
Write-Host "[7/7] Removing files..." -ForegroundColor Cyan
$pathsToRemove = @($InstallDir, $DataDir)
foreach ($path in $pathsToRemove) {
    if (Test-Path $path) {
        Remove-Item -Path $path -Recurse -Force -ErrorAction SilentlyContinue
        if (Test-Path $path) {
            Write-Host "  Warning: Could not fully remove $path (files may be locked)" -ForegroundColor Yellow
        } else {
            Write-Host "  Removed: $path" -ForegroundColor Green
        }
    } else {
        Write-Host "  Path not found: $path" -ForegroundColor Yellow
    }
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Green
Write-Host "  Emergency uninstall complete." -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host ""
Write-Host "A reboot is recommended to finalize cleanup." -ForegroundColor Yellow
`)
	return sb.String()
}

// generateKillTokenForDeviceHandler wraps generateKillTokenForDevice for the services-based router
func generateKillTokenForDeviceHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.generateKillTokenForDevice
}

// getEmergencyUninstallScriptHandler wraps getEmergencyUninstallScript for the services-based router
func getEmergencyUninstallScriptHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.getEmergencyUninstallScript
}
