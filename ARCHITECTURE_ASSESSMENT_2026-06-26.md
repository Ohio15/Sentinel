# Sentinel RMM — Architecture Assessment

**Date:** 2026-06-26
**Repo state assessed:** `Ohio15/Sentinel` @ `v1.77.40-4-g4c78f0d` (production reported at v1.78.0)
**Method:** Live read of the working tree at `D:\Projects\Sentinel` across backend, agent, frontends, infra, and data layer, cross-checked against in-repo audit docs and project memory.

---

## 1. Executive Summary

Sentinel is a self-hosted, full-stack **Remote Monitoring & Management (RMM)** platform — comparable in ambition to Tactical RMM, NinjaOne, or Datto. It manages a fleet of endpoint agents that run with SYSTEM/root privilege, collects metrics and inventory, executes remote commands and scripts, provides remote desktop, and self-updates the fleet through a staged rollout pipeline. It is a genuinely large, coherent system: a Go backend, a Go agent + watchdog pair, an Electron desktop console, a web console, and a React Native mobile app, all deployed via Docker Compose behind Traefik with a Prometheus/Grafana observability stack.

**Overall maturity: solid and well-architected for a single-maintainer project, with security hygiene that is much better than the project's own older audit docs suggest.** The codebase shows real engineering discipline — defense-in-depth command/path validation, mTLS agent transport, credential-rotation machinery, distributed WebSocket hub, graduated agent self-healing, and a hardened CI/CD with secret scanning and size guards.

The biggest *current* gaps are not in the code but around it: **binary code signing is still unimplemented** (the one acknowledged open critical), there is meaningful **version/release skew** between the repo and production, an accumulation of **root-level debris** (one-off fix scripts and patches), and **two parallel frontends** whose roles overlap.

> **Important correction to prior memory/audit notes:** Older review docs in this repo (and the shared-brain memory) state that `.env` with live production secrets is committed to git. **That is no longer true.** As of this commit, `.env` is untracked, listed in `.gitignore`, and has **zero** commits across all of git history. The credential-rotation/history-scrub work that was previously outstanding appears to have been completed. Pre-commit secret-scanning and gitleaks CI are now in place.

**Headline scorecard (assessor's read):**

| Dimension | Rating | Note |
|---|---|---|
| Architecture & separation of concerns | Strong | Clean Go package layout, monolith-with-modules, gRPC data plane split from command plane |
| Security posture | Good | Strong input/transport controls; main gap is code signing + a stale dev token in docs |
| Agent reliability/self-healing | Strong | Watchdog, 4-layer update path, silent-agent detector, auto-rollback |
| Data layer | Strong | Postgres-only, 58 sequential migrations, credential & audit tables |
| CI/CD & release | Good (complex) | Multi-platform installers, gated releases; but skew and dual release paths add risk |
| Observability | Good | Prometheus + Grafana + blackbox synthetic probes; Alertmanager still UI-only |
| Repo hygiene | Fair | Size guard + gitignore good; root littered with fix-*.js / *.patch / screenshots |
| Documentation | Mixed | Rich but partly stale; multiple overlapping review docs of differing vintage |

---

## 2. System Overview

```
                          ┌────────────────────────── Endpoints (Win / Linux / macOS / Synology) ─────────────────────────┐
                          │   sentinel-agent (Go, SYSTEM)  ⇄  sentinel-watchdog (Go, Windows)                              │
                          │     • metrics / inventory / logs   • remote shell + scripts   • WebRTC remote desktop          │
                          └───────────────┬───────────────────────────────────────────────────────────────────────────────┘
                                          │  WSS + mTLS (:8443)   |   gRPC data plane (:4444 / plaintext :4445 via CF tunnel)
                                          ▼
   Internet ──► infra-traefik (edge, :80/:443) ──► sentinel-frontend (Nginx)         sentinel-agent-gateway (Traefik)
                      │                                   │                                   │ terminates agent mTLS / gRPC
                      └──────────────► sentinel-backend (Go / Gin) :8080 ◄────────────────────┘
                                          │   REST API • WebSocket hub • alerting • rollouts • PKI • push
                       ┌──────────────────┼───────────────────┐
                       ▼                  ▼                   ▼
                 Postgres 16         Redis 7            Prometheus / Grafana
              (devices, metrics,  (pub/sub, streams,   / node-exporter / cAdvisor
               commands, creds,    rate limits,         / blackbox synthetic probes
               audit, rollouts)    hub state)
```

**Consoles:** Electron desktop app (`src/`) and a separate web console (`frontend/`) consume the same REST + WebSocket API; a React Native / Expo app (`mobile/`) provides a read-and-respond mobile view with push notifications.

---

## 3. Technology Stack (as built)

| Layer | Technology | Notes |
|---|---|---|
| Backend API | **Go + Gin** | Monolith with internal modules; entry `server/cmd/sentinel/main.go` (~617 LOC boot) |
| Data plane | **gRPC (proto3)** | `dataplane.proto` — metrics/inventory/logs/file chunks, split from the command plane |
| Agent / Watchdog | **Go** | `agent/cmd/sentinel-agent`, `agent/cmd/sentinel-watchdog`; ~35 internal packages |
| Database | **PostgreSQL 16** (pgx/v5 pool) | golang-migrate, 58 migrations, SCRAM-SHA-256, SSL in prod |
| Cache / bus | **Redis 7** | Pub/sub + Streams (command queue), rate-limit buckets, hub state, AOF persistence |
| Desktop console | **Electron + React 18 + Vite + TS** | Zustand, React Query, Tailwind, xterm.js, Recharts, WebRTC |
| Web console | **React 18 + Vite** (`frontend/`) | Served by Nginx (`Dockerfile.web`) |
| Mobile | **Expo 52 / React Native 0.76** | Expo Router, RN Paper (MD3), secure-store, push |
| Edge routing | **Traefik v3** | `infra-traefik` for web; `sentinel-agent-gateway` for agent mTLS/gRPC |
| Observability | **Prometheus + Grafana + Alertmanager** | node-exporter, cAdvisor, blackbox synthetic probes |
| Orchestration | **Docker Compose** | ~11 services; self-hosted deploy on the "NEXUS" host |
| CI/CD | **GitHub Actions** | 7 workflows + a shared `dev-standards` release contract |

---

## 4. Component Deep-Dive

### 4.1 Backend (`server/`)

A modular Go monolith on Gin, booting HTTP (`:8080`), a dedicated agent **mTLS** listener (`:8443`), and **gRPC** data plane (`:50051`, plus plaintext `:4445`/`:4444` for Cloudflare-tunnel agents). The boot sequence is disciplined: config validation → structured logging → DB migrate → first-run admin seeding (random bcrypt-cost-12 password) → Redis → WebSocket hub (local or Redis-distributed) → DI service container → routers → background tasks (log cleanup, metrics retention, **5-minute agent-health recovery scan**) → graceful 30s shutdown.

**Package layout is clean and responsibility-driven:** `api/` (~33k LOC, 62 files) holds handlers and the two routers (admin router + dedicated agent-mTLS router); `middleware/` (auth, CSRF, multi-tier rate limiting); `websocket/` (local `hub.go` + `distributed_hub.go` over Redis); `credentials/` (API-key manager, JWT rotation manager, AES-256-GCM encryptor); `grpc/`, `queue/`, `metrics/`, `alerting/`, `notifier/`, `pki/`, `push/`, `turn/`, `messaging/`, `audit/`.

**API surface** is ~100+ routes spanning auth/MFA/WebAuthn, devices, commands, scripts, alerts + alert rules, rollouts, webhooks, users/invitations/enrollment-tokens, and credential management. Authentication is layered: **JWT** (HMAC-SHA256, algorithm-confusion protected, optional dual-key rotation), **managed API keys** (`sk_live_*`, bcrypt-hashed, per-key permissions/IP-allowlist/expiry in `credential_keys`), a **static fallback API key** (operator role), **agent enrollment tokens** (constant-time compared), and **mTLS** client certs post-enrollment. RBAC is `admin / operator / viewer`, enforced at the route via `RequireRole(...)`.

**WebSocket / agent protocol** is the heart of the system: 50+ message types (heartbeat/metrics/commands/terminal/file-transfer/RDP/WebRTC/USB/inventory/mobile). Heartbeat-ack carries an `updateAvailable` flag gated by a release-readiness check (prevents update retry-storms). The **distributed hub** tracks agent location in Redis with per-agent channels and TTL expiry, enabling multi-server scale-out. Backpressure is handled per-client (1024-msg buffers, drop counters, atomic metrics).

**Notable subsystems:** binary patching at download time (`bootstrap.go` injects server URL + enrollment token into fixed-width placeholders); the **rollouts MVP** (`/api/rollouts`, immediate mode, all-online or device-list targets, 5s dispatch ticker, optional failure threshold); and the **silent-agent detector** — a graduated self-heal that escalates from "push a repair command over WS" → "mint a fresh one-time enrollment token" → "flag for manual onsite review." This directly targets the worst RMM failure mode (an agent going dark and needing a physical visit).

**Code health:** error handling uses a consistent `sanitizedError()` pattern (log internally, return generic externally); parameterized SQL throughout; panic-recover on long-running goroutines; only ~5 TODO/FIXME; 22 Go test files concentrated on the critical paths (auth, CSRF, rate limit, hub, rollouts, cert renewal). One historical dead path (`/ws/agent/mtls` behind Traefik passthrough) was previously flagged — worth a confirmation grep that it's gone, but no longer the active concern it once was.

### 4.2 Agent & Watchdog (`agent/`)

A cross-platform Go agent (Windows/Linux/macOS/Synology) paired with a Windows watchdog. The agent handles enrollment, mTLS connection (`wss://…:8443/ws/agent`), a ~10s heartbeat, ~1s metrics (via gRPC data plane with WebSocket fallback), command/script execution, file transfer, and WebRTC remote desktop via helper processes. ~35 internal packages include `client` (auto-reconnect with exponential backoff + jitter), `config` (embedded-placeholder patching), `mtls`, `updater`, `protection` (file/process tamper protection via NTFS ACLs / chmod), `terminal`, `filetransfer`, `executor`, `collector`, `desktop`, `inventory`, `peripheral`, and `recert`.

The **watchdog** is a strong reliability story: SCM monitoring with cooldown-bounded restarts, IPC-heartbeat freshness checks, independent HTTP update polling, **update ordering** (watchdog updates itself before the agent so an old watchdog can't block a new agent), atomic `.old/.new` binary swaps with SHA-256 verification, and **auto-rollback on post-update health failure**. This is the kind of resilience engineering that separates a toy RMM from a deployable one.

### 4.3 Frontends (`src/`, `frontend/`, `mobile/`)

There are **two web/desktop frontends**, which is the main structural ambiguity:

- **`src/`** — the Electron desktop console (React 18 + Vite + TS, Zustand, React Query, Tailwind, xterm.js, Recharts, custom WebRTC remote-desktop stack). ~23 pages, 30+ components, electron-updater auto-update via GitHub Releases. This is the feature-rich primary console (devices, tickets with Kanban/calendar/analytics, scripts, certificates, network, remote desktop, recordings).
- **`frontend/`** — a separate web build (pinned at an older version, ~1.76.14) shipped as the Nginx container. Roles overlap with `src/`'s renderer.

This duplication is a maintenance liability (two React apps, two version lines, two build paths) and the most obvious candidate for consolidation — ideally one shared component/render layer with thin Electron and web shells.

- **`mobile/`** — Expo 52 / RN 0.76 app (Expo Router, RN Paper MD3, secure-store, push). Dashboard/devices/alerts/tickets tabs + detail screens. Functional and current, but lighter (no visible test files; Jest configured).

### 4.4 Data Layer (`server/pkg/database/migrations/`)

PostgreSQL-only (no SQLite), accessed via pgxpool with parameterized SQL and transactions. **58 sequential golang-migrate migrations** (`000001`…`000058`), the latest being `agent_health_recovery`. The schema is mature: `users`, `devices`, partitioned `device_metrics` (time-series, 90-day retention via cleanup job), `commands`, `scripts`, `alerts`/`alert_rules`, `sessions`, `enrollment_tokens`, `agent_releases`, `agent_health`, `rollouts`/`rollout_devices`, `webauthn_credentials`, `audit_log`, and a notably complete credential-management trio (`credential_keys`, `credential_rotation_log`, `credential_rotation_schedule`) supporting zero-downtime rotation with grace periods. **Caveat:** `docker-compose.yml` mounts `./src/main/migrations` into Postgres init, but the authoritative migrations live under `server/pkg/database/migrations/` (applied by the backend on boot) — the compose mount path is stale/empty and should be cleaned up to avoid confusion.

### 4.5 Deployment, CI/CD & Observability

~11 Docker Compose services on a self-hosted host, fronted by Traefik on a shared `edge` network, with hardening throughout (read-only root FS, `no-new-privileges`, non-root UIDs, tmpfs scratch). A `systemd` oneshot (`ensure-edge-network.service`) deterministically re-attaches containers to `edge` on boot to defeat a Docker network race that otherwise 502s all `/api` routes — a nice example of operational scar tissue turned into a guardrail.

CI/CD spans 7 workflows: `ci.yml` (gitleaks + govulncheck + npm audit + Go race tests + web build + integration tests), `deploy-web.yml` (drift-proof `fetch + reset --hard origin/main`, health-checked, auto-rollback), `release.yml` (delegates to a shared `dev-standards` docker-release contract), `build-installers.yml` (6-platform agent + Windows/Inno, deb, rpm, Synology spk, macOS universal2), `deploy-installers.yml`, and `size-guard.yml`. Pre-commit hooks add local secret-scan + size-guard. Observability is Prometheus + Grafana + node-exporter + cAdvisor, plus **blackbox synthetic probes** against the public `/health` URL (catches Cloudflare/Traefik/backend chain failures that internal scraping misses). Alertmanager is deployed but still **UI-only / shadow mode** — no external paging yet.

---

## 5. Strengths

1. **Coherent, idiomatic architecture.** A modular Go monolith with a cleanly separated gRPC data plane is the right call for this scale — simpler than microservices, but with scale-out already designed in via the Redis-distributed hub.
2. **Serious agent reliability engineering.** Watchdog + ordered self-update + atomic swap + auto-rollback + graduated silent-agent recovery is best-in-class for a project this size and directly addresses RMM's hardest operational problems.
3. **Defense-in-depth on the dangerous surfaces.** Command execution (per-segment whitelist/blacklist, PowerShell-specific blocks, argument sanitization) and file paths (Unicode/RTL/zero-width normalization, 8.3 resolution, reserved-name and symlink-TOCTOU checks) are genuinely thorough — these are the places an RMM gets owned, and they're well-defended.
4. **Mature credential & transport story.** mTLS agent transport, managed API keys with per-key scoping, JWT algorithm-confusion protection, and a full credential-rotation subsystem with grace periods.
5. **Production-minded CI/CD and ops.** Multi-platform installer matrix, gated releases, secret scanning at commit and CI, size guard against binaries-in-git, synthetic public probes, and boot-race guardrails.
6. **Hygiene already cleaned up.** The previously-reported committed-secrets problem is resolved; `.env` is untracked with zero history.

---

## 6. Risks & Gaps (prioritized)

### High

- **H1 — Binary code signing is unimplemented (acknowledged).** `build-installers.yml` literally notes "Code signing is not yet enabled — Phase 2 follows." Agents and the watchdog run as SYSTEM/root and self-update from your servers; unsigned binaries mean tampering is undetectable and OS SmartScreen/Gatekeeper friction persists. This is the standout open critical from the project's own audit (formerly "C-03"). *Recommend: procure a Windows EV (or standard) cert + Apple Developer ID + GPG, add a signing stage, and verify signatures in the watchdog before swap.*
- **H2 — Stale dev enrollment token committed in `CLAUDE.md`.** The literal token `40addfff-…` appears at two places in tracked docs. If that token is still valid in any environment, it's a live enrollment credential in the repo; if it's been rotated, it's misleading. *Recommend: remove from docs, confirm it's revoked, and add it to the gitleaks allowlist only as a redaction.*
- **H3 — Release/version skew across repo and production.** `agent/version.json` says `1.77.39`, `git describe` says `v1.77.40-4`, and the changelog states production is already on **`1.78.0`**. The project's own model requires multiple version files to stay in lockstep; drift here risks agents not seeing updates or the rollout pipeline targeting the wrong baseline. *Recommend: reconcile the version files, tag production state explicitly, and document the single source of truth.*

### Medium

- **M1 — Two overlapping frontends (`src/` renderer vs `frontend/`).** Divergent versions and duplicated UI logic double the maintenance and bug surface. *Recommend: extract a shared component/render package; make Electron and web thin shells.*
- **M2 — Integration tests are non-blocking tech debt.** `ci.yml` runs integration tests with `continue-on-error: true` because the test backend doesn't reliably pass health checks on the self-hosted runner — so a whole tier of regressions can't fail a deploy. *Recommend: stabilize the test compose health check and re-arm the gate.*
- **M3 — Alertmanager is shadow-mode only.** Monitoring detects but cannot page. For a platform whose job is alerting on *other* people's machines, its own on-call path is incomplete. *Recommend: wire at least one external route (email/Slack/Signal).*
- **M4 — Audit-log coverage is partial.** The framework and table exist, but several sensitive actions (command/script execution, device deletion/uninstall, role changes, login failures) may not yet emit audit records consistently. *Recommend: enumerate sensitive actions and assert an audit row for each (ideally test-enforced).*
- **M5 — Stale compose migration mount.** `./src/main/migrations` is mounted into Postgres init but empty; authoritative migrations are elsewhere. Harmless today, confusing later. *Recommend: remove the mount.*

### Low

- **L1 — Root-directory debris.** ~15 one-off `fix-*.js` / `fix_*.ps1` scripts, two large `*.patch` files, and debug PNGs sit in the repo root (most are gitignored, but several docs/patches are tracked). Plus a dozen overlapping review docs of differing vintage (`ARCHITECTURE_REVIEW.md`, `SECURITY_REVIEW.md`, `CORRECTIONS_REQUIRED.md`, `QA_REPORT.md`, the `TLS_*.md` set). *Recommend: archive into `docs/history/` and keep one current ARCHITECTURE + one current SECURITY doc.*
- **L2 — Documentation drift.** Several in-repo docs still describe the original Oracle-Cloud reference architecture and the now-false "committed .env" state. *Recommend: prune to reflect the self-hosted reality.*
- **L3 — TLS fallback-to-insecure.** Both server and agent reportedly fall back to insecure transport if certs are missing — convenient in dev, risky if it ever masks a prod misconfig. *Recommend: a `TLS_ENFORCE=true` production guard.*

---

## 7. Recommended Roadmap

**Now (this week):**
- Reconcile version files and tag production (H3).
- Remove/rotate the `CLAUDE.md` enrollment token (H2).
- Remove the stale compose migration mount (M5).

**Next (2–4 weeks):**
- Implement code signing end-to-end and verify-before-swap in the watchdog (H1).
- Stabilize and re-arm integration tests (M2).
- Complete audit-log coverage with test enforcement (M4).
- Wire one real Alertmanager route (M3).

**Later (1–2 months):**
- Consolidate the two frontends behind a shared render layer (M1).
- Archive historical docs; keep one ARCHITECTURE + one SECURITY doc current (L1/L2).
- Add `TLS_ENFORCE` and remove insecure fallbacks in production (L3).
- Revisit the previously-deferred mTLS/gRPC agent-gateway refactor if/when it buys real simplification.

---

## 8. Caveats on This Assessment

- This is a **static read** of the working tree plus in-repo docs; it does not include running the system, load testing, or a live pen-test. Severity ratings are an engineer's judgment, not a formal audit.
- Some figures (LOC, route counts, message-type counts) are approximate.
- Several findings were **cross-checked and corrected** against the actual repo (notably the `.env`/git-history claim, which prior docs got wrong). Where in-repo audit docs and the live code disagreed, the **live code wins** and is what's reported here.

---

*Prepared from a live analysis of `D:\Projects\Sentinel` on 2026-06-26.*
