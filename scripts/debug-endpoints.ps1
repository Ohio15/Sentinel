# Debug failing endpoints
$BaseUrl = $env:SENTINEL_URL
$Username = "admin"
$Password = $env:SENTINEL_ADMIN_PASSWORD
if (-not $BaseUrl) { Write-Host "Set SENTINEL_URL env var" -ForegroundColor Red; exit 1 }
if (-not $Password) { Write-Host "Set SENTINEL_ADMIN_PASSWORD env var" -ForegroundColor Red; exit 1 }

# Trust all certs
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

# Login first
$loginBody = @{ identifier = $Username; password = $Password } | ConvertTo-Json
$loginResponse = Invoke-WebRequest -Uri "$BaseUrl/api/auth/login" -Method POST -Body $loginBody -ContentType "application/json" -UseBasicParsing
$loginData = $loginResponse.Content | ConvertFrom-Json
$token = $loginData.accessToken
$csrf = $loginData.csrfToken

Write-Host "Logged in, token: $($token.Substring(0,30))..."
Write-Host "CSRF token: $csrf"
Write-Host ""

$headers = @{
    "Authorization" = "Bearer $token"
    "X-CSRF-Token" = $csrf
}

# Test list scripts
Write-Host "=== Testing GET /api/scripts ==="
try {
    $response = Invoke-WebRequest -Uri "$BaseUrl/api/scripts" -Method GET -Headers $headers -UseBasicParsing
    Write-Host "Status: $($response.StatusCode)"
    Write-Host "Body: $($response.Content)"
} catch {
    Write-Host "ERROR: $($_.Exception.Message)" -ForegroundColor Red
    if ($_.Exception.Response) {
        $stream = $_.Exception.Response.GetResponseStream()
        $reader = New-Object System.IO.StreamReader($stream)
        $body = $reader.ReadToEnd()
        Write-Host "Response Body: $body" -ForegroundColor Red
        Write-Host "Status Code: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
    }
}

Write-Host ""

# Test create script
Write-Host "=== Testing POST /api/scripts ==="
$scriptBody = @{
    name = "Debug Test Script"
    description = "Debug test"
    content = "Write-Host 'Test'"
    language = "powershell"
    osTypes = @("windows")
} | ConvertTo-Json -Depth 3

Write-Host "Request body: $scriptBody"
try {
    $response = Invoke-WebRequest -Uri "$BaseUrl/api/scripts" -Method POST -Body $scriptBody -ContentType "application/json" -Headers $headers -UseBasicParsing
    Write-Host "Status: $($response.StatusCode)"
    Write-Host "Body: $($response.Content)"
} catch {
    Write-Host "ERROR: $($_.Exception.Message)" -ForegroundColor Red
    if ($_.Exception.Response) {
        $stream = $_.Exception.Response.GetResponseStream()
        $reader = New-Object System.IO.StreamReader($stream)
        $body = $reader.ReadToEnd()
        Write-Host "Response Body: $body" -ForegroundColor Red
        Write-Host "Status Code: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
    }
}

Write-Host ""

# Test list users
Write-Host "=== Testing GET /api/users ==="
try {
    $response = Invoke-WebRequest -Uri "$BaseUrl/api/users" -Method GET -Headers $headers -UseBasicParsing
    Write-Host "Status: $($response.StatusCode)"
    Write-Host "Body: $($response.Content)"
} catch {
    Write-Host "ERROR: $($_.Exception.Message)" -ForegroundColor Red
    if ($_.Exception.Response) {
        $stream = $_.Exception.Response.GetResponseStream()
        $reader = New-Object System.IO.StreamReader($stream)
        $body = $reader.ReadToEnd()
        Write-Host "Response Body: $body" -ForegroundColor Red
        Write-Host "Status Code: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
    }
}
