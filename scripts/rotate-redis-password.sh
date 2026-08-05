#!/usr/bin/env bash
#
# rotate-redis-password.sh
# -----------------------------------------------------------------------------
# Rotate REDIS_PASSWORD on the deploy host, atomically for both consumers, with
# automatic rollback if anything fails.
#
# WHY IT IS ONE SCRIPT
# --------------------
# REDIS_PASSWORD feeds TWO services: `redis` (via the generated config file) and
# `backend` (via REDIS_URL in its environment). The backend does a fail-fast
# Ping at boot (server/cmd/sentinel/main.go: log.Fatalf on Redis connect error),
# so a window where one side has the old secret and the other the new one is not
# a degraded state, it is a crash loop. Both containers are therefore recreated
# in a single `docker compose up`, which also lets compose honour
# `depends_on: redis: condition: service_healthy` and bring the backend up only
# after Redis is healthy on the NEW password.
#
# WHAT IS AFFECTED
# ----------------
# Redis holds only ephemeral, TTL'd state (agent-location keys, command/response
# streams, one-shot uninstall/kill tokens, recording-id cache, rate-limit
# counters). Postgres is the system of record. `appendonly yes` on the named
# volume redis-data means the dataset survives a recreate anyway. The real cost
# of this operation is the backend restart: agents reconnect and the API is
# unavailable for the length of the backend's boot.
#
# SECRET HYGIENE
# --------------
# The password is never placed on any process argv (bash builtins and env vars
# only), never echoed, and never written anywhere except .env (mode 0600) and
# the generated redis.conf (mode 0440 root:redis). Only sha256 prefixes are
# printed.
#
# Usage:
#   bash scripts/rotate-redis-password.sh            # rotate
#   bash scripts/rotate-redis-password.sh --verify   # verify current state only
# -----------------------------------------------------------------------------
set -euo pipefail
cd "$(dirname "$0")/.."
PROJECT_DIR="$(pwd)"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[rotate]${NC} $*"; }
warn()  { echo -e "${YELLOW}[rotate]${NC} $*"; }
err()   { echo -e "${RED}[rotate]${NC} $*" >&2; }
fatal() { err "FATAL: $*"; exit 1; }

ENV_FILE="$PROJECT_DIR/.env"
VERIFY_ONLY=0
[[ "${1:-}" == "--verify" ]] && VERIFY_ONLY=1

fp() { printf '%s' "$1" | sha256sum | cut -c1-12; }   # fingerprint, never the value

# --- functional verification, all evidence taken at the observable boundary ---

verify_stack() {
  local failures=0

  # 1. The secret must not be in the recorded argv.
  if docker inspect sentinel-redis --format '{{json .Config.Cmd}}' | grep -q 'requirepass'; then
    err "CHECK 1 FAIL: Config.Cmd still carries requirepass"; failures=1
  else
    info "CHECK 1 ok: Config.Cmd carries no requirepass"
  fi

  # 2. The secret must not be in the recorded healthcheck.
  if docker inspect sentinel-redis --format '{{json .Config.Healthcheck.Test}}' | grep -qE '\-a", "[0-9a-f]{32}'; then
    err "CHECK 2 FAIL: healthcheck still carries a literal secret"; failures=1
  else
    info "CHECK 2 ok: healthcheck carries no literal secret"
  fi

  # 3. Redis accepts the password that is actually in the config file. Read
  #    inside the container so the value never crosses the host boundary.
  if docker exec sentinel-redis sh -c \
      'REDISCLI_AUTH=$(sed -n "s/^requirepass //p" /usr/local/etc/redis/redis.conf) redis-cli ping' \
      2>/dev/null | grep -q PONG; then
    info "CHECK 3 ok: Redis accepts the configured password"
  else
    err "CHECK 3 FAIL: Redis does not accept the configured password"; failures=1
  fi

  # 4. Redis rejects a password that exists nowhere.
  if docker exec sentinel-redis sh -c \
      'REDISCLI_AUTH=not-a-real-password-only-used-to-prove-auth-is-enforced redis-cli ping' \
      2>&1 | grep -qi 'WRONGPASS\|invalid'; then
    info "CHECK 4 ok: Redis rejects a wrong password"
  else
    err "CHECK 4 FAIL: Redis did not reject a wrong password (is auth on?)"; failures=1
  fi

  # 5. Container health.
  local rh bh
  rh="$(docker inspect sentinel-redis --format '{{.State.Health.Status}}' 2>/dev/null || echo unknown)"
  bh="$(docker inspect sentinel-backend --format '{{.State.Health.Status}}' 2>/dev/null || echo unknown)"
  if [[ "$rh" == healthy ]]; then info "CHECK 5a ok: sentinel-redis healthy"; else err "CHECK 5a FAIL: sentinel-redis=$rh"; failures=1; fi
  if [[ "$bh" == healthy ]]; then info "CHECK 5b ok: sentinel-backend healthy"; else err "CHECK 5b FAIL: sentinel-backend=$bh"; failures=1; fi

  # 6. The backend is really talking to Redis, not merely running. The backend
  #    log.Fatalf's on a failed Redis ping at boot, so a healthy backend that
  #    booted after the cutover has authenticated; corroborate with a live
  #    client connection on the Redis side.
  local clients
  clients="$(docker exec sentinel-redis sh -c \
    'REDISCLI_AUTH=$(sed -n "s/^requirepass //p" /usr/local/etc/redis/redis.conf) redis-cli info clients' \
    2>/dev/null | tr -d '\r' | sed -n 's/^connected_clients://p')"
  if [[ "${clients:-0}" -ge 1 ]]; then
    info "CHECK 6 ok: Redis reports $clients connected client(s)"
  else
    err "CHECK 6 FAIL: no clients connected to Redis"; failures=1
  fi

  # 7. No Redis auth failures in the backend log since it came up.
  if docker logs sentinel-backend --since 10m 2>&1 \
      | sed 's#redis://[^@]*@#redis://<redacted>@#g' \
      | grep -qi 'failed to connect to redis\|NOAUTH\|WRONGPASS\|RATE-LIMIT. Redis unavailable'; then
    err "CHECK 7 FAIL: backend log shows Redis connectivity/auth problems"; failures=1
  else
    info "CHECK 7 ok: backend log clean of Redis auth/connectivity errors"
  fi

  return $failures
}

if [[ "$VERIFY_ONLY" == 1 ]]; then
  verify_stack
  exit $?
fi

# ------------------------------- rotation ------------------------------------

[[ -f "$ENV_FILE" ]] || fatal "$ENV_FILE not found."
command -v openssl >/dev/null || fatal "openssl is required to mint the new secret."

set -a; # shellcheck disable=SC1090
source "$ENV_FILE"; set +a
OLD_PASSWORD="${REDIS_PASSWORD:-}"
[[ -n "$OLD_PASSWORD" ]] || fatal "REDIS_PASSWORD is not set in $ENV_FILE; this script rotates, it does not bootstrap."

BACKUP="$ENV_FILE.pre-redis-rotate-$(date +%Y%m%d-%H%M%S)"
cp -p "$ENV_FILE" "$BACKUP"
chmod 600 "$BACKUP"
info "backed up .env -> $BACKUP"
info "old secret fingerprint: sha256:$(fp "$OLD_PASSWORD")"

NEW_PASSWORD="$(openssl rand -hex 32)"
[[ "$NEW_PASSWORD" =~ ^[0-9a-f]{64}$ ]] || fatal "generated secret has the wrong shape."
info "new secret fingerprint: sha256:$(fp "$NEW_PASSWORD")"

rollback() {
  err "ROLLING BACK to $BACKUP"
  cp -p "$BACKUP" "$ENV_FILE"
  bash "$PROJECT_DIR/scripts/generate-redis-conf.sh" || err "rollback: config regeneration failed"
  docker compose up -d --force-recreate redis backend || err "rollback: recreate failed"
  err "rollback complete; stack is back on the previous secret."
}

# Rewrite .env atomically. ENVIRON is used rather than an awk -v argument so the
# secret never appears in the host process table.
TMP_ENV="$(mktemp "$PROJECT_DIR/.env.rotate.XXXXXX")"
chmod 600 "$TMP_ENV"
if ! NEW_PW="$NEW_PASSWORD" awk '
      BEGIN { v = ENVIRON["NEW_PW"]; done = 0 }
      /^REDIS_PASSWORD=/ { print "REDIS_PASSWORD=" v; done = 1; next }
      { print }
      END { if (!done) { print "REDIS_PASSWORD=" v } }
    ' "$ENV_FILE" >"$TMP_ENV"; then
  rm -f "$TMP_ENV"; fatal "failed to rewrite .env; nothing changed."
fi
# Sanity: the rewrite must not have lost lines.
if [[ "$(wc -l <"$TMP_ENV")" -lt "$(wc -l <"$ENV_FILE")" ]]; then
  rm -f "$TMP_ENV"; fatal ".env rewrite lost lines; aborting before any change."
fi
mv -f "$TMP_ENV" "$ENV_FILE"
chmod 600 "$ENV_FILE"
info ".env updated"

CUTOVER_START="$(date +%s)"

if ! bash "$PROJECT_DIR/scripts/generate-redis-conf.sh"; then
  rollback; fatal "config generation failed."
fi

# --force-recreate is REQUIRED for redis: after this change the redis service no
# longer interpolates REDIS_PASSWORD, so its compose config hash does not move
# when .env changes and plain `up -d` would leave the old container running with
# the old password. The backend recreates on its own (its config hash does move),
# but naming it here keeps both cutovers inside one compose run.
if ! docker compose up -d --force-recreate redis backend; then
  rollback; fatal "recreate failed."
fi

info "waiting for both containers to report healthy..."
deadline=$(( $(date +%s) + 180 ))
while :; do
  rh="$(docker inspect sentinel-redis   --format '{{.State.Health.Status}}' 2>/dev/null || echo unknown)"
  bh="$(docker inspect sentinel-backend --format '{{.State.Health.Status}}' 2>/dev/null || echo unknown)"
  [[ "$rh" == healthy && "$bh" == healthy ]] && break
  if (( $(date +%s) > deadline )); then
    err "timed out waiting for health (redis=$rh backend=$bh)"
    rollback; fatal "cutover did not converge."
  fi
  sleep 3
done
CUTOVER_END="$(date +%s)"
info "both healthy after $((CUTOVER_END - CUTOVER_START))s"

if ! verify_stack; then
  rollback; fatal "post-rotation verification failed."
fi

# The old secret must be dead. Piped over stdin so it never touches an argv.
if printf '%s\n' "$OLD_PASSWORD" | docker exec -i sentinel-redis sh -c \
     'read -r p; REDISCLI_AUTH="$p" redis-cli ping' 2>&1 | grep -q PONG; then
  err "CHECK 8 FAIL: the OLD password still authenticates."
  rollback; fatal "rotation did not take effect."
fi
info "CHECK 8 ok: the old password no longer authenticates"

echo
info "ROTATION COMPLETE"
info "  old: sha256:$(fp "$OLD_PASSWORD")   (now dead)"
info "  new: sha256:$(fp "$NEW_PASSWORD")"
info "  interruption: $((CUTOVER_END - CUTOVER_START))s"
info "  backup:  $BACKUP"
warn "  rollback: cp -p '$BACKUP' '$ENV_FILE' && bash scripts/generate-redis-conf.sh && docker compose up -d --force-recreate redis backend"
warn "  Store the new secret in 1Password and confirm before deleting the backup."
