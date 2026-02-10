# Sentinel RMM - Phase 3: Disable Windows Consumer Features & Telemetry
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
