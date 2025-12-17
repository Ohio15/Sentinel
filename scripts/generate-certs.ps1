# Generate self-signed certificates for Sentinel RMM
# This script creates a CA certificate and server certificates for TLS/mTLS

param(
    [string]$OutputDir = "certs",
    [string]$Hostname = $env:COMPUTERNAME,
    [int]$ValidityDays = 365
)

# Ensure output directory exists
$CertsPath = Join-Path $PSScriptRoot ".." $OutputDir
if (-not (Test-Path $CertsPath)) {
    New-Item -ItemType Directory -Path $CertsPath -Force | Out-Null
    Write-Host "Created certificates directory: $CertsPath" -ForegroundColor Green
}

Write-Host "`n=== Sentinel Certificate Generator ===" -ForegroundColor Cyan
Write-Host "Output Directory: $CertsPath" -ForegroundColor Yellow
Write-Host "Hostname: $Hostname" -ForegroundColor Yellow
Write-Host "Validity: $ValidityDays days" -ForegroundColor Yellow
Write-Host ""

# Generate CA private key and certificate
Write-Host "[1/4] Generating CA private key..." -ForegroundColor Cyan
$CAKeyPath = Join-Path $CertsPath "ca-key.pem"
$CACertPath = Join-Path $CertsPath "ca-cert.pem"

& openssl genrsa -out $CAKeyPath 4096 2>&1 | Out-Null
if ($LASTEXITCODE -eq 0) {
    Write-Host "  Generated CA private key: ca-key.pem" -ForegroundColor Green
} else {
    Write-Host "  ERROR: Failed to generate CA private key" -ForegroundColor Red
    exit 1
}

Write-Host "[2/4] Generating CA certificate..." -ForegroundColor Cyan
& openssl req -new -x509 -days $ValidityDays -key $CAKeyPath -out $CACertPath `
    -subj "/C=US/ST=State/L=City/O=Sentinel/OU=IT/CN=Sentinel CA" 2>&1 | Out-Null

if ($LASTEXITCODE -eq 0) {
    Write-Host "  Generated CA certificate: ca-cert.pem" -ForegroundColor Green
} else {
    Write-Host "  ERROR: Failed to generate CA certificate" -ForegroundColor Red
    exit 1
}

# Generate Server private key and CSR
Write-Host "[3/4] Generating server private key..." -ForegroundColor Cyan
$ServerKeyPath = Join-Path $CertsPath "server-key.pem"
$ServerCSRPath = Join-Path $CertsPath "server.csr"
$ServerCertPath = Join-Path $CertsPath "server-cert.pem"

& openssl genrsa -out $ServerKeyPath 4096 2>&1 | Out-Null
if ($LASTEXITCODE -eq 0) {
    Write-Host "  Generated server private key: server-key.pem" -ForegroundColor Green
} else {
    Write-Host "  ERROR: Failed to generate server private key" -ForegroundColor Red
    exit 1
}

# Create OpenSSL config for SAN (Subject Alternative Names)
$ConfigPath = Join-Path $CertsPath "server.cnf"
@"
[req]
default_bits = 4096
prompt = no
default_md = sha256
distinguished_name = dn
req_extensions = v3_req

[dn]
C=US
ST=State
L=City
O=Sentinel
OU=IT
CN=$Hostname

[v3_req]
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = $Hostname
DNS.2 = localhost
DNS.3 = *.local
IP.1 = 127.0.0.1
IP.2 = ::1
"@ | Out-File -FilePath $ConfigPath -Encoding ASCII

# Generate server CSR
& openssl req -new -key $ServerKeyPath -out $ServerCSRPath -config $ConfigPath 2>&1 | Out-Null
if ($LASTEXITCODE -eq 0) {
    Write-Host "  Generated server CSR: server.csr" -ForegroundColor Green
} else {
    Write-Host "  ERROR: Failed to generate server CSR" -ForegroundColor Red
    exit 1
}

# Sign server certificate with CA
Write-Host "[4/4] Signing server certificate with CA..." -ForegroundColor Cyan
& openssl x509 -req -days $ValidityDays -in $ServerCSRPath `
    -CA $CACertPath -CAkey $CAKeyPath -CAcreateserial `
    -out $ServerCertPath -extfile $ConfigPath -extensions v3_req 2>&1 | Out-Null

if ($LASTEXITCODE -eq 0) {
    Write-Host "  Generated server certificate: server-cert.pem" -ForegroundColor Green
} else {
    Write-Host "  ERROR: Failed to sign server certificate" -ForegroundColor Red
    exit 1
}

# Clean up temporary files
Remove-Item $ServerCSRPath -Force -ErrorAction SilentlyContinue
Remove-Item $ConfigPath -Force -ErrorAction SilentlyContinue
Remove-Item (Join-Path $CertsPath "ca-cert.srl") -Force -ErrorAction SilentlyContinue

# Set appropriate permissions (Windows)
Write-Host "`n=== Setting file permissions ===" -ForegroundColor Cyan
$keyFiles = @($CAKeyPath, $ServerKeyPath)
foreach ($keyFile in $keyFiles) {
    if (Test-Path $keyFile) {
        # Remove inheritance and set restrictive permissions
        $acl = Get-Acl $keyFile
        $acl.SetAccessRuleProtection($true, $false)
        $acl.Access | ForEach-Object { $acl.RemoveAccessRule($_) | Out-Null }

        # Add permission for current user
        $permission = New-Object System.Security.AccessControl.FileSystemAccessRule(
            $env:USERNAME, "FullControl", "Allow"
        )
        $acl.AddAccessRule($permission)
        Set-Acl $keyFile $acl
        Write-Host "  Secured: $(Split-Path $keyFile -Leaf)" -ForegroundColor Green
    }
}

# Display certificate information
Write-Host "`n=== Certificate Information ===" -ForegroundColor Cyan
Write-Host "`nCA Certificate:" -ForegroundColor Yellow
& openssl x509 -in $CACertPath -noout -subject -dates 2>&1 | ForEach-Object {
    Write-Host "  $_" -ForegroundColor White
}

Write-Host "`nServer Certificate:" -ForegroundColor Yellow
& openssl x509 -in $ServerCertPath -noout -subject -dates -ext subjectAltName 2>&1 | ForEach-Object {
    Write-Host "  $_" -ForegroundColor White
}

# Summary
Write-Host "`n=== Generation Complete ===" -ForegroundColor Green
Write-Host "`nGenerated files in: $CertsPath" -ForegroundColor Cyan
Write-Host "  ca-cert.pem       - CA certificate (distribute to clients)" -ForegroundColor White
Write-Host "  ca-key.pem        - CA private key (keep secure!)" -ForegroundColor White
Write-Host "  server-cert.pem   - Server certificate" -ForegroundColor White
Write-Host "  server-key.pem    - Server private key (keep secure!)" -ForegroundColor White

Write-Host "`nNext steps:" -ForegroundColor Yellow
Write-Host "  1. Distribute ca-cert.pem to all agents" -ForegroundColor White
Write-Host "  2. Configure gRPC server to use server-cert.pem and server-key.pem" -ForegroundColor White
Write-Host "  3. Configure agents to trust ca-cert.pem" -ForegroundColor White
Write-Host "  4. Restart Sentinel server and agents" -ForegroundColor White

Write-Host "`nFor production use:" -ForegroundColor Yellow
Write-Host "  - Use certificates from a trusted CA" -ForegroundColor White
Write-Host "  - Store private keys in secure key management systems" -ForegroundColor White
Write-Host "  - Implement certificate rotation policies" -ForegroundColor White
Write-Host ""
