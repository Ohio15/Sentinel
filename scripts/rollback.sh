#!/bin/bash
# Sentinel RMM - Quick Rollback Script
# Restores a previous deployment from backup
#
# Usage:
#   ./rollback.sh                           # Rollback to most recent backup
#   ./rollback.sh backup-20260118-120000    # Rollback to specific backup
#   ./rollback.sh --list                    # List available backups

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BACKUP_DIR="$PROJECT_DIR/backups"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# List available backups
list_backups() {
    echo ""
    echo "Available Backups:"
    echo "-----------------------------------------------------------"

    # List Docker image backups
    echo ""
    echo "Docker Images:"
    docker images sentinel-backend --format "  {{.Tag}}" | grep "^  backup-" | sort -r || echo "  No backups found"

    # List database backups
    echo ""
    echo "Database Dumps:"
    if [ -d "$BACKUP_DIR" ]; then
        ls -la "$BACKUP_DIR"/db-backup-*.sql 2>/dev/null | awk '{print "  " $NF}' || echo "  No backups found"
    else
        echo "  No backup directory found"
    fi

    # Show most recent backup
    if [ -f "$BACKUP_DIR/.latest_backup" ]; then
        echo ""
        echo "Most Recent Backup:"
        echo "  $(cat "$BACKUP_DIR/.latest_backup")"
    fi

    echo ""
}

# Show help
show_help() {
    echo "Sentinel RMM - Rollback Script"
    echo ""
    echo "Usage:"
    echo "  ./rollback.sh                           # Rollback to most recent backup"
    echo "  ./rollback.sh backup-20260118-120000    # Rollback to specific backup"
    echo "  ./rollback.sh --list                    # List available backups"
    echo ""
}

# Handle arguments
BACKUP_TAG=""
case "${1:-}" in
    --list|-l)
        list_backups
        exit 0
        ;;
    --help|-h)
        show_help
        exit 0
        ;;
    "")
        # Use most recent backup
        if [ -f "$BACKUP_DIR/.latest_backup" ]; then
            BACKUP_TAG=$(cat "$BACKUP_DIR/.latest_backup")
        else
            log_error "No backup specified and no recent backup found"
            echo ""
            list_backups
            exit 1
        fi
        ;;
    *)
        BACKUP_TAG="$1"
        ;;
esac

echo ""
echo "==========================================================="
echo "  SENTINEL RMM - ROLLBACK"
echo "==========================================================="
echo ""
echo "  Rolling back to: $BACKUP_TAG"
echo ""
echo "==========================================================="

cd "$PROJECT_DIR"

# Verify backup exists
BACKEND_BACKUP=$(docker images sentinel-backend --format "{{.Tag}}" | grep "^$BACKUP_TAG$" || true)
if [ -z "$BACKEND_BACKUP" ]; then
    log_error "Backup not found: $BACKUP_TAG"
    echo ""
    list_backups
    exit 1
fi

# ==============================================================
# STEP 1: Stop Current Containers
# ==============================================================
echo ""
echo -e "${YELLOW}[STEP 1/4] Stopping current containers...${NC}"
echo "-----------------------------------------------------------"

log_info "Stopping services..."
docker-compose down 2>&1 || true
log_success "Services stopped"

# ==============================================================
# STEP 2: Restore Docker Images
# ==============================================================
echo ""
echo -e "${YELLOW}[STEP 2/4] Restoring Docker images...${NC}"
echo "-----------------------------------------------------------"

log_info "Restoring backend image..."
docker tag sentinel-backend:$BACKUP_TAG sentinel-backend:latest
log_success "Backend image restored"

if docker images sentinel-frontend:$BACKUP_TAG --format "{{.Tag}}" | grep -q "$BACKUP_TAG"; then
    log_info "Restoring frontend image..."
    docker tag sentinel-frontend:$BACKUP_TAG sentinel-frontend:latest
    log_success "Frontend image restored"
else
    log_warn "No frontend backup found for $BACKUP_TAG"
fi

# ==============================================================
# STEP 3: Restore Database (Optional)
# ==============================================================
echo ""
echo -e "${YELLOW}[STEP 3/4] Checking database backup...${NC}"
echo "-----------------------------------------------------------"

DB_BACKUP="$BACKUP_DIR/db-$BACKUP_TAG.sql"
if [ -f "$DB_BACKUP" ]; then
    echo ""
    echo -e "${YELLOW}Database backup found: $DB_BACKUP${NC}"
    echo ""
    read -p "Do you want to restore the database? [y/N] " -n 1 -r
    echo ""

    if [[ $REPLY =~ ^[Yy]$ ]]; then
        log_info "Starting PostgreSQL for restore..."
        docker-compose up -d postgres
        sleep 10

        log_info "Restoring database..."
        # Drop and recreate database
        docker exec sentinel-postgres psql -U sentinel -c "DROP DATABASE IF EXISTS sentinel;"
        docker exec sentinel-postgres psql -U sentinel -c "CREATE DATABASE sentinel;"

        # Restore from backup
        cat "$DB_BACKUP" | docker exec -i sentinel-postgres psql -U sentinel sentinel

        log_success "Database restored"
    else
        log_info "Skipping database restore"
    fi
else
    log_warn "No database backup found for $BACKUP_TAG"
fi

# ==============================================================
# STEP 4: Start Restored Services
# ==============================================================
echo ""
echo -e "${YELLOW}[STEP 4/4] Starting restored services...${NC}"
echo "-----------------------------------------------------------"

log_info "Starting services..."
docker-compose up -d
log_success "Services started"

log_info "Waiting for services to stabilize (30s)..."
sleep 30

# Health check
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 -k "https://sentinelrmm.us:4443/health" 2>/dev/null || echo "000")
if [ "$HTTP_STATUS" = "200" ]; then
    log_success "Health check passed"
else
    log_warn "Health check returned HTTP $HTTP_STATUS"
fi

# ==============================================================
# RESULT
# ==============================================================
echo ""
echo "==========================================================="
echo -e "${GREEN}  ROLLBACK COMPLETE${NC}"
echo "==========================================================="
echo ""
echo "  Restored to: $BACKUP_TAG"
echo ""
echo "  View logs: docker-compose logs -f"
echo "  Run validation: ./scripts/validate-critical-paths.sh"
echo ""
