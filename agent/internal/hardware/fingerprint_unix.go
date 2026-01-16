//go:build linux || darwin

package hardware

import (
	"crypto/rand"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// getMachineID returns the machine ID from /etc/machine-id (Linux)
// or the Hardware UUID (macOS).
func getMachineID() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return getLinuxMachineID()
	case "darwin":
		return getDarwinHardwareUUID()
	default:
		return "", nil
	}
}

// getLinuxMachineID reads /etc/machine-id which is stable across reboots
// but may change on OS reinstall.
func getLinuxMachineID() (string, error) {
	// Try /etc/machine-id first (systemd)
	data, err := os.ReadFile("/etc/machine-id")
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}

	// Fallback to /var/lib/dbus/machine-id
	data, err = os.ReadFile("/var/lib/dbus/machine-id")
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}

	return "", err
}

// getDarwinHardwareUUID returns the Hardware UUID from macOS system_profiler.
func getDarwinHardwareUUID() (string, error) {
	cmd := exec.Command("system_profiler", "SPHardwareDataType")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, "Hardware UUID:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	return "", nil
}

// getBIOSSerial returns the system serial number.
func getBIOSSerial() (string, error) {
	switch runtime.GOOS {
	case "linux":
		// Try reading from DMI
		data, err := os.ReadFile("/sys/class/dmi/id/product_serial")
		if err == nil {
			serial := strings.TrimSpace(string(data))
			if serial != "" && serial != "None" && serial != "To be filled by O.E.M." {
				return serial, nil
			}
		}

		// Fallback to dmidecode (requires root)
		cmd := exec.Command("dmidecode", "-s", "system-serial-number")
		output, err := cmd.Output()
		if err == nil {
			serial := strings.TrimSpace(string(output))
			if serial != "" && serial != "None" {
				return serial, nil
			}
		}

	case "darwin":
		cmd := exec.Command("system_profiler", "SPHardwareDataType")
		output, err := cmd.Output()
		if err == nil {
			for _, line := range strings.Split(string(output), "\n") {
				if strings.Contains(line, "Serial Number") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						return strings.TrimSpace(parts[1]), nil
					}
				}
			}
		}
	}

	return "", nil
}

// cryptoRandRead wraps crypto/rand.Read
func cryptoRandRead(b []byte) (int, error) {
	return rand.Read(b)
}
