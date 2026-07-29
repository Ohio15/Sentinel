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

# Guard: refuse a version whose tag already exists (locally or on origin).
# Tags are permanent — v1.77.40 was burned on a commit whose version files
# still said 1.77.39, so tag-name/file drift is a real, observed failure mode.
Push-Location $ProjectRoot
$existingLocal  = git tag -l "v$Version"
$existingRemote = git ls-remote --tags origin "refs/tags/v$Version"
Pop-Location
if ($existingLocal -or $existingRemote) {
    throw "Tag v$Version already exists (local: '$existingLocal', remote: '$existingRemote'). Pick the next free version."
}

# RW-1 signing gate: every release MUST be signed. Refuse to produce an unsigned
# release (which would ship binaries that self-updating agents fail-closed on,
# AND leave a build with no embedded verification key). Both the private key
# (for cmd/sign) and the public key (baked into the binaries via -ldflags) are
# required. No key material lives in the repo — these come from the NEXUS build
# host's protected store.
$SigningKey    = $env:SENTINEL_UPDATE_SIGNING_KEY     # PEM path for cmd/sign
$SigningPubKey = $env:SENTINEL_UPDATE_SIGNING_PUBKEY   # hex-encoded Ed25519 public key
if ([string]::IsNullOrWhiteSpace($SigningKey)) {
    throw "SENTINEL_UPDATE_SIGNING_KEY is not set. Refusing to build an unsigned release (RW-1)."
}
if (-not (Test-Path $SigningKey)) {
    throw "SENTINEL_UPDATE_SIGNING_KEY points at a missing file: $SigningKey"
}
if ([string]::IsNullOrWhiteSpace($SigningPubKey)) {
    throw "SENTINEL_UPDATE_SIGNING_PUBKEY is not set. The public key must be embedded in the binaries (RW-1)."
}
$SigningLdflags = "-X github.com/sentinel/agent/internal/updatesig.SigningPublicKeyHex=$SigningPubKey"

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
    }
    # NOTE: root package.json is deliberately NOT updated here — it tracks the
    # server/repo version line, which diverges from the agent line on
    # server-only releases. Bumping it to the agent version could silently
    # DOWNGRADE it (server line is ahead, e.g. 1.78.x vs agent 1.77.x).
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

Write-Host "  Building sentinel-agent.exe (embedding signing pubkey)..."
go build -ldflags $SigningLdflags -o "../release/agent/sentinel-agent.exe" ./cmd/sentinel-agent
if ($LASTEXITCODE -ne 0) { throw "Agent build failed" }

Write-Host "  Building sentinel-watchdog.exe (embedding signing pubkey)..."
go build -ldflags $SigningLdflags -o "../release/agent/sentinel-watchdog.exe" ./cmd/sentinel-watchdog
if ($LASTEXITCODE -ne 0) { throw "Watchdog build failed" }

# Build the signing tool for the host so we can sign the artifacts below.
Write-Host "  Building signing tool (cmd/sign)..."
$SignTool = "$ProjectRoot/release/agent/sentinel-sign.exe"
$prevGOOS = $env:GOOS; $prevGOARCH = $env:GOARCH
Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
go build -o "$SignTool" ./cmd/sign
if ($LASTEXITCODE -ne 0) { throw "Signing tool build failed" }
$env:GOOS = $prevGOOS; $env:GOARCH = $prevGOARCH

Pop-Location

# Copy to installers directory
Write-Host "Copying binaries to installers directory..." -ForegroundColor Yellow
Copy-Item "$ProjectRoot/release/agent/sentinel-agent.exe" "$ProjectRoot/installers/sentinel-agent-windows-amd64.exe" -Force
Copy-Item "$ProjectRoot/release/agent/sentinel-watchdog.exe" "$ProjectRoot/installers/sentinel-watchdog-windows-amd64.exe" -Force
Write-Host "  Copied to installers/" -ForegroundColor Green

# RW-1: sign every artifact. cmd/sign writes a "<file>.sig" sidecar (base64
# Ed25519 over the raw bytes) that the update server serves alongside the binary,
# and returns the base64 signature we record in version.json. The signature
# format is byte-identical to internal/updatesig.Verify. Sign both the canonical
# release copies and the installer copies (identical bytes -> identical sig, but
# each path needs its own sidecar so the server can find it next to the binary).
Write-Host ""
Write-Host "Signing artifacts (Ed25519 detached)..." -ForegroundColor Yellow

function Sign-Artifact {
    param([string]$Path)
    if (-not (Test-Path $Path)) { throw "Cannot sign missing artifact: $Path" }
    $sig = & $SignTool $Path
    if ($LASTEXITCODE -ne 0) { throw "Signing failed for $Path" }
    Write-Host "  Signed: $Path" -ForegroundColor Green
    return $sig.Trim()
}

$AgentSig    = Sign-Artifact "$ProjectRoot/release/agent/sentinel-agent.exe"
$WatchdogSig = Sign-Artifact "$ProjectRoot/release/agent/sentinel-watchdog.exe"
[void](Sign-Artifact "$ProjectRoot/installers/sentinel-agent-windows-amd64.exe")
[void](Sign-Artifact "$ProjectRoot/installers/sentinel-watchdog-windows-amd64.exe")

# Record the primary-platform (windows-amd64) agent signature in version.json for
# audit/record. The authoritative per-binary signature the server serves is the
# sidecar .sig read by getBinarySignature. Agents fail closed if the sidecar is
# absent, so this JSON field is a convenience record, not the trust source.
Write-Host "Recording signature in version.json files..." -ForegroundColor Yellow
foreach ($jsonPath in $JsonFiles) {
    if (Test-Path $jsonPath) {
        $json = Get-Content $jsonPath -Raw | ConvertFrom-Json
        if ($json.PSObject.Properties.Name -contains 'signature') {
            $json.signature = $AgentSig
        } else {
            $json | Add-Member -NotePropertyName 'signature' -NotePropertyValue $AgentSig
        }
        $json | ConvertTo-Json -Depth 10 | Set-Content $jsonPath
        Write-Host "  Signature written: $jsonPath" -ForegroundColor Green
    }
}
Write-Host "  Watchdog signature: $WatchdogSig" -ForegroundColor DarkGray

# Git operations
Write-Host ""
Write-Host "Committing changes..." -ForegroundColor Yellow
Push-Location $ProjectRoot

git add agent/cmd/sentinel-agent/main.go
git add agent/cmd/sentinel-watchdog/main.go
git add agent/version.json
git add release/agent/version.json
git add installers/version.json
git add installers/sentinel-agent-windows-amd64.exe
git add installers/sentinel-watchdog-windows-amd64.exe
# RW-1: signature sidecars the update server serves next to each binary.
git add installers/sentinel-agent-windows-amd64.exe.sig
git add installers/sentinel-watchdog-windows-amd64.exe.sig
git add release/agent/sentinel-agent.exe.sig
git add release/agent/sentinel-watchdog.exe.sig

$commitMsg = "Release v$Version"
if ($Changelog -ne "") {
    $commitMsg = "Release v$Version - $Changelog"
}

git commit -m "$commitMsg`n`nCo-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
git push

# Tag AT the release commit so the tag name and the version files can never
# drift (the guard above already ensured the tag is free).
git tag -a "v$Version" -m "Release v$Version"
git push origin "v$Version"

Pop-Location

Write-Host ""
Write-Host "=== Release v$Version built and committed ===" -ForegroundColor Green

# Deploy if requested
if ($Deploy) {
    Write-Host ""
    Write-Host "Deploying to remote server (192.168.1.20)..." -ForegroundColor Yellow

    $RemoteHost = "ohio_@192.168.1.20"
    $RemotePath = "D:/Projects/Sentinel/installers"

    scp "$ProjectRoot/installers/version.json" "${RemoteHost}:${RemotePath}/version.json"
    scp "$ProjectRoot/installers/sentinel-agent-windows-amd64.exe" "${RemoteHost}:${RemotePath}/sentinel-agent-windows-amd64.exe"
    scp "$ProjectRoot/installers/sentinel-watchdog-windows-amd64.exe" "${RemoteHost}:${RemotePath}/sentinel-watchdog-windows-amd64.exe"
    # RW-1: the .sig sidecars MUST travel with the binaries — the update server
    # reads "<binary>.sig" to populate the signature the agent verifies.
    scp "$ProjectRoot/installers/sentinel-agent-windows-amd64.exe.sig" "${RemoteHost}:${RemotePath}/sentinel-agent-windows-amd64.exe.sig"
    scp "$ProjectRoot/installers/sentinel-watchdog-windows-amd64.exe.sig" "${RemoteHost}:${RemotePath}/sentinel-watchdog-windows-amd64.exe.sig"

    Write-Host "Deployment complete!" -ForegroundColor Green
    Write-Host ""
    Write-Host "Agents should detect the update within their next check interval."
}

Write-Host ""
Write-Host "Done! Version $Version is ready." -ForegroundColor Cyan
if (-not $Deploy) {
    Write-Host "Run with -Deploy to push to remote server." -ForegroundColor Yellow
}
