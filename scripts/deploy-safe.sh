#!/bin/bash
# Sentinel RMM - Safe Production Deployment Script
# This script ensures safe deployments with validation and rollback capability
#
# Features:
#   - Pre-deployment validation
#   - Automatic backup before deployment
#   - Health checks after deployment
#   - Automatic rollback on failure
#
# Usage:
#   ./deploy-safe.sh                    # Full deployment
#   ./deploy-safe.sh --skip-backup      # Skip backup (for minor changes)
#   ./deploy-safe.sh --no-rollback      # Don't auto-rollback on failure

set -e

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BACKUP_DIR="$PROJECT_DIR/backups"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
BACKUP_TAG="backup-$TIMESTAMP"

# Options
SKIP_BACKUP=false
AUTO_ROLLBACK=true

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --skip-backup)
            SKIP_BACKUP=true
            shift
            ;;
        --no-rollback)
            AUTO_ROLLBACK=false
            shift
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

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

echo ""
echo "==========================================================="
echo "  SENTINEL RMM - SAFE PRODUCTION DEPLOYMENT"
echo "==========================================================="
echo ""
echo "  Project:   $PROJECT_DIR"
echo "  Timestamp: $TIMESTAMP"
echo "  Backup:    $SKIP_BACKUP && echo 'SKIP' || echo 'YES'"
echo "  Rollback:  $AUTO_ROLLBACK && echo 'AUTO' || echo 'MANUAL'"
echo ""
echo "==========================================================="

cd "$PROJECT_DIR"

# ==============================================================
# STEP 1: Pre-deployment Validation
# ==============================================================
echo ""
echo -e "${YELLOW}[STEP 1/7] Running pre-deployment validation...${NC}"
echo "-----------------------------------------------------------"

# Check if Go backend compiles
log_info "Checking Go backend compilation..."
cd server
if go build -o /dev/null ./cmd/sentinel 2>&1; then
    log_success "Go backend compiles successfully"
else
    log_error "Go backend compilation failed"
    exit 1
fi
cd ..

# Check if TypeScript compiles
log_info "Checking TypeScript compilation..."
if npx tsc --noEmit -p tsconfig.main.json 2>&1; then
    log_success "TypeScript compiles successfully"
else
    log_warn "TypeScript has warnings (continuing anyway)"
fi

# Run the critical path validation on current production
log_info "Running critical path validation on current production..."
if [ -x "$SCRIPT_DIR/validate-critical-paths.sh" ]; then
    if "$SCRIPT_DIR/validate-critical-paths.sh" "https://sentinelrmm.us:4443" 2>&1; then
        log_success "Current production is healthy"
    else
        log_warn "Current production has issues - deployment may fix them"
    fi
else
    log_warn "Validation script not found - skipping"
fi

# ==============================================================
# STEP 2: Create Backup
# ==============================================================
echo ""
echo -e "${YELLOW}[STEP 2/7] Creating backup...${NC}"
echo "-----------------------------------------------------------"

if [ "$SKIP_BACKUP" = false ]; then
    mkdir -p "$BACKUP_DIR"

    # Backup Docker images
    log_info "Tagging current Docker images..."
    docker tag sentinel-backend:latest sentinel-backend:$BACKUP_TAG 2>/dev/null || log_warn "No existing backend image to backup"
    docker tag sentinel-frontend:latest sentinel-frontend:$BACKUP_TAG 2>/dev/null || log_warn "No existing frontend image to backup"

    # Backup database
    log_info "Backing up database..."
    if docker exec sentinel-postgres pg_dump -U sentinel sentinel > "$BACKUP_DIR/db-$BACKUP_TAG.sql" 2>/dev/null; then
        log_success "Database backed up to $BACKUP_DIR/db-$BACKUP_TAG.sql"
    else
        log_warn "Database backup failed (container may not be running)"
    fi

    # Save backup tag for rollback
    echo "$BACKUP_TAG" > "$BACKUP_DIR/.latest_backup"
    log_success "Backup created: $BACKUP_TAG"
else
    log_warn "Backup skipped (--skip-backup flag)"
fi

# ==============================================================
# STEP 3: Pull Latest Code
# ==============================================================
echo ""
echo -e "${YELLOW}[STEP 3/7] Pulling latest code...${NC}"
echo "-----------------------------------------------------------"

log_info "Pulling from Git..."
if git pull 2>&1; then
    log_success "Code updated from Git"
else
    log_error "Git pull failed - resolve conflicts and retry"
    exit 1
fi

# ==============================================================
# STEP 4: Build New Images
# ==============================================================
echo ""
echo -e "${YELLOW}[STEP 4/7] Building new Docker images...${NC}"
echo "-----------------------------------------------------------"

log_info "Building images (this may take a few minutes)..."
if docker-compose build --no-cache 2>&1; then
    log_success "Docker images built successfully"
else
    log_error "Docker build failed"
    exit 1
fi

# ==============================================================
# STEP 5: Deploy New Version
# ==============================================================
echo ""
echo -e "${YELLOW}[STEP 5/7] Deploying new version...${NC}"
echo "-----------------------------------------------------------"

log_info "Starting new containers..."
if docker-compose up -d 2>&1; then
    log_success "Containers started"
else
    log_error "Container startup failed"
    if [ "$AUTO_ROLLBACK" = true ]; then
        log_warn "Initiating automatic rollback..."
        "$SCRIPT_DIR/rollback.sh" "$BACKUP_TAG"
    fi
    exit 1
fi

# ==============================================================
# STEP 6: Wait for Services
# ==============================================================
echo ""
echo -e "${YELLOW}[STEP 6/7] Waiting for services to stabilize...${NC}"
echo "-----------------------------------------------------------"

log_info "Waiting 60 seconds for services to stabilize..."
sleep 60

# ==============================================================
# STEP 7: Post-deployment Validation
# ==============================================================
echo ""
echo -e "${YELLOW}[STEP 7/7] Running post-deployment validation...${NC}"
echo "-----------------------------------------------------------"

DEPLOYMENT_SUCCESS=true

# Run critical path validation
if [ -x "$SCRIPT_DIR/validate-critical-paths.sh" ]; then
    if "$SCRIPT_DIR/validate-critical-paths.sh" "https://sentinelrmm.us:4443" 2>&1; then
        log_success "Post-deployment validation passed"
    else
        log_error "Post-deployment validation FAILED"
        DEPLOYMENT_SUCCESS=false
    fi
else
    # Fallback: basic health check
    log_info "Running basic health check..."
    HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 -k "https://sentinelrmm.us:4443/health")
    if [ "$HTTP_STATUS" = "200" ]; then
        log_success "Health check passed (HTTP $HTTP_STATUS)"
    else
        log_error "Health check failed (HTTP $HTTP_STATUS)"
        DEPLOYMENT_SUCCESS=false
    fi
fi

# ==============================================================
# RESULT
# ==============================================================
echo ""
echo "==========================================================="

if [ "$DEPLOYMENT_SUCCESS" = true ]; then
    echo -e "${GREEN}  DEPLOYMENT SUCCESSFUL${NC}"
    echo "==========================================================="
    echo ""
    echo "  Backup available: $BACKUP_TAG"
    echo "  To rollback: ./scripts/rollback.sh $BACKUP_TAG"
    echo ""
    echo "  View logs: docker-compose logs -f"
    echo ""
    exit 0
else
    echo -e "${RED}  DEPLOYMENT FAILED${NC}"
    echo "==========================================================="

    if [ "$AUTO_ROLLBACK" = true ] && [ "$SKIP_BACKUP" = false ]; then
        echo ""
        log_warn "Initiating automatic rollback..."
        "$SCRIPT_DIR/rollback.sh" "$BACKUP_TAG"
    else
        echo ""
        echo "  Manual intervention required."
        echo "  To rollback: ./scripts/rollback.sh $BACKUP_TAG"
        echo "  View logs: docker-compose logs -f"
        echo ""
    fi
    exit 1
fi
