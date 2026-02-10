#!/bin/bash
#
# Sentinel RMM Agent - Uninstaller for macOS
# Run with sudo: sudo ./uninstall.sh
#
# Options:
#   --keep-config    Preserve configuration files
#   --keep-logs      Preserve log files
#   --force          Skip confirmation prompt
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Options
KEEP_CONFIG=0
KEEP_LOGS=0
FORCE=0

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --keep-config)
            KEEP_CONFIG=1
            shift
            ;;
        --keep-logs)
            KEEP_LOGS=1
            shift
            ;;
        --force)
            FORCE=1
            shift
            ;;
        -h|--help)
            echo "Sentinel RMM Agent Uninstaller"
            echo ""
            echo "Usage: sudo ./uninstall.sh [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --keep-config    Preserve configuration files in /etc/sentinel"
            echo "  --keep-logs      Preserve log files in /var/log/sentinel"
            echo "  --force          Skip confirmation prompt"
            echo "  -h, --help       Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Check for root privileges
if [[ $EUID -ne 0 ]]; then
    echo -e "${RED}Error: This script must be run as root.${NC}"
    echo "Please run: sudo $0"
    exit 1
fi

echo "=============================================="
echo "  Sentinel RMM Agent - Uninstaller"
echo "=============================================="
echo ""

# Confirmation prompt
if [[ $FORCE -eq 0 ]]; then
    echo -e "${YELLOW}WARNING: This will remove the Sentinel RMM Agent from this system.${NC}"
    echo ""
    read -p "Are you sure you want to continue? (y/N) " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Uninstallation cancelled."
        exit 0
    fi
fi

echo ""

# Stop and unload services
echo "[1/5] Stopping services..."

if launchctl list | grep -q "com.sentinel.agent"; then
    echo "  - Unloading agent service..."
    launchctl unload /Library/LaunchDaemons/com.sentinel.agent.plist 2>/dev/null || true
fi

if launchctl list | grep -q "com.sentinel.watchdog"; then
    echo "  - Unloading watchdog service..."
    launchctl unload /Library/LaunchDaemons/com.sentinel.watchdog.plist 2>/dev/null || true
fi

# Give services time to stop
sleep 2

# Kill any remaining processes
echo "[2/5] Stopping any remaining processes..."
pkill -9 sentinel-agent 2>/dev/null || true
pkill -9 sentinel-watchdog 2>/dev/null || true

# Remove LaunchDaemon plists
echo "[3/5] Removing LaunchDaemon configurations..."
rm -f /Library/LaunchDaemons/com.sentinel.agent.plist
rm -f /Library/LaunchDaemons/com.sentinel.watchdog.plist
echo "  - Removed LaunchDaemon plists"

# Remove binaries
echo "[4/5] Removing binaries..."
rm -f /usr/local/bin/sentinel-agent
rm -f /usr/local/bin/sentinel-watchdog
echo "  - Removed binaries from /usr/local/bin/"

# Remove data directories
echo "[5/5] Removing data directories..."

if [[ $KEEP_CONFIG -eq 0 ]]; then
    rm -rf /etc/sentinel
    echo "  - Removed /etc/sentinel/"
else
    echo -e "  - ${YELLOW}Preserved /etc/sentinel/ (--keep-config)${NC}"
fi

if [[ $KEEP_LOGS -eq 0 ]]; then
    rm -rf /var/log/sentinel
    rm -f /var/log/sentinel-install.log
    echo "  - Removed /var/log/sentinel/"
else
    echo -e "  - ${YELLOW}Preserved /var/log/sentinel/ (--keep-logs)${NC}"
fi

rm -rf /var/lib/sentinel
echo "  - Removed /var/lib/sentinel/"

# Forget the package receipt
echo ""
echo "Removing package receipt..."
pkgutil --forget com.sentinel.agent 2>/dev/null || true

echo ""
echo -e "${GREEN}=============================================="
echo "  Uninstallation Complete!"
echo "==============================================${NC}"
echo ""
echo "The Sentinel RMM Agent has been removed from this system."

if [[ $KEEP_CONFIG -eq 1 ]]; then
    echo ""
    echo "Configuration files were preserved in /etc/sentinel/"
    echo "To remove them manually: sudo rm -rf /etc/sentinel"
fi

if [[ $KEEP_LOGS -eq 1 ]]; then
    echo ""
    echo "Log files were preserved in /var/log/sentinel/"
    echo "To remove them manually: sudo rm -rf /var/log/sentinel"
fi

echo ""
exit 0
