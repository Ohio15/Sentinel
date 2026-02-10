# Sentinel RMM - Phase 1: Remove OEM/Manufacturer Bloatware
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
