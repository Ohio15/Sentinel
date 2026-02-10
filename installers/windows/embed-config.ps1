<#
.SYNOPSIS
    Embeds organization config into a Sentinel installer

.DESCRIPTION
    Takes the base installer template and appends organization-specific
    configuration to create a customized installer for deployment.

.PARAMETER TemplateInstaller
    Path to the template installer (default: sentinel-installer-template.exe)

.PARAMETER OutputInstaller
    Path for the output installer (default: sentinel-installer.exe)

.PARAMETER ServerUrl
    Sentinel server URL (e.g., https://sentinelrmm.us)

.PARAMETER GrpcEndpoint
    gRPC endpoint (e.g., sentinelrmm.us:4444)

.PARAMETER EnrollmentToken
    Enrollment token for this organization

.PARAMETER OrganizationId
    Organization UUID

.PARAMETER ConfigFile
    Path to JSON config file (alternative to individual parameters)

.EXAMPLE
    .\embed-config.ps1 -ServerUrl "https://sentinelrmm.us" -GrpcEndpoint "sentinelrmm.us:4444" -EnrollmentToken "abc123" -OrganizationId "org-uuid"

.EXAMPLE
    .\embed-config.ps1 -ConfigFile ".\org-config.json" -OutputInstaller "sentinel-acme-corp.exe"
#>

param(
    [string]$TemplateInstaller,
    [string]$OutputInstaller,
    [string]$ServerUrl,
    [string]$GrpcEndpoint,
    [string]$EnrollmentToken,
    [string]$OrganizationId,
    [string]$ConfigFile
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent (Split-Path -Parent $ScriptDir)
$ReleaseDir = Join-Path $ProjectRoot "release\agent"

# Defaults
if (-not $TemplateInstaller) {
    $TemplateInstaller = Join-Path $ReleaseDir "sentinel-installer-template.exe"
}
if (-not $OutputInstaller) {
    $OutputInstaller = Join-Path $ReleaseDir "sentinel-installer.exe"
}

$ConfigMarker = "---SENTINEL-CONFIG---"

function Write-Info { param($msg) Write-Host "[INFO] $msg" -ForegroundColor Cyan }
function Write-Success { param($msg) Write-Host "[OK] $msg" -ForegroundColor Green }
function Write-Err { param($msg) Write-Host "[ERROR] $msg" -ForegroundColor Red }

# Build config JSON
function Build-ConfigJson {
    if ($ConfigFile -and (Test-Path $ConfigFile)) {
        Write-Info "Loading config from file: $ConfigFile"
        $content = Get-Content $ConfigFile -Raw
        # Validate JSON
        try {
            $null = $content | ConvertFrom-Json
            return $content.Trim()
        } catch {
            throw "Invalid JSON in config file: $_"
        }
    }

    if (-not $ServerUrl) {
        throw "ServerUrl is required. Use -ServerUrl parameter or -ConfigFile"
    }
    if (-not $GrpcEndpoint) {
        throw "GrpcEndpoint is required. Use -GrpcEndpoint parameter or -ConfigFile"
    }

    $config = @{
        server_url = $ServerUrl
        grpc_endpoint = $GrpcEndpoint
        enrollment_token = if ($EnrollmentToken) { $EnrollmentToken } else { "" }
        organization_id = if ($OrganizationId) { $OrganizationId } else { "" }
    }

    return ($config | ConvertTo-Json -Compress)
}

# Check if installer already has embedded config
function Has-EmbeddedConfig {
    param($InstallerPath)

    $bytes = [System.IO.File]::ReadAllBytes($InstallerPath)
    $text = [System.Text.Encoding]::UTF8.GetString($bytes)
    return $text.Contains($ConfigMarker)
}

# Main
try {
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Magenta
    Write-Host "  Sentinel Config Embedder" -ForegroundColor Magenta
    Write-Host "========================================" -ForegroundColor Magenta
    Write-Host ""

    # Verify template exists
    if (-not (Test-Path $TemplateInstaller)) {
        throw "Template installer not found: $TemplateInstaller`nRun build.ps1 first to create the template."
    }

    # Check if template already has config
    if (Has-EmbeddedConfig -InstallerPath $TemplateInstaller) {
        Write-Err "Template already has embedded config. Please use a clean template."
        Write-Info "The template should be: sentinel-installer-template.exe"
        exit 1
    }

    # Build config
    $configJson = Build-ConfigJson
    Write-Info "Config: $configJson"

    # Copy template to output
    Write-Info "Creating installer copy..."
    Copy-Item $TemplateInstaller $OutputInstaller -Force

    # Append config
    Write-Info "Embedding configuration..."
    $configData = "`n$ConfigMarker`n$configJson"
    $configBytes = [System.Text.Encoding]::UTF8.GetBytes($configData)

    # Append to file
    $stream = [System.IO.File]::Open($OutputInstaller, [System.IO.FileMode]::Append)
    try {
        $stream.Write($configBytes, 0, $configBytes.Length)
    } finally {
        $stream.Close()
    }

    # Verify
    $finalSize = (Get-Item $OutputInstaller).Length
    $hash = (Get-FileHash $OutputInstaller -Algorithm SHA256).Hash.ToLower()

    Write-Host ""
    Write-Host "========================================" -ForegroundColor Green
    Write-Host "  Config Embedded Successfully!" -ForegroundColor Green
    Write-Host "========================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "Output: $OutputInstaller" -ForegroundColor White
    Write-Host "Size: $([math]::Round($finalSize / 1MB, 2)) MB" -ForegroundColor White
    Write-Host "SHA256: $hash" -ForegroundColor White
    Write-Host ""

    # Show deployment command
    Write-Host "Deployment options:" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  Interactive: " -NoNewline -ForegroundColor Yellow
    Write-Host "$OutputInstaller"
    Write-Host ""
    Write-Host "  Silent:      " -NoNewline -ForegroundColor Yellow
    Write-Host "$OutputInstaller /VERYSILENT /SUPPRESSMSGBOXES /NORESTART"
    Write-Host ""

} catch {
    Write-Err $_.Exception.Message
    exit 1
}
