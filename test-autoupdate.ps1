# Sentinel Agent Auto-Update Test Script
# Run this script as Administrator (right-click > Run as Administrator)

Write-Host "=== Sentinel Agent Auto-Update Test ===" -ForegroundColor Cyan
Write-Host ""

# Step 1: Uninstall existing service
Write-Host "[1/3] Uninstalling existing service..." -ForegroundColor Yellow
& "D:\Projects\Sentinel\release\agent\sentinel-agent.exe" --uninstall 2>$null
Start-Sleep -Seconds 2

# Verify service is gone
$service = Get-Service -Name "SentinelAgent" -ErrorAction SilentlyContinue
if ($service) {
    Write-Host "  Warning: Service still exists, forcing removal..." -ForegroundColor Red
    sc.exe delete SentinelAgent 2>$null
    Start-Sleep -Seconds 2
}
Write-Host "  Done." -ForegroundColor Green

# Step 2: Install v1.17.0 as service
Write-Host ""
Write-Host "[2/3] Installing v1.17.0 agent as service..." -ForegroundColor Yellow
& "D:\Projects\Sentinel\downloads\sentinel-agent-v1.17.0.exe" --install --server="http://localhost:8081" --token="40addfff-a1c0-4825-8e70-ca422dffd90e"
Start-Sleep -Seconds 3
Write-Host "  Done." -ForegroundColor Green

# Step 3: Check service status
Write-Host ""
Write-Host "[3/3] Checking service status..." -ForegroundColor Yellow
$service = Get-Service -Name "SentinelAgent" -ErrorAction SilentlyContinue
if ($service) {
    Write-Host "  Service Status: $($service.Status)" -ForegroundColor Green

    # Get installed version
    $agentPath = "C:\Program Files\Sentinel Agent\sentinel-agent.exe"
    if (Test-Path $agentPath) {
        $version = & $agentPath --version 2>$null
        Write-Host "  Installed Version: $version" -ForegroundColor Green
    }
} else {
    Write-Host "  ERROR: Service not found!" -ForegroundColor Red
}

Write-Host ""
Write-Host "=== Setup Complete ===" -ForegroundColor Cyan
Write-Host ""
Write-Host "The agent should now:" -ForegroundColor White
Write-Host "  1. Connect to the server at localhost:8081"
Write-Host "  2. Detect that v1.19.0 is available"
Write-Host "  3. Download and apply the update automatically"
Write-Host ""
Write-Host "Check the Sentinel app to see the agent version update from 1.17.0 to 1.19.0"
Write-Host ""
Write-Host "Press any key to exit..."
$null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")
