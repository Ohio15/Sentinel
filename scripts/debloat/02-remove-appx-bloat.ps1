# Sentinel RMM - Phase 2: Remove Windows Store Bloatware (UWP Apps)
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
