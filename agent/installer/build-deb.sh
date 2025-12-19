#!/bin/bash
# Build Linux DEB installer for Sentinel Agent
# Run this script on Linux (or WSL) to create the DEB package

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

echo "Building Sentinel Agent DEB installer v$VERSION"

# Paths
BINARY="$AGENT_DIR/../release/agent/sentinel-agent-linux"
DEB_ROOT="$SCRIPT_DIR/deb-root"
OUTPUT="$AGENT_DIR/../release/agent/sentinel-agent_${VERSION}_amd64.deb"
# Also create a version-agnostic symlink for easier access
OUTPUT_LINK="$AGENT_DIR/../release/agent/sentinel-agent.deb"

# Check if binary exists
if [ ! -f "$BINARY" ]; then
    echo "Error: Linux binary not found at $BINARY"
    echo "Build the Linux binary first with: GOOS=linux GOARCH=amd64 go build -o ../release/agent/sentinel-agent-linux ./cmd/sentinel-agent"
    exit 1
fi

# Clean previous build
rm -rf "$DEB_ROOT"

# Create directory structure (Debian package layout)
mkdir -p "$DEB_ROOT/DEBIAN"
mkdir -p "$DEB_ROOT/usr/local/bin"
mkdir -p "$DEB_ROOT/etc/sentinel"
mkdir -p "$DEB_ROOT/lib/systemd/system"

# Copy binary
cp "$BINARY" "$DEB_ROOT/usr/local/bin/sentinel-agent"
chmod +x "$DEB_ROOT/usr/local/bin/sentinel-agent"

# Create control file
cat > "$DEB_ROOT/DEBIAN/control" << EOF
Package: sentinel-agent
Version: $VERSION
Section: admin
Priority: optional
Architecture: amd64
Depends: systemd
Maintainer: Sentinel <support@sentinel.local>
Description: Sentinel RMM Agent
 Remote monitoring and management agent for Sentinel RMM.
 Provides system monitoring, remote management, and alerting
 capabilities for managed endpoints.
EOF

# Create systemd service file
cat > "$DEB_ROOT/lib/systemd/system/sentinel-agent.service" << 'EOF'
[Unit]
Description=Sentinel RMM Agent
Documentation=https://github.com/Ohio15/Sentinel
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/sentinel-agent --service
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=sentinel-agent

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=/etc/sentinel /var/log
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
EOF

# Create config template with placeholders for embedding at download time
cat > "$DEB_ROOT/etc/sentinel/config.json" << 'EOF'
{
  "serverUrl": "__SERVERURL__",
  "enrollmentToken": "__TOKEN__"
}
EOF
chmod 600 "$DEB_ROOT/etc/sentinel/config.json"

# Create conffiles to mark config as configuration file (won't be overwritten on upgrade)
cat > "$DEB_ROOT/DEBIAN/conffiles" << 'EOF'
/etc/sentinel/config.json
EOF

# Create postinst script
cat > "$DEB_ROOT/DEBIAN/postinst" << 'EOF'
#!/bin/bash
set -e

# Reload systemd to pick up new service file
systemctl daemon-reload

# Enable and start the service
systemctl enable sentinel-agent
systemctl start sentinel-agent

echo "Sentinel Agent installed and started successfully"
exit 0
EOF
chmod +x "$DEB_ROOT/DEBIAN/postinst"

# Create prerm script
cat > "$DEB_ROOT/DEBIAN/prerm" << 'EOF'
#!/bin/bash
set -e

# Stop the service before removal
systemctl stop sentinel-agent || true
systemctl disable sentinel-agent || true

exit 0
EOF
chmod +x "$DEB_ROOT/DEBIAN/prerm"

# Create postrm script
cat > "$DEB_ROOT/DEBIAN/postrm" << 'EOF'
#!/bin/bash
set -e

# Reload systemd after service file removal
systemctl daemon-reload

# Clean up config directory on purge
if [ "$1" = "purge" ]; then
    rm -rf /etc/sentinel
fi

exit 0
EOF
chmod +x "$DEB_ROOT/DEBIAN/postrm"

# Build the DEB
echo "Building DEB..."
dpkg-deb --build --root-owner-group "$DEB_ROOT" "$OUTPUT"

if [ -f "$OUTPUT" ]; then
    SIZE=$(du -h "$OUTPUT" | cut -f1)
    echo "Success: Built $OUTPUT ($SIZE)"

    # Create version-agnostic symlink
    ln -sf "$(basename "$OUTPUT")" "$OUTPUT_LINK"
    echo "Created symlink: $OUTPUT_LINK"
else
    echo "Error: Failed to create DEB"
    exit 1
fi

# Clean up
rm -rf "$DEB_ROOT"

echo "Done!"
echo ""
echo "Install with: sudo dpkg -i $OUTPUT"
