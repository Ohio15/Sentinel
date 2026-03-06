#!/bin/bash
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-/var/backups/sentinel}"
RETENTION_DAYS="${RETENTION_DAYS:-30}"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
COMPOSE_FILE="${COMPOSE_FILE:-/home/ohio_/Sentinel/docker-compose.yml}"

mkdir -p "$BACKUP_DIR"

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"; }

log "Starting Sentinel backup..."

# PostgreSQL
log "Backing up PostgreSQL..."
docker compose -f "$COMPOSE_FILE" exec -T postgres \
    pg_dump -U "${POSTGRES_USER:-sentinel}" "${POSTGRES_DB:-sentinel}" \
    | gzip > "$BACKUP_DIR/postgres_${TIMESTAMP}.sql.gz"
log "PostgreSQL backup: $(du -h "$BACKUP_DIR/postgres_${TIMESTAMP}.sql.gz" | cut -f1)"

# Redis
log "Backing up Redis..."
docker compose -f "$COMPOSE_FILE" exec -T redis \
    redis-cli -a "${REDIS_PASSWORD:-redis_dev_password}" BGSAVE 2>/dev/null
sleep 3
docker cp sentinel-redis:/data/dump.rdb "$BACKUP_DIR/redis_${TIMESTAMP}.rdb" 2>/dev/null || log "Redis backup skipped (no data)"

# Certificates
log "Backing up certificates..."
SENTINEL_DIR="$(dirname "$COMPOSE_FILE")"
tar -czf "$BACKUP_DIR/certs_${TIMESTAMP}.tar.gz" -C "$SENTINEL_DIR" certs/ 2>/dev/null || log "Cert backup skipped"

# Cleanup old backups
log "Cleaning backups older than ${RETENTION_DAYS} days..."
find "$BACKUP_DIR" -type f -mtime +${RETENTION_DAYS} -delete 2>/dev/null

log "Backup complete. Total size: $(du -sh "$BACKUP_DIR" | cut -f1)"
