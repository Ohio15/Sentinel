#Requires -Version 5.1
<#
.SYNOPSIS
    Recovery System Tester for Sentinel Agent
.DESCRIPTION
    Tests the Sentinel agent recovery mechanisms including watchdog functionality,
    service restart capabilities, and failure recovery scenarios.
.NOTES
    Author: Sentinel QA Team
    Version: 1.0.0
#>

[CmdletBinding()]
param(
    [Parameter()]
    [string]$OutputPath = ".\TestResults",

    [Parameter()]
    [switch]$DestructiveTests,

    [Parameter()]
    [int]$RecoveryTimeout = 60
)

$ErrorActionPreference = "Stop"
$script:RecoveryTestResults = @{
    Component = "RecoverySystem"
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
    Write-Host "[RECOVERY-TEST] $Message" -ForegroundColor $color
}

function Test-ServiceRecoveryConfig {
    Write-TestLog "Testing service recovery configuration..." -Level Info

    $result = @{
        Name = "Service Recovery Configuration"
        Status = "Unknown"
        Details = @{}
    }

    try {
        $serviceName = "SentinelAgent"
        $service = Get-WmiObject -Class Win32_Service -Filter "Name='$serviceName'" -ErrorAction SilentlyContinue

        if ($service) {
            # Check recovery options via sc.exe
            $recoveryInfo = & sc.exe qfailure $serviceName 2>&1

            $result.Details.RecoveryConfig = $recoveryInfo | Out-String

            # Parse recovery actions
            if ($recoveryInfo -match "RESTART") {
                $result.Status = "Passed"
                $result.Details.HasAutoRestart = $true
                Write-TestLog "Service has auto-restart configured" -Level Success
            } else {
                $result.Status = "Warning"
                $result.Details.HasAutoRestart = $false
                Write-TestLog "Service does not have auto-restart configured" -Level Warning
            }
        } else {
            $result.Status = "Skipped"
            $result.Details.Message = "Service not found"
            Write-TestLog "SentinelAgent service not found" -Level Warning
        }
    }
    catch {
        $result.Status = "Failed"
        $result.Error = $_.Exception.Message
        Write-TestLog "Service recovery config test failed: $($_.Exception.Message)" -Level Error
    }

    $script:RecoveryTestResults.Tests += $result
    return $result.Status -eq "Passed"
}

function Test-WatchdogHealth {
    Write-TestLog "Testing watchdog health..." -Level Info

    $result = @{
        Name = "Watchdog Health"
        Status = "Unknown"
        Details = @{}
    }

    try {
        $watchdogService = Get-Service -Name "SentinelWatchdog" -ErrorAction SilentlyContinue

        if ($watchdogService) {
            $result.Details.Status = $watchdogService.Status.ToString()

            if ($watchdogService.Status -eq "Running") {
                # Check watchdog process
                $watchdogProcess = Get-Process -Name "sentinel-watchdog" -ErrorAction SilentlyContinue

                if ($watchdogProcess) {
                    $result.Details.ProcessId = $watchdogProcess.Id
                    $result.Details.WorkingSet = [math]::Round($watchdogProcess.WorkingSet64 / 1MB, 2)
                    $result.Details.StartTime = $watchdogProcess.StartTime.ToString()

                    $result.Status = "Passed"
                    Write-TestLog "Watchdog is healthy (PID: $($watchdogProcess.Id))" -Level Success
                } else {
                    $result.Status = "Failed"
                    $result.Error = "Watchdog service running but process not found"
                    Write-TestLog "Watchdog process not found" -Level Error
                }
            } else {
                $result.Status = "Failed"
                $result.Error = "Watchdog service not running"
                Write-TestLog "Watchdog service not running: $($watchdogService.Status)" -Level Error
            }
        } else {
            $result.Status = "Skipped"
            $result.Details.Message = "Watchdog service not installed"
            Write-TestLog "Watchdog service not installed" -Level Warning
        }
    }
    catch {
        $result.Status = "Failed"
        $result.Error = $_.Exception.Message
        Write-TestLog "Watchdog health test failed: $($_.Exception.Message)" -Level Error
    }

    $script:RecoveryTestResults.Tests += $result
    return $result.Status -eq "Passed"
}

function Test-AgentHealthEndpoint {
    Write-TestLog "Testing agent health endpoint..." -Level Info

    $result = @{
        Name = "Agent Health Endpoint"
        Status = "Unknown"
        Details = @{}
    }

    try {
        $response = Invoke-RestMethod -Uri "http://localhost:8090/health" -Method GET -TimeoutSec 10

        $result.Details = $response

        if ($response.status -eq "healthy") {
            $result.Status = "Passed"
            Write-TestLog "Agent health endpoint reports healthy" -Level Success
        } else {
            $result.Status = "Warning"
            $result.Details.Message = "Agent reports unhealthy status"
            Write-TestLog "Agent reports non-healthy status: $($response.status)" -Level Warning
        }
    }
    catch {
        $result.Status = "Failed"
        $result.Error = $_.Exception.Message
        Write-TestLog "Health endpoint test failed: $($_.Exception.Message)" -Level Error
    }

    $script:RecoveryTestResults.Tests += $result
    return $result.Status -eq "Passed"
}

function Test-HeartbeatMechanism {
    Write-TestLog "Testing heartbeat mechanism..." -Level Info

    $result = @{
        Name = "Heartbeat Mechanism"
        Status = "Unknown"
        Details = @{}
    }

    try {
        $config = Get-Content "C:\ProgramData\Sentinel\config.json" -ErrorAction SilentlyContinue | ConvertFrom-Json

        if ($config) {
            $result.Details.HeartbeatInterval = $config.heartbeat_interval
            $result.Details.ServerUrl = $config.server_url

            # Check if agent is sending heartbeats (check logs)
            $logPath = "C:\ProgramData\Sentinel\logs\agent.log"
            if (Test-Path $logPath) {
                $recentLogs = Get-Content $logPath -Tail 100
                $heartbeatLogs = $recentLogs | Where-Object { $_ -match "heartbeat|ping" }

                if ($heartbeatLogs.Count -gt 0) {
                    $result.Details.RecentHeartbeats = $heartbeatLogs.Count
                    $result.Status = "Passed"
                    Write-TestLog "Found $($heartbeatLogs.Count) recent heartbeat entries" -Level Success
                } else {
                    $result.Status = "Warning"
                    $result.Details.Message = "No recent heartbeat entries in logs"
                    Write-TestLog "No recent heartbeat logs found" -Level Warning
                }
            } else {
                $result.Status = "Warning"
                $result.Details.Message = "Agent log file not found"
                Write-TestLog "Agent log file not found" -Level Warning
            }
        } else {
            $result.Status = "Skipped"
            $result.Details.Message = "Config file not found"
            Write-TestLog "Config file not found" -Level Warning
        }
    }
    catch {
        $result.Status = "Failed"
        $result.Error = $_.Exception.Message
        Write-TestLog "Heartbeat test failed: $($_.Exception.Message)" -Level Error
    }

    $script:RecoveryTestResults.Tests += $result
    return $result.Status -eq "Passed"
}

function Test-CrashRecovery {
    Write-TestLog "Testing crash recovery (simulation)..." -Level Info

    $result = @{
        Name = "Crash Recovery Simulation"
        Status = "Unknown"
        Details = @{}
    }

    if (-not $DestructiveTests) {
        $result.Status = "Skipped"
        $result.Details.Message = "Destructive tests not enabled (use -DestructiveTests flag)"
        Write-TestLog "Skipping crash recovery test (destructive tests disabled)" -Level Warning
        $script:RecoveryTestResults.Tests += $result
        return $false
    }

    try {
        $agentProcess = Get-Process -Name "sentinel-agent" -ErrorAction SilentlyContinue

        if ($agentProcess) {
            $originalPid = $agentProcess.Id
            Write-TestLog "Killing agent process (PID: $originalPid)..." -Level Warning

            Stop-Process -Id $originalPid -Force

            # Wait for recovery
            Write-TestLog "Waiting for watchdog recovery (max $RecoveryTimeout seconds)..." -Level Info

            $recovered = $false
            $startTime = Get-Date

            while ((Get-Date) -lt $startTime.AddSeconds($RecoveryTimeout)) {
                Start-Sleep -Seconds 2
                $newProcess = Get-Process -Name "sentinel-agent" -ErrorAction SilentlyContinue

                if ($newProcess -and $newProcess.Id -ne $originalPid) {
                    $recovered = $true
                    $recoveryTime = ((Get-Date) - $startTime).TotalSeconds

                    $result.Details.RecoveryTime = $recoveryTime
                    $result.Details.NewPid = $newProcess.Id
                    $result.Details.OriginalPid = $originalPid

                    Write-TestLog "Agent recovered in $recoveryTime seconds (new PID: $($newProcess.Id))" -Level Success
                    break
                }
            }

            if ($recovered) {
                $result.Status = "Passed"
            } else {
                $result.Status = "Failed"
                $result.Error = "Agent did not recover within $RecoveryTimeout seconds"
                Write-TestLog "Agent failed to recover within timeout" -Level Error
            }
        } else {
            $result.Status = "Skipped"
            $result.Details.Message = "Agent process not running"
            Write-TestLog "Agent not running, skipping crash recovery test" -Level Warning
        }
    }
    catch {
        $result.Status = "Failed"
        $result.Error = $_.Exception.Message
        Write-TestLog "Crash recovery test failed: $($_.Exception.Message)" -Level Error
    }

    $script:RecoveryTestResults.Tests += $result
    return $result.Status -eq "Passed"
}

function Test-ConfigRecovery {
    Write-TestLog "Testing config recovery..." -Level Info

    $result = @{
        Name = "Config Recovery"
        Status = "Unknown"
        Details = @{}
    }

    try {
        $configPath = "C:\ProgramData\Sentinel\config.json"
        $backupPath = "C:\ProgramData\Sentinel\config.json.backup"

        if (Test-Path $configPath) {
            $config = Get-Content $configPath | ConvertFrom-Json
            $result.Details.ConfigExists = $true
            $result.Details.RequiredFields = @("server_url", "device_id")

            # Check required fields
            $missingFields = @()
            foreach ($field in $result.Details.RequiredFields) {
                if (-not $config.$field) {
                    $missingFields += $field
                }
            }

            if ($missingFields.Count -eq 0) {
                $result.Status = "Passed"
                Write-TestLog "Config has all required fields" -Level Success
            } else {
                $result.Status = "Warning"
                $result.Details.MissingFields = $missingFields
                Write-TestLog "Config missing fields: $($missingFields -join ', ')" -Level Warning
            }

            # Check for backup
            if (Test-Path $backupPath) {
                $result.Details.BackupExists = $true
                Write-TestLog "Config backup exists" -Level Info
            } else {
                $result.Details.BackupExists = $false
                Write-TestLog "No config backup found" -Level Warning
            }
        } else {
            $result.Status = "Failed"
            $result.Error = "Config file not found"
            Write-TestLog "Config file not found at $configPath" -Level Error
        }
    }
    catch {
        $result.Status = "Failed"
        $result.Error = $_.Exception.Message
        Write-TestLog "Config recovery test failed: $($_.Exception.Message)" -Level Error
    }

    $script:RecoveryTestResults.Tests += $result
    return $result.Status -eq "Passed"
}

function Test-LogRotation {
    Write-TestLog "Testing log rotation..." -Level Info

    $result = @{
        Name = "Log Rotation"
        Status = "Unknown"
        Details = @{}
    }

    try {
        $logPath = "C:\ProgramData\Sentinel\logs"

        if (Test-Path $logPath) {
            $logs = Get-ChildItem -Path $logPath -Filter "*.log*" -Recurse

            $result.Details.TotalLogFiles = $logs.Count
            $result.Details.TotalSize = [math]::Round(($logs | Measure-Object -Property Length -Sum).Sum / 1MB, 2)

            # Check if logs are being rotated (look for numbered backups)
            $rotatedLogs = $logs | Where-Object { $_.Name -match "\.\d+$" -or $_.Name -match "\.log\." }

            if ($rotatedLogs.Count -gt 0) {
                $result.Details.RotatedLogs = $rotatedLogs.Count
                $result.Status = "Passed"
                Write-TestLog "Found $($rotatedLogs.Count) rotated log files" -Level Success
            } else {
                $result.Status = "Warning"
                $result.Details.Message = "No rotated logs found (may be new installation)"
                Write-TestLog "No rotated logs found" -Level Warning
            }

            # Check for oversized logs
            $oversizedLogs = $logs | Where-Object { $_.Length -gt 50MB }
            if ($oversizedLogs.Count -gt 0) {
                $result.Status = "Warning"
                $result.Details.OversizedLogs = $oversizedLogs.Name
                Write-TestLog "Found oversized logs: $($oversizedLogs.Name -join ', ')" -Level Warning
            }
        } else {
            $result.Status = "Skipped"
            $result.Details.Message = "Log directory not found"
            Write-TestLog "Log directory not found" -Level Warning
        }
    }
    catch {
        $result.Status = "Failed"
        $result.Error = $_.Exception.Message
        Write-TestLog "Log rotation test failed: $($_.Exception.Message)" -Level Error
    }

    $script:RecoveryTestResults.Tests += $result
    return $result.Status -eq "Passed"
}

# Main test execution
function Start-RecoveryTests {
    Write-TestLog "=" * 50 -Level Info
    Write-TestLog "SENTINEL RECOVERY SYSTEM TESTS" -Level Info
    Write-TestLog "=" * 50 -Level Info

    if ($DestructiveTests) {
        Write-TestLog "WARNING: Destructive tests enabled!" -Level Warning
    }

    Test-ServiceRecoveryConfig
    Test-WatchdogHealth
    Test-AgentHealthEndpoint
    Test-HeartbeatMechanism
    Test-CrashRecovery
    Test-ConfigRecovery
    Test-LogRotation

    # Summary
    $passed = ($script:RecoveryTestResults.Tests | Where-Object { $_.Status -eq "Passed" }).Count
    $failed = ($script:RecoveryTestResults.Tests | Where-Object { $_.Status -eq "Failed" }).Count
    $skipped = ($script:RecoveryTestResults.Tests | Where-Object { $_.Status -in @("Skipped", "Warning") }).Count

    Write-TestLog "`nRecovery Tests Summary:" -Level Info
    Write-TestLog "  Passed: $passed" -Level Success
    Write-TestLog "  Failed: $failed" -Level $(if ($failed -gt 0) { "Error" } else { "Info" })
    Write-TestLog "  Skipped/Warning: $skipped" -Level Warning

    # Export results
    if ($OutputPath) {
        $resultsFile = Join-Path $OutputPath "recovery-test-results-$(Get-Date -Format 'yyyyMMdd-HHmmss').json"
        $script:RecoveryTestResults | ConvertTo-Json -Depth 10 | Out-File $resultsFile
        Write-TestLog "Results exported to: $resultsFile" -Level Info
    }

    return $script:RecoveryTestResults
}

Start-RecoveryTests
