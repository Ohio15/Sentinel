#Requires -Version 5.1
<#
.SYNOPSIS
    Path Traversal Security Tests for Sentinel File Transfer
.DESCRIPTION
    Tests the path traversal protection in the file transfer component
    to ensure directory boundary validation works correctly.
.NOTES
    Security Fix: CW-001
    Author: Sentinel QA Team
#>

[CmdletBinding()]
param(
    [string]$AgentUrl = "http://localhost:8090",
    [string]$OutputPath = ".\TestResults"
)

$ErrorActionPreference = "Stop"
$script:Results = @{
    Component = "FileTransfer-PathTraversal"
    Tests = @()
}

function Test-PathTraversalBlocked {
    param([string]$TestPath, [string]$Description)

    $result = @{
        Name = "Path Traversal: $Description"
        TestPath = $TestPath
        Status = "Unknown"
    }

    try {
        $body = @{ path = $TestPath } | ConvertTo-Json
        $response = Invoke-RestMethod -Uri "$AgentUrl/api/files/list" -Method POST -Body $body -ContentType "application/json" -ErrorAction Stop

        # If we get here, the attack was NOT blocked
        $result.Status = "Failed"
        $result.Error = "Path traversal attack was NOT blocked - security vulnerability!"
        Write-Host "[FAIL] $Description - Attack succeeded (VULNERABLE)" -ForegroundColor Red
    }
    catch {
        if ($_.Exception.Message -match "not allowed|denied|forbidden|outside|path validation") {
            $result.Status = "Passed"
            Write-Host "[PASS] $Description - Attack blocked correctly" -ForegroundColor Green
        }
        else {
            # Other error, might still be a pass if it's a 403/401
            if ($_.Exception.Response.StatusCode -in @(403, 401)) {
                $result.Status = "Passed"
                Write-Host "[PASS] $Description - Blocked with status code" -ForegroundColor Green
            }
            else {
                $result.Status = "Inconclusive"
                $result.Error = $_.Exception.Message
                Write-Host "[WARN] $Description - Inconclusive: $($_.Exception.Message)" -ForegroundColor Yellow
            }
        }
    }

    $script:Results.Tests += $result
}

function Test-DirectoryBoundary {
    Write-Host "`n=== Directory Boundary Tests ===" -ForegroundColor Cyan

    # Basic traversal attempts
    Test-PathTraversalBlocked -TestPath "C:\Users\admin\..\..\..\Windows\System32" -Description "Basic parent directory traversal"
    Test-PathTraversalBlocked -TestPath "C:\ProgramData\Sentinel\..\..\Windows\System32\config" -Description "Traversal from allowed directory"
    Test-PathTraversalBlocked -TestPath "/etc/../../../etc/passwd" -Description "Unix-style traversal"
    Test-PathTraversalBlocked -TestPath "....//....//....//etc/passwd" -Description "Double-dot bypass attempt"
}

function Test-EncodingBypass {
    Write-Host "`n=== Encoding Bypass Tests ===" -ForegroundColor Cyan

    # URL encoding attempts
    Test-PathTraversalBlocked -TestPath "C:\Users\admin\%2e%2e\%2e%2e\Windows" -Description "URL-encoded dots"
    Test-PathTraversalBlocked -TestPath "C:\Users\admin\..%252f..%252fWindows" -Description "Double URL encoding"

    # Unicode attempts
    Test-PathTraversalBlocked -TestPath "C:\Users\admin\..%c0%af..%c0%afWindows" -Description "UTF-8 overlong encoding"
    Test-PathTraversalBlocked -TestPath "C:\Users\admin\..%u002f..%u002fWindows" -Description "Unicode encoding"
}

function Test-SimilarPathBypass {
    Write-Host "`n=== Similar Path Bypass Tests ===" -ForegroundColor Cyan

    # Tests for strings.HasPrefix vulnerability
    # If base is "C:\Users\admin", "C:\Users\admin-attacker" should NOT be allowed
    Test-PathTraversalBlocked -TestPath "C:\Users\admin-attacker\secrets" -Description "Similar prefix attack (admin-attacker)"
    Test-PathTraversalBlocked -TestPath "C:\ProgramData\SentinelMalware" -Description "Similar prefix attack (SentinelMalware)"
}

function Test-NullByteInjection {
    Write-Host "`n=== Null Byte Injection Tests ===" -ForegroundColor Cyan

    Test-PathTraversalBlocked -TestPath "C:\ProgramData\Sentinel\..\..\Windows\System32%00.txt" -Description "Null byte truncation"
}

function Test-WindowsSpecific {
    Write-Host "`n=== Windows-Specific Tests ===" -ForegroundColor Cyan

    # Windows alternate data streams
    Test-PathTraversalBlocked -TestPath "C:\ProgramData\Sentinel\file.txt:Zone.Identifier" -Description "Alternate data stream"

    # Windows reserved names
    Test-PathTraversalBlocked -TestPath "C:\ProgramData\Sentinel\..\..\CON" -Description "Reserved name CON"
    Test-PathTraversalBlocked -TestPath "C:\ProgramData\Sentinel\..\..\NUL" -Description "Reserved name NUL"

    # UNC path attempts
    Test-PathTraversalBlocked -TestPath "\\127.0.0.1\C$\Windows\System32" -Description "UNC path to C$"
    Test-PathTraversalBlocked -TestPath "\\?\C:\Windows\System32" -Description "Extended path prefix"
}

function Test-ValidPaths {
    Write-Host "`n=== Valid Path Tests (Should Succeed) ===" -ForegroundColor Cyan

    $validPaths = @(
        "C:\ProgramData\Sentinel\logs",
        "C:\ProgramData\Sentinel\config"
    )

    foreach ($path in $validPaths) {
        $result = @{
            Name = "Valid Path: $path"
            TestPath = $path
            Status = "Unknown"
        }

        try {
            $body = @{ path = $path } | ConvertTo-Json
            $response = Invoke-RestMethod -Uri "$AgentUrl/api/files/list" -Method POST -Body $body -ContentType "application/json" -TimeoutSec 10

            $result.Status = "Passed"
            Write-Host "[PASS] Valid path accepted: $path" -ForegroundColor Green
        }
        catch {
            if ($_.Exception.Message -match "not found|does not exist") {
                $result.Status = "Passed"
                $result.Details = "Path allowed but does not exist"
                Write-Host "[PASS] Valid path accepted (not found): $path" -ForegroundColor Green
            }
            else {
                $result.Status = "Failed"
                $result.Error = "Valid path was rejected: $($_.Exception.Message)"
                Write-Host "[FAIL] Valid path rejected: $path - $($_.Exception.Message)" -ForegroundColor Red
            }
        }

        $script:Results.Tests += $result
    }
}

# Main execution
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host "SENTINEL PATH TRAVERSAL SECURITY TESTS" -ForegroundColor Cyan
Write-Host "Security Fix: CW-001" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan

Test-DirectoryBoundary
Test-EncodingBypass
Test-SimilarPathBypass
Test-NullByteInjection
Test-WindowsSpecific
Test-ValidPaths

# Summary
$passed = ($script:Results.Tests | Where-Object { $_.Status -eq "Passed" }).Count
$failed = ($script:Results.Tests | Where-Object { $_.Status -eq "Failed" }).Count
$inconclusive = ($script:Results.Tests | Where-Object { $_.Status -eq "Inconclusive" }).Count

Write-Host "`n" + "=" * 60 -ForegroundColor Cyan
Write-Host "RESULTS SUMMARY" -ForegroundColor Cyan
Write-Host "  Passed: $passed" -ForegroundColor Green
Write-Host "  Failed: $failed" -ForegroundColor $(if ($failed -gt 0) { "Red" } else { "Green" })
Write-Host "  Inconclusive: $inconclusive" -ForegroundColor Yellow
Write-Host "=" * 60 -ForegroundColor Cyan

if ($failed -gt 0) {
    Write-Host "`nWARNING: Security vulnerabilities detected!" -ForegroundColor Red
}

# Export results
if ($OutputPath) {
    if (-not (Test-Path $OutputPath)) {
        New-Item -ItemType Directory -Path $OutputPath -Force | Out-Null
    }
    $resultsFile = Join-Path $OutputPath "path-traversal-results-$(Get-Date -Format 'yyyyMMdd-HHmmss').json"
    $script:Results | ConvertTo-Json -Depth 10 | Out-File $resultsFile
    Write-Host "Results exported to: $resultsFile"
}

return $script:Results
