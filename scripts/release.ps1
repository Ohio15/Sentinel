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
.PARAMETER DeployOnly
    Skip build/sign/commit/tag and deploy the already-built, already-signed
    artifacts for -Version. Used to retry a deploy that failed verification
    (the tag guard otherwise makes a retry impossible without a version bump).
.PARAMETER DeployTree
    The deploy tree the update server serves from (default: $HOME/Sentinel)
.PARAMETER RotateSigningKey
    Authorize publishing under a signing key that differs from the one the last
    release published. Without this the release ABORTS on key change — every
    deployed agent has the old public key baked in and fails closed forever.
.PARAMETER Changelog
    Optional changelog entry for this release
.EXAMPLE
    pwsh scripts/release.ps1 -Version "1.77.41" -Changelog "First signed release"
    pwsh scripts/release.ps1 -Version "1.77.41" -Deploy
    pwsh scripts/release.ps1 -Version "1.77.41" -DeployOnly   # retry a failed deploy
#>
param(
    [Parameter(Mandatory=$true)]
    [string]$Version,

    [switch]$Deploy,

    [switch]$DeployOnly,

    [string]$DeployTree = (Join-Path ([Environment]::GetFolderPath('UserProfile')) "Sentinel"),

    [switch]$RotateSigningKey,

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

# Validate in-body, not via ValidatePattern: .NET '$' also matches before a
# trailing newline, so an argument with a stray newline would pass an attribute
# check and then flow into the tag name and Go source.
if ($Version -cmatch '\s' -or $Version -notmatch '^\d+\.\d+\.\d+$') {
    throw "Version must be strict semver with no whitespace (got '$Version')."
}
if ($DeployOnly) { $Deploy = $true }

$ReleaseDate = Get-Date -Format "yyyy-MM-dd"

Write-Host "=== Sentinel Release Script ===" -ForegroundColor Cyan
Write-Host "Version: $Version"
Write-Host "Date: $ReleaseDate"
if ($DeployOnly) { Write-Host "Mode: DEPLOY-ONLY (no build, no commit, no tag)" -ForegroundColor Yellow }
Write-Host ""

# The repo root is the parent of scripts/. No fallback: a wrong root must fail.
$ProjectRoot = Split-Path -Parent $PSScriptRoot
if (-not (Test-Path "$ProjectRoot/package.json")) {
    throw "Repo root not found at $ProjectRoot (no package.json). Run this script from a Sentinel checkout."
}

# The trust-anchor decision and the served-artifact classification live in
# scripts/lib/release-checks.ps1 and are covered by
# scripts/test/release-checks.tests.ps1. Both controls shipped defects that a
# review caught only on a second pass, so they are tested, not re-derived here.
. (Join-Path $PSScriptRoot 'lib/release-checks.ps1')

# Test-SameDir compares two directories through symlinks (device:inode on
# Linux; normalized-path fallback elsewhere).
function Test-SameDir {
    param([string]$A, [string]$B)
    if (-not (Test-Path $A) -or -not (Test-Path $B)) { return $false }
    if ($IsLinux) { return (stat -c '%d:%i' $A) -eq (stat -c '%d:%i' $B) }
    return (Resolve-Path $A).Path -eq (Resolve-Path $B).Path
}

# Test-Contains checks real-path containment with separator awareness, so
# "/home/x/Sentinel-keys" is NOT considered inside "/home/x/Sentinel".
function Test-PathContains {
    param([string]$Parent, [string]$Child)
    $p = (Resolve-Path $Parent).Path.TrimEnd([IO.Path]::DirectorySeparatorChar)
    $c = (Resolve-Path $Child).Path
    if ($IsLinux) {
        # Resolve symlinks so a symlinked key can't smuggle material into a tree.
        $p = (readlink -f $p); $c = (readlink -f $c)
    }
    return $c -eq $p -or $c.StartsWith($p + [IO.Path]::DirectorySeparatorChar) -or $c.StartsWith($p + '/')
}

$ServeDir = "$DeployTree/installers"

# Guards BEFORE any mutation: never build from (or into) the deploy tree. A run
# from there would overwrite live, bind-mounted served binaries mid-script.
if (Test-SameDir $ProjectRoot $DeployTree) {
    throw "Refusing to run from the deploy tree ($DeployTree). Build from a dedicated clean checkout."
}
if ((Test-Path $ServeDir) -and (Test-SameDir "$ProjectRoot/installers" $ServeDir)) {
    throw "Build installers dir and serving dir are the same directory. Refusing."
}
if ($Deploy -and -not (Test-Path $ServeDir)) {
    throw "Deploy tree not found at $ServeDir. -Deploy must run on the serving host (use -DeployTree to override)."
}

# ------------------------------------------------------------------------------
# RW-1 signing gate: every release MUST be signed, the public key embedded in
# the binaries MUST be the pair of the private key that signs the manifests,
# AND it must be the same key the previously published release used (the fleet
# has that key baked in and fails closed on anything else).
# ------------------------------------------------------------------------------
if ($DeployOnly) {
    # A redeploy signs nothing — it needs only the PUBLIC key, to build the
    # verifier it re-checks artifacts with. Requiring the private key here would
    # put release key material on the serving host for an operation that never
    # uses it (and would let a deploy retry be blocked by key availability).
    $vj = Get-Content "$ProjectRoot/installers/version.json" -Raw | ConvertFrom-Json
    if ($vj.version -ne $Version) { throw "-DeployOnly: installers/version.json is v$($vj.version), expected v$Version. Nothing to redeploy for this version." }
    $SigningPubKey = "$($vj.signingPublicKeyHex)".Trim().ToLowerInvariant()
    if ($SigningPubKey -notmatch '^[0-9a-f]{64}$') {
        throw "-DeployOnly: installers/version.json records no usable signingPublicKeyHex, so the verifier cannot be built. Run a full release."
    }
    Write-Host "Verifier public key (from installers/version.json): $SigningPubKey" -ForegroundColor DarkGray
    $SigningLdflags = "-X github.com/sentinel/agent/internal/updatesig.SigningPublicKeyHex=$SigningPubKey"
}
else {

$SigningKey = $env:SENTINEL_UPDATE_SIGNING_KEY     # PKCS#8 PEM path for cmd/sign
if ([string]::IsNullOrWhiteSpace($SigningKey)) {
    throw "SENTINEL_UPDATE_SIGNING_KEY is not set. Refusing to build an unsigned release (RW-1)."
}
if (-not (Test-Path $SigningKey)) {
    throw "SENTINEL_UPDATE_SIGNING_KEY points at a missing file: $SigningKey"
}
$SigningKeyReal = (Resolve-Path $SigningKey).Path
if ($IsLinux) { $SigningKeyReal = (readlink -f $SigningKeyReal) }
foreach ($tree in @($ProjectRoot, $DeployTree)) {
    if ((Test-Path $tree) -and (Test-PathContains $tree $SigningKeyReal)) {
        throw "Signing key $SigningKeyReal resolves inside $tree — key material must never be under a git tree or the serving directory."
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
# -passin pass: makes an encrypted key fail immediately instead of blocking the
# release on an interactive prompt (cmd/sign accepts only unencrypted PKCS#8).
$pubPem = & openssl pkey -in $SigningKeyReal -passin pass: -pubout
$pubB64 = ($pubPem | Where-Object { $_ -notmatch '^-----' }) -join ''
$pubDer = [Convert]::FromBase64String($pubB64)
$derHex = [BitConverter]::ToString($pubDer).Replace('-', '').ToLowerInvariant()
if ($pubDer.Length -ne 44 -or -not $derHex.StartsWith('302a300506032b6570032100')) {
    throw "Signing key is not Ed25519 (unexpected SPKI: $derHex)."
}
$SigningPubKey = $derHex.Substring(24)

# Pin 1 (optional operator cross-check via SENTINEL_UPDATE_SIGNING_PUBKEY) and
# Pin 2 (the mandatory anchor) are both evaluated by Resolve-SigningKeyDecision
# below, so neither can be satisfied by the other's absence.
# Pin 2 (MANDATORY): the key the last release published — the anchor the
# deployed fleet actually trusts. Read from the newest release TAG (immutable
# and repo-wide) rather than HEAD, so checking out an older commit cannot make
# the anchor vanish. UNKNOWN IS NEVER TREATED AS CLEAN: any failure to read the
# anchor ABORTS. Only a successful read of a file that genuinely lacks the
# field is a trust-establishing (TOFU) release.
$anchorRef = (git -C $ProjectRoot tag -l "v*" --sort=-v:refname | Select-Object -First 1)
if ([string]::IsNullOrWhiteSpace($anchorRef)) { $anchorRef = 'HEAD' }
$anchorPath = "${anchorRef}:installers/version.json"
# $anchorRead stays $false unless the read demonstrably succeeded — the decision
# function treats "unknown" as abort-worthy, never as a fresh fleet.
$anchorRead = $false
$prevPub = ''
try {
    $anchorRaw = (git -C $ProjectRoot show $anchorPath | Out-String)
    if (-not [string]::IsNullOrWhiteSpace($anchorRaw)) {
        $anchorJson = $anchorRaw | ConvertFrom-Json
        $anchorRead = $true
        if ($anchorJson.PSObject.Properties.Name -contains 'signingPublicKeyHex') {
            $prevPub = "$($anchorJson.signingPublicKeyHex)"
        }
    }
}
catch { $anchorRead = $false }

$keyDecision = Resolve-SigningKeyDecision -AnchorRead $anchorRead -PreviousKey $prevPub `
                   -DerivedKey $SigningPubKey -EnvKey "$($env:SENTINEL_UPDATE_SIGNING_PUBKEY)" `
                   -Rotate ([bool]$RotateSigningKey)
Write-Host "Trust anchor: $anchorPath — $($keyDecision.Reason)" -ForegroundColor DarkGray
if ($keyDecision.Action -eq 'abort') {
    throw "REFUSING TO PUBLISH ($($keyDecision.Reason)): $($keyDecision.Message)"
}
if ($keyDecision.Reason -eq 'rotation-authorized') { Write-Host "WARNING: $($keyDecision.Message)" -ForegroundColor Red }
elseif ($keyDecision.Reason -eq 'tofu') { Write-Host "NOTE: $($keyDecision.Message)" -ForegroundColor Yellow }
Write-Host "Signing public key (derived from private key): $SigningPubKey" -ForegroundColor DarkGray
$SigningLdflags = "-X github.com/sentinel/agent/internal/updatesig.SigningPublicKeyHex=$SigningPubKey"

} # end of full-release signing gate (skipped for -DeployOnly)

# ------------------------------------------------------------------------------
# Target matrix. Every (platform, arch) the update server ADVERTISES must get a
# signed binary here, or its agents fail closed — enforced by the served-state
# check in the deploy block, not by comment. Per-target command lists: the
# watchdog is Windows-only (its !windows build is a stub that exits 64), so
# publishing a signed Linux watchdog would ship a signed broken binary.
# Fleet census 2026-07-29: all windows/amd64 + one ubuntu/amd64.
# ------------------------------------------------------------------------------
$Targets = @(
    @{ Platform = "windows"; Arch = "amd64"; Ext = ".exe"
       Commands = @("sentinel-agent", "sentinel-watchdog", "sentinel-bootstrap", "verify") },
    @{ Platform = "linux";   Arch = "amd64"; Ext = ""
       Commands = @("sentinel-agent", "sentinel-bootstrap", "verify") }
)
# cmd dir -> published binary basename
$BinaryName = @{ "sentinel-agent" = "sentinel-agent"; "sentinel-watchdog" = "sentinel-watchdog"
                 "sentinel-bootstrap" = "sentinel-bootstrap"; "verify" = "sentinel-verify" }

$JsonFiles = @(
    "$ProjectRoot/agent/version.json",
    "$ProjectRoot/release/agent/version.json",
    "$ProjectRoot/installers/version.json"
)

# Artifacts accumulate as @{Path; Platform; Arch} for signing/verify/deploy.
$Artifacts = [System.Collections.Generic.List[hashtable]]::new()
foreach ($t in $Targets) {
    foreach ($cmd in $t.Commands) {
        $Artifacts.Add(@{
            Path     = "$ProjectRoot/installers/$($BinaryName[$cmd])-$($t.Platform)-$($t.Arch)$($t.Ext)"
            Platform = $t.Platform; Arch = $t.Arch; Cmd = $cmd
        })
    }
}

$HostToolDir = $null
$lock = $null
try {

# Single-writer lock for the WHOLE run: two concurrent releases in one checkout
# would otherwise race over the working tree, the version files and the git
# index long before either reached the deploy stage.
$LockFile = Join-Path ([Environment]::GetFolderPath('UserProfile')) ".sentinel-release.lock"
try { $lock = [IO.File]::Open($LockFile, 'OpenOrCreate', 'Write', 'None') }
catch [System.IO.IOException] { throw "Another release is in progress (lock held: $LockFile). Refusing to interleave." }
catch { throw "Cannot acquire the release lock $LockFile : $($_.Exception.Message)" }

if (-not $DeployOnly) {

    # Guard: refuse a version whose tag already exists (locally or on origin).
    # Tags are permanent — v1.77.40 was burned on a commit whose version files
    # still said 1.77.39, so tag-name/file drift is a real, observed failure mode.
    $existingLocal  = git -C $ProjectRoot tag -l "v$Version"
    $existingRemote = git -C $ProjectRoot ls-remote --tags origin "refs/tags/v$Version"
    if ($existingLocal -or $existingRemote) {
        throw "Tag v$Version already exists (local: '$existingLocal', remote: '$existingRemote'). Pick the next free version, or use -DeployOnly to retry deploying this version."
    }
    # Refuse to release from a branch whose upstream has moved — a push failing
    # after the release commit exists is a manual-intervention state.
    git -C $ProjectRoot fetch origin --quiet
    $branch = (git -C $ProjectRoot rev-parse --abbrev-ref HEAD).Trim()
    if ($branch -eq 'HEAD') { throw "Detached HEAD — check out the release branch before releasing." }
    # No upstream is fine (a fresh branch); a MOVED upstream is not. The old
    # $LASTEXITCODE guard was dead code under terminating native errors.
    $behind = $null
    try { $behind = (git -C $ProjectRoot rev-list --count "HEAD..origin/$branch") } catch { $behind = $null }
    if ($null -ne $behind -and "$behind".Trim() -ne '0') {
        throw "Branch $branch is $("$behind".Trim()) commit(s) behind origin/$branch. Pull before releasing."
    }

    # --------------------------------------------------------------------------
    # Version updates. EVERY published binary's version string must be bumped —
    # a sidecar attesting v$Version for a binary that self-reports something
    # else is an audit lie (sentinel-bootstrap shipped as 1.0.0 this way).
    # --------------------------------------------------------------------------
    $FilesToUpdate = @(
        @{ Path = "$ProjectRoot/agent/cmd/sentinel-agent/main.go"
           Pattern = 'var Version = "[^"]*"';  Replacement = "var Version = `"$Version`"" },
        # The Windows watchdog declares Version inside a var(...) block, so this
        # pattern is anchored to its own line rather than the `var ` keyword.
        @{ Path = "$ProjectRoot/agent/cmd/sentinel-watchdog/main.go"
           Pattern = '(?m)^\tVersion = "[^"]*"';  Replacement = "`tVersion = `"$Version`"" },
        @{ Path = "$ProjectRoot/agent/cmd/sentinel-watchdog/main_other.go"
           Pattern = 'var Version = "[^"]*"';  Replacement = "var Version = `"$Version`"" },
        @{ Path = "$ProjectRoot/agent/cmd/sentinel-bootstrap/main.go"
           Pattern = 'var Version = "[^"]*"';  Replacement = "var Version = `"$Version`"" }
        # NOTE: root package.json is deliberately NOT updated here — it tracks the
        # server/repo version line, which diverges from the agent line on
        # server-only releases. Bumping it to the agent version could silently
        # DOWNGRADE it (server line is ahead, e.g. 1.78.x vs agent 1.77.x).
    )

    Write-Host "Updating version in source files..." -ForegroundColor Yellow
    foreach ($file in $FilesToUpdate) {
        if (-not (Test-Path $file.Path)) { throw "Version file not found: $($file.Path)" }
        $content = Get-Content $file.Path -Raw
        if ($content -notmatch $file.Pattern) { throw "Version pattern not found in $($file.Path) — refusing to publish a binary with an unbumped version." }
        # Escape '$' in the replacement so a version can never be interpreted as
        # a .NET substitution token.
        Set-Content $file.Path ($content -replace $file.Pattern, $file.Replacement.Replace('$', '$$')) -NoNewline
        Write-Host "  Updated: $($file.Path)" -ForegroundColor Green
    }

    Write-Host "Updating version.json files..." -ForegroundColor Yellow
    foreach ($jsonPath in $JsonFiles) {
        if (-not (Test-Path $jsonPath)) { throw "version.json not found: $jsonPath" }
        $json = Get-Content $jsonPath -Raw | ConvertFrom-Json
        $json.version = $Version
        $json.releaseDate = $ReleaseDate
        if ($Changelog -ne "") { $json.changelog = $Changelog }
        $json | ConvertTo-Json -Depth 10 | Set-Content $jsonPath
        Write-Host "  Updated: $jsonPath" -ForegroundColor Green
    }

    # --------------------------------------------------------------------------
    # Build. Host tools go in a private mode-700 temp dir (mktemp -d, not a
    # predictable name): sentinel-sign is the one binary that reads the signing
    # key, so a pre-created world-writable path would be a key-exfil TOCTOU.
    # --------------------------------------------------------------------------
    $HostToolDir = if ($IsLinux) { (mktemp -d "/tmp/sentinel-release-XXXXXXXX").Trim() }
                   else { (New-Item -ItemType Directory -Force (Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString()))).FullName }

    Push-Location "$ProjectRoot/agent"
    try {
        Write-Host ""
        Write-Host "Building host tools (cmd/sign, cmd/verify)..." -ForegroundColor Yellow
        # Clear any inherited cross-compile env FIRST — host tools must be
        # native or the signing step dies after all files have been mutated.
        Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
        $env:CGO_ENABLED = "0"
        $SignTool   = Join-Path $HostToolDir "sentinel-sign"
        $VerifyTool = Join-Path $HostToolDir "sentinel-verify"
        go build -o $SignTool ./cmd/sign
        # The verifier gets the DERIVED pubkey embedded — running it against
        # every signed artifact proves end-to-end that the key that signed is
        # the pair of the key the fleet binaries will trust.
        go build -ldflags $SigningLdflags -o $VerifyTool ./cmd/verify

        Write-Host "Building agent binaries..." -ForegroundColor Yellow
        foreach ($a in $Artifacts) {
            Write-Host "  Building $($a.Path)"
            $env:GOOS = $a.Platform; $env:GOARCH = $a.Arch; $env:CGO_ENABLED = "0"
            go build -ldflags $SigningLdflags -o $a.Path "./cmd/$($a.Cmd)"
        }
    }
    finally {
        Pop-Location
        Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
    }

    # --------------------------------------------------------------------------
    # RW-1: sign every artifact with its OWN platform/arch tuple, then verify
    # every signature with the independently built verifier. cmd/sign writes a
    # "<file>.manifest.json" sidecar binding {version, platform, arch, sha256,
    # signedDowngrade} under one Ed25519 signature; the server serves the
    # sidecar next to the binary and agents rebuild + verify that manifest.
    # --------------------------------------------------------------------------
    Write-Host ""
    Write-Host "Signing artifacts (Ed25519 signed manifest)..." -ForegroundColor Yellow
    $env:SENTINEL_UPDATE_SIGNING_KEY = $SigningKeyReal
    foreach ($a in $Artifacts) {
        # No signed downgrade on a forward release — anti-rollback holds.
        $sig = & $SignTool -version $Version -platform $a.Platform -arch $a.Arch $a.Path
        $a.Signature = "$sig".Trim()
        Write-Host "  Signed ($($a.Platform)/$($a.Arch)): $($a.Path)" -ForegroundColor Green
    }

    Write-Host "Verifying every signature end-to-end (independent verifier)..." -ForegroundColor Yellow
    foreach ($a in $Artifacts) {
        & $VerifyTool -binary $a.Path -manifest "$($a.Path).manifest.json"
        Write-Host "  Verified: $($a.Path)" -ForegroundColor Green
    }

    # Record the primary-platform agent signature AND the signing public key in
    # version.json. The sidecars remain the trust source (agents fail closed
    # without them); signingPublicKeyHex is the durable audit record AND the
    # anchor Pin 2 reads on the next release.
    $primary = $Artifacts | Where-Object { $_.Cmd -eq 'sentinel-agent' -and $_.Platform -eq 'windows' -and $_.Arch -eq 'amd64' }
    if (-not $primary -or [string]::IsNullOrWhiteSpace($primary.Signature)) {
        throw "Primary windows/amd64 agent signature missing — refusing to write a null signature into version.json."
    }
    Write-Host "Recording signature + public key in version.json files..." -ForegroundColor Yellow
    foreach ($jsonPath in $JsonFiles) {
        $json = Get-Content $jsonPath -Raw | ConvertFrom-Json
        foreach ($field in @(@{ N = 'signature'; V = $primary.Signature }, @{ N = 'signingPublicKeyHex'; V = $SigningPubKey })) {
            if ($json.PSObject.Properties.Name -contains $field.N) { $json.($field.N) = $field.V }
            else { $json | Add-Member -NotePropertyName $field.N -NotePropertyValue $field.V }
        }
        $json | ConvertTo-Json -Depth 10 | Set-Content $jsonPath
        Write-Host "  Recorded: $jsonPath" -ForegroundColor Green
    }

    # --------------------------------------------------------------------------
    # Git. Binaries are gitignored BY DESIGN (10MB size guard; "binaries do not
    # go in git") — commit version files and the installers/ signed sidecars
    # only. Native failures throw, so the tag can only ever land on a
    # successful release commit.
    # --------------------------------------------------------------------------
    Write-Host ""
    Write-Host "Committing changes..." -ForegroundColor Yellow
    foreach ($f in @("agent/cmd/sentinel-agent/main.go", "agent/cmd/sentinel-watchdog/main.go",
                     "agent/cmd/sentinel-watchdog/main_other.go", "agent/cmd/sentinel-bootstrap/main.go",
                     "agent/version.json", "installers/version.json")) {
        git -C $ProjectRoot add $f
    }
    # release/agent/version.json is tracked but sits under the ignored release/
    # directory, so a plain `git add` on it exits 1 ("paths are ignored"). The
    # pre-rewrite script never checked git exit codes, so this add failed
    # SILENTLY for every release and the tracked file drifted. -f is correct and
    # narrow here: one known, explicitly tracked, human-readable JSON file.
    git -C $ProjectRoot add -f release/agent/version.json
    foreach ($a in $Artifacts) {
        git -C $ProjectRoot add ([IO.Path]::GetRelativePath($ProjectRoot, "$($a.Path).manifest.json"))
    }

    $commitMsg = "Release v$Version"
    if ($Changelog -ne "") { $commitMsg = "Release v$Version - $Changelog" }
    git -C $ProjectRoot commit -m "$commitMsg`n`nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
    git -C $ProjectRoot push

    # Tag AT the release commit so the tag name and the version files can never
    # drift (the guard above ensured the tag is free, and a failed commit or
    # push has already aborted the script before this line).
    git -C $ProjectRoot tag -a "v$Version" -m "Release v$Version (signing key $SigningPubKey)"
    git -C $ProjectRoot push origin "v$Version"

    Write-Host ""
    Write-Host "=== Release v$Version built, signed, verified, and committed ===" -ForegroundColor Green
}
else {
    # DeployOnly: the artifacts and sidecars must already exist and verify.
    $HostToolDir = if ($IsLinux) { (mktemp -d "/tmp/sentinel-release-XXXXXXXX").Trim() }
                   else { (New-Item -ItemType Directory -Force (Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString()))).FullName }
    $VerifyTool = Join-Path $HostToolDir "sentinel-verify"
    Push-Location "$ProjectRoot/agent"
    try {
        Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
        $env:CGO_ENABLED = "0"
        go build -ldflags $SigningLdflags -o $VerifyTool ./cmd/verify
    } finally { Pop-Location; Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue }

    Write-Host "Re-verifying existing artifacts for v$Version..." -ForegroundColor Yellow
    foreach ($a in $Artifacts) {
        if (-not (Test-Path $a.Path)) { throw "-DeployOnly: missing artifact $($a.Path). Run a full release." }
        $m = Get-Content "$($a.Path).manifest.json" -Raw | ConvertFrom-Json
        # cmd/verify only proves the sidecar is self-consistent against the
        # embedded key — it takes version/platform/arch FROM the sidecar. The
        # expected-value binding has to happen here or a validly-signed sidecar
        # for the wrong target (or one authorizing a downgrade) would pass.
        if ($m.version -ne $Version)   { throw "-DeployOnly: $($a.Path) sidecar is v$($m.version), expected v$Version." }
        if ($m.platform -ne $a.Platform -or $m.arch -ne $a.Arch) { throw "-DeployOnly: $($a.Path) sidecar claims $($m.platform)/$($m.arch), expected $($a.Platform)/$($a.Arch)." }
        if ($m.signedDowngrade)        { throw "-DeployOnly: $($a.Path) sidecar authorizes a signed downgrade — refusing (anti-rollback)." }
        & $VerifyTool -binary $a.Path -manifest "$($a.Path).manifest.json"
        Write-Host "  Verified: $($a.Path)" -ForegroundColor Green
    }
}

# ------------------------------------------------------------------------------
# Deploy: staged local copy into the serving directory (bind-mounted RO into
# the update-server container). Order matters — binaries + sidecars land FIRST
# while the old version.json still advertises the previous release (so no agent
# is offered the new version until its artifacts are consistent); version.json
# flips LAST. Backups live OUTSIDE the serving directory: that directory is
# published unauthenticated at /installers/, so a backup inside it would
# permanently expose every superseded binary and its valid signature.
# ------------------------------------------------------------------------------
if ($Deploy) {
    Write-Host ""
    Write-Host "Deploying to $ServeDir (staged copy)..." -ForegroundColor Yellow

    $Stamp     = Get-Date -Format "yyyyMMdd-HHmmss"
    $BackupRoot = Join-Path ([Environment]::GetFolderPath('UserProfile')) ".sentinel-release-backups"
    $BackupDir = Join-Path $BackupRoot "v$Version-$Stamp"
    New-Item -ItemType Directory -Force $BackupDir | Out-Null
    # These hold every superseded signed binary — keep them owner-only.
    if ($IsLinux) { chmod 700 $BackupRoot; chmod 700 $BackupDir }

    # Sweep staging copies orphaned by a previous kill/reboot. They are full
    # binaries sitting in a directory published unauthenticated at /installers/,
    # and the staleness gate deliberately ignores them, so nothing else reports
    # them. Only this script ever creates these names.
    $orphans = @(Get-ChildItem $ServeDir -Filter '*.staging-*' -File -ErrorAction SilentlyContinue)
    if ($orphans.Count -gt 0) {
        Write-Host "Removing $($orphans.Count) orphaned staging file(s) from the served directory: $($orphans.Name -join ', ')" -ForegroundColor Yellow
        $orphans | Remove-Item -Force
    }

    $DeployFiles = [System.Collections.Generic.List[string]]::new()
    foreach ($a in $Artifacts) {
        $DeployFiles.Add($a.Path)
        $DeployFiles.Add("$($a.Path).manifest.json")
    }

    # Atomic per-file replace: rename() on the same filesystem. Move-Item -Force
    # over an existing target is not guaranteed atomic; native mv -f is.
    function Move-IntoPlace {
        param([string]$Tmp, [string]$Dst)
        if ($IsLinux) { mv -f $Tmp $Dst } else { Move-Item $Tmp $Dst -Force }
    }

    # Files the deploy CREATED (no predecessor) must be deleted on restore —
    # otherwise a failed verification leaves a mixed old/new serving state.
    $CreatedFiles = [System.Collections.Generic.List[string]]::new()
    # Staging names are per-process so a concurrent/stale run can't collide or
    # have its in-flight files deleted by our cleanup.
    $StagingSuffix = ".staging-$PID"

    try {
        foreach ($src in $DeployFiles) {
            $dst = Join-Path $ServeDir (Split-Path -Leaf $src)
            if (Test-Path $dst) { Copy-Item $dst $BackupDir -Force } else { $CreatedFiles.Add($dst) }
            $tmp = "$dst$StagingSuffix"
            Copy-Item $src $tmp -Force
            Move-IntoPlace $tmp $dst
        }
        # version.json flips last — this is the moment the release goes live.
        # It needs the same created-file tracking as the artifacts, or a rollback
        # leaves it advertising this version with nothing behind it.
        if (Test-Path "$ServeDir/version.json") { Copy-Item "$ServeDir/version.json" $BackupDir -Force }
        else { $CreatedFiles.Add("$ServeDir/version.json") }
        Copy-Item "$ProjectRoot/installers/version.json" "$ServeDir/version.json$StagingSuffix" -Force
        Move-IntoPlace "$ServeDir/version.json$StagingSuffix" "$ServeDir/version.json"

        # ---- Verify at the serving directory ----
        $served = (Get-Content "$ServeDir/version.json" -Raw | ConvertFrom-Json).version
        if ($served -ne $Version) { throw "Deploy verification FAILED: serving version '$served', expected '$Version'." }
        foreach ($a in $Artifacts) {
            $bin = Join-Path $ServeDir (Split-Path -Leaf $a.Path)
            $m = Get-Content "$bin.manifest.json" -Raw | ConvertFrom-Json
            if ($m.version -ne $Version) { throw "Deploy verification FAILED: sidecar for $bin is v$($m.version), expected v$Version." }
            if ($m.platform -ne $a.Platform -or $m.arch -ne $a.Arch) { throw "Deploy verification FAILED: sidecar for $bin claims $($m.platform)/$($m.arch), expected $($a.Platform)/$($a.Arch)." }
            if ($m.signedDowngrade) { throw "Deploy verification FAILED: sidecar for $bin authorizes a signed downgrade." }
            $actual = (Get-FileHash -Algorithm SHA256 $bin).Hash.ToLowerInvariant()
            if ($actual -ne $m.sha256) { throw "Deploy verification FAILED: served $bin sha256 $actual does not match its sidecar ($($m.sha256))." }
            # Re-verify the SIGNATURE at the boundary that actually serves it,
            # not just the hash (the verifier is kept alive for this).
            & $VerifyTool -binary $bin -manifest "$bin.manifest.json"
        }

        # ---- Enforce "every served agent/watchdog binary is from THIS signed
        # release". The update server advertises 7 (platform,arch) tuples and
        # falls back to unsuffixed names in release/agent; any stale artifact
        # left here is announced as $Version and served unsigned, sending those
        # agents into a permanent fail-closed retry loop. This is the check
        # that makes the invariant real instead of a comment.
        # Classification lives in scripts/lib/release-checks.ps1 (tested by
        # scripts/test/release-checks.tests.ps1). Both serving roots are
        # scanned: installers/ (the server's first search root) and
        # release/agent (also bind-mounted, and the source of the unsuffixed
        # fallback the server uses for linux-arm64 and darwin-*).
        $stale = @(Get-ServedArtifactProblems -Roots @($ServeDir, "$DeployTree/release/agent") `
                       -ExpectedVersion $Version `
                       -HashProvider { param($p) (Get-FileHash -Algorithm SHA256 $p).Hash.ToLowerInvariant() } `
                       -Verifier {
                           param($b, $s)
                           try { & $VerifyTool -binary $b -manifest $s | Out-Null; return $true }
                           catch { return $false }
                       })
        if ($stale.Count -gt 0) {
            throw @"
Deploy verification FAILED: served artifacts that are NOT from this signed release:
$($stale -join "`n")
The update server advertises these targets and will announce v$Version while
serving these bytes — agents on them fail closed permanently. Archive or
rebuild them, then re-run with -DeployOnly.
"@
        }
    }
    catch {
        Write-Host "Deploy verification failed — restoring backup from $BackupDir..." -ForegroundColor Red
        Get-ChildItem $ServeDir -Filter "*$StagingSuffix" -ErrorAction SilentlyContinue | Remove-Item -Force
        # Restore atomically too: clients are reading these files live.
        foreach ($f in Get-ChildItem $BackupDir -File) {
            $dst = Join-Path $ServeDir $f.Name
            $tmp = "$dst$StagingSuffix"
            Copy-Item $f.FullName $tmp -Force
            Move-IntoPlace $tmp $dst
        }
        foreach ($f in $CreatedFiles) { if (Test-Path $f) { Remove-Item $f -Force } }
        # Positive confirmation that the restore actually landed. A "verified"
        # verdict must mean the check RAN and found nothing — an empty backup
        # set is not proof of a good restore, so it is reported separately.
        $bad = @()
        $restored = @(Get-ChildItem $BackupDir -File)
        foreach ($f in $restored) {
            $dst = Join-Path $ServeDir $f.Name
            if (-not (Test-Path $dst) -or
                (Get-FileHash -Algorithm SHA256 $dst).Hash -ne (Get-FileHash -Algorithm SHA256 $f.FullName).Hash) { $bad += $dst }
        }
        $leftover = @($CreatedFiles | Where-Object { Test-Path $_ })
        if ($bad.Count -gt 0 -or $leftover.Count -gt 0) {
            Write-Host "RESTORE INCOMPLETE — mismatched: $($bad -join ', '); undeleted new files: $($leftover -join ', '). Serving state is INCONSISTENT; fix by hand." -ForegroundColor Red
        }
        elseif ($restored.Count -eq 0) {
            Write-Host "Nothing to restore (the serving directory held no previous copies of these files). Every file this deploy created was removed; the serving directory is back to empty for these artifacts — confirm it is serving what you expect." -ForegroundColor Yellow
        }
        else {
            Write-Host "Restore verified: $($restored.Count) file(s) match the pre-deploy backup and all newly created files were removed." -ForegroundColor Yellow
        }
        throw
    }

    Write-Host "Deployment verified at the serving directory: v$Version, signatures re-checked, no stale served artifacts." -ForegroundColor Green
    Write-Host "Backup of replaced files: $BackupDir (outside the served volume)" -ForegroundColor DarkGray

    # The deploy tree is a git checkout and installers/version.json + sidecars
    # are TRACKED. We just overwrote them, so the tree is now dirty by design:
    # a future `git pull`/`checkout .`/`stash` there would silently revert the
    # served release. Report it — rule 11 (a branch move in a deploy tree IS a
    # deployment) applies in reverse here.
    # Guarded: a git failure here (not a work tree, dubious-ownership refusal)
    # must not turn a fully verified deploy into a non-zero exit.
    $dirty = $null
    try { $dirty = git -C $DeployTree status --porcelain -- installers } catch { $dirty = $null }
    if ($dirty) {
        Write-Host ""
        Write-Host "NOTE: $DeployTree has local changes under installers/ (expected — the deploy wrote them)." -ForegroundColor Yellow
        Write-Host "A git pull/checkout/stash in that tree would REVERT the served release. Deploy only via this script." -ForegroundColor Yellow
    }

    # Probe the real HTTP serving boundary. The server caches version.json for
    # 60s, so poll rather than firing once (a single immediate probe reports a
    # false MISMATCH on a perfectly good deploy). TLS verification stays ON —
    # a "VERIFIED" derived from an unauthenticated channel is not verification.
    $probeUrl = "$($env:SENTINEL_UPDATE_CHECK_URL)".Trim()
    if ($probeUrl -eq '') {
        # There is deliberately no default: the internal hostname uses a private
        # TLD no public CA can certify, so a default probe with TLS verification
        # on could only ever fail — 100 seconds of guaranteed "UNVERIFIED" reads
        # as noise and trains operators to ignore the one real check.
        Write-Host "HTTP boundary NOT PROBED: set SENTINEL_UPDATE_CHECK_URL to an https URL this host can verify. Filesystem checks passed; the served endpoint is UNCONFIRMED." -ForegroundColor Yellow
    }
    elseif ($probeUrl -notmatch '^https://') {
        throw "SENTINEL_UPDATE_CHECK_URL must be https (got '$probeUrl') — a verdict from an unauthenticated channel is not verification."
    }
    else {
        $deadline = (Get-Date).AddSeconds(100)
        $probeState = 'UNVERIFIED'; $probeDetail = ''
        while ((Get-Date) -lt $deadline) {
            try {
                $resp = Invoke-RestMethod -Uri $probeUrl -TimeoutSec 10
                if ("$($resp.latestVersion)" -eq $Version) { $probeState = 'VERIFIED'; break }
                # Could still be the server's 60s version cache — keep polling.
                $probeState = 'MISMATCH'; $probeDetail = "reports '$($resp.latestVersion)'"
            }
            catch { $probeState = 'UNVERIFIED'; $probeDetail = $_.Exception.Message }
            Start-Sleep -Seconds 10
        }
        switch ($probeState) {
            'VERIFIED'  { Write-Host "HTTP boundary VERIFIED: $probeUrl reports latestVersion=$Version" -ForegroundColor Green }
            # A positive read of the WRONG version outlasting the cache TTL is a
            # real disagreement between the serving dir and what is served —
            # that is a failure, not a warning.
            'MISMATCH'  { throw "HTTP boundary MISMATCH after the cache TTL: $probeUrl $probeDetail, expected '$Version'. The deployed files and the served response disagree — do NOT open the rollout gate." }
            default     { Write-Host "HTTP boundary UNVERIFIED: probe of $probeUrl failed ($probeDetail). Filesystem checks passed; the served endpoint is UNCONFIRMED — confirm manually." -ForegroundColor Yellow }
        }
    }

    Write-Host ""
    Write-Host "NOTE: agents are NOT offered this update until an agent_releases row" -ForegroundColor Yellow
    Write-Host "for v$Version exists (the server suppresses updateAvailable without it)." -ForegroundColor Yellow
    Write-Host "That row is the rollout gate and it is FLEET-WIDE — inserting it announces" -ForegroundColor Yellow
    Write-Host "to every online agent. See docs/SIGNED-RELEASE-CANARY-PLAN.md." -ForegroundColor Yellow
}

Write-Host ""
Write-Host "Done! Version $Version is ready." -ForegroundColor Cyan
if (-not $Deploy) {
    Write-Host "Run with -Deploy on the serving host to deploy." -ForegroundColor Yellow
}

}
finally {
    if ($HostToolDir -and (Test-Path $HostToolDir)) { Remove-Item -Recurse -Force $HostToolDir }
    if ($lock) { $lock.Dispose() }
}
