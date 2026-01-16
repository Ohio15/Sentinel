#Requires -Version 5.1
<#
.SYNOPSIS
    Master Test Controller for Sentinel Agent Testing
.DESCRIPTION
    Orchestrates comprehensive testing of all Sentinel agent components including
    update system, recovery mechanisms, and generates consolidated reports.
.NOTES
    Author: Sentinel QA Team
    Version: 1.0.0
#>

[CmdletBinding()]
param(
    [Parameter()]
    [string]$TestSuite = "All",

    [Parameter()]
    [string]$OutputPath = ".\TestResults",

    [Parameter()]
    [switch]$SkipUpdate,

    [Parameter()]
    [switch]$SkipRecovery,

    [Parameter()]
    [switch]$Verbose
)

$ErrorActionPreference = "Stop"
$script:TestResults = @{
    StartTime = Get-Date
    EndTime = $null
    TotalTests = 0
    Passed = 0
    Failed = 0
    Skipped = 0
    Results = @()
}

function Write-TestLog {
    param(
        [string]$Message,
        [ValidateSet("Info", "Warning", "Error", "Success")]
        [string]$Level = "Info"
    )

    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $color = switch ($Level) {
        "Info" { "White" }
        "Warning" { "Yellow" }
        "Error" { "Red" }
        "Success" { "Green" }
    }

    Write-Host "[$timestamp] [$Level] $Message" -ForegroundColor $color
}

function Initialize-TestEnvironment {
    Write-TestLog "Initializing test environment..." -Level Info

    # Create output directory
    if (-not (Test-Path $OutputPath)) {
        New-Item -ItemType Directory -Path $OutputPath -Force | Out-Null
    }

    # Verify Sentinel agent is installed
    $agentPath = "C:\Program Files\Sentinel\sentinel-agent.exe"
    if (-not (Test-Path $agentPath)) {
        Write-TestLog "Sentinel agent not found at $agentPath" -Level Warning
    }

    # Check service status
    $service = Get-Service -Name "SentinelAgent" -ErrorAction SilentlyContinue
    if ($service) {
        Write-TestLog "Sentinel Agent service status: $($service.Status)" -Level Info
    }

    Write-TestLog "Test environment initialized" -Level Success
}

function Invoke-TestScript {
    param(
        [string]$ScriptPath,
        [string]$TestName,
        [hashtable]$Parameters = @{}
    )

    $script:TestResults.TotalTests++

    try {
        Write-TestLog "Running test: $TestName" -Level Info
        $startTime = Get-Date

        $result = & $ScriptPath @Parameters

        $endTime = Get-Date
        $duration = ($endTime - $startTime).TotalSeconds

        $script:TestResults.Passed++
        $script:TestResults.Results += @{
            Name = $TestName
            Status = "Passed"
            Duration = $duration
            Details = $result
        }

        Write-TestLog "Test passed: $TestName (${duration}s)" -Level Success
        return $true
    }
    catch {
        $script:TestResults.Failed++
        $script:TestResults.Results += @{
            Name = $TestName
            Status = "Failed"
            Error = $_.Exception.Message
            StackTrace = $_.ScriptStackTrace
        }

        Write-TestLog "Test failed: $TestName - $($_.Exception.Message)" -Level Error
        return $false
    }
}

function Test-AgentConnectivity {
    Write-TestLog "Testing agent connectivity..." -Level Info

    $tests = @(
        @{
            Name = "Local Agent API"
            Test = {
                $response = Invoke-RestMethod -Uri "http://localhost:8090/health" -TimeoutSec 5
                return $response.status -eq "healthy"
            }
        },
        @{
            Name = "Server Connection"
            Test = {
                $config = Get-Content "C:\ProgramData\Sentinel\config.json" | ConvertFrom-Json
                $response = Invoke-WebRequest -Uri "$($config.server_url)/api/health" -TimeoutSec 10
                return $response.StatusCode -eq 200
            }
        }
    )

    foreach ($test in $tests) {
        $script:TestResults.TotalTests++
        try {
            $result = & $test.Test
            if ($result) {
                $script:TestResults.Passed++
                $script:TestResults.Results += @{
                    Name = $test.Name
                    Status = "Passed"
                }
                Write-TestLog "$($test.Name): Passed" -Level Success
            } else {
                throw "Test returned false"
            }
        }
        catch {
            $script:TestResults.Failed++
            $script:TestResults.Results += @{
                Name = $test.Name
                Status = "Failed"
                Error = $_.Exception.Message
            }
            Write-TestLog "$($test.Name): Failed - $($_.Exception.Message)" -Level Error
        }
    }
}

function Test-SecurityComponents {
    Write-TestLog "Testing security components..." -Level Info

    # Test path traversal protection
    $script:TestResults.TotalTests++
    try {
        $maliciousPath = "C:\Users\admin\..\..\..\Windows\System32\config"
        $response = Invoke-RestMethod -Uri "http://localhost:8090/api/files/list" -Method POST -Body (@{path=$maliciousPath} | ConvertTo-Json) -ContentType "application/json" -ErrorAction Stop

        # If we get here without error, the protection failed
        $script:TestResults.Failed++
        $script:TestResults.Results += @{
            Name = "Path Traversal Protection"
            Status = "Failed"
            Error = "Path traversal attack was not blocked"
        }
        Write-TestLog "Path Traversal Protection: FAILED - Attack not blocked!" -Level Error
    }
    catch {
        if ($_.Exception.Message -match "not allowed|denied|forbidden|outside") {
            $script:TestResults.Passed++
            $script:TestResults.Results += @{
                Name = "Path Traversal Protection"
                Status = "Passed"
            }
            Write-TestLog "Path Traversal Protection: Passed" -Level Success
        } else {
            $script:TestResults.Failed++
            $script:TestResults.Results += @{
                Name = "Path Traversal Protection"
                Status = "Failed"
                Error = $_.Exception.Message
            }
            Write-TestLog "Path Traversal Protection: Failed - $($_.Exception.Message)" -Level Error
        }
    }

    # Test command validation
    $script:TestResults.TotalTests++
    try {
        $dangerousCmd = "rm -rf /"
        $response = Invoke-RestMethod -Uri "http://localhost:8090/api/execute" -Method POST -Body (@{command=$dangerousCmd; type="bash"} | ConvertTo-Json) -ContentType "application/json" -ErrorAction Stop

        $script:TestResults.Failed++
        $script:TestResults.Results += @{
            Name = "Dangerous Command Blocking"
            Status = "Failed"
            Error = "Dangerous command was not blocked"
        }
        Write-TestLog "Dangerous Command Blocking: FAILED - Command not blocked!" -Level Error
    }
    catch {
        if ($_.Exception.Message -match "blacklisted|blocked|denied|validation failed") {
            $script:TestResults.Passed++
            $script:TestResults.Results += @{
                Name = "Dangerous Command Blocking"
                Status = "Passed"
            }
            Write-TestLog "Dangerous Command Blocking: Passed" -Level Success
        } else {
            $script:TestResults.Failed++
            $script:TestResults.Results += @{
                Name = "Dangerous Command Blocking"
                Status = "Failed"
                Error = $_.Exception.Message
            }
            Write-TestLog "Dangerous Command Blocking: Failed - $($_.Exception.Message)" -Level Error
        }
    }
}

function Start-MasterTest {
    Write-TestLog "=" * 60 -Level Info
    Write-TestLog "SENTINEL MASTER TEST SUITE" -Level Info
    Write-TestLog "=" * 60 -Level Info

    Initialize-TestEnvironment

    # Run connectivity tests
    Test-AgentConnectivity

    # Run security tests
    Test-SecurityComponents

    # Run update tests
    if (-not $SkipUpdate) {
        $updateScript = Join-Path $PSScriptRoot "Test-SentinelUpdate.ps1"
        if (Test-Path $updateScript) {
            Invoke-TestScript -ScriptPath $updateScript -TestName "Update System Tests" -Parameters @{OutputPath = $OutputPath}
        } else {
            Write-TestLog "Update test script not found, skipping" -Level Warning
            $script:TestResults.Skipped++
        }
    } else {
        Write-TestLog "Skipping update tests (SkipUpdate flag set)" -Level Info
        $script:TestResults.Skipped++
    }

    # Run recovery tests
    if (-not $SkipRecovery) {
        $recoveryScript = Join-Path $PSScriptRoot "Test-SentinelRecovery.ps1"
        if (Test-Path $recoveryScript) {
            Invoke-TestScript -ScriptPath $recoveryScript -TestName "Recovery System Tests" -Parameters @{OutputPath = $OutputPath}
        } else {
            Write-TestLog "Recovery test script not found, skipping" -Level Warning
            $script:TestResults.Skipped++
        }
    } else {
        Write-TestLog "Skipping recovery tests (SkipRecovery flag set)" -Level Info
        $script:TestResults.Skipped++
    }

    # Generate report
    $script:TestResults.EndTime = Get-Date

    $reportScript = Join-Path $PSScriptRoot "Test-SentinelReport.ps1"
    if (Test-Path $reportScript) {
        & $reportScript -TestResults $script:TestResults -OutputPath $OutputPath
    } else {
        # Inline report generation
        Write-TestLog "`n" + "=" * 60 -Level Info
        Write-TestLog "TEST RESULTS SUMMARY" -Level Info
        Write-TestLog "=" * 60 -Level Info
        Write-TestLog "Total Tests: $($script:TestResults.TotalTests)" -Level Info
        Write-TestLog "Passed: $($script:TestResults.Passed)" -Level Success
        Write-TestLog "Failed: $($script:TestResults.Failed)" -Level $(if ($script:TestResults.Failed -gt 0) { "Error" } else { "Info" })
        Write-TestLog "Skipped: $($script:TestResults.Skipped)" -Level Warning

        $duration = ($script:TestResults.EndTime - $script:TestResults.StartTime).TotalSeconds
        Write-TestLog "Total Duration: ${duration}s" -Level Info
    }

    # Export results
    $resultsFile = Join-Path $OutputPath "test-results-$(Get-Date -Format 'yyyyMMdd-HHmmss').json"
    $script:TestResults | ConvertTo-Json -Depth 10 | Out-File $resultsFile
    Write-TestLog "Results exported to: $resultsFile" -Level Info

    return $script:TestResults
}

# Run tests
Start-MasterTest
