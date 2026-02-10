# Sentinel RMM - Full Windows Debloat Script
# Runs all debloat phases to transform any Windows PC into a clean, business-ready system
#
# Usage:
#   Local:  .\windows-debloat-full.ps1
#   Remote: iex (irm http://192.168.137.1:8080/windows-debloat-full.ps1)
#
# Phases:
#   1. Remove OEM/Manufacturer Bloatware
#   2. Remove Windows Store Bloatware (UWP Apps)
#   3. Disable Windows Consumer Features & Telemetry
#   4. Disable Windows Annoyances (Widgets, Suggestions, Copilot)
#   5. Apply Security Baseline

#Requires -RunAsAdministrator

$ErrorActionPreference = "SilentlyContinue"
$startTime = Get-Date

Write-Host @"

  ____             _   _            _   ____  __  __ __  __
 / ___|  ___ _ __ | |_(_)_ __   ___| | |  _ \|  \/  |  \/  |
 \___ \ / _ \ '_ \| __| | '_ \ / _ \ | | |_) | |\/| | |\/| |
  ___) |  __/ | | | |_| | | | |  __/ | |  _ <| |  | | |  | |
 |____/ \___|_| |_|\__|_|_| |_|\___|_| |_| \_\_|  |_|_|  |_|

  Windows Debloat - Full System Cleanup

"@ -ForegroundColor Cyan

Write-Host "========================================" -ForegroundColor White
Write-Host "Computer: $env:COMPUTERNAME" -ForegroundColor White
Write-Host "User: $env:USERNAME" -ForegroundColor White
Write-Host "OS: $(Get-CimInstance Win32_OperatingSystem | Select-Object -ExpandProperty Caption)" -ForegroundColor White
Write-Host "========================================`n" -ForegroundColor White

# ============================================================================
# Phase 1: Remove OEM Bloatware
# ============================================================================
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "PHASE 1: Remove OEM Bloatware" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

# Stop and disable OEM services
Write-Host "`nStopping OEM services..." -ForegroundColor Yellow
$oemServices = Get-Service | Where-Object {
    $_.DisplayName -match "Acer|HP|Dell|Lenovo|ASUS|Norton|McAfee|ExpressVPN"
}
foreach ($svc in $oemServices) {
    Stop-Service $svc.Name -Force -ErrorAction SilentlyContinue
    Set-Service $svc.Name -StartupType Disabled -ErrorAction SilentlyContinue
    Write-Host "  Disabled: $($svc.DisplayName)" -ForegroundColor Gray
}

# Remove OEM programs via registry
Write-Host "`nRemoving OEM programs..." -ForegroundColor Yellow
$uninstallPaths = @(
    "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall",
    "HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall"
)
$bloatPatterns = @(
    "*Acer*", "*Care Center*", "*Quick Access*", "*Planet9*",
    "*HP Support*", "*HP Audio*", "*HP JumpStart*", "*HP Sure*",
    "*Dell SupportAssist*", "*Dell Digital*", "*Dell Update*", "*Dell Customer*",
    "*Lenovo Vantage*", "*Lenovo Now*", "*Lenovo Settings*", "*Lenovo ID*",
    "*MyASUS*", "*ASUS Giftbox*", "*ASUS Smart*", "*ROG Gaming*",
    "*Norton*", "*McAfee*", "*ExpressVPN*", "*Amazon*", "*Booking*",
    "*WildTangent*", "*App Explorer*"
)

foreach ($path in $uninstallPaths) {
    Get-ChildItem $path -ErrorAction SilentlyContinue | ForEach-Object {
        $props = Get-ItemProperty $_.PSPath -ErrorAction SilentlyContinue
        foreach ($pattern in $bloatPatterns) {
            if ($props.DisplayName -like $pattern) {
                Write-Host "  Removing: $($props.DisplayName)" -ForegroundColor Gray
                if ($props.UninstallString -match "MsiExec") {
                    $guid = [regex]::Match($props.UninstallString, '\{[A-F0-9-]+\}').Value
                    if ($guid) { Start-Process msiexec.exe -ArgumentList "/x $guid /qn /norestart" -Wait -ErrorAction SilentlyContinue }
                }
                Remove-Item $_.PSPath -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
    }
}

Write-Host "  Phase 1 complete" -ForegroundColor Green

# ============================================================================
# Phase 2: Remove Windows Store Bloatware
# ============================================================================
Write-Host "`n========================================" -ForegroundColor Cyan
Write-Host "PHASE 2: Remove Store Bloatware" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

$removeApps = @(
    "*3DBuilder*","*3DViewer*","*BingFinance*","*BingNews*","*BingSports*","*BingWeather*",
    "*GetHelp*","*Getstarted*","*MicrosoftOfficeHub*","*MicrosoftSolitaireCollection*",
    "*MixedReality*","*NetworkSpeedTest*","*News*","*Office.Sway*","*OneConnect*",
    "*People*","*Print3D*","*SkypeApp*","*Wallet*","*Whiteboard*",
    "*WindowsFeedbackHub*","*WindowsMaps*","*WindowsSoundRecorder*",
    "*YourPhone*","*ZuneMusic*","*ZuneVideo*","*Todos*","*PowerAutomateDesktop*",
    "*GamingApp*","*Clipchamp*","*549981C3F5F10*","*Xbox*",
    "*CandyCrush*","*BubbleWitch*","*Facebook*","*Twitter*","*Instagram*","*TikTok*",
    "*Netflix*","*Hulu*","*Disney*","*Spotify*","*Dolby*","*Duolingo*","*king.com*"
)

Write-Host "`nRemoving AppX packages..." -ForegroundColor Yellow
foreach ($app in $removeApps) {
    Get-AppxPackage -AllUsers -Name $app -ErrorAction SilentlyContinue | ForEach-Object {
        Write-Host "  Removing: $($_.Name)" -ForegroundColor Gray
        Remove-AppxPackage -Package $_.PackageFullName -AllUsers -ErrorAction SilentlyContinue
    }
}

Write-Host "`nDeprovisioning packages..." -ForegroundColor Yellow
foreach ($app in $removeApps) {
    Get-AppxProvisionedPackage -Online -ErrorAction SilentlyContinue |
        Where-Object { $_.DisplayName -like $app } |
        ForEach-Object {
            Write-Host "  Deprovisioning: $($_.DisplayName)" -ForegroundColor Gray
            Remove-AppxProvisionedPackage -Online -PackageName $_.PackageName -ErrorAction SilentlyContinue | Out-Null
        }
}

Write-Host "  Phase 2 complete" -ForegroundColor Green

# ============================================================================
# Phase 3: Disable Consumer Features & Telemetry
# ============================================================================
Write-Host "`n========================================" -ForegroundColor Cyan
Write-Host "PHASE 3: Disable Consumer Features" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

Write-Host "`nConfiguring registry..." -ForegroundColor Yellow

# Cloud Content
$cloudPath = "HKLM:\SOFTWARE\Policies\Microsoft\Windows\CloudContent"
if (!(Test-Path $cloudPath)) { New-Item -Path $cloudPath -Force | Out-Null }
@{
    "DisableWindowsConsumerFeatures" = 1
    "DisableSoftLanding" = 1
    "DisableCloudOptimizedContent" = 1
}.GetEnumerator() | ForEach-Object { Set-ItemProperty -Path $cloudPath -Name $_.Key -Value $_.Value -Type DWord -Force }

# Content Delivery Manager
$cdmPath = "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\ContentDeliveryManager"
if (!(Test-Path $cdmPath)) { New-Item -Path $cdmPath -Force | Out-Null }
@{
    "ContentDeliveryAllowed" = 0
    "OemPreInstalledAppsEnabled" = 0
    "PreInstalledAppsEnabled" = 0
    "SilentInstalledAppsEnabled" = 0
}.GetEnumerator() | ForEach-Object { Set-ItemProperty -Path $cdmPath -Name $_.Key -Value $_.Value -Type DWord -Force -ErrorAction SilentlyContinue }

# Telemetry
$telemetryPath = "HKLM:\SOFTWARE\Policies\Microsoft\Windows\DataCollection"
if (!(Test-Path $telemetryPath)) { New-Item -Path $telemetryPath -Force | Out-Null }
Set-ItemProperty -Path $telemetryPath -Name "AllowTelemetry" -Value 0 -Type DWord -Force

Write-Host "  Consumer features disabled" -ForegroundColor Gray

# Disable telemetry services
@("DiagTrack", "dmwappushservice") | ForEach-Object {
    Stop-Service $_ -Force -ErrorAction SilentlyContinue
    Set-Service $_ -StartupType Disabled -ErrorAction SilentlyContinue
}
Write-Host "  Telemetry services disabled" -ForegroundColor Gray

Write-Host "  Phase 3 complete" -ForegroundColor Green

# ============================================================================
# Phase 4: Disable Annoyances
# ============================================================================
Write-Host "`n========================================" -ForegroundColor Cyan
Write-Host "PHASE 4: Disable Annoyances" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

Write-Host "`nConfiguring user settings..." -ForegroundColor Yellow

# Explorer settings
$explorerPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced"
@{
    "Start_IrisRecommendations" = 0
    "TaskbarDa" = 0
    "TaskbarMn" = 0
}.GetEnumerator() | ForEach-Object { Set-ItemProperty -Path $explorerPath -Name $_.Key -Value $_.Value -Type DWord -Force -ErrorAction SilentlyContinue }

# Copilot
$copilotPath = "HKCU:\Software\Policies\Microsoft\Windows\WindowsCopilot"
if (!(Test-Path $copilotPath)) { New-Item -Path $copilotPath -Force | Out-Null }
Set-ItemProperty -Path $copilotPath -Name "TurnOffWindowsCopilot" -Value 1 -Type DWord -Force

# Search
$searchPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\SearchSettings"
if (!(Test-Path $searchPath)) { New-Item -Path $searchPath -Force | Out-Null }
Set-ItemProperty -Path $searchPath -Name "IsDynamicSearchBoxEnabled" -Value 0 -Type DWord -Force

Write-Host "  Disabled: Widgets, Chat, Copilot, Start suggestions, Search highlights" -ForegroundColor Gray
Write-Host "  Phase 4 complete" -ForegroundColor Green

# ============================================================================
# Phase 5: Security Baseline
# ============================================================================
Write-Host "`n========================================" -ForegroundColor Cyan
Write-Host "PHASE 5: Security Baseline" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

Write-Host "`nConfiguring security settings..." -ForegroundColor Yellow

# Enable Windows Defender
Set-MpPreference -DisableRealtimeMonitoring $false -ErrorAction SilentlyContinue
Start-Service WinDefend -ErrorAction SilentlyContinue

# Disable risky services
@("RemoteRegistry", "lltdsvc") | ForEach-Object {
    Stop-Service $_ -Force -ErrorAction SilentlyContinue
    Set-Service $_ -StartupType Disabled -ErrorAction SilentlyContinue
}

# Enable firewall
Set-NetFirewallProfile -Profile Domain,Public,Private -Enabled True -ErrorAction SilentlyContinue

# Disable Remote Assistance
$raPath = "HKLM:\SYSTEM\CurrentControlSet\Control\Remote Assistance"
if (!(Test-Path $raPath)) { New-Item -Path $raPath -Force | Out-Null }
Set-ItemProperty -Path $raPath -Name "fAllowToGetHelp" -Value 0 -Type DWord -Force

# Disable AutoPlay
$autoplayPath = "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\Explorer"
if (!(Test-Path $autoplayPath)) { New-Item -Path $autoplayPath -Force | Out-Null }
Set-ItemProperty -Path $autoplayPath -Name "NoDriveTypeAutoRun" -Value 255 -Type DWord -Force

Write-Host "  Security baseline applied" -ForegroundColor Gray
Write-Host "  Phase 5 complete" -ForegroundColor Green

# ============================================================================
# Summary
# ============================================================================
$endTime = Get-Date
$duration = $endTime - $startTime

Write-Host "`n========================================" -ForegroundColor Green
Write-Host "DEBLOAT COMPLETE" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host "Duration: $($duration.Minutes)m $($duration.Seconds)s" -ForegroundColor White
Write-Host "`nSummary:" -ForegroundColor Cyan
Write-Host "  - OEM bloatware removed" -ForegroundColor White
Write-Host "  - Store bloatware removed" -ForegroundColor White
Write-Host "  - Consumer features disabled" -ForegroundColor White
Write-Host "  - UI annoyances disabled" -ForegroundColor White
Write-Host "  - Security baseline applied" -ForegroundColor White
Write-Host "`nREBOOT REQUIRED to complete all changes." -ForegroundColor Yellow
Write-Host "`nPress Enter to exit..." -ForegroundColor Gray
Read-Host
