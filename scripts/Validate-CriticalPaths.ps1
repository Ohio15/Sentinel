# Sentinel RMM - Critical Path Validation Script (PowerShell)
# Run this BEFORE every deployment to catch breaking changes
#
# Usage:
#   .\Validate-CriticalPaths.ps1                           # Test remote production
#   .\Validate-CriticalPaths.ps1 -BaseUrl "http://localhost:8090"  # Test local
#   .\Validate-CriticalPaths.ps1 -Verbose                  # With debug output

[CmdletBinding()]
param(
    [string]$BaseUrl = "https://sentinelrmm.us:4443",
    [int]$TimeoutSeconds = 10
)

$ErrorActionPreference = "Continue"

# Ignore SSL certificate errors (for self-signed certs)
if ($PSVersionTable.PSVersion.Major -lt 6) {
    # PowerShell 5.x - use .NET callback
    Add-Type @"
    using System.Net;
    using System.Security.Cryptography.X509Certificates;
    public class TrustAllCertsPolicy : ICertificatePolicy {
        public bool CheckValidationResult(ServicePoint srvPoint, X509Certificate certificate, WebRequest request, int certificateProblem) {
            return true;
        }
    }
"@ -ErrorAction SilentlyContinue
    [System.Net.ServicePointManager]::CertificatePolicy = New-Object TrustAllCertsPolicy
    [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.SecurityProtocolType]::Tls12
}

# Counters
$script:Passed = 0
$script:Failed = 0
$script:Warnings = 0

# Test results collection
$script:TestResults = @()

function Write-TestPass {
    param([string]$Message)
    Write-Host "[PASS] $Message" -ForegroundColor Green
    $script:Passed++
    $script:TestResults += @{ Status = "PASS"; Message = $Message }
}

function Write-TestFail {
    param([string]$Message, [string]$Details = "")
    Write-Host "[FAIL] $Message" -ForegroundColor Red
    if ($Details) {
        Write-Host "       $Details" -ForegroundColor DarkRed
    }
    $script:Failed++
    $script:TestResults += @{ Status = "FAIL"; Message = $Message; Details = $Details }
}

function Write-TestWarn {
    param([string]$Message)
    Write-Host "[WARN] $Message" -ForegroundColor Yellow
    $script:Warnings++
    $script:TestResults += @{ Status = "WARN"; Message = $Message }
}

function Write-TestInfo {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Cyan
}

function Invoke-TestRequest {
    param(
        [string]$Method = "GET",
        [string]$Endpoint,
        [hashtable]$Body = $null,
        [hashtable]$Headers = @{}
    )

    $url = "$BaseUrl$Endpoint"
    Write-Verbose "Request: $Method $url"

    try {
        $params = @{
            Uri = $url
            Method = $Method
            TimeoutSec = $TimeoutSeconds
            ErrorAction = "Stop"
            UseBasicParsing = $true
        }

        # PowerShell 6+ supports SkipCertificateCheck
        if ($PSVersionTable.PSVersion.Major -ge 6) {
            $params.SkipCertificateCheck = $true
        }

        if ($Body) {
            $params.Body = ($Body | ConvertTo-Json)
            $params.ContentType = "application/json"
        }

        if ($Headers.Count -gt 0) {
            $params.Headers = $Headers
        }

        $response = Invoke-WebRequest @params

        return @{
            StatusCode = [int]$response.StatusCode
            Body = $response.Content
            Success = $true
        }
    }
    catch {
        $statusCode = 0
        $responseBody = ""

        if ($_.Exception.Response) {
            try {
                $statusCode = [int]$_.Exception.Response.StatusCode
            } catch {
                # Try parsing from status description
                if ($_.Exception.Response.StatusCode) {
                    $statusCode = [int]([System.Net.HttpStatusCode]::($_.Exception.Response.StatusCode.ToString()))
                }
            }
            try {
                $stream = $_.Exception.Response.GetResponseStream()
                if ($stream) {
                    $reader = New-Object System.IO.StreamReader($stream)
                    $responseBody = $reader.ReadToEnd()
                    $reader.Close()
                }
            } catch { }
        }

        Write-Verbose "Error: Status=$statusCode Body=$responseBody Exception=$($_.Exception.Message)"

        return @{
            StatusCode = $statusCode
            Body = $responseBody
            Success = $false
            Error = $_.Exception.Message
        }
    }
}

# ==============================================================
# HEADER
# ==============================================================
Write-Host ""
Write-Host "===========================================================" -ForegroundColor Cyan
Write-Host "  SENTINEL RMM - CRITICAL PATH VALIDATION" -ForegroundColor Cyan
Write-Host "===========================================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Target: $BaseUrl"
Write-Host "  Time:   $(Get-Date)"
Write-Host ""
Write-Host "===========================================================" -ForegroundColor Cyan

# ==============================================================
# PHASE 1: Health Endpoints
# ==============================================================
Write-Host ""
Write-Host "[PHASE 1] Health Endpoints" -ForegroundColor Yellow
Write-Host "-----------------------------------------------------------"

# Test /health
$result = Invoke-TestRequest -Endpoint "/health"
if ($result.StatusCode -eq 200) {
    Write-TestPass "/health endpoint responding"
} else {
    Write-TestFail "/health endpoint failed (HTTP $($result.StatusCode))"
}

# Test /health/ready
$result = Invoke-TestRequest -Endpoint "/health/ready"
if ($result.StatusCode -eq 200) {
    Write-TestPass "/health/ready - Service ready"

    # Try to parse as JSON for detailed status
    try {
        $bodyObj = $result.Body | ConvertFrom-Json -ErrorAction Stop
        if ($null -ne $bodyObj.database) {
            if ($bodyObj.database -eq $true) {
                Write-TestPass "Database connection verified"
            } else {
                Write-TestFail "Database connection failed"
            }
        }
        if ($null -ne $bodyObj.redis) {
            if ($bodyObj.redis -eq $true) {
                Write-TestPass "Redis connection verified"
            } else {
                Write-TestFail "Redis connection failed"
            }
        }
    } catch {
        # Plain text response (older format) - just check for healthy status
        if ($result.Body -match "healthy|ready|ok") {
            Write-TestInfo "  Response: $($result.Body.Trim()) (plain text format)"
        }
    }
} else {
    Write-TestFail "/health/ready failed (HTTP $($result.StatusCode)) - DB or Redis may be down"
}

# Test /health/live
$result = Invoke-TestRequest -Endpoint "/health/live"
if ($result.StatusCode -eq 200) {
    Write-TestPass "/health/live - Service is alive"
} else {
    Write-TestFail "/health/live failed (HTTP $($result.StatusCode))"
}

# ==============================================================
# PHASE 2: Public API Endpoints
# ==============================================================
Write-Host ""
Write-Host "[PHASE 2] Public API Endpoints" -ForegroundColor Yellow
Write-Host "-----------------------------------------------------------"

# Test agent version endpoint
$result = Invoke-TestRequest -Endpoint "/api/agent/version"
if ($result.StatusCode -eq 200) {
    Write-TestPass "/api/agent/version - Agent version endpoint working"
    try {
        $versionObj = $result.Body | ConvertFrom-Json
        if ($versionObj.version) {
            Write-TestInfo "  Current agent version: $($versionObj.version)"
        }
    } catch { }
} else {
    Write-TestFail "/api/agent/version failed (HTTP $($result.StatusCode))"
}

# Test bootstrap endpoint
$result = Invoke-TestRequest -Endpoint "/api/bootstrap/agent-info"
if ($result.StatusCode -eq 200) {
    Write-TestPass "/api/bootstrap/agent-info - Bootstrap info available"
} else {
    Write-TestWarn "/api/bootstrap/agent-info returned $($result.StatusCode) (may be expected)"
}

# ==============================================================
# PHASE 3: Installation Code Flow (CRITICAL)
# ==============================================================
Write-Host ""
Write-Host "[PHASE 3] Installation Code Flow (CRITICAL)" -ForegroundColor Yellow
Write-Host "-----------------------------------------------------------"

# Test with invalid code
$result = Invoke-TestRequest -Endpoint "/api/public/install/validate-code?code=INVALID-CODE"
if ($result.StatusCode -eq 200) {
    $body = $result.Body
    if ($body -match '"valid"\s*:\s*false' -or $body -match '"status"\s*:\s*"invalid"') {
        Write-TestPass "Invalid code returns valid:false (correct behavior)"
    } else {
        Write-TestWarn "Invalid code response format unexpected: $body"
    }
} elseif ($result.StatusCode -in @(400, 404)) {
    Write-TestPass "Invalid code properly rejected (HTTP $($result.StatusCode))"
} elseif ($result.StatusCode -eq 500 -and $result.Body -match '"valid"\s*:\s*false') {
    # Known bug: server returns 500 but response is correct
    # This happens when pgx.ErrNoRows isn't being caught properly
    Write-TestFail "Installation code validation returns 500 (pgx error handling bug)" "Response body is correct, but HTTP 500 indicates pgx.ErrNoRows bug - deploy fix from installation_codes.go"
} else {
    Write-TestFail "Invalid code handling broken (HTTP $($result.StatusCode))" $result.Body
}

# Test with empty code
$result = Invoke-TestRequest -Endpoint "/api/public/install/validate-code?code="
if ($result.StatusCode -in @(200, 400, 404)) {
    Write-TestPass "Empty code handled gracefully (HTTP $($result.StatusCode))"
} else {
    Write-TestFail "Empty code caused error (HTTP $($result.StatusCode))"
}

# ==============================================================
# PHASE 4: Authentication Endpoints
# ==============================================================
Write-Host ""
Write-Host "[PHASE 4] Authentication Endpoints" -ForegroundColor Yellow
Write-Host "-----------------------------------------------------------"

# Test login endpoint
$result = Invoke-TestRequest -Method "POST" -Endpoint "/api/auth/login" -Body @{
    email = "test@test.com"
    password = "wrong"
}
if ($result.StatusCode -in @(400, 401)) {
    Write-TestPass "/api/auth/login - Endpoint reachable, rejects invalid credentials"
} elseif ($result.StatusCode -eq 429) {
    Write-TestWarn "/api/auth/login - Rate limited (expected in production)"
} else {
    Write-TestFail "/api/auth/login - Unexpected response (HTTP $($result.StatusCode))"
}

# Test register endpoint
$result = Invoke-TestRequest -Method "POST" -Endpoint "/api/auth/register" -Body @{
    email = ""
    password = ""
}
if ($result.StatusCode -in @(400, 401, 422)) {
    Write-TestPass "/api/auth/register - Endpoint reachable, validates input"
} elseif ($result.StatusCode -eq 429) {
    Write-TestWarn "/api/auth/register - Rate limited"
} else {
    Write-TestFail "/api/auth/register - Unexpected response (HTTP $($result.StatusCode))"
}

# ==============================================================
# PHASE 5: Agent Endpoints
# ==============================================================
Write-Host ""
Write-Host "[PHASE 5] Agent Endpoints" -ForegroundColor Yellow
Write-Host "-----------------------------------------------------------"

# Test enrollment endpoint
$result = Invoke-TestRequest -Method "POST" -Endpoint "/api/agent/enroll" -Body @{
    hostname = "test"
}
if ($result.StatusCode -in @(400, 401, 403)) {
    Write-TestPass "/api/agent/enroll - Requires valid enrollment token"
} else {
    Write-TestFail "/api/agent/enroll - Unexpected response (HTTP $($result.StatusCode))"
}

# Test download endpoint
$result = Invoke-TestRequest -Endpoint "/api/agents/download/windows/amd64"
if ($result.StatusCode -in @(200, 400, 401)) {
    Write-TestPass "/api/agents/download - Download endpoint exists"
} else {
    Write-TestWarn "/api/agents/download - Unexpected response (HTTP $($result.StatusCode))"
}

# ==============================================================
# PHASE 6: Protected Endpoint Security
# ==============================================================
Write-Host ""
Write-Host "[PHASE 6] Protected Endpoint Security" -ForegroundColor Yellow
Write-Host "-----------------------------------------------------------"

$protectedEndpoints = @(
    "/api/devices",
    "/api/alerts",
    "/api/scripts",
    "/api/users",
    "/api/clients"
)

# Note: /api/enrollment-tokens is intentionally public for self-hosted setups

foreach ($endpoint in $protectedEndpoints) {
    $result = Invoke-TestRequest -Endpoint $endpoint
    if ($result.StatusCode -in @(401, 403)) {
        Write-TestPass "$endpoint - Protected (requires auth)"
    } elseif ($result.StatusCode -eq 200) {
        Write-TestFail "$endpoint - NOT PROTECTED! Accessible without auth!"
    } else {
        Write-TestWarn "$endpoint - Unexpected status (HTTP $($result.StatusCode))"
    }
}

# ==============================================================
# PHASE 7: WebSocket Endpoints
# ==============================================================
Write-Host ""
Write-Host "[PHASE 7] WebSocket Endpoints" -ForegroundColor Yellow
Write-Host "-----------------------------------------------------------"

# WebSocket endpoints (basic reachability test)
$result = Invoke-TestRequest -Endpoint "/ws/agent" -Headers @{
    "Upgrade" = "websocket"
    "Connection" = "Upgrade"
}
if ($result.StatusCode -in @(400, 426, 101)) {
    Write-TestPass "/ws/agent - WebSocket endpoint exists"
} else {
    Write-TestWarn "/ws/agent - Unexpected response (HTTP $($result.StatusCode))"
}

$result = Invoke-TestRequest -Endpoint "/ws/dashboard" -Headers @{
    "Upgrade" = "websocket"
    "Connection" = "Upgrade"
}
if ($result.StatusCode -in @(400, 401, 426)) {
    Write-TestPass "/ws/dashboard - WebSocket endpoint exists (requires auth)"
} else {
    Write-TestWarn "/ws/dashboard - Unexpected response (HTTP $($result.StatusCode))"
}

# ==============================================================
# PHASE 8: Error Handling
# ==============================================================
Write-Host ""
Write-Host "[PHASE 8] Error Handling" -ForegroundColor Yellow
Write-Host "-----------------------------------------------------------"

# Test non-existent resource
$result = Invoke-TestRequest -Endpoint "/api/devices/99999999"
if ($result.StatusCode -in @(401, 404)) {
    Write-TestPass "Non-existent resource returns proper error"

    # Check for sensitive info leakage
    if ($result.Body -match "sql|postgres|pgx|database|connection") {
        Write-TestFail "Response may leak database implementation details"
    } else {
        Write-TestPass "No database implementation details leaked"
    }
} else {
    Write-TestWarn "Non-existent device returned HTTP $($result.StatusCode)"
}

# ==============================================================
# RESULTS
# ==============================================================
Write-Host ""
Write-Host "===========================================================" -ForegroundColor Cyan
Write-Host "  VALIDATION RESULTS" -ForegroundColor Cyan
Write-Host "===========================================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Passed:   $script:Passed" -ForegroundColor Green
Write-Host "  Failed:   $script:Failed" -ForegroundColor Red
Write-Host "  Warnings: $script:Warnings" -ForegroundColor Yellow
Write-Host ""

if ($script:Failed -eq 0) {
    Write-Host "===========================================================" -ForegroundColor Green
    Write-Host "  ALL CRITICAL TESTS PASSED - Safe to deploy" -ForegroundColor Green
    Write-Host "===========================================================" -ForegroundColor Green
    exit 0
} else {
    Write-Host "===========================================================" -ForegroundColor Red
    Write-Host "  CRITICAL TESTS FAILED - DO NOT DEPLOY" -ForegroundColor Red
    Write-Host "===========================================================" -ForegroundColor Red
    Write-Host ""
    Write-Host "Failed tests:" -ForegroundColor Red
    $script:TestResults | Where-Object { $_.Status -eq "FAIL" } | ForEach-Object {
        Write-Host "  - $($_.Message)" -ForegroundColor Red
        if ($_.Details) {
            Write-Host "    $($_.Details)" -ForegroundColor DarkRed
        }
    }
    exit 1
}
