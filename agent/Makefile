# Sentinel Agent Makefile
# Build cross-platform agent binaries

VERSION := 1.0.0
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)

# Output directory
OUTPUT_DIR := ../downloads

# Binary names
BINARY_NAME := sentinel-agent
WINDOWS_BINARY := $(BINARY_NAME).exe
MACOS_BINARY := $(BINARY_NAME)-macos
LINUX_BINARY := $(BINARY_NAME)-linux

.PHONY: all clean windows linux macos deps test

all: deps windows linux macos

deps:
	go mod download
	go mod tidy

test:
	go test -v ./...

# Build for Windows (64-bit)
windows:
	@echo "Building for Windows amd64..."
	@mkdir -p $(OUTPUT_DIR)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(OUTPUT_DIR)/$(WINDOWS_BINARY) ./cmd/sentinel-agent
	@echo "Built: $(OUTPUT_DIR)/$(WINDOWS_BINARY)"

# Build for Windows (32-bit)
windows-386:
	@echo "Building for Windows 386..."
	@mkdir -p $(OUTPUT_DIR)
	GOOS=windows GOARCH=386 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME)-386.exe ./cmd/sentinel-agent
	@echo "Built: $(OUTPUT_DIR)/$(BINARY_NAME)-386.exe"

# Build for macOS (Intel)
macos:
	@echo "Building for macOS amd64..."
	@mkdir -p $(OUTPUT_DIR)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(OUTPUT_DIR)/$(MACOS_BINARY) ./cmd/sentinel-agent
	@echo "Built: $(OUTPUT_DIR)/$(MACOS_BINARY)"

# Build for macOS (Apple Silicon)
macos-arm64:
	@echo "Building for macOS arm64..."
	@mkdir -p $(OUTPUT_DIR)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME)-macos-arm64 ./cmd/sentinel-agent
	@echo "Built: $(OUTPUT_DIR)/$(BINARY_NAME)-macos-arm64"

# Build for Linux (64-bit)
linux:
	@echo "Building for Linux amd64..."
	@mkdir -p $(OUTPUT_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(OUTPUT_DIR)/$(LINUX_BINARY) ./cmd/sentinel-agent
	@echo "Built: $(OUTPUT_DIR)/$(LINUX_BINARY)"

# Build for Linux (ARM64)
linux-arm64:
	@echo "Building for Linux arm64..."
	@mkdir -p $(OUTPUT_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/sentinel-agent
	@echo "Built: $(OUTPUT_DIR)/$(BINARY_NAME)-linux-arm64"

# Build for Linux (ARM)
linux-arm:
	@echo "Building for Linux arm..."
	@mkdir -p $(OUTPUT_DIR)
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME)-linux-arm ./cmd/sentinel-agent
	@echo "Built: $(OUTPUT_DIR)/$(BINARY_NAME)-linux-arm"

# Build all platforms
build-all: windows windows-386 macos macos-arm64 linux linux-arm64 linux-arm

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf $(OUTPUT_DIR)/$(BINARY_NAME)*
	go clean

# Install locally (for development)
install:
	go install ./cmd/sentinel-agent

# Run locally
run:
	go run ./cmd/sentinel-agent --server=http://localhost:8080 --token=dev-token

# Generate checksums
checksums:
	@echo "Generating checksums..."
	@cd $(OUTPUT_DIR) && sha256sum $(BINARY_NAME)* > checksums.txt
	@echo "Checksums written to $(OUTPUT_DIR)/checksums.txt"

# UPX compression (requires upx installed)
compress:
	@echo "Compressing binaries with UPX..."
	-upx --best $(OUTPUT_DIR)/$(WINDOWS_BINARY)
	-upx --best $(OUTPUT_DIR)/$(LINUX_BINARY)
	@echo "Note: macOS binaries cannot be compressed with UPX"

# Release build (all platforms + checksums)
release: clean build-all checksums
	@echo "Release build complete!"
	@ls -la $(OUTPUT_DIR)/
