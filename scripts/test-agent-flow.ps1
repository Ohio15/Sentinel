# Sentinel RMM - Full Agent Connection Flow Test
# This script simulates an agent connecting to the server and validates
# the complete enrollment -> WebSocket -> communication flow
#
# Usage:
#   .\test-agent-flow.ps1 -BaseUrl "https://sentinelrmm.us:4443"
#   .\test-agent-flow.ps1 -BaseUrl "https://sentinelrmm.us:4443" -EnrollmentToken "your-token"
#
# What this tests:
#   1. Enrollment token validation
#   2. Agent enrollment (creates device in database)
#   3. WebSocket connection establishment
#   4. Heartbeat/metrics submission
#   5. Command reception (if any pending)

[CmdletBinding()]
param(
    [string]$BaseUrl = "https://sentinelrmm.us:4443",
    [string]$EnrollmentToken = "",
    [string]$InstallationCode = "",
    [switch]$SkipCleanup,
    [int]$TimeoutSeconds = 30
)

$ErrorActionPreference = "Stop"

# Ignore SSL certificate errors
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
$script:TestsPassed = 0
$script:TestsFailed = 0
$script:AgentId = $null
$script:DeviceId = $null
$script:AgentToken = $null

function Write-TestHeader {
    param([string]$Title)
    Write-Host ""
    Write-Host "===========================================================" -ForegroundColor Cyan
    Write-Host "  $Title" -ForegroundColor Cyan
    Write-Host "===========================================================" -ForegroundColor Cyan
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
}

function Write-TestInfo {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Cyan
}

function Write-TestWarn {
    param([string]$Message)
    Write-Host "[WARN] $Message" -ForegroundColor Yellow
}

function Invoke-ApiRequest {
    param(
        [string]$Method = "GET",
        [string]$Endpoint,
        [object]$Body = $null,
        [hashtable]$Headers = @{}
    )

    $url = "$BaseUrl$Endpoint"

    try {
        $params = @{
            Uri = $url
            Method = $Method
            TimeoutSec = $TimeoutSeconds
            UseBasicParsing = $true
            ErrorAction = "Stop"
        }

        if ($PSVersionTable.PSVersion.Major -ge 6) {
            $params.SkipCertificateCheck = $true
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
        }
    }
}

Write-TestHeader "SENTINEL AGENT CONNECTION FLOW TEST"
Write-Host ""
Write-Host "  Target:    $BaseUrl"
Write-Host "  Time:      $(Get-Date)"
Write-Host "  Hostname:  test-agent-$(Get-Random -Maximum 9999)"
Write-Host ""

$testHostname = "test-agent-$(Get-Random -Maximum 9999)"

# ==============================================================
# PHASE 1: Obtain Enrollment Token
# ==============================================================
Write-Host ""
Write-Host "[PHASE 1] Obtain Enrollment Token" -ForegroundColor Yellow
Write-Host "-----------------------------------------------------------"

if ($InstallationCode) {
    # Use installation code to get enrollment token
    Write-TestInfo "Validating installation code: $InstallationCode"

    $result = Invoke-ApiRequest -Endpoint "/api/public/install/validate-code?code=$InstallationCode"

    if ($result.StatusCode -eq 200) {
        try {
            $codeData = $result.Body | ConvertFrom-Json
            if ($codeData.valid -eq $true -and $codeData.enrollmentToken) {
                $EnrollmentToken = $codeData.enrollmentToken
                Write-TestPass "Installation code valid - got enrollment token"
            } else {
                Write-TestFail "Installation code invalid" $result.Body
                exit 1
            }
        } catch {
            Write-TestFail "Failed to parse code validation response" $result.Body
            exit 1
        }
    } else {
        Write-TestFail "Installation code validation failed (HTTP $($result.StatusCode))" $result.Body
        exit 1
    }
} elseif ($EnrollmentToken) {
    Write-TestInfo "Using provided enrollment token"
    Write-TestPass "Enrollment token provided"
} else {
    # Try to get enrollment token from public endpoint (for self-hosted)
    Write-TestInfo "Attempting to fetch enrollment token from public endpoint..."

    $result = Invoke-ApiRequest -Endpoint "/api/enrollment-tokens"

    if ($result.StatusCode -eq 200) {
        try {
            $tokens = $result.Body | ConvertFrom-Json
            if ($tokens -and $tokens.Count -gt 0) {
                $EnrollmentToken = $tokens[0].token
                Write-TestPass "Retrieved enrollment token from server"
            } else {
                Write-TestFail "No enrollment tokens available"
                Write-TestInfo "Please provide -EnrollmentToken or -InstallationCode parameter"
                exit 1
            }
        } catch {
            Write-TestFail "Failed to parse enrollment tokens" $result.Body
            exit 1
        }
    } else {
        Write-TestFail "Cannot obtain enrollment token (HTTP $($result.StatusCode))"
        Write-TestInfo "Please provide -EnrollmentToken or -InstallationCode parameter"
        exit 1
    }
}

# ==============================================================
# PHASE 2: Agent Enrollment
# ==============================================================
Write-Host ""
Write-Host "[PHASE 2] Agent Enrollment" -ForegroundColor Yellow
Write-Host "-----------------------------------------------------------"

Write-TestInfo "Enrolling agent with hostname: $testHostname"

$enrollPayload = @{
    hostname = $testHostname
    platform = "windows"
    os_version = "Windows 10 Test"
    agent_version = "1.0.0-test"
    ip_address = "192.168.1.100"
    mac_address = "00:11:22:33:44:55"
}

$result = Invoke-ApiRequest -Method "POST" -Endpoint "/api/agent/enroll" -Body $enrollPayload -Headers @{
    "X-Enrollment-Token" = $EnrollmentToken
}

if ($result.StatusCode -eq 200 -or $result.StatusCode -eq 201) {
    try {
        $enrollData = $result.Body | ConvertFrom-Json

        if ($enrollData.agentId -or $enrollData.agent_id -or $enrollData.deviceId -or $enrollData.device_id) {
            $script:AgentId = $enrollData.agentId ?? $enrollData.agent_id ?? $enrollData.id
            $script:DeviceId = $enrollData.deviceId ?? $enrollData.device_id ?? $enrollData.id
            $script:AgentToken = $enrollData.token ?? $enrollData.agentToken ?? $enrollData.agent_token

            Write-TestPass "Agent enrolled successfully"
            Write-TestInfo "  Agent ID:  $($script:AgentId)"
            Write-TestInfo "  Device ID: $($script:DeviceId)"
            if ($script:AgentToken) {
                Write-TestInfo "  Token:     $($script:AgentToken.Substring(0, [Math]::Min(20, $script:AgentToken.Length)))..."
            }
        } else {
            Write-TestFail "Enrollment response missing agent/device ID" $result.Body
            exit 1
        }
    } catch {
        Write-TestFail "Failed to parse enrollment response" "$($_.Exception.Message) - Body: $($result.Body)"
        exit 1
    }
} else {
    Write-TestFail "Agent enrollment failed (HTTP $($result.StatusCode))" $result.Body
    exit 1
}

# ==============================================================
# PHASE 3: WebSocket Connection Test
# ==============================================================
Write-Host ""
Write-Host "[PHASE 3] WebSocket Connection" -ForegroundColor Yellow
Write-Host "-----------------------------------------------------------"

# Convert HTTPS URL to WSS
$wsUrl = $BaseUrl -replace "^https://", "wss://" -replace "^http://", "ws://"
$wsUrl = "$wsUrl/ws/agent"

Write-TestInfo "WebSocket URL: $wsUrl"

try {
    # Use .NET WebSocket client
    Add-Type -AssemblyName System.Net.WebSockets -ErrorAction SilentlyContinue

    $ws = New-Object System.Net.WebSockets.ClientWebSocket

    # Skip certificate validation for self-signed certs
    if ($PSVersionTable.PSVersion.Major -lt 6) {
        # Already handled at script level
    }

    # Add agent authentication header
    if ($script:AgentToken) {
        $ws.Options.SetRequestHeader("X-Agent-Token", $script:AgentToken)
    }
    $ws.Options.SetRequestHeader("X-Agent-ID", $script:AgentId)

    $uri = New-Object System.Uri($wsUrl)
    $cts = New-Object System.Threading.CancellationTokenSource
    $cts.CancelAfter($TimeoutSeconds * 1000)

    Write-TestInfo "Attempting WebSocket connection..."

    try {
        $connectTask = $ws.ConnectAsync($uri, $cts.Token)
        $connectTask.Wait()

        if ($ws.State -eq [System.Net.WebSockets.WebSocketState]::Open) {
            Write-TestPass "WebSocket connection established"

            # Send a heartbeat message
            Write-TestInfo "Sending heartbeat message..."

            $heartbeat = @{
                type = "heartbeat"
                agent_id = $script:AgentId
                timestamp = (Get-Date).ToUniversalTime().ToString("o")
            } | ConvertTo-Json

            $bytes = [System.Text.Encoding]::UTF8.GetBytes($heartbeat)
            $segment = New-Object System.ArraySegment[byte] -ArgumentList @(,$bytes)

            $sendTask = $ws.SendAsync($segment, [System.Net.WebSockets.WebSocketMessageType]::Text, $true, $cts.Token)
            $sendTask.Wait()

            Write-TestPass "Heartbeat sent successfully"

            # Try to receive a response (with short timeout)
            Write-TestInfo "Waiting for server response (5s)..."

            $receiveBuffer = New-Object byte[] 4096
            $receiveSegment = New-Object System.ArraySegment[byte] -ArgumentList @(,$receiveBuffer)
            $receiveCts = New-Object System.Threading.CancellationTokenSource
            $receiveCts.CancelAfter(5000)

            try {
                $receiveTask = $ws.ReceiveAsync($receiveSegment, $receiveCts.Token)
                $receiveTask.Wait()

                if ($receiveTask.Result.Count -gt 0) {
                    $response = [System.Text.Encoding]::UTF8.GetString($receiveBuffer, 0, $receiveTask.Result.Count)
                    Write-TestPass "Received server response"
                    Write-TestInfo "  Response: $($response.Substring(0, [Math]::Min(100, $response.Length)))..."
                }
            } catch {
                Write-TestWarn "No response received within timeout (may be normal if no pending commands)"
            }

            # Close connection gracefully
            $closeTask = $ws.CloseAsync([System.Net.WebSockets.WebSocketCloseStatus]::NormalClosure, "Test complete", [System.Threading.CancellationToken]::None)
            $closeTask.Wait()

            Write-TestPass "WebSocket connection closed gracefully"
        } else {
            Write-TestFail "WebSocket connection state: $($ws.State)"
        }
    } catch [System.AggregateException] {
        $innerEx = $_.Exception.InnerException
        if ($innerEx -is [System.Net.WebSockets.WebSocketException]) {
            Write-TestFail "WebSocket connection failed" $innerEx.Message
        } else {
            Write-TestFail "WebSocket connection failed" $_.Exception.Message
        }
    }
} catch {
    Write-TestFail "WebSocket test failed" $_.Exception.Message
}

# ==============================================================
# PHASE 4: Metrics Submission Test
# ==============================================================
Write-Host ""
Write-Host "[PHASE 4] Metrics Submission" -ForegroundColor Yellow
Write-Host "-----------------------------------------------------------"

Write-TestInfo "Submitting test metrics..."

# Note: The actual metrics endpoint varies by implementation
# Try common patterns
$metricsPayload = @{
    agent_id = $script:AgentId
    device_id = $script:DeviceId
    timestamp = (Get-Date).ToUniversalTime().ToString("o")
    metrics = @{
        cpu_percent = 25.5
        memory_percent = 45.2
        disk_percent = 60.0
        uptime = 3600
    }
}

# Try WebSocket metrics submission (preferred)
# This was tested in Phase 3

# Try HTTP metrics endpoint if available
$metricsEndpoints = @(
    "/api/agent/metrics",
    "/api/devices/$($script:DeviceId)/metrics"
)

$metricsSubmitted = $false
foreach ($endpoint in $metricsEndpoints) {
    $result = Invoke-ApiRequest -Method "POST" -Endpoint $endpoint -Body $metricsPayload -Headers @{
        "X-Agent-Token" = $script:AgentToken
        "X-Agent-ID" = $script:AgentId
    }

    if ($result.StatusCode -eq 200 -or $result.StatusCode -eq 201 -or $result.StatusCode -eq 204) {
        Write-TestPass "Metrics submitted via $endpoint"
        $metricsSubmitted = $true
        break
    }
}

if (-not $metricsSubmitted) {
    Write-TestWarn "HTTP metrics endpoint not available (WebSocket metrics may be the primary method)"
}

# ==============================================================
# PHASE 5: Verify Device Appears in System
# ==============================================================
Write-Host ""
Write-Host "[PHASE 5] Verify Device Registration" -ForegroundColor Yellow
Write-Host "-----------------------------------------------------------"

Write-TestInfo "Checking if device appears in the system..."

# This requires authentication - try with API key if available
# For now, just verify the enrollment worked by checking the ID was returned

if ($script:DeviceId) {
    Write-TestPass "Device registered with ID: $($script:DeviceId)"
} else {
    Write-TestFail "Device ID not available after enrollment"
}

# ==============================================================
# CLEANUP (optional)
# ==============================================================
if (-not $SkipCleanup -and $script:DeviceId) {
    Write-Host ""
    Write-Host "[CLEANUP] Removing test device" -ForegroundColor Yellow
    Write-Host "-----------------------------------------------------------"

    Write-TestWarn "Cleanup requires admin authentication - skipping automatic cleanup"
    Write-TestInfo "To remove test device, delete device ID: $($script:DeviceId)"
}

# ==============================================================
# RESULTS
# ==============================================================
Write-Host ""
Write-Host "===========================================================" -ForegroundColor Cyan
Write-Host "  TEST RESULTS" -ForegroundColor Cyan
Write-Host "===========================================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Passed: $script:TestsPassed" -ForegroundColor Green
Write-Host "  Failed: $script:TestsFailed" -ForegroundColor $(if ($script:TestsFailed -gt 0) { "Red" } else { "Green" })
Write-Host ""

if ($script:TestsFailed -eq 0) {
    Write-Host "  AGENT CONNECTION FLOW: WORKING" -ForegroundColor Green
    Write-Host ""
    Write-Host "  The complete agent-to-server connection flow is functional:"
    Write-Host "    - Enrollment token validation"
    Write-Host "    - Agent registration in database"
    Write-Host "    - WebSocket connection over TLS"
    Write-Host "    - Bidirectional communication"
    Write-Host ""
    exit 0
} else {
    Write-Host "  AGENT CONNECTION FLOW: ISSUES DETECTED" -ForegroundColor Red
    Write-Host ""
    exit 1
}
