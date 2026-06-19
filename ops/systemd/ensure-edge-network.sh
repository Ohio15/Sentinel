#!/usr/bin/env bash
#
# ensure-edge-network.sh
#
# Reconnect critical containers to the external 'edge' Docker network after boot.
#
# Why this exists:
#   On the 2026-06-18 17:20 host reboot, dockerd restored sentinel-backend onto
#   only its internal 'sentinel_sentinel-network' and silently skipped the 'edge'
#   endpoint (frontend got both, backend got one). With no edge attachment,
#   infra-traefik could not resolve 'sentinel-backend' (NXDOMAIN) and returned
#   502 for every /api route -> sentinelrmm.us API down for ~22h until manually
#   reconnected. This is a known Docker race restoring external-network endpoints
#   on daemon restart. This unit deterministically re-attaches the affected
#   containers on every boot, regardless of the race outcome.
#
# Idempotent: skips containers already on edge. Safe to run repeatedly.

set -u

# Containers that MUST be on the edge network (they declare it in compose and are
# routed by infra-traefik via container DNS name). Add others here if needed.
CONTAINERS="sentinel-backend sentinel-frontend sentinel-grafana sentinel-prometheus sentinel-alertmanager"

log() { echo "[ensure-edge-network] $*"; }

for c in $CONTAINERS; do
  # Wait for the container to be running (restart policy may lag behind dockerd).
  running=false
  for _ in $(seq 1 30); do
    if [ "$(docker inspect -f '{{.State.Running}}' "$c" 2>/dev/null)" = "true" ]; then
      running=true
      break
    fi
    sleep 2
  done

  if [ "$running" != "true" ]; then
    log "SKIP $c — not running after wait"
    continue
  fi

  if docker inspect -f '{{json .NetworkSettings.Networks}}' "$c" 2>/dev/null | grep -q '"edge"'; then
    log "OK   $c already on edge"
    continue
  fi

  if docker network connect edge "$c" 2>/dev/null; then
    log "FIX  $c reconnected to edge"
  else
    log "WARN $c failed to connect to edge"
  fi
done

exit 0
