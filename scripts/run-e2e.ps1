# E2E Test Runner - reads password from stdin or uses default
$BaseUrl = $env:SENTINEL_URL
$Username = "admin"
$Password = $env:SENTINEL_ADMIN_PASSWORD
if (-not $BaseUrl) { Write-Host "Set SENTINEL_URL and SENTINEL_ADMIN_PASSWORD env vars" -ForegroundColor Red; exit 1 }

Set-Location "D:\Projects\Sentinel"
. "./scripts/Test-E2E-Comprehensive.ps1" -BaseUrl $BaseUrl -Username $Username -Password $Password -SkipCleanup
