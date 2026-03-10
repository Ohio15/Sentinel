//go:build !windows

package ipc

import "os"

// secureWriteFile writes data to a file with restrictive permissions on non-Windows platforms.
func secureWriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

// secureDirectory is a no-op on non-Windows platforms since Unix permissions are sufficient.
func secureDirectory(path string) {
	// Unix permissions set by MkdirAll are sufficient
}
