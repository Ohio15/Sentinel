# Regression tests for the signed-release pipeline's load-bearing checks
# (scripts/lib/release-checks.ps1).
#
# Why these exist: two consecutive adversarial reviews found defects in the SAME
# two controls — the trust anchor treated every read failure as "no previous
# key" (silent TOFU, fleet-trust re-root), and the served-artifact gate could
# not match the unsuffixed filenames the update server falls back to (stale
# unsigned binaries served while the deploy reported clean). Reviews find those
# classes once; tests keep them found. Every case below is a defect that was
# actually shipped or actually possible.
#
# Run:  pwsh -File scripts/test/release-checks.tests.ps1
# Exit: 0 = all pass, 1 = failures (suitable for CI).

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot '../lib/release-checks.ps1')

$script:pass = 0; $script:fail = 0
function Assert-Equal {
    param($Expected, $Actual, [string]$What)
    if ("$Expected" -eq "$Actual") { $script:pass++; Write-Host "  PASS  $What" -ForegroundColor DarkGreen }
    else { $script:fail++; Write-Host "  FAIL  $What`n        expected: $Expected`n        actual:   $Actual" -ForegroundColor Red }
}
function Assert-True { param([bool]$Cond, [string]$What) Assert-Equal $true $Cond $What }

# Synthetic 64-hex stand-ins. Deliberately NOT the production signing public
# key: these checks are key-agnostic, and hardcoding the real value would put a
# high-entropy production constant in the test suite for no benefit.
$KEY_A = 'a1' * 32
$KEY_B = 'b2' * 32

Write-Host "`nResolve-SigningKeyDecision" -ForegroundColor Cyan

# The CRITICAL defect: the anchor read failing must ABORT, never degrade to TOFU.
$d = Resolve-SigningKeyDecision -AnchorRead $false -PreviousKey '' -DerivedKey $KEY_A
Assert-Equal 'abort' $d.Action 'unreadable anchor aborts (does not fall through to TOFU)'
Assert-Equal 'anchor-unreadable' $d.Reason 'unreadable anchor is reported as unknown, not as a fresh fleet'

# ...and it must abort even when the operator supplies a matching env key, so a
# stale environment cannot stand in for the anchor.
$d = Resolve-SigningKeyDecision -AnchorRead $false -PreviousKey '' -DerivedKey $KEY_A -EnvKey $KEY_A
Assert-Equal 'abort' $d.Action 'unreadable anchor aborts even with a matching env cross-check'

# The HIGH defect: unsetting the env var must NOT disable the pin.
$d = Resolve-SigningKeyDecision -AnchorRead $true -PreviousKey $KEY_A -DerivedKey $KEY_B -EnvKey ''
Assert-Equal 'abort' $d.Action 'key change aborts with no env var set (pin is not env-dependent)'
Assert-Equal 'key-changed' $d.Reason 'key change is reported as such'

$d = Resolve-SigningKeyDecision -AnchorRead $true -PreviousKey $KEY_A -DerivedKey $KEY_B -Rotate $true
Assert-Equal 'proceed' $d.Action 'explicit -RotateSigningKey authorizes a key change'
Assert-Equal 'rotation-authorized' $d.Reason 'rotation is recorded distinctly from a normal publish'

$d = Resolve-SigningKeyDecision -AnchorRead $true -PreviousKey '' -DerivedKey $KEY_A
Assert-Equal 'tofu' $d.Reason 'successful read with no recorded key is a genuine TOFU release'

$d = Resolve-SigningKeyDecision -AnchorRead $true -PreviousKey $KEY_A -DerivedKey $KEY_A
Assert-Equal 'key-matches' $d.Reason 'matching key proceeds'

$d = Resolve-SigningKeyDecision -AnchorRead $true -PreviousKey $KEY_A -DerivedKey $KEY_A -EnvKey $KEY_B
Assert-Equal 'env-mismatch' $d.Reason 'stale env cross-check aborts even when the anchor agrees'

$d = Resolve-SigningKeyDecision -AnchorRead $true -PreviousKey $KEY_A -DerivedKey $KEY_A -EnvKey "  $($KEY_A.ToUpper())  "
Assert-Equal 'key-matches' $d.Reason 'env cross-check tolerates whitespace and case'

$d = Resolve-SigningKeyDecision -AnchorRead $true -PreviousKey 'not-a-key' -DerivedKey $KEY_A
Assert-Equal 'anchor-malformed' $d.Reason 'malformed recorded key aborts rather than being treated as absent'

$d = Resolve-SigningKeyDecision -AnchorRead $true -PreviousKey '' -DerivedKey 'zzzz'
Assert-Equal 'derived-malformed' $d.Reason 'malformed derived key aborts'

Write-Host "`nTest-IsAgentArtifactName" -ForegroundColor Cyan

# The HIGH defect: the server falls back to UNSUFFIXED names for linux/darwin,
# and the original gate regex required a '-' or terminal '.exe', so these
# slipped through entirely.
foreach ($n in @('sentinel-agent', 'sentinel-watchdog', 'sentinel-bootstrap', 'sentinel-verify')) {
    Assert-True (Test-IsAgentArtifactName $n) "unsuffixed fallback name '$n' is classified as a served artifact"
}
foreach ($n in @('sentinel-agent.exe', 'sentinel-agent-windows-amd64.exe', 'sentinel-agent-linux-amd64',
                 'sentinel-agent-windows-386.exe', 'sentinel-watchdog-linux-arm64', 'sentinel-verify-linux-amd64')) {
    Assert-True (Test-IsAgentArtifactName $n) "'$n' is classified as a served artifact"
}
# Must NOT sweep in sidecars, staging copies, backups, or non-agent artifacts.
foreach ($n in @('sentinel-agent-windows-amd64.exe.manifest.json', 'sentinel-agent.manifest.json',
                 'sentinel-agent-windows-amd64.exe.staging-1234',
                 'sentinel-agent-linux-amd64.bak-20260619-162959',
                 'sentinel-desktop-windows-amd64.exe', 'sentinel-desktop-helper-windows-amd64.exe',
                 'sentinel-installer-template.exe', 'sentinel-setup-base.exe',
                 'sentinel-agent_1.77.40_amd64.deb', 'sentinel-agent-1.77.40-1.x86_64.rpm',
                 'sentinel-agent-1.77.40-armv8.spk', 'version.json', 'install.ps1', 'regfix.exe')) {
    Assert-Equal $false (Test-IsAgentArtifactName $n) "'$n' is NOT treated as a served agent artifact"
}

Write-Host "`nGet-ServedArtifactProblems" -ForegroundColor Cyan

$root = Join-Path ([IO.Path]::GetTempPath()) ("relchk-" + [guid]::NewGuid().ToString('N').Substring(0,8))
New-Item -ItemType Directory -Force $root | Out-Null
try {
    $V = '1.77.41'
    # Deterministic fake hashes: content -> "hash of" marker.
    $hasher = { param($p) "sha-" + (Split-Path -Leaf $p) }
    $okVerifier = { param($b, $s) $true }
    $badVerifier = { param($b, $s) $false }

    function New-Artifact {
        param([string]$Name, [string]$Ver, [string]$Sha = $null, [bool]$Downgrade = $false, [switch]$NoSidecar)
        $p = Join-Path $root $Name
        Set-Content $p "binary"
        if (-not $NoSidecar) {
            $sha = if ($Sha) { $Sha } else { "sha-$Name" }
            @{ version = $Ver; platform = 'windows'; arch = 'amd64'; sha256 = $sha; signedDowngrade = $Downgrade } |
                ConvertTo-Json | Set-Content "$p.manifest.json"
        }
        return $p
    }

    # Clean case.
    New-Artifact -Name 'sentinel-agent-windows-amd64.exe' -Ver $V | Out-Null
    $probs = @(Get-ServedArtifactProblems -Roots @($root) -ExpectedVersion $V -HashProvider $hasher -Verifier $okVerifier)
    Assert-Equal 0 $probs.Count 'a correctly signed current artifact produces no problems'

    # An unsigned UNSUFFIXED fallback — the exact case the old gate missed.
    New-Artifact -Name 'sentinel-agent' -Ver $V -NoSidecar | Out-Null
    $probs = @(Get-ServedArtifactProblems -Roots @($root) -ExpectedVersion $V -HashProvider $hasher -Verifier $okVerifier)
    Assert-Equal 1 $probs.Count 'an unsigned unsuffixed fallback binary IS reported'
    Assert-True ($probs[0] -like '*no signed sidecar*') 'missing sidecar is the reported reason'
    Remove-Item (Join-Path $root 'sentinel-agent')

    # Stale artifact from an older release (the windows-386 case).
    New-Artifact -Name 'sentinel-agent-windows-386.exe' -Ver '1.77.10' | Out-Null
    $probs = @(Get-ServedArtifactProblems -Roots @($root) -ExpectedVersion $V -HashProvider $hasher -Verifier $okVerifier)
    Assert-Equal 1 $probs.Count 'a stale-version artifact IS reported'
    Assert-True ($probs[0] -like '*sidecar v1.77.10*') 'stale version is named in the problem'
    Remove-Item (Join-Path $root 'sentinel-agent-windows-386.exe'), (Join-Path $root 'sentinel-agent-windows-386.exe.manifest.json')

    # Sidecar copied from a sibling: right version, wrong bytes. Version-only
    # checking (the round-3 gate) accepted this.
    New-Artifact -Name 'sentinel-bootstrap-windows-amd64.exe' -Ver $V -Sha 'sha-someone-else' | Out-Null
    $probs = @(Get-ServedArtifactProblems -Roots @($root) -ExpectedVersion $V -HashProvider $hasher -Verifier $okVerifier)
    Assert-Equal 1 $probs.Count 'a sidecar whose sha256 does not match the bytes IS reported'
    Assert-True ($probs[0] -like '*sha256 does not match*') 'hash mismatch is the reported reason'
    Remove-Item (Join-Path $root 'sentinel-bootstrap-windows-amd64.exe'), (Join-Path $root 'sentinel-bootstrap-windows-amd64.exe.manifest.json')

    # signedDowngrade must never be accepted on a forward release.
    New-Artifact -Name 'sentinel-verify-windows-amd64.exe' -Ver $V -Downgrade $true | Out-Null
    $probs = @(Get-ServedArtifactProblems -Roots @($root) -ExpectedVersion $V -HashProvider $hasher -Verifier $okVerifier)
    Assert-Equal 1 $probs.Count 'a sidecar authorizing a signed downgrade IS reported'
    Assert-True ($probs[0] -like '*DOWNGRADE*') 'downgrade authorization is the reported reason'
    Remove-Item (Join-Path $root 'sentinel-verify-windows-amd64.exe'), (Join-Path $root 'sentinel-verify-windows-amd64.exe.manifest.json')

    # Signature that does not verify under this release's key (wrong/rotated key).
    $probs = @(Get-ServedArtifactProblems -Roots @($root) -ExpectedVersion $V -HashProvider $hasher -Verifier $badVerifier)
    Assert-Equal 1 $probs.Count 'an artifact whose signature does not verify IS reported'
    Assert-True ($probs[0] -like '*does not verify*') 'signature failure is the reported reason'

    # Unreadable sidecar must be a problem, not a silent skip.
    Set-Content (Join-Path $root 'sentinel-watchdog-windows-amd64.exe') 'binary'
    Set-Content (Join-Path $root 'sentinel-watchdog-windows-amd64.exe.manifest.json') '{ not json'
    $probs = @(Get-ServedArtifactProblems -Roots @($root) -ExpectedVersion $V -HashProvider $hasher -Verifier $okVerifier)
    Assert-Equal 1 @($probs | Where-Object { $_ -like '*unreadable sidecar*' }).Count 'an unreadable sidecar IS reported'

    # A missing root must be skipped quietly, not throw.
    $probs = @(Get-ServedArtifactProblems -Roots @((Join-Path $root 'does-not-exist')) -ExpectedVersion $V -HashProvider $hasher -Verifier $okVerifier)
    Assert-Equal 0 $probs.Count 'a non-existent root is skipped without error'
}
finally { Remove-Item -Recurse -Force $root }

Write-Host "`n$script:pass passed, $script:fail failed" -ForegroundColor $(if ($script:fail) { 'Red' } else { 'Green' })
if ($script:fail) { exit 1 }
