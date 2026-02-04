<#
.SYNOPSIS
    Sentinel Release Script - Updates versions, builds, and deploys
.DESCRIPTION
    Updates version numbers across all files, builds agent binaries,
    and optionally deploys to the remote server.
.PARAMETER Version
    The new version number (e.g., "1.70.0")
.PARAMETER Deploy
    If specified, also deploys to the remote server
.PARAMETER Changelog
    Optional changelog entry for this release
.EXAMPLE
    .\release.ps1 -Version "1.70.0" -Changelog "New feature X"
    .\release.ps1 -Version "1.70.0" -Deploy
#>
param(
    [Parameter(Mandatory=$true)]
    [string]$Version,

    [switch]$Deploy,

    [string]$Changelog = ""
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
if (-not (Test-Path "$ProjectRoot/package.json")) {
    $ProjectRoot = "D:/Projects/Sentinel"
}

$ReleaseDate = Get-Date -Format "yyyy-MM-dd"

Write-Host "=== Sentinel Release Script ===" -ForegroundColor Cyan
Write-Host "Version: $Version"
Write-Host "Date: $ReleaseDate"
Write-Host ""

# Files to update
$FilesToUpdate = @(
    @{
        Path = "$ProjectRoot/agent/cmd/sentinel-agent/main.go"
        Pattern = 'var Version = "[^"]*"'
        Replacement = "var Version = `"$Version`""
    },
    @{
        Path = "$ProjectRoot/agent/cmd/sentinel-watchdog/main.go"
        Pattern = 'Version = "[^"]*"'
        Replacement = "Version = `"$Version`""
    },
    @{
        Path = "$ProjectRoot/package.json"
        Pattern = '"version": "[^"]*"'
        Replacement = "`"version`": `"$Version`""
    }
)

$JsonFiles = @(
    "$ProjectRoot/agent/version.json",
    "$ProjectRoot/release/agent/version.json",
    "$ProjectRoot/installers/version.json"
)

# Update Go and package.json files
Write-Host "Updating version in source files..." -ForegroundColor Yellow
foreach ($file in $FilesToUpdate) {
    if (Test-Path $file.Path) {
        $content = Get-Content $file.Path -Raw
        $content = $content -replace $file.Pattern, $file.Replacement
        Set-Content $file.Path $content -NoNewline
        Write-Host "  Updated: $($file.Path)" -ForegroundColor Green
    } else {
        Write-Host "  Not found: $($file.Path)" -ForegroundColor Red
    }
}

# Update JSON version files
Write-Host "Updating version.json files..." -ForegroundColor Yellow
foreach ($jsonPath in $JsonFiles) {
    if (Test-Path $jsonPath) {
        $json = Get-Content $jsonPath -Raw | ConvertFrom-Json
        $json.version = $Version
        $json.releaseDate = $ReleaseDate
        if ($Changelog -ne "") {
            $json.changelog = $Changelog
        }
        $json | ConvertTo-Json -Depth 10 | Set-Content $jsonPath
        Write-Host "  Updated: $jsonPath" -ForegroundColor Green
    } else {
        Write-Host "  Not found: $jsonPath" -ForegroundColor Red
    }
}

# Build binaries
Write-Host ""
Write-Host "Building agent binaries..." -ForegroundColor Yellow
Push-Location "$ProjectRoot/agent"

$env:GOOS = "windows"
$env:GOARCH = "amd64"

Write-Host "  Building sentinel-agent.exe..."
go build -o "../release/agent/sentinel-agent.exe" ./cmd/sentinel-agent
if ($LASTEXITCODE -ne 0) { throw "Agent build failed" }

Write-Host "  Building sentinel-watchdog.exe..."
go build -o "../release/agent/sentinel-watchdog.exe" ./cmd/sentinel-watchdog
if ($LASTEXITCODE -ne 0) { throw "Watchdog build failed" }

Pop-Location

# Copy to installers directory
Write-Host "Copying binaries to installers directory..." -ForegroundColor Yellow
Copy-Item "$ProjectRoot/release/agent/sentinel-agent.exe" "$ProjectRoot/installers/sentinel-agent-windows-amd64.exe" -Force
Copy-Item "$ProjectRoot/release/agent/sentinel-watchdog.exe" "$ProjectRoot/installers/sentinel-watchdog-windows-amd64.exe" -Force
Write-Host "  Copied to installers/" -ForegroundColor Green

# Git operations
Write-Host ""
Write-Host "Committing changes..." -ForegroundColor Yellow
Push-Location $ProjectRoot

git add agent/cmd/sentinel-agent/main.go
git add agent/cmd/sentinel-watchdog/main.go
git add agent/version.json
git add package.json
git add installers/version.json
git add installers/sentinel-agent-windows-amd64.exe
git add installers/sentinel-watchdog-windows-amd64.exe

$commitMsg = "Release v$Version"
if ($Changelog -ne "") {
    $commitMsg = "Release v$Version - $Changelog"
}

git commit -m "$commitMsg`n`nCo-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
git push

Pop-Location

Write-Host ""
Write-Host "=== Release v$Version built and committed ===" -ForegroundColor Green

# Deploy if requested
if ($Deploy) {
    Write-Host ""
    Write-Host "Deploying to remote server (192.168.1.20)..." -ForegroundColor Yellow

    $RemoteHost = "REDACTED_SSH_TARGET"
    $RemotePath = "D:/Projects/Sentinel/installers"

    scp "$ProjectRoot/installers/version.json" "${RemoteHost}:${RemotePath}/version.json"
    scp "$ProjectRoot/installers/sentinel-agent-windows-amd64.exe" "${RemoteHost}:${RemotePath}/sentinel-agent-windows-amd64.exe"
    scp "$ProjectRoot/installers/sentinel-watchdog-windows-amd64.exe" "${RemoteHost}:${RemotePath}/sentinel-watchdog-windows-amd64.exe"

    Write-Host "Deployment complete!" -ForegroundColor Green
    Write-Host ""
    Write-Host "Agents should detect the update within their next check interval."
}

Write-Host ""
Write-Host "Done! Version $Version is ready." -ForegroundColor Cyan
if (-not $Deploy) {
    Write-Host "Run with -Deploy to push to remote server." -ForegroundColor Yellow
}
