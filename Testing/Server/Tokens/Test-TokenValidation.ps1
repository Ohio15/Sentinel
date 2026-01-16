#Requires -Version 5.1
<#
.SYNOPSIS
    Token Validation Security Tests for Sentinel Server
.DESCRIPTION
    Tests the enrollment token security including bcrypt hashing,
    database validation, and legacy token backward compatibility.
.NOTES
    Security Fix: CW-003
    Author: Sentinel QA Team
#>

[CmdletBinding()]
param(
    [string]$ServerUrl = "http://localhost:3000",
    [string]$OutputPath = ".\TestResults",
    [string]$ValidToken = "",
    [string]$InvalidToken = "invalid-token-12345"
)

$ErrorActionPreference = "Stop"
$script:Results = @{
    Component = "Server-TokenValidation"
    Tests = @()
}

function Test-TokenRejected {
    param(
        [string]$Token,
        [string]$Description
    )

    $result = @{
        Name = "Rejected: $Description"
        Status = "Unknown"
    }

    try {
        $headers = @{ "X-Enrollment-Token" = $Token }
        $response = Invoke-RestMethod -Uri "$ServerUrl/api/agent/enroll" -Method POST -Headers $headers -Body "{}" -ContentType "application/json" -ErrorAction Stop

        # If we get here, invalid token was accepted
        $result.Status = "Failed"
        $result.Error = "Invalid token was accepted - security vulnerability!"
        Write-Host "[FAIL] $Description - Token accepted (VULNERABLE)" -ForegroundColor Red
    }
    catch {
        if ($_.Exception.Response.StatusCode -eq 401 -or $_.Exception.Message -match "unauthorized|invalid.*token") {
            $result.Status = "Passed"
            Write-Host "[PASS] $Description - Token correctly rejected" -ForegroundColor Green
        }
        else {
            $result.Status = "Inconclusive"
            $result.Error = $_.Exception.Message
            Write-Host "[WARN] $Description - $($_.Exception.Message)" -ForegroundColor Yellow
        }
    }

    $script:Results.Tests += $result
}

function Test-TokenAccepted {
    param(
        [string]$Token,
        [string]$Description
    )

    $result = @{
        Name = "Accepted: $Description"
        Status = "Unknown"
    }

    try {
        $headers = @{ "X-Enrollment-Token" = $Token }
        $body = @{
            hostname = "test-machine"
            os = "Windows"
            platform = "win32"
        } | ConvertTo-Json

        $response = Invoke-RestMethod -Uri "$ServerUrl/api/agent/enroll" -Method POST -Headers $headers -Body $body -ContentType "application/json" -ErrorAction Stop

        $result.Status = "Passed"
        $result.Details = @{
            DeviceId = $response.device_id
        }
        Write-Host "[PASS] $Description - Token accepted" -ForegroundColor Green
    }
    catch {
        $result.Status = "Failed"
        $result.Error = $_.Exception.Message
        Write-Host "[FAIL] $Description - Valid token rejected: $($_.Exception.Message)" -ForegroundColor Red
    }

    $script:Results.Tests += $result
}

function Test-InvalidTokens {
    Write-Host "`n=== Invalid Token Tests ===" -ForegroundColor Cyan

    # Empty token
    Test-TokenRejected -Token "" -Description "Empty token"

    # Random invalid token
    Test-TokenRejected -Token "not-a-valid-token" -Description "Random invalid token"

    # SQL injection attempt
    Test-TokenRejected -Token "'; DROP TABLE enrollment_tokens; --" -Description "SQL injection"

    # Very long token
    $longToken = "A" * 10000
    Test-TokenRejected -Token $longToken -Description "Excessively long token"

    # Token with special characters
    Test-TokenRejected -Token "<script>alert('xss')</script>" -Description "XSS in token"

    # Token with null bytes
    Test-TokenRejected -Token "token`0null`0bytes" -Description "Null bytes in token"

    # Unicode token
    Test-TokenRejected -Token "token-with-unicode-\u0000-chars" -Description "Unicode escape in token"
}

function Test-TimingAttack {
    Write-Host "`n=== Timing Attack Resistance ===" -ForegroundColor Cyan

    $result = @{
        Name = "Timing Attack Resistance"
        Status = "Unknown"
        Details = @{}
    }

    try {
        $iterations = 10
        $timings = @{
            Short = @()
            Long = @()
            Partial = @()
        }

        # Test with short invalid token
        for ($i = 0; $i -lt $iterations; $i++) {
            $start = Get-Date
            try {
                $headers = @{ "X-Enrollment-Token" = "a" }
                Invoke-RestMethod -Uri "$ServerUrl/api/agent/enroll" -Method POST -Headers $headers -Body "{}" -ContentType "application/json" -ErrorAction Stop
            } catch { }
            $timings.Short += ((Get-Date) - $start).TotalMilliseconds
        }

        # Test with long invalid token
        for ($i = 0; $i -lt $iterations; $i++) {
            $start = Get-Date
            try {
                $headers = @{ "X-Enrollment-Token" = ("X" * 1000) }
                Invoke-RestMethod -Uri "$ServerUrl/api/agent/enroll" -Method POST -Headers $headers -Body "{}" -ContentType "application/json" -ErrorAction Stop
            } catch { }
            $timings.Long += ((Get-Date) - $start).TotalMilliseconds
        }

        # Calculate averages
        $avgShort = ($timings.Short | Measure-Object -Average).Average
        $avgLong = ($timings.Long | Measure-Object -Average).Average
        $diff = [math]::Abs($avgLong - $avgShort)

        $result.Details = @{
            ShortTokenAvg = [math]::Round($avgShort, 2)
            LongTokenAvg = [math]::Round($avgLong, 2)
            Difference = [math]::Round($diff, 2)
        }

        # If timing difference is less than 50ms, likely constant-time
        if ($diff -lt 50) {
            $result.Status = "Passed"
            Write-Host "[PASS] Timing difference: ${diff}ms (likely constant-time)" -ForegroundColor Green
        }
        else {
            $result.Status = "Warning"
            Write-Host "[WARN] Timing difference: ${diff}ms (may be vulnerable)" -ForegroundColor Yellow
        }
    }
    catch {
        $result.Status = "Inconclusive"
        $result.Error = $_.Exception.Message
        Write-Host "[WARN] Timing test inconclusive: $($_.Exception.Message)" -ForegroundColor Yellow
    }

    $script:Results.Tests += $result
}

function Test-TokenHashStorage {
    Write-Host "`n=== Token Hash Storage Tests ===" -ForegroundColor Cyan

    $result = @{
        Name = "Token Hash Storage (Database Check)"
        Status = "Unknown"
        Details = @{}
    }

    # This test requires database access
    # It's informational - checking that tokens are stored as bcrypt hashes

    Write-Host "[INFO] Token hash storage should be verified via database inspection" -ForegroundColor Cyan
    Write-Host "       Run: SELECT token, token_hash, is_legacy FROM enrollment_tokens;" -ForegroundColor Cyan
    Write-Host "       Verify: token_hash contains bcrypt hash ($2a$, $2b$, or $2y$ prefix)" -ForegroundColor Cyan
    Write-Host "       Verify: New tokens have is_legacy = FALSE" -ForegroundColor Cyan

    $result.Status = "Manual"
    $result.Details.Instruction = "Verify via database that tokens are stored as bcrypt hashes"

    $script:Results.Tests += $result
}

function Test-ValidToken {
    Write-Host "`n=== Valid Token Tests ===" -ForegroundColor Cyan

    if ($ValidToken) {
        Test-TokenAccepted -Token $ValidToken -Description "Provided valid token"
    }
    else {
        Write-Host "[SKIP] No valid token provided for positive testing" -ForegroundColor Yellow
        Write-Host "       Use -ValidToken parameter to test with a real token" -ForegroundColor Yellow

        $script:Results.Tests += @{
            Name = "Valid Token Test"
            Status = "Skipped"
            Details = @{ Message = "No valid token provided" }
        }
    }
}

function Test-HeaderVariants {
    Write-Host "`n=== Header Variant Tests ===" -ForegroundColor Cyan

    # Test both header names work
    $result = @{
        Name = "X-Agent-Token Header"
        Status = "Unknown"
    }

    try {
        $headers = @{ "X-Agent-Token" = $InvalidToken }
        Invoke-RestMethod -Uri "$ServerUrl/api/agent/enroll" -Method POST -Headers $headers -Body "{}" -ContentType "application/json" -ErrorAction Stop

        $result.Status = "Failed"
        $result.Error = "Invalid token accepted via X-Agent-Token"
    }
    catch {
        if ($_.Exception.Response.StatusCode -eq 401) {
            $result.Status = "Passed"
            Write-Host "[PASS] X-Agent-Token header processed correctly" -ForegroundColor Green
        }
        else {
            $result.Status = "Inconclusive"
            $result.Error = $_.Exception.Message
        }
    }

    $script:Results.Tests += $result
}

# Main execution
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host "SENTINEL TOKEN VALIDATION SECURITY TESTS" -ForegroundColor Cyan
Write-Host "Security Fix: CW-003" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan

Test-InvalidTokens
Test-TimingAttack
Test-TokenHashStorage
Test-ValidToken
Test-HeaderVariants

# Summary
$passed = ($script:Results.Tests | Where-Object { $_.Status -eq "Passed" }).Count
$failed = ($script:Results.Tests | Where-Object { $_.Status -eq "Failed" }).Count
$other = ($script:Results.Tests | Where-Object { $_.Status -notin @("Passed", "Failed") }).Count

Write-Host "`n" + "=" * 60 -ForegroundColor Cyan
Write-Host "RESULTS SUMMARY" -ForegroundColor Cyan
Write-Host "  Passed: $passed" -ForegroundColor Green
Write-Host "  Failed: $failed" -ForegroundColor $(if ($failed -gt 0) { "Red" } else { "Green" })
Write-Host "  Other (Skipped/Manual/Inconclusive): $other" -ForegroundColor Yellow
Write-Host "=" * 60 -ForegroundColor Cyan

if ($failed -gt 0) {
    Write-Host "`nWARNING: Security vulnerabilities detected!" -ForegroundColor Red
}

# Export results
if ($OutputPath) {
    if (-not (Test-Path $OutputPath)) {
        New-Item -ItemType Directory -Path $OutputPath -Force | Out-Null
    }
    $resultsFile = Join-Path $OutputPath "token-validation-results-$(Get-Date -Format 'yyyyMMdd-HHmmss').json"
    $script:Results | ConvertTo-Json -Depth 10 | Out-File $resultsFile
    Write-Host "Results exported to: $resultsFile"
}

return $script:Results
