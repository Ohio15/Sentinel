# Quick login test
param(
    [string]$BaseUrl = "https://sentinelrmm.us:4443",
    [string]$Username = "admin",
    [string]$Password = 'V_hqyDU.!dsRKoiBN*.JPVnsFJ4Xzo'
)

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

$body = @{
    identifier = $Username
    password = $Password
} | ConvertTo-Json

Write-Host "Request body: $body"
Write-Host "URL: $BaseUrl/api/auth/login"

try {
    $response = Invoke-WebRequest -Uri "$BaseUrl/api/auth/login" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing -ErrorAction Stop
    Write-Host "Success! Status: $($response.StatusCode)"
    $data = $response.Content | ConvertFrom-Json
    Write-Host "User: $($data.user.username)"
    Write-Host "Token: $($data.accessToken.Substring(0, 50))..."
} catch {
    Write-Host "Error: $($_.Exception.Message)" -ForegroundColor Red
    if ($_.Exception.Response) {
        $stream = $_.Exception.Response.GetResponseStream()
        $reader = New-Object System.IO.StreamReader($stream)
        $responseBody = $reader.ReadToEnd()
        Write-Host "Response: $responseBody" -ForegroundColor Red
    }
}
