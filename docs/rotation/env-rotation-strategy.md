# API_KEYS Env-Var Rotation Strategy

**Status**: Mechanical specification. Phase 2 of the Credential Rotation Plan, incident #19.
**Author**: Phase 2 design pass, branch `phase2-api-keys-overlap` (commit `99121e7`), 2026-05-01.
**Companion docs**:
- `migrate-shim-revocation.md` — DB-side revocation of any migrated `credential_keys` row.
- `revoke_legacy_api_key.up.sql` / `revoke_legacy_api_key.down.sql` — SQL companions for the shim path.

This document covers ONLY the env-var (`API_KEYS`) rotation surface. The DB-side revocation is independent and runs after Phase 7 of this strategy.

---

## 0. Critical invariant (READ FIRST)

**`API_KEYS` is parsed once at middleware factory time, not per-request.**

Source of truth: `server/internal/middleware/auth.go` — `loadAPIKeys()` is called inside `AuthOrAPIKeyMiddleware` at the closure creation site (the `return func(c *gin.Context)` factory body, executed at boot when routes are wired). The resulting `[]string` is captured by the request handler closure. `os.Getenv("API_KEYS")` is **not** re-read on subsequent requests.

Implication: editing `~/Sentinel/.env` does **nothing** until the `sentinel-backend` container is restarted. Every cutover step that mutates `.env` MUST be paired with `docker compose restart sentinel-backend`. Skipping the restart will appear to work in `grep` but the live process keeps the old key set.

The factory also emits one boot log line: `[AUTH] API_KEYS list contains N keys`. Use it to confirm the restart picked up the new state (Section 3, Section 6).

---

## 1. Key generation

```bash
NEW_KEY=$(openssl rand -hex 32)
# Sanity-check: 64 hex chars exactly
[ "${#NEW_KEY}" -eq 64 ] && echo "ok: 64 chars" || { echo "FAIL: ${#NEW_KEY} chars"; unset NEW_KEY; }
# Sanity-check: hex-only (no smuggled non-hex bytes)
echo "$NEW_KEY" | grep -qE '^[0-9a-f]{64}$' && echo "ok: hex" || echo "FAIL: non-hex char"
```

Run on NEXUS, in a `umask 077` shell, in a tmux/screen scrollback that will be cleared. Do not echo `$NEW_KEY` after these two checks. Do not write it to disk anywhere except `.env` (Section 2) and the local distribution sink (Section 7).

`<NEW_KEY>` placeholder = the value of `$NEW_KEY` from this step. `<OLD_KEY>` = the existing single value currently on the `API_KEYS=` line of `~/Sentinel/.env` (the leaked `55ccf1fd…` key for incident #19).

---

## 2. NEXUS `.env` edit — append, do not overwrite

The single line we are mutating has the form:

```
API_KEYS=<OLD_KEY>
```

Goal: transform to `API_KEYS=<OLD_KEY>,<NEW_KEY>` with no other byte changed.

### 2a. Snapshot first

```bash
cp ~/Sentinel/.env ~/Sentinel/.env.bak.$(date -u +%Y%m%dT%H%M%SZ)
```

### 2b. Append the new key

Use a literal-mode `sed` so any `/`, `&`, or `\` in the key is harmless. The hex-only sanity check in Section 1 already excludes those, but the pattern below is robust regardless:

```bash
sed -i.tmp -e "/^API_KEYS=/ s|\$|,${NEW_KEY}|" ~/Sentinel/.env
rm ~/Sentinel/.env.tmp
```

Equivalent `printf`-based alternative if `sed -i` is unavailable:

```bash
awk -v k="$NEW_KEY" 'BEGIN{FS=OFS=""} /^API_KEYS=/{print $0 "," k; next} {print}' \
    ~/Sentinel/.env > ~/Sentinel/.env.new && mv ~/Sentinel/.env.new ~/Sentinel/.env
```

### 2c. Verify exactly two comma-separated values

```bash
grep "^API_KEYS=" ~/Sentinel/.env | tr ',' '\n' | wc -l
# Expected output: 2
```

If output is not `2`, restore the snapshot from 2a and abort:

```bash
cp ~/Sentinel/.env.bak.<ts> ~/Sentinel/.env
```

Additional sanity:

```bash
grep -c "^API_KEYS=" ~/Sentinel/.env
# Expected: 1   (exactly one API_KEYS line, not duplicated)
awk -F= '/^API_KEYS=/{print length($2)}' ~/Sentinel/.env
# Expected: 129   (64 + 1 + 64 = old + comma + new)
```

---

## 3. Restart sequence

```bash
docker compose -f ~/Sentinel/docker-compose.yml restart sentinel-backend
```

Wait for healthy:

```bash
for i in $(seq 1 30); do
  state=$(docker inspect -f '{{.State.Health.Status}}' sentinel-backend 2>/dev/null)
  echo "[$i] health=$state"
  [ "$state" = "healthy" ] && break
  sleep 2
done
[ "$state" = "healthy" ] || { echo "FAIL: not healthy after 60s"; exit 1; }
```

Confirm the factory picked up the two-key list:

```bash
docker logs --since 1m sentinel-backend 2>&1 | grep "\[AUTH\] API_KEYS list contains"
# Expected: [AUTH] API_KEYS list contains 2 keys
```

If the count is not `2`, the container is running with stale env. Re-check `.env`, re-run the restart.

---

## 4. Smoke verification (4 probes)

Run from NEXUS (or any host that reaches `https://sentinelrmm.us` / local equivalent). Backend uses the `X-API-Key` request header, **not** `Authorization`. The "empty Authorization" probe in the original spec is reinterpreted below as "empty `X-API-Key`" to match middleware reality (`auth.go:177`).

Variables:

```bash
HOST="https://sentinelrmm.us"   # or https://localhost with -k for in-cluster
PROBE="/api/devices"            # any AuthOrAPIKey-protected route
```

| # | Probe | Command | Expected HTTP |
|---|---|---|---|
| a | New key validates | `curl -s -o /dev/null -w "%{http_code}\n" -H "X-API-Key: $NEW_KEY" "$HOST$PROBE"` | `200` |
| b | Old key still validates (overlap window proof) | `curl -s -o /dev/null -w "%{http_code}\n" -H "X-API-Key: $OLD_KEY" "$HOST$PROBE"` | `200` |
| c | Garbage key rejected | `curl -s -o /dev/null -w "%{http_code}\n" -H "X-API-Key: deadbeefdeadbeef" "$HOST$PROBE"` | `401` |
| d | Missing/empty key header | `curl -s -o /dev/null -w "%{http_code}\n" -H "X-API-Key:" "$HOST$PROBE"` | `401` |

Probe (b) is the load-bearing one: if it returns 401, the middleware did not load the old key and recovery scripts will start failing **right now**. Roll back via Section 2a before proceeding.

A 401 on (a) or 200 on (c)/(d) is a hard stop — investigate before any caller updates.

---

## 5. 48-hour telemetry watch

The middleware emits per-request log lines via `log.Printf`. Container stdout flows to Docker's json-file driver. The log lines we care about (auth.go:188 / auth.go:193):

```
[AUTH] API key accepted prefix=<8chars> ip=<ip> path=<path>
[AUTH] API key rejected prefix=<8chars> ip=<ip> path=<path>
```

`<8chars>` = first 8 chars of the supplied X-API-Key. For `<OLD_KEY>` = `55ccf1fd…` the prefix is `55ccf1fd`. For `<NEW_KEY>` it is the first 8 hex chars of the new key.

### 5a. Per-key acceptance count (rolling)

```bash
docker logs --since 1h sentinel-backend 2>&1 \
  | awk '/\[AUTH\] API key accepted/ { for (i=1;i<=NF;i++) if ($i ~ /^prefix=/) print $i }' \
  | sort | uniq -c | sort -rn
```

### 5b. Per-key rejection count (the 48h watch metric)

```bash
docker logs --since 48h sentinel-backend 2>&1 \
  | awk '/\[AUTH\] API key rejected/ { for (i=1;i<=NF;i++) if ($i ~ /^prefix=/) print $i }' \
  | sort | uniq -c | sort -rn
```

Interpretation:
- Rejections of `prefix=55ccf1fd` (old key) appearing in this window mean a caller has not been migrated to `<NEW_KEY>` yet, OR a leak-replay attempt is in progress. Cross-reference with `ip=` field before drawing conclusions.
- Rejections of `prefix=<NEW_KEY[:8]>` are typos / partial rollouts. Investigate the source IP's caller code.
- After 48h, the goal is **zero** acceptances of the old prefix from any internal IP. Only then proceed to Section 6.

Per-IP breakdown for old-key acceptance (the "who still uses the old key" report):

```bash
docker logs --since 48h sentinel-backend 2>&1 \
  | awk '/\[AUTH\] API key accepted prefix=55ccf1fd/ { for (i=1;i<=NF;i++) if ($i ~ /^ip=/) print $i }' \
  | sort | uniq -c | sort -rn
```

If this returns any rows after the 48h window, do NOT proceed to Section 6 — a caller is still pinned to `<OLD_KEY>`.

---

## 6. Phase 7 close-out — drop the old key

### 6a. Inverse `.env` edit

Snapshot, then strip the `<OLD_KEY>,` prefix from the line:

```bash
cp ~/Sentinel/.env ~/Sentinel/.env.bak.closeout.$(date -u +%Y%m%dT%H%M%SZ)
sed -i.tmp -e "/^API_KEYS=/ s|=${OLD_KEY},|=|" ~/Sentinel/.env
rm ~/Sentinel/.env.tmp
```

Verify exactly one value remains:

```bash
grep "^API_KEYS=" ~/Sentinel/.env | tr ',' '\n' | wc -l
# Expected: 1
awk -F= '/^API_KEYS=/{print length($2)}' ~/Sentinel/.env
# Expected: 64
```

### 6b. Restart and confirm count = 1

```bash
docker compose -f ~/Sentinel/docker-compose.yml restart sentinel-backend
# wait-for-healthy loop from Section 3, then:
docker logs --since 1m sentinel-backend 2>&1 | grep "\[AUTH\] API_KEYS list contains"
# Expected: [AUTH] API_KEYS list contains 1 keys
```

### 6c. Negative smoke: old key now 401s

```bash
curl -s -o /dev/null -w "%{http_code}\n" -H "X-API-Key: $OLD_KEY" "$HOST$PROBE"
# Expected: 401
curl -s -o /dev/null -w "%{http_code}\n" -H "X-API-Key: $NEW_KEY" "$HOST$PROBE"
# Expected: 200
```

If old key returns 200, the restart did not pick up the trimmed env. Re-check `.env` and re-run.

### 6d. Hand-off to DB revocation

Phase 7 (env layer) is now complete. The DB-side revocation (`migrate-shim-revocation.md` + `revoke_legacy_api_key.up.sql`) is the next step in the SOP and is independent of this document. Run it only after 6c is green.

---

## 7. Distribution to Ron's local dev environment

Local dev box: Windows, `D:/Projects/Sentinel/.env.local` (gitignored — verify in `.gitignore` before writing).

### 7a. Transport

Out-of-band only. Do **not** paste in chat, commit messages, PR descriptions, or any cloud-synced location. Acceptable channels:

1. SSH into NEXUS, copy from `~/Sentinel/.env` directly via `scp` into the local file.
2. Encrypted password manager entry, copy/paste into the local `.env.local` once.

### 7b. Local `.env.local` shape

```
SENTINEL_API_KEY=<NEW_KEY>
SENTINEL_BASE_URL=https://sentinelrmm.us
```

The `SENTINEL_API_KEY` env var name matches what the recovery scripts will read after their hardcode is removed.

### 7c. Recovery script update (companion edit)

Three scripts currently hardcode the leaked key:
- `D:/Projects/Sentinel/scripts/recovery-steps.py`
- `D:/Projects/Sentinel/scripts/recovery-task.py`
- `D:/Projects/Sentinel/scripts/send-recovery-command.py`

Replace the hardcoded literal with:

```python
import os
API_KEY = os.environ["SENTINEL_API_KEY"]   # raises KeyError if unset — fail loudly
```

Loading order at runtime: caller is expected to source `.env.local` (PowerShell: `Get-Content .env.local | % { if ($_ -match '^([^=]+)=(.*)$') { [Environment]::SetEnvironmentVariable($Matches[1], $Matches[2]) } }`), or run under a wrapper that injects it.

The script edits are out of scope for this document — they live in the same Phase 4 PR as the auth middleware change. This section exists only to document the env-var contract the scripts depend on.

### 7d. Local file permissions

```powershell
icacls D:\Projects\Sentinel\.env.local /inheritance:r /grant:r "$env:USERNAME:(R,W)"
```

`.env.local` must never appear in `git status`. Verify:

```bash
cd D:/Projects/Sentinel && git check-ignore -v .env.local
# Expected: a line confirming .gitignore rule match
```

---

## Cross-references

- Auth middleware source: `server/internal/middleware/auth.go` (`loadAPIKeys`, `keyPrefix`, `AuthOrAPIKeyMiddleware`).
- Test coverage: `server/internal/middleware/auth_test.go` (overlap, constant-time, prefix-only logging).
- DB-side companion: `docs/rotation/migrate-shim-revocation.md`.
- SQL companion: `docs/rotation/revoke_legacy_api_key.up.sql` (apply post-Section 6d), `revoke_legacy_api_key.down.sql` (rollback).
- Top-level SOP: `CREDENTIAL_ROTATION_PLAN.md`.

---

## Related: rotating the Redis secret

The Redis secret does **not** follow the append-both-values pattern documented
above, because `requirepass` accepts exactly one value — there is no two-value
overlap window. It is a hard cutover, and it is automated:

```bash
ssh ohio_@192.168.1.20 'cd ~/Sentinel && bash scripts/rotate-redis-password.sh'
```

That script snapshots `.env` to `.env.pre-redis-rotate-<timestamp>`, mints a new
64-hex secret, rewrites `.env` atomically, regenerates `configs/redis/redis.conf`
(where `requirepass` now lives — see `configs/redis/README.md` for why it is no
longer on the argv or in the healthcheck), and recreates `redis` and `backend`
in a single `docker compose up`.

Two properties make the single-command form mandatory rather than convenient:

1. **Both services consume the value.** `redis` reads it from the generated
   config; `backend` receives it inside `REDIS_URL`. Naming only one service in
   `docker compose up -d` does not stop the other from recreating.
2. **The backend fails fast on a Redis auth error** (`log.Fatalf` in
   `server/cmd/sentinel/main.go`). A window where the two sides disagree is a
   backend crash loop, not a degraded mode. Recreating both together lets
   `depends_on: redis: condition: service_healthy` sequence the cutover.

Rollback is one file copy plus a recreate; the script prints the exact command,
and performs it automatically if any post-rotation check fails.

Verification only, no changes:

```bash
ssh ohio_@192.168.1.20 'cd ~/Sentinel && bash scripts/rotate-redis-password.sh --verify'
```
