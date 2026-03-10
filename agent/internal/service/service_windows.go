//go:build windows

package service

import (
	"fmt"
	"log"
	"os/exec"
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

// removeDefenderExclusions removes Windows Defender exclusions that were added during install.
// This is best-effort: failures are logged but do not block the uninstall. (I-10)
func removeDefenderExclusions(installPath string) {
	log.Printf("Removing Windows Defender exclusions for %s...", installPath)

	// Remove folder exclusion
	cmd := exec.Command("powershell", "-Command",
		fmt.Sprintf("Remove-MpPreference -ExclusionPath '%s' -ErrorAction SilentlyContinue", installPath))
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: failed to remove Defender path exclusion: %v", err)
	}

	// Remove process exclusions
	if err := exec.Command("powershell", "-Command",
		"Remove-MpPreference -ExclusionProcess 'sentinel-agent.exe' -ErrorAction SilentlyContinue").Run(); err != nil {
		log.Printf("Warning: failed to remove Defender process exclusion for sentinel-agent.exe: %v", err)
	}
	if err := exec.Command("powershell", "-Command",
		"Remove-MpPreference -ExclusionProcess 'sentinel-watchdog.exe' -ErrorAction SilentlyContinue").Run(); err != nil {
		log.Printf("Warning: failed to remove Defender process exclusion for sentinel-watchdog.exe: %v", err)
	}

	log.Println("Windows Defender exclusion removal complete")
}
