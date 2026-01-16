//go:build windows

package hardware

import (
	"crypto/rand"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// getMachineID returns the Windows Machine GUID from the registry.
// This is the most stable identifier on Windows - it persists across
// reinstalls of the agent and hostname changes, and only changes
// if Windows is reinstalled.
func getMachineID() (string, error) {
	// Open the Cryptography key where MachineGuid is stored
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Cryptography`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return "", err
	}
	defer key.Close()

	// Read the MachineGuid value
	machineGUID, _, err := key.GetStringValue("MachineGuid")
	if err != nil {
		return "", err
	}

	return strings.ToUpper(strings.TrimSpace(machineGUID)), nil
}

// getBIOSSerial returns the BIOS serial number using WMIC.
// This is a hardware-level identifier that doesn't change.
func getBIOSSerial() (string, error) {
	cmd := exec.Command("wmic", "bios", "get", "SerialNumber", "/format:value")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "SerialNumber=") {
			serial := strings.TrimSpace(strings.TrimPrefix(line, "SerialNumber="))
			// Skip common placeholder values
			if serial != "" &&
				serial != "To be filled by O.E.M." &&
				serial != "Default string" &&
				serial != "System Serial Number" &&
				serial != "None" &&
				serial != "0" {
				return serial, nil
			}
		}
	}

	return "", nil
}

// cryptoRandRead wraps crypto/rand.Read
func cryptoRandRead(b []byte) (int, error) {
	return rand.Read(b)
}
