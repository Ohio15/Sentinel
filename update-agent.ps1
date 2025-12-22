# Update Sentinel Agent script
Write-Host "Stopping services..."
Stop-Service SentinelWatchdog -Force -ErrorAction SilentlyContinue
Stop-Service SentinelAgent -Force -ErrorAction SilentlyContinue

Write-Host "Waiting for services to stop..."
Start-Sleep 5

Write-Host "Killing any remaining processes..."
Get-Process | Where-Object { $_.Name -like '*sentinel*' } | Stop-Process -Force -ErrorAction SilentlyContinue

Write-Host "Waiting after kill..."
Start-Sleep 3

Write-Host "Copying new agent..."
Copy-Item 'D:\Projects\Sentinel\release\agent\sentinel-agent.exe' 'C:\Program Files\Sentinel Agent\sentinel-agent.exe' -Force

Write-Host "Starting services..."
Start-Service SentinelAgent
Start-Sleep 2
Start-Service SentinelWatchdog

Write-Host "Checking version..."
& 'C:\Program Files\Sentinel Agent\sentinel-agent.exe' -version

Write-Host "Service status:"
Get-Service SentinelAgent, SentinelWatchdog | Format-Table Name, Status
