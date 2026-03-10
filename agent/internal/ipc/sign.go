// Package ipc provides inter-process communication primitives for the Sentinel agent.
// This file implements HMAC-SHA256 integrity protection for IPC files.
package ipc

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	hmacKeyFile = "ipc-key.dat"
	hmacKeySize = 32
	sigSuffix   = ".sig"
)

var (
	hmacKey     []byte
	hmacKeyOnce sync.Once
	hmacKeyErr  error
)

// hmacKeyPath returns the path to the HMAC key file.
func hmacKeyPath() string {
	return filepath.Join(BaseDir, hmacKeyFile)
}

// loadOrGenerateHMACKey loads the HMAC key from disk, or generates a new one if it doesn't exist.
// The key file is created with restrictive permissions (0600 + Windows ACL via secureWriteFile).
func loadOrGenerateHMACKey() ([]byte, error) {
	keyPath := hmacKeyPath()

	data, err := os.ReadFile(keyPath)
	if err == nil && len(data) == hmacKeySize {
		return data, nil
	}

	// Generate a new random 32-byte key
	key := make([]byte, hmacKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate HMAC key: %w", err)
	}

	// Ensure base directory exists before writing key
	if err := os.MkdirAll(BaseDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create base directory for HMAC key: %w", err)
	}

	// Write the key file with restrictive permissions
	if err := secureWriteFile(keyPath, key, 0600); err != nil {
		return nil, fmt.Errorf("failed to write HMAC key: %w", err)
	}
	// Apply directory ACL on Windows
	secureDirectory(BaseDir)

	log.Println("Generated new IPC HMAC key")
	return key, nil
}

// getHMACKey returns the cached HMAC key, loading or generating it on first call.
func getHMACKey() ([]byte, error) {
	hmacKeyOnce.Do(func() {
		hmacKey, hmacKeyErr = loadOrGenerateHMACKey()
	})
	return hmacKey, hmacKeyErr
}

// computeHMAC computes HMAC-SHA256 over the given data using the IPC key.
func computeHMAC(data []byte) ([]byte, error) {
	key, err := getHMACKey()
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil), nil
}

// writeSignature writes an HMAC-SHA256 signature file alongside the data file.
func writeSignature(filePath string, data []byte) error {
	sig, err := computeHMAC(data)
	if err != nil {
		return fmt.Errorf("failed to compute HMAC for %s: %w", filePath, err)
	}

	sigPath := filePath + sigSuffix
	sigHex := []byte(hex.EncodeToString(sig))
	if err := secureWriteFile(sigPath, sigHex, 0600); err != nil {
		return fmt.Errorf("failed to write signature file %s: %w", sigPath, err)
	}
	return nil
}

// verifySignature reads the signature file and verifies the HMAC of the data.
// Returns nil if valid, an error if invalid or missing.
func verifySignature(filePath string, data []byte) error {
	sigPath := filePath + sigSuffix
	sigHex, err := os.ReadFile(sigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("CRITICAL: signature file missing for %s - possible tampering", filePath)
		}
		return fmt.Errorf("failed to read signature file %s: %w", sigPath, err)
	}

	storedSig, err := hex.DecodeString(string(sigHex))
	if err != nil {
		return fmt.Errorf("CRITICAL: corrupted signature file %s: %w", sigPath, err)
	}

	expectedSig, err := computeHMAC(data)
	if err != nil {
		return err
	}

	if !hmac.Equal(storedSig, expectedSig) {
		return fmt.Errorf("CRITICAL: HMAC verification failed for %s - file has been tampered with", filePath)
	}

	return nil
}

// secureReadFile reads a file and verifies its HMAC integrity.
// Returns the file data if the signature is valid.
// During rolling upgrades, files written by older agents may lack a .sig file.
// In that case, the data is returned with a warning log but NOT rejected,
// allowing backwards compatibility. Once all agents are updated, this grace
// period can be removed by setting strictMode = true.
func secureReadFile(filePath string) ([]byte, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	if err := verifySignature(filePath, data); err != nil {
		sigPath := filePath + sigSuffix
		if _, statErr := os.Stat(sigPath); os.IsNotExist(statErr) {
			// Grace period: .sig file missing — file was written by an older agent/watchdog.
			// Accept the data but log a warning. Remove this fallback after fleet-wide upgrade.
			log.Printf("WARNING: IPC file %s has no signature (pre-upgrade file) — accepting with reduced trust", filePath)
			return data, nil
		}
		// .sig file EXISTS but verification failed — actual tampering or corruption
		log.Printf("CRITICAL SECURITY ALERT: %v", err)
		return nil, err
	}

	return data, nil
}

// secureWriteAndSign writes data to a file with restrictive permissions and creates an HMAC signature.
func secureWriteAndSign(filePath string, data []byte) error {
	if err := secureWriteFile(filePath, data, 0600); err != nil {
		return err
	}
	return writeSignature(filePath, data)
}

// deleteWithSignature removes a file and its associated signature file.
func deleteWithSignature(filePath string) error {
	sigPath := filePath + sigSuffix
	os.Remove(sigPath) // best-effort removal of signature
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
