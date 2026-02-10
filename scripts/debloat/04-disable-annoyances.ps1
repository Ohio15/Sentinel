# Sentinel RMM - Phase 4: Disable Windows Annoyances
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
