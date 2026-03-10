//go:build windows

package service

import (
	"fmt"
	"log"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

// isWindowsAdmin checks if the current process has administrator privileges
func isWindowsAdmin() bool {
	var sid *windows.SID

	// Although this looks scary, it is directly copied from the
	// temporary fix in https://github.com/golang/go/issues/28804#issuecomment-438838144
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}

	return member
}

// waitForWatchdogStopped polls the Windows Service Control Manager until the
// SentinelWatchdog service is stopped or no longer exists, with a timeout.
func waitForWatchdogStopped(timeout, interval time.Duration) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to SCM: %w", err)
	}
	defer m.Disconnect()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s, err := m.OpenService("SentinelWatchdog")
		if err != nil {
			// Service doesn't exist — treat as stopped
			log.Println("Watchdog service not found in SCM (already removed)")
			return nil
		}

		status, err := s.Query()
		s.Close()
		if err != nil {
			// Can't query — service may be in the process of being removed
			log.Printf("Warning: could not query watchdog status: %v", err)
			return nil
		}

		if status.State == windows.SERVICE_STOPPED {
			log.Println("Watchdog service confirmed stopped")
			return nil
		}

		log.Printf("Watchdog state: %d, waiting for stop...", status.State)
		time.Sleep(interval)
	}

	return fmt.Errorf("watchdog did not stop within %v", timeout)
}
