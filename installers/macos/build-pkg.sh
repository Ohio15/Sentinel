#!/bin/bash
#
# Sentinel RMM Agent - macOS Package Builder
# Builds a signed .pkg installer using pkgbuild and productbuild
#
# Usage: ./build-pkg.sh [--sign "Developer ID Installer: ..."]
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
BUILD_DIR="$SCRIPT_DIR/build"
OUTPUT_DIR="$SCRIPT_DIR/output"
PAYLOAD_DIR="$BUILD_DIR/payload"
SCRIPTS_DIR="$SCRIPT_DIR/scripts"

# Package metadata
PKG_IDENTIFIER="com.sentinel.agent"
PKG_VERSION=$(cat "$PROJECT_ROOT/installers/version.json" 2>/dev/null | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || echo "1.0.0")
PKG_NAME="SentinelAgent-${PKG_VERSION}.pkg"

# Signing identity (optional)
SIGN_IDENTITY=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --sign)
            SIGN_IDENTITY="$2"
            shift 2
            ;;
        --version)
            PKG_VERSION="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

echo "=============================================="
echo "  Sentinel RMM Agent - macOS Package Builder"
echo "=============================================="
echo ""
echo "Version: $PKG_VERSION"
echo "Package: $PKG_NAME"
echo ""

# Clean previous build
echo "[1/6] Cleaning previous build..."
rm -rf "$BUILD_DIR" "$OUTPUT_DIR"
mkdir -p "$BUILD_DIR" "$OUTPUT_DIR" "$PAYLOAD_DIR"

# Create payload directory structure
echo "[2/6] Creating payload structure..."
mkdir -p "$PAYLOAD_DIR/usr/local/bin"
mkdir -p "$PAYLOAD_DIR/etc/sentinel"
mkdir -p "$PAYLOAD_DIR/Library/LaunchDaemons"
mkdir -p "$PAYLOAD_DIR/var/log/sentinel"

# Copy binaries
echo "[3/6] Copying binaries..."
AGENT_BINARY="$PROJECT_ROOT/release/agent/sentinel-agent-darwin-amd64"
WATCHDOG_BINARY="$PROJECT_ROOT/release/agent/sentinel-watchdog-darwin-amd64"

# Check for ARM64 binaries as alternative
if [[ ! -f "$AGENT_BINARY" ]]; then
    AGENT_BINARY="$PROJECT_ROOT/release/agent/sentinel-agent-darwin-arm64"
fi
if [[ ! -f "$WATCHDOG_BINARY" ]]; then
    WATCHDOG_BINARY="$PROJECT_ROOT/release/agent/sentinel-watchdog-darwin-arm64"
fi

# Check for universal binaries
if [[ ! -f "$AGENT_BINARY" ]]; then
    AGENT_BINARY="$PROJECT_ROOT/release/agent/sentinel-agent"
fi
if [[ ! -f "$WATCHDOG_BINARY" ]]; then
    WATCHDOG_BINARY="$PROJECT_ROOT/release/agent/sentinel-watchdog"
fi

if [[ -f "$AGENT_BINARY" ]]; then
    cp "$AGENT_BINARY" "$PAYLOAD_DIR/usr/local/bin/sentinel-agent"
    chmod 755 "$PAYLOAD_DIR/usr/local/bin/sentinel-agent"
    echo "  - Agent binary: $(basename "$AGENT_BINARY")"
else
    echo "  WARNING: Agent binary not found. Package will be created without binaries."
    echo "           Build the agent first with: GOOS=darwin GOARCH=amd64 go build ..."
    # Create placeholder for testing
    echo '#!/bin/bash' > "$PAYLOAD_DIR/usr/local/bin/sentinel-agent"
    echo 'echo "Sentinel Agent placeholder - replace with actual binary"' >> "$PAYLOAD_DIR/usr/local/bin/sentinel-agent"
    chmod 755 "$PAYLOAD_DIR/usr/local/bin/sentinel-agent"
fi

if [[ -f "$WATCHDOG_BINARY" ]]; then
    cp "$WATCHDOG_BINARY" "$PAYLOAD_DIR/usr/local/bin/sentinel-watchdog"
    chmod 755 "$PAYLOAD_DIR/usr/local/bin/sentinel-watchdog"
    echo "  - Watchdog binary: $(basename "$WATCHDOG_BINARY")"
else
    echo "  WARNING: Watchdog binary not found."
    # Create placeholder for testing
    echo '#!/bin/bash' > "$PAYLOAD_DIR/usr/local/bin/sentinel-watchdog"
    echo 'echo "Sentinel Watchdog placeholder - replace with actual binary"' >> "$PAYLOAD_DIR/usr/local/bin/sentinel-watchdog"
    chmod 755 "$PAYLOAD_DIR/usr/local/bin/sentinel-watchdog"
fi

# Copy LaunchDaemon plists
echo "[4/6] Installing LaunchDaemon plists..."
cp "$SCRIPT_DIR/launchd/com.sentinel.agent.plist" "$PAYLOAD_DIR/Library/LaunchDaemons/"
cp "$SCRIPT_DIR/launchd/com.sentinel.watchdog.plist" "$PAYLOAD_DIR/Library/LaunchDaemons/"
chmod 644 "$PAYLOAD_DIR/Library/LaunchDaemons/"*.plist

# Create default config template
echo "[5/6] Creating configuration template..."
cat > "$PAYLOAD_DIR/etc/sentinel/config.json.template" << 'EOF'
{
  "server_url": "%%SERVER_URL%%",
  "grpc_endpoint": "%%GRPC_ENDPOINT%%",
  "enrollment_token": "%%ENROLLMENT_TOKEN%%",
  "organization_id": "%%ORGANIZATION_ID%%"
}
EOF
chmod 644 "$PAYLOAD_DIR/etc/sentinel/config.json.template"

# Build the component package
echo "[6/6] Building package..."
COMPONENT_PKG="$BUILD_DIR/SentinelAgent-component.pkg"

pkgbuild \
    --root "$PAYLOAD_DIR" \
    --identifier "$PKG_IDENTIFIER" \
    --version "$PKG_VERSION" \
    --scripts "$SCRIPTS_DIR" \
    --install-location "/" \
    "$COMPONENT_PKG"

# Build the final product package
if [[ -n "$SIGN_IDENTITY" ]]; then
    echo ""
    echo "Signing package with: $SIGN_IDENTITY"
    productbuild \
        --distribution "$SCRIPT_DIR/Distribution.xml" \
        --package-path "$BUILD_DIR" \
        --sign "$SIGN_IDENTITY" \
        "$OUTPUT_DIR/$PKG_NAME"
else
    productbuild \
        --distribution "$SCRIPT_DIR/Distribution.xml" \
        --package-path "$BUILD_DIR" \
        "$OUTPUT_DIR/$PKG_NAME"
    echo ""
    echo "NOTE: Package is unsigned. For distribution, sign with:"
    echo "  ./build-pkg.sh --sign \"Developer ID Installer: Your Name (TEAMID)\""
fi

# Show result
echo ""
echo "=============================================="
echo "  Build Complete!"
echo "=============================================="
echo ""
echo "Package: $OUTPUT_DIR/$PKG_NAME"
echo "Size: $(du -h "$OUTPUT_DIR/$PKG_NAME" | cut -f1)"
echo ""
echo "To install (requires sudo):"
echo "  sudo installer -pkg \"$OUTPUT_DIR/$PKG_NAME\" -target /"
echo ""
echo "To verify contents:"
echo "  pkgutil --payload-files \"$OUTPUT_DIR/$PKG_NAME\""
echo ""
