package crypto_test

import (
	"fmt"
	"log"

	"github.com/sentinel/agent/internal/crypto"
)

// ExampleEncryptConfig demonstrates how to encrypt configuration data
func ExampleEncryptConfig() {
	configJSON := []byte(`{
		"agent_id": "test-123",
		"server_url": "https://example.com",
		"enrolled": true
	}`)

	// Encrypt the config
	encrypted, err := crypto.EncryptConfig(configJSON)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Encrypted %d bytes to %d bytes\n", len(configJSON), len(encrypted))
	// Output: Encrypted config data
}

// ExampleDecryptConfig demonstrates how to decrypt configuration data
func ExampleDecryptConfig() {
	configJSON := []byte(`{"test": "data"}`)

	// First encrypt
	encrypted, err := crypto.EncryptConfig(configJSON)
	if err != nil {
		log.Fatal(err)
	}

	// Then decrypt
	decrypted, err := crypto.DecryptConfig(encrypted)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(decrypted))
	// Output: {"test": "data"}
}

// ExampleIsEncrypted demonstrates how to check if data is encrypted
func ExampleIsEncrypted() {
	plaintext := []byte(`{"test": "data"}`)
	encrypted, _ := crypto.EncryptConfig(plaintext)

	fmt.Printf("Plaintext encrypted: %v\n", crypto.IsEncrypted(plaintext))
	fmt.Printf("Ciphertext encrypted: %v\n", crypto.IsEncrypted(encrypted))
	// Output:
	// Plaintext encrypted: false
	// Ciphertext encrypted: true
}

// ExampleDeriveKey demonstrates key derivation
func ExampleDeriveKey() {
	key1, err := crypto.DeriveKey()
	if err != nil {
		log.Fatal(err)
	}

	key2, err := crypto.DeriveKey()
	if err != nil {
		log.Fatal(err)
	}

	// Keys should be deterministic (same on same machine)
	fmt.Printf("Key length: %d bytes\n", len(key1))
	fmt.Printf("Keys match: %v\n", string(key1) == string(key2))
	// Output:
	// Key length: 32 bytes
	// Keys match: true
}
