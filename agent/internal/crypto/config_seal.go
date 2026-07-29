package crypto

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sentinel/agent/internal/ipc"
	"github.com/sentinel/agent/internal/paths"
)

// The config AES-256-GCM key is no longer derived purely from world-readable
// machine identifiers (MachineGuid + hostname). Those are readable by any local
// user, which made config.json — and the enrollment token it holds — trivially
// decryptable (AG-H3). Instead we mix in a 32-byte random "machine secret" that
// is stored in a SYSTEM+Administrators-only DACL-protected file and, on Windows,
// additionally sealed at rest with DPAPI machine scope. The cipher is unchanged;
// only the key source is hardened.
const (
	machineSecretFileName = "config-secret.dat"
	machineSecretSize     = 32

	// sealMagic prefixes a DPAPI-sealed blob so unsealing can distinguish a
	// sealed payload from a legacy plaintext one (enabling migration).
	sealMagic = "SNTLSEAL\x01"
)

// KeyStoreDir returns the directory that holds the machine secret. It defaults
// to the agent data directory (same location as config.json) and is a var so
// tests can redirect it to a writable temp directory.
var KeyStoreDir = func() string { return paths.DataDir() }

var (
	machineSecret     []byte
	machineSecretOnce sync.Once
	machineSecretErr  error
)

// machineSecretPath returns the absolute path to the machine secret file.
func machineSecretPath() string {
	return filepath.Join(KeyStoreDir(), machineSecretFileName)
}

// loadOrCreateMachineSecret loads the persisted machine secret, unsealing it on
// read, or generates and seals a fresh one on first use. The secret file is
// written with a SYSTEM+Administrators-only DACL and the write fails hard if the
// DACL cannot be applied (no world-readable fallback).
func loadOrCreateMachineSecret() ([]byte, error) {
	path := machineSecretPath()

	if raw, err := os.ReadFile(path); err == nil {
		secret, uerr := UnsealMachineData(raw)
		if uerr == nil && len(secret) == machineSecretSize {
			return secret, nil
		}
		// Corrupt or truncated secret — fall through and regenerate. Any
		// config encrypted under the old (unrecoverable) secret is handled by
		// the legacy-key migration path in DecryptConfig.
	}

	secret := make([]byte, machineSecretSize)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("failed to generate machine secret: %w", err)
	}

	if err := ipc.EnsureSecureDir(KeyStoreDir(), 0700); err != nil {
		return nil, fmt.Errorf("failed to prepare key store directory: %w", err)
	}

	sealed, err := SealMachineData(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to seal machine secret: %w", err)
	}

	if err := ipc.SecureWriteFileStrict(path, sealed, 0600); err != nil {
		return nil, fmt.Errorf("failed to persist machine secret: %w", err)
	}

	return secret, nil
}

// getMachineSecret returns the cached machine secret, loading or generating it
// on first call.
func getMachineSecret() ([]byte, error) {
	machineSecretOnce.Do(func() {
		machineSecret, machineSecretErr = loadOrCreateMachineSecret()
	})
	return machineSecret, machineSecretErr
}
