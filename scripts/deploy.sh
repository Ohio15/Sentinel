#!/bin/bash
set -e

# Sentinel RMM Platform - Deployment Script
# For Oracle Cloud Free Tier deployment

echo "=========================================="
echo "  Sentinel RMM Platform Deployment"
echo "=========================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}Please run as root or with sudo${NC}"
    exit 1
fi

# Configuration
DOMAIN=${DOMAIN:-"sentinel.example.com"}
EMAIL=${EMAIL:-"admin@example.com"}
INSTALL_DIR=${INSTALL_DIR:-"/opt/sentinel"}

echo ""
echo -e "${YELLOW}Configuration:${NC}"
echo "  Domain: $DOMAIN"
echo "  Email: $EMAIL"
echo "  Install Directory: $INSTALL_DIR"
echo ""

# Check prerequisites
check_prerequisites() {
    echo -e "${YELLOW}Checking prerequisites...${NC}"

    # Check for Docker
    if ! command -v docker &> /dev/null; then
        echo -e "${RED}Docker is not installed. Installing...${NC}"
        curl -fsSL https://get.docker.com -o get-docker.sh
        sh get-docker.sh
        rm get-docker.sh
        systemctl enable docker
        systemctl start docker
    fi
    echo -e "${GREEN}✓ Docker installed${NC}"

    # Check for Docker Compose
    if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
        echo -e "${RED}Docker Compose is not installed. Installing...${NC}"
        curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
        chmod +x /usr/local/bin/docker-compose
    fi
    echo -e "${GREEN}✓ Docker Compose installed${NC}"

    # Check for Git
    if ! command -v git &> /dev/null; then
        echo -e "${RED}Git is not installed. Installing...${NC}"
        apt-get update && apt-get install -y git
    fi
    echo -e "${GREEN}✓ Git installed${NC}"
}

# Generate secure passwords
generate_passwords() {
    echo -e "${YELLOW}Generating secure passwords...${NC}"

    export POSTGRES_PASSWORD=$(openssl rand -base64 32 | tr -dc 'a-zA-Z0-9' | head -c 32)
    export JWT_SECRET=$(openssl rand -base64 64 | tr -dc 'a-zA-Z0-9' | head -c 64)
    export ENROLLMENT_TOKEN=$(openssl rand -base64 32 | tr -dc 'a-zA-Z0-9' | head -c 32)

    echo -e "${GREEN}✓ Passwords generated${NC}"
}

# Create installation directory and files
setup_installation() {
    echo -e "${YELLOW}Setting up installation directory...${NC}"

    mkdir -p "$INSTALL_DIR"
    cd "$INSTALL_DIR"

    # Create .env file
    cat > .env << EOF
# Sentinel RMM Configuration
# Generated on $(date)

# Domain and SSL
DOMAIN=${DOMAIN}
ACME_EMAIL=${EMAIL}

# Database
POSTGRES_USER=sentinel
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_DB=sentinel

# Security
JWT_SECRET=${JWT_SECRET}
ENROLLMENT_TOKEN=${ENROLLMENT_TOKEN}

# URLs
DATABASE_URL=postgres://sentinel:${POSTGRES_PASSWORD}@postgres:5432/sentinel?sslmode=disable
REDIS_URL=redis://redis:6379
EOF

    echo -e "${GREEN}✓ Environment file created${NC}"
}

# Copy or download application files
setup_application() {
    echo -e "${YELLOW}Setting up application files...${NC}"

    # If we're in the source directory, copy files
    if [ -f "docker-compose.yml" ]; then
        cp docker-compose.yml "$INSTALL_DIR/"
        cp -r server "$INSTALL_DIR/" 2>/dev/null || true
        cp -r frontend "$INSTALL_DIR/" 2>/dev/null || true
    else
        echo -e "${RED}Application files not found. Please clone the repository first.${NC}"
        exit 1
    fi

    echo -e "${GREEN}✓ Application files ready${NC}"
}

# Configure firewall
configure_firewall() {
    echo -e "${YELLOW}Configuring firewall...${NC}"

    # Check if iptables is available
    if command -v iptables &> /dev/null; then
        # Allow HTTP and HTTPS
        iptables -A INPUT -p tcp --dport 80 -j ACCEPT 2>/dev/null || true
        iptables -A INPUT -p tcp --dport 443 -j ACCEPT 2>/dev/null || true
    fi

    # For Oracle Cloud, also need to configure Security Lists in the console

    echo -e "${GREEN}✓ Firewall configured${NC}"
    echo -e "${YELLOW}Note: Remember to configure Oracle Cloud Security Lists to allow ports 80 and 443${NC}"
}

# Start the application
start_application() {
    echo -e "${YELLOW}Starting Sentinel RMM Platform...${NC}"

    cd "$INSTALL_DIR"

    # Pull and build images
    docker-compose pull
    docker-compose build

    # Start services
    docker-compose up -d

    echo -e "${GREEN}✓ Application started${NC}"
}

# Wait for services to be healthy
wait_for_services() {
    echo -e "${YELLOW}Waiting for services to be healthy...${NC}"

    local max_attempts=30
    local attempt=0

    while [ $attempt -lt $max_attempts ]; do
        if curl -s -f http://localhost:8080/health > /dev/null 2>&1; then
            echo -e "${GREEN}✓ Services are healthy${NC}"
            return 0
        fi
        attempt=$((attempt + 1))
        echo "  Waiting... ($attempt/$max_attempts)"
        sleep 10
    done

    echo -e "${RED}Services failed to become healthy${NC}"
    return 1
}

# Print summary
print_summary() {
    echo ""
    echo "=========================================="
    echo -e "${GREEN}  Deployment Complete!${NC}"
    echo "=========================================="
    echo ""
    echo "Access the platform at:"
    echo "  https://${DOMAIN}"
    echo ""
    echo "Default credentials:"
    echo "  Email: admin@sentinel.local"
    echo "  Password: admin"
    echo ""
    echo "Agent enrollment token:"
    echo "  ${ENROLLMENT_TOKEN}"
    echo ""
    echo "Important files:"
    echo "  Configuration: ${INSTALL_DIR}/.env"
    echo "  Logs: docker-compose logs -f"
    echo ""
    echo -e "${YELLOW}IMPORTANT: Change the default password immediately!${NC}"
    echo ""
}

# Main execution
main() {
    check_prerequisites
    generate_passwords
    setup_installation
    setup_application
    configure_firewall
    start_application
    wait_for_services
    print_summary
}

# Run main function
main "$@"
