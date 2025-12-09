//go:build windows

package collector

import (
	"os"
	"os/exec"
	"strings"
)

// getHardwareInfo returns serial number, manufacturer, and model on Windows
func (c *Collector) getHardwareInfo() (serialNumber, manufacturer, model string) {
	// Get BIOS serial number
	cmd := exec.Command("wmic", "bios", "get", "SerialNumber", "/format:value")
	output, err := cmd.Output()
	if err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			if strings.HasPrefix(line, "SerialNumber=") {
				serialNumber = strings.TrimSpace(strings.TrimPrefix(line, "SerialNumber="))
				break
			}
		}
	}

	// Get system manufacturer and model
	cmd = exec.Command("wmic", "computersystem", "get", "Manufacturer,Model", "/format:csv")
	output, err = cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for i, line := range lines {
			// Skip header and empty lines
			if i == 0 || strings.TrimSpace(line) == "" || strings.HasPrefix(line, "Node") {
				continue
			}
			parts := strings.Split(line, ",")
			if len(parts) >= 3 {
				manufacturer = strings.TrimSpace(parts[1])
				model = strings.TrimSpace(parts[2])
				break
			}
		}
	}

	return serialNumber, manufacturer, model
}

// getDomainInfo returns the computer's domain or workgroup on Windows
func (c *Collector) getDomainInfo() string {
	// Try USERDOMAIN first (usually the domain name)
	domain := os.Getenv("USERDOMAIN")
	if domain != "" && domain != os.Getenv("COMPUTERNAME") {
		return domain
	}

	// Try to get domain from wmic
	cmd := exec.Command("wmic", "computersystem", "get", "Domain", "/format:value")
	output, err := cmd.Output()
	if err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			if strings.HasPrefix(line, "Domain=") {
				domain = strings.TrimSpace(strings.TrimPrefix(line, "Domain="))
				if domain != "" {
					return domain
				}
			}
		}
	}

	// Fallback to workgroup
	cmd = exec.Command("wmic", "computersystem", "get", "Workgroup", "/format:value")
	output, err = cmd.Output()
	if err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			if strings.HasPrefix(line, "Workgroup=") {
				return strings.TrimSpace(strings.TrimPrefix(line, "Workgroup="))
			}
		}
	}

	return ""
}
