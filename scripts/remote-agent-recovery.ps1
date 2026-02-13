# Remote Agent Recovery Script for Sentinel RMM
# This script creates a scheduled task to download and replace the agent binary
# It uses the "scheduled task" pattern from industry best practices to survive agent restart

param(
    [string]$ServerUrl = "https://sentinelrmm.us:8443",
    [string]$Platform = "windows",
    [string]$Arch = "amd64"
)

$ErrorActionPreference = "Stop"

# Define paths
$agentPath = "C:\Program Files\Sentinel\sentinel-agent.exe"
$watchdogPath = "C:\Program Files\Sentinel\sentinel-watchdog.exe"
$downloadUrl = "$ServerUrl/api/agent/update/download?platform=$Platform&arch=$Arch"
$tempPath = "$env:TEMP\sentinel-agent-update.exe"
$backupPath = "$agentPath.bak"
$logPath = "$env:TEMP\sentinel-recovery.log"

# Create the update script that will run via scheduled task
$updateScript = @"
`$ErrorActionPreference = 'Continue'
Start-Transcript -Path '$logPath' -Append

Write-Host "Starting Sentinel Agent Recovery at `$(Get-Date)"

try {
    # Download new agent binary
    Write-Host "Downloading agent from $downloadUrl"

    # Use TLS 1.2
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

    # Download with retry
    `$maxRetries = 3
    `$retryCount = 0
    `$downloaded = `$false

    while (-not `$downloaded -and `$retryCount -lt `$maxRetries) {
        try {
            Invoke-WebRequest -Uri '$downloadUrl' -OutFile '$tempPath' -UseBasicParsing -TimeoutSec 120
            `$downloaded = `$true
            Write-Host "Download complete"
        } catch {
            `$retryCount++
            Write-Host "Download attempt `$retryCount failed: `$_"
            Start-Sleep -Seconds 5
        }
    }

    if (-not `$downloaded) {
        throw "Failed to download agent after `$maxRetries attempts"
    }

    # Verify download
    if (-not (Test-Path '$tempPath') -or (Get-Item '$tempPath').Length -lt 1000000) {
        throw "Downloaded file is missing or too small"
    }

    Write-Host "Stopping SentinelWatchdog service..."
    Stop-Service -Name "SentinelWatchdog" -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2

    Write-Host "Stopping SentinelAgent service..."
    Stop-Service -Name "SentinelAgent" -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2

    # Kill any remaining processes
    Get-Process -Name "sentinel-agent" -ErrorAction SilentlyContinue | Stop-Process -Force
    Get-Process -Name "sentinel-watchdog" -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 1

    # Backup current binary
    if (Test-Path '$agentPath') {
        Write-Host "Backing up current agent..."
        Copy-Item -Path '$agentPath' -Destination '$backupPath' -Force
    }

    # Replace agent binary
    Write-Host "Installing new agent binary..."
    Copy-Item -Path '$tempPath' -Destination '$agentPath' -Force

    # Clean up temp file
    Remove-Item -Path '$tempPath' -Force -ErrorAction SilentlyContinue

    Write-Host "Starting SentinelWatchdog service..."
    Start-Service -Name "SentinelWatchdog" -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2

    Write-Host "Starting SentinelAgent service..."
    Start-Service -Name "SentinelAgent" -ErrorAction SilentlyContinue

    Write-Host "Recovery complete at `$(Get-Date)"

} catch {
    Write-Host "ERROR: `$_"

    # Attempt rollback if we have a backup
    if (Test-Path '$backupPath') {
        Write-Host "Attempting rollback..."
        Copy-Item -Path '$backupPath' -Destination '$agentPath' -Force -ErrorAction SilentlyContinue
        Start-Service -Name "SentinelWatchdog" -ErrorAction SilentlyContinue
        Start-Service -Name "SentinelAgent" -ErrorAction SilentlyContinue
    }
}

Stop-Transcript

# Clean up the scheduled task
schtasks /delete /tn "SentinelAgentRecovery" /f 2>`$null
"@

# Save the update script
$scriptPath = "$env:TEMP\sentinel-recovery-task.ps1"
$updateScript | Out-File -FilePath $scriptPath -Encoding UTF8 -Force

# Create a scheduled task to run in 10 seconds
$taskName = "SentinelAgentRecovery"
$runTime = (Get-Date).AddSeconds(10).ToString("HH:mm")

# Delete existing task if present
schtasks /delete /tn $taskName /f 2>$null

# Create the scheduled task
$action = "powershell.exe -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$scriptPath`""
schtasks /create /tn $taskName /tr $action /sc once /st $runTime /ru SYSTEM /f /rl HIGHEST

Write-Output "Recovery scheduled. Task '$taskName' will run at $runTime"
Write-Output "Log will be written to: $logPath"
Write-Output "Agent will be updated from: $downloadUrl"
