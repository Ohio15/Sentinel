package crypto

import (
	"fmt"
	"os"
)

// MigrateConfigFile migrates an unencrypted config file to encrypted format
// This function can be called standalone for manual migration
func MigrateConfigFile(configPath string) error {
	// Read the config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Check if already encrypted
	if IsEncrypted(data) {
		return fmt.Errorf("config file is already encrypted")
	}

	// Encrypt the data
	encryptedData, err := EncryptConfig(data)
	if err != nil {
		return fmt.Errorf("failed to encrypt config: %w", err)
	}

	// Create backup of original file
	backupPath := configPath + ".backup"
	if err := os.WriteFile(backupPath, data, 0600); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	// Write encrypted data
	if err := os.WriteFile(configPath, encryptedData, 0600); err != nil {
		return fmt.Errorf("failed to write encrypted config: %w", err)
	}

	return nil
}

// DecryptConfigFile decrypts a config file and writes the plaintext version
// This is useful for debugging or emergency recovery
// WARNING: This should only be used for troubleshooting
func DecryptConfigFile(configPath, outputPath string) error {
	// Read the config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Check if encrypted
	if !IsEncrypted(data) {
		return fmt.Errorf("config file is not encrypted")
	}

	// Decrypt the data
	decryptedData, err := DecryptConfig(data)
	if err != nil {
		return fmt.Errorf("failed to decrypt config: %w", err)
	}

	// Write decrypted data
	if err := os.WriteFile(outputPath, decryptedData, 0600); err != nil {
		return fmt.Errorf("failed to write decrypted config: %w", err)
	}

	return nil
}

// VerifyConfigFile checks if a config file can be successfully decrypted
func VerifyConfigFile(configPath string) error {
	// Read the config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Check if encrypted
	if !IsEncrypted(data) {
		return fmt.Errorf("config file is not encrypted")
	}

	// Try to decrypt
	_, err = DecryptConfig(data)
	if err != nil {
		return fmt.Errorf("failed to decrypt config: %w", err)
	}

	return nil
}
