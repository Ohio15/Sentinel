# Sentinel RMM - Enable Remote Access Script
# Run on target PC to enable WinRM for remote management
# Usage: iex (irm http://192.168.137.1:8080/enable-remote.ps1)

#Requires -RunAsAdministrator

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Sentinel RMM - Enable Remote Access" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

# Set network to Private (required for WinRM)
Write-Host "`n[1/6] Setting network profile to Private..." -ForegroundColor Yellow
Get-NetConnectionProfile | Set-NetConnectionProfile -NetworkCategory Private -ErrorAction SilentlyContinue

# Enable WinRM
Write-Host "[2/6] Enabling WinRM service..." -ForegroundColor Yellow
Enable-PSRemoting -Force -SkipNetworkProfileCheck
Set-Service WinRM -StartupType Automatic
Start-Service WinRM
winrm quickconfig -force 2>$null | Out-Null

# Configure WinRM settings
Write-Host "[3/6] Configuring WinRM settings..." -ForegroundColor Yellow
Set-Item WSMan:\localhost\Service\AllowUnencrypted -Value $true -Force
Set-Item WSMan:\localhost\Service\Auth\Basic -Value $true -Force
Set-Item WSMan:\localhost\Client\TrustedHosts -Value "*" -Force

# Configure firewall
Write-Host "[4/6] Configuring firewall rules..." -ForegroundColor Yellow
netsh advfirewall firewall add rule name="WinRM-HTTP" dir=in action=allow protocol=TCP localport=5985 | Out-Null
netsh advfirewall firewall add rule name="ICMP Allow" dir=in action=allow protocol=icmpv4 | Out-Null
netsh advfirewall firewall add rule name="Allow-Local-Subnet" dir=in action=allow remoteip=192.168.137.0/24 | Out-Null

# Get current IP
Write-Host "[5/6] Getting network information..." -ForegroundColor Yellow
$ip = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -like '192.168.*' -and $_.InterfaceAlias -notlike '*Loopback*' }).IPAddress | Select-Object -First 1

# Verify WinRM
Write-Host "[6/6] Verifying WinRM configuration..." -ForegroundColor Yellow
$winrmStatus = Get-Service WinRM

Write-Host "`n========================================" -ForegroundColor Green
Write-Host "Remote Access Enabled Successfully!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host "IP Address: $ip" -ForegroundColor White
Write-Host "WinRM Status: $($winrmStatus.Status)" -ForegroundColor White
Write-Host "`nFrom management PC, run:" -ForegroundColor Cyan
Write-Host "  Test-WSMan -ComputerName $ip" -ForegroundColor White
Write-Host "  Enter-PSSession -ComputerName $ip -Credential (Get-Credential)" -ForegroundColor White
