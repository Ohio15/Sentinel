#!/bin/bash
#
# Build script for Sentinel Agent .deb package
#
# Usage: ./build-deb.sh [version]
# Example: ./build-deb.sh 1.70.0
#
# Requirements:
#   - dpkg-deb (standard on Debian/Ubuntu)
#   - fakeroot (optional, for non-root builds)
#
# NOTE: Make this script executable on Linux:
#   chmod +x build-deb.sh

set -e

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="${1:-1.0.0}"
PACKAGE_NAME="sentinel-agent"
ARCH="amd64"
OUTPUT_DIR="${SCRIPT_DIR}/output"
BUILD_DIR="${SCRIPT_DIR}/build-deb"

# Source binary locations
AGENT_BINARY="${SCRIPT_DIR}/../sentinel-agent-linux-amd64"
WATCHDOG_BINARY="${SCRIPT_DIR}/../sentinel-watchdog-linux-amd64"

echo "=============================================="
echo "Building Sentinel Agent .deb package"
echo "=============================================="
echo "Version: ${VERSION}"
echo "Architecture: ${ARCH}"
echo ""

# Verify source binaries exist
if [ ! -f "${AGENT_BINARY}" ]; then
    echo "ERROR: Agent binary not found: ${AGENT_BINARY}"
    echo "Build the Go binaries first with:"
    echo "  GOOS=linux GOARCH=amd64 go build -o installers/sentinel-agent-linux-amd64 ./cmd/sentinel-agent"
    exit 1
fi

if [ ! -f "${WATCHDOG_BINARY}" ]; then
    echo "ERROR: Watchdog binary not found: ${WATCHDOG_BINARY}"
    echo "Build the Go binaries first with:"
    echo "  GOOS=linux GOARCH=amd64 go build -o installers/sentinel-watchdog-linux-amd64 ./cmd/sentinel-watchdog"
    exit 1
fi

# Clean previous build
rm -rf "${BUILD_DIR}"
mkdir -p "${BUILD_DIR}"
mkdir -p "${OUTPUT_DIR}"

# Create package directory structure
PACKAGE_DIR="${BUILD_DIR}/${PACKAGE_NAME}_${VERSION}_${ARCH}"
mkdir -p "${PACKAGE_DIR}/DEBIAN"
mkdir -p "${PACKAGE_DIR}/usr/local/bin"
mkdir -p "${PACKAGE_DIR}/etc/systemd/system"

echo "Creating package structure..."

# Copy binaries
cp "${AGENT_BINARY}" "${PACKAGE_DIR}/usr/local/bin/sentinel-agent"
cp "${WATCHDOG_BINARY}" "${PACKAGE_DIR}/usr/local/bin/sentinel-watchdog"
chmod 755 "${PACKAGE_DIR}/usr/local/bin/sentinel-agent"
chmod 755 "${PACKAGE_DIR}/usr/local/bin/sentinel-watchdog"

# Copy systemd service files
cp "${SCRIPT_DIR}/systemd/sentinel-agent.service" "${PACKAGE_DIR}/etc/systemd/system/"
cp "${SCRIPT_DIR}/systemd/sentinel-watchdog.service" "${PACKAGE_DIR}/etc/systemd/system/"
chmod 644 "${PACKAGE_DIR}/etc/systemd/system/"*.service

# Process control file with version
sed "s/%%VERSION%%/${VERSION}/g" "${SCRIPT_DIR}/debian/control" > "${PACKAGE_DIR}/DEBIAN/control"

# Copy maintainer scripts
cp "${SCRIPT_DIR}/debian/conffiles" "${PACKAGE_DIR}/DEBIAN/"
cp "${SCRIPT_DIR}/debian/postinst" "${PACKAGE_DIR}/DEBIAN/"
cp "${SCRIPT_DIR}/debian/prerm" "${PACKAGE_DIR}/DEBIAN/"
cp "${SCRIPT_DIR}/debian/postrm" "${PACKAGE_DIR}/DEBIAN/"

# Set script permissions
chmod 755 "${PACKAGE_DIR}/DEBIAN/postinst"
chmod 755 "${PACKAGE_DIR}/DEBIAN/prerm"
chmod 755 "${PACKAGE_DIR}/DEBIAN/postrm"
chmod 644 "${PACKAGE_DIR}/DEBIAN/control"
chmod 644 "${PACKAGE_DIR}/DEBIAN/conffiles"

# Calculate installed size (in KB)
INSTALLED_SIZE=$(du -sk "${PACKAGE_DIR}" | cut -f1)
echo "Installed-Size: ${INSTALLED_SIZE}" >> "${PACKAGE_DIR}/DEBIAN/control"

echo "Building .deb package..."

# Build the package
OUTPUT_FILE="${OUTPUT_DIR}/${PACKAGE_NAME}_${VERSION}_${ARCH}.deb"

if command -v fakeroot &> /dev/null; then
    fakeroot dpkg-deb --build "${PACKAGE_DIR}" "${OUTPUT_FILE}"
else
    dpkg-deb --build "${PACKAGE_DIR}" "${OUTPUT_FILE}"
fi

# Verify the package
echo ""
echo "Verifying package..."
dpkg-deb --info "${OUTPUT_FILE}"

# Clean up build directory
rm -rf "${BUILD_DIR}"

echo ""
echo "=============================================="
echo "Build complete!"
echo "=============================================="
echo "Package: ${OUTPUT_FILE}"
echo ""
echo "Install with:"
echo "  sudo dpkg -i ${OUTPUT_FILE}"
echo ""
echo "Or on systems with apt:"
echo "  sudo apt install ./${PACKAGE_NAME}_${VERSION}_${ARCH}.deb"
echo ""
