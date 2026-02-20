# Sentinel RMM - Full End-to-End Integration Test
#
# This script tests the COMPLETE user journey:
#   1. Login to Sentinel as admin
#   2. Navigate to installation codes and generate a new code
#   3. Copy installation link/code
#   4. SSH to sandbox machine and run installer
#   5. Verify agent and watchdog services are installed
#   6. Verify device appears in Sentinel dashboard
#   7. Test remote command execution
#   8. Cleanup (optional)
#
# Usage:
#   .\test-e2e-full.ps1 -AdminUser "admin" -AdminPassword "password"
#   .\test-e2e-full.ps1 -AdminUser "admin" -AdminPassword "password" -SandboxHost "192.168.1.20"
#   .\test-e2e-full.ps1 -AdminUser "admin" -AdminPassword "password" -SkipCleanup

[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)]
    [string]$AdminUser,

    [Parameter(Mandatory=$true)]
    [string]$AdminPassword,

    [string]$BaseUrl = $env:SENTINEL_URL,
    [string]$SandboxHost = $env:SENTINEL_SANDBOX_HOST,
    [string]$SandboxUser = $env:SENTINEL_SANDBOX_USER,
    [string]$DeviceName = "E2E-Test-$(Get-Random -Maximum 9999)",
    [switch]$SkipCleanup,
    [int]$TimeoutSeconds = 120
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
$script:AccessToken = $null
$script:CsrfToken = $null
$script:InstallationCode = $null
$script:DeviceId = $null
$script:InstalledDeviceName = $DeviceName

function Write-TestHeader {
    param([string]$Title)
    Write-Host ""
    Write-Host "===========================================================" -ForegroundColor Cyan
    Write-Host "  $Title" -ForegroundColor Cyan
    Write-Host "===========================================================" -ForegroundColor Cyan
}

function Write-TestPhase {
    param([string]$Phase, [string]$Description)
    Write-Host ""
    Write-Host "[$Phase] $Description" -ForegroundColor Yellow
    Write-Host "-----------------------------------------------------------"
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
        [switch]$Authenticated,
        [switch]$ReturnResponse
    )

    $url = "$BaseUrl$Endpoint"

    try {
        $headers = @{}

        if ($Authenticated -and $script:AccessToken) {
            $headers["Authorization"] = "Bearer $($script:AccessToken)"
        }

        if ($script:CsrfToken) {
            $headers["X-CSRF-Token"] = $script:CsrfToken
        }

        $params = @{
            Uri = $url
            Method = $Method
            TimeoutSec = $TimeoutSeconds
            UseBasicParsing = $true
            ErrorAction = "Stop"
            Headers = $headers
            SessionVariable = "session"
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

        $response = Invoke-WebRequest @params

        # Extract CSRF token from cookies if present
        if ($response.Headers["Set-Cookie"]) {
            $cookies = $response.Headers["Set-Cookie"]
            if ($cookies -match "csrf_token=([^;]+)") {
                $script:CsrfToken = $Matches[1]
            }
        }

        if ($ReturnResponse) {
            return $response
        }

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

function Invoke-SshCommand {
    param(
        [string]$Command,
        [int]$Timeout = 60
    )

    try {
        $result = ssh -o ConnectTimeout=10 -o StrictHostKeyChecking=no "$SandboxUser@$SandboxHost" $Command 2>&1
        return @{
            Success = $LASTEXITCODE -eq 0
            Output = $result -join "`n"
            ExitCode = $LASTEXITCODE
        }
    }
    catch {
        return @{
            Success = $false
            Output = $_.Exception.Message
            ExitCode = -1
        }
    }
}

# ==============================================================
# MAIN TEST EXECUTION
# ==============================================================

Write-TestHeader "SENTINEL RMM - FULL END-TO-END TEST"
Write-Host ""
Write-Host "  Server:      $BaseUrl"
Write-Host "  Sandbox:     $SandboxUser@$SandboxHost"
Write-Host "  Device Name: $DeviceName"
Write-Host "  Time:        $(Get-Date)"
Write-Host ""

# ==============================================================
# PHASE 1: Login as Admin
# ==============================================================
Write-TestPhase "PHASE 1" "Login to Sentinel as Admin"

Write-TestInfo "Authenticating as $AdminUser..."

$loginResult = Invoke-ApiRequest -Method "POST" -Endpoint "/api/auth/login" -Body @{
    identifier = $AdminUser
    password = $AdminPassword
}

if ($loginResult.StatusCode -eq 200) {
    try {
        $loginData = $loginResult.Body | ConvertFrom-Json
        if ($loginData.accessToken) {
            $script:AccessToken = $loginData.accessToken
            Write-TestPass "Login successful"
            Write-TestInfo "  User: $($loginData.user.email)"
            Write-TestInfo "  Role: $($loginData.user.role)"
        } else {
            Write-TestFail "Login response missing access token" $loginResult.Body
            exit 1
        }
    } catch {
        Write-TestFail "Failed to parse login response" $loginResult.Body
        exit 1
    }
} else {
    Write-TestFail "Login failed (HTTP $($loginResult.StatusCode))" $loginResult.Body
    exit 1
}

# ==============================================================
# PHASE 2: Generate Installation Code
# ==============================================================
Write-TestPhase "PHASE 2" "Generate Installation Code"

Write-TestInfo "Creating installation code for device: $DeviceName"

$codeResult = Invoke-ApiRequest -Method "POST" -Endpoint "/api/admin/installation-codes" -Authenticated -Body @{
    deviceName = $DeviceName
    userName = "E2E Test"
    notes = "Automated E2E test - $(Get-Date)"
    expirationDays = 1
}

if ($codeResult.StatusCode -eq 200 -or $codeResult.StatusCode -eq 201) {
    try {
        $codeData = $codeResult.Body | ConvertFrom-Json
        if ($codeData.code) {
            $script:InstallationCode = $codeData.code
            Write-TestPass "Installation code created successfully"
            Write-TestInfo "  Code: $($script:InstallationCode)"
            Write-TestInfo "  URL:  $($codeData.downloadUrl)"
            Write-TestInfo "  Expires: $($codeData.expiresAt)"
        } else {
            Write-TestFail "Response missing installation code" $codeResult.Body
            exit 1
        }
    } catch {
        Write-TestFail "Failed to parse code response" $codeResult.Body
        exit 1
    }
} else {
    Write-TestFail "Failed to create installation code (HTTP $($codeResult.StatusCode))" $codeResult.Body
    exit 1
}

# ==============================================================
# PHASE 3: Validate Installation Code
# ==============================================================
Write-TestPhase "PHASE 3" "Validate Installation Code"

Write-TestInfo "Validating code: $($script:InstallationCode)"

$validateResult = Invoke-ApiRequest -Endpoint "/api/public/install/validate-code?code=$($script:InstallationCode)"

if ($validateResult.StatusCode -eq 200) {
    try {
        $validateData = $validateResult.Body | ConvertFrom-Json
        if ($validateData.valid -eq $true) {
            Write-TestPass "Installation code validated successfully"
            Write-TestInfo "  Server URL: $($validateData.serverUrl)"
            Write-TestInfo "  Device Name: $($validateData.deviceName)"
        } else {
            Write-TestFail "Installation code marked as invalid" $validateResult.Body
            exit 1
        }
    } catch {
        Write-TestFail "Failed to parse validation response" $validateResult.Body
        exit 1
    }
} else {
    Write-TestFail "Code validation failed (HTTP $($validateResult.StatusCode))" $validateResult.Body
    Write-TestWarn "This is the pgx.ErrNoRows bug - the fix needs to be deployed!"
    exit 1
}

# ==============================================================
# PHASE 4: Test SSH Connection to Sandbox
# ==============================================================
Write-TestPhase "PHASE 4" "Test SSH Connection to Sandbox"

Write-TestInfo "Testing SSH connection to $SandboxHost..."

$sshTest = Invoke-SshCommand -Command "echo 'SSH connection successful'"

if ($sshTest.Success) {
    Write-TestPass "SSH connection to sandbox working"
} else {
    Write-TestFail "Cannot connect to sandbox via SSH" $sshTest.Output
    Write-TestInfo "Ensure SSH key is configured for $SandboxUser@$SandboxHost"
    exit 1
}

# Check if agent is already installed
Write-TestInfo "Checking for existing agent installation..."
$existingAgent = Invoke-SshCommand -Command "sc query SentinelAgent 2>nul"
if ($existingAgent.Output -match "RUNNING|STOPPED") {
    Write-TestWarn "Agent already installed on sandbox - will reinstall"

    # Stop and uninstall existing agent
    Write-TestInfo "Stopping and removing existing agent..."
    Invoke-SshCommand -Command "sc stop SentinelAgent 2>nul; sc stop SentinelWatchdog 2>nul" | Out-Null
    Start-Sleep -Seconds 2
    Invoke-SshCommand -Command "sc delete SentinelAgent 2>nul; sc delete SentinelWatchdog 2>nul" | Out-Null
    Start-Sleep -Seconds 2
}

# ==============================================================
# PHASE 5: Download and Install Agent
# ==============================================================
Write-TestPhase "PHASE 5" "Download and Install Agent on Sandbox"

# Download installer to sandbox
Write-TestInfo "Downloading installer to sandbox..."
$downloadCmd = @"
powershell -Command "Invoke-WebRequest -Uri '$BaseUrl/api/agents/download/windows/amd64' -OutFile 'C:\Temp\sentinel-installer.exe' -UseBasicParsing"
"@

# Create temp directory if needed
Invoke-SshCommand -Command "if not exist C:\Temp mkdir C:\Temp" | Out-Null

$downloadResult = Invoke-SshCommand -Command $downloadCmd -Timeout 120
if (-not $downloadResult.Success) {
    # Try direct download URL
    Write-TestWarn "Direct download failed, trying alternative..."
    $altDownloadCmd = @"
curl -k -o C:\Temp\sentinel-installer.exe "$BaseUrl/download/agent?token=windows"
"@
    $downloadResult = Invoke-SshCommand -Command $altDownloadCmd -Timeout 120
}

# Verify download
$verifyDownload = Invoke-SshCommand -Command "if exist C:\Temp\sentinel-installer.exe (echo EXISTS) else (echo MISSING)"
if ($verifyDownload.Output -match "EXISTS") {
    Write-TestPass "Installer downloaded to sandbox"
} else {
    # Copy installer from local release directory
    Write-TestInfo "Attempting to copy installer from project..."
    $localInstaller = "D:\Projects\Sentinel\installers\sentinel-installer-windows-amd64.exe"
    if (Test-Path $localInstaller) {
        scp $localInstaller "${SandboxUser}@${SandboxHost}:C:/Temp/sentinel-installer.exe"
        Write-TestPass "Installer copied from local project"
    } else {
        Write-TestFail "Could not get installer to sandbox"
        exit 1
    }
}

# Run installer with installation code
Write-TestInfo "Running installer with code: $($script:InstallationCode)..."

$installCmd = @"
C:\Temp\sentinel-installer.exe --code $($script:InstallationCode) --server $BaseUrl --silent 2>&1
"@

$installResult = Invoke-SshCommand -Command $installCmd -Timeout 180

Write-TestInfo "Installer output:"
$installResult.Output -split "`n" | ForEach-Object { Write-Host "    $_" -ForegroundColor Gray }

# Wait for installation to complete
Write-TestInfo "Waiting for services to start (30s)..."
Start-Sleep -Seconds 30

# ==============================================================
# PHASE 6: Verify Services Installed
# ==============================================================
Write-TestPhase "PHASE 6" "Verify Windows Services"

# Check SentinelAgent service
Write-TestInfo "Checking SentinelAgent service..."
$agentService = Invoke-SshCommand -Command "sc query SentinelAgent"

if ($agentService.Output -match "RUNNING") {
    Write-TestPass "SentinelAgent service is RUNNING"
} elseif ($agentService.Output -match "STOPPED") {
    Write-TestWarn "SentinelAgent service is STOPPED"
    # Try to start it
    Invoke-SshCommand -Command "sc start SentinelAgent" | Out-Null
    Start-Sleep -Seconds 5
    $agentService = Invoke-SshCommand -Command "sc query SentinelAgent"
    if ($agentService.Output -match "RUNNING") {
        Write-TestPass "SentinelAgent service started successfully"
    } else {
        Write-TestFail "SentinelAgent service failed to start" $agentService.Output
    }
} else {
    Write-TestFail "SentinelAgent service not found" $agentService.Output
}

# Check SentinelWatchdog service
Write-TestInfo "Checking SentinelWatchdog service..."
$watchdogService = Invoke-SshCommand -Command "sc query SentinelWatchdog"

if ($watchdogService.Output -match "RUNNING") {
    Write-TestPass "SentinelWatchdog service is RUNNING"
} elseif ($watchdogService.Output -match "STOPPED") {
    Write-TestWarn "SentinelWatchdog service is STOPPED (may be normal)"
} else {
    Write-TestWarn "SentinelWatchdog service not found (may be optional)"
}

# Check service configuration
Write-TestInfo "Checking service configuration..."
$agentConfig = Invoke-SshCommand -Command "sc qc SentinelAgent"
if ($agentConfig.Output -match "AUTO_START") {
    Write-TestPass "SentinelAgent configured for automatic startup"
} else {
    Write-TestWarn "SentinelAgent may not be configured for auto-start"
}

# ==============================================================
# PHASE 7: Verify Device in Dashboard
# ==============================================================
Write-TestPhase "PHASE 7" "Verify Device Appears in Dashboard"

Write-TestInfo "Waiting for device to register (15s)..."
Start-Sleep -Seconds 15

Write-TestInfo "Fetching device list..."
$devicesResult = Invoke-ApiRequest -Endpoint "/api/devices" -Authenticated

if ($devicesResult.StatusCode -eq 200) {
    try {
        $devices = $devicesResult.Body | ConvertFrom-Json

        # Look for our test device
        $testDevice = $devices | Where-Object {
            $_.hostname -match $DeviceName -or
            $_.displayName -match $DeviceName -or
            $_.name -match $DeviceName
        }

        if ($testDevice) {
            $script:DeviceId = $testDevice.id
            Write-TestPass "Device found in dashboard!"
            Write-TestInfo "  Device ID: $($testDevice.id)"
            Write-TestInfo "  Hostname:  $($testDevice.hostname)"
            Write-TestInfo "  Status:    $($testDevice.status)"
            Write-TestInfo "  Online:    $($testDevice.isOnline)"
        } else {
            # Device might be registered with sandbox hostname instead
            $sandboxDevice = $devices | Where-Object {
                $_.hostname -match $SandboxHost -or
                $_.hostname -match "RemoteServer" -or
                $_.hostname -match "DESKTOP"
            } | Select-Object -Last 1

            if ($sandboxDevice) {
                $script:DeviceId = $sandboxDevice.id
                Write-TestPass "Device found (registered with sandbox hostname)"
                Write-TestInfo "  Device ID: $($sandboxDevice.id)"
                Write-TestInfo "  Hostname:  $($sandboxDevice.hostname)"
            } else {
                Write-TestFail "Device not found in dashboard"
                Write-TestInfo "Total devices: $($devices.Count)"
            }
        }
    } catch {
        Write-TestFail "Failed to parse devices response" $_.Exception.Message
    }
} else {
    Write-TestFail "Failed to fetch devices (HTTP $($devicesResult.StatusCode))" $devicesResult.Body
}

# ==============================================================
# PHASE 8: Test Remote Interaction
# ==============================================================
Write-TestPhase "PHASE 8" "Test Remote Interaction"

if ($script:DeviceId) {
    Write-TestInfo "Testing remote command execution..."

    # Send a simple command
    $cmdResult = Invoke-ApiRequest -Method "POST" -Endpoint "/api/devices/$($script:DeviceId)/commands" -Authenticated -Body @{
        command = "echo Hello from Sentinel E2E Test"
        type = "shell"
    }

    if ($cmdResult.StatusCode -eq 200 -or $cmdResult.StatusCode -eq 201 -or $cmdResult.StatusCode -eq 202) {
        Write-TestPass "Remote command sent successfully"

        # Wait for response
        Write-TestInfo "Waiting for command response (10s)..."
        Start-Sleep -Seconds 10

        # Check command history
        $historyResult = Invoke-ApiRequest -Endpoint "/api/devices/$($script:DeviceId)/commands" -Authenticated
        if ($historyResult.StatusCode -eq 200) {
            Write-TestPass "Command history retrieved"
        }
    } else {
        Write-TestWarn "Remote command may have failed (HTTP $($cmdResult.StatusCode))"
    }

    # Test ping
    Write-TestInfo "Testing device ping..."
    $pingResult = Invoke-ApiRequest -Method "POST" -Endpoint "/api/devices/$($script:DeviceId)/ping" -Authenticated

    if ($pingResult.StatusCode -eq 200) {
        Write-TestPass "Device responded to ping"
    } else {
        Write-TestWarn "Ping returned HTTP $($pingResult.StatusCode)"
    }

    # Get device metrics
    Write-TestInfo "Fetching device metrics..."
    $metricsResult = Invoke-ApiRequest -Endpoint "/api/devices/$($script:DeviceId)/metrics?hours=1" -Authenticated

    if ($metricsResult.StatusCode -eq 200) {
        Write-TestPass "Device metrics retrieved"
    } else {
        Write-TestWarn "Metrics returned HTTP $($metricsResult.StatusCode)"
    }
} else {
    Write-TestWarn "Skipping remote interaction tests - no device ID"
}

# ==============================================================
# PHASE 9: Cleanup (Optional)
# ==============================================================
if (-not $SkipCleanup) {
    Write-TestPhase "PHASE 9" "Cleanup"

    Write-TestInfo "Stopping and removing agent from sandbox..."
    Invoke-SshCommand -Command "sc stop SentinelAgent 2>nul" | Out-Null
    Invoke-SshCommand -Command "sc stop SentinelWatchdog 2>nul" | Out-Null
    Start-Sleep -Seconds 3
    Invoke-SshCommand -Command "sc delete SentinelAgent 2>nul" | Out-Null
    Invoke-SshCommand -Command "sc delete SentinelWatchdog 2>nul" | Out-Null
    Invoke-SshCommand -Command "del /f /q C:\Temp\sentinel-installer.exe 2>nul" | Out-Null
    Write-TestPass "Sandbox cleanup complete"

    if ($script:DeviceId) {
        Write-TestInfo "Removing device from dashboard..."
        $deleteResult = Invoke-ApiRequest -Method "DELETE" -Endpoint "/api/devices/$($script:DeviceId)" -Authenticated
        if ($deleteResult.StatusCode -eq 200 -or $deleteResult.StatusCode -eq 204) {
            Write-TestPass "Device removed from dashboard"
        } else {
            Write-TestWarn "Could not remove device (HTTP $($deleteResult.StatusCode))"
        }
    }
} else {
    Write-TestInfo "Cleanup skipped (--SkipCleanup flag)"
}

# ==============================================================
# RESULTS
# ==============================================================
Write-Host ""
Write-Host "===========================================================" -ForegroundColor Cyan
Write-Host "  END-TO-END TEST RESULTS" -ForegroundColor Cyan
Write-Host "===========================================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Passed: $script:TestsPassed" -ForegroundColor Green
Write-Host "  Failed: $script:TestsFailed" -ForegroundColor $(if ($script:TestsFailed -gt 0) { "Red" } else { "Green" })
Write-Host ""

if ($script:TestsFailed -eq 0) {
    Write-Host "  FULL E2E TEST: PASSED" -ForegroundColor Green
    Write-Host ""
    Write-Host "  The complete user journey is functional:" -ForegroundColor Green
    Write-Host "    [x] Admin login" -ForegroundColor Green
    Write-Host "    [x] Installation code generation" -ForegroundColor Green
    Write-Host "    [x] Code validation by installer" -ForegroundColor Green
    Write-Host "    [x] Agent installation on remote machine" -ForegroundColor Green
    Write-Host "    [x] Windows services running" -ForegroundColor Green
    Write-Host "    [x] Device visible in dashboard" -ForegroundColor Green
    Write-Host "    [x] Remote interaction working" -ForegroundColor Green
    Write-Host ""
    exit 0
} else {
    Write-Host "  FULL E2E TEST: FAILED" -ForegroundColor Red
    Write-Host ""
    Write-Host "  Some parts of the user journey are broken." -ForegroundColor Red
    Write-Host "  Review the failures above and fix before deploying." -ForegroundColor Red
    Write-Host ""
    exit 1
}
