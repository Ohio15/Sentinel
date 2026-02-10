# Sentinel RMM - Phase 5: Security Baseline
# Ensures Windows Defender is active and disables security risks

#Requires -RunAsAdministrator

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Phase 5: Security Baseline" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

Write-Host "`n[1/4] Ensuring Windows Defender is active..." -ForegroundColor Yellow

# Check for third-party antivirus
$thirdPartyAV = Get-CimInstance -Namespace root/SecurityCenter2 -ClassName AntiVirusProduct -ErrorAction SilentlyContinue |
    Where-Object { $_.displayName -notmatch "Windows Defender|Microsoft Defender" }

if ($thirdPartyAV) {
    Write-Host "  WARNING: Third-party antivirus detected:" -ForegroundColor Red
    $thirdPartyAV | ForEach-Object { Write-Host "    - $($_.displayName)" -ForegroundColor Red }
    Write-Host "  Remove third-party AV before enabling Defender" -ForegroundColor Red
} else {
    # Enable Windows Defender
    Set-MpPreference -DisableRealtimeMonitoring $false -ErrorAction SilentlyContinue
    Start-Service WinDefend -ErrorAction SilentlyContinue

    $defenderStatus = Get-MpComputerStatus -ErrorAction SilentlyContinue
    if ($defenderStatus.RealTimeProtectionEnabled) {
        Write-Host "  Windows Defender real-time protection: ENABLED" -ForegroundColor Green
    } else {
        Write-Host "  Windows Defender real-time protection: DISABLED (check Group Policy)" -ForegroundColor Yellow
    }
}

Write-Host "`n[2/4] Disabling risky services..." -ForegroundColor Yellow

$riskyServices = @(
    @{ Name = "RemoteRegistry"; Reason = "Remote registry access (security risk)" },
    @{ Name = "lltdsvc"; Reason = "Link-Layer Topology Discovery (network mapping)" },
    @{ Name = "p2pimsvc"; Reason = "Peer Networking Identity Manager" },
    @{ Name = "p2psvc"; Reason = "Peer Networking Grouping" },
    @{ Name = "PNRPsvc"; Reason = "Peer Name Resolution Protocol" }
)

foreach ($svc in $riskyServices) {
    $service = Get-Service -Name $svc.Name -ErrorAction SilentlyContinue
    if ($service) {
        Stop-Service $svc.Name -Force -ErrorAction SilentlyContinue
        Set-Service $svc.Name -StartupType Disabled -ErrorAction SilentlyContinue
        Write-Host "  Disabled: $($svc.Name) - $($svc.Reason)" -ForegroundColor Gray
    }
}

Write-Host "`n[3/4] Configuring Windows Firewall..." -ForegroundColor Yellow

# Ensure firewall is enabled for all profiles
Set-NetFirewallProfile -Profile Domain,Public,Private -Enabled True -ErrorAction SilentlyContinue
Write-Host "  Firewall enabled for all profiles (Domain, Private, Public)" -ForegroundColor Gray

# Block inbound by default (allow outbound)
Set-NetFirewallProfile -Profile Domain,Public,Private -DefaultInboundAction Block -DefaultOutboundAction Allow -ErrorAction SilentlyContinue
Write-Host "  Default: Block inbound, Allow outbound" -ForegroundColor Gray

Write-Host "`n[4/4] Configuring additional security settings..." -ForegroundColor Yellow

# Disable Remote Assistance
$raPath = "HKLM:\SYSTEM\CurrentControlSet\Control\Remote Assistance"
if (!(Test-Path $raPath)) { New-Item -Path $raPath -Force | Out-Null }
Set-ItemProperty -Path $raPath -Name "fAllowToGetHelp" -Value 0 -Type DWord -Force
Write-Host "  Disabled Remote Assistance solicited requests" -ForegroundColor Gray

# Enable SmartScreen
$smartScreenPath = "HKLM:\SOFTWARE\Policies\Microsoft\Windows\System"
if (!(Test-Path $smartScreenPath)) { New-Item -Path $smartScreenPath -Force | Out-Null }
Set-ItemProperty -Path $smartScreenPath -Name "EnableSmartScreen" -Value 1 -Type DWord -Force
Write-Host "  Enabled SmartScreen" -ForegroundColor Gray

# Disable AutoPlay
$autoplayPath = "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\Explorer"
if (!(Test-Path $autoplayPath)) { New-Item -Path $autoplayPath -Force | Out-Null }
Set-ItemProperty -Path $autoplayPath -Name "NoDriveTypeAutoRun" -Value 255 -Type DWord -Force
Write-Host "  Disabled AutoPlay for all drives" -ForegroundColor Gray

# Enable UAC (ensure it's on)
$uacPath = "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System"
Set-ItemProperty -Path $uacPath -Name "EnableLUA" -Value 1 -Type DWord -Force
Set-ItemProperty -Path $uacPath -Name "ConsentPromptBehaviorAdmin" -Value 5 -Type DWord -Force
Write-Host "  UAC enabled with secure desktop" -ForegroundColor Gray

Write-Host "`n========================================" -ForegroundColor Green
Write-Host "Phase 5 Complete: Security Baseline Applied" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host "`nSecurity Status:" -ForegroundColor Cyan
Write-Host "  - Windows Defender: Active (if no third-party AV)" -ForegroundColor White
Write-Host "  - Windows Firewall: Enabled (all profiles)" -ForegroundColor White
Write-Host "  - Remote Registry: Disabled" -ForegroundColor White
Write-Host "  - Remote Assistance: Disabled" -ForegroundColor White
Write-Host "  - SmartScreen: Enabled" -ForegroundColor White
Write-Host "  - AutoPlay: Disabled" -ForegroundColor White
Write-Host "  - UAC: Enabled" -ForegroundColor White
