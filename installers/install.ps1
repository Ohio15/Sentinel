# Sentinel Agent Installation Script for Windows
# Usage: irm https://your-server/install.ps1 | iex
# Or with parameters: .\install.ps1 -Server "http://server:8080" -Token "your-token"

param(
    [string]$Server = "",
    [string]$Token = "",
    [switch]$Silent,
    [switch]$Force,
    [switch]$Repair,
    [switch]$Verify
)

# IMPORTANT: Keep window open on any error
trap {
    Write-Host ""
    Write-Host "ERROR: $_" -ForegroundColor Red
    Write-Host ""
    Write-Host "Press any key to close..." -ForegroundColor Gray
    $null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown") 2>$null
    if (-not $?) { cmd /c pause }
    exit 1
}

# Do not auto-exit on errors - we want to show them to the user
$ErrorActionPreference = "Stop"

# Global progress tracking
$script:TotalSteps = 6
$script:CurrentStep = 0

# Reliable pause function that works in all contexts
function Pause-Script {
    if ($Silent) { return }

    Write-Host ""
    Write-Host "Press any key to close this window..." -ForegroundColor Cyan

    # Try multiple methods to pause
    try {
        $null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")
    } catch {
        try {
            Read-Host "Press Enter to close"
        } catch {
            # Last resort - use cmd pause
            cmd /c pause
        }
    }
}

# Console progress bar with npm/docker style
function Show-Progress {
    param(
        [int]$Step,
        [int]$Total,
        [string]$Status,
        [string]$Detail = ""
    )

    if ($Silent) { return }

    $percent = [math]::Round(($Step / $Total) * 100)
    $barWidth = 40
    $filled = [math]::Round($barWidth * $Step / $Total)
    $empty = $barWidth - $filled

    # Use simple characters for compatibility
    $bar = ("=" * $filled) + ("-" * $empty)

    Write-Host ""
    Write-Host "  [$bar] $percent%" -ForegroundColor Cyan
    Write-Host "  $Status" -ForegroundColor White
    if ($Detail) {
        Write-Host "  $Detail" -ForegroundColor Gray
    }
}

function Update-InstallProgress {
    param(
        [string]$Status,
        [string]$Detail = ""
    )

    $script:CurrentStep++
    Show-Progress -Step $script:CurrentStep -Total $script:TotalSteps -Status $Status -Detail $Detail

    # Also update Windows progress bar
    if (-not $Silent) {
        $percent = [math]::Round(($script:CurrentStep / $script:TotalSteps) * 100)
        Write-Progress -Activity "Installing Sentinel Agent" -Status $Status -PercentComplete $percent -CurrentOperation $Detail
    }
}

# Finish dialog
function Show-FinishDialog {
    param(
        [bool]$Success = $true,
        [string]$ErrorMessage = ""
    )

    # Close the Write-Progress bar
    Write-Progress -Activity "Installing Sentinel Agent" -Completed

    Write-Host ""
    Write-Host ""

    if ($Success) {
        Write-Host "  ================================================================" -ForegroundColor Green
        Write-Host "  =                                                              =" -ForegroundColor Green
        Write-Host "  =          INSTALLATION COMPLETED SUCCESSFULLY                 =" -ForegroundColor Green
        Write-Host "  =                                                              =" -ForegroundColor Green
        Write-Host "  ================================================================" -ForegroundColor Green
        Write-Host ""
        Write-Host "  The Sentinel Agent is now running and will start automatically" -ForegroundColor White
        Write-Host "  when Windows boots." -ForegroundColor White
        Write-Host ""
        Write-Host "  Services installed:" -ForegroundColor DarkGray
        Write-Host "    - SentinelAgent    (Remote Monitoring Service)" -ForegroundColor Gray
        Write-Host "    - SentinelWatchdog (Auto-recovery Service)" -ForegroundColor Gray
        Write-Host ""
    } else {
        Write-Host "  ================================================================" -ForegroundColor Red
        Write-Host "  =                                                              =" -ForegroundColor Red
        Write-Host "  =                   INSTALLATION FAILED                        =" -ForegroundColor Red
        Write-Host "  =                                                              =" -ForegroundColor Red
        Write-Host "  ================================================================" -ForegroundColor Red
        Write-Host ""
        if ($ErrorMessage) {
            Write-Host "  Error: $ErrorMessage" -ForegroundColor Yellow
            Write-Host ""
        }
        Write-Host "  Troubleshooting:" -ForegroundColor DarkGray
        Write-Host "    - Ensure you have administrator privileges" -ForegroundColor Gray
        Write-Host "    - Check that the server URL is accessible" -ForegroundColor Gray
        Write-Host "    - Verify the enrollment token is valid" -ForegroundColor Gray
        Write-Host ""
    }

    if (-not $Silent) {
        # Try to show a Windows Forms finish button dialog
        $showedForm = $false
        try {
            Add-Type -AssemblyName System.Windows.Forms -ErrorAction Stop
            Add-Type -AssemblyName System.Drawing -ErrorAction Stop

            $form = New-Object System.Windows.Forms.Form
            $form.Text = "Sentinel Agent Installation"
            $form.Size = New-Object System.Drawing.Size(450, 220)
            $form.StartPosition = "CenterScreen"
            $form.FormBorderStyle = "FixedDialog"
            $form.MaximizeBox = $false
            $form.MinimizeBox = $false
            $form.TopMost = $true

            # Icon based on success/failure
            $iconBox = New-Object System.Windows.Forms.PictureBox
            $iconBox.Location = New-Object System.Drawing.Point(20, 25)
            $iconBox.Size = New-Object System.Drawing.Size(48, 48)
            if ($Success) {
                $iconBox.Image = [System.Drawing.SystemIcons]::Information.ToBitmap()
                $form.BackColor = [System.Drawing.Color]::FromArgb(240, 255, 240)
            } else {
                $iconBox.Image = [System.Drawing.SystemIcons]::Error.ToBitmap()
                $form.BackColor = [System.Drawing.Color]::FromArgb(255, 245, 245)
            }

            $label = New-Object System.Windows.Forms.Label
            $label.Location = New-Object System.Drawing.Point(80, 25)
            $label.Size = New-Object System.Drawing.Size(340, 80)
            $label.Font = New-Object System.Drawing.Font("Segoe UI", 10)

            if ($Success) {
                $label.Text = "Sentinel Agent has been installed successfully!`n`nThe agent is now running and monitoring this device.`nIt will start automatically when Windows boots."
            } else {
                $shortError = if ($ErrorMessage.Length -gt 100) { $ErrorMessage.Substring(0, 97) + "..." } else { $ErrorMessage }
                $label.Text = "Installation failed.`n`n$shortError"
            }

            $button = New-Object System.Windows.Forms.Button
            $button.Location = New-Object System.Drawing.Point(175, 130)
            $button.Size = New-Object System.Drawing.Size(100, 35)
            $button.Text = "Finish"
            $button.Font = New-Object System.Drawing.Font("Segoe UI", 10)
            $button.BackColor = [System.Drawing.Color]::FromArgb(0, 120, 212)
            $button.ForeColor = [System.Drawing.Color]::White
            $button.FlatStyle = "Flat"
            $button.DialogResult = [System.Windows.Forms.DialogResult]::OK
            $button.Cursor = [System.Windows.Forms.Cursors]::Hand

            $form.AcceptButton = $button
            $form.Controls.Add($iconBox)
            $form.Controls.Add($label)
            $form.Controls.Add($button)

            [void]$form.ShowDialog()
            $form.Dispose()
            $showedForm = $true
        } catch {
            # Windows Forms not available, fall through to console
        }

        if (-not $showedForm) {
            Pause-Script
        }
    }

    exit $(if ($Success) { 0 } else { 1 })
}

# Helper to exit with finish dialog
function Wait-BeforeExit {
    param(
        [int]$ExitCode = 0,
        [string]$ErrorMessage = ""
    )
    Show-FinishDialog -Success ($ExitCode -eq 0) -ErrorMessage $ErrorMessage
}

# ASCII Banner
function Show-Banner {
    Write-Host ""
    Write-Host "  ____             _   _            _ " -ForegroundColor Cyan
    Write-Host " / ___|  ___ _ __ | |_(_)_ __   ___| |" -ForegroundColor Cyan
    Write-Host " \___ \ / _ \ '_ \| __| | '_ \ / _ \ |" -ForegroundColor Cyan
    Write-Host "  ___) |  __/ | | | |_| | | | |  __/ |" -ForegroundColor Cyan
    Write-Host " |____/ \___|_| |_|\__|_|_| |_|\___|_|" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "       Remote Monitoring & Management" -ForegroundColor DarkCyan
    Write-Host ""
}

function Write-Step {
    param([string]$Message)
    Write-Host "[*] " -NoNewline -ForegroundColor Yellow
    Write-Host $Message
}

function Write-Success {
    param([string]$Message)
    Write-Host "[+] " -NoNewline -ForegroundColor Green
    Write-Host $Message
}

function Write-InstallError {
    param([string]$Message)
    Write-Host "[!] " -NoNewline -ForegroundColor Red
    Write-Host $Message
}

function Test-Administrator {
    $currentUser = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($currentUser)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Get-BootstrapperPath {
    return Join-Path $env:TEMP "sentinel-bootstrap.exe"
}

function Get-InstalledAgentPath {
    $agentPath = Join-Path ${env:ProgramFiles} "Sentinel Agent\sentinel-agent.exe"
    if (Test-Path $agentPath) {
        return $agentPath
    }
    return $null
}

# Main installation function
function Install-SentinelAgent {
    try {
        if (-not $Silent) {
            Show-Banner
        }

        # Step 1: Check administrator privileges
        Update-InstallProgress -Status "Checking administrator privileges..." -Detail "Verifying elevation"

        if (-not (Test-Administrator)) {
            Write-InstallError "Administrator privileges required!"
            Write-Host ""
            Write-Host "Please run this script as Administrator:" -ForegroundColor Yellow
            Write-Host "  Right-click the script -> Run with PowerShell (Admin)" -ForegroundColor Gray
            Write-Host "  Or: Right-click PowerShell -> Run as Administrator" -ForegroundColor Gray
            Write-Host ""
            Wait-BeforeExit -ExitCode 1 -ErrorMessage "Administrator privileges required. Right-click and Run as Administrator."
        }

        Write-Success "Running with administrator privileges"

        # Step 2: Validate parameters
        Update-InstallProgress -Status "Validating configuration..." -Detail "Checking server and token"

        if ([string]::IsNullOrEmpty($Server)) {
            if ($env:SENTINEL_SERVER) {
                $script:Server = $env:SENTINEL_SERVER
            } else {
                Write-InstallError "Server URL is required!"
                Wait-BeforeExit -ExitCode 1 -ErrorMessage "Server URL not provided. The installer script may not have been generated correctly."
            }
        }

        if ([string]::IsNullOrEmpty($Token) -and -not $Repair -and -not $Verify) {
            if ($env:SENTINEL_TOKEN) {
                $script:Token = $env:SENTINEL_TOKEN
            } else {
                Write-InstallError "Enrollment token is required!"
                Wait-BeforeExit -ExitCode 1 -ErrorMessage "Enrollment token not provided. The installer script may not have been generated correctly."
            }
        }

        Write-Success "Configuration validated"
        Write-Host "      Server: $Server" -ForegroundColor Gray

        # Step 3: Check existing installation
        Update-InstallProgress -Status "Checking for existing installation..." -Detail "Scanning program files"

        $existingAgent = Get-InstalledAgentPath
        if ($existingAgent -and -not $Force -and -not $Repair -and -not $Verify) {
            Write-Host ""
            Write-Host "Sentinel Agent is already installed at:" -ForegroundColor Yellow
            Write-Host "  $existingAgent" -ForegroundColor Gray
            Write-Host ""
            Write-Host "Use -Force to reinstall, -Repair to repair, or -Verify to check integrity" -ForegroundColor Gray
            Wait-BeforeExit -ExitCode 0
        }

        if ($existingAgent) {
            Write-Step "Existing installation found - will update"
        } else {
            Write-Success "No existing installation found"
        }

        # Step 4: Download bootstrapper
        Update-InstallProgress -Status "Downloading Sentinel Agent..." -Detail "Connecting to $Server"

        $bootstrapperUrl = "$Server/api/bootstrap/download?platform=windows&arch=amd64"
        if (-not [string]::IsNullOrEmpty($Token)) {
            $bootstrapperUrl += "&token=$Token"
        }

        $bootstrapperPath = Get-BootstrapperPath

        $webClient = New-Object System.Net.WebClient
        $webClient.Headers.Add("User-Agent", "Sentinel-Installer/1.0")

        try {
            $webClient.DownloadFile($bootstrapperUrl, $bootstrapperPath)
        } catch {
            $errorMsg = $_.Exception.Message
            if ($_.Exception.InnerException -is [System.Net.WebException]) {
                $response = $_.Exception.InnerException.Response
                if ($response) {
                    $statusCode = [int]$response.StatusCode
                    if ($statusCode -eq 401 -or $statusCode -eq 403) {
                        Write-InstallError "Invalid or expired enrollment token"
                        Wait-BeforeExit -ExitCode 1 -ErrorMessage "Invalid or expired enrollment token. Generate a new token from the Sentinel dashboard."
                    }
                }
            }
            Write-InstallError "Download failed: $errorMsg"
            Wait-BeforeExit -ExitCode 1 -ErrorMessage "Failed to download agent. Server: $Server - Error: $errorMsg"
        }

        if (-not (Test-Path $bootstrapperPath)) {
            Write-InstallError "Download failed - file not created"
            Wait-BeforeExit -ExitCode 1 -ErrorMessage "Download failed - bootstrapper file was not created"
        }

        $fileSize = (Get-Item $bootstrapperPath).Length
        Write-Success "Downloaded agent ($([math]::Round($fileSize/1MB, 2)) MB)"

        # Step 5: Run bootstrapper/installer
        Update-InstallProgress -Status "Installing Sentinel Agent..." -Detail "Running installer"

        $installArgs = @("--server=$Server")

        if (-not [string]::IsNullOrEmpty($Token)) {
            $installArgs += "--token=$Token"
        }

        if ($Silent) { $installArgs += "--silent" }
        if ($Force) { $installArgs += "--force" }
        if ($Repair) { $installArgs += "--repair" }
        if ($Verify) { $installArgs += "--verify" }

        Write-Step "Running installer..."

        $process = Start-Process -FilePath $bootstrapperPath -ArgumentList $installArgs -Wait -PassThru -NoNewWindow

        # Cleanup downloaded file
        if (Test-Path $bootstrapperPath) {
            Remove-Item $bootstrapperPath -Force -ErrorAction SilentlyContinue
        }

        if ($process.ExitCode -ne 0) {
            Write-InstallError "Installer exited with code $($process.ExitCode)"
            Wait-BeforeExit -ExitCode 1 -ErrorMessage "Installer failed with exit code $($process.ExitCode)"
        }

        Write-Success "Agent installed successfully"

        # Step 6: Verify installation
        Update-InstallProgress -Status "Verifying installation..." -Detail "Checking services"

        Start-Sleep -Seconds 2  # Give services time to start

        $agentService = Get-Service -Name "SentinelAgent" -ErrorAction SilentlyContinue
        $watchdogService = Get-Service -Name "SentinelWatchdog" -ErrorAction SilentlyContinue

        if ($agentService -and $agentService.Status -eq "Running") {
            Write-Success "SentinelAgent service is running"
        } else {
            Write-Step "SentinelAgent service status: $(if ($agentService) { $agentService.Status } else { 'Not found' })"
        }

        if ($watchdogService -and $watchdogService.Status -eq "Running") {
            Write-Success "SentinelWatchdog service is running"
        } else {
            Write-Step "SentinelWatchdog service status: $(if ($watchdogService) { $watchdogService.Status } else { 'Not found' })"
        }

        # Show completion dialog
        Show-FinishDialog -Success $true

    } catch {
        Write-Host ""
        Write-InstallError "Installation failed: $_"
        Wait-BeforeExit -ExitCode 1 -ErrorMessage "$_"
    }
}

# Run the installation
Install-SentinelAgent
