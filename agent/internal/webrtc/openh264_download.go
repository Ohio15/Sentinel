//go:build windows

package webrtc

import (
	"compress/bzip2"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	openH264Version    = "2.4.1"
	openH264URL        = "https://github.com/cisco/openh264/releases/download/v2.4.1/openh264-2.4.1-win64.dll.bz2"
	openH264SHA256     = "" // Verify after first successful download and fill in
	openH264FileName   = "openh264-2.4.1-win64.dll"
	openH264InstallDir = `C:\ProgramData\Sentinel`
)

// downloadOpenH264 downloads the OpenH264 DLL from Cisco's GitHub releases
// as a last-resort fallback when the DLL is not already present on the system.
func downloadOpenH264() error {
	installPath := filepath.Join(openH264InstallDir, openH264FileName)

	// Check if DLL already exists
	if _, err := os.Stat(installPath); err == nil {
		log.Printf("[openh264] DLL already exists at %s, skipping download", installPath)
		return nil
	}

	// Ensure install directory exists
	if err := os.MkdirAll(openH264InstallDir, 0755); err != nil {
		return fmt.Errorf("failed to create install directory %s: %w\nYou can manually download from %s and place at %s",
			openH264InstallDir, err, openH264URL, installPath)
	}

	log.Printf("[openh264] Downloading OpenH264 v%s from Cisco GitHub...", openH264Version)

	// HTTP GET with 60-second timeout
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(openH264URL)
	if err != nil {
		return fmt.Errorf("failed to download OpenH264 from %s: %w\nYou can manually download from %s and place at %s",
			openH264URL, err, openH264URL, installPath)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download OpenH264: HTTP %d from %s\nYou can manually download from %s and place at %s",
			resp.StatusCode, openH264URL, openH264URL, installPath)
	}

	// Decompress bz2 stream
	bz2Reader := bzip2.NewReader(resp.Body)

	// Write to temp file first for atomic operation
	tmpPath := installPath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file %s: %w\nYou can manually download from %s and place at %s",
			tmpPath, err, openH264URL, installPath)
	}

	// Hash while writing
	hasher := sha256.New()
	writer := io.MultiWriter(tmpFile, hasher)

	_, err = io.Copy(writer, bz2Reader)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to decompress OpenH264 DLL: %w\nYou can manually download from %s and place at %s",
			err, openH264URL, installPath)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write OpenH264 DLL: %w\nYou can manually download from %s and place at %s",
			err, openH264URL, installPath)
	}

	// Compute and log SHA256 for verification
	computedHash := hex.EncodeToString(hasher.Sum(nil))
	log.Printf("[openh264] SHA256 of downloaded DLL: %s", computedHash)

	// Verify hash if a known hash is configured
	if openH264SHA256 != "" && computedHash != openH264SHA256 {
		os.Remove(tmpPath)
		return fmt.Errorf("SHA256 mismatch for OpenH264 DLL: expected %s, got %s\nYou can manually download from %s and place at %s",
			openH264SHA256, computedHash, openH264URL, installPath)
	}

	// Atomic rename from temp to final path
	if err := os.Rename(tmpPath, installPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to move OpenH264 DLL to final path: %w\nYou can manually download from %s and place at %s",
			err, openH264URL, installPath)
	}

	log.Printf("[openh264] OpenH264 DLL downloaded successfully to %s", installPath)
	return nil
}
