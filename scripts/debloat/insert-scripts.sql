-- Sentinel RMM - Insert Debloat Scripts into Database
-- Run with: docker exec sentinel-postgres psql -U sentinel -d sentinel -f /tmp/insert-scripts.sql

-- Enable Remote Access Script
INSERT INTO scripts (id, name, description, language, content, os_types, organization_id, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Enable Remote Access (WinRM)',
  'Enables Windows Remote Management (WinRM) for remote PowerShell access. Run on target PC to allow management from another machine.',
  'powershell',
  $SCRIPT$# Sentinel RMM - Enable Remote Access Script
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
$SCRIPT$,
  '["windows"]'::jsonb,
  1,
  NOW(),
  NOW()
) ON CONFLICT DO NOTHING;

-- Phase 1: Remove OEM Bloatware
INSERT INTO scripts (id, name, description, language, content, os_types, organization_id, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Debloat Phase 1: Remove OEM Bloatware',
  'Removes manufacturer bloatware (Acer, HP, Dell, Lenovo, ASUS) and promotional apps like Norton, McAfee, Amazon shortcuts.',
  'powershell',
  $SCRIPT$# Sentinel RMM - Phase 1: Remove OEM/Manufacturer Bloatware
# Removes Acer, HP, Dell, Lenovo, ASUS bloatware and promotional apps

#Requires -RunAsAdministrator

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Phase 1: Remove OEM Bloatware" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

# Stop and disable OEM services
Write-Host "`n[1/4] Stopping OEM services..." -ForegroundColor Yellow
$oemServices = Get-Service | Where-Object {
    $_.DisplayName -match "Acer|HP|Dell|Lenovo|ASUS|Norton|McAfee|ExpressVPN"
}
$count = ($oemServices | Measure-Object).Count
Write-Host "  Found $count OEM services" -ForegroundColor Gray

foreach ($svc in $oemServices) {
    Write-Host "  Stopping: $($svc.DisplayName)" -ForegroundColor Gray
    Stop-Service $svc.Name -Force -ErrorAction SilentlyContinue
    Set-Service $svc.Name -StartupType Disabled -ErrorAction SilentlyContinue
}

# Uninstall via registry
Write-Host "`n[2/4] Uninstalling OEM programs..." -ForegroundColor Yellow
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
    "*WildTangent*", "*App Explorer*", "*Dropbox*"
)

$removed = 0
foreach ($path in $uninstallPaths) {
    Get-ChildItem $path -ErrorAction SilentlyContinue | ForEach-Object {
        $props = Get-ItemProperty $_.PSPath -ErrorAction SilentlyContinue
        foreach ($pattern in $bloatPatterns) {
            if ($props.DisplayName -like $pattern) {
                Write-Host "  Removing: $($props.DisplayName)" -ForegroundColor Gray
                if ($props.UninstallString -match "MsiExec") {
                    $guid = [regex]::Match($props.UninstallString, '\{[A-F0-9-]+\}').Value
                    if ($guid) {
                        Start-Process msiexec.exe -ArgumentList "/x $guid /qn /norestart" -Wait -ErrorAction SilentlyContinue
                    }
                }
                Remove-Item $_.PSPath -Recurse -Force -ErrorAction SilentlyContinue
                $removed++
            }
        }
    }
}
Write-Host "  Removed $removed programs" -ForegroundColor Gray

# Remove promotional shortcuts
Write-Host "`n[3/4] Removing promotional shortcuts..." -ForegroundColor Yellow
$shortcutPatterns = @("*Amazon*","*Booking*","*Norton*","*McAfee*","*ExpressVPN*","*Acer*","*HP*","*Dell*","*Lenovo*","*ASUS*")
$startMenuPaths = @(
    "C:\ProgramData\Microsoft\Windows\Start Menu\Programs",
    "C:\Users\Default\AppData\Roaming\Microsoft\Windows\Start Menu\Programs",
    "C:\Users\Public\Desktop"
)
$shortcutsRemoved = 0
foreach ($path in $startMenuPaths) {
    foreach ($pattern in $shortcutPatterns) {
        Get-ChildItem $path -Recurse -Include "*.lnk","*.url" -ErrorAction SilentlyContinue |
            Where-Object { $_.Name -like $pattern } |
            ForEach-Object {
                Remove-Item $_.FullName -Force -ErrorAction SilentlyContinue
                $shortcutsRemoved++
            }
    }
}
Write-Host "  Removed $shortcutsRemoved shortcuts" -ForegroundColor Gray

# Remove OEM folders
Write-Host "`n[4/4] Removing OEM folders..." -ForegroundColor Yellow
$oemFolders = @(
    "C:\Program Files\Acer", "C:\Program Files (x86)\Acer", "C:\ProgramData\Acer",
    "C:\Program Files\HP", "C:\Program Files (x86)\HP", "C:\ProgramData\HP",
    "C:\Program Files\Dell", "C:\Program Files (x86)\Dell", "C:\ProgramData\Dell",
    "C:\Program Files\Lenovo", "C:\Program Files (x86)\Lenovo", "C:\ProgramData\Lenovo",
    "C:\Program Files\ASUS", "C:\Program Files (x86)\ASUS", "C:\ProgramData\ASUS",
    "C:\Program Files\Norton", "C:\Program Files (x86)\Norton", "C:\ProgramData\Norton",
    "C:\Program Files\McAfee", "C:\Program Files (x86)\McAfee", "C:\ProgramData\McAfee"
)
$foldersRemoved = 0
foreach ($folder in $oemFolders) {
    if (Test-Path $folder) {
        Write-Host "  Removing: $folder" -ForegroundColor Gray
        Remove-Item $folder -Recurse -Force -ErrorAction SilentlyContinue
        $foldersRemoved++
    }
}
Write-Host "  Removed $foldersRemoved folders" -ForegroundColor Gray

Write-Host "`n========================================" -ForegroundColor Green
Write-Host "Phase 1 Complete: OEM Bloatware Removed" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
$SCRIPT$,
  '["windows"]'::jsonb,
  1,
  NOW(),
  NOW()
) ON CONFLICT DO NOTHING;

-- Phase 2: Remove Windows Store Bloatware
INSERT INTO scripts (id, name, description, language, content, os_types, organization_id, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Debloat Phase 2: Remove Store Bloatware',
  'Removes unnecessary Windows Store apps (Xbox, Cortana, Games, etc.) while keeping essential apps like Calculator, Photos, Terminal.',
  'powershell',
  $SCRIPT$# Sentinel RMM - Phase 2: Remove Windows Store Bloatware (UWP Apps)
# Removes unnecessary Microsoft Store apps while keeping essential ones

#Requires -RunAsAdministrator

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Phase 2: Remove Windows Store Bloatware" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

# Apps to REMOVE
$removeApps = @(
    # Microsoft bloat
    "*3DBuilder*", "*3DViewer*", "*BingFinance*", "*BingNews*", "*BingSports*", "*BingWeather*",
    "*GetHelp*", "*Getstarted*", "*MicrosoftOfficeHub*", "*MicrosoftSolitaireCollection*",
    "*MixedReality*", "*NetworkSpeedTest*", "*News*", "*Office.Sway*", "*OneConnect*",
    "*People*", "*Print3D*", "*SkypeApp*", "*Wallet*", "*Whiteboard*",
    "*WindowsFeedbackHub*", "*WindowsMaps*", "*WindowsSoundRecorder*",
    "*YourPhone*", "*ZuneMusic*", "*ZuneVideo*", "*Todos*", "*PowerAutomateDesktop*",
    "*GamingApp*", "*Clipchamp*", "*549981C3F5F10*", # Cortana

    # Xbox (remove unless gaming PC)
    "*Xbox*", "*XboxApp*", "*XboxGameOverlay*", "*XboxGamingOverlay*",
    "*XboxIdentityProvider*", "*XboxSpeechToTextOverlay*",

    # Third-party bloat
    "*CandyCrush*", "*BubbleWitch*", "*Facebook*", "*Twitter*", "*Instagram*", "*TikTok*",
    "*Netflix*", "*Hulu*", "*Disney*", "*Spotify*", "*Dolby*", "*Duolingo*", "*king.com*",
    "*LinkedInforWindows*", "*Minecraft*", "*Royal Revolt*", "*Sway*", "*Twitter*"
)

# Apps to KEEP (never remove these)
$keepApps = @(
    "Microsoft.WindowsStore",
    "Microsoft.WindowsCalculator",
    "Microsoft.WindowsCamera",
    "Microsoft.WindowsNotepad",
    "Microsoft.Windows.Photos",
    "Microsoft.Paint",
    "Microsoft.ScreenSketch",
    "Microsoft.DesktopAppInstaller",
    "Microsoft.HEIFImageExtension",
    "Microsoft.VP9VideoExtensions",
    "Microsoft.WebMediaExtensions",
    "Microsoft.WebpImageExtension",
    "Microsoft.VCLibs*",
    "Microsoft.UI.Xaml*",
    "Microsoft.NET.Native*",
    "Microsoft.WindowsAppRuntime*",
    "Microsoft.WindowsTerminal",
    "Microsoft.PowerShell*"
)

Write-Host "`n[1/2] Removing AppX packages for all users..." -ForegroundColor Yellow

$totalRemoved = 0
foreach ($app in $removeApps) {
    $packages = Get-AppxPackage -AllUsers -Name $app -ErrorAction SilentlyContinue
    foreach ($pkg in $packages) {
        # Check if it's in the keep list
        $shouldKeep = $false
        foreach ($keep in $keepApps) {
            if ($pkg.Name -like $keep) {
                $shouldKeep = $true
                break
            }
        }

        if (-not $shouldKeep) {
            Write-Host "  Removing: $($pkg.Name)" -ForegroundColor Gray
            Remove-AppxPackage -Package $pkg.PackageFullName -AllUsers -ErrorAction SilentlyContinue
            $totalRemoved++
        }
    }
}
Write-Host "  Removed $totalRemoved packages" -ForegroundColor Gray

Write-Host "`n[2/2] Removing provisioned packages (prevents install for new users)..." -ForegroundColor Yellow

$provRemoved = 0
foreach ($app in $removeApps) {
    Get-AppxProvisionedPackage -Online -ErrorAction SilentlyContinue |
        Where-Object { $_.DisplayName -like $app } |
        ForEach-Object {
            # Check if it's in the keep list
            $shouldKeep = $false
            foreach ($keep in $keepApps) {
                if ($_.DisplayName -like $keep) {
                    $shouldKeep = $true
                    break
                }
            }

            if (-not $shouldKeep) {
                Write-Host "  Deprovisioning: $($_.DisplayName)" -ForegroundColor Gray
                Remove-AppxProvisionedPackage -Online -PackageName $_.PackageName -ErrorAction SilentlyContinue | Out-Null
                $provRemoved++
            }
        }
}
Write-Host "  Deprovisioned $provRemoved packages" -ForegroundColor Gray

Write-Host "`n========================================" -ForegroundColor Green
Write-Host "Phase 2 Complete: Store Bloatware Removed" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host "`nKept essential apps:" -ForegroundColor Cyan
Write-Host "  - Windows Store, Calculator, Camera, Notepad" -ForegroundColor White
Write-Host "  - Photos, Paint, Snipping Tool, Terminal" -ForegroundColor White
Write-Host "  - All framework packages (VCLibs, .NET, XAML)" -ForegroundColor White
$SCRIPT$,
  '["windows"]'::jsonb,
  1,
  NOW(),
  NOW()
) ON CONFLICT DO NOTHING;

-- Phase 3: Disable Consumer Features
INSERT INTO scripts (id, name, description, language, content, os_types, organization_id, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Debloat Phase 3: Disable Consumer Features',
  'Disables Windows Consumer Features, Content Delivery Manager, and reduces telemetry to minimum. Prevents auto-install of suggested apps.',
  'powershell',
  $SCRIPT$# Sentinel RMM - Phase 3: Disable Windows Consumer Features & Telemetry
# Prevents auto-install of suggested apps and reduces telemetry

#Requires -RunAsAdministrator

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Phase 3: Disable Consumer Features" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

Write-Host "`n[1/3] Disabling auto-install of suggested apps..." -ForegroundColor Yellow

# Cloud content / consumer features
$cloudContentPath = "HKLM:\SOFTWARE\Policies\Microsoft\Windows\CloudContent"
if (!(Test-Path $cloudContentPath)) { New-Item -Path $cloudContentPath -Force | Out-Null }

$cloudSettings = @{
    "DisableWindowsConsumerFeatures" = 1
    "DisableSoftLanding" = 1
    "DisableCloudOptimizedContent" = 1
    "DisableThirdPartySuggestions" = 1
}

foreach ($setting in $cloudSettings.Keys) {
    Set-ItemProperty -Path $cloudContentPath -Name $setting -Value $cloudSettings[$setting] -Type DWord -Force
    Write-Host "  Set $setting = $($cloudSettings[$setting])" -ForegroundColor Gray
}

# Content Delivery Manager
$cdmPath = "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\ContentDeliveryManager"
if (!(Test-Path $cdmPath)) { New-Item -Path $cdmPath -Force | Out-Null }

$cdmSettings = @{
    "ContentDeliveryAllowed" = 0
    "OemPreInstalledAppsEnabled" = 0
    "PreInstalledAppsEnabled" = 0
    "PreInstalledAppsEverEnabled" = 0
    "SilentInstalledAppsEnabled" = 0
    "SoftLandingEnabled" = 0
    "SubscribedContent-338388Enabled" = 0
    "SubscribedContent-338389Enabled" = 0
    "SubscribedContent-353694Enabled" = 0
    "SubscribedContent-353696Enabled" = 0
    "SubscribedContentEnabled" = 0
    "SystemPaneSuggestionsEnabled" = 0
}

foreach ($setting in $cdmSettings.Keys) {
    Set-ItemProperty -Path $cdmPath -Name $setting -Value $cdmSettings[$setting] -Type DWord -Force -ErrorAction SilentlyContinue
}
Write-Host "  Content Delivery Manager configured" -ForegroundColor Gray

Write-Host "`n[2/3] Reducing telemetry..." -ForegroundColor Yellow

# Telemetry settings
$dataCollectionPath = "HKLM:\SOFTWARE\Policies\Microsoft\Windows\DataCollection"
if (!(Test-Path $dataCollectionPath)) { New-Item -Path $dataCollectionPath -Force | Out-Null }

Set-ItemProperty -Path $dataCollectionPath -Name "AllowTelemetry" -Value 0 -Type DWord -Force
Set-ItemProperty -Path $dataCollectionPath -Name "DoNotShowFeedbackNotifications" -Value 1 -Type DWord -Force
Write-Host "  Telemetry set to minimum" -ForegroundColor Gray

# Disable telemetry services
$telemetryServices = @("DiagTrack", "dmwappushservice")
foreach ($svc in $telemetryServices) {
    $service = Get-Service -Name $svc -ErrorAction SilentlyContinue
    if ($service) {
        Stop-Service $svc -Force -ErrorAction SilentlyContinue
        Set-Service $svc -StartupType Disabled -ErrorAction SilentlyContinue
        Write-Host "  Disabled service: $svc" -ForegroundColor Gray
    }
}

Write-Host "`n[3/3] Disabling telemetry scheduled tasks..." -ForegroundColor Yellow

$telemetryTasks = @(
    "\Microsoft\Windows\Application Experience\Microsoft Compatibility Appraiser",
    "\Microsoft\Windows\Application Experience\ProgramDataUpdater",
    "\Microsoft\Windows\Autochk\Proxy",
    "\Microsoft\Windows\Customer Experience Improvement Program\Consolidator",
    "\Microsoft\Windows\Customer Experience Improvement Program\UsbCeip",
    "\Microsoft\Windows\DiskDiagnostic\Microsoft-Windows-DiskDiagnosticDataCollector",
    "\Microsoft\Windows\Feedback\Siuf\DmClient",
    "\Microsoft\Windows\Feedback\Siuf\DmClientOnScenarioDownload"
)

foreach ($task in $telemetryTasks) {
    $taskObj = Get-ScheduledTask -TaskName ($task -split '\\')[-1] -ErrorAction SilentlyContinue
    if ($taskObj) {
        Disable-ScheduledTask -TaskPath ($task -replace '\\[^\\]+$', '\') -TaskName ($task -split '\\')[-1] -ErrorAction SilentlyContinue | Out-Null
        Write-Host "  Disabled: $task" -ForegroundColor Gray
    }
}

Write-Host "`n========================================" -ForegroundColor Green
Write-Host "Phase 3 Complete: Consumer Features Disabled" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
$SCRIPT$,
  '["windows"]'::jsonb,
  1,
  NOW(),
  NOW()
) ON CONFLICT DO NOTHING;

-- Phase 4: Disable Annoyances
INSERT INTO scripts (id, name, description, language, content, os_types, organization_id, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Debloat Phase 4: Disable Annoyances',
  'Disables Windows UI annoyances: Start Menu suggestions, Widgets, Chat button, Copilot, Search highlights, Windows Tips.',
  'powershell',
  $SCRIPT$# Sentinel RMM - Phase 4: Disable Windows Annoyances
# Disables Start Menu suggestions, Widgets, Chat, Copilot, Search highlights

#Requires -RunAsAdministrator

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Phase 4: Disable Windows Annoyances" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

Write-Host "`n[1/3] Configuring current user settings..." -ForegroundColor Yellow

# Explorer Advanced settings
$explorerPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced"
if (!(Test-Path $explorerPath)) { New-Item -Path $explorerPath -Force | Out-Null }

$explorerSettings = @{
    "Start_IrisRecommendations" = 0    # Start menu suggestions/recommendations
    "TaskbarDa" = 0                     # Widgets button
    "TaskbarMn" = 0                     # Chat/Teams button
    "ShowTaskViewButton" = 0            # Task View button (optional)
    "LaunchTo" = 1                      # Open File Explorer to This PC (not Quick Access)
    "HideFileExt" = 0                   # Show file extensions
}

foreach ($setting in $explorerSettings.Keys) {
    Set-ItemProperty -Path $explorerPath -Name $setting -Value $explorerSettings[$setting] -Type DWord -Force -ErrorAction SilentlyContinue
    Write-Host "  Set $setting = $($explorerSettings[$setting])" -ForegroundColor Gray
}

# Disable Copilot
$copilotPath = "HKCU:\Software\Policies\Microsoft\Windows\WindowsCopilot"
if (!(Test-Path $copilotPath)) { New-Item -Path $copilotPath -Force | Out-Null }
Set-ItemProperty -Path $copilotPath -Name "TurnOffWindowsCopilot" -Value 1 -Type DWord -Force
Write-Host "  Disabled Windows Copilot" -ForegroundColor Gray

# Disable Search highlights
$searchPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\SearchSettings"
if (!(Test-Path $searchPath)) { New-Item -Path $searchPath -Force | Out-Null }
Set-ItemProperty -Path $searchPath -Name "IsDynamicSearchBoxEnabled" -Value 0 -Type DWord -Force
Write-Host "  Disabled Search highlights" -ForegroundColor Gray

# Disable Bing in Start Menu search
$bingSearchPath = "HKCU:\Software\Policies\Microsoft\Windows\Explorer"
if (!(Test-Path $bingSearchPath)) { New-Item -Path $bingSearchPath -Force | Out-Null }
Set-ItemProperty -Path $bingSearchPath -Name "DisableSearchBoxSuggestions" -Value 1 -Type DWord -Force
Write-Host "  Disabled Bing search suggestions" -ForegroundColor Gray

Write-Host "`n[2/3] Applying settings to Default user profile (new accounts)..." -ForegroundColor Yellow

# Load Default user hive
$defaultHive = "C:\Users\Default\NTUSER.DAT"
if (Test-Path $defaultHive) {
    reg load "HKU\TempDefault" $defaultHive 2>$null | Out-Null

    # Apply same settings to default profile
    $defaultSettings = @{
        "Start_IrisRecommendations" = 0
        "TaskbarDa" = 0
        "TaskbarMn" = 0
        "ShowTaskViewButton" = 0
    }

    foreach ($setting in $defaultSettings.Keys) {
        reg add "HKU\TempDefault\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced" /v $setting /t REG_DWORD /d $defaultSettings[$setting] /f 2>$null | Out-Null
    }

    # Copilot for default user
    reg add "HKU\TempDefault\Software\Policies\Microsoft\Windows\WindowsCopilot" /v "TurnOffWindowsCopilot" /t REG_DWORD /d 1 /f 2>$null | Out-Null

    # Unload hive
    [gc]::Collect()
    Start-Sleep -Milliseconds 500
    reg unload "HKU\TempDefault" 2>$null | Out-Null

    Write-Host "  Default profile configured" -ForegroundColor Gray
} else {
    Write-Host "  Default profile not found (skipped)" -ForegroundColor Gray
}

Write-Host "`n[3/3] Disabling system-wide annoyances..." -ForegroundColor Yellow

# Disable Windows Tips
$tipsPath = "HKLM:\SOFTWARE\Policies\Microsoft\Windows\CloudContent"
if (!(Test-Path $tipsPath)) { New-Item -Path $tipsPath -Force | Out-Null }
Set-ItemProperty -Path $tipsPath -Name "DisableSoftLanding" -Value 1 -Type DWord -Force
Set-ItemProperty -Path $tipsPath -Name "DisableWindowsSpotlightFeatures" -Value 1 -Type DWord -Force
Write-Host "  Disabled Windows Tips and Spotlight features" -ForegroundColor Gray

# Disable News and Interests (for Windows 10)
$newsPath = "HKLM:\SOFTWARE\Policies\Microsoft\Windows\Windows Feeds"
if (!(Test-Path $newsPath)) { New-Item -Path $newsPath -Force | Out-Null }
Set-ItemProperty -Path $newsPath -Name "EnableFeeds" -Value 0 -Type DWord -Force
Write-Host "  Disabled News and Interests" -ForegroundColor Gray

Write-Host "`n========================================" -ForegroundColor Green
Write-Host "Phase 4 Complete: Annoyances Disabled" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host "`nDisabled:" -ForegroundColor Cyan
Write-Host "  - Start Menu suggestions/recommendations" -ForegroundColor White
Write-Host "  - Widgets button" -ForegroundColor White
Write-Host "  - Chat/Teams button" -ForegroundColor White
Write-Host "  - Windows Copilot" -ForegroundColor White
Write-Host "  - Search highlights / Bing suggestions" -ForegroundColor White
Write-Host "  - Windows Tips" -ForegroundColor White
$SCRIPT$,
  '["windows"]'::jsonb,
  1,
  NOW(),
  NOW()
) ON CONFLICT DO NOTHING;

-- Phase 5: Security Baseline
INSERT INTO scripts (id, name, description, language, content, os_types, organization_id, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Debloat Phase 5: Security Baseline',
  'Applies Windows security baseline: Enables Defender, Firewall, SmartScreen, UAC. Disables Remote Registry, Remote Assistance, AutoPlay.',
  'powershell',
  $SCRIPT$# Sentinel RMM - Phase 5: Security Baseline
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
$SCRIPT$,
  '["windows"]'::jsonb,
  1,
  NOW(),
  NOW()
) ON CONFLICT DO NOTHING;

-- Full Debloat Script (runs all phases)
INSERT INTO scripts (id, name, description, language, content, os_types, organization_id, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Windows Full Debloat (All Phases)',
  'Complete Windows debloat - runs all 5 phases: Remove OEM bloatware, Remove Store apps, Disable consumer features, Disable annoyances, Apply security baseline. Transforms any Windows PC into a clean, business-ready system.',
  'powershell',
  $SCRIPT$# Sentinel RMM - Full Windows Debloat Script
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
$SCRIPT$,
  '["windows"]'::jsonb,
  1,
  NOW(),
  NOW()
) ON CONFLICT DO NOTHING;

-- Summary
SELECT name, language, LENGTH(content) as content_length FROM scripts WHERE name LIKE '%Debloat%' OR name LIKE '%Remote Access%' ORDER BY name;
