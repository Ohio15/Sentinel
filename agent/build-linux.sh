#!/bin/bash
# Build script for Sentinel Agent on Linux
# Prerequisites: Install development libraries for X11, PulseAudio, and Opus

set -e

echo "=== Sentinel Agent Linux Build Script ==="

# Check for required packages
check_package() {
    if ! pkg-config --exists "$1" 2>/dev/null; then
        echo "ERROR: Missing package: $1"
        echo "Install with: sudo apt-get install $2"
        return 1
    fi
    echo "✓ Found $1"
}

echo ""
echo "Checking dependencies..."

# Check for X11 libraries
if ! pkg-config --exists x11 2>/dev/null; then
    echo "ERROR: libx11-dev not found"
    echo "Install with: sudo apt-get install libx11-dev libxext-dev libxtst-dev libxfixes-dev"
    exit 1
fi
echo "✓ X11 libraries found"

# Check for PulseAudio
if ! pkg-config --exists libpulse-simple 2>/dev/null; then
    echo "WARNING: libpulse-dev not found - audio capture will not work"
    echo "Install with: sudo apt-get install libpulse-dev"
    HAS_PULSE=0
else
    echo "✓ PulseAudio found"
    HAS_PULSE=1
fi

# Check for Opus (for audio encoding)
if ! pkg-config --exists opus 2>/dev/null; then
    echo "WARNING: libopus-dev not found - audio encoding will not work"
    echo "Install with: sudo apt-get install libopus-dev"
    HAS_OPUS=0
else
    echo "✓ Opus found"
    HAS_OPUS=1
fi

echo ""
echo "Building Sentinel Agent..."

# Set build flags
export CGO_ENABLED=1
export GOOS=linux
export GOARCH=amd64

# Build the agent
go build -o sentinel-agent ./cmd/sentinel-agent

if [ $? -eq 0 ]; then
    echo ""
    echo "=== Build successful! ==="
    echo "Binary: ./sentinel-agent"
    ls -la sentinel-agent
else
    echo ""
    echo "=== Build failed ==="
    exit 1
fi
