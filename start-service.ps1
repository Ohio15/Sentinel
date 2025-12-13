# Run this script as Administrator
Write-Host "Starting SentinelAgent service..." -ForegroundColor Cyan
Start-Service SentinelAgent -ErrorAction Stop
Write-Host "Service started successfully!" -ForegroundColor Green
Start-Sleep -Seconds 5
Get-Service SentinelAgent | Format-List *
