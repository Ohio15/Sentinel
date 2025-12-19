#!/bin/bash
# Build macOS PKG installer for Sentinel Agent
# Run this script on macOS to create the PKG installer

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AGENT_DIR="$(dirname "$SCRIPT_DIR")"
VERSION_FILE="$AGENT_DIR/version.json"

# Read version
if [ -f "$VERSION_FILE" ]; then
    VERSION=$(cat "$VERSION_FILE" | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"\([^"]*\)"$/\1/')
else
    VERSION="1.0.0"
    echo "Warning: version.json not found, using default version $VERSION"
fi

echo "Building Sentinel Agent PKG installer v$VERSION"

# Paths
BINARY="$AGENT_DIR/../release/agent/sentinel-agent-macos"
PKG_ROOT="$SCRIPT_DIR/pkg-root"
SCRIPTS_DIR="$SCRIPT_DIR/pkg-scripts"
OUTPUT="$AGENT_DIR/../release/agent/sentinel-agent.pkg"

# Check if binary exists
if [ ! -f "$BINARY" ]; then
    echo "Error: macOS binary not found at $BINARY"
    echo "Build the macOS binary first with: go build -o ../release/agent/sentinel-agent-macos ./cmd/sentinel-agent"
    exit 1
fi

# Clean previous build
rm -rf "$PKG_ROOT" "$SCRIPTS_DIR"

# Create directory structure
mkdir -p "$PKG_ROOT/usr/local/bin"
mkdir -p "$PKG_ROOT/Library/LaunchDaemons"
mkdir -p "$PKG_ROOT/etc/sentinel"
mkdir -p "$SCRIPTS_DIR"

# Copy binary
cp "$BINARY" "$PKG_ROOT/usr/local/bin/sentinel-agent"
chmod +x "$PKG_ROOT/usr/local/bin/sentinel-agent"

# Create LaunchDaemon plist
cat > "$PKG_ROOT/Library/LaunchDaemons/com.sentinel.agent.plist" << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.sentinel.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/sentinel-agent</string>
        <string>--service</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/var/log/sentinel-agent.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/sentinel-agent.err</string>
</dict>
</plist>
EOF

# Create config file with placeholders for embedding at download time
cat > "$PKG_ROOT/etc/sentinel/config.json" << 'EOF'
{
  "serverUrl": "__SERVERURL__",
  "enrollmentToken": "__TOKEN__"
}
EOF

# Create post-install script
cat > "$SCRIPTS_DIR/postinstall" << 'EOF'
#!/bin/bash
# Post-installation script for Sentinel Agent

# Set proper permissions
chmod 644 /Library/LaunchDaemons/com.sentinel.agent.plist
chmod 755 /usr/local/bin/sentinel-agent
chmod 600 /etc/sentinel/config.json

# Load and start the agent service
launchctl load /Library/LaunchDaemons/com.sentinel.agent.plist
launchctl start com.sentinel.agent

echo "Sentinel Agent installed and started successfully"
exit 0
EOF
chmod +x "$SCRIPTS_DIR/postinstall"

# Create pre-install script (stops existing service if running)
cat > "$SCRIPTS_DIR/preinstall" << 'EOF'
#!/bin/bash
# Pre-installation script for Sentinel Agent

# Stop and unload existing service if present
if launchctl list | grep -q "com.sentinel.agent"; then
    launchctl stop com.sentinel.agent 2>/dev/null || true
    launchctl unload /Library/LaunchDaemons/com.sentinel.agent.plist 2>/dev/null || true
fi

exit 0
EOF
chmod +x "$SCRIPTS_DIR/preinstall"

# Build the PKG
echo "Building PKG..."
pkgbuild --root "$PKG_ROOT" \
         --scripts "$SCRIPTS_DIR" \
         --identifier "com.sentinel.agent" \
         --version "$VERSION" \
         --install-location "/" \
         "$OUTPUT"

if [ -f "$OUTPUT" ]; then
    SIZE=$(du -h "$OUTPUT" | cut -f1)
    echo "Success: Built $OUTPUT ($SIZE)"
else
    echo "Error: Failed to create PKG"
    exit 1
fi

# Clean up
rm -rf "$PKG_ROOT" "$SCRIPTS_DIR"

echo "Done!"
