//go:build !windows

package ipc

import (
	"fmt"
	"os"
)

// secureWriteFile writes data to a file with restrictive permissions on non-Windows platforms.
func secureWriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

// secureDirectory is a no-op on non-Windows platforms since Unix permissions are sufficient.
func secureDirectory(path string) {
	// Unix permissions set by MkdirAll are sufficient
}

// SecureWriteFileStrict writes data with restrictive 0600-style permissions.
// On non-Windows platforms Unix mode bits are enforced by the filesystem, so
// this is equivalent to a permission-checked WriteFile. It is documented as
// best-effort parity with the Windows DACL path: the caller MUST still pass a
// mode with no group/other bits for secret files.
func SecureWriteFileStrict(path string, data []byte, perm os.FileMode) error {
	// Exclusive create so a secret is never written into a pre-existing (possibly
	// attacker-pre-created) file. Remove any file we own, then O_EXCL-create; a
	// racing re-creation fails closed rather than leaking into the other file.
	_ = os.Remove(path)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("refusing to write secret %s: exclusive create failed: %w", path, err)
	}
	if _, werr := f.Write(data); werr != nil {
		f.Close()
		_ = os.Remove(path)
		return werr
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(path)
		return cerr
	}
	// Enforce the mode explicitly in case umask narrowed the create perm.
	if err := os.Chmod(path, perm); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("failed to enforce permissions on %s: %w", path, err)
	}
	return nil
}

// SecureFileStrict enforces restrictive (0600) permissions on an existing file.
func SecureFileStrict(path string) error {
	return os.Chmod(path, 0600)
}

// SecureDirStrict enforces restrictive (0700) permissions on an existing directory.
func SecureDirStrict(path string) error {
	return os.Chmod(path, 0700)
}

// EnsureSecureDir creates the directory tree and enforces 0700 permissions.
func EnsureSecureDir(path string, perm os.FileMode) error {
	if err := os.MkdirAll(path, perm); err != nil {
		return err
	}
	return os.Chmod(path, 0700)
}

// VerifyFileSecurity checks that a file is not group/other accessible. Owner
// verification against a privileged account is a Windows-specific concept;
// on non-Windows platforms this is a best-effort mode-bit check.
func VerifyFileSecurity(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("insecure permissions %v on %s (group/other access must be removed)", info.Mode().Perm(), path)
	}
	return nil
}
