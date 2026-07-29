# Sentinel Security Remediation Plan — 2026-07-29

Scope: resolutions for the app/agent/watchdog audit of 2026-07-29 (4 parallel
agents; shared-brain incident `f789aa76`). Renderer app was clean and is not
in scope. This plan covers the agent, the watchdog, and the agent-facing
server endpoints.

Finding-ID legend: `WD-*` watchdog audit, `AG-*` agent audit (both agent
auditors merged), `SEC-*` server audit.

---

## 1. Survey — what the evidence actually shows

Sixty-plus findings collapse into **two critical clusters and four
supporting workstreams**:

- **Cluster A — Update chain has no authenticity control.** No
  Authenticode/publisher signature anywhere before a binary is swapped and
  run as SYSTEM. Checksum-only, and the checksum is self-referential
  (same response as the URL; self-computed from the artifact when omitted).
  Bypassable on ≥3 paths and reintroduces the 2026-04-27 "download+run" shape
  in bootstrap recovery. No anti-rollback. → server compromise or tunnel MITM
  = SYSTEM RCE fleet-wide. Findings: AG-C1, AG-C2, AG-H1, AG-H4, AG-H5,
  WD-H2, WD-H3, WD-H4.
- **Cluster B — Local secrets reduce to obfuscation on Windows.** Go file
  modes set no DACL; mTLS `client.key`, `config.json`, and the IPC key land
  under ProgramData *Users:Read*. Config "encryption" key = SHA256(MachineGuid
  + hostname), both world-readable. → any local user recovers the enrollment
  token and client key and impersonates the agent. Findings: AG-CRIT1, AG-H2,
  AG-H3, WD-M9.
- **Workstream C — Watchdog supervision is incorrect.** Its two guarantees
  both fail: restarts the agent mid-swap (WD-C1) and its crash-loop breaker is
  unreachable (WD-C2), plus resilience defects (WD-H5 ticker panic, WD-H6
  mutex deadlock, WD-H1 ACL-restore, WD-H7 self-update race, WD-M1/M2 PID
  reuse). This is an availability incident waiting to fire and is independent
  of Clusters A/B.
- **Workstream D — Executor/file input hardening.** Script exec has no
  whitelist (AG-H script), newline whitelist bypass (AG-H newline), arbitrary
  SYSTEM write with all-drives base + symlink TOCTOU (AG-H write), weak
  secondary path validator, desktop-helper IPC token predictable.
- **Workstream E — Server.** SEC-001 no rate-limit → bcrypt-scan DoS;
  SEC-002 `use_count` never incremented on direct enroll → one-time tokens
  replay; SEC-003 IDOR on token update/regenerate; SEC-005 unmasked token.
- **Workstream F — Hygiene.** Delete dead RCE-shaped force-update code
  (SEC-007, confirmed unreachable but must not be left for a refactor to
  re-enable), `.bak` auth files (SEC-006), finish the enrollment-token
  cutover, and the 70 Dependabot vulns.

## 2. Root cause

Two systemic roots, not sixty point bugs:

1. **Trust was placed in transport, not in artifacts.** The design assumed
   "the server/tunnel is authenticated, therefore what it sends is
   trustworthy." Every Cluster-A finding is a corollary. The fix is to make
   the *artifact* self-authenticating (signed), independent of the channel.
2. **POSIX file-permission assumptions on a Windows-primary product.** Every
   Cluster-B finding and WD-M9 stem from `0600`/`0755` being a no-op without
   an explicit DACL, and from treating machine identifiers as secrets. The
   fix is OS-correct secret sealing (DPAPI/CNG) and DACLs at create time.

## 3. Options (with concerns)

**Signing scheme for Cluster A** — the pivotal decision:

- **Option 1 — Ed25519 detached signature, public key embedded in the
  binary (RECOMMENDED).** Release pipeline signs every artifact with a
  private key held on the build host; every update path verifies the detached
  signature against the baked-in public key immediately before swap.
  *Concern:* key custody/rotation is on us (mitigate: key in the NEXUS build
  host's protected store, rotation = shipped new pubkey in a signed update).
  *Why correct:* no external dependency, deployable now, verifies the actual
  bytes regardless of channel, works identically on Windows/Linux/macOS.
- **Option 2 — Authenticode only.** *Concern:* blocked on code-signing
  certificate procurement (the still-open C-03 item — lead time + cost), and
  Authenticode covers Windows PE only, not the Linux/darwin agents or the
  detached JSON update requests. Good defense-in-depth, wrong as the sole or
  first control.
- **Option 3 — Do both, Ed25519 first.** Ed25519 now as the enforced gate;
  add Authenticode when the cert lands, as a second, OS-native layer.

Recommendation: **Option 3**, sequenced Ed25519-first. Do not let cert
procurement block the critical.

**Secret sealing for Cluster B:**

- **Option A — DPAPI/CNG machine scope (RECOMMENDED)** for the mTLS key and
  config, plus `secureWriteFile` (SYSTEM+Admins DACL) for all identity files.
  OS-native, no key-management burden.
- **Option B — keep AES-GCM, fix only the key derivation** (mix a
  SYSTEM-only random secret into DeriveKey). Smaller diff, but still a
  hand-rolled scheme and the key file itself needs the same DACL work; strictly
  worse than A for the same effort.

## 4. Recommendation

Three implementation waves. Wave A ships now (independent, availability- and
DoS-facing, no external deps). Wave B is the signature anchor (the top
critical). Wave C seals secrets and hardens the executor, in parallel once B's
design is fixed.

Sequencing rationale (correct-over-easy): Cluster A is the highest severity,
but it needs a signing-scheme decision and a bootstrap-of-trust rollout, so
running the cheap, self-contained Wave A in parallel is deliberate
parallelism, not deferral of the critical. Cluster A's real-world trigger is
server-compromise / tunnel-MITM (bounded today by CF tunnel + mTLS), which is
what makes a short parallel window acceptable rather than a hard freeze.

## 5. Plan

### Wave A — ship now (P0, no external deps, ~2–3 days)

- **RW-3 Watchdog correctness** (`agent/cmd/sentinel-watchdog/main.go`):
  WD-C1 gate `checkAndRestartAgent` on `updateInProgress`/`selfUpdateInProgress`
  + post-update grace; WD-C2 count restart *attempts*, reset only after
  health-verified, add exponential backoff; WD-H5 clamp `CheckInterval` to
  [1,3600] and `MaxRestarts`≥1, handle the unmarshal error, `defer recover()`
  on long-lived goroutines; WD-H6 `exec.CommandContext` timeout + release
  `ws.mu` before external processes; WD-H1 `defer` protection re-enable on all
  exit paths; WD-M1/M2 verify PID image path before OpenProcess/taskkill
  (kill by PID, per the CLAUDE.md rule); WD-M6 require N consecutive
  non-running samples before rollback.
- **RW-5 Server hardening** (`server/internal/api/router.go`,
  `middleware/auth.go`, `api/agents.go`): SEC-001 add `rateLimitMiddleware`
  to `/api/agent` enroll + update groups and `/api/agents`; SEC-002 move
  `use_count` increment into `ValidateDatabaseToken` with
  `UPDATE ... WHERE use_count < max_uses RETURNING id` (closes TOCTOU);
  SEC-003 add `organization_id` scope to update/regenerate; SEC-005 mask token
  in `getEnrollmentToken`.
- **RW-6 Hygiene**: delete dead `sendForceUpdateCommand`/`sendWindowsForceUpdate`
  (SEC-007) and the `.bak`/`.backup` auth files (SEC-006); finish the
  enrollment-token `.env` cutover on NEXUS + deactivate old row after the 48h
  watch; open a tracked Dependabot-bump sub-effort.

### Wave B — signature anchor (P1, ~1–2 weeks, the top critical)

- **RW-1 Signed update chain** (Ed25519 embedded-pubkey):
  - Release pipeline (`scripts/release.ps1` + CI): generate a detached
    signature per artifact; publish signature alongside binary; store the
    private key in the NEXUS build host protected store.
  - Verify pre-swap on **all three** paths — agent updater
    (`internal/updater/updater.go`), watchdog self-update
    (`internal/selfupdate/selfupdate.go`), and bootstrap recovery
    (`updater.go` Layer-4). Reject empty/self-computed checksums (AG-C2,
    WD-H2, AG-H1). Constrain `DownloadURL` to the configured origin + require
    https (WD-H3).
  - Anti-rollback (AG-H4): strict version parse, refuse target ≤ current
    unless an explicitly signed downgrade flag is present.
  - Replace the broken batch escaping / templating with argv-based invocation
    and path-identity checks (WD-H4, AG-H5).
  - **Bootstrap-of-trust note:** the verifying build must be delivered over
    the current unsigned channel once; every subsequent update is enforced.
    Plan this as a single "trust-establishing" release.

### Wave C — secret sealing + executor hardening (P2, parallel with/after B, ~1–2 weeks)

- **RW-2 Secret sealing**: DPAPI/CNG machine scope for mTLS key
  (`internal/mtls/mtls.go`, `recert.go`) and config
  (`internal/crypto/config_crypto*.go`); route all identity files through
  `ipc.secureWriteFile` and apply a protected DACL to the certs/data dir
  (`internal/paths/paths.go`); validate IPC key file owner/DACL on read and
  regenerate if wrong (WD-M9, AG L3).
- **RW-4 Executor/input hardening**: real argv allow-list (or drop the shell
  for command mode) and split on newlines/CR before base-command extraction
  (AG-H script, AG-H newline); constrain file-transfer allowed bases and route
  through `WriteFileWithLimits`, deny writes into install/system dirs, open
  with no-follow / verify handle identity (AG-H write, AG-M path); crypto/rand
  desktop-helper token + session-scoped pipe DACL (AG-H desktop); fail-closed
  mTLS→token downgrade (AG silent-downgrade); validate CA-cert content before
  replacing the trust anchor (AG CA-overwrite).

## 6. Unknowns

- **Signing key custody/rotation** — where the Ed25519 private key lives and
  how it rotates (proposal: NEXUS build host protected store; rotation via a
  signed pubkey update). Needs Ron's sign-off.
- **Authenticode cert** — is procurement moving (C-03)? Determines when the
  second signing layer lands.
- **DPAPI machine-scope survivability** across the OS reprovision/restore SOP
  — verify sealed secrets survive or are re-provisioned on the documented PC
  reprovision flow.
- **Release-freeze policy** — confirm the intent: no *trusted* release until
  Wave B, but Wave A/B still ship over the existing channel (the channel is
  the thing being fixed).
- **Fleet blast radius of the executor argv allow-list** — need the real
  inventory of commands operators run so the allow-list doesn't break
  legitimate workflows.

## 7. Go / No-Go

- **Go now** on Wave A (self-contained, reversible, high availability/DoS
  value) and on the **Ed25519-first** signing decision.
- **No-Go / needs Ron** on: signing-key custody model, the release-freeze
  policy wording, and any history-scrub / force-push (circuit-breaker, per
  `CREDENTIAL_ROTATION_PLAN.md`).
- Nothing here is applied yet — this is scope + plan only.
