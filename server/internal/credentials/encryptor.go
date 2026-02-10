package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

var (
	ErrInvalidKeyLength = errors.New("master key must be at least 32 bytes")
	ErrDecryptionFailed = errors.New("decryption failed: invalid ciphertext")
	ErrInvalidCiphertext = errors.New("ciphertext too short")
)

// KeyEncryptor handles AES-256-GCM encryption for credential storage
type KeyEncryptor struct {
	key []byte
}

// NewKeyEncryptor creates a new encryptor with the given master key
// The master key should be at least 32 bytes and derived from a secure source
func NewKeyEncryptor(masterKey []byte) (*KeyEncryptor, error) {
	if len(masterKey) < 32 {
		return nil, ErrInvalidKeyLength
	}

	// Derive a 32-byte key using HKDF for additional security
	hkdfReader := hkdf.New(sha256.New, masterKey, nil, []byte("sentinel-credential-encryption-v1"))
	derivedKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, derivedKey); err != nil {
		return nil, fmt.Errorf("failed to derive key: %w", err)
	}

	return &KeyEncryptor{key: derivedKey}, nil
}

// Encrypt encrypts data using AES-256-GCM
// Returns: nonce (12 bytes) || ciphertext || tag (16 bytes)
func (e *KeyEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and prepend nonce
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts data encrypted with Encrypt
func (e *KeyEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrInvalidCiphertext
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// DeriveKeyFromPassword derives a master key from a password using HKDF
// This can be used if the master key is stored as a password/passphrase
func DeriveKeyFromPassword(password string, salt []byte) ([]byte, error) {
	if len(salt) < 16 {
		return nil, errors.New("salt must be at least 16 bytes")
	}

	hkdfReader := hkdf.New(sha256.New, []byte(password), salt, []byte("sentinel-master-key-v1"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, fmt.Errorf("failed to derive key: %w", err)
	}

	return key, nil
}
