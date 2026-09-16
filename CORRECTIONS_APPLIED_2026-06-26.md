# Sentinel — Corrections Applied (2026-06-26)

Follow-up to `ARCHITECTURE_ASSESSMENT_2026-06-26.md`. This pass dug into each
surfaced issue against industry comparables (Tactical RMM, MeshCentral, NinjaOne,
Datto, ConnectWise, CrowdStrike/SentinelOne for endpoint-agent norms) and applied
the corrections that could be **verified by inspection**. Go changes that need a
compiler are delivered as ready-to-apply patches rather than committed blind,
because the server's Go build is not gated in CI (integration tests are
`continue-on-error`) and this sandbox has no Go 1.25 toolchain — pushing an
unverified Go edit could reach production via auto-deploy.

---

## A. Corrections to the assessment itself (verified against live code)

Digging in revealed that three findings were **overstated or already resolved** —
recording them so the assessment isn't trusted beyond what the code supports:

| Finding | Reality after code-level verification |
|---|---|
| **M3 — "Alertmanager is shadow-mode / can't page"** | **Already resolved.** `configs/alertmanager/alertmanager.yml` routes critical+warning through `alertmanager-ntfy` → ntfy ("Phase 3.5 LIVE"), with inhibit rules. No change needed. |
| **L3 — "TLS falls back to insecure"** | **Largely false for current code.** The server mTLS listener uses `RequireAndVerifyClientCert` (the only safe setting); the agent gRPC data plane *disables itself* when no CA is present rather than going plaintext. The only soft spot is the agent's mTLS→token fallback, which is still WSS-encrypted (weaker auth, not plaintext). Hardening is optional, not a hole. |
| **H3 — "version skew"** | **Real, but partly by design.** `installers/version.json` documents a deliberate convention: it tracks the *published agent binary* version and may diverge from `package.json` (server/repo version). The genuinely-wrong part was the lagging copies (below). |

This is the value of verifying against code: the standard is to report only what
the source supports.

---

## B. Corrections applied (verified)

### H2 — Stale enrollment token removed from tracked docs ✅
**Standard:** no credentials in source control (OWASP ASVS V2.10, CIS 4). Comparable
RMMs keep enrollment secrets in env/secret stores, never in repo docs.
**Change:** `CLAUDE.md` — replaced the literal enrollment token (2 places) with
`$env:SENTINEL_ENROLLMENT_TOKEN` and a rotation note pointing at `.env` /
`enrollment_tokens`. **Action for you:** if that token was ever live, rotate it
(new token → `.env` → restart `sentinel-backend`) and revoke the old one.

### M2 — Integration-test gate root cause fixed ✅
**Root cause found:** `docker-compose.test.yml` set `MTLS_ENABLED=false`, but the
server only reads **`ENABLE_MTLS`** (default `true`); `MTLS_ENABLED` is read
nowhere. So the test backend kept mTLS on, hit `log.Fatalf` (no certs mounted),
and never became healthy — which is exactly why the integration-test job was made
`continue-on-error: true` (PR #47).
**Change:** corrected the env var to `ENABLE_MTLS=false` with an explanatory
comment. **Standard:** CI quality gates must actually block (DORA/Accelerate);
a non-blocking integration suite is a silent regression channel.
**Action for you:** after one green run on the self-hosted runner, remove
`continue-on-error: true` from the `integration-tests` job in `ci.yml` to re-arm
the gate.

### M5 — Stale Postgres init mount removed ✅
**Change:** `docker-compose.yml` — removed
`./src/main/migrations:/docker-entrypoint-initdb.d:ro` (the directory is empty;
schema is owned by the backend's golang-migrate runner under
`server/pkg/database/migrations`). **Standard:** single, app-owned migration path
(12-Factor; matches how every comparable Go service manages schema).

### H3 — Version files reconciled to 1.77.39 + drift guard added ✅
**Standard:** single source of truth + automated consistency gate (the project's
own "files must stay in sync" rule, enforced rather than documented).
**Changes (your approved choice: sync laggards up, no forward bump):**
- `release/agent/version.json` 1.77.10 → **1.77.39** (mirrors `agent/version.json`)
- `installers/version.json` 1.77.10 → **1.77.39** (kept the published-binary convention note)
- `agent/cmd/sentinel-agent/main.go` fallback 1.77.10 → **1.77.39**
- `agent/cmd/sentinel-watchdog/main.go` fallback 1.77.10 → **1.77.39**
- **New:** `scripts/check-version-consistency.sh` — fails if any lockstep file
  drifts from `agent/version.json` (frontend/mobile intentionally excluded; they
  track separate product lines).
- **New CI job** `version-consistency` in `.github/workflows/ci.yml` (fast,
  blocking). Verified locally: guard now reports **PASS** for all five files.

### H1 — Code-signing runbook + paste-ready CI scaffold ✅ (doc)
**Standard:** every comparable RMM/EDR signs its agents and verifies the signature
before update (supply-chain integrity). Sentinel currently does neither
("Code signing is not yet enabled — Phase 2 follows").
**Deliverable:** `docs/CODE_SIGNING.md` — platform-by-platform plan (Windows
Authenticode via Azure Trusted Signing, macOS Developer ID + notarization, Linux
GPG), **secret-gated CI snippets** that no-op until certs are provisioned, and the
**watchdog verify-before-swap** pattern with a staged rollout flag so enforcement
never bricks the existing fleet. Certificate procurement is the one thing only you
can do — the runbook lists exactly what to buy and the secrets to add.

---

## C. Ready-to-apply (NOT committed — needs `go build ./...` first)

These are correct-by-pattern but I won't push unverified Go into an auto-deploying
backend. Apply locally, build, then commit.

### M4 — Audit logging for the most sensitive actions
**Gap:** `ActionCommandExecuted`, `ActionScriptExecuted`, and `ActionLoginFailed`
constants exist but are **never called** — remote command/script execution (an
RMM's highest-impact action) and auth events are unaudited, while device
enable/disable already are. **Standard:** SOC 2 CC7 / NIST 800-53 AU-2 require
audit trails for privileged actions and authentication.

The audit logger is already on the `Router` (`r.audit *audit.Logger`) and used
elsewhere, so these follow the proven pattern:

**1. `server/internal/api/devices.go` — in `executeCommand`, after the command is
dispatched (just before the success `c.JSON`):**
```go
if r.audit != nil {
    r.audit.LogFromContextWithSeverity(c, audit.ActionCommandExecuted,
        audit.ResourceTypeCommand, &commandID, audit.SeverityWarning,
        map[string]interface{}{
            "deviceId":    id.String(),
            "agentId":     agentID,
            "commandType": req.CommandType,
        })
}
```

**2. `server/internal/api/handlers.go` — in `executeScript`, after `SendToAgent`
succeeds:** (ensure `"github.com/sentinel/server/internal/audit"` is imported)
```go
if r.audit != nil {
    r.audit.LogFromContextWithSeverity(c, audit.ActionScriptExecuted,
        audit.ResourceTypeScript, &scriptID, audit.SeverityWarning,
        map[string]interface{}{
            "deviceId": deviceID.String(),
            "language": script.Language,
        })
}
```

**3. `server/internal/api/auth.go` — in `login`, add failure + success events:**
```go
// on each Unauthorized return (user not found / disabled / bad password):
if r.audit != nil {
    r.audit.LogSecurityEvent(c, audit.ActionLoginFailed, false,
        map[string]interface{}{"identifier": req.Identifier})
}
// on success, before writing the 200 response:
if r.audit != nil {
    r.audit.LogSecurityEvent(c, audit.ActionLoginSuccess, true,
        map[string]interface{}{"userId": user.ID.String()})
}
```

### L3 — Fail-fast production assertion (optional hardening)
Transport is already fail-closed, but a startup guard prevents a silent
misconfiguration (mTLS accidentally disabled in prod). In
`server/pkg/config/config.go` validation, when `Environment == "production"`:
refuse to start (or log CRITICAL) if `!EnableMTLS` or any of
`TLSCertPath/TLSKeyPath/CACertPath` is empty. Mirrors the existing
production check that already requires `AllowedOrigins`.

---

## D. One follow-up that needs your knowledge (release state)

`agent/version.json` advertises **1.77.39** as latest, but `installers/version.json`
(published-binary tracker) was stuck at **1.77.10**, and a `v1.77.40` tag exists
while the changelog mentions prod **1.78.0**. The actual update trigger is gated by
rows in the `agent_releases` table. **Please confirm a 1.77.39 agent-binary release
row exists before announcing** — if the backend advertises a version with no
published binary / no `agent_releases` row, agents can get told to update to
something that isn't there (this matches the agent-comms incidents in the project
history). `scripts/publish-1.77.10-agent_releases.sql` is the precedent for
inserting that row.

---

## E. Repo-state observation (please verify on your machine)

Running `git status` from the Linux mount showed ~400 files as modified with a
pure CRLF↔LF line-ending delta (`git diff --ignore-all-space` is empty; only
`.gitattributes` content differs). This is almost certainly a **mount artifact**
of viewing a Windows (CRLF) checkout through Linux — not real content change.
Before committing the corrections above, check `git status` on your Windows
machine and stage **only** the intended files, e.g.:

```
git add CLAUDE.md docker-compose.yml docker-compose.test.yml \
        release/agent/version.json installers/version.json \
        agent/cmd/sentinel-agent/main.go agent/cmd/sentinel-watchdog/main.go \
        scripts/check-version-consistency.sh .github/workflows/ci.yml \
        docs/CODE_SIGNING.md
```

so you don't sweep a repo-wide line-ending renormalization into the same commit.

---

## Summary

| Issue | Status | Type |
|---|---|---|
| H2 stale token in docs | **Fixed** | doc |
| M2 integration-test gate root cause | **Fixed** (env var) | config |
| M5 stale migration mount | **Fixed** | config |
| H3 version skew | **Fixed** + drift guard + CI gate | config + new |
| H1 code signing | **Runbook + CI scaffold delivered** | doc (cert procurement = you) |
| M3 alertmanager | **No action — already live** | corrected finding |
| L3 TLS fallback | **Largely a non-issue**; optional fail-fast patch provided | corrected + patch |
| M4 audit coverage | **Ready-to-apply patch** (needs `go build`) | Go (not committed) |
| Release-state (agent_releases row) | **Needs your confirmation** | follow-up |
| Repo-wide CRLF diff | **Flagged** (verify on Windows) | observation |
