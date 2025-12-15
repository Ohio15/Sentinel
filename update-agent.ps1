# Update Sentinel Agent Script
# Must be run as Administrator

$ErrorActionPreference = "Stop"

Write-Host "Stopping Sentinel Agent service..."
Stop-Service 'SentinelAgent' -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2

# Kill any remaining processes
Write-Host "Stopping any remaining sentinel-agent processes..."
Get-Process -Name "sentinel-agent" -ErrorAction SilentlyContinue | Stop-Process -Force
Get-Process -Name "sentinel-watchdog" -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 2

Write-Host "Copying new agent binary..."
Copy-Item "D:\Projects\Sentinel\release\agent\sentinel-agent.exe" "C:\Program Files\Sentinel Agent\sentinel-agent.exe" -Force

Write-Host "Starting Sentinel Agent service..."
Start-Service 'SentinelAgent'

Write-Host "Done! Verifying..."
Get-Item "C:\Program Files\Sentinel Agent\sentinel-agent.exe" | Select-Object Name, Length, LastWriteTime
Get-Service 'SentinelAgent' | Select-Object Name, Status
