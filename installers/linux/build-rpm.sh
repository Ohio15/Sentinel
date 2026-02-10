#!/bin/bash
#
# Build script for Sentinel Agent .rpm package
#
# Usage: ./build-rpm.sh [version]
# Example: ./build-rpm.sh 1.70.0
#
# Requirements:
#   - rpmbuild (from rpm-build package)
#   - rpm (for verification)
#
# Install requirements:
#   Fedora/RHEL: sudo dnf install rpm-build
#   CentOS: sudo yum install rpm-build
#
# NOTE: Make this script executable on Linux:
#   chmod +x build-rpm.sh

set -e

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="${1:-1.0.0}"
RELEASE="1"
PACKAGE_NAME="sentinel-agent"
ARCH="x86_64"
OUTPUT_DIR="${SCRIPT_DIR}/output"

# Source binary locations
AGENT_BINARY="${SCRIPT_DIR}/../sentinel-agent-linux-amd64"
WATCHDOG_BINARY="${SCRIPT_DIR}/../sentinel-watchdog-linux-amd64"

echo "=============================================="
echo "Building Sentinel Agent .rpm package"
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

# Create RPM build directory structure
RPM_BUILD_DIR="${SCRIPT_DIR}/rpmbuild"
rm -rf "${RPM_BUILD_DIR}"
mkdir -p "${RPM_BUILD_DIR}"/{BUILD,RPMS,SOURCES,SPECS,SRPMS}
mkdir -p "${OUTPUT_DIR}"

echo "Setting up RPM build environment..."

# Copy source files to SOURCES
cp "${AGENT_BINARY}" "${RPM_BUILD_DIR}/SOURCES/sentinel-agent-linux-amd64"
cp "${WATCHDOG_BINARY}" "${RPM_BUILD_DIR}/SOURCES/sentinel-watchdog-linux-amd64"
cp "${SCRIPT_DIR}/systemd/sentinel-agent.service" "${RPM_BUILD_DIR}/SOURCES/"
cp "${SCRIPT_DIR}/systemd/sentinel-watchdog.service" "${RPM_BUILD_DIR}/SOURCES/"

# Process spec file with version
sed "s/%%VERSION%%/${VERSION}/g" "${SCRIPT_DIR}/rpm/sentinel-agent.spec" > "${RPM_BUILD_DIR}/SPECS/sentinel-agent.spec"

echo "Building .rpm package..."

# Build the RPM
rpmbuild --define "_topdir ${RPM_BUILD_DIR}" \
         --define "version ${VERSION}" \
         --define "release ${RELEASE}" \
         -bb "${RPM_BUILD_DIR}/SPECS/sentinel-agent.spec"

# Find and copy the built RPM
BUILT_RPM=$(find "${RPM_BUILD_DIR}/RPMS" -name "*.rpm" -type f | head -1)

if [ -z "${BUILT_RPM}" ]; then
    echo "ERROR: RPM build failed - no output file found"
    exit 1
fi

OUTPUT_FILE="${OUTPUT_DIR}/${PACKAGE_NAME}-${VERSION}-${RELEASE}.${ARCH}.rpm"
cp "${BUILT_RPM}" "${OUTPUT_FILE}"

# Verify the package
echo ""
echo "Verifying package..."
rpm -qip "${OUTPUT_FILE}"

# Clean up build directory
rm -rf "${RPM_BUILD_DIR}"

echo ""
echo "=============================================="
echo "Build complete!"
echo "=============================================="
echo "Package: ${OUTPUT_FILE}"
echo ""
echo "Install with:"
echo "  sudo rpm -i ${OUTPUT_FILE}"
echo ""
echo "Or on systems with dnf/yum:"
echo "  sudo dnf install ${OUTPUT_FILE}"
echo "  sudo yum install ${OUTPUT_FILE}"
echo ""
