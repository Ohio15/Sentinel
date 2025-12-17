# Sentinel Agent Build Script for Windows
# Usage: .\build.ps1 [-Platform <windows|linux|macos|all>] [-Arch <amd64|386|arm64>] [-BuildMsi]

param(
    [string]$Platform = "windows",
    [string]$Arch = "amd64",
    [switch]$BuildMsi
)

$ErrorActionPreference = "Stop"

# Read version from version.json
$VersionFile = Join-Path $PSScriptRoot "version.json"
if (Test-Path $VersionFile) {
    $VersionInfo = Get-Content $VersionFile -Raw | ConvertFrom-Json
    $Version = $VersionInfo.version
} else {
    Write-Host "Warning: version.json not found, using default version" -ForegroundColor Yellow
    $Version = "1.0.0"
}

Write-Host "Building Sentinel Agent v$Version" -ForegroundColor Cyan

$OutputDir = "..\release\agent"
$BinaryName = "sentinel-agent"
$GoPath = $env:GOPATH
if (-not $GoPath) { $GoPath = "$env:USERPROFILE\go" }

# Add Go to PATH if not already present
$GoBin = "C:\Program Files\Go\bin"
if (Test-Path $GoBin) {
    $env:PATH = "$GoBin;$env:PATH"
}

# Create output directory
if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir | Out-Null
}

# Build function
function Build-Agent {
    param(
        [string]$OS,
        [string]$Architecture,
        [string]$OutputName
    )

    Write-Host "Building for $OS/$Architecture..." -ForegroundColor Cyan

    $env:GOOS = $OS
    $env:GOARCH = $Architecture
    $env:CGO_ENABLED = "0"

    # For Windows builds, generate resource file with admin manifest
    if ($OS -eq "windows") {
        Write-Host "  Generating Windows resource file with admin manifest..." -ForegroundColor Yellow
        Push-Location ".\cmd\sentinel-agent"
        & "$GoPath\bin\goversioninfo.exe" -64
        if ($LASTEXITCODE -ne 0) {
            Write-Host "  Warning: goversioninfo failed, building without manifest" -ForegroundColor Yellow
        }
        Pop-Location
    }

    $ldflags = "-s -w -X main.Version=$Version"
    $output = Join-Path $OutputDir $OutputName

    go build -ldflags $ldflags -o $output ./cmd/sentinel-agent

    if ($LASTEXITCODE -eq 0) {
        $size = (Get-Item $output).Length / 1MB
        Write-Host "  Built: $output ($([math]::Round($size, 2)) MB)" -ForegroundColor Green
    } else {
        Write-Host "  Build failed!" -ForegroundColor Red
        exit 1
    }

    # Clean up generated resource file
    if ($OS -eq "windows") {
        $sysoFile = ".\cmd\sentinel-agent\resource.syso"
        if (Test-Path $sysoFile) {
            Remove-Item $sysoFile -Force
        }
    }
}

# MSI Build function
function Build-Msi {
    Write-Host "`nBuilding MSI installer..." -ForegroundColor Cyan

    # Check for WiX toolset
    $WixPath = $null
    $PossibleWixPaths = @(
        "${env:ProgramFiles(x86)}\WiX Toolset v3.11\bin",
        "${env:ProgramFiles(x86)}\WiX Toolset v3.14\bin",
        "${env:ProgramFiles}\WiX Toolset v3.11\bin",
        "${env:ProgramFiles}\WiX Toolset v3.14\bin",
        "$env:USERPROFILE\.dotnet\tools"
    )

    foreach ($path in $PossibleWixPaths) {
        if (Test-Path "$path\candle.exe") {
            $WixPath = $path
            break
        }
    }

    # Check for WiX via dotnet tool
    $DotnetWix = Get-Command wix -ErrorAction SilentlyContinue
    if ($DotnetWix) {
        Write-Host "  Found WiX via dotnet tool" -ForegroundColor Gray
        $UseDotnetWix = $true
    } elseif ($WixPath) {
        Write-Host "  Found WiX at: $WixPath" -ForegroundColor Gray
        $UseDotnetWix = $false
    } else {
        Write-Host "  WiX Toolset not found. Skipping MSI build." -ForegroundColor Yellow
        Write-Host "  Install WiX: winget install WiX.WiX3 OR dotnet tool install --global wix" -ForegroundColor Yellow
        return
    }

    $InstallerDir = Join-Path $PSScriptRoot "installer"
    $WxsFile = Join-Path $InstallerDir "sentinel-agent.wxs"
    $SourceDir = Resolve-Path $OutputDir
    $MsiOutput = Join-Path $OutputDir "sentinel-agent.msi"

    if (-not (Test-Path $WxsFile)) {
        Write-Host "  WXS file not found at: $WxsFile" -ForegroundColor Red
        return
    }

    # Check that Windows binary exists
    $WindowsBinary = Join-Path $SourceDir "sentinel-agent.exe"
    if (-not (Test-Path $WindowsBinary)) {
        Write-Host "  Windows binary not found. Build Windows first." -ForegroundColor Red
        return
    }

    # Create temp directory for build
    $TempDir = Join-Path $env:TEMP "sentinel-msi-build"
    if (Test-Path $TempDir) {
        Remove-Item $TempDir -Recurse -Force
    }
    New-Item -ItemType Directory -Path $TempDir | Out-Null

    try {
        if ($UseDotnetWix) {
            # Use dotnet wix tool (WiX v4+)
            Write-Host "  Compiling with WiX v4..." -ForegroundColor Yellow
            wix build $WxsFile -d SourceDir="$SourceDir" -o $MsiOutput
        } else {
            # Use WiX v3 toolset
            $Candle = Join-Path $WixPath "candle.exe"
            $Light = Join-Path $WixPath "light.exe"
            $WixobjFile = Join-Path $TempDir "sentinel-agent.wixobj"

            Write-Host "  Compiling WXS..." -ForegroundColor Yellow
            & $Candle $WxsFile -dSourceDir="$SourceDir" -out $WixobjFile -arch x64
            if ($LASTEXITCODE -ne 0) {
                Write-Host "  Candle failed!" -ForegroundColor Red
                return
            }

            Write-Host "  Linking MSI..." -ForegroundColor Yellow
            & $Light $WixobjFile -out $MsiOutput -sice:ICE61
            if ($LASTEXITCODE -ne 0) {
                Write-Host "  Light failed!" -ForegroundColor Red
                return
            }
        }

        if (Test-Path $MsiOutput) {
            $size = (Get-Item $MsiOutput).Length / 1MB
            Write-Host "  Built: $MsiOutput ($([math]::Round($size, 2)) MB)" -ForegroundColor Green
        }
    } finally {
        # Clean up temp directory
        if (Test-Path $TempDir) {
            Remove-Item $TempDir -Recurse -Force
        }
    }
}

# Download dependencies
Write-Host "Downloading dependencies..." -ForegroundColor Yellow
go mod download
go mod tidy

# Build based on platform
switch ($Platform.ToLower()) {
    "windows" {
        Build-Agent -OS "windows" -Architecture $Arch -OutputName "$BinaryName.exe"
        if ($BuildMsi) {
            Build-Msi
        }
    }
    "linux" {
        Build-Agent -OS "linux" -Architecture $Arch -OutputName "$BinaryName-linux"
    }
    "macos" {
        Build-Agent -OS "darwin" -Architecture $Arch -OutputName "$BinaryName-macos"
    }
    "all" {
        Write-Host "`nBuilding all platforms...`n" -ForegroundColor Yellow

        # Windows
        Build-Agent -OS "windows" -Architecture "amd64" -OutputName "$BinaryName.exe"

        # Linux
        Build-Agent -OS "linux" -Architecture "amd64" -OutputName "$BinaryName-linux"
        Build-Agent -OS "linux" -Architecture "arm64" -OutputName "$BinaryName-linux-arm64"

        # macOS
        Build-Agent -OS "darwin" -Architecture "amd64" -OutputName "$BinaryName-macos"
        Build-Agent -OS "darwin" -Architecture "arm64" -OutputName "$BinaryName-macos-arm64"

        # Build MSI if requested
        if ($BuildMsi) {
            Build-Msi
        }
    }
    default {
        Write-Host "Unknown platform: $Platform" -ForegroundColor Red
        Write-Host "Valid options: windows, linux, macos, all" -ForegroundColor Yellow
        exit 1
    }
}

Write-Host "`nBuild complete!" -ForegroundColor Green
Write-Host "Output directory: $(Resolve-Path $OutputDir)" -ForegroundColor Gray

# Copy version.json to output directory for the server to read
Copy-Item $VersionFile -Destination $OutputDir -Force
Write-Host "Copied version.json to output directory" -ForegroundColor Gray

# List built files
Get-ChildItem $OutputDir -Filter "sentinel-agent*" | Format-Table Name, @{N='Size (MB)';E={[math]::Round($_.Length/1MB, 2)}}
