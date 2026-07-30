# Pure decision logic for the signed-release pipeline (scripts/release.ps1).
#
# These two checks are the pipeline's load-bearing verifications, and both of
# them shipped defects that a code review caught only on the second pass: the
# trust anchor reported "no previous key" on every read failure, and the
# served-artifact gate could not match the unsuffixed filenames the update
# server actually falls back to. They live here, free of I/O and of the
# release's side effects, so scripts/test/release-checks.tests.ps1 can assert
# on them directly. Change them only with a test that fails first.

Set-StrictMode -Version Latest

# Resolve-SigningKeyDecision decides whether a release may publish under the
# key derived from the signing private key.
#
#   AnchorRead  — did the trust-anchor read SUCCEED? $false means unknown, and
#                 unknown is never clean: the caller must abort.
#   PreviousKey — key recorded by the last release ('' = none recorded).
#   DerivedKey  — key derived from the private key that will sign.
#   EnvKey      — optional operator cross-check ('' = not supplied).
#   Rotate      — operator passed -RotateSigningKey.
#
# Returns @{ Action = 'proceed'|'abort'; Reason = <code>; Message = <text> }.
function Resolve-SigningKeyDecision {
    param(
        [bool]$AnchorRead,
        [string]$PreviousKey,
        [string]$DerivedKey,
        [string]$EnvKey = '',
        [bool]$Rotate = $false
    )
    $prev = "$PreviousKey".Trim().ToLowerInvariant()
    $derived = "$DerivedKey".Trim().ToLowerInvariant()
    $env = "$EnvKey".Trim().ToLowerInvariant()

    if ($derived -notmatch '^[0-9a-f]{64}$') {
        return @{ Action = 'abort'; Reason = 'derived-malformed'
                  Message = "Derived signing public key is not 64 hex chars ('$derived')." }
    }
    if (-not $AnchorRead) {
        return @{ Action = 'abort'; Reason = 'anchor-unreadable'
                  Message = "The trust anchor could not be read. An unreadable anchor is NOT a fresh fleet — refusing to publish." }
    }
    if ($prev -ne '' -and $prev -notmatch '^[0-9a-f]{64}$') {
        return @{ Action = 'abort'; Reason = 'anchor-malformed'
                  Message = "Trust anchor records a malformed signing key ('$prev')." }
    }
    if ($env -ne '' -and $env -ne $derived) {
        return @{ Action = 'abort'; Reason = 'env-mismatch'
                  Message = "SENTINEL_UPDATE_SIGNING_PUBKEY ($env) does not match the key derived from the private key ($derived)." }
    }
    if ($prev -ne '' -and $prev -ne $derived) {
        if (-not $Rotate) {
            return @{ Action = 'abort'; Reason = 'key-changed'
                      Message = "Signing key changed ($prev -> $derived). Every deployed agent has the previous key embedded and fails closed on anything else. Re-run with -RotateSigningKey only if a manual reinstall path exists for every device." }
        }
        return @{ Action = 'proceed'; Reason = 'rotation-authorized'
                  Message = "Publishing under a ROTATED key ($prev -> $derived). Existing agents will fail closed until reinstalled." }
    }
    if ($prev -eq '') {
        return @{ Action = 'proceed'; Reason = 'tofu'
                  Message = "Trust anchor read OK and records no key — trust-establishing (TOFU) release. Publishing activates the pin for later releases." }
    }
    return @{ Action = 'proceed'; Reason = 'key-matches'
              Message = "Signing key matches the previously published key." }
}

# Filenames the update server can resolve as an agent-family binary. It tries
# "sentinel-<kind>-<platform>-<arch>[.exe]" first and then falls back to the
# UNSUFFIXED "sentinel-<kind>[.exe]" (installer_paths.go findPlatformBinary),
# so both forms must be classified. Excluded: sidecars, signatures, staging
# copies, timestamped backups, and the desktop/installer/packaging artifacts
# that are not part of the signed agent update surface.
function Test-IsAgentArtifactName {
    param([string]$Name)
    if ($Name -match '\.(manifest\.json|sig)$') { return $false }
    if ($Name -match '\.staging-') { return $false }
    if ($Name -match '\.bak') { return $false }
    return $Name -match '^sentinel-(agent|watchdog|bootstrap|verify)(-[a-z0-9]+-[a-z0-9]+)?(\.exe)?$'
}

# Get-ServedArtifactProblems classifies every agent-family binary under the
# given roots against the release being deployed. Any binary the server can
# serve MUST come from this release and carry a sidecar that binds its bytes:
# a stale or unsigned one is announced as this version and served anyway, which
# puts the agents on that target into a permanent fail-closed retry loop.
#
#   Roots          — directories to scan (missing ones are skipped).
#   ExpectedVersion— the release being deployed.
#   HashProvider   — scriptblock: path -> lowercase sha256 hex.
#   Verifier       — scriptblock: (binaryPath, sidecarPath) -> $true if the
#                    Ed25519 signature verifies under this release's key.
#
# Returns an array of problem strings (empty = every served artifact checks out).
function Get-ServedArtifactProblems {
    param(
        [string[]]$Roots,
        [string]$ExpectedVersion,
        [scriptblock]$HashProvider,
        [scriptblock]$Verifier
    )
    $problems = @()
    foreach ($root in $Roots) {
        if (-not (Test-Path $root)) { continue }
        foreach ($f in (Get-ChildItem $root -File -ErrorAction SilentlyContinue)) {
            if (-not (Test-IsAgentArtifactName $f.Name)) { continue }
            $side = "$($f.FullName).manifest.json"
            if (-not (Test-Path $side)) { $problems += "$($f.FullName) — no signed sidecar"; continue }
            try { $m = Get-Content $side -Raw | ConvertFrom-Json }
            catch { $problems += "$($f.FullName) — unreadable sidecar"; continue }
            if ("$($m.version)" -ne $ExpectedVersion) {
                $problems += "$($f.FullName) — sidecar v$($m.version), expected v$ExpectedVersion"; continue
            }
            $actual = (& $HashProvider $f.FullName)
            if ("$actual".ToLowerInvariant() -ne "$($m.sha256)".ToLowerInvariant()) {
                $problems += "$($f.FullName) — sha256 does not match its sidecar"; continue
            }
            if ($m.PSObject.Properties.Name -contains 'signedDowngrade' -and $m.signedDowngrade) {
                $problems += "$($f.FullName) — sidecar authorizes a signed DOWNGRADE (anti-rollback bypass)"; continue
            }
            if (-not (& $Verifier $f.FullName $side)) {
                $problems += "$($f.FullName) — signature does not verify against this release's key"; continue
            }
        }
    }
    # Callers must wrap in @(...) — PowerShell unrolls a returned array, so an
    # empty result arrives as $null otherwise (and .Count throws under
    # StrictMode). Both call sites do this.
    return $problems
}
