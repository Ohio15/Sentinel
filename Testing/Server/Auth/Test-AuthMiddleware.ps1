#Requires -Version 5.1
<#
.SYNOPSIS
    Authentication Middleware Tests for Sentinel Server
.DESCRIPTION
    Tests JWT validation, API key authentication, and role-based access control.
.NOTES
    Author: Sentinel QA Team
#>

[CmdletBinding()]
param(
    [string]$ServerUrl = "http://localhost:3000",
    [string]$OutputPath = ".\TestResults",
    [string]$ValidJWT = "",
    [string]$ValidAPIKey = ""
)

$ErrorActionPreference = "Stop"
$script:Results = @{
    Component = "Server-AuthMiddleware"
    Tests = @()
}

function Test-Endpoint {
    param(
        [string]$Endpoint,
        [string]$Method = "GET",
        [hashtable]$Headers = @{},
        [string]$Body = "",
        [int]$ExpectedStatus,
        [string]$Description
    )

    $result = @{
        Name = $Description
        Endpoint = $Endpoint
        Method = $Method
        ExpectedStatus = $ExpectedStatus
        Status = "Unknown"
    }

    try {
        $params = @{
            Uri = "$ServerUrl$Endpoint"
            Method = $Method
            Headers = $Headers
            ContentType = "application/json"
            ErrorAction = "Stop"
        }

        if ($Body) {
            $params.Body = $Body
        }

        $response = Invoke-WebRequest @params

        if ($response.StatusCode -eq $ExpectedStatus) {
            $result.Status = "Passed"
            $result.ActualStatus = $response.StatusCode
            Write-Host "[PASS] $Description (Status: $($response.StatusCode))" -ForegroundColor Green
        }
        else {
            $result.Status = "Failed"
            $result.ActualStatus = $response.StatusCode
            $result.Error = "Expected $ExpectedStatus, got $($response.StatusCode)"
            Write-Host "[FAIL] $Description - Expected $ExpectedStatus, got $($response.StatusCode)" -ForegroundColor Red
        }
    }
    catch {
        $statusCode = $_.Exception.Response.StatusCode.value__

        if ($statusCode -eq $ExpectedStatus) {
            $result.Status = "Passed"
            $result.ActualStatus = $statusCode
            Write-Host "[PASS] $Description (Status: $statusCode)" -ForegroundColor Green
        }
        elseif ($ExpectedStatus -eq 401 -and $_.Exception.Message -match "401|Unauthorized") {
            $result.Status = "Passed"
            $result.ActualStatus = 401
            Write-Host "[PASS] $Description (Status: 401)" -ForegroundColor Green
        }
        elseif ($ExpectedStatus -eq 403 -and $_.Exception.Message -match "403|Forbidden") {
            $result.Status = "Passed"
            $result.ActualStatus = 403
            Write-Host "[PASS] $Description (Status: 403)" -ForegroundColor Green
        }
        else {
            $result.Status = "Failed"
            $result.Error = $_.Exception.Message
            Write-Host "[FAIL] $Description - $($_.Exception.Message)" -ForegroundColor Red
        }
    }

    $script:Results.Tests += $result
}

function Test-UnauthenticatedAccess {
    Write-Host "`n=== Unauthenticated Access Tests ===" -ForegroundColor Cyan

    # Public endpoints should work without auth
    Test-Endpoint -Endpoint "/api/health" -Method GET -ExpectedStatus 200 -Description "Health endpoint (public)"

    # Protected endpoints should require auth
    Test-Endpoint -Endpoint "/api/devices" -Method GET -ExpectedStatus 401 -Description "Devices endpoint without auth"
    Test-Endpoint -Endpoint "/api/users" -Method GET -ExpectedStatus 401 -Description "Users endpoint without auth"
    Test-Endpoint -Endpoint "/api/alerts" -Method GET -ExpectedStatus 401 -Description "Alerts endpoint without auth"
}

function Test-InvalidJWT {
    Write-Host "`n=== Invalid JWT Tests ===" -ForegroundColor Cyan

    # Malformed JWT
    Test-Endpoint -Endpoint "/api/devices" -Method GET -Headers @{ Authorization = "Bearer not.a.jwt" } -ExpectedStatus 401 -Description "Malformed JWT"

    # Expired JWT (example expired token)
    $expiredJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VySWQiOiIxMjM0NTY3ODkwIiwiZXhwIjoxfQ.abc123"
    Test-Endpoint -Endpoint "/api/devices" -Method GET -Headers @{ Authorization = "Bearer $expiredJWT" } -ExpectedStatus 401 -Description "Expired JWT"

    # JWT with wrong signature
    $wrongSigJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VySWQiOiJ0ZXN0Iiwicm9sZSI6ImFkbWluIn0.wrong_signature"
    Test-Endpoint -Endpoint "/api/devices" -Method GET -Headers @{ Authorization = "Bearer $wrongSigJWT" } -ExpectedStatus 401 -Description "JWT with invalid signature"

    # JWT with none algorithm (security test)
    $noneAlgJWT = "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJ1c2VySWQiOiJ0ZXN0Iiwicm9sZSI6ImFkbWluIn0."
    Test-Endpoint -Endpoint "/api/devices" -Method GET -Headers @{ Authorization = "Bearer $noneAlgJWT" } -ExpectedStatus 401 -Description "JWT with 'none' algorithm (should be rejected)"
}

function Test-InvalidAPIKey {
    Write-Host "`n=== Invalid API Key Tests ===" -ForegroundColor Cyan

    # Empty API key
    Test-Endpoint -Endpoint "/api/devices" -Method GET -Headers @{ "X-API-Key" = "" } -ExpectedStatus 401 -Description "Empty API key"

    # Random invalid API key
    Test-Endpoint -Endpoint "/api/devices" -Method GET -Headers @{ "X-API-Key" = "invalid-api-key" } -ExpectedStatus 401 -Description "Invalid API key"

    # SQL injection in API key
    Test-Endpoint -Endpoint "/api/devices" -Method GET -Headers @{ "X-API-Key" = "'; DROP TABLE users; --" } -ExpectedStatus 401 -Description "SQL injection in API key"
}

function Test-ValidAuthentication {
    Write-Host "`n=== Valid Authentication Tests ===" -ForegroundColor Cyan

    if ($ValidJWT) {
        Test-Endpoint -Endpoint "/api/devices" -Method GET -Headers @{ Authorization = "Bearer $ValidJWT" } -ExpectedStatus 200 -Description "Valid JWT authentication"
    }
    else {
        Write-Host "[SKIP] No valid JWT provided" -ForegroundColor Yellow
        $script:Results.Tests += @{
            Name = "Valid JWT authentication"
            Status = "Skipped"
            Details = @{ Message = "No valid JWT provided" }
        }
    }

    if ($ValidAPIKey) {
        Test-Endpoint -Endpoint "/api/devices" -Method GET -Headers @{ "X-API-Key" = $ValidAPIKey } -ExpectedStatus 200 -Description "Valid API key authentication"
    }
    else {
        Write-Host "[SKIP] No valid API key provided" -ForegroundColor Yellow
        $script:Results.Tests += @{
            Name = "Valid API key authentication"
            Status = "Skipped"
            Details = @{ Message = "No valid API key provided" }
        }
    }
}

function Test-AuthHeaderParsing {
    Write-Host "`n=== Auth Header Parsing Tests ===" -ForegroundColor Cyan

    # Missing Bearer prefix
    Test-Endpoint -Endpoint "/api/devices" -Method GET -Headers @{ Authorization = "some.jwt.token" } -ExpectedStatus 401 -Description "Missing Bearer prefix"

    # Wrong auth type
    Test-Endpoint -Endpoint "/api/devices" -Method GET -Headers @{ Authorization = "Basic dXNlcjpwYXNz" } -ExpectedStatus 401 -Description "Basic auth instead of Bearer"

    # Multiple spaces
    Test-Endpoint -Endpoint "/api/devices" -Method GET -Headers @{ Authorization = "Bearer  token  spaces" } -ExpectedStatus 401 -Description "Multiple spaces in header"
}

function Test-WebSocketAuth {
    Write-Host "`n=== WebSocket Auth Parameter Tests ===" -ForegroundColor Cyan

    # WebSocket endpoints accept token as query param
    Test-Endpoint -Endpoint "/api/ws?token=invalid" -Method GET -ExpectedStatus 401 -Description "Invalid token in query param"
}

# Main execution
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host "SENTINEL AUTH MIDDLEWARE TESTS" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan

Test-UnauthenticatedAccess
Test-InvalidJWT
Test-InvalidAPIKey
Test-ValidAuthentication
Test-AuthHeaderParsing
Test-WebSocketAuth

# Summary
$passed = ($script:Results.Tests | Where-Object { $_.Status -eq "Passed" }).Count
$failed = ($script:Results.Tests | Where-Object { $_.Status -eq "Failed" }).Count
$skipped = ($script:Results.Tests | Where-Object { $_.Status -eq "Skipped" }).Count

Write-Host "`n" + "=" * 60 -ForegroundColor Cyan
Write-Host "RESULTS SUMMARY" -ForegroundColor Cyan
Write-Host "  Passed: $passed" -ForegroundColor Green
Write-Host "  Failed: $failed" -ForegroundColor $(if ($failed -gt 0) { "Red" } else { "Green" })
Write-Host "  Skipped: $skipped" -ForegroundColor Yellow
Write-Host "=" * 60 -ForegroundColor Cyan

if ($failed -gt 0) {
    Write-Host "`nWARNING: Authentication issues detected!" -ForegroundColor Red
}

# Export results
if ($OutputPath) {
    if (-not (Test-Path $OutputPath)) {
        New-Item -ItemType Directory -Path $OutputPath -Force | Out-Null
    }
    $resultsFile = Join-Path $OutputPath "auth-middleware-results-$(Get-Date -Format 'yyyyMMdd-HHmmss').json"
    $script:Results | ConvertTo-Json -Depth 10 | Out-File $resultsFile
    Write-Host "Results exported to: $resultsFile"
}

return $script:Results
