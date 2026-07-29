package crypto

import (
	"bytes"
	"crypto/rand"
	"os"
	"testing"
)

// TestMain redirects the machine-secret key store to a writable temp directory
// so encryption/decryption tests (and the external example tests compiled into
// the same test binary) do not attempt to write into the privileged agent data
// directory (e.g. /etc/sentinel, C:\ProgramData\Sentinel).
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "sentinel-crypto-test")
	if err != nil {
		panic(err)
	}
	originalKeyStoreDir := KeyStoreDir
	KeyStoreDir = func() string { return tmp }

	// Pre-seed the machine secret so the tests (running as an unprivileged
	// user) do not exercise the production create-path, which applies a
	// SYSTEM+Administrators-only DACL that would lock the test user out of the
	// temp directory. The unseal path accepts a bare 32-byte file (no seal
	// prefix), matching a pre-existing/legacy secret on disk.
	seed := make([]byte, machineSecretSize)
	if _, rerr := rand.Read(seed); rerr != nil {
		panic(rerr)
	}
	if werr := os.WriteFile(machineSecretPath(), seed, 0600); werr != nil {
		panic(werr)
	}

	code := m.Run()

	KeyStoreDir = originalKeyStoreDir
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

func TestEncryptDecrypt(t *testing.T) {
	testData := []byte(`{"test": "data", "number": 123}`)

	// Encrypt
	encrypted, err := EncryptConfig(testData)
	if err != nil {
		t.Fatalf("EncryptConfig failed: %v", err)
	}

	// Verify magic bytes
	if !IsEncrypted(encrypted) {
		t.Fatal("Encrypted data should have magic bytes")
	}

	// Decrypt
	decrypted, err := DecryptConfig(encrypted)
	if err != nil {
		t.Fatalf("DecryptConfig failed: %v", err)
	}

	// Verify data matches
	if !bytes.Equal(testData, decrypted) {
		t.Fatalf("Decrypted data doesn't match original.\nExpected: %s\nGot: %s", testData, decrypted)
	}
}

func TestIsEncrypted(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected bool
	}{
		{
			name:     "empty data",
			data:     []byte{},
			expected: false,
		},
		{
			name:     "short data",
			data:     []byte("SNT"),
			expected: false,
		},
		{
			name:     "unencrypted JSON",
			data:     []byte(`{"test": "data"}`),
			expected: false,
		},
		{
			name:     "encrypted data",
			data:     []byte("SNTL\x01" + "random data"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsEncrypted(tt.data)
			if result != tt.expected {
				t.Errorf("IsEncrypted() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDeriveKey(t *testing.T) {
	// Test that key derivation works
	key1, err := DeriveKey()
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}

	// Verify key length (should be 32 bytes for AES-256)
	if len(key1) != 32 {
		t.Fatalf("Key length should be 32 bytes, got %d", len(key1))
	}

	// Derive key again and verify it's the same (deterministic)
	key2, err := DeriveKey()
	if err != nil {
		t.Fatalf("DeriveKey failed on second call: %v", err)
	}

	if !bytes.Equal(key1, key2) {
		t.Fatal("DeriveKey should be deterministic")
	}
}

func TestDecryptInvalidData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "too short",
			data: []byte("SNTL\x01"),
		},
		{
			name: "wrong magic",
			data: []byte("XXXX\x01" + string(make([]byte, 50))),
		},
		{
			name: "wrong version",
			data: []byte("SNTL\x99" + string(make([]byte, 50))),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecryptConfig(tt.data)
			if err == nil {
				t.Error("DecryptConfig should fail for invalid data")
			}
		})
	}
}

func TestGetMachineID(t *testing.T) {
	// Test that we can get a machine ID
	machineID, err := GetMachineID()
	if err != nil {
		t.Fatalf("GetMachineID failed: %v", err)
	}

	if machineID == "" {
		t.Fatal("GetMachineID returned empty string")
	}

	t.Logf("Machine ID: %s", machineID)
}
