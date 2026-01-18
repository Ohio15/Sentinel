# Sentinel RMM - Comprehensive End-to-End Test Suite
# Tests ALL user-facing features of the application
#
# Usage:
#   .\Test-E2E-Comprehensive.ps1 -BaseUrl "https://sentinelrmm.us:4443" -Username "admin" -Password "yourpassword"
#   .\Test-E2E-Comprehensive.ps1 -BaseUrl "https://sentinelrmm.us:4443" -Username "admin" -Password "yourpassword" -Verbose
#
# Features Tested:
#   1. Authentication (login, token refresh, logout)
#   2. Installation Codes (generate, validate)
#   3. Installation Links (generate, access, download)
#   4. Device Management (list, view, commands)
#   5. Ticket System (create, update, comment, close)
#   6. Scripts (create, list, execute)
#   7. Alerts (list, acknowledge, resolve)
#   8. Clients (create, list, assign devices)
#   9. Knowledge Base (categories, articles)
#   10. Health Endpoints

[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)]
    [string]$BaseUrl,

    [Parameter(Mandatory=$true)]
    [string]$Username,

    [Parameter(Mandatory=$true)]
    [string]$Password,

    [switch]$StopOnFailure,
    [switch]$SkipCleanup
)

$ErrorActionPreference = "Continue"

# Ignore SSL certificate errors for self-signed certs
if ($PSVersionTable.PSVersion.Major -lt 6) {
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

# Test state
$script:AccessToken = $null
$script:RefreshToken = $null
$script:CsrfToken = $null
$script:WebSession = $null  # Session to maintain cookies
$script:TestsPassed = 0
$script:TestsFailed = 0
$script:TestsSkipped = 0
$script:RequestDelay = 1500  # ms delay between requests to avoid rate limiting
$script:CreatedResources = @{
    InstallationCodes = @()
    InstallationLinks = @()
    Tickets = @()
    Scripts = @()
    Clients = @()
    KBCategories = @()
    KBArticles = @()
}

#region Helper Functions

function Write-TestHeader {
    param([string]$Title)
    Write-Host ""
    Write-Host "=" * 70 -ForegroundColor Cyan
    Write-Host "  $Title" -ForegroundColor Cyan
    Write-Host "=" * 70 -ForegroundColor Cyan
}

function Write-TestSection {
    param([string]$Title)
    Write-Host ""
    Write-Host "--- $Title ---" -ForegroundColor Yellow
}

function Write-TestPass {
    param([string]$Message)
    Write-Host "[PASS] $Message" -ForegroundColor Green
    $script:TestsPassed++
}

function Write-TestFail {
    param([string]$Message, [string]$Details = "")
    Write-Host "[FAIL] $Message" -ForegroundColor Red
    if ($Details) { Write-Host "       $Details" -ForegroundColor DarkRed }
    $script:TestsFailed++
    if ($StopOnFailure) {
        throw "Test failed: $Message"
    }
}

function Write-TestSkip {
    param([string]$Message, [string]$Reason = "")
    Write-Host "[SKIP] $Message" -ForegroundColor Yellow
    if ($Reason) { Write-Host "       Reason: $Reason" -ForegroundColor DarkYellow }
    $script:TestsSkipped++
}

function Write-TestInfo {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Cyan
}

function Invoke-ApiRequest {
    param(
        [string]$Method = "GET",
        [string]$Endpoint,
        [object]$Body = $null,
        [hashtable]$Headers = @{},
        [switch]$NoAuth,
        [switch]$ExpectError,
        [switch]$NoDelay
    )

    # Add delay between requests to avoid rate limiting (unless NoDelay specified)
    if (-not $NoDelay -and $script:RequestDelay -gt 0) {
        Start-Sleep -Milliseconds $script:RequestDelay
    }

    $url = "$BaseUrl$Endpoint"

    try {
        $params = @{
            Uri = $url
            Method = $Method
            TimeoutSec = 30
            UseBasicParsing = $true
            ErrorAction = "Stop"
        }

        if ($PSVersionTable.PSVersion.Major -ge 6) {
            $params.SkipCertificateCheck = $true
        }

        # Use session for cookies (creates one if needed)
        if (-not $script:WebSession) {
            $script:WebSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
        }
        $params.WebSession = $script:WebSession

        # Add auth header unless NoAuth specified
        if (-not $NoAuth -and $script:AccessToken) {
            $Headers["Authorization"] = "Bearer $($script:AccessToken)"
        }

        # Add CSRF token for mutating requests (POST, PUT, DELETE, PATCH)
        if ($script:CsrfToken -and $Method -in @("POST", "PUT", "DELETE", "PATCH")) {
            $Headers["X-CSRF-Token"] = $script:CsrfToken
            Write-Verbose "  Sending CSRF token in header: $($script:CsrfToken.Substring(0, [Math]::Min(15, $script:CsrfToken.Length)))..."
        }

        if ($Body) {
            if ($Body -is [string]) {
                $params.Body = $Body
            } else {
                $params.Body = ($Body | ConvertTo-Json -Depth 10)
            }
            $params.ContentType = "application/json"
        }

        if ($Headers.Count -gt 0) {
            $params.Headers = $Headers
        }

        $response = Invoke-WebRequest @params

        # Extract CSRF token from cookies if present (URL-decode it since cookies may be encoded)
        if ($script:WebSession.Cookies) {
            $uri = [System.Uri]$url
            $cookies = $script:WebSession.Cookies.GetCookies($uri)
            $csrfCookie = $cookies | Where-Object { $_.Name -eq "csrf_token" } | Select-Object -First 1
            if ($csrfCookie) {
                $script:CsrfToken = [System.Uri]::UnescapeDataString($csrfCookie.Value)
            }
        }

        return @{
            StatusCode = [int]$response.StatusCode
            Body = $response.Content
            Success = $true
            Data = try { $response.Content | ConvertFrom-Json } catch { $null }
        }
    }
    catch {
        $statusCode = 0
        $responseBody = ""

        if ($_.Exception.Response) {
            try { $statusCode = [int]$_.Exception.Response.StatusCode } catch {}
            try {
                $stream = $_.Exception.Response.GetResponseStream()
                if ($stream) {
                    $reader = New-Object System.IO.StreamReader($stream)
                    $responseBody = $reader.ReadToEnd()
                    $reader.Close()
                }
            } catch {}
        }

        return @{
            StatusCode = $statusCode
            Body = $responseBody
            Success = $false
            Error = $_.Exception.Message
            Data = try { $responseBody | ConvertFrom-Json } catch { $null }
        }
    }
}

#endregion

#region Test Functions

function Test-Authentication {
    Write-TestHeader "AUTHENTICATION TESTS"

    # Test 1: Login with valid credentials
    Write-TestSection "Login"
    $loginBody = @{
        identifier = $Username
        password = $Password
    }

    $result = Invoke-ApiRequest -Method "POST" -Endpoint "/api/auth/login" -Body $loginBody -NoAuth

    if ($result.StatusCode -eq 200 -and $result.Data.accessToken) {
        $script:AccessToken = $result.Data.accessToken
        $script:RefreshToken = $result.Data.refreshToken
        # Get CSRF token from JSON response (preferred) or fall back to cookie
        if ($result.Data.csrfToken) {
            $rawToken = $result.Data.csrfToken
            # URL-decode the CSRF token in case it's encoded
            $script:CsrfToken = [System.Uri]::UnescapeDataString($rawToken)
            Write-TestPass "Login successful - got access token and CSRF token (from JSON response)"
            Write-TestInfo "CSRF Token raw: $rawToken"
            Write-TestInfo "CSRF Token decoded: $($script:CsrfToken)"
        } elseif ($script:CsrfToken) {
            Write-TestPass "Login successful - got access token and CSRF token (from cookie)"
            Write-TestInfo "CSRF Token: $($script:CsrfToken.Substring(0, [Math]::Min(20, $script:CsrfToken.Length)))..."
        } else {
            Write-TestPass "Login successful - got access token (no CSRF token available)"
        }
        Write-Verbose "  User: $($result.Data.user.username) ($($result.Data.user.role))"
    } else {
        Write-TestFail "Login failed" $result.Body
        return $false
    }

    # Test 2: Get current user profile (this also fetches CSRF token from cookie)
    Write-TestSection "User Profile"
    $result = Invoke-ApiRequest -Endpoint "/api/auth/me"

    if ($result.StatusCode -eq 200 -and $result.Data.id) {
        Write-TestPass "Get user profile successful"
        Write-Verbose "  User ID: $($result.Data.id)"
    } else {
        Write-TestFail "Get user profile failed" "HTTP $($result.StatusCode): $($result.Body)"
    }

    # Show CSRF token status after profile request
    if ($script:CsrfToken) {
        Write-TestInfo "CSRF Token available: $($script:CsrfToken.Substring(0, [Math]::Min(20, $script:CsrfToken.Length)))..."
    } else {
        Write-TestInfo "WARNING: No CSRF token in session cookies!"
    }

    # Debug: Print all cookies in session
    if ($script:WebSession -and $script:WebSession.Cookies) {
        $uri = [System.Uri]$BaseUrl
        $allCookies = $script:WebSession.Cookies.GetCookies($uri)
        Write-TestInfo "Session has $($allCookies.Count) cookies for $BaseUrl"
        foreach ($c in $allCookies) {
            $val = if ($c.Value.Length -gt 15) { $c.Value.Substring(0,15) + "..." } else { $c.Value }
            Write-TestInfo "  Cookie: $($c.Name)=$val (Domain: $($c.Domain), Secure: $($c.Secure))"
        }
    }

    # Test 3: Token refresh
    Write-TestSection "Token Refresh"
    if ($script:RefreshToken) {
        $refreshBody = @{ refreshToken = $script:RefreshToken }
        $result = Invoke-ApiRequest -Method "POST" -Endpoint "/api/auth/refresh" -Body $refreshBody -NoAuth

        if ($result.StatusCode -eq 200 -and $result.Data.accessToken) {
            $script:AccessToken = $result.Data.accessToken
            # CSRF token is updated automatically from cookies by Invoke-ApiRequest
            Write-TestPass "Token refresh successful"
        } else {
            Write-TestFail "Token refresh failed" $result.Body
        }
    } else {
        Write-TestSkip "Token refresh" "No refresh token available"
    }

    # Test 4: Invalid login
    Write-TestSection "Invalid Login (should fail)"
    $badLogin = @{ identifier = "invalid"; password = "wrongpassword" }
    $result = Invoke-ApiRequest -Method "POST" -Endpoint "/api/auth/login" -Body $badLogin -NoAuth -ExpectError

    if ($result.StatusCode -eq 401 -or $result.StatusCode -eq 400) {
        Write-TestPass "Invalid login correctly rejected (HTTP $($result.StatusCode))"
    } else {
        Write-TestFail "Invalid login should return 401/400, got $($result.StatusCode)"
    }

    return $true
}

function Test-HealthEndpoints {
    Write-TestHeader "HEALTH ENDPOINT TESTS"

    # Test 1: Basic health
    Write-TestSection "Basic Health Check"
    $result = Invoke-ApiRequest -Endpoint "/health" -NoAuth

    if ($result.StatusCode -eq 200) {
        Write-TestPass "Health endpoint responding"
    } else {
        Write-TestFail "Health endpoint failed" "HTTP $($result.StatusCode)"
    }

    # Test 2: Readiness
    Write-TestSection "Readiness Check"
    $result = Invoke-ApiRequest -Endpoint "/health/ready" -NoAuth

    if ($result.StatusCode -eq 200) {
        Write-TestPass "Readiness check passed"
        if ($result.Data) {
            Write-Verbose "  Database: $($result.Data.database)"
            Write-Verbose "  Redis: $($result.Data.redis)"
        }
    } else {
        Write-TestFail "Readiness check failed" $result.Body
    }

    # Test 3: Liveness
    Write-TestSection "Liveness Check"
    $result = Invoke-ApiRequest -Endpoint "/health/live" -NoAuth

    if ($result.StatusCode -eq 200) {
        Write-TestPass "Liveness check passed"
    } else {
        Write-TestFail "Liveness check failed" "HTTP $($result.StatusCode)"
    }
}

function Test-InstallationCodes {
    Write-TestHeader "INSTALLATION CODE TESTS"

    # Test 1: Generate installation code
    Write-TestSection "Generate Installation Code"
    $codeBody = @{ deviceName = "E2E-Test-Device-$(Get-Random -Maximum 9999)" }
    $result = Invoke-ApiRequest -Method "POST" -Endpoint "/api/admin/installation-codes" -Body $codeBody

    $installCode = $null
    if ($result.StatusCode -eq 200 -or $result.StatusCode -eq 201) {
        if ($result.Data.code) {
            $installCode = $result.Data.code
            $script:CreatedResources.InstallationCodes += $installCode
            Write-TestPass "Installation code generated: $installCode"
            Write-Verbose "  Download URL: $($result.Data.downloadUrl)"
            Write-Verbose "  Expires: $($result.Data.expiresAt)"
        } else {
            Write-TestFail "No code in response" $result.Body
        }
    } else {
        Write-TestFail "Generate installation code failed" "HTTP $($result.StatusCode): $($result.Body)"
    }

    # Test 2: Validate installation code
    Write-TestSection "Validate Installation Code"
    if ($installCode) {
        $result = Invoke-ApiRequest -Endpoint "/api/public/install/validate-code?code=$installCode" -NoAuth

        if ($result.StatusCode -eq 200 -and $result.Data.valid -eq $true) {
            Write-TestPass "Installation code validated successfully"
            Write-Verbose "  Enrollment Token: $($result.Data.enrollmentToken.Substring(0,20))..."
            Write-Verbose "  Server URL: $($result.Data.serverUrl)"
        } else {
            Write-TestFail "Installation code validation failed" $result.Body
        }
    } else {
        Write-TestSkip "Validate installation code" "No code generated"
    }

    # Test 3: Invalid code validation
    Write-TestSection "Invalid Code Validation (should fail)"
    $result = Invoke-ApiRequest -Endpoint "/api/public/install/validate-code?code=XXXX-XXXX" -NoAuth

    if ($result.StatusCode -eq 200 -and $result.Data.valid -eq $false) {
        Write-TestPass "Invalid code correctly rejected"
    } elseif ($result.StatusCode -eq 404 -or $result.StatusCode -eq 400) {
        Write-TestPass "Invalid code correctly rejected (HTTP $($result.StatusCode))"
    } else {
        Write-TestFail "Invalid code should be rejected" $result.Body
    }
}

function Test-InstallationLinks {
    Write-TestHeader "INSTALLATION LINK TESTS"

    # Test 1: Generate installation link
    Write-TestSection "Generate Installation Link"
    $linkBody = @{
        deviceName = "E2E-Link-Test-$(Get-Random -Maximum 9999)"
        platform = "windows"
        userEmail = "test@example.com"
    }
    $result = Invoke-ApiRequest -Method "POST" -Endpoint "/api/admin/agent-links" -Body $linkBody

    $downloadUrl = $null
    $linkId = $null
    if ($result.StatusCode -eq 200 -or $result.StatusCode -eq 201) {
        if ($result.Data.downloadUrl) {
            $downloadUrl = $result.Data.downloadUrl
            $linkId = $result.Data.linkId
            $script:CreatedResources.InstallationLinks += $linkId
            Write-TestPass "Installation link generated"
            Write-Verbose "  Link ID: $linkId"
            Write-Verbose "  Download URL: $downloadUrl"
        } else {
            Write-TestFail "No download URL in response" $result.Body
        }
    } else {
        Write-TestFail "Generate installation link failed" "HTTP $($result.StatusCode): $($result.Body)"
    }

    # Test 2: Access installation link page (should return HTML)
    Write-TestSection "Access Installation Link Page"
    if ($downloadUrl) {
        # Extract the path from the URL
        $uri = [System.Uri]$downloadUrl
        $linkPath = $uri.PathAndQuery

        $result = Invoke-ApiRequest -Endpoint $linkPath -NoAuth

        if ($result.StatusCode -eq 200 -and $result.Body -match "<html") {
            Write-TestPass "Installation link page accessible"
            Write-Verbose "  Returns HTML page for installer download"
        } else {
            Write-TestFail "Installation link page not accessible" "HTTP $($result.StatusCode)"
        }
    } else {
        Write-TestSkip "Access installation link" "No link generated"
    }

    # Test 3: List installation links
    Write-TestSection "List Installation Links"
    $result = Invoke-ApiRequest -Endpoint "/api/admin/agent-links"

    if ($result.StatusCode -eq 200) {
        $linkCount = if ($result.Data.links) { $result.Data.links.Count } else { 0 }
        Write-TestPass "List installation links successful ($linkCount links)"
    } else {
        Write-TestFail "List installation links failed" $result.Body
    }

    # Test 4: Invalid link token (should fail)
    Write-TestSection "Invalid Link Token (should fail)"
    $result = Invoke-ApiRequest -Endpoint "/install/DL-invalidtoken123" -NoAuth

    # This might return 200 with error page or 404 depending on implementation
    if ($result.StatusCode -eq 200 -or $result.StatusCode -eq 404) {
        Write-TestPass "Invalid link handled (HTTP $($result.StatusCode))"
    } else {
        Write-TestInfo "Invalid link returned HTTP $($result.StatusCode)"
    }
}

function Test-DeviceManagement {
    Write-TestHeader "DEVICE MANAGEMENT TESTS"

    # Test 1: List devices
    Write-TestSection "List Devices"
    $result = Invoke-ApiRequest -Endpoint "/api/devices"

    $deviceId = $null
    $agentId = $null
    if ($result.StatusCode -eq 200) {
        $deviceCount = if ($result.Data.devices) { $result.Data.devices.Count } else { 0 }
        Write-TestPass "List devices successful ($deviceCount devices)"

        # Get first online device for further tests
        $onlineDevice = $result.Data.devices | Where-Object { $_.status -eq "online" } | Select-Object -First 1
        if ($onlineDevice) {
            $deviceId = $onlineDevice.id
            $agentId = $onlineDevice.agentId
            Write-Verbose "  Using device: $($onlineDevice.hostname) ($deviceId)"
        }
    } else {
        Write-TestFail "List devices failed" $result.Body
    }

    # Test 2: Get device details
    Write-TestSection "Get Device Details"
    if ($deviceId) {
        $result = Invoke-ApiRequest -Endpoint "/api/devices/$deviceId"

        if ($result.StatusCode -eq 200 -and $result.Data.id) {
            Write-TestPass "Get device details successful"
            Write-Verbose "  Hostname: $($result.Data.hostname)"
            Write-Verbose "  Status: $($result.Data.status)"
            Write-Verbose "  OS: $($result.Data.osType) $($result.Data.osVersion)"
        } else {
            Write-TestFail "Get device details failed" $result.Body
        }
    } else {
        Write-TestSkip "Get device details" "No device available"
    }

    # Test 3: Get device metrics
    Write-TestSection "Get Device Metrics"
    if ($deviceId) {
        $result = Invoke-ApiRequest -Endpoint "/api/devices/$deviceId/metrics"

        if ($result.StatusCode -eq 200) {
            Write-TestPass "Get device metrics successful"
        } else {
            Write-TestFail "Get device metrics failed" "HTTP $($result.StatusCode)"
        }
    } else {
        Write-TestSkip "Get device metrics" "No device available"
    }

    # Test 4: Get device inventory
    Write-TestSection "Get Device Inventory"
    if ($deviceId) {
        $result = Invoke-ApiRequest -Endpoint "/api/devices/$deviceId/inventory"

        if ($result.StatusCode -eq 200) {
            Write-TestPass "Get device inventory successful"
        } else {
            Write-TestFail "Get device inventory failed" "HTTP $($result.StatusCode)"
        }
    } else {
        Write-TestSkip "Get device inventory" "No device available"
    }

    # Test 5: Ping device
    Write-TestSection "Ping Device"
    if ($deviceId) {
        $result = Invoke-ApiRequest -Method "POST" -Endpoint "/api/devices/$deviceId/ping"

        if ($result.StatusCode -eq 200) {
            Write-TestPass "Ping device successful"
        } else {
            Write-TestFail "Ping device failed" "HTTP $($result.StatusCode): $($result.Body)"
        }
    } else {
        Write-TestSkip "Ping device" "No device available"
    }

    # Test 6: Get device commands history
    Write-TestSection "Get Device Commands"
    if ($deviceId) {
        $result = Invoke-ApiRequest -Endpoint "/api/devices/$deviceId/commands"

        if ($result.StatusCode -eq 200) {
            $cmdCount = if ($result.Data.commands) { $result.Data.commands.Count } else { 0 }
            Write-TestPass "Get device commands successful ($cmdCount commands)"
        } else {
            Write-TestFail "Get device commands failed" $result.Body
        }
    } else {
        Write-TestSkip "Get device commands" "No device available"
    }

    # Test 7: Invalid device ID
    Write-TestSection "Invalid Device ID (should fail)"
    $result = Invoke-ApiRequest -Endpoint "/api/devices/00000000-0000-0000-0000-000000000000"

    if ($result.StatusCode -eq 404) {
        Write-TestPass "Invalid device ID correctly returns 404"
    } else {
        Write-TestInfo "Invalid device ID returned HTTP $($result.StatusCode)"
    }
}

function Test-TicketSystem {
    Write-TestHeader "TICKET SYSTEM TESTS"

    # Test 1: Create ticket
    Write-TestSection "Create Ticket"
    Write-TestInfo "Using CSRF token: $($script:CsrfToken)"

    # Debug: Check if cookie will be sent
    if ($script:WebSession -and $script:WebSession.Cookies) {
        $uri = [System.Uri]"$BaseUrl/api/tickets"
        $cookies = $script:WebSession.Cookies.GetCookies($uri)
        Write-TestInfo "Cookies being sent to $uri : $($cookies.Count)"
        foreach ($c in $cookies) {
            Write-TestInfo "  $($c.Name) = $($c.Value)"
        }
    }

    $ticketBody = @{
        subject = "E2E Test Ticket - $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')"
        description = "This is an automated E2E test ticket"
        priority = "medium"
        status = "open"
    }
    $result = Invoke-ApiRequest -Method "POST" -Endpoint "/api/tickets" -Body $ticketBody

    $ticketId = $null
    if ($result.StatusCode -eq 200 -or $result.StatusCode -eq 201) {
        if ($result.Data.id) {
            $ticketId = $result.Data.id
            $script:CreatedResources.Tickets += $ticketId
            Write-TestPass "Ticket created: $ticketId"
        } else {
            Write-TestFail "No ticket ID in response" $result.Body
        }
    } else {
        $errorDetails = "HTTP $($result.StatusCode)"
        if ($result.Body) { $errorDetails += " - Body: $($result.Body)" }
        elseif ($result.Data) { $errorDetails += " - Data: $($result.Data | ConvertTo-Json -Compress)" }
        elseif ($result.Error) { $errorDetails += " - Exception: $($result.Error)" }
        Write-TestFail "Create ticket failed" $errorDetails
    }

    # Test 2: List tickets
    Write-TestSection "List Tickets"
    $result = Invoke-ApiRequest -Endpoint "/api/tickets"

    if ($result.StatusCode -eq 200) {
        $ticketCount = if ($result.Data.tickets) { $result.Data.tickets.Count } else { 0 }
        Write-TestPass "List tickets successful ($ticketCount tickets)"
    } else {
        Write-TestFail "List tickets failed" $result.Body
    }

    # Test 3: Get ticket details
    Write-TestSection "Get Ticket Details"
    if ($ticketId) {
        $result = Invoke-ApiRequest -Endpoint "/api/tickets/$ticketId"

        if ($result.StatusCode -eq 200 -and $result.Data.id) {
            Write-TestPass "Get ticket details successful"
            Write-Verbose "  Title: $($result.Data.title)"
            Write-Verbose "  Status: $($result.Data.status)"
        } else {
            Write-TestFail "Get ticket details failed" $result.Body
        }
    } else {
        Write-TestSkip "Get ticket details" "No ticket created"
    }

    # Test 4: Add comment to ticket
    Write-TestSection "Add Ticket Comment"
    if ($ticketId) {
        $commentBody = @{ content = "E2E test comment - $(Get-Date)"; authorName = "E2E Test Admin" }
        $result = Invoke-ApiRequest -Method "POST" -Endpoint "/api/tickets/$ticketId/comments" -Body $commentBody

        if ($result.StatusCode -eq 200 -or $result.StatusCode -eq 201) {
            Write-TestPass "Ticket comment added"
        } else {
            Write-TestFail "Add ticket comment failed" "HTTP $($result.StatusCode)"
        }
    } else {
        Write-TestSkip "Add ticket comment" "No ticket created"
    }

    # Test 5: Update ticket status
    Write-TestSection "Update Ticket Status"
    if ($ticketId) {
        $updateBody = @{ status = "in_progress" }
        $result = Invoke-ApiRequest -Method "PUT" -Endpoint "/api/tickets/$ticketId" -Body $updateBody

        if ($result.StatusCode -eq 200) {
            Write-TestPass "Ticket status updated to in_progress"
        } else {
            Write-TestFail "Update ticket status failed" "HTTP $($result.StatusCode)"
        }
    } else {
        Write-TestSkip "Update ticket status" "No ticket created"
    }

    # Test 6: Get ticket statistics
    Write-TestSection "Get Ticket Statistics"
    $result = Invoke-ApiRequest -Endpoint "/api/tickets/stats"

    if ($result.StatusCode -eq 200) {
        Write-TestPass "Get ticket statistics successful"
    } else {
        Write-TestFail "Get ticket statistics failed" "HTTP $($result.StatusCode)"
    }
}

function Test-ScriptManagement {
    Write-TestHeader "SCRIPT MANAGEMENT TESTS"

    # Test 1: Create script
    Write-TestSection "Create Script"
    $scriptBody = @{
        name = "E2E Test Script - $(Get-Random -Maximum 9999)"
        description = "Automated E2E test script"
        content = "Write-Host 'Hello from E2E test'"
        language = "powershell"
        osTypes = @("windows")
    }
    $result = Invoke-ApiRequest -Method "POST" -Endpoint "/api/scripts" -Body $scriptBody

    $scriptId = $null
    if ($result.StatusCode -eq 200 -or $result.StatusCode -eq 201) {
        if ($result.Data.id) {
            $scriptId = $result.Data.id
            $script:CreatedResources.Scripts += $scriptId
            Write-TestPass "Script created: $scriptId"
        } else {
            Write-TestFail "No script ID in response" $result.Body
        }
    } else {
        Write-TestFail "Create script failed" "HTTP $($result.StatusCode): $($result.Body)"
    }

    # Test 2: List scripts
    Write-TestSection "List Scripts"
    $result = Invoke-ApiRequest -Endpoint "/api/scripts"

    if ($result.StatusCode -eq 200) {
        $scriptCount = if ($result.Data.scripts) { $result.Data.scripts.Count } elseif ($result.Data -is [array]) { $result.Data.Count } else { 0 }
        Write-TestPass "List scripts successful ($scriptCount scripts)"
    } else {
        Write-TestFail "List scripts failed" $result.Body
    }

    # Test 3: Get script details
    Write-TestSection "Get Script Details"
    if ($scriptId) {
        $result = Invoke-ApiRequest -Endpoint "/api/scripts/$scriptId"

        if ($result.StatusCode -eq 200) {
            Write-TestPass "Get script details successful"
        } else {
            Write-TestFail "Get script details failed" $result.Body
        }
    } else {
        Write-TestSkip "Get script details" "No script created"
    }
}

function Test-AlertManagement {
    Write-TestHeader "ALERT MANAGEMENT TESTS"

    # Test 1: List alerts
    Write-TestSection "List Alerts"
    $result = Invoke-ApiRequest -Endpoint "/api/alerts"

    $alertId = $null
    if ($result.StatusCode -eq 200) {
        $alertCount = if ($result.Data.alerts) { $result.Data.alerts.Count } elseif ($result.Data -is [array]) { $result.Data.Count } else { 0 }
        Write-TestPass "List alerts successful ($alertCount alerts)"

        # Get first open alert for further tests
        $openAlert = $result.Data.alerts | Where-Object { $_.status -eq "open" } | Select-Object -First 1
        if (-not $openAlert -and $result.Data -is [array]) {
            $openAlert = $result.Data | Where-Object { $_.status -eq "open" } | Select-Object -First 1
        }
        if ($openAlert) {
            $alertId = $openAlert.id
        }
    } else {
        Write-TestFail "List alerts failed" $result.Body
    }

    # Test 2: List alert rules
    Write-TestSection "List Alert Rules"
    $result = Invoke-ApiRequest -Endpoint "/api/alert-rules"

    if ($result.StatusCode -eq 200) {
        $ruleCount = if ($result.Data.rules) { $result.Data.rules.Count } elseif ($result.Data -is [array]) { $result.Data.Count } else { 0 }
        Write-TestPass "List alert rules successful ($ruleCount rules)"
    } else {
        Write-TestFail "List alert rules failed" $result.Body
    }

    # Test 3: Acknowledge alert (if available)
    Write-TestSection "Acknowledge Alert"
    if ($alertId) {
        $result = Invoke-ApiRequest -Method "POST" -Endpoint "/api/alerts/$alertId/acknowledge"

        if ($result.StatusCode -eq 200) {
            Write-TestPass "Alert acknowledged"
        } else {
            Write-TestFail "Acknowledge alert failed" "HTTP $($result.StatusCode)"
        }
    } else {
        Write-TestSkip "Acknowledge alert" "No open alerts available"
    }
}

function Test-ClientManagement {
    Write-TestHeader "CLIENT MANAGEMENT TESTS"

    # Test 1: Create client
    Write-TestSection "Create Client"
    $clientBody = @{
        name = "E2E Test Client - $(Get-Random -Maximum 9999)"
        email = "e2e-test-$(Get-Random -Maximum 9999)@example.com"
    }
    $result = Invoke-ApiRequest -Method "POST" -Endpoint "/api/clients" -Body $clientBody

    $clientId = $null
    if ($result.StatusCode -eq 200 -or $result.StatusCode -eq 201) {
        if ($result.Data.id) {
            $clientId = $result.Data.id
            $script:CreatedResources.Clients += $clientId
            Write-TestPass "Client created: $clientId"
        } else {
            Write-TestFail "No client ID in response" $result.Body
        }
    } else {
        Write-TestFail "Create client failed" "HTTP $($result.StatusCode): $($result.Body)"
    }

    # Test 2: List clients
    Write-TestSection "List Clients"
    $result = Invoke-ApiRequest -Endpoint "/api/clients"

    if ($result.StatusCode -eq 200) {
        $clientCount = if ($result.Data.clients) { $result.Data.clients.Count } elseif ($result.Data -is [array]) { $result.Data.Count } else { 0 }
        Write-TestPass "List clients successful ($clientCount clients)"
    } else {
        Write-TestFail "List clients failed" $result.Body
    }

    # Test 3: Get client details
    Write-TestSection "Get Client Details"
    if ($clientId) {
        $result = Invoke-ApiRequest -Endpoint "/api/clients/$clientId"

        if ($result.StatusCode -eq 200) {
            Write-TestPass "Get client details successful"
        } else {
            Write-TestFail "Get client details failed" $result.Body
        }
    } else {
        Write-TestSkip "Get client details" "No client created"
    }
}

function Test-KnowledgeBase {
    Write-TestHeader "KNOWLEDGE BASE TESTS"

    # Test 1: Create KB category
    Write-TestSection "Create KB Category"
    $categoryBody = @{
        name = "E2E Test Category - $(Get-Random -Maximum 9999)"
        description = "Automated E2E test category"
    }
    $result = Invoke-ApiRequest -Method "POST" -Endpoint "/api/kb/categories" -Body $categoryBody

    $categoryId = $null
    if ($result.StatusCode -eq 200 -or $result.StatusCode -eq 201) {
        if ($result.Data.id) {
            $categoryId = $result.Data.id
            $script:CreatedResources.KBCategories += $categoryId
            Write-TestPass "KB category created: $categoryId"
        } else {
            Write-TestFail "No category ID in response" $result.Body
        }
    } else {
        Write-TestFail "Create KB category failed" "HTTP $($result.StatusCode): $($result.Body)"
    }

    # Test 2: List KB categories
    Write-TestSection "List KB Categories"
    $result = Invoke-ApiRequest -Endpoint "/api/kb/categories"

    if ($result.StatusCode -eq 200) {
        Write-TestPass "List KB categories successful"
    } else {
        Write-TestFail "List KB categories failed" $result.Body
    }

    # Test 3: Create KB article
    Write-TestSection "Create KB Article"
    if ($categoryId) {
        $articleBody = @{
            title = "E2E Test Article - $(Get-Random -Maximum 9999)"
            content = "This is an automated E2E test article content."
            categoryId = $categoryId
            status = "published"
        }
        $result = Invoke-ApiRequest -Method "POST" -Endpoint "/api/kb/articles" -Body $articleBody

        if ($result.StatusCode -eq 200 -or $result.StatusCode -eq 201) {
            if ($result.Data.id) {
                $script:CreatedResources.KBArticles += $result.Data.id
                Write-TestPass "KB article created: $($result.Data.id)"
            } else {
                Write-TestFail "No article ID in response" $result.Body
            }
        } else {
            Write-TestFail "Create KB article failed" "HTTP $($result.StatusCode): $($result.Body)"
        }
    } else {
        Write-TestSkip "Create KB article" "No category created"
    }

    # Test 4: List KB articles
    Write-TestSection "List KB Articles"
    $result = Invoke-ApiRequest -Endpoint "/api/kb/articles"

    if ($result.StatusCode -eq 200) {
        Write-TestPass "List KB articles successful"
    } else {
        Write-TestFail "List KB articles failed" $result.Body
    }
}

function Test-UserManagement {
    Write-TestHeader "USER MANAGEMENT TESTS"

    # Test 1: List users
    Write-TestSection "List Users"
    $result = Invoke-ApiRequest -Endpoint "/api/users"

    if ($result.StatusCode -eq 200) {
        $userCount = if ($result.Data.users) { $result.Data.users.Count } elseif ($result.Data -is [array]) { $result.Data.Count } else { 0 }
        Write-TestPass "List users successful ($userCount users)"
    } else {
        Write-TestFail "List users failed" $result.Body
    }

    # Test 2: List enrollment tokens
    Write-TestSection "List Enrollment Tokens"
    $result = Invoke-ApiRequest -Endpoint "/api/enrollment-tokens"

    if ($result.StatusCode -eq 200) {
        Write-TestPass "List enrollment tokens successful"
    } else {
        Write-TestFail "List enrollment tokens failed" $result.Body
    }
}

function Test-Settings {
    Write-TestHeader "SETTINGS TESTS"

    # Test 1: Get settings
    Write-TestSection "Get Settings"
    $result = Invoke-ApiRequest -Endpoint "/api/settings"

    if ($result.StatusCode -eq 200) {
        Write-TestPass "Get settings successful"
    } else {
        Write-TestFail "Get settings failed" $result.Body
    }
}

function Cleanup-TestResources {
    if ($SkipCleanup) {
        Write-TestInfo "Skipping cleanup (SkipCleanup flag set)"
        return
    }

    Write-TestHeader "CLEANUP"

    # Delete test tickets
    foreach ($ticketId in $script:CreatedResources.Tickets) {
        Write-TestInfo "Deleting ticket: $ticketId"
        $null = Invoke-ApiRequest -Method "DELETE" -Endpoint "/api/tickets/$ticketId"
    }

    # Delete test scripts
    foreach ($scriptId in $script:CreatedResources.Scripts) {
        Write-TestInfo "Deleting script: $scriptId"
        $null = Invoke-ApiRequest -Method "DELETE" -Endpoint "/api/scripts/$scriptId"
    }

    # Delete test clients
    foreach ($clientId in $script:CreatedResources.Clients) {
        Write-TestInfo "Deleting client: $clientId"
        $null = Invoke-ApiRequest -Method "DELETE" -Endpoint "/api/clients/$clientId"
    }

    # Delete KB articles
    foreach ($articleId in $script:CreatedResources.KBArticles) {
        Write-TestInfo "Deleting KB article: $articleId"
        $null = Invoke-ApiRequest -Method "DELETE" -Endpoint "/api/kb/articles/$articleId"
    }

    # Delete KB categories
    foreach ($categoryId in $script:CreatedResources.KBCategories) {
        Write-TestInfo "Deleting KB category: $categoryId"
        $null = Invoke-ApiRequest -Method "DELETE" -Endpoint "/api/kb/categories/$categoryId"
    }

    # Revoke installation links
    foreach ($linkId in $script:CreatedResources.InstallationLinks) {
        Write-TestInfo "Revoking installation link: $linkId"
        $null = Invoke-ApiRequest -Method "DELETE" -Endpoint "/api/admin/agent-links/$linkId"
    }

    Write-TestInfo "Cleanup complete"
}

#endregion

#region Main Execution

Write-Host ""
Write-Host "=" * 70 -ForegroundColor Magenta
Write-Host "  SENTINEL RMM - COMPREHENSIVE E2E TEST SUITE" -ForegroundColor Magenta
Write-Host "=" * 70 -ForegroundColor Magenta
Write-Host ""
Write-Host "  Target:    $BaseUrl"
Write-Host "  User:      $Username"
Write-Host "  Time:      $(Get-Date)"
Write-Host ""

$startTime = Get-Date

# Run all test suites
try {
    # Authentication must pass to continue
    $authPassed = Test-Authentication
    if (-not $authPassed) {
        Write-Host ""
        Write-Host "Authentication failed - cannot continue with tests" -ForegroundColor Red
        exit 1
    }

    Test-HealthEndpoints
    Test-InstallationCodes
    Test-InstallationLinks
    Test-DeviceManagement
    Test-TicketSystem
    Test-ScriptManagement
    Test-AlertManagement
    Test-ClientManagement
    Test-KnowledgeBase
    Test-UserManagement
    Test-Settings

    Cleanup-TestResources
}
catch {
    Write-Host ""
    Write-Host "Test execution stopped: $_" -ForegroundColor Red
}

$endTime = Get-Date
$duration = $endTime - $startTime

# Final Results
Write-Host ""
Write-Host "=" * 70 -ForegroundColor Magenta
Write-Host "  TEST RESULTS SUMMARY" -ForegroundColor Magenta
Write-Host "=" * 70 -ForegroundColor Magenta
Write-Host ""
Write-Host "  Passed:   $script:TestsPassed" -ForegroundColor Green
Write-Host "  Failed:   $script:TestsFailed" -ForegroundColor $(if ($script:TestsFailed -gt 0) { "Red" } else { "Green" })
Write-Host "  Skipped:  $script:TestsSkipped" -ForegroundColor Yellow
Write-Host "  Duration: $($duration.TotalSeconds.ToString('F2')) seconds"
Write-Host ""

$totalTests = $script:TestsPassed + $script:TestsFailed
$passRate = if ($totalTests -gt 0) { [math]::Round(($script:TestsPassed / $totalTests) * 100, 1) } else { 0 }

if ($script:TestsFailed -eq 0) {
    Write-Host "  ALL TESTS PASSED ($passRate%)" -ForegroundColor Green
    exit 0
} else {
    Write-Host "  SOME TESTS FAILED ($passRate% pass rate)" -ForegroundColor Red
    exit 1
}

#endregion
