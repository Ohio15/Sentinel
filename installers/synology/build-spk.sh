#!/bin/bash
#
# Build script for Sentinel Agent .spk package (Synology)
#
# Usage: ./build-spk.sh [version] [arch]
# Example: ./build-spk.sh 1.70.0 amd64
#
# Architectures:
#   amd64 - x86_64 Synology NAS (most desktop models)
#   arm64 - ARM64 Synology NAS (some newer models)
#
# NOTE: Make this script executable on Linux:
#   chmod +x build-spk.sh

set -e

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="${1:-1.0.0}"
ARCH="${2:-amd64}"
PACKAGE_NAME="sentinel-agent"
OUTPUT_DIR="${SCRIPT_DIR}/output"
BUILD_DIR="${SCRIPT_DIR}/build-spk"

# Map architecture to Synology arch names
case "$ARCH" in
    amd64|x86_64)
        SYNOLOGY_ARCH="x86_64"
        GO_ARCH="amd64"
        ;;
    arm64|aarch64)
        SYNOLOGY_ARCH="armv8"
        GO_ARCH="arm64"
        ;;
    *)
        echo "ERROR: Unsupported architecture: $ARCH"
        echo "Supported: amd64, arm64"
        exit 1
        ;;
esac

# Source binary locations
AGENT_BINARY="${SCRIPT_DIR}/../sentinel-agent-linux-${GO_ARCH}"
WATCHDOG_BINARY="${SCRIPT_DIR}/../sentinel-watchdog-linux-${GO_ARCH}"

echo "=============================================="
echo "Building Sentinel Agent .spk package"
echo "=============================================="
echo "Version: ${VERSION}"
echo "Architecture: ${SYNOLOGY_ARCH} (${GO_ARCH})"
echo ""

# Verify source binaries exist
if [ ! -f "${AGENT_BINARY}" ]; then
    echo "ERROR: Agent binary not found: ${AGENT_BINARY}"
    echo "Build the Go binaries first with:"
    echo "  GOOS=linux GOARCH=${GO_ARCH} go build -o installers/sentinel-agent-linux-${GO_ARCH} ./cmd/sentinel-agent"
    exit 1
fi

# Clean previous build
rm -rf "${BUILD_DIR}"
mkdir -p "${BUILD_DIR}"
mkdir -p "${OUTPUT_DIR}"

# Create package directory structure
PACKAGE_DIR="${BUILD_DIR}/package"
mkdir -p "${PACKAGE_DIR}"

echo "Creating package structure..."

# Copy binaries to package
cp "${AGENT_BINARY}" "${PACKAGE_DIR}/sentinel-agent"
chmod 755 "${PACKAGE_DIR}/sentinel-agent"

if [ -f "${WATCHDOG_BINARY}" ]; then
    cp "${WATCHDOG_BINARY}" "${PACKAGE_DIR}/sentinel-watchdog"
    chmod 755 "${PACKAGE_DIR}/sentinel-watchdog"
fi

# Create default config template
cat > "${PACKAGE_DIR}/config.json" << 'CONFIGEOF'
{
  "server_url": "%%SERVER_URL%%",
  "grpc_endpoint": "%%GRPC_ENDPOINT%%",
  "enrollment_token": "%%ENROLLMENT_TOKEN%%",
  "organization_id": "%%ORGANIZATION_ID%%"
}
CONFIGEOF
chmod 640 "${PACKAGE_DIR}/config.json"

# Create package.tgz
echo "Creating package.tgz..."
cd "${PACKAGE_DIR}"
tar -czf "${BUILD_DIR}/package.tgz" *
cd "${SCRIPT_DIR}"

# Create scripts directory
mkdir -p "${BUILD_DIR}/scripts"
cp "${SCRIPT_DIR}/scripts/start-stop-status" "${BUILD_DIR}/scripts/"
cp "${SCRIPT_DIR}/scripts/postinst" "${BUILD_DIR}/scripts/"
cp "${SCRIPT_DIR}/scripts/preuninst" "${BUILD_DIR}/scripts/"
chmod 755 "${BUILD_DIR}/scripts/"*

# Create INFO file
echo "Creating INFO file..."
sed -e "s/%%VERSION%%/${VERSION}/g" \
    -e "s/%%ARCH%%/${SYNOLOGY_ARCH}/g" \
    "${SCRIPT_DIR}/INFO.template" > "${BUILD_DIR}/INFO"

# Create the SPK file (it's just a tar archive)
echo "Building .spk package..."
OUTPUT_FILE="${OUTPUT_DIR}/${PACKAGE_NAME}-${VERSION}-${SYNOLOGY_ARCH}.spk"
cd "${BUILD_DIR}"
tar -cf "${OUTPUT_FILE}" INFO package.tgz scripts
cd "${SCRIPT_DIR}"

# Verify the package
echo ""
echo "Verifying package..."
tar -tvf "${OUTPUT_FILE}"

# Clean up build directory
rm -rf "${BUILD_DIR}"

echo ""
echo "=============================================="
echo "Build complete!"
echo "=============================================="
echo "Package: ${OUTPUT_FILE}"
echo ""
echo "Install on Synology NAS:"
echo "  1. Open Package Center"
echo "  2. Click 'Manual Install'"
echo "  3. Browse to ${OUTPUT_FILE}"
echo "  4. Follow the installation wizard"
echo ""
echo "Or install via SSH:"
echo "  sudo synopkg install ${OUTPUT_FILE}"
echo ""
