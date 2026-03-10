//go:build !windows

package service

import "time"

// isWindowsAdmin is a no-op on non-Windows systems
func isWindowsAdmin() bool {
	return false
}

// waitForWatchdogStopped on non-Windows simply waits a brief period.
// Linux/macOS use systemd/launchd which handle stop synchronously.
func waitForWatchdogStopped(timeout, interval time.Duration) error {
	time.Sleep(1 * time.Second)
	return nil
}

// removeDefenderExclusions is a no-op on non-Windows systems.
// Windows Defender is only present on Windows. (I-10)
func removeDefenderExclusions(installPath string) {
	// No-op: Windows Defender exclusions only apply to Windows
}
