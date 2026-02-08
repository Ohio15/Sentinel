//go:build windows

package collector

import (
	"os"
	"os/exec"
	"strconv"
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

// getDeviceType determines the device type based on chassis type and other indicators
func (c *Collector) getDeviceType(osName, platformFamily, manufacturer, model string) string {
	// Check for virtual machine first
	vmIndicators := []string{"vmware", "virtual", "hyperv", "qemu", "kvm", "xen", "virtualbox", "parallels"}
	lowerManufacturer := strings.ToLower(manufacturer)
	lowerModel := strings.ToLower(model)
	for _, vm := range vmIndicators {
		if strings.Contains(lowerManufacturer, vm) || strings.Contains(lowerModel, vm) {
			return "virtual"
		}
	}

	// Check for Windows Server
	if strings.Contains(strings.ToLower(osName), "server") {
		return "server"
	}

	// Get chassis type from WMI - most reliable indicator for desktop vs laptop
	// ChassisTypes values: https://docs.microsoft.com/en-us/windows/win32/cimwin32prov/win32-systemenclosure
	// 3=Desktop, 4=Low Profile Desktop, 5=Pizza Box, 6=Mini Tower, 7=Tower, 8=Portable
	// 9=Laptop, 10=Notebook, 11=Hand Held, 12=Docking Station, 13=All in One
	// 14=Sub Notebook, 15=Space-Saving, 16=Lunch Box, 17=Main System Chassis
	// 30=Tablet, 31=Convertible, 32=Detachable
	cmd := exec.Command("wmic", "systemenclosure", "get", "ChassisTypes", "/format:value")
	output, err := cmd.Output()
	if err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			if strings.HasPrefix(line, "ChassisTypes=") {
				chassisStr := strings.TrimSpace(strings.TrimPrefix(line, "ChassisTypes="))
				// ChassisTypes is returned as {X} or {X,Y} format
				chassisStr = strings.Trim(chassisStr, "{}")
				parts := strings.Split(chassisStr, ",")
				for _, part := range parts {
					if chassisType, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
						switch chassisType {
						case 8, 9, 10, 14: // Portable, Laptop, Notebook, Sub Notebook
							return "laptop"
						case 30, 31, 32: // Tablet, Convertible, Detachable
							return "tablet"
						case 3, 4, 5, 6, 7, 13, 15, 16, 17: // Desktop variants
							return "desktop"
						case 23: // Rack Mount Chassis - typically servers
							return "server"
						}
					}
				}
			}
		}
	}

	// Check for battery as fallback indicator for laptop
	cmd = exec.Command("wmic", "path", "Win32_Battery", "get", "Status", "/format:value")
	output, err = cmd.Output()
	if err == nil && strings.Contains(string(output), "Status=") {
		// Has battery = likely laptop or tablet
		return "laptop"
	}

	// Check model name for hints
	modelLower := strings.ToLower(model)
	if strings.Contains(modelLower, "laptop") || strings.Contains(modelLower, "notebook") ||
		strings.Contains(modelLower, "thinkpad") || strings.Contains(modelLower, "latitude") ||
		strings.Contains(modelLower, "elitebook") || strings.Contains(modelLower, "probook") ||
		strings.Contains(modelLower, "pavilion") || strings.Contains(modelLower, "inspiron") ||
		strings.Contains(modelLower, "macbook") || strings.Contains(modelLower, "surface") {
		return "laptop"
	}

	if strings.Contains(modelLower, "optiplex") || strings.Contains(modelLower, "prodesk") ||
		strings.Contains(modelLower, "elitedesk") || strings.Contains(modelLower, "thinkcentre") ||
		strings.Contains(modelLower, "imac") || strings.Contains(modelLower, "mac mini") {
		return "desktop"
	}

	if strings.Contains(modelLower, "poweredge") || strings.Contains(modelLower, "proliant") ||
		strings.Contains(modelLower, "bladecenter") {
		return "server"
	}

	// Default to desktop if we can't determine
	return "desktop"
}
