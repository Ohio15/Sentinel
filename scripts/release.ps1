<#
.SYNOPSIS
    Sentinel Release Script - versions, builds, signs, verifies, and deploys
.DESCRIPTION
    Runs on the canonical signing/build host (NEXUS, decision 2026-07-29) under
    PowerShell 7.4+ from a DEDICATED CLEAN build checkout — never the deploy
    tree. Updates version numbers, cross-builds the fleet target matrix, signs
    every served artifact with the Ed25519 release key (RW-1), verifies every
    signature end-to-end with an independently built verifier, commits and tags,
    and optionally deploys by staged local copy into the serving directory.

    Installer BINARIES are deliberately not in git (.gitignore *.exe / release/,
    10MB pre-commit size guard) — only version files and signed-manifest
    sidecars are committed. Deployment is a filesystem copy on the serving host.
.PARAMETER Version
    The new version number, strict semver (e.g. "1.77.41")
.PARAMETER Deploy
    Also deploy to the serving directory on this host
.PARAMETER DeployTree
    The deploy tree the update server serves from (default: $HOME/Sentinel)
.PARAMETER Changelog
    Optional changelog entry for this release
.EXAMPLE
    pwsh scripts/release.ps1 -Version "1.77.41" -Changelog "First signed release"
    pwsh scripts/release.ps1 -Version "1.77.41" -Deploy
#>
param(
    [Parameter(Mandatory=$true)]
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version,

    [switch]$Deploy,

    [string]$DeployTree = (Join-Path ([Environment]::GetFolderPath('UserProfile')) "Sentinel"),

    [string]$Changelog = ""
)

$ErrorActionPreference = "Stop"
# Make native command failures (git, go, openssl) terminate the script. This
# requires pwsh 7.4+; without it a failed `git commit` would fall through and
# burn the immutable tag on the wrong commit (the v1.77.40 drift bug).
if ($PSVersionTable.PSVersion -lt [version]'7.4') {
    throw "PowerShell 7.4+ required (native-command error handling). Found $($PSVersionTable.PSVersion)."
}
$PSNativeCommandUseErrorActionPreference = $true

$ReleaseDate = Get-Date -Format "yyyy-MM-dd"

Write-Host "=== Sentinel Release Script ===" -ForegroundColor Cyan
Write-Host "Version: $Version"
Write-Host "Date: $ReleaseDate"
Write-Host ""

# The repo root is the parent of scripts/. No fallback: a wrong root must fail.
$ProjectRoot = Split-Path -Parent $PSScriptRoot
if (-not (Test-Path "$ProjectRoot/package.json")) {
    throw "Repo root not found at $ProjectRoot (no package.json). Run this script from a Sentinel checkout."
}

# SameDir compares two directories through symlinks (device:inode on Linux;
# normalized-path fallback elsewhere).
function Test-SameDir {
    param([string]$A, [string]$B)
    if (-not (Test-Path $A) -or -not (Test-Path $B)) { return $false }
    if ($IsLinux) {
        $ia = stat -c '%d:%i' $A
        $ib = stat -c '%d:%i' $B
        return $ia -eq $ib
    }
    return (Resolve-Path $A).Path -eq (Resolve-Path $B).Path
}

# Guard BEFORE any mutation: never build from the deploy tree. A build run
# from there would overwrite live, bind-mounted served binaries mid-script.
if (Test-SameDir $ProjectRoot $DeployTree) {
    throw "Refusing to run from the deploy tree ($DeployTree). Build from a dedicated clean checkout."
}
if ($Deploy -and -not (Test-Path "$DeployTree/installers")) {
    throw "Deploy tree not found at $DeployTree/installers. -Deploy must run on the serving host (use -DeployTree to override)."
}

# ------------------------------------------------------------------------------
# RW-1 signing gate: every release MUST be signed, and the public key embedded
# in the binaries MUST be the pair of the private key that signs the manifests.
# The pubkey is therefore DERIVED from the private key, never taken on trust
# from the environment (a mismatched pubkey would brick fleet self-update; an
# attacker-supplied one would substitute the fleet's root of trust).
# ------------------------------------------------------------------------------
$SigningKey = $env:SENTINEL_UPDATE_SIGNING_KEY     # PKCS#8 PEM path for cmd/sign
if ([string]::IsNullOrWhiteSpace($SigningKey)) {
    throw "SENTINEL_UPDATE_SIGNING_KEY is not set. Refusing to build an unsigned release (RW-1)."
}
if (-not (Test-Path $SigningKey)) {
    throw "SENTINEL_UPDATE_SIGNING_KEY points at a missing file: $SigningKey"
}
$SigningKeyReal = (Resolve-Path $SigningKey).Path
foreach ($tree in @($ProjectRoot, $DeployTree)) {
    if ((Test-Path $tree) -and $SigningKeyReal.StartsWith((Resolve-Path $tree).Path)) {
        throw "Signing key $SigningKeyReal lives inside $tree — it must never be under a git tree or the serving directory."
    }
}
if ($IsLinux) {
    $perms = stat -c '%a' $SigningKeyReal
    if ($perms -notin @('600', '400')) {
        throw "Signing key $SigningKeyReal has mode $perms; require 600 or 400."
    }
}

# Derive the hex Ed25519 public key from the private key. Ed25519 SPKI DER is
# exactly 44 bytes: 12-byte header (302a300506032b6570032100) + 32-byte key.
$pubPem = & openssl pkey -in $SigningKeyReal -pubout
$pubB64 = ($pubPem | Where-Object { $_ -notmatch '^-----' }) -join ''
$pubDer = [Convert]::FromBase64String($pubB64)
$derHex = [BitConverter]::ToString($pubDer).Replace('-', '').ToLowerInvariant()
if ($pubDer.Length -ne 44 -or -not $derHex.StartsWith('302a300506032b6570032100')) {
    throw "Signing key is not Ed25519 (unexpected SPKI: $derHex)."
}
$SigningPubKey = $derHex.Substring(24)
if (-not [string]::IsNullOrWhiteSpace($env:SENTINEL_UPDATE_SIGNING_PUBKEY) -and
    $env:SENTINEL_UPDATE_SIGNING_PUBKEY.ToLowerInvariant() -ne $SigningPubKey) {
    throw "SENTINEL_UPDATE_SIGNING_PUBKEY ($($env:SENTINEL_UPDATE_SIGNING_PUBKEY)) does not match the key derived from the signing private key ($SigningPubKey). Environment is stale or tampered — refusing."
}
Write-Host "Signing public key (derived from private key): $SigningPubKey" -ForegroundColor DarkGray
$SigningLdflags = "-X github.com/sentinel/agent/internal/updatesig.SigningPublicKeyHex=$SigningPubKey"

# Guard: refuse a version whose tag already exists (locally or on origin).
# Tags are permanent — v1.77.40 was burned on a commit whose version files
# still said 1.77.39, so tag-name/file drift is a real, observed failure mode.
$existingLocal  = git -C $ProjectRoot tag -l "v$Version"
$existingRemote = git -C $ProjectRoot ls-remote --tags origin "refs/tags/v$Version"
if ($existingLocal -or $existingRemote) {
    throw "Tag v$Version already exists (local: '$existingLocal', remote: '$existingRemote'). Pick the next free version."
}

# ------------------------------------------------------------------------------
# Version updates
# ------------------------------------------------------------------------------
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

Write-Host "Updating version in source files..." -ForegroundColor Yellow
foreach ($file in $FilesToUpdate) {
    if (-not (Test-Path $file.Path)) { throw "Version file not found: $($file.Path)" }
    $content = Get-Content $file.Path -Raw
    $content = $content -replace $file.Pattern, $file.Replacement
    Set-Content $file.Path $content -NoNewline
    Write-Host "  Updated: $($file.Path)" -ForegroundColor Green
}

Write-Host "Updating version.json files..." -ForegroundColor Yellow
foreach ($jsonPath in $JsonFiles) {
    if (-not (Test-Path $jsonPath)) { throw "version.json not found: $jsonPath" }
    $json = Get-Content $jsonPath -Raw | ConvertFrom-Json
    $json.version = $Version
    $json.releaseDate = $ReleaseDate
    if ($Changelog -ne "") {
        $json.changelog = $Changelog
    }
    $json | ConvertTo-Json -Depth 10 | Set-Content $jsonPath
    Write-Host "  Updated: $jsonPath" -ForegroundColor Green
}

# ------------------------------------------------------------------------------
# Build. Target matrix reflects the actual fleet (census 2026-07-29: every
# device is windows/amd64 plus one ubuntu/amd64 box). Widen deliberately, and
# only together with server/internal/api/agent_updates.go supportedAgentTargets
# — every advertised target MUST get a signed binary or its agents fail closed.
# ------------------------------------------------------------------------------
$Targets = @(
    @{ Platform = "windows"; Arch = "amd64"; Ext = ".exe" },
    @{ Platform = "linux";   Arch = "amd64"; Ext = "" }
)
$Commands = @("sentinel-agent", "sentinel-watchdog", "sentinel-bootstrap")

# Artifacts accumulate as @{Path; Platform; Arch} for signing/verify/deploy.
$Artifacts = [System.Collections.Generic.List[hashtable]]::new()

# Host tools (signer + verifier) build into a private temp dir so the one
# binary that reads the private key never sits in a tree that gets packaged.
$HostToolDir = Join-Path ([System.IO.Path]::GetTempPath()) "sentinel-release-$PID"
New-Item -ItemType Directory -Force $HostToolDir | Out-Null

Push-Location "$ProjectRoot/agent"
try {
    Write-Host ""
    Write-Host "Building host tools (cmd/sign, cmd/verify)..." -ForegroundColor Yellow
    $SignTool   = Join-Path $HostToolDir "sentinel-sign"
    $VerifyTool = Join-Path $HostToolDir "sentinel-verify"
    go build -o $SignTool ./cmd/sign
    # The verifier gets the DERIVED pubkey embedded — running it against every
    # signed artifact below proves end-to-end that the key that signed is the
    # pair of the key the fleet binaries will trust.
    go build -ldflags $SigningLdflags -o $VerifyTool ./cmd/verify

    Write-Host "Building agent binaries..." -ForegroundColor Yellow
    foreach ($t in $Targets) {
        foreach ($cmd in $Commands + @("verify")) {
            $name = if ($cmd -eq "verify") { "sentinel-verify" } else { $cmd }
            $out = "$ProjectRoot/installers/$name-$($t.Platform)-$($t.Arch)$($t.Ext)"
            Write-Host "  Building $out"
            $env:GOOS = $t.Platform; $env:GOARCH = $t.Arch; $env:CGO_ENABLED = "0"
            go build -ldflags $SigningLdflags -o $out "./cmd/$cmd"
            $Artifacts.Add(@{ Path = $out; Platform = $t.Platform; Arch = $t.Arch })
        }
    }
}
finally {
    Pop-Location
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
}

# Legacy dev-fallback copies the server can resolve when the platform-named
# file is absent. Identical bytes to the installers copies; each path gets its
# own sidecar. These live under gitignored release/ and are NOT committed.
Write-Host "Copying windows/amd64 canonical binaries to release/agent..." -ForegroundColor Yellow
New-Item -ItemType Directory -Force "$ProjectRoot/release/agent" | Out-Null
foreach ($pair in @(
    @{ Src = "sentinel-agent-windows-amd64.exe";    Dst = "sentinel-agent.exe" },
    @{ Src = "sentinel-watchdog-windows-amd64.exe"; Dst = "sentinel-watchdog.exe" },
    @{ Src = "sentinel-verify-windows-amd64.exe";   Dst = "sentinel-verify.exe" })) {
    $dst = "$ProjectRoot/release/agent/$($pair.Dst)"
    Copy-Item "$ProjectRoot/installers/$($pair.Src)" $dst -Force
    $Artifacts.Add(@{ Path = $dst; Platform = "windows"; Arch = "amd64" })
}

# ------------------------------------------------------------------------------
# RW-1: sign every artifact with its OWN platform/arch tuple, then verify every
# signature with the independently built verifier (embedded derived pubkey).
# cmd/sign writes a "<file>.manifest.json" sidecar binding {version, platform,
# arch, sha256, signedDowngrade} under one Ed25519 signature; the server serves
# the sidecar next to the binary and agents rebuild + verify the exact manifest.
# ------------------------------------------------------------------------------
Write-Host ""
Write-Host "Signing artifacts (Ed25519 signed manifest)..." -ForegroundColor Yellow
$env:SENTINEL_UPDATE_SIGNING_KEY = $SigningKeyReal
$AgentSig = $null
foreach ($a in $Artifacts) {
    # No signed downgrade on a forward release — anti-rollback holds.
    $sig = & $SignTool -version $Version -platform $a.Platform -arch $a.Arch $a.Path
    $a.Signature = "$sig".Trim()
    Write-Host "  Signed ($($a.Platform)/$($a.Arch)): $($a.Path)" -ForegroundColor Green
    if ($a.Path -like "*installers/sentinel-agent-windows-amd64.exe") { $AgentSig = $a.Signature }
}

Write-Host "Verifying every signature end-to-end (independent verifier)..." -ForegroundColor Yellow
foreach ($a in $Artifacts) {
    & $VerifyTool -binary $a.Path -manifest "$($a.Path).manifest.json"
    Write-Host "  Verified: $($a.Path)" -ForegroundColor Green
}
Remove-Item -Recurse -Force $HostToolDir

# Record the primary-platform agent signature AND the signing public key in
# version.json. The sidecars remain the trust source (agents fail closed
# without them); these fields are the durable audit record answering "which
# key did this release embed?" — without them a key substitution would be
# retroactively undetectable.
Write-Host "Recording signature + public key in version.json files..." -ForegroundColor Yellow
foreach ($jsonPath in $JsonFiles) {
    $json = Get-Content $jsonPath -Raw | ConvertFrom-Json
    foreach ($field in @(@{ N = 'signature'; V = $AgentSig }, @{ N = 'signingPublicKeyHex'; V = $SigningPubKey })) {
        if ($json.PSObject.Properties.Name -contains $field.N) {
            $json.($field.N) = $field.V
        } else {
            $json | Add-Member -NotePropertyName $field.N -NotePropertyValue $field.V
        }
    }
    $json | ConvertTo-Json -Depth 10 | Set-Content $jsonPath
    Write-Host "  Recorded: $jsonPath" -ForegroundColor Green
}

# ------------------------------------------------------------------------------
# Git. Binaries are gitignored BY DESIGN (10MB size guard; "binaries do not go
# in git") — commit version files and the installers/ signed sidecars only.
# Native failures throw (PSNativeCommandUseErrorActionPreference), so the tag
# can only ever land on a successful release commit.
# ------------------------------------------------------------------------------
Write-Host ""
Write-Host "Committing changes..." -ForegroundColor Yellow
git -C $ProjectRoot add agent/cmd/sentinel-agent/main.go
git -C $ProjectRoot add agent/cmd/sentinel-watchdog/main.go
git -C $ProjectRoot add agent/version.json
git -C $ProjectRoot add installers/version.json
foreach ($a in $Artifacts) {
    if ($a.Path -like "$ProjectRoot/installers/*") {
        $rel = [System.IO.Path]::GetRelativePath($ProjectRoot, "$($a.Path).manifest.json")
        git -C $ProjectRoot add $rel
    }
}
# release/agent/version.json is tracked despite the release/ ignore rule;
# its sidecars are not (new files under an ignored dir), so they stay local.
git -C $ProjectRoot add release/agent/version.json

$commitMsg = "Release v$Version"
if ($Changelog -ne "") {
    $commitMsg = "Release v$Version - $Changelog"
}
git -C $ProjectRoot commit -m "$commitMsg`n`nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
git -C $ProjectRoot push

# Tag AT the release commit so the tag name and the version files can never
# drift (the guard above already ensured the tag is free, and a failed commit
# or push has already aborted the script before this line).
git -C $ProjectRoot tag -a "v$Version" -m "Release v$Version (signing key $SigningPubKey)"
git -C $ProjectRoot push origin "v$Version"

Write-Host ""
Write-Host "=== Release v$Version built, signed, verified, and committed ===" -ForegroundColor Green

# ------------------------------------------------------------------------------
# Deploy: staged local copy into the serving directory (bind-mounted RO into
# the update-server container). Order matters — binaries + sidecars land
# FIRST while the old version.json still advertises the previous release (so
# no agent is offered the new version until its artifacts are consistent);
# version.json flips LAST. A backup of every replaced file is kept and
# restored automatically if post-deploy verification fails.
# ------------------------------------------------------------------------------
if ($Deploy) {
    Write-Host ""
    Write-Host "Deploying to $DeployTree/installers (staged copy)..." -ForegroundColor Yellow
    $ServeDir  = "$DeployTree/installers"
    $Stamp     = Get-Date -Format "yyyyMMdd-HHmmss"
    $BackupDir = "$ServeDir/.backup-v$Version-$Stamp"
    New-Item -ItemType Directory -Force $BackupDir | Out-Null

    $DeployFiles = [System.Collections.Generic.List[string]]::new()
    foreach ($a in $Artifacts) {
        if ($a.Path -like "$ProjectRoot/installers/*") {
            $DeployFiles.Add($a.Path)
            $DeployFiles.Add("$($a.Path).manifest.json")
        }
    }

    # Atomic per-file replace: rename() on the same filesystem. Move-Item -Force
    # over an existing target is not guaranteed atomic, native mv -f is.
    function Move-IntoPlace {
        param([string]$Tmp, [string]$Dst)
        if ($IsLinux) { mv -f $Tmp $Dst } else { Move-Item $Tmp $Dst -Force }
    }

    # Files the deploy CREATED (no predecessor) must be deleted on restore —
    # otherwise a failed verification leaves a mixed old/new serving state.
    $CreatedFiles = [System.Collections.Generic.List[string]]::new()

    try {
        # Back up whatever is being replaced, then move pairs into place.
        foreach ($src in $DeployFiles) {
            $dst = Join-Path $ServeDir (Split-Path -Leaf $src)
            if (Test-Path $dst) { Copy-Item $dst $BackupDir -Force } else { $CreatedFiles.Add($dst) }
            $tmp = "$dst.staging"
            Copy-Item $src $tmp -Force
            Move-IntoPlace $tmp $dst
        }
        # version.json flips last — this is the moment the release goes live.
        if (Test-Path "$ServeDir/version.json") { Copy-Item "$ServeDir/version.json" $BackupDir -Force }
        Copy-Item "$ProjectRoot/installers/version.json" "$ServeDir/version.json.staging" -Force
        Move-IntoPlace "$ServeDir/version.json.staging" "$ServeDir/version.json"

        # Verify at the serving directory: advertised version, and every served
        # binary's bytes must match its sidecar's version AND sha256.
        $served = (Get-Content "$ServeDir/version.json" -Raw | ConvertFrom-Json).version
        if ($served -ne $Version) { throw "Deploy verification FAILED: serving version '$served', expected '$Version'." }
        foreach ($a in $Artifacts) {
            if ($a.Path -notlike "$ProjectRoot/installers/*") { continue }
            $bin = Join-Path $ServeDir (Split-Path -Leaf $a.Path)
            $m = Get-Content "$bin.manifest.json" -Raw | ConvertFrom-Json
            if ($m.version -ne $Version) { throw "Deploy verification FAILED: sidecar for $bin is v$($m.version), expected v$Version." }
            $actual = (Get-FileHash -Algorithm SHA256 $bin).Hash.ToLowerInvariant()
            if ($actual -ne $m.sha256) { throw "Deploy verification FAILED: served $bin sha256 $actual does not match its sidecar ($($m.sha256))." }
        }
    }
    catch {
        Write-Host "Deploy verification failed — restoring backup from $BackupDir..." -ForegroundColor Red
        Get-ChildItem $ServeDir -Filter '*.staging' -ErrorAction SilentlyContinue | Remove-Item -Force
        foreach ($f in Get-ChildItem $BackupDir) {
            Copy-Item $f.FullName (Join-Path $ServeDir $f.Name) -Force
        }
        foreach ($f in $CreatedFiles) {
            if (Test-Path $f) { Remove-Item $f -Force }
        }
        throw
    }

    Write-Host "Deployment verified at the serving directory: v$Version with matching signed sidecars." -ForegroundColor Green
    Write-Host "Backup of replaced files kept at $BackupDir" -ForegroundColor DarkGray

    # Probe the actual HTTP serving boundary when possible. Positive
    # confirmation only — an unreachable endpoint is reported as UNVERIFIED,
    # never as success.
    $probeUrl = if ($env:SENTINEL_UPDATE_CHECK_URL) { $env:SENTINEL_UPDATE_CHECK_URL } else { "https://sentinel.nexus/api/agent/version?platform=windows&arch=amd64" }
    try {
        $resp = Invoke-RestMethod -Uri $probeUrl -SkipCertificateCheck -TimeoutSec 10
        if ("$($resp.latestVersion)" -eq $Version) {
            Write-Host "HTTP boundary VERIFIED: $probeUrl reports latestVersion=$Version" -ForegroundColor Green
        } else {
            Write-Host "HTTP boundary MISMATCH: $probeUrl reports '$($resp.latestVersion)', expected '$Version'. Investigate before rollout." -ForegroundColor Red
        }
    }
    catch {
        Write-Host "HTTP boundary UNVERIFIED: probe of $probeUrl failed ($($_.Exception.Message)). Filesystem checks passed, but confirm the served endpoint manually." -ForegroundColor Yellow
    }

    Write-Host ""
    Write-Host "NOTE: agents are NOT offered this update until an agent_releases row" -ForegroundColor Yellow
    Write-Host "for v$Version exists (the server suppresses updateAvailable without it)." -ForegroundColor Yellow
    Write-Host "That row is the rollout gate — insert it to begin the (canary) rollout;" -ForegroundColor Yellow
    Write-Host "see scripts/publish-1.77.10-agent_releases.sql for the shape." -ForegroundColor Yellow
}

Write-Host ""
Write-Host "Done! Version $Version is ready." -ForegroundColor Cyan
if (-not $Deploy) {
    Write-Host "Run with -Deploy on the serving host to deploy." -ForegroundColor Yellow
}
