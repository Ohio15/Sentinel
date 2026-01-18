# E2E Test Runner - reads password from stdin or uses default
$BaseUrl = "https://sentinelrmm.us:4443"
$Username = "admin"
$Password = 'REDACTED_PASSWORD'

Set-Location "D:\Projects\Sentinel"
. "./scripts/Test-E2E-Comprehensive.ps1" -BaseUrl $BaseUrl -Username $Username -Password $Password -SkipCleanup
