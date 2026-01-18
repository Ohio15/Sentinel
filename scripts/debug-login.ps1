# Debug login test
param([string]$BaseUrl = "https://sentinelrmm.us:4443", [string]$Username = "admin", [string]$Password = 'V_hqyDU.!dsRKoiBN*.JPVnsFJ4Xzo')

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

Write-Host "Username: $Username"
Write-Host "Password: $Password"
Write-Host "Password length: $($Password.Length)"

$body = @{
    identifier = $Username
    password = $Password
}

$json = $body | ConvertTo-Json
Write-Host "JSON body: $json"
Write-Host "JSON length: $($json.Length)"

try {
    $response = Invoke-WebRequest -Uri "$BaseUrl/api/auth/login" -Method POST -Body $json -ContentType "application/json" -UseBasicParsing -ErrorAction Stop
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
