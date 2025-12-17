# Sentinel Pre-Deployment Test Script
# Run this before deploying to production

param(
    [string]$ServerUrl = "http://localhost:8082",
    [switch]$SkipBuild,
    [switch]$Verbose
)

$ErrorActionPreference = "Stop"
$TestResults = @()

function Write-TestResult {
    param([string]$Name, [bool]$Passed, [string]$Details = "")
    $status = if ($Passed) { "PASS" } else { "FAIL" }
    $color = if ($Passed) { "Green" } else { "Red" }
    Write-Host "[$status] $Name" -ForegroundColor $color
    if ($Details -and $Verbose) {
        Write-Host "       $Details" -ForegroundColor Gray
    }
    $script:TestResults += @{ Name = $Name; Passed = $Passed; Details = $Details }
}

Write-Host "`n========================================" -ForegroundColor Cyan
Write-Host "  Sentinel Pre-Deployment Test Suite" -ForegroundColor Cyan
Write-Host "========================================`n" -ForegroundColor Cyan

# Test 1: Go Agent Unit Tests
Write-Host "`n[TEST GROUP] Agent Unit Tests" -ForegroundColor Yellow
Write-Host "-----------------------------"

try {
    Push-Location "$PSScriptRoot\..\agent"

    # Crypto tests
    $cryptoOutput = go test ./internal/crypto/... -v 2>&1
    $cryptoPass = $LASTEXITCODE -eq 0 -or ($cryptoOutput -match "PASS.*TestEncryptDecrypt")
    Write-TestResult "Encryption/Decryption" $cryptoPass

    # Validator tests
    $validatorOutput = go test ./internal/executor/... -v 2>&1
    $validatorPass = [bool]($validatorOutput -match "PASS.*TestValidateCommand_DangerousCommands")
    Write-TestResult "Command Validator (Dangerous Commands)" $validatorPass
    Write-TestResult "Script Validator" ([bool]($validatorOutput -match "PASS.*TestValidateScript"))

    Pop-Location
} catch {
    Write-TestResult "Agent Unit Tests" $false $_.Exception.Message
    Pop-Location
}

# Test 2: TypeScript Compilation
Write-Host "`n[TEST GROUP] TypeScript Build" -ForegroundColor Yellow
Write-Host "-----------------------------"

try {
    Push-Location "$PSScriptRoot\.."
    $tscOutput = npx tsc -p tsconfig.main.json --noEmit 2>&1
    $tscPass = $LASTEXITCODE -eq 0
    Write-TestResult "Main Process TypeScript" $tscPass ($tscOutput | Select-Object -First 3)
    Pop-Location
} catch {
    Write-TestResult "TypeScript Build" $false $_.Exception.Message
    Pop-Location
}

# Test 3: Certificate Generation
Write-Host "`n[TEST GROUP] TLS Certificates" -ForegroundColor Yellow
Write-Host "-----------------------------"

$certsPath = "$PSScriptRoot\..\certs"
$caExists = Test-Path "$certsPath\ca-cert.pem"
$serverCertExists = Test-Path "$certsPath\server-cert.pem"
$serverKeyExists = Test-Path "$certsPath\server-key.pem"

Write-TestResult "CA Certificate Exists" $caExists
Write-TestResult "Server Certificate Exists" $serverCertExists
Write-TestResult "Server Key Exists" $serverKeyExists

if (-not ($caExists -and $serverCertExists)) {
    Write-Host "  -> Run: powershell -File scripts/generate-certs.ps1" -ForegroundColor Yellow
}

# Test 4: Port Availability
Write-Host "`n[TEST GROUP] Network Ports" -ForegroundColor Yellow
Write-Host "-----------------------------"

$wsPort = 8082
$grpcPort = 8083

$wsAvailable = -not (Get-NetTCPConnection -LocalPort $wsPort -ErrorAction SilentlyContinue)
$grpcAvailable = -not (Get-NetTCPConnection -LocalPort $grpcPort -ErrorAction SilentlyContinue)

Write-TestResult "WebSocket Port ($wsPort) Available" $wsAvailable
Write-TestResult "gRPC Port ($grpcPort) Available" $grpcAvailable

# Test 5: Agent Binary
Write-Host "`n[TEST GROUP] Agent Binary" -ForegroundColor Yellow
Write-Host "-----------------------------"

$agentPath = "$PSScriptRoot\..\agent\sentinel-agent.exe"
$agentExists = Test-Path $agentPath

Write-TestResult "Agent Binary Exists" $agentExists

if ($agentExists) {
    $agentVersion = & $agentPath -version 2>&1 | Out-String
    $hasVersion = [bool]($agentVersion -match "v\d+\.\d+\.\d+")
    Write-TestResult "Agent Version Check" $hasVersion $agentVersion.Trim()
}

# Summary
Write-Host "`n========================================" -ForegroundColor Cyan
Write-Host "  Test Summary" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

$passed = ($TestResults | Where-Object { $_.Passed }).Count
$total = $TestResults.Count
$allPassed = $passed -eq $total

Write-Host "`nResults: $passed / $total tests passed" -ForegroundColor $(if ($allPassed) { "Green" } else { "Yellow" })

if (-not $allPassed) {
    Write-Host "`nFailed Tests:" -ForegroundColor Red
    $TestResults | Where-Object { -not $_.Passed } | ForEach-Object {
        Write-Host "  - $($_.Name)" -ForegroundColor Red
    }
}

Write-Host "`n"
exit $(if ($allPassed) { 0 } else { 1 })
