#!/bin/bash
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-/var/backups/sentinel}"
TIMESTAMP="${1:?Usage: $0 <backup_timestamp> (e.g., 20260306_020000)}"
COMPOSE_FILE="${COMPOSE_FILE:-/home/ohio_/Sentinel/docker-compose.yml}"

PG_BACKUP="$BACKUP_DIR/postgres_${TIMESTAMP}.sql.gz"

if [ ! -f "$PG_BACKUP" ]; then
    echo "ERROR: Backup not found: $PG_BACKUP"
    echo "Available backups:"
    ls -lh "$BACKUP_DIR"/postgres_*.sql.gz 2>/dev/null || echo "  None found"
    exit 1
fi

echo "WARNING: This will REPLACE the database with backup from ${TIMESTAMP}"
echo "Backup size: $(du -h "$PG_BACKUP" | cut -f1)"
read -p "Press Enter to continue or Ctrl+C to abort..."

echo "Restoring PostgreSQL..."
gunzip -c "$PG_BACKUP" | docker compose -f "$COMPOSE_FILE" exec -T postgres \
    psql -U "${POSTGRES_USER:-sentinel}" "${POSTGRES_DB:-sentinel}"

echo "Restarting backend..."
docker compose -f "$COMPOSE_FILE" restart backend

echo "Restore complete. Verify: curl -s http://localhost:8080/health"
