#!/bin/bash
set -e

# Sentinel Agent Build Script
# Cross-compiles the Rust agent for multiple platforms

echo "=========================================="
echo "  Sentinel Agent Build Script"
echo "=========================================="

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Directory setup
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AGENT_DIR="$(dirname "$SCRIPT_DIR")/agent"
BUILD_DIR="$AGENT_DIR/target/release"
OUTPUT_DIR="$AGENT_DIR/dist"

cd "$AGENT_DIR"

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Check if cross is installed
if ! command -v cross &> /dev/null; then
    echo -e "${YELLOW}Installing cross for cross-compilation...${NC}"
    cargo install cross
fi

# Build targets
TARGETS=(
    "x86_64-unknown-linux-gnu"
    "x86_64-pc-windows-gnu"
    "x86_64-apple-darwin"
    "aarch64-unknown-linux-gnu"
)

echo ""
echo -e "${YELLOW}Building agent for multiple platforms...${NC}"
echo ""

for target in "${TARGETS[@]}"; do
    echo -e "Building for ${GREEN}$target${NC}..."

    # Use cross for cross-compilation
    if cross build --release --target "$target" 2>/dev/null; then
        # Copy the built binary to output directory
        case "$target" in
            *windows*)
                cp "target/$target/release/sentinel-agent.exe" "$OUTPUT_DIR/sentinel-agent-$target.exe" 2>/dev/null || true
                ;;
            *)
                cp "target/$target/release/sentinel-agent" "$OUTPUT_DIR/sentinel-agent-$target" 2>/dev/null || true
                ;;
        esac
        echo -e "  ${GREEN}✓ Built successfully${NC}"
    else
        echo -e "  ${YELLOW}⚠ Skipped (target not available)${NC}"
    fi
done

# Build for current platform as fallback
echo -e "Building for ${GREEN}current platform${NC}..."
cargo build --release
case "$(uname -s)" in
    MINGW*|CYGWIN*|MSYS*)
        cp "target/release/sentinel-agent.exe" "$OUTPUT_DIR/" 2>/dev/null || true
        ;;
    *)
        cp "target/release/sentinel-agent" "$OUTPUT_DIR/" 2>/dev/null || true
        ;;
esac
echo -e "  ${GREEN}✓ Built successfully${NC}"

echo ""
echo "=========================================="
echo -e "${GREEN}  Build Complete!${NC}"
echo "=========================================="
echo ""
echo "Built binaries are in: $OUTPUT_DIR"
ls -la "$OUTPUT_DIR"
echo ""
