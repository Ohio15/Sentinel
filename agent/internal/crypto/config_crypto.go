package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

const (
	// Magic bytes to identify encrypted config: "SNTL" + version byte
	magicBytes     = "SNTL"
	currentVersion = byte(1)
	nonceSize      = 12 // GCM standard nonce size
)

// GetMachineID is implemented in platform-specific files
// See config_crypto_windows.go and config_crypto_unix.go

// DeriveKey derives a 32-byte encryption key from machine-specific data
func DeriveKey() ([]byte, error) {
	// Get machine ID
	machineID, err := GetMachineID()
	if err != nil {
		return nil, fmt.Errorf("failed to get machine ID: %w", err)
	}

	// Get hostname
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}

	// Combine machine ID and hostname, then hash with SHA-256
	data := machineID + hostname
	hash := sha256.Sum256([]byte(data))

	return hash[:], nil
}

// EncryptConfig encrypts configuration data using AES-256-GCM
// Format: [4 bytes magic "SNTL"][1 byte version][12 bytes nonce][encrypted data][16 bytes auth tag]
func EncryptConfig(data []byte) ([]byte, error) {
	// Derive encryption key
	key, err := DeriveKey()
	if err != nil {
		return nil, fmt.Errorf("failed to derive key: %w", err)
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt data
	ciphertext := gcm.Seal(nil, nonce, data, nil)

	// Build final format: magic + version + nonce + ciphertext (includes auth tag)
	result := make([]byte, 0, len(magicBytes)+1+nonceSize+len(ciphertext))
	result = append(result, []byte(magicBytes)...)
	result = append(result, currentVersion)
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

// DecryptConfig decrypts configuration data encrypted with EncryptConfig
func DecryptConfig(data []byte) ([]byte, error) {
	// Minimum size: magic(4) + version(1) + nonce(12) + tag(16) = 33 bytes
	minSize := len(magicBytes) + 1 + nonceSize + 16
	if len(data) < minSize {
		return nil, fmt.Errorf("data too short to be encrypted config")
	}

	// Check magic bytes
	if string(data[:len(magicBytes)]) != magicBytes {
		return nil, fmt.Errorf("invalid magic bytes")
	}

	offset := len(magicBytes)

	// Check version
	version := data[offset]
	if version != currentVersion {
		return nil, fmt.Errorf("unsupported version: %d", version)
	}
	offset++

	// Extract nonce
	nonce := data[offset : offset+nonceSize]
	offset += nonceSize

	// Extract ciphertext (includes auth tag)
	ciphertext := data[offset:]

	// Derive encryption key
	key, err := DeriveKey()
	if err != nil {
		return nil, fmt.Errorf("failed to derive key: %w", err)
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt data
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

// IsEncrypted checks if the data appears to be encrypted config
func IsEncrypted(data []byte) bool {
	if len(data) < len(magicBytes)+1 {
		return false
	}
	return string(data[:len(magicBytes)]) == magicBytes
}
