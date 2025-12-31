#!/bin/bash
# Sentinel Agent Installation Script for Linux/macOS
# Usage: curl -sSL https://your-server/install.sh | bash -s -- --server=URL --token=TOKEN
# Or: ./install.sh --server=http://server:8080 --token=your-token

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Default values
SERVER=""
TOKEN=""
SILENT=false
FORCE=false
REPAIR=false
VERIFY=false

# Platform detection
detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$ARCH" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        armv7l|armhf)
            ARCH="arm"
            ;;
        *)
            echo -e "${RED}Unsupported architecture: $ARCH${NC}"
            exit 1
            ;;
    esac

    case "$OS" in
        linux)
            PLATFORM="linux"
            ;;
        darwin)
            PLATFORM="darwin"
            ;;
        *)
            echo -e "${RED}Unsupported operating system: $OS${NC}"
            exit 1
            ;;
    esac
}

# Banner
show_banner() {
    echo ""
    echo -e "${CYAN}  ____             _   _            _ ${NC}"
    echo -e "${CYAN} / ___|  ___ _ __ | |_(_)_ __   ___| |${NC}"
    echo -e "${CYAN} \\___ \\ / _ \\ '_ \\| __| | '_ \\ / _ \\ |${NC}"
    echo -e "${CYAN}  ___) |  __/ | | | |_| | | | |  __/ |${NC}"
    echo -e "${CYAN} |____/ \\___|_| |_|\\__|_|_| |_|\\___|_|${NC}"
    echo ""
    echo -e "       Remote Monitoring & Management"
    echo ""
}

# Logging functions
log_step() {
    echo -e "${YELLOW}[*]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[+]${NC} $1"
}

log_error() {
    echo -e "${RED}[!]${NC} $1"
}

# Check for root privileges
check_root() {
    if [ "$EUID" -ne 0 ]; then
        log_error "Root privileges required!"
        echo ""
        echo -e "${YELLOW}Please run with sudo:${NC}"
        echo "  sudo $0 $*"
        echo ""
        exit 1
    fi
}

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --server=*)
                SERVER="${1#*=}"
                shift
                ;;
            --token=*)
                TOKEN="${1#*=}"
                shift
                ;;
            --silent)
                SILENT=true
                shift
                ;;
            --force)
                FORCE=true
                shift
                ;;
            --repair)
                REPAIR=true
                shift
                ;;
            --verify)
                VERIFY=true
                shift
                ;;
            --help|-h)
                show_help
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done
}

show_help() {
    echo "Sentinel Agent Installation Script"
    echo ""
    echo "Usage: $0 --server=URL --token=TOKEN [options]"
    echo ""
    echo "Required:"
    echo "  --server=URL     Sentinel server URL (e.g., http://server:8080)"
    echo "  --token=TOKEN    Enrollment token from Sentinel dashboard"
    echo ""
    echo "Options:"
    echo "  --silent         Silent installation (no output)"
    echo "  --force          Force reinstall if agent exists"
    echo "  --repair         Repair existing installation"
    echo "  --verify         Verify installation integrity"
    echo "  --help, -h       Show this help message"
    echo ""
    echo "Examples:"
    echo "  # Basic installation"
    echo "  sudo $0 --server=http://sentinel.example.com:8080 --token=abc123"
    echo ""
    echo "  # One-liner installation"
    echo "  curl -sSL http://server/install.sh | sudo bash -s -- --server=URL --token=TOKEN"
}

# Check for existing installation
check_existing() {
    INSTALL_PATH=""
    if [ -f "/opt/sentinel/sentinel-agent" ]; then
        INSTALL_PATH="/opt/sentinel/sentinel-agent"
    elif [ -f "/usr/local/sentinel/sentinel-agent" ]; then
        INSTALL_PATH="/usr/local/sentinel/sentinel-agent"
    fi

    if [ -n "$INSTALL_PATH" ] && [ "$FORCE" = false ] && [ "$REPAIR" = false ] && [ "$VERIFY" = false ]; then
        echo ""
        echo -e "${YELLOW}Sentinel Agent is already installed at:${NC}"
        echo "  $INSTALL_PATH"
        echo ""
        echo -e "${YELLOW}Options:${NC}"
        echo "  --force   : Reinstall the agent"
        echo "  --repair  : Repair the installation"
        echo "  --verify  : Verify installation integrity"
        echo ""
        exit 0
    fi
}

# Download with retry
download_file() {
    local url=$1
    local dest=$2
    local max_retries=3
    local retry=0

    while [ $retry -lt $max_retries ]; do
        if command -v curl &> /dev/null; then
            if curl -sSL --fail -o "$dest" "$url" 2>/dev/null; then
                return 0
            fi
        elif command -v wget &> /dev/null; then
            if wget -q -O "$dest" "$url" 2>/dev/null; then
                return 0
            fi
        else
            log_error "Neither curl nor wget found. Please install one of them."
            exit 1
        fi

        retry=$((retry + 1))
        if [ $retry -lt $max_retries ]; then
            log_step "Download failed, retrying ($retry/$max_retries)..."
            sleep 2
        fi
    done

    return 1
}

# Main installation function
install_agent() {
    if [ "$SILENT" = false ]; then
        show_banner
    fi

    check_root "$@"
    detect_platform

    # Check for required tools
    if ! command -v curl &> /dev/null && ! command -v wget &> /dev/null; then
        log_error "curl or wget is required for installation"
        exit 1
    fi

    # Validate parameters
    if [ -z "$SERVER" ]; then
        # Check environment variable
        if [ -n "$SENTINEL_SERVER" ]; then
            SERVER="$SENTINEL_SERVER"
        else
            log_error "Server URL is required!"
            echo ""
            echo -e "${YELLOW}Usage:${NC}"
            echo "  $0 --server='http://your-server:8080' --token='your-token'"
            echo ""
            exit 1
        fi
    fi

    if [ -z "$TOKEN" ] && [ "$REPAIR" = false ] && [ "$VERIFY" = false ]; then
        # Check environment variable
        if [ -n "$SENTINEL_TOKEN" ]; then
            TOKEN="$SENTINEL_TOKEN"
        else
            log_error "Enrollment token is required!"
            echo ""
            echo -e "${YELLOW}Get your token from the Sentinel dashboard:${NC}"
            echo "  Settings -> Enrollment -> Generate Token"
            echo ""
            exit 1
        fi
    fi

    check_existing

    # Create temp directory
    TEMP_DIR=$(mktemp -d)
    trap "rm -rf $TEMP_DIR" EXIT

    BOOTSTRAP_PATH="$TEMP_DIR/sentinel-bootstrap"

    # Build download URL
    BOOTSTRAP_URL="${SERVER}/api/bootstrap/download?platform=${PLATFORM}&arch=${ARCH}"
    if [ -n "$TOKEN" ]; then
        BOOTSTRAP_URL="${BOOTSTRAP_URL}&token=${TOKEN}"
    fi

    # Download bootstrapper
    log_step "Downloading Sentinel bootstrapper..."
    if ! download_file "$BOOTSTRAP_URL" "$BOOTSTRAP_PATH"; then
        log_error "Failed to download bootstrapper"
        exit 1
    fi

    chmod +x "$BOOTSTRAP_PATH"
    FILE_SIZE=$(stat -f%z "$BOOTSTRAP_PATH" 2>/dev/null || stat -c%s "$BOOTSTRAP_PATH" 2>/dev/null)
    log_success "Downloaded bootstrapper ($((FILE_SIZE / 1024)) KB)"

    # Prepare arguments
    ARGS="--server=$SERVER"

    if [ -n "$TOKEN" ]; then
        ARGS="$ARGS --token=$TOKEN"
    fi

    if [ "$SILENT" = true ]; then
        ARGS="$ARGS --silent"
    fi

    if [ "$FORCE" = true ]; then
        ARGS="$ARGS --force"
    fi

    if [ "$REPAIR" = true ]; then
        ARGS="$ARGS --repair"
    fi

    if [ "$VERIFY" = true ]; then
        ARGS="$ARGS --verify"
    fi

    # Run bootstrapper
    log_step "Running bootstrapper..."
    if ! "$BOOTSTRAP_PATH" $ARGS; then
        log_error "Installation failed"
        exit 1
    fi

    log_success "Installation completed successfully!"

    echo ""
    echo -e "${GREEN}The Sentinel Agent is now running and will start automatically${NC}"
    echo -e "${GREEN}when the system boots.${NC}"
    echo ""
}

# Parse arguments and run
parse_args "$@"
install_agent
