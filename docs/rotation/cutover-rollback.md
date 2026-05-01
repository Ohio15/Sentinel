# Sentinel API Key Rotation — Cutover Rollback & Failure-Mode Recovery

**Scope:** Failure-mode recovery procedures for the API key rotation cutover.
**Companion:** [`cutover-runbook.md`](./cutover-runbook.md) — happy-path procedure (T-5 smoke, cutover, 48h watch, drop step).
**Audience:** Operator (Ron) executing or recovering from the cutover.
**Host:** NEXUS — `ssh ohio_@192.168.1.20`
**Compose project:** `~/Sentinel/` on NEXUS.
**Placeholders used throughout:** `<OLD_KEY>` (pre-rotation key, gen-N) and `<NEW_KEY>` (post-rotation key, gen-N+1).

This document is a decision tree. Find the symptom, run the probe, run the recovery. Do not improvise — escalate by following the cross-references.

---

## Quick index

| ID  | Symptom                                                                  | Severity                  |
|-----|--------------------------------------------------------------------------|---------------------------|
| F1  | New key returns 401 during T-5 smoke test                                | Recoverable, no abort     |
| F2  | Old key returns 401 during T-5 smoke test (overlap window broken)        | Stop & investigate        |
| F3  | `sentinel-backend` container won't start (restart loop, non-zero exit)   | Roll back image+env       |
| F4  | Cutover succeeded; unexpected old-key 401s during 48h watch              | Extend or abort           |
| F5  | Drop step succeeded; subsequent requests start 401ing                    | Re-add old key, ntfy P1   |
| F6  | ntfy unreachable during watch window                                     | Known scar #17, best-effort |
| UR  | **Universal rollback** — restore `.env`, recreate container              | Panic button              |

---

## F1 — New key 401s during T-5 smoke test

**Symptom:** Smoke test (`curl -H "Authorization: Bearer <NEW_KEY>" ...`) returns HTTP 401 against `sentinel-backend` after the runbook's pre-cutover restart. Old key still works.

**Likely cause:** Environment not picked up after restart. Either the container was reloaded (not recreated) and Docker held the old env, or `.env` was edited but compose didn't see it.

**Probe (run in order):**
```bash
ssh ohio_@192.168.1.20
docker inspect sentinel-backend --format '{{.Config.Env}}' | tr ',' '\n' | grep -i API_KEYS
docker exec sentinel-backend env | grep API_KEYS
grep '^API_KEYS=' ~/Sentinel/.env
```

Expected during overlap window: `API_KEYS=<OLD_KEY>,<NEW_KEY>` in **all three** outputs.

**Decision:**
- If `.env` is correct but `docker inspect` shows the old value -> environment is stale.
- If `.env` itself is wrong -> jump to **F2** (the edit clobbered).
- If both correct but `env` inside the container is wrong -> jump to **F4 / auth.go regression path**.

**Recovery (stale env):**
```bash
cd ~/Sentinel
docker compose up -d --force-recreate sentinel-backend
# wait 10s for healthcheck, then re-smoke:
curl -sS -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer <NEW_KEY>" \
  https://sentinel.nexus/api/v1/health
```
Expected: `200`. If still `401` -> escalate to **F4**.

**Do NOT proceed to cutover** until smoke is green for both `<OLD_KEY>` and `<NEW_KEY>`.

---

## F2 — Old key 401s during T-5 smoke test (overlap broke backward compat)

**Symptom:** Smoke test of `<OLD_KEY>` returns 401 immediately after the overlap restart. This means the comma-list parser broke, `auth.go` regressed, or the `.env` edit clobbered the old key entirely.

**Likely causes:**
1. `API_KEYS` parsed as a single literal string instead of comma-split (regression in `internal/auth/auth.go` or middleware).
2. `.env` edit replaced `<OLD_KEY>` instead of appending `,<NEW_KEY>`.
3. Hidden whitespace / CRLF in the `.env` file from a Windows-side editor.

**Probe:**
```bash
ssh ohio_@192.168.1.20
grep '^API_KEYS=' ~/Sentinel/.env
# expect exactly:  API_KEYS=<OLD_KEY>,<NEW_KEY>
file ~/Sentinel/.env                     # check for CRLF
od -c ~/Sentinel/.env | grep -A1 API_KEYS  # inspect for stray \r
ls -lt ~/Sentinel/.env.pre-rotate-*       # confirm backup exists
```

**Recovery — restore prior `.env` and stop:**
```bash
cd ~/Sentinel
# pick the most recent backup
LATEST=$(ls -1t ~/Sentinel/.env.pre-rotate-* | head -1)
cp "$LATEST" ~/Sentinel/.env
docker compose up -d --force-recreate sentinel-backend
# verify old key validates again:
curl -sS -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer <OLD_KEY>" \
  https://sentinel.nexus/api/v1/health
```
Expected: `200` against `<OLD_KEY>`.

**STOP.** Do not retry cutover. Investigate `internal/auth/auth.go` parser and re-test in dev before re-attempting. File a ticket: "auth.go API_KEYS multi-value parsing regression". Cross-ref runbook step "T-5 smoke" — that gate exists exactly to catch this.

---

## F3 — `sentinel-backend` container won't start (restart loop, non-zero exit)

**Symptom:** After `docker compose up -d --force-recreate sentinel-backend`, the container is in `Restarting` or `Exited (N)` where `N != 0`. Health endpoint unreachable.

**Probe (top-down):**
```bash
ssh ohio_@192.168.1.20
docker ps -a --filter name=sentinel-backend
docker logs --tail=80 sentinel-backend
docker inspect sentinel-backend --format '{{.State.ExitCode}} {{.State.Error}}'
docker compose -f ~/Sentinel/docker-compose.yml config | grep -A3 sentinel-backend
```

**Triage tree:**
| Log signature                                              | Cause                                     | Recovery                                                                                          |
|------------------------------------------------------------|-------------------------------------------|---------------------------------------------------------------------------------------------------|
| `panic: invalid API_KEYS` / env parse error                | `.env` malformed                          | F2 recovery (restore backup `.env`).                                                              |
| `dial tcp ... postgres ... connection refused`             | DB dependency not up                      | `docker compose up -d postgres redis && sleep 5 && docker compose up -d sentinel-backend`         |
| Compile/runtime error referencing recent code              | Bad image deployed alongside rotation     | Identify previous tag, pin image, restart (see "Image rollback" below).                           |
| `bind: address already in use`                             | Stale process holding port                | `ss -tlnp \| grep :<port>`; identify owning PID; coordinate kill before recreate.                 |
| OOMKilled (`State.OOMKilled: true`)                        | Memory limit too tight                    | Separate incident — not rotation-related; revert `.env` first via UR, file ticket.                |

**Image rollback (if rotation overlapped a deploy):**
```bash
cd ~/Sentinel
# inspect prior digest from registry / your deploy log
docker compose pull sentinel-backend            # only if you know prior tag is fine
# or pin explicitly in docker-compose.yml: image: ghcr.io/.../sentinel-backend:<prior-tag>
docker compose up -d --force-recreate sentinel-backend
```

**Always:** if `.env` was modified during this incident, run **UR** first to remove rotation as a variable, then debug the image separately.

---

## F4 — Cutover succeeded but consumer breaks during 48h watch (unexpected old-key 401)

**Symptom:** Cutover step landed cleanly; T-5 smoke green for `<NEW_KEY>`; runbook proceeded to 48h watch with `API_KEYS=<NEW_KEY>` only. Backend logs now show 401s with the old key prefix.

**Likely cause:** An off-repo consumer (B2 inventory miss). Common culprits: ad-hoc cron jobs, a personal script on `remoteserver`, an MCP client config, an external webhook integrator, a forgotten Postman collection.

**Probe — identify the caller:**
```bash
ssh ohio_@192.168.1.20
# Look at recent 401s in backend logs:
docker logs sentinel-backend --since 2h 2>&1 | grep -E '401|unauthorized' | tail -50
# If the auth layer logs key prefix + source IP + UA:
docker logs sentinel-backend --since 2h 2>&1 \
  | grep -E 'auth=fail|key_prefix=' \
  | awk '{print $0}' | tail -50
# Cross-reference IPs with home subnet, Tailscale, or external:
# 192.168.1.x  -> LAN consumer
# 100.x.x.x    -> Tailscale node
# anything else -> external integration
```

**Decision tree:**

```
Reject rate < 5/hr  AND  caller identified by IP/UA?
├── YES -> EXTEND watch window: re-add OLD_KEY for another 48h.
│           Notify the consumer owner. Set calendar to redo drop step.
└── NO  -> Reject rate ≥ 5/hr  OR  caller unidentifiable?
            ├── Identifiable, high rate     -> EXTEND + page consumer owner immediately.
            └── Unidentifiable, high rate   -> ABORT cutover. Revert to OLD_KEY as primary,
                                                keep NEW_KEY valid for 7d while you hunt.
```

**Recovery — EXTEND watch (re-add old key for 48h):**
```bash
ssh ohio_@192.168.1.20
cd ~/Sentinel
cp .env .env.pre-extend-$(date -u +%Y%m%dT%H%M%SZ)
# edit API_KEYS back to dual-value:
sed -i 's|^API_KEYS=.*|API_KEYS=<OLD_KEY>,<NEW_KEY>|' .env
grep '^API_KEYS=' .env
docker compose up -d --force-recreate sentinel-backend
# verify both keys validate:
for K in '<OLD_KEY>' '<NEW_KEY>'; do
  curl -sS -o /dev/null -w "$K -> %{http_code}\n" \
    -H "Authorization: Bearer $K" \
    https://sentinel.nexus/api/v1/health
done
# ntfy (best-effort, see F6):
curl -fsS -d "Sentinel rotation watch EXTENDED 48h — unidentified consumer" \
  https://ntfy.nexus/sentinel-ops || true
```
Re-run the runbook drop step **only after** the new 48h shows zero unexpected old-key traffic.

**Recovery — ABORT cutover (high reject rate, caller unknown):**
```bash
ssh ohio_@192.168.1.20
cd ~/Sentinel
cp .env .env.pre-abort-$(date -u +%Y%m%dT%H%M%SZ)
sed -i 's|^API_KEYS=.*|API_KEYS=<OLD_KEY>,<NEW_KEY>|' .env
docker compose up -d --force-recreate sentinel-backend
```
Status: rotation paused, both keys live, 7-day investigation window. File a P1 ticket. Do not retry cutover until the consumer is identified, migrated, and re-verified silent.

---

## F5 — Drop step succeeded but subsequent requests start 401ing

**Symptom:** Drop step (final `<NEW_KEY>`-only state) was green at T+0; some minutes/hours later, requests using `<NEW_KEY>` begin returning 401.

**Likely causes (rule out in order):**
1. `.env` was edited again post-drop (config-management drift, another operator).
2. Container was recreated from a different env source (compose override, swarm, watchtower).
3. New image deployed mid-watch with different auth code.
4. Genuine compromise of `<NEW_KEY>` and someone rotated it again — least likely, but check.

**Probe:**
```bash
ssh ohio_@192.168.1.20
grep '^API_KEYS=' ~/Sentinel/.env
docker inspect sentinel-backend --format '{{.Config.Env}}' | tr ',' '\n' | grep API_KEYS
docker inspect sentinel-backend --format 'image={{.Config.Image}} created={{.Created}}'
ls -lt ~/Sentinel/.env*
# any unexpected recent .env mutation?
sudo journalctl --since '6 hours ago' | grep -iE 'sentinel|compose|watchtower' | tail -50
```

**Recovery — restore dual-key for 24h while investigating:**
```bash
ssh ohio_@192.168.1.20
cd ~/Sentinel
cp .env .env.pre-f5-$(date -u +%Y%m%dT%H%M%SZ)
sed -i 's|^API_KEYS=.*|API_KEYS=<OLD_KEY>,<NEW_KEY>|' .env
docker compose up -d --force-recreate sentinel-backend
curl -fsS -d "Sentinel rotation: F5 — post-drop 401s, dual-key restored for 24h" \
  https://ntfy.nexus/sentinel-ops || true
```
Open a P1. Do **not** treat as routine — F5 means the system regressed silently after a green drop.

---

## F6 — ntfy unreachable during watch window

**Symptom:** Cutover ntfy probes (`curl https://ntfy.nexus/...`) hang, time out, or return non-2xx. Container `ntfy` may be stopped, unhealthy, or the host route is broken.

**Context:** This is **known scar #17**. After commit `b443f89`, ntfy publishing across Sentinel is best-effort: failures must not block primary work. The same rule applies to the rotation runbook — the cutover does not depend on ntfy succeeding.

**Probe:**
```bash
ssh ohio_@192.168.1.20
docker ps --filter name=ntfy
docker logs --tail=30 ntfy
curl -sS -o /dev/null -w '%{http_code}\n' https://ntfy.nexus/healthz
```

**Recovery (do this AFTER the rotation step, not during):**
- All cutover/rollback ntfy calls in this document use `|| true`. They will not fail the script.
- If ntfy is down >1 hour:
  ```bash
  ssh ohio_@192.168.1.20
  docker restart ntfy
  sleep 5
  curl -sS -o /dev/null -w '%{http_code}\n' https://ntfy.nexus/healthz
  ```
- If still down, **file a separate incident** referencing scar #17. Do **not** delay the rotation drop step waiting for ntfy.

**Rule:** ntfy outage alone is never grounds to abort or extend the rotation. It is a notification channel, not a control plane.

---

## Universal rollback (panic button) — UR

**When to use:** Any state where you are unsure what's wrong with auth, the container, or `.env`, and you need to return to the pre-rotation baseline immediately. Single command, no thinking.

```bash
ssh ohio_@192.168.1.20 'cp $(ls -1t ~/Sentinel/.env.pre-rotate-* | head -1) ~/Sentinel/.env && cd ~/Sentinel && docker compose up -d --force-recreate sentinel-backend'
```

What it does:
1. Picks the most recent runbook-created backup (`.env.pre-rotate-<UTC-timestamp>` per the cutover runbook step "Backup `.env`").
2. Overwrites the live `.env` with that backup.
3. Force-recreates `sentinel-backend` so the env is freshly loaded (avoids the F1 stale-env trap).

After UR, verify:
```bash
ssh ohio_@192.168.1.20 'curl -sS -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer <OLD_KEY>" https://sentinel.nexus/api/v1/health'
```
Expected: `200`. The system is now on the pre-rotation key. Stop, breathe, diagnose. Do not re-attempt cutover until root cause is documented.

**Backup-file convention:** the runbook's "Backup `.env`" step writes `~/Sentinel/.env.pre-rotate-<UTC-timestamp>` (e.g. `.env.pre-rotate-20260501T1830Z`). UR depends on this file existing — do not skip the runbook's backup step.

---

## Cross-references

- Happy-path procedure: [`cutover-runbook.md`](./cutover-runbook.md) — T-5 smoke, cutover, 48h watch, drop step, backup-file naming.
- Migration shim revocation: [`migrate-shim-revocation.md`](./migrate-shim-revocation.md).
- DB-side revocation SQL: [`revoke_legacy_api_key.up.sql`](./revoke_legacy_api_key.up.sql) / [`revoke_legacy_api_key.down.sql`](./revoke_legacy_api_key.down.sql).
- Credential rotation SOP (8-phase): `~/.claude/projects/C--Users-ohio-/memory/feedback_credential-rotation-protocol.md`.
- ntfy best-effort scar #17: commit `b443f89`.

## Failure modes considered but excluded

- **Postgres / Redis outage during cutover** — not rotation-specific; treated as standard service incident, runbook does not gate on these. F3's DB-connection branch points at the existing service-up runbook.
- **TLS / Cloudflare tunnel down** — affects all of Sentinel, not just rotation. Out of scope; covered by edge-traefik runbook.
- **Wrong key written to .env (e.g. typo of `<NEW_KEY>`)** — collapses into F2 (smoke test detects it) and UR (recovery is the same).
- **Operator pastes key into wrong service** — caught by the per-service smoke step in the runbook; not a distinct failure mode here.
- **Time skew / clock drift causing JWT-style validation failures** — Sentinel API keys are static bearer tokens, not time-bound. Excluded.
