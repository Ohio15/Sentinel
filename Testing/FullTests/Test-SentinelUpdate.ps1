#Requires -Version 5.1
<#
.SYNOPSIS
    Update System Tester for Sentinel Agent
.DESCRIPTION
    Tests the Sentinel agent update mechanism including version checking,
    download verification, rollback capabilities, and update integrity.
.NOTES
    Author: Sentinel QA Team
    Version: 1.0.0
#>

[CmdletBinding()]
param(
    [Parameter()]
    [string]$OutputPath = ".\TestResults",

    [Parameter()]
    [switch]$SimulateFailure,

    [Parameter()]
    [string]$TestVersion = "99.99.99"
)

$ErrorActionPreference = "Stop"
$script:UpdateTestResults = @{
    Component = "UpdateSystem"
    Tests = @()
}

function Write-TestLog {
    param([string]$Message, [string]$Level = "Info")
    $color = switch ($Level) {
        "Info" { "Cyan" }
        "Warning" { "Yellow" }
        "Error" { "Red" }
        "Success" { "Green" }
    }
    Write-Host "[UPDATE-TEST] $Message" -ForegroundColor $color
}

function Get-CurrentAgentVersion {
    try {
        $agentPath = "C:\Program Files\Sentinel\sentinel-agent.exe"
        if (Test-Path $agentPath) {
            $versionInfo = (Get-Item $agentPath).VersionInfo
            return $versionInfo.FileVersion
        }

        # Fallback to version.json
        $versionFile = "C:\Program Files\Sentinel\version.json"
        if (Test-Path $versionFile) {
            $version = Get-Content $versionFile | ConvertFrom-Json
            return $version.version
        }

        return $null
    }
    catch {
        return $null
    }
}

function Test-VersionCheck {
    Write-TestLog "Testing version check mechanism..." -Level Info

    $result = @{
        Name = "Version Check API"
        Status = "Unknown"
        Details = @{}
    }

    try {
        $config = Get-Content "C:\ProgramData\Sentinel\config.json" -ErrorAction SilentlyContinue | ConvertFrom-Json
        $serverUrl = if ($config) { $config.server_url } else { "http://localhost:3000" }

        $response = Invoke-RestMethod -Uri "$serverUrl/api/updates/check" -Method GET -TimeoutSec 30

        $result.Details = @{
            CurrentVersion = Get-CurrentAgentVersion
            LatestVersion = $response.version
            UpdateAvailable = $response.update_available
        }

        $result.Status = "Passed"
        Write-TestLog "Version check successful - Current: $($result.Details.CurrentVersion), Latest: $($result.Details.LatestVersion)" -Level Success
    }
    catch {
        $result.Status = "Failed"
        $result.Error = $_.Exception.Message
        Write-TestLog "Version check failed: $($_.Exception.Message)" -Level Error
    }

    $script:UpdateTestResults.Tests += $result
    return $result.Status -eq "Passed"
}

function Test-DownloadIntegrity {
    Write-TestLog "Testing download integrity verification..." -Level Info

    $result = @{
        Name = "Download Integrity"
        Status = "Unknown"
        Details = @{}
    }

    try {
        $tempPath = Join-Path $env:TEMP "sentinel-test-download"
        New-Item -ItemType Directory -Path $tempPath -Force | Out-Null

        $config = Get-Content "C:\ProgramData\Sentinel\config.json" -ErrorAction SilentlyContinue | ConvertFrom-Json
        $serverUrl = if ($config) { $config.server_url } else { "http://localhost:3000" }

        # Get update manifest
        $manifest = Invoke-RestMethod -Uri "$serverUrl/api/updates/manifest" -Method GET -TimeoutSec 30

        if ($manifest.files) {
            foreach ($file in $manifest.files) {
                Write-TestLog "Downloading: $($file.name)" -Level Info

                $downloadPath = Join-Path $tempPath $file.name
                Invoke-WebRequest -Uri "$serverUrl/api/updates/download/$($file.name)" -OutFile $downloadPath -TimeoutSec 120

                # Verify checksum
                $actualHash = (Get-FileHash -Path $downloadPath -Algorithm SHA256).Hash

                if ($actualHash -eq $file.sha256) {
                    Write-TestLog "Checksum verified for $($file.name)" -Level Success
                } else {
                    throw "Checksum mismatch for $($file.name): Expected $($file.sha256), Got $actualHash"
                }
            }
        }

        $result.Status = "Passed"
        $result.Details.FilesVerified = $manifest.files.Count

        # Cleanup
        Remove-Item -Path $tempPath -Recurse -Force -ErrorAction SilentlyContinue
    }
    catch {
        $result.Status = "Failed"
        $result.Error = $_.Exception.Message
        Write-TestLog "Download integrity test failed: $($_.Exception.Message)" -Level Error
    }

    $script:UpdateTestResults.Tests += $result
    return $result.Status -eq "Passed"
}

function Test-RollbackCapability {
    Write-TestLog "Testing rollback capability..." -Level Info

    $result = @{
        Name = "Rollback Capability"
        Status = "Unknown"
        Details = @{}
    }

    try {
        $backupPath = "C:\ProgramData\Sentinel\backup"

        # Check if backup exists
        if (Test-Path $backupPath) {
            $backups = Get-ChildItem -Path $backupPath -Directory | Sort-Object LastWriteTime -Descending

            $result.Details.BackupCount = $backups.Count
            $result.Details.LatestBackup = if ($backups.Count -gt 0) { $backups[0].Name } else { $null }

            # Verify backup integrity
            if ($backups.Count -gt 0) {
                $latestBackup = $backups[0].FullName
                $requiredFiles = @("sentinel-agent.exe", "version.json")

                $missingFiles = @()
                foreach ($file in $requiredFiles) {
                    if (-not (Test-Path (Join-Path $latestBackup $file))) {
                        $missingFiles += $file
                    }
                }

                if ($missingFiles.Count -eq 0) {
                    $result.Status = "Passed"
                    Write-TestLog "Rollback backup verified with all required files" -Level Success
                } else {
                    $result.Status = "Failed"
                    $result.Error = "Missing files in backup: $($missingFiles -join ', ')"
                    Write-TestLog "Backup incomplete: $($result.Error)" -Level Error
                }
            } else {
                $result.Status = "Warning"
                $result.Details.Message = "No backups found - fresh installation"
                Write-TestLog "No backups found (may be fresh install)" -Level Warning
            }
        } else {
            $result.Status = "Warning"
            $result.Details.Message = "Backup directory does not exist"
            Write-TestLog "Backup directory not found" -Level Warning
        }
    }
    catch {
        $result.Status = "Failed"
        $result.Error = $_.Exception.Message
        Write-TestLog "Rollback test failed: $($_.Exception.Message)" -Level Error
    }

    $script:UpdateTestResults.Tests += $result
    return $result.Status -eq "Passed"
}

function Test-UpdateProcess {
    Write-TestLog "Testing update process (dry run)..." -Level Info

    $result = @{
        Name = "Update Process Dry Run"
        Status = "Unknown"
        Details = @{}
    }

    try {
        # This is a dry-run test - doesn't actually update
        $agentPath = "C:\Program Files\Sentinel\sentinel-agent.exe"

        if (Test-Path $agentPath) {
            # Test that agent can query update status
            $updateCheck = & $agentPath --check-update 2>&1

            $result.Details.UpdateCheckOutput = $updateCheck | Out-String
            $result.Status = "Passed"
            Write-TestLog "Update check command executed successfully" -Level Success
        } else {
            $result.Status = "Skipped"
            $result.Details.Message = "Agent executable not found"
            Write-TestLog "Agent not installed, skipping update process test" -Level Warning
        }
    }
    catch {
        $result.Status = "Failed"
        $result.Error = $_.Exception.Message
        Write-TestLog "Update process test failed: $($_.Exception.Message)" -Level Error
    }

    $script:UpdateTestResults.Tests += $result
    return $result.Status -eq "Passed"
}

function Test-WatchdogRecovery {
    Write-TestLog "Testing watchdog update recovery..." -Level Info

    $result = @{
        Name = "Watchdog Update Recovery"
        Status = "Unknown"
        Details = @{}
    }

    try {
        $watchdogPath = "C:\Program Files\Sentinel\sentinel-watchdog.exe"

        if (Test-Path $watchdogPath) {
            $service = Get-Service -Name "SentinelWatchdog" -ErrorAction SilentlyContinue

            if ($service) {
                $result.Details.ServiceStatus = $service.Status.ToString()
                $result.Details.StartType = $service.StartType.ToString()

                if ($service.Status -eq "Running") {
                    $result.Status = "Passed"
                    Write-TestLog "Watchdog service is running" -Level Success
                } else {
                    $result.Status = "Warning"
                    $result.Details.Message = "Watchdog service not running"
                    Write-TestLog "Watchdog service not running: $($service.Status)" -Level Warning
                }
            } else {
                $result.Status = "Warning"
                $result.Details.Message = "Watchdog service not installed"
                Write-TestLog "Watchdog service not found" -Level Warning
            }
        } else {
            $result.Status = "Skipped"
            $result.Details.Message = "Watchdog executable not found"
            Write-TestLog "Watchdog not installed" -Level Warning
        }
    }
    catch {
        $result.Status = "Failed"
        $result.Error = $_.Exception.Message
        Write-TestLog "Watchdog test failed: $($_.Exception.Message)" -Level Error
    }

    $script:UpdateTestResults.Tests += $result
    return $result.Status -eq "Passed"
}

# Main test execution
function Start-UpdateTests {
    Write-TestLog "=" * 50 -Level Info
    Write-TestLog "SENTINEL UPDATE SYSTEM TESTS" -Level Info
    Write-TestLog "=" * 50 -Level Info

    $currentVersion = Get-CurrentAgentVersion
    Write-TestLog "Current Agent Version: $(if ($currentVersion) { $currentVersion } else { 'Not Installed' })" -Level Info

    Test-VersionCheck
    Test-DownloadIntegrity
    Test-RollbackCapability
    Test-UpdateProcess
    Test-WatchdogRecovery

    # Summary
    $passed = ($script:UpdateTestResults.Tests | Where-Object { $_.Status -eq "Passed" }).Count
    $failed = ($script:UpdateTestResults.Tests | Where-Object { $_.Status -eq "Failed" }).Count
    $skipped = ($script:UpdateTestResults.Tests | Where-Object { $_.Status -in @("Skipped", "Warning") }).Count

    Write-TestLog "`nUpdate Tests Summary:" -Level Info
    Write-TestLog "  Passed: $passed" -Level Success
    Write-TestLog "  Failed: $failed" -Level $(if ($failed -gt 0) { "Error" } else { "Info" })
    Write-TestLog "  Skipped/Warning: $skipped" -Level Warning

    # Export results
    if ($OutputPath) {
        $resultsFile = Join-Path $OutputPath "update-test-results-$(Get-Date -Format 'yyyyMMdd-HHmmss').json"
        $script:UpdateTestResults | ConvertTo-Json -Depth 10 | Out-File $resultsFile
        Write-TestLog "Results exported to: $resultsFile" -Level Info
    }

    return $script:UpdateTestResults
}

Start-UpdateTests
