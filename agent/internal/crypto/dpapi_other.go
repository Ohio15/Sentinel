//go:build !windows

package crypto

import "bytes"

// On non-Windows platforms there is no DPAPI. Secret material at rest is
// protected by filesystem permissions (0600) and the SYSTEM+Administrators-only
// DACL parity applied by ipc.SecureWriteFileStrict. Sealing is therefore an
// identity transform here, documented as best-effort: the private key / machine
// secret is stored in cleartext behind restrictive permissions rather than an
// OS-native keystore. UnsealMachineData still strips a seal prefix if one is
// somehow present so blobs remain portable.

// SealMachineData returns the plaintext unchanged (no OS keystore available).
func SealMachineData(plaintext []byte) ([]byte, error) {
	return plaintext, nil
}

// UnsealMachineData returns the data unchanged, stripping a seal prefix if
// present (should not occur on this platform, but kept for portability).
func UnsealMachineData(data []byte) ([]byte, error) {
	if bytes.HasPrefix(data, []byte(sealMagic)) {
		return data[len(sealMagic):], nil
	}
	return data, nil
}

// IsSealed reports whether data carries the seal prefix.
func IsSealed(data []byte) bool {
	return bytes.HasPrefix(data, []byte(sealMagic))
}
