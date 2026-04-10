#!/bin/bash
# Sentinel RMM Production Deployment Script
# This script sets up TLS certificates, mTLS, and deploys the containers

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
CERTS_DIR="$PROJECT_DIR/certs"

echo "=== Sentinel RMM Production Deployment ==="
echo ""

# Check for required environment file
if [ ! -f "$PROJECT_DIR/.env" ]; then
    echo "ERROR: .env file not found!"
    echo "Copy .env.production.template to .env and configure it first."
    exit 1
fi

# Source environment variables
set -a
source "$PROJECT_DIR/.env"
set +a

# Validate required variables
if [ -z "$DOMAIN" ] || [ "$DOMAIN" = "sentinel.yourdomain.com" ]; then
    echo "ERROR: DOMAIN not configured in .env"
    exit 1
fi

if [ -z "$ACME_EMAIL" ] || [ "$ACME_EMAIL" = "admin@yourdomain.com" ]; then
    echo "ERROR: ACME_EMAIL not configured in .env"
    exit 1
fi

echo "Configuration:"
echo "  Domain: $DOMAIN"
echo "  ACME Email: $ACME_EMAIL"
echo "  mTLS Enabled: ${MTLS_ENABLED:-false}"
echo ""

# Step 1: Create certificates directory
echo "Step 1: Setting up certificates directory..."
mkdir -p "$CERTS_DIR"
mkdir -p "$PROJECT_DIR/configs/traefik-agent/dynamic"

# Step 2: Generate CA certificate if mTLS is enabled and CA doesn't exist
if [ "${MTLS_ENABLED:-false}" = "true" ]; then
    echo "Step 2: Setting up mTLS Certificate Authority..."

    if [ ! -f "$CERTS_DIR/ca.crt" ]; then
        echo "Generating new CA certificate..."

        # Generate CA private key
        openssl genrsa -out "$CERTS_DIR/ca.key" 4096

        # Generate CA certificate
        openssl req -new -x509 -days 3650 \
            -key "$CERTS_DIR/ca.key" \
            -sha256 \
            -out "$CERTS_DIR/ca.crt" \
            -subj "/C=US/ST=Security/L=Sentinel/O=Sentinel RMM/OU=Agent Authentication/CN=Sentinel RMM CA"

        # Set permissions
        chmod 600 "$CERTS_DIR/ca.key"
        chmod 644 "$CERTS_DIR/ca.crt"

        echo "CA certificate generated successfully!"
        echo "CA certificate fingerprint:"
        openssl x509 -in "$CERTS_DIR/ca.crt" -noout -fingerprint -sha256
    else
        echo "CA certificate already exists, skipping generation."
    fi
else
    echo "Step 2: mTLS disabled, skipping CA setup."
fi

# Step 3: Create Traefik log directory
echo "Step 3: Setting up Traefik logs directory..."
mkdir -p /var/log/traefik || true

# Step 4: Pull latest images
echo "Step 4: Pulling Docker images..."
cd "$PROJECT_DIR"
docker compose pull traefik postgres redis 2>/dev/null || docker-compose pull traefik postgres redis

# Step 5: Build application images
echo "Step 5: Building application images..."
docker compose build --no-cache backend 2>/dev/null || docker-compose build --no-cache backend

# Step 6: Start services
echo "Step 6: Starting services..."
docker compose up -d 2>/dev/null || docker-compose up -d

echo ""
echo "=== Deployment Complete ==="
echo ""
echo "Services should be starting. Check status with:"
echo "  docker compose ps"
echo "  docker compose logs -f"
echo ""
echo "Once Traefik has obtained a certificate, your services will be available at:"
echo "  Web Dashboard: https://$DOMAIN"
echo "  API: https://$DOMAIN/api"
echo "  Agent Connections: https://$DOMAIN:8443/ws/agent"
echo ""

if [ "${MTLS_ENABLED:-false}" = "true" ]; then
    echo "mTLS is enabled. Agent certificates can be generated with:"
    echo "  ./scripts/generate-agent-cert.sh <agent-id>"
    echo ""
fi

echo "Check Traefik logs for certificate status:"
echo "  docker compose logs traefik"
