<#
.SYNOPSIS
    Builds the Sentinel RMM Agent installer using Inno Setup

.DESCRIPTION
    This script compiles the Inno Setup installer and optionally embeds
    organization-specific configuration for silent deployment.

.PARAMETER Config
    Path to JSON config file to embed in installer (optional)

.PARAMETER ServerUrl
    Server URL (alternative to config file)

.PARAMETER GrpcEndpoint
    gRPC endpoint (alternative to config file)

.PARAMETER EnrollmentToken
    Enrollment token (alternative to config file)

.PARAMETER OrganizationId
    Organization ID (alternative to config file)

.PARAMETER OutputName
    Output filename (default: sentinel-installer.exe)

.PARAMETER InnoSetupPath
    Path to Inno Setup compiler (auto-detected if not specified)

.PARAMETER SkipBuild
    Skip building agents, use existing binaries

.EXAMPLE
    .\build.ps1
    Builds the base installer template

.EXAMPLE
    .\build.ps1 -ServerUrl "https://sentinelrmm.us" -GrpcEndpoint "sentinelrmm.us:4444" -EnrollmentToken "abc123" -OrganizationId "org-uuid"
    Builds installer with embedded config

.EXAMPLE
    .\build.ps1 -Config ".\my-org-config.json" -OutputName "sentinel-installer-myorg.exe"
    Builds installer with config from file
#>

param(
    [string]$Config,
    [string]$ServerUrl,
    [string]$GrpcEndpoint,
    [string]$EnrollmentToken,
    [string]$OrganizationId,
    [string]$OutputName = "sentinel-installer.exe",
    [string]$InnoSetupPath,
    [switch]$SkipBuild,
    [switch]$Verbose
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent (Split-Path -Parent $ScriptDir)
$ReleaseDir = Join-Path $ProjectRoot "release\agent"
$ResourcesDir = Join-Path $ScriptDir "resources"

# Colors for output
function Write-Info { param($msg) Write-Host "[INFO] $msg" -ForegroundColor Cyan }
function Write-Success { param($msg) Write-Host "[OK] $msg" -ForegroundColor Green }
function Write-Warn { param($msg) Write-Host "[WARN] $msg" -ForegroundColor Yellow }
function Write-Err { param($msg) Write-Host "[ERROR] $msg" -ForegroundColor Red }

# Find Inno Setup compiler
function Find-InnoSetup {
    $possiblePaths = @(
        "C:\Program Files (x86)\Inno Setup 6\ISCC.exe",
        "C:\Program Files\Inno Setup 6\ISCC.exe",
        "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe",
        "D:\Program Files (x86)\Inno Setup 6\ISCC.exe"
    )

    foreach ($path in $possiblePaths) {
        if (Test-Path $path) {
            return $path
        }
    }

    # Try to find via registry
    try {
        $regPath = Get-ItemProperty -Path "HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\Inno Setup 6_is1" -ErrorAction SilentlyContinue
        if ($regPath -and $regPath.InstallLocation) {
            $iscc = Join-Path $regPath.InstallLocation "ISCC.exe"
            if (Test-Path $iscc) { return $iscc }
        }
    } catch {}

    return $null
}

# Create placeholder resources if they don't exist
function Ensure-Resources {
    Write-Info "Checking resources..."

    # Ensure resources directory exists
    if (-not (Test-Path $ResourcesDir)) {
        New-Item -ItemType Directory -Path $ResourcesDir -Force | Out-Null
    }

    # License file
    $licensePath = Join-Path $ResourcesDir "license.rtf"
    if (-not (Test-Path $licensePath)) {
        Write-Warn "Creating placeholder license.rtf"
        $licenseContent = @"
{\rtf1\ansi\deff0
{\fonttbl{\f0\fswiss Helvetica;}}
{\colortbl;\red0\green0\blue0;}
\viewkind4\uc1\pard\cf1\f0\fs24

\b SENTINEL RMM AGENT LICENSE AGREEMENT\b0\par
\par
Version 1.0\par
\par
\b 1. GRANT OF LICENSE\b0\par
\par
This License Agreement ("Agreement") grants you a non-exclusive, non-transferable license to install and use the Sentinel RMM Agent software ("Software") on computers within your organization.\par
\par
\b 2. RESTRICTIONS\b0\par
\par
You may not:\par
- Reverse engineer, decompile, or disassemble the Software\par
- Rent, lease, or lend the Software to third parties\par
- Remove any proprietary notices or labels on the Software\par
- Use the Software for any unlawful purpose\par
\par
\b 3. OWNERSHIP\b0\par
\par
The Software is licensed, not sold. Sentinel RMM retains all ownership rights to the Software.\par
\par
\b 4. TERMINATION\b0\par
\par
This license is effective until terminated. It will terminate automatically if you fail to comply with any term of this Agreement.\par
\par
\b 5. DISCLAIMER OF WARRANTIES\b0\par
\par
THE SOFTWARE IS PROVIDED "AS IS" WITHOUT WARRANTY OF ANY KIND. SENTINEL RMM DISCLAIMS ALL WARRANTIES, EXPRESS OR IMPLIED.\par
\par
\b 6. LIMITATION OF LIABILITY\b0\par
\par
IN NO EVENT SHALL SENTINEL RMM BE LIABLE FOR ANY INDIRECT, INCIDENTAL, SPECIAL, OR CONSEQUENTIAL DAMAGES.\par
\par
\b 7. SUPPORT\b0\par
\par
For support, visit: https://sentinelrmm.us/support\par
\par
By installing this software, you agree to these terms.\par
}
"@
        Set-Content -Path $licensePath -Value $licenseContent -Encoding UTF8
    }

    # Icon file (create placeholder if missing)
    $iconPath = Join-Path $ResourcesDir "sentinel.ico"
    if (-not (Test-Path $iconPath)) {
        Write-Warn "Icon file missing: $iconPath"
        Write-Warn "Please add a sentinel.ico file to the resources directory"

        # Try to copy from app resources
        $appIconPath = Join-Path $ProjectRoot "resources\icon.ico"
        if (Test-Path $appIconPath) {
            Copy-Item $appIconPath $iconPath
            Write-Success "Copied icon from app resources"
        }
    }

    # Wizard images (create simple placeholders if missing)
    $wizardLargePath = Join-Path $ResourcesDir "wizard-large.bmp"
    $wizardSmallPath = Join-Path $ResourcesDir "wizard-small.bmp"

    if (-not (Test-Path $wizardLargePath)) {
        Write-Warn "Wizard large image missing, will use Inno Setup defaults"
    }

    if (-not (Test-Path $wizardSmallPath)) {
        Write-Warn "Wizard small image missing, will use Inno Setup defaults"
    }
}

# Verify agent binaries exist
function Verify-Binaries {
    Write-Info "Verifying agent binaries..."

    $agentPath = Join-Path $ReleaseDir "sentinel-agent.exe"
    $watchdogPath = Join-Path $ReleaseDir "sentinel-watchdog.exe"

    if (-not (Test-Path $agentPath)) {
        throw "Agent binary not found: $agentPath"
    }

    if (-not (Test-Path $watchdogPath)) {
        throw "Watchdog binary not found: $watchdogPath"
    }

    $agentInfo = Get-Item $agentPath
    $watchdogInfo = Get-Item $watchdogPath

    Write-Success "Agent: $($agentInfo.Length / 1MB | ForEach-Object { '{0:N2}' -f $_ }) MB"
    Write-Success "Watchdog: $($watchdogInfo.Length / 1MB | ForEach-Object { '{0:N2}' -f $_ }) MB"
}

# Build config JSON
function Build-ConfigJson {
    if ($Config -and (Test-Path $Config)) {
        Write-Info "Using config from file: $Config"
        return Get-Content $Config -Raw
    }

    if ($ServerUrl -and $GrpcEndpoint) {
        Write-Info "Building config from parameters"
        $configObj = @{
            server_url = $ServerUrl
            grpc_endpoint = $GrpcEndpoint
            enrollment_token = if ($EnrollmentToken) { $EnrollmentToken } else { "" }
            organization_id = if ($OrganizationId) { $OrganizationId } else { "" }
        }
        return $configObj | ConvertTo-Json -Compress
    }

    return $null
}

# Compile installer with Inno Setup
function Compile-Installer {
    param($IsccPath)

    Write-Info "Compiling installer with Inno Setup..."

    $issPath = Join-Path $ScriptDir "sentinel-setup.iss"

    if (-not (Test-Path $issPath)) {
        throw "Inno Setup script not found: $issPath"
    }

    # Build command
    $args = @(
        "/Q",  # Quiet mode
        "`"$issPath`""
    )

    Write-Info "Running: $IsccPath $($args -join ' ')"

    $process = Start-Process -FilePath $IsccPath -ArgumentList $args -Wait -PassThru -NoNewWindow
    if ($process.ExitCode -ne 0) {
        throw "Inno Setup compilation failed with exit code: $($process.ExitCode)"
    }

    Write-Success "Installer compiled successfully"
}

# Embed config into installer
function Embed-Config {
    param($InstallerPath, $ConfigJson)

    if (-not $ConfigJson) {
        Write-Info "No config to embed, creating template installer"
        return
    }

    Write-Info "Embedding configuration into installer..."

    $marker = "---SENTINEL-CONFIG---"
    $configData = $marker + "`n" + $ConfigJson

    # Read installer binary
    $bytes = [System.IO.File]::ReadAllBytes($InstallerPath)

    # Append config
    $configBytes = [System.Text.Encoding]::UTF8.GetBytes($configData)
    $newBytes = New-Object byte[] ($bytes.Length + $configBytes.Length)
    [Array]::Copy($bytes, $newBytes, $bytes.Length)
    [Array]::Copy($configBytes, 0, $newBytes, $bytes.Length, $configBytes.Length)

    # Write back
    [System.IO.File]::WriteAllBytes($InstallerPath, $newBytes)

    Write-Success "Config embedded successfully ($($configBytes.Length) bytes)"
}

# Rename output file
function Rename-Output {
    param($OutputName)

    $templatePath = Join-Path $ReleaseDir "sentinel-installer-template.exe"
    $finalPath = Join-Path $ReleaseDir $OutputName

    if (Test-Path $templatePath) {
        if ($templatePath -ne $finalPath) {
            if (Test-Path $finalPath) {
                Remove-Item $finalPath -Force
            }
            Copy-Item $templatePath $finalPath
            Write-Success "Created: $finalPath"
        }
        return $finalPath
    }

    throw "Compiled installer not found: $templatePath"
}

# Calculate file hash
function Get-FileHash256 {
    param($Path)
    $hash = Get-FileHash -Path $Path -Algorithm SHA256
    return $hash.Hash.ToLower()
}

# Main build process
function Main {
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Magenta
    Write-Host "  Sentinel Agent Installer Builder" -ForegroundColor Magenta
    Write-Host "========================================" -ForegroundColor Magenta
    Write-Host ""

    try {
        # Find Inno Setup
        if ($InnoSetupPath -and (Test-Path $InnoSetupPath)) {
            $iscc = $InnoSetupPath
        } else {
            $iscc = Find-InnoSetup
        }

        if (-not $iscc) {
            throw "Inno Setup 6 not found. Please install it from: https://jrsoftware.org/isdl.php"
        }
        Write-Success "Inno Setup found: $iscc"

        # Ensure resources exist
        Ensure-Resources

        # Verify binaries
        Verify-Binaries

        # Build config
        $configJson = Build-ConfigJson

        # Compile installer
        Compile-Installer -IsccPath $iscc

        # Get output path and embed config
        $installerPath = Rename-Output -OutputName $OutputName
        Embed-Config -InstallerPath $installerPath -ConfigJson $configJson

        # Show results
        $fileInfo = Get-Item $installerPath
        $hash = Get-FileHash256 -Path $installerPath

        Write-Host ""
        Write-Host "========================================" -ForegroundColor Green
        Write-Host "  Build Complete!" -ForegroundColor Green
        Write-Host "========================================" -ForegroundColor Green
        Write-Host ""
        Write-Host "Output: $installerPath" -ForegroundColor White
        Write-Host "Size: $($fileInfo.Length / 1MB | ForEach-Object { '{0:N2}' -f $_ }) MB" -ForegroundColor White
        Write-Host "SHA256: $hash" -ForegroundColor White
        Write-Host ""

        if ($configJson) {
            Write-Host "Config embedded: Yes" -ForegroundColor Green
        } else {
            Write-Host "Config embedded: No (template installer)" -ForegroundColor Yellow
            Write-Host ""
            Write-Host "To create a configured installer, run:" -ForegroundColor Cyan
            Write-Host "  .\build.ps1 -ServerUrl `"https://...`" -GrpcEndpoint `"...:4444`" -EnrollmentToken `"...`" -OrganizationId `"...`"" -ForegroundColor White
        }

        return $installerPath

    } catch {
        Write-Err $_.Exception.Message
        if ($Verbose) {
            Write-Err $_.ScriptStackTrace
        }
        exit 1
    }
}

# Run main
Main
