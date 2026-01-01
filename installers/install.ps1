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

# Do not auto-exit on errors - we want to show them to the user
$ErrorActionPreference = "Continue"
$ProgressPreference = "SilentlyContinue"

# Helper to pause before exit so user can see output
function Wait-BeforeExit {
    param([int]$ExitCode = 0)
    if (-not $Silent) {
        Write-Host ""
        Write-Host "Press any key to close this window..." -ForegroundColor Gray
        try {
            $null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")
        } catch {
            Read-Host "Press Enter to close"
        }
    }
    exit $ExitCode
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

function Write-Error {
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
    if (-not $Silent) {
        Show-Banner
    }

    # Check for administrator privileges
    if (-not (Test-Administrator)) {
        Write-Error "Administrator privileges required!"
        Write-Host ""
        Write-Host "Please run this script as Administrator:" -ForegroundColor Yellow
        Write-Host "  Right-click PowerShell -> Run as Administrator" -ForegroundColor Gray
        Write-Host ""
        Wait-BeforeExit 1
    }

    # Validate parameters
    if ([string]::IsNullOrEmpty($Server)) {
        # Check environment variable
        if ($env:SENTINEL_SERVER) {
            $Server = $env:SENTINEL_SERVER
        } else {
            Write-Error "Server URL is required!"
            Write-Host ""
            Write-Host "Usage:" -ForegroundColor Yellow
            Write-Host "  .\install.ps1 -Server 'http://your-server:8080' -Token 'your-token'" -ForegroundColor Gray
            Write-Host ""
            Write-Host "Or set environment variable:" -ForegroundColor Yellow
            Write-Host "  `$env:SENTINEL_SERVER = 'http://your-server:8080'" -ForegroundColor Gray
            Write-Host ""
            Wait-BeforeExit 1
        }
    }

    if ([string]::IsNullOrEmpty($Token) -and -not $Repair -and -not $Verify) {
        # Check environment variable
        if ($env:SENTINEL_TOKEN) {
            $Token = $env:SENTINEL_TOKEN
        } else {
            Write-Error "Enrollment token is required!"
            Write-Host ""
            Write-Host "Get your token from the Sentinel dashboard:" -ForegroundColor Yellow
            Write-Host "  Settings -> Enrollment -> Generate Token" -ForegroundColor Gray
            Write-Host ""
            Wait-BeforeExit 1
        }
    }

    # Check for existing installation
    $existingAgent = Get-InstalledAgentPath
    if ($existingAgent -and -not $Force -and -not $Repair -and -not $Verify) {
        Write-Host ""
        Write-Host "Sentinel Agent is already installed at:" -ForegroundColor Yellow
        Write-Host "  $existingAgent" -ForegroundColor Gray
        Write-Host ""
        Write-Host "Options:" -ForegroundColor Yellow
        Write-Host "  -Force   : Reinstall the agent" -ForegroundColor Gray
        Write-Host "  -Repair  : Repair the installation" -ForegroundColor Gray
        Write-Host "  -Verify  : Verify installation integrity" -ForegroundColor Gray
        Write-Host ""
        Wait-BeforeExit 0
    }

    $bootstrapperUrl = "$Server/api/bootstrap/download?platform=windows&arch=amd64"
    if (-not [string]::IsNullOrEmpty($Token)) {
        $bootstrapperUrl += "&token=$Token"
    }

    $bootstrapperPath = Get-BootstrapperPath

    try {
        # Download bootstrapper
        Write-Step "Downloading Sentinel bootstrapper..."

        $webClient = New-Object System.Net.WebClient
        $webClient.Headers.Add("User-Agent", "Sentinel-Installer/1.0")

        try {
            $webClient.DownloadFile($bootstrapperUrl, $bootstrapperPath)
        } catch {
            if ($_.Exception.InnerException -is [System.Net.WebException]) {
                $response = $_.Exception.InnerException.Response
                if ($response) {
                    $statusCode = [int]$response.StatusCode
                    if ($statusCode -eq 401 -or $statusCode -eq 403) {
                        Write-Error "Invalid or expired enrollment token"
                        Wait-BeforeExit 1
                    }
                }
            }
            throw
        }

        if (-not (Test-Path $bootstrapperPath)) {
            throw "Download failed - file not found"
        }

        $fileSize = (Get-Item $bootstrapperPath).Length
        Write-Success "Downloaded bootstrapper ($([math]::Round($fileSize/1KB, 0)) KB)"

        # Prepare arguments
        $args = @("--server=$Server")

        if (-not [string]::IsNullOrEmpty($Token)) {
            $args += "--token=$Token"
        }

        if ($Silent) {
            $args += "--silent"
        }

        if ($Force) {
            $args += "--force"
        }

        if ($Repair) {
            $args += "--repair"
        }

        if ($Verify) {
            $args += "--verify"
        }

        # Run bootstrapper
        Write-Step "Running bootstrapper..."

        $process = Start-Process -FilePath $bootstrapperPath -ArgumentList $args -Wait -PassThru -NoNewWindow

        if ($process.ExitCode -ne 0) {
            throw "Bootstrapper exited with code $($process.ExitCode)"
        }

        Write-Success "Installation completed successfully!"

    } catch {
        Write-Error "Installation failed: $_"
        Wait-BeforeExit 1
    } finally {
        # Cleanup
        if (Test-Path $bootstrapperPath) {
            Remove-Item $bootstrapperPath -Force -ErrorAction SilentlyContinue
        }
    }

    Write-Host ""
    Write-Host "The Sentinel Agent is now running and will start automatically" -ForegroundColor Green
    Write-Host "when Windows boots." -ForegroundColor Green
    Write-Host ""
}

# Run the installation
Install-SentinelAgent
