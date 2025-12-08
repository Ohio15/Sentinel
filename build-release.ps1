# Stop any running Electron/Sentinel processes
Write-Host "Stopping Electron processes..."
Get-Process -Name "electron", "Sentinel" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

Start-Sleep -Seconds 3

# Remove release folder
$releasePath = "D:\Projects\Sentinel\release"
if (Test-Path $releasePath) {
    Write-Host "Removing release folder..."
    Remove-Item -Path $releasePath -Recurse -Force -ErrorAction SilentlyContinue
}

Start-Sleep -Seconds 1

# Build
Write-Host "Building..."
Set-Location "D:\Projects\Sentinel"
npm run dist:win
