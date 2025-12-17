//go:build windows
// +build windows

package crypto

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// GetMachineID returns a machine-specific identifier for Windows
func GetMachineID() (string, error) {
	return getWindowsMachineID()
}

// getWindowsMachineID retrieves the machine GUID from Windows registry
func getWindowsMachineID() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("failed to open registry key: %w", err)
	}
	defer k.Close()

	machineGUID, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return "", fmt.Errorf("failed to read MachineGuid: %w", err)
	}

	return strings.TrimSpace(machineGUID), nil
}
