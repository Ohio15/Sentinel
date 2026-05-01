# API_KEY Rotation Cutover Runbook

**Status**: Operational checklist. Phase 2 of the Credential Rotation Plan.
**Companion docs**:
- [`env-rotation-strategy.md`](./env-rotation-strategy.md) — overall strategy, why overlap, key formats
- [`cutover-rollback.md`](./cutover-rollback.md) — abort/rollback procedure if any verification fails
- [`migrate-shim-revocation.md`](./migrate-shim-revocation.md) — paired DB shim revocation (separate change, not part of this cutover)

**Branch / commit cut-over depends on**: `phase2-api-keys-overlap` @ `99121e7` (auth.go API_KEYS overlap patch — accepts comma-separated list, validates against any).

**Total estimated wall-clock**: ~15 min from T-15 to T+0. 48h watch window after.

**Conventions in this doc**:
- `<OLD_KEY>` = the currently-deployed `SENTINEL_API_KEY` value (the `55ccf1fd...` 64-hex string Ron has in `.env.local`).
- `<NEW_KEY>` = the freshly generated 64-hex string from T-15.
- Never paste real key material into terminal history, chat, ntfy, or commit messages.
- All NEXUS commands assume `ssh ohio_@192.168.1.20` is open in one window. Keep a second SSH session as backup in case the primary drops mid-cutover.

---

## T-30 min: Pre-flight

Pre-conditions that must ALL be true before proceeding. Do not start T-15 until every box is checked.

- [ ] **Branch merged + image built**
  - Verify on GitHub: `phase2-api-keys-overlap` is merged into `main`.
  - Verify Sentinel CI release pipeline produced a new `sentinel-backend` image.
  - Command (local PowerShell):
    ```powershell
    gh run list --repo Ohio15/Sentinel --workflow=release.yml --limit 5
    ```
  - The most recent run on `main` must be `completed` / `success` and post-merge of `99121e7`.
  - **If fails**: STOP. Do not proceed. Re-run pipeline, investigate failure. No cutover on a stale image.

- [ ] **Local `.env.local` has current key**
  - Command:
    ```powershell
    Select-String -Path "D:/Projects/Sentinel/.env.local" -Pattern "^SENTINEL_API_KEY="
    ```
  - Expected: one match, value = `<OLD_KEY>`.
  - **If fails**: locate the working key from current 1Password entry / current NEXUS `.env`. Do not generate a new one yet — that is T-15.

- [ ] **NEXUS reachable**
  - Command:
    ```powershell
    ssh ohio_@192.168.1.20 "uptime && docker ps --filter name=sentinel-backend --format '{{.Names}}\t{{.Status}}'"
    ```
  - Expected: uptime line + `sentinel-backend ... Up X minutes (healthy)`.
  - **If fails**: do not cutover. Resolve SSH/container health first. If Tailscale is the only path, retry via `ssh ohio_@100.98.48.63`.

- [ ] **Rollback runbook open in another window**
  - Open [`cutover-rollback.md`](./cutover-rollback.md) in a separate editor pane. Do NOT close it until T+48h.

---

## T-15 min: Generate the new key

Generate `<NEW_KEY>` and stage it in a session env var. Do not echo, do not log, do not ntfy.

- [ ] **Generate on NEXUS** (preferred — keeps the new value off the Windows clipboard ring):
  ```bash
  ssh ohio_@192.168.1.20
  # Inside the SSH session:
  read -s NEW_KEY < <(openssl rand -hex 32 | tr -d '\n' | tee /dev/null) ; echo
  # Equivalent simpler form if `read -s` feels fragile:
  export NEW_KEY="$(openssl rand -hex 32)"
  ```
  Note: the `export` form puts the value in the process env only; it never appears in `~/.bash_history` because there is no literal value typed.

- [ ] **Verify length**:
  ```bash
  echo -n "$NEW_KEY" | wc -c
  # Expected: 64
  echo -n "$NEW_KEY" | grep -E '^[0-9a-f]{64}$' >/dev/null && echo OK || echo FAIL
  # Expected: OK
  ```
  - **If fails (not 64 / not hex)**: regenerate. Do not proceed with a malformed key.

- [ ] **Stage in local PowerShell session as well** (for T+0 distribution):
  - On NEXUS, copy the value into a transient file readable only by `ohio_`:
    ```bash
    umask 077
    printf '%s' "$NEW_KEY" > /tmp/.nk-$$
    ls -la /tmp/.nk-$$
    ```
  - In a fresh local PowerShell window, pull it via `scp` into a session env var (do not write to disk locally):
    ```powershell
    $env:NEW_KEY = (ssh ohio_@192.168.1.20 "cat /tmp/.nk-*").Trim()
    if ($env:NEW_KEY.Length -ne 64) { Write-Error "NEW_KEY length wrong"; exit 1 }
    ```
  - Then on NEXUS: `shred -u /tmp/.nk-*`

- [ ] **Negative checks**:
  - Do NOT `echo $NEW_KEY` in any shared terminal.
  - Do NOT post `<NEW_KEY>` to the `nexus-alerts` ntfy topic, Slack, email, or any chat. Ntfy is in scope for the leak surface.
  - Do NOT commit any file containing the literal key.

---

## T-10 min: Stage the .env on NEXUS

NEXUS still SSH'd. `$NEW_KEY` is set in the SSH session.

- [ ] **Backup current .env**:
  ```bash
  cp ~/Sentinel/.env ~/Sentinel/.env.pre-rotate-$(date +%s)
  ls -la ~/Sentinel/.env.pre-rotate-*
  ```
  - Expected: file present, mode `-rw-------` (or matching the original).
  - **If fails**: STOP. Investigate filesystem / permissions before any further write.

- [ ] **Capture old key value into a session var (for the overlap line)**:
  ```bash
  OLD_KEY="$(grep -E '^SENTINEL_API_KEY=|^API_KEY=|^API_KEYS=' ~/Sentinel/.env | head -1 | cut -d= -f2-)"
  echo -n "$OLD_KEY" | wc -c   # Expected: 64
  ```
  - The variable name in the .env may be `API_KEY` (legacy single) or `API_KEYS` (post-patch list). The auth.go patch on `99121e7` reads `API_KEYS` first, falls back to `API_KEY`. Confirm against `env-rotation-strategy.md` §2.

- [ ] **Append-or-replace `API_KEYS` line with overlap**:
  ```bash
  # If API_KEYS line exists, replace it; if not, append it. Single perl invocation:
  perl -i -pe 'BEGIN{$found=0} if(/^API_KEYS=/){$_="API_KEYS=$ENV{OLD_KEY},$ENV{NEW_KEY}\n";$found=1} END{if(!$found){open(my $fh,">>","/home/ohio_/Sentinel/.env"); print $fh "API_KEYS=$ENV{OLD_KEY},$ENV{NEW_KEY}\n"; close $fh}}' ~/Sentinel/.env
  ```
  - The `BEGIN`/`END` trick: perl `-i -pe` only edits matched lines. The END block appends if no match was found.
  - Alternative if you prefer atomic edit-or-append in two steps:
    ```bash
    if grep -q '^API_KEYS=' ~/Sentinel/.env; then
        sed -i.bak "s|^API_KEYS=.*|API_KEYS=${OLD_KEY},${NEW_KEY}|" ~/Sentinel/.env
    else
        printf 'API_KEYS=%s,%s\n' "$OLD_KEY" "$NEW_KEY" >> ~/Sentinel/.env
    fi
    ```

- [ ] **Verify exactly two values in API_KEYS**:
  ```bash
  grep "^API_KEYS=" ~/Sentinel/.env | cut -d= -f2- | tr ',' '\n' | wc -l
  # Expected: 2
  grep "^API_KEYS=" ~/Sentinel/.env | cut -d= -f2- | tr ',' '\n' | awk '{print length($0)}'
  # Expected: two lines, each "64"
  ```
  - **If output != 2 or any length != 64**: restore from backup IMMEDIATELY and do not proceed:
    ```bash
    cp ~/Sentinel/.env.pre-rotate-<timestamp> ~/Sentinel/.env
    ```
    Then debug the perl/sed command before retrying.

- [ ] **Confirm no quoting / trailing whitespace surprises**:
  ```bash
  cat -A ~/Sentinel/.env | grep API_KEYS
  ```
  Expected ending: `,<64hex>$` (no `^M`, no trailing space before `$`).

---

## T-5 min: Restart sentinel-backend with overlap config

Container restart. This is the moment of truth for `auth.go @ 99121e7`.

- [ ] **Pull latest image and restart**:
  ```bash
  cd ~/Sentinel
  docker compose pull sentinel-backend
  docker compose up -d sentinel-backend
  ```
  - `pull` is idempotent. If CI tagged the new image as `:latest` or pinned digest in compose, `up -d` will recreate the container.

- [ ] **Wait 30s, then check health**:
  ```bash
  sleep 30
  docker inspect sentinel-backend --format '{{.State.Health.Status}}'
  ```
  - Expected: `healthy`.
  - **If `starting`**: wait another 15s and re-check. If still `starting` after 60s total, examine `docker logs --tail 100 sentinel-backend`.
  - **If `unhealthy`**: jump to [`cutover-rollback.md`](./cutover-rollback.md) §"Container unhealthy after restart".

- [ ] **4-curl smoke test** (run all four; record results before deciding):

  Determine the backend URL. Internal-to-NEXUS direct port (bypasses Traefik for raw verification):
  ```bash
  # From within NEXUS:
  BACKEND="http://localhost:8080"   # adjust if compose maps a different port
  ```

  1. **New key — must be 200**:
     ```bash
     curl -sS -o /dev/null -w "new_key=%{http_code}\n" \
       -H "X-API-Key: $NEW_KEY" \
       "$BACKEND/api/v1/health"
     ```
     Expected: `new_key=200`.

  2. **Old key — must be 200** (overlap window):
     ```bash
     curl -sS -o /dev/null -w "old_key=%{http_code}\n" \
       -H "X-API-Key: $OLD_KEY" \
       "$BACKEND/api/v1/health"
     ```
     Expected: `old_key=200`.

  3. **Garbage key — must be 401**:
     ```bash
     curl -sS -o /dev/null -w "garbage=%{http_code}\n" \
       -H "X-API-Key: deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef" \
       "$BACKEND/api/v1/health"
     ```
     Expected: `garbage=401`.

  4. **No auth header — must be 401**:
     ```bash
     curl -sS -o /dev/null -w "no_auth=%{http_code}\n" \
       "$BACKEND/api/v1/health"
     ```
     Expected: `no_auth=401`.

- [ ] **Decision gate**:
  - All four curls match expectations -> proceed to T+0.
  - ANY curl deviates (especially: new_key != 200, old_key != 200, garbage == 200) -> **STOP**. Open [`cutover-rollback.md`](./cutover-rollback.md) and execute the rollback procedure. Do not attempt forward fixes mid-cutover.

---

## T+0 min: Distribute new key

Backend now accepts both keys. Move clients to `<NEW_KEY>`.

- [ ] **Update local `.env.local`**:
  ```powershell
  # In a fresh PowerShell where $env:NEW_KEY is still set (from T-15):
  $envFile = "D:/Projects/Sentinel/.env.local"
  $content = Get-Content $envFile -Raw
  $updated = $content -replace '(?m)^SENTINEL_API_KEY=.*$', "SENTINEL_API_KEY=$($env:NEW_KEY)"
  Set-Content -Path $envFile -Value $updated -NoNewline
  Select-String -Path $envFile -Pattern "^SENTINEL_API_KEY="
  ```
  - Verify the displayed value's first 8 chars match the first 8 of `<NEW_KEY>` (mental check, no full-key echo).

- [ ] **Recovery scripts — IMPL-29 follow-up status**:
  - **Current state (verified at runbook authoring time, 2026-05-01)**: `scripts/recovery-steps.py:9`, `scripts/recovery-task.py:16`, and `scripts/send-recovery-command.py:16` each contain the literal hardcoded API_KEY (the rotated-out value `55ccf1fd...` — full literal redacted from this doc to keep it gitleaks-clean). The IMPL-29 refactor to read from `os.environ` has NOT yet landed.
  - **Required action this cutover**: MANUAL edit each file. Replace the hardcoded literal with `<NEW_KEY>`. This is debt — it should be `os.environ["SENTINEL_API_KEY"]` per IMPL-29; flag back to the supervisor that the refactor is still owed.
  - Commands (local):
    ```powershell
    # Repeat for each of the three files:
    code "D:/Projects/Sentinel/scripts/recovery-steps.py"
    code "D:/Projects/Sentinel/scripts/recovery-task.py"
    code "D:/Projects/Sentinel/scripts/send-recovery-command.py"
    ```
    Edit line 9 / 16 / 16 respectively. Do NOT commit these edits — the leaked old key is still in git history; replacing one literal with another doesn't fix the underlying class of bug. The IMPL-29 refactor will land separately.
  - **If IMPL-29 has already landed by the time of cutover**: confirm scripts read `os.environ["SENTINEL_API_KEY"]`, then simply update `.env.local` (already done above) — no code edit needed.

- [ ] **Smoke-test recovery flow**:
  ```powershell
  # Pick the lightest-touch script — `recovery-steps.py` typically just queries.
  # From a shell where SENTINEL_API_KEY=<NEW_KEY> is exported (or the file edits are saved):
  python "D:/Projects/Sentinel/scripts/recovery-steps.py" --help
  # Then a real read-only call against staging/dev recovery endpoint per the script's normal usage.
  ```
  - Expected: 200 / non-error path.
  - **If 401**: the script is hitting the backend with the wrong key. Re-check the literal you edited (or the env var if IMPL-29 landed).

---

## T+0 to T+48h: Watch

Monitor for residual old-key usage. The 48h window starts at the moment T+0 completes.

- [ ] **Tail backend logs for auth rejections by prefix**:
  ```bash
  ssh ohio_@192.168.1.20 \
    "docker logs -f --tail 0 sentinel-backend 2>&1 | grep -E 'auth: rejected key|auth: accepted key'"
  ```
  - The auth.go patch logs the first 8 hex chars as `prefix=<XXXXXXXX>` on both accept and reject paths (per `env-rotation-strategy.md` §3 — log format). Use the first 8 chars of `<OLD_KEY>` and `<NEW_KEY>` to distinguish.
  - Expected during overlap:
    - `accepted key prefix=<new8>` — increasing (new clients).
    - `accepted key prefix=<old8>` — should decay toward zero over 48h.
    - `rejected key prefix=<old8>` — should NOT appear during overlap (because old key still accepted). If it does appear, something is wrong with the patch.
    - `rejected key prefix=<new8>` — should be zero.

- [ ] **Configure ntfy alert on first old-key reject AFTER cutover**:
  - This catches the `T+48h` drop misfire. Until T+48h, old-key is *accepted*; if it gets *rejected* before T+48h, the patch broke.
  - On NEXUS, add a one-shot watcher (kill before T+48h drop):
    ```bash
    nohup bash -c '
      docker logs -f --tail 0 sentinel-backend 2>&1 \
        | grep --line-buffered "rejected key prefix='"${OLD_KEY:0:8}"'" \
        | head -1 \
        | xargs -I{} curl -d "First post-cutover old-key rejection seen: {}" https://ntfy.sh/nexus-alerts
    ' >/tmp/old-key-watch.log 2>&1 &
    echo "watcher pid: $!"
    ```
    Save the PID. Kill it at T+48h before dropping the old key (otherwise it fires on the intentional drop).

- [ ] **Daily pulse during watch window**:
  - At T+24h: count old-key accepts in last 24h:
    ```bash
    docker logs --since 24h sentinel-backend 2>&1 \
      | grep -c "accepted key prefix=${OLD_KEY:0:8}"
    ```
  - Expected: trending toward zero. If still high (>10/hr), investigate which client is still on the old key before dropping.

- [ ] **Watch window end calculation**:
  - If T+0 = `2026-05-01 17:00 UTC`, T+48h = `2026-05-03 17:00 UTC`.
  - Adjust if cutover occurs at a different time. Do NOT drop the old key before the full 48h — even if traffic looks zero.

---

## T+48h: Drop old key

Watch window expired. Old key has been silent (or any remaining client found and migrated).

- [ ] **Kill the ntfy watcher** from the watch step:
  ```bash
  ps -ef | grep "rejected key prefix=" | grep -v grep
  kill <pid>
  ```

- [ ] **Backup .env again** (separate from pre-rotate backup):
  ```bash
  cp ~/Sentinel/.env ~/Sentinel/.env.pre-drop-$(date +%s)
  ```

- [ ] **Edit API_KEYS to leave only `<NEW_KEY>`**:
  ```bash
  # NEW_KEY may not be in env at T+48h. Recover from the .env if needed:
  CURRENT_NEW_KEY="$(grep '^API_KEYS=' ~/Sentinel/.env | cut -d= -f2- | tr ',' '\n' | tail -1)"
  echo -n "$CURRENT_NEW_KEY" | wc -c   # Expected: 64
  sed -i.bak "s|^API_KEYS=.*|API_KEYS=${CURRENT_NEW_KEY}|" ~/Sentinel/.env

  # Verify:
  grep "^API_KEYS=" ~/Sentinel/.env | cut -d= -f2- | tr ',' '\n' | wc -l
  # Expected: 1
  ```

- [ ] **Restart backend**:
  ```bash
  cd ~/Sentinel && docker compose up -d sentinel-backend
  sleep 30
  docker inspect sentinel-backend --format '{{.State.Health.Status}}'
  # Expected: healthy
  ```

- [ ] **Verify old key now 401s**:
  ```bash
  # OLD_KEY env var is likely gone. Recover from the .env.pre-rotate-* backup:
  OLD_KEY="$(grep '^API_KEYS=' ~/Sentinel/.env.pre-rotate-* | head -1 | cut -d= -f2- | tr ',' '\n' | head -1)"
  curl -sS -o /dev/null -w "%{http_code}\n" \
    -H "X-API-Key: $OLD_KEY" \
    http://localhost:8080/api/v1/health
  # Expected: 401
  ```
  - **If 200**: the .env edit didn't take effect, or compose didn't reload env. Check container env: `docker exec sentinel-backend env | grep API_KEYS`. If still shows old+new, force-recreate: `docker compose up -d --force-recreate sentinel-backend`.

- [ ] **Verify new key still 200s**:
  ```bash
  curl -sS -o /dev/null -w "%{http_code}\n" \
    -H "X-API-Key: $CURRENT_NEW_KEY" \
    http://localhost:8080/api/v1/health
  # Expected: 200
  ```

- [ ] **Cleanup pre-rotate backup** (after a final 7-day grace — not on cutover day):
  ```bash
  # ON OR AFTER 2026-05-10 only:
  shred -u ~/Sentinel/.env.pre-rotate-* ~/Sentinel/.env.pre-drop-*
  ```
  Until then, the backups exist as a recovery anchor.

- [ ] **Mark rotation complete** in the credential rotation tracker. Open follow-ups:
  - [ ] IMPL-29 refactor of recovery scripts to use `os.environ` (still owed).
  - [ ] Migrate-shim DB revocation per [`migrate-shim-revocation.md`](./migrate-shim-revocation.md) (separate work, separate cutover).
  - [ ] Git-history scrub of the leaked `<OLD_KEY>` literal across the three recovery scripts (BFG / `git filter-repo`).

---

## Quick reference: phase headers

1. T-30 min: Pre-flight
2. T-15 min: Generate the new key
3. T-10 min: Stage the .env on NEXUS
4. T-5 min: Restart sentinel-backend with overlap config
5. T+0 min: Distribute new key
6. T+0 to T+48h: Watch
7. T+48h: Drop old key
