//go:build !windows

package service

// isWindowsAdmin is a no-op on non-Windows systems
func isWindowsAdmin() bool {
	return false
}
