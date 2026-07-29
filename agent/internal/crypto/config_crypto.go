package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
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

// DeriveKey derives the 32-byte AES-256 encryption key for the config.
//
// The key mixes the machine ID and hostname with a 32-byte random machine
// secret that is stored in a SYSTEM+Administrators-only DACL-protected file
// (DPAPI-sealed at rest on Windows). Prior to this change the key was
// SHA256(MachineGuid + hostname) — both world-readable — which reduced config
// "encryption" to obfuscation (AG-H3). The machine secret is the component that
// is not world-readable and makes the key non-derivable by an unprivileged
// local user.
func DeriveKey() ([]byte, error) {
	machineID, err := GetMachineID()
	if err != nil {
		return nil, fmt.Errorf("failed to get machine ID: %w", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}

	secret, err := getMachineSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to load machine secret: %w", err)
	}

	h := sha256.New()
	h.Write([]byte(machineID))
	h.Write([]byte(hostname))
	h.Write(secret)
	return h.Sum(nil), nil
}

// deriveLegacyKey reproduces the pre-hardening key derivation
// (SHA256(machineID + hostname)) so config written under the old scheme can be
// decrypted once and re-sealed under the new key.
func deriveLegacyKey() ([]byte, error) {
	machineID, err := GetMachineID()
	if err != nil {
		return nil, fmt.Errorf("failed to get machine ID: %w", err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}
	hash := sha256.Sum256([]byte(machineID + hostname))
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

// DecryptConfig decrypts configuration data encrypted with EncryptConfig.
// It transparently migrates data encrypted under the legacy world-readable key
// scheme (see DecryptConfigMigrate).
func DecryptConfig(data []byte) ([]byte, error) {
	plaintext, _, err := DecryptConfigMigrate(data)
	return plaintext, err
}

// DecryptConfigMigrate decrypts config data, attempting the current key first
// and falling back to the legacy SHA256(machineID+hostname) key. The returned
// usedLegacy flag is true when the fallback succeeded, signalling the caller to
// re-write the config so it is re-sealed under the current key.
func DecryptConfigMigrate(data []byte) (plaintext []byte, usedLegacy bool, err error) {
	// Minimum size: magic(4) + version(1) + nonce(12) + tag(16) = 33 bytes
	minSize := len(magicBytes) + 1 + nonceSize + 16
	if len(data) < minSize {
		return nil, false, fmt.Errorf("data too short to be encrypted config")
	}

	if string(data[:len(magicBytes)]) != magicBytes {
		return nil, false, fmt.Errorf("invalid magic bytes")
	}

	offset := len(magicBytes)

	version := data[offset]
	if version != currentVersion {
		return nil, false, fmt.Errorf("unsupported version: %d", version)
	}
	offset++

	nonce := data[offset : offset+nonceSize]
	offset += nonceSize
	ciphertext := data[offset:]

	// Attempt the current (hardened) key first.
	if key, derr := DeriveKey(); derr == nil {
		if pt, oerr := gcmOpen(key, nonce, ciphertext); oerr == nil {
			return pt, false, nil
		}
	}

	// Migration: try the legacy key once. A success here means the config was
	// written under the old scheme and must be re-sealed by the caller.
	legacyKey, lerr := deriveLegacyKey()
	if lerr != nil {
		return nil, false, fmt.Errorf("failed to decrypt with current key and legacy key unavailable: %w", lerr)
	}
	pt, oerr := gcmOpen(legacyKey, nonce, ciphertext)
	if oerr != nil {
		return nil, false, fmt.Errorf("failed to decrypt config with current or legacy key")
	}
	log.Println("[CONFIG] Decrypted config under legacy key scheme; it will be re-sealed under the hardened key on next save")
	return pt, true, nil
}

// gcmOpen decrypts ciphertext (including its auth tag) with the given AES key
// and nonce using AES-256-GCM.
func gcmOpen(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// IsEncrypted checks if the data appears to be encrypted config
func IsEncrypted(data []byte) bool {
	if len(data) < len(magicBytes)+1 {
		return false
	}
	return string(data[:len(magicBytes)]) == magicBytes
}
