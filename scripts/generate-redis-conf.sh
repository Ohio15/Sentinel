#!/usr/bin/env bash
#
# generate-redis-conf.sh
# -----------------------------------------------------------------------------
# Render configs/redis/redis.conf on the deploy host from .env.
#
# WHY THIS EXISTS
# ---------------
# Redis' password used to be passed as `--requirepass <secret>` on the argv and
# as `redis-cli -a <secret>` in the healthcheck. Both are recorded by Docker and
# republished forever:
#   * argv        -> the container's Config.Cmd, readable by anyone with the
#                    Docker socket (`docker inspect sentinel-redis`).
#   * healthcheck -> the `exec_create` / `exec_start` Actions in the Docker
#                    EVENT STREAM, re-emitted every `interval` (10s), to every
#                    consumer (infra-traefik consumes it continuously).
# Moving requirepass into a config file removes both: the argv becomes
# `redis-server /usr/local/etc/redis/redis.conf` and the healthcheck reads the
# secret at runtime from that file.
#
# The rendered file therefore CONTAINS A SECRET and is NOT in git
# (configs/redis/redis.conf is gitignored). It must be regenerated on each host
# whenever REDIS_PASSWORD changes. Note that the NEXUS deploy tree is force-synced
# by .github/workflows/deploy-web.yml with `git reset --hard origin/main`, which
# preserves untracked files — so a gitignored generated file survives deploys,
# but a tracked one would be clobbered. Keep it gitignored.
#
# PERMISSIONS
# -----------
# 0440 root:<redis gid>, NOT 0400 root:root. The redis image's entrypoint drops
# privileges (gosu/setpriv) to the `redis` user before exec'ing redis-server, so
# a root-only-readable config would make the server fail to start. 0440 with the
# group set to the image's redis gid is the least privilege that actually works.
# The healthcheck can read it regardless: the image sets no USER, so `docker exec`
# (and therefore healthchecks) run as root.
#
# Exit 0 = rendered. Non-zero = nothing written (fails closed).
# Run locally:  bash scripts/generate-redis-conf.sh
# -----------------------------------------------------------------------------
set -euo pipefail
cd "$(dirname "$0")/.."
PROJECT_DIR="$(pwd)"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[redis-conf]${NC} $*"; }
warn()  { echo -e "${YELLOW}[redis-conf]${NC} $*"; }
fatal() { echo -e "${RED}[redis-conf] FATAL:${NC} $*" >&2; exit 1; }

ENV_FILE="${ENV_FILE:-$PROJECT_DIR/.env}"
CONF_DIR="${REDIS_CONF_DIR:-$PROJECT_DIR/configs/redis}"
CONF_FILE="$CONF_DIR/redis.conf"

[[ -f "$ENV_FILE" ]] || fatal "$ENV_FILE not found. Copy .env.production.template and fill it in."

# Read REDIS_PASSWORD without echoing it and without ever placing it on an argv.
# Plain `source`, NOT `set -a`: exporting it would hand the secret to every
# child process this script spawns (docker run/inspect below), and an exported
# REDIS_PASSWORD outlives its accuracy — docker compose lets OS environment win
# over .env, which is exactly how the first rotation attempt crash-looped the
# backend on a stale password (2026-08-05).
# shellcheck disable=SC1090
source "$ENV_FILE"

[[ -n "${REDIS_PASSWORD:-}" ]] || fatal "REDIS_PASSWORD is unset or empty in $ENV_FILE. Refusing to render a passwordless Redis."

# Shape check. The rotation script mints 64 hex chars; anything shorter is very
# likely a leftover placeholder (e.g. the old compose default) and must not ship.
if [[ ! "$REDIS_PASSWORD" =~ ^[0-9a-f]{64}$ ]]; then
  fatal "REDIS_PASSWORD is not 64 lowercase hex characters. Generate one with scripts/rotate-redis-password.sh."
fi

# A '#' or leading whitespace would break the healthcheck's
# `sed -n 's/^requirepass //p'` extraction, and a newline would break the file.
if [[ "$REDIS_PASSWORD" == *[$'\n\r \t']* ]]; then
  fatal "REDIS_PASSWORD contains whitespace; refusing to render an ambiguous config."
fi

# Resolve the redis gid from the actual image rather than hardcoding it, so an
# image bump that changes the uid/gid fails loudly here instead of as a
# mysterious "can't open config file" crash loop.
REDIS_IMAGE="$(docker inspect --format '{{.Config.Image}}' sentinel-redis 2>/dev/null || true)"
if [[ -z "$REDIS_IMAGE" ]]; then
  REDIS_IMAGE="$(docker compose config --images 2>/dev/null | sed -n '/^redis:/p' | head -1 || true)"
fi
[[ -n "$REDIS_IMAGE" ]] || fatal "Could not determine the redis image (container absent and 'docker compose config --images' gave nothing)."

REDIS_GID="$(docker run --rm --entrypoint id "$REDIS_IMAGE" -g redis 2>/dev/null || true)"
[[ "$REDIS_GID" =~ ^[0-9]+$ ]] || fatal "Could not resolve the 'redis' gid inside $REDIS_IMAGE."
info "image=$REDIS_IMAGE redis gid=$REDIS_GID"

mkdir -p "$CONF_DIR"

# Render into a temp file IN THE TARGET DIRECTORY so the final move is an atomic
# same-filesystem rename: readers (redis, the healthcheck) never observe a
# partially written config.
umask 077
TMP_FILE="$(mktemp "$CONF_DIR/.redis.conf.XXXXXX")"
cleanup() { [[ -e "${TMP_FILE:-}" ]] && rm -f "$TMP_FILE"; }
trap cleanup EXIT

# bash's printf is a builtin, so the secret never appears on any process argv.
{
  printf '%s\n' '# GENERATED FILE - DO NOT EDIT, DO NOT COMMIT.'
  printf '%s\n' '# Rendered by scripts/generate-redis-conf.sh from .env on the deploy host.'
  printf '%s\n' '# Contains the Redis password: it lives here instead of on the redis-server'
  printf '%s\n' '# argv so it stays out of Config.Cmd and out of the Docker event stream.'
  printf '%s\n' '# Regenerate after every REDIS_PASSWORD change, then recreate the container.'
  printf '%s\n' '#'
  printf '%s\n' '# These four directives are the exact settings that were previously passed as'
  printf '%s\n' '# command-line flags; do not drop one when editing the generator.'
  printf '%s\n' 'appendonly yes'
  printf '%s\n' 'maxmemory 256mb'
  printf '%s\n' 'maxmemory-policy allkeys-lru'
  # Must be the ONLY line starting with "requirepass": the compose healthcheck
  # extracts it with `sed -n 's/^requirepass //p'`.
  printf 'requirepass %s\n' "$REDIS_PASSWORD"
} >"$TMP_FILE"

sudo chown "root:$REDIS_GID" "$TMP_FILE"
sudo chmod 0440 "$TMP_FILE"
sudo mv -f "$TMP_FILE" "$CONF_FILE"
trap - EXIT

info "wrote $CONF_FILE ($(stat -c '%a %U:%G' "$CONF_FILE"))"
info "requirepass fingerprint: sha256:$(printf '%s' "$REDIS_PASSWORD" | sha256sum | cut -c1-12)"
warn "The running container does NOT pick this up until it is recreated:"
warn "  docker compose up -d --force-recreate redis"
