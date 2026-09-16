# Sentinel Credential Rotation Plan — 2026-04-30

**Status**: Phase 1 (planning) only. Read-only investigation. No values rotated, no .env modified.
**Baseline**: v1.67.10..HEAD (per gitleaks scan).
**Scope**: 3 leaked credentials below. SOP: D:/Projects/dev-standards/credential-rotation/PROTOCOL.md

## 1. Per-Credential Audit

### 1a. API_KEY — server static API key (256-bit hex)

| Location | Detail |
|---|---|
| Server reader | server/pkg/config/config.go:111 — getEnv(API_KEY); injected into cfg.APIKey |
| Server validator | server/internal/middleware/auth.go:138-153 (AuthOrAPIKeyMiddleware) — subtle.ConstantTimeCompare of header X-API-Key vs apiKey; sets role=operator |
| DB migration shim | server/internal/credentials/api_key_manager.go:395-444 (MigrateStaticAPIKey) — bcrypt-stores static key into credential_keys with metadata original_still_valid:true |
| Compose injection | docker-compose.yml:30 |
| NEXUS .env | API_KEY=55ccf1fd... (confirmed present, redacted) |
| Container env | confirmed identical, redacted |
| Working-tree leaks | scripts/recovery-steps.py:9, scripts/recovery-task.py:16, scripts/send-recovery-command.py:16 (all hardcode the literal) |
| Git-history leaks | commits 4a60c93 (v1.74.0), 367c0ea — both within v1.67.10..HEAD window |
| Doc leaks | None in markdown files (verified) |
| Overlap support? | NO. Single-key constant-time compare. No API_KEYS comma-list parser. Rotation = brief outage OR add ~15 LOC overlap code first (Protocol Phase 4). |

### 1b. ENROLLMENT_TOKEN — agent enrollment bearer (UUID 40addfff-...)

| Location | Detail |
|---|---|
| Server reader | server/pkg/config/config.go:110 — getEnv(ENROLLMENT_TOKEN); required at startup (config.go:192-194) |
| Server validators | server/internal/middleware/auth.go:114 (AgentAuthMiddleware, static-only); auth.go:228 (NewAgentAuthMiddleware, DB-first with static fallback at :252); auth.go:263 (ValidateDatabaseToken, plaintext + bcrypt branches) |
| Header names | X-Enrollment-Token (primary), X-Agent-Token (alias) |
| Agent installer-side | UUID baked into installer scripts via %%ENROLLMENT_TOKEN%% template substitution: installers/synology/build-spk.sh:87, installers/macos/build-pkg.sh:122, installers/linux/rpm/sentinel-agent.spec:52, installers/linux/build-deb.sh:81, installers/linux/debian/postinst:50, installers/macos/scripts/postinstall:52 |
| Server install handlers | server/internal/api/agents.go:514-591 writes the live token into bash one-liner installer scripts |
| Working-tree leaks | **CLAUDE.md:235 (binary-patch example) and CLAUDE.md:294 (Default token) — developer doc** |
| Git-history leaks | commits 315e0ba, 791698a, 7e2ebf6, plus binary leaks via installers/sentinel-agent-windows-amd64.exe, agent/sentinel-agent.exe, agent/sentinel-watchdog.exe (binaries inherit the patched-in token literal) |
| NEXUS .env | ENROLLMENT_TOKEN=40addfff... (confirmed present, redacted) |
| Overlap support? | YES, via DB. enrollment_tokens table holds N tokens; static env is a backwards-compat fallback. Rotation = INSERT new row, then drop static. 17 active rows present on NEXUS. |

### 1c. TURN_AUTH_SECRET — TURN HMAC shared secret

| Location | Detail |
|---|---|
| Server reader | server/cmd/sentinel/main.go:199 — os.Getenv(TURN_AUTH_SECRET) |
| Server consumer | server/internal/turn/server.go:191 (HMAC-SHA1 in GenerateCredentials); server.go:241 (matching HMAC in authHandler) |
| Compose injection | docker-compose.yml:51 |
| NEXUS .env | TURN_AUTH_SECRET= (**EMPTY** — confirmed via container env) |
| Working-tree leaks | NONE in working tree |
| Git-history leaks | agent/internal/desktop/helper/webrtc.go in 8 commits: bad511d, 1f984ff, 8dad864, 142f15c, 095e618, b786b15, f159ffa, 598187a (all within v1.67.10..HEAD) |
| **Critical mismatch** | Leaked literal uWdWNmkhvyqTmhD0 is paired with username e8dd65b92f8b3c9bd6c4e894 and URL turn:a.relay.metered.ca. **This is a 3rd-party metered.ca TURN credential, NOT the Sentinel TURN_AUTH_SECRET.** Different secret surfaces. Sentinel TURN is currently unconfigured. |

## 2. Blast Radius

- **Enrolled devices**: 11 rows in devices table; 37 non-revoked rows in client_certificates. Steady-state agents authenticate via mTLS client certs (AGENT_MTLS_PORT=8443), not enrollment token. Enrollment token is used at first-time install only -> rotation drops zero existing agents.
- **Active enrollment tokens**: 17 in DB (mostly one-shot install codes). The Default Token (40addfff-...) has max_uses=NULL, use_count=0.
- **Active client browser sessions**: API_KEY is server-to-server / scripted-recovery only; browser UI uses JWT. Rotating API_KEY breaks the 3 hardcoded recovery scripts and any external automation that holds the literal — inventory beyond repo unknown (open question).
- **WebRTC sessions**: Sentinel TURN_AUTH_SECRET is empty -> no live TURN sessions to drop. metered.ca is a different surface.

## 3. Per-Credential Rotation Plan

### Phase 0.5 — Triage
Capture leaking SHAs (above), snapshot 24h backend logs to ~/Sentinel/forensic/2026-04-30/. Severity: API_KEY=High/High/Recoverable; ENROLLMENT_TOKEN=Med/Med/Recoverable (mTLS limits exposure); metered.ca=High/High (3rd party).

### API_KEY rotation
- **Phase 2 (cleanup)**: Replace literal with os.environ[SENTINEL_API_KEY] in scripts/recovery-steps.py, scripts/recovery-task.py, scripts/send-recovery-command.py. Local commit, no push.
- **Phase 3 (observability)**: Confirm backend already logs the [WARN] API key used line in auth.go:146; verify request logging captures source IP via Cloudflare header.
- **Phase 4 (overlap support)**: Add API_KEYS comma-list reader to pkg/config/config.go and update AuthOrAPIKeyMiddleware to iterate with constant-iteration count and padding. ~15 LOC; replace cfg.APIKey string with cfg.APIKeys []string. Build, deploy.
- **Phase 5 (deploy new)**: Generate 32-byte hex; append to API_KEYS env so old + new both validate. docker compose restart backend. Distribute new key to 3 scripts via env. Hit GET /api/credentials/api-keys with each to verify 200.
- **Phase 6 (watch)**: Tail backend for Invalid API key and 401s with prefix 55ccf1fd... for 48h.
- **Phase 7 (drop)**: Remove old from API_KEYS. Restart. Also revoke the migrated credential_keys row from MigrateStaticAPIKey if present (otherwise old key remains valid via DB path).

### ENROLLMENT_TOKEN rotation
- **Phase 2 (cleanup)**: Replace literal in CLAUDE.md:235,294 with <DEFAULT_TOKEN_FROM_ENV> placeholder. Audit agent/README.md for additional references.
- **Phase 3 (overlap)**: NOT NEEDED — DB already supports multi-token via enrollment_tokens. Plan: INSERT new row before retiring old.
- **Phase 5 (deploy new)**: SQL: INSERT INTO enrollment_tokens (token, name, is_active, is_legacy) VALUES (gen_random_uuid(), Default Token v2 - 2026-04-30, TRUE, FALSE). Update NEXUS .env ENROLLMENT_TOKEN=<new>, docker compose restart backend. Static fallback now matches new; DB still accepts old too.
- **Phase 4 (agent rollout)**: No agent action — mTLS-enrolled agents do NOT re-present this token. Only fresh installs care. Cutover is instant.
- **Phase 6 (watch)**: 48h watch backend logs for Invalid agent token referencing the old UUID.
- **Phase 7 (drop)**: UPDATE enrollment_tokens SET is_active=FALSE WHERE token=<OLD_ENROLLMENT_TOKEN_UUID — value redacted 2026-09-16; read it from ENROLLMENT_TOKEN in the deploy .env, never from this document>. Restart backend.

### TURN_AUTH_SECRET / metered.ca rotation
- **Phase 2 (cleanup)**: NONE — already absent from working tree.
- **Sentinel TURN_AUTH_SECRET**: empty in prod; defer rotation until TURN is enabled (TURN_ENABLED=true). Generate fresh secret at activation time, never bake into agent binary.
- **metered.ca credential**: OUT OF SCOPE for env rotation. Action: log in to dashboard.metered.ca, rotate the API key for username e8dd65b92f8b3c9bd6c4e894 OR delete that user. Track separately.

### Phase 7 — Eradication (history scrub)

replacements.txt:

    55ccf1fd8b1d937fd9377a5c306eaf675e00a5876e1cd33e5ac1c602f7559168==><REDACTED:rotated-2026-04-30>
    <OLD_ENROLLMENT_TOKEN_UUID — value redacted 2026-09-16; read it from ENROLLMENT_TOKEN in the deploy .env, never from this document>==><REDACTED:rotated-2026-04-30>
    uWdWNmkhvyqTmhD0==><REDACTED:rotated-2026-04-30>
    e8dd65b92f8b3c9bd6c4e894==><REDACTED:rotated-2026-04-30>

Then git filter-repo --replace-text replacements.txt. Force-push to main — circuit-breaker scope, requires explicit Ron CONFIRM. **Caveat**: binaries committed in bad511d, 8dad864, 142f15c (installers/*.exe, agent/*.exe) likely contain the patched-in literals at byte-level. filter-repo --replace-text will rewrite them and produce unusable binaries. Companion command: git filter-repo --invert-paths --path agent/sentinel-agent.exe --path agent/sentinel-watchdog.exe --path agent/sentinel-desktop.exe --path agent/sentinel-desktop-helper.exe --path installers/sentinel-agent-windows-amd64.exe --path installers/sentinel-bootstrap-windows-amd64.exe --path installers/regfix.exe --path downloads/sentinel-watchdog.exe.

## 4. Risks and Gotchas

1. **CLAUDE.md leaks the enrollment token**. Even after DB rotation, anyone copy-pasting from CLAUDE.md:235 or :294 hits Invalid agent token. Doc must be updated in Phase 2 and validated during Phase 6 watch.
2. **Binary leaks**. git filter-repo --replace-text corrupts the patched-in EXE bytes. --invert-paths --path for the .exe files is required in addition. Binaries should never have been committed; .gitignore cleaned this at bad511d but history retains them.
3. **Server has no API_KEYS overlap support**. Phase 4 of the SOP is mandatory engineering work (~15 LOC PR), not a skip. Without it: ~30 sec backend restart with stale scripts hitting 401 until updated. Recommend implementing the overlap.
4. **Single static env var assumption**. cfg.APIKey is a single string. Adding API_KEYS requires touching Config struct, Load(), and AuthOrAPIKeyMiddleware. Replace single field with slice and adapt callers — do not fork into two code paths.
5. **TURN secret confusion**. The task brief asserts uWdWNmkhvyqTmhD0 IS TURN_AUTH_SECRET. **It is not.** It is a metered.ca 3rd-party password. Rotating Sentinel TURN_AUTH_SECRET does nothing for that exposure. metered.ca rotation is dashboard-driven, not env-driven.
6. **Frontend TURN architecture concern**: Bundling any TURN long-term shared secret into a browser bundle (Vite build args) is wrong by design — clients should call /api/turn/credentials and receive HMAC-derived ephemeral creds (TURN REST API). The leaked literal in agent/internal/desktop/helper/webrtc.go confirms a past anti-pattern of baking creds into the Go agent binary. Confirm the current runtime path goes through turnServer.GenerateCredentials() and not a baked literal anywhere.
7. **API_KEY MigrateStaticAPIKey shim** stores a bcrypt of the static key in credential_keys with original_still_valid:true. After rotation, that DB row also needs revoke or the old key keeps validating via the DB-key path (ValidateKey flow).
8. **Force-push to main**: per SOP, requires explicit CONFIRM: git filter-repo force-push to Sentinel main is authorized from Ron. Coordinate with any open PRs and the NEXUS self-hosted runner re-pulling main.

## Forensic Record

- Detection: gitleaks scan v1.67.10..HEAD, 2026-04-30
- Scope SHAs: API_KEY=4a60c93,367c0ea; ENROLL=315e0ba,791698a,7e2ebf6; metered.ca=bad511d,1f984ff,8dad864,142f15c,095e618,b786b15,f159ffa,598187a
- Plan author: QA Butcher subagent, 2026-04-30
