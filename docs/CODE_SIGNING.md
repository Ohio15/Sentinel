# Code Signing & Update Integrity (Finding H1 / audit "C-03")

**Status:** Not yet implemented. `build-installers.yml` ships unsigned binaries
("Code signing is not yet enabled — Phase 2 follows"). This is the last open
*critical* from the security audit and the single highest-leverage hardening
left for Sentinel.

## Why this matters for an RMM specifically

Sentinel agents run as **SYSTEM/root** and **self-update from your servers**. That
makes the update channel a supply-chain path into every managed endpoint. Two
independent risks:

1. **Distribution trust.** Unsigned binaries trigger SmartScreen (Windows) and
   Gatekeeper (macOS) friction, and give endpoint EDR/AV no publisher identity to
   allowlist — so customers either can't install cleanly or have to weaken their
   own security to do so.
2. **Update integrity.** SHA256SUMS proves a download wasn't corrupted; it does
   **not** prove it came from you. If the update host or release assets are ever
   tampered with, the watchdog will happily promote a malicious binary. A
   signature the watchdog *verifies before swap* closes that hole.

### How the industry comps handle it (benchmark)

| Product | Windows | macOS | Update-time verification |
|---|---|---|---|
| NinjaOne / Datto / ConnectWise (commercial RMM) | EV / OV Authenticode-signed agent | Developer ID + notarized | Signed installers; managed update servers |
| CrowdStrike Falcon, SentinelOne (EDR) | Signed (often EV) | Signed + notarized + hardened runtime | Signature checked before load |
| Tactical RMM (open source) | Signs the agent (community/MSP cert); meshagent is signed | — | Hash + signed releases |
| **Sentinel (today)** | **unsigned** | **unsigned** | SHA256 only, no signature check |

The standard every comparable product meets: **sign every shipped binary, and
verify the signature on the endpoint before executing/promoting an update.**
Sentinel currently does neither.

---

## Target end-state

1. **Windows** (`sentinel-agent.exe`, `sentinel-watchdog.exe`, desktop helpers,
   installer): Authenticode-signed + timestamped (RFC-3161).
2. **macOS** (`.pkg`, universal2 binaries): `codesign` with Developer ID
   Application + hardened runtime, then `notarytool` submit + `stapler staple`.
3. **Linux** (`.deb` / `.rpm`): GPG-signed packages + repo metadata; publish the
   public key.
4. **Watchdog verifies before swap**: the updater refuses to promote a downloaded
   binary whose embedded signature doesn't chain to the expected Sentinel/EV
   publisher — gated by a config flag during rollout (below) so we never brick
   the existing fleet.

---

## Certificate options (pick per-platform)

**Windows** — choose one:
- **Azure Trusted Signing** (recommended): ~$10/mo, no hardware token, cloud HSM,
  immediate SmartScreen reputation under Microsoft's CA. Easiest CI integration.
- **EV code-signing cert** on FIPS token/HSM (DigiCert/Sectigo): instant
  SmartScreen reputation, but token-in-CI is awkward (needs a cloud HSM or a
  self-hosted signer). Use only if Trusted Signing isn't an option.
- **OV cert**: cheapest, but SmartScreen reputation must be earned over time.

**macOS**: Apple Developer Program ($99/yr) → *Developer ID Application* +
*Developer ID Installer* certs. Required for notarization.

**Linux**: a dedicated GPG key pair; publish the public key at a stable URL and
in the docs so customers can verify.

---

## CI integration (paste-ready, no-op until secrets exist)

Add these **after** the platform packaging steps in
`.github/workflows/build-installers.yml`. Each is gated on a secret being
present, so the pipeline keeps working unchanged until certs are provisioned.

> Implementation note: GitHub Actions can't reference `secrets.*` directly in a
> step-level `if:`. Promote the secret to an env var at job level, then gate on
> the env var, as shown.

```yaml
  # ----- Windows signing (Azure Trusted Signing) -----
  # job: package-windows
  env:
    SIGN_AZURE_TENANT: ${{ secrets.AZURE_TENANT_ID }}
  steps:
    - name: Sign Windows binaries (Authenticode)
      if: ${{ env.SIGN_AZURE_TENANT != '' }}
      uses: azure/trusted-signing-action@v0
      with:
        azure-tenant-id: ${{ secrets.AZURE_TENANT_ID }}
        azure-client-id: ${{ secrets.AZURE_CLIENT_ID }}
        azure-client-secret: ${{ secrets.AZURE_CLIENT_SECRET }}
        endpoint: ${{ secrets.AZURE_SIGN_ENDPOINT }}
        trusted-signing-account-name: ${{ secrets.AZURE_SIGN_ACCOUNT }}
        certificate-profile-name: ${{ secrets.AZURE_SIGN_PROFILE }}
        files-folder: installers
        files-folder-filter: exe
        timestamp-rfc3161: http://timestamp.acs.microsoft.com
        timestamp-digest: SHA256
```

```yaml
  # ----- macOS sign + notarize -----
  # job: package-macos
  env:
    SIGN_MAC_ID: ${{ secrets.APPLE_DEVELOPER_ID }}
  steps:
    - name: Import signing certificate
      if: ${{ env.SIGN_MAC_ID != '' }}
      run: |
        echo "${{ secrets.APPLE_CERT_P12_BASE64 }}" | base64 -d > cert.p12
        security create-keychain -p "${{ secrets.KEYCHAIN_PW }}" build.keychain
        security import cert.p12 -k build.keychain -P "${{ secrets.APPLE_CERT_PW }}" -T /usr/bin/codesign
        security set-key-partition-list -S apple-tool:,apple: -s -k "${{ secrets.KEYCHAIN_PW }}" build.keychain
    - name: Sign, notarize, staple
      if: ${{ env.SIGN_MAC_ID != '' }}
      run: |
        codesign --force --options runtime --timestamp \
          --sign "Developer ID Application: ${{ secrets.APPLE_TEAM }}" installers/macos/output/*.pkg
        xcrun notarytool submit installers/macos/output/*.pkg \
          --apple-id "${{ secrets.APPLE_ID }}" --team-id "${{ secrets.APPLE_TEAM_ID }}" \
          --password "${{ secrets.APPLE_APP_PW }}" --wait
        xcrun stapler staple installers/macos/output/*.pkg
```

```yaml
  # ----- Linux GPG signing -----
  # job: package-linux-deb / package-linux-rpm
  env:
    SIGN_GPG: ${{ secrets.LINUX_GPG_PRIVATE_KEY }}
  steps:
    - name: GPG sign packages
      if: ${{ env.SIGN_GPG != '' }}
      run: |
        echo "${{ secrets.LINUX_GPG_PRIVATE_KEY }}" | gpg --batch --import
        dpkg-sig --sign builder installers/linux/output/*.deb || true
        rpm --addsign installers/linux/output/*.rpm || true
        gpg --armor --detach-sign --output SHA256SUMS.asc SHA256SUMS
```

**Secrets to add** (Repo → Settings → Secrets → Actions): the Azure Trusted
Signing set, the Apple set (`APPLE_*`, `KEYCHAIN_PW`), and `LINUX_GPG_PRIVATE_KEY`.
Until they exist, every signing step is skipped and the build is unchanged.

---

## Watchdog: verify-before-swap (the part that actually protects endpoints)

Signing the build only helps if the endpoint checks it. Add a verification gate
in the updater immediately before the atomic `.new → exe` promotion. On Windows,
verify the Authenticode chain (via `WinVerifyTrust` / PowerShell
`Get-AuthenticodeSignature`) and assert the signer subject matches the expected
Sentinel publisher.

**Rollout sequencing (do not brick the fleet):**
1. Ship a watchdog that *can* verify signatures but treats enforcement as
   **opt-in**, controlled by `SENTINEL_REQUIRE_SIGNED_UPDATES` (default `false`).
   It logs the verification result either way.
2. Start signing all new releases. Existing agents update normally.
3. Once telemetry shows the whole fleet is on a signing-aware watchdog **and**
   recent releases are signed, flip the default to `true` (or set it via config).
   From then on, an unsigned/mis-signed binary is rejected before promotion.

This staged approach mirrors how the comps introduce enforcement: capability
first, enforce once coverage is proven.

---

## Acceptance checklist

- [ ] Cert(s) provisioned; secrets added to Actions.
- [ ] `build-installers.yml` signs Windows + macOS + Linux artifacts.
- [ ] `signtool verify /pa`, `codesign --verify --deep --strict`, and
      `spctl -a -t install` pass on the published artifacts.
- [ ] Watchdog verifies signature before swap (opt-in flag shipped).
- [ ] Public GPG key + verification instructions documented for Linux customers.
- [ ] After fleet coverage confirmed, signed-update enforcement enabled.
- [ ] Remove the "Code signing is not yet enabled" note from `build-installers.yml`.
