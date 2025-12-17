//go:build !windows
// +build !windows

package crypto

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// GetMachineID returns a machine-specific identifier for Unix systems
func GetMachineID() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return getMacOSMachineID()
	default: // linux and other unix
		return getLinuxMachineID()
	}
}

// getLinuxMachineID reads /etc/machine-id
func getLinuxMachineID() (string, error) {
	// Try /etc/machine-id first
	data, err := os.ReadFile("/etc/machine-id")
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}

	// Fallback to /var/lib/dbus/machine-id
	data, err = os.ReadFile("/var/lib/dbus/machine-id")
	if err != nil {
		return "", fmt.Errorf("failed to read machine-id: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}

// getMacOSMachineID retrieves the IOPlatformUUID using ioreg command
func getMacOSMachineID() (string, error) {
	// Use ioreg to get the IOPlatformUUID
	cmd := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to execute ioreg: %w", err)
	}

	// Parse the output to extract IOPlatformUUID
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "IOPlatformUUID") {
			// Format: "IOPlatformUUID" = "XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX"
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				uuid := strings.TrimSpace(parts[1])
				uuid = strings.Trim(uuid, "\"")
				return uuid, nil
			}
		}
	}

	return "", fmt.Errorf("IOPlatformUUID not found in ioreg output")
}
