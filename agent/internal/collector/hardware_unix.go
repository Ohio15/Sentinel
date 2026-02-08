//go:build !windows

package collector

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// getHardwareInfo returns serial number, manufacturer, and model on Unix systems
func (c *Collector) getHardwareInfo() (serialNumber, manufacturer, model string) {
	// Try dmidecode (requires root)
	if data, err := exec.Command("dmidecode", "-s", "system-serial-number").Output(); err == nil {
		serialNumber = strings.TrimSpace(string(data))
	}
	if data, err := exec.Command("dmidecode", "-s", "system-manufacturer").Output(); err == nil {
		manufacturer = strings.TrimSpace(string(data))
	}
	if data, err := exec.Command("dmidecode", "-s", "system-product-name").Output(); err == nil {
		model = strings.TrimSpace(string(data))
	}

	// Fallback to /sys filesystem on Linux
	if serialNumber == "" {
		if data, err := os.ReadFile("/sys/class/dmi/id/product_serial"); err == nil {
			serialNumber = strings.TrimSpace(string(data))
		}
	}
	if manufacturer == "" {
		if data, err := os.ReadFile("/sys/class/dmi/id/sys_vendor"); err == nil {
			manufacturer = strings.TrimSpace(string(data))
		}
	}
	if model == "" {
		if data, err := os.ReadFile("/sys/class/dmi/id/product_name"); err == nil {
			model = strings.TrimSpace(string(data))
		}
	}

	return serialNumber, manufacturer, model
}

// getDomainInfo returns the domain name on Unix systems
func (c *Collector) getDomainInfo() string {
	// Try to get domain from hostname
	if data, err := exec.Command("hostname", "-d").Output(); err == nil {
		domain := strings.TrimSpace(string(data))
		if domain != "" && domain != "(none)" {
			return domain
		}
	}

	// Try dnsdomainname
	if data, err := exec.Command("dnsdomainname").Output(); err == nil {
		domain := strings.TrimSpace(string(data))
		if domain != "" {
			return domain
		}
	}

	// Check /etc/resolv.conf for search domain
	if data, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "search ") || strings.HasPrefix(line, "domain ") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					return parts[1]
				}
			}
		}
	}

	return ""
}

// getDeviceType determines the device type based on chassis type and other indicators
func (c *Collector) getDeviceType(osName, platformFamily, manufacturer, model string) string {
	// Check for virtual machine first
	vmIndicators := []string{"vmware", "virtual", "hyperv", "qemu", "kvm", "xen", "virtualbox", "parallels", "bochs", "innotek"}
	lowerManufacturer := strings.ToLower(manufacturer)
	lowerModel := strings.ToLower(model)
	for _, vm := range vmIndicators {
		if strings.Contains(lowerManufacturer, vm) || strings.Contains(lowerModel, vm) {
			return "virtual"
		}
	}

	// Check /sys/class/dmi/id/product_name for VM indicators
	if data, err := os.ReadFile("/sys/class/dmi/id/product_name"); err == nil {
		productName := strings.ToLower(string(data))
		for _, vm := range vmIndicators {
			if strings.Contains(productName, vm) {
				return "virtual"
			}
		}
	}

	// Check for server OS indicators
	if strings.Contains(strings.ToLower(osName), "server") {
		return "server"
	}

	// Check chassis type from DMI
	// Values: https://www.dmtf.org/sites/default/files/standards/documents/DSP0134_3.4.0.pdf
	// 3=Desktop, 4=Low Profile Desktop, 5=Pizza Box, 6=Mini Tower, 7=Tower
	// 8=Portable, 9=Laptop, 10=Notebook, 11=Hand Held, 13=All in One
	// 14=Sub Notebook, 30=Tablet, 31=Convertible, 32=Detachable
	// 17=Main Server Chassis, 23=Rack Mount Chassis
	chassisType := 0
	if data, err := os.ReadFile("/sys/class/dmi/id/chassis_type"); err == nil {
		chassisStr := strings.TrimSpace(string(data))
		if val, err := strconv.Atoi(chassisStr); err == nil {
			chassisType = val
		}
	}

	// Fallback to dmidecode
	if chassisType == 0 {
		if data, err := exec.Command("dmidecode", "-s", "chassis-type").Output(); err == nil {
			chassisStr := strings.TrimSpace(strings.ToLower(string(data)))
			switch {
			case strings.Contains(chassisStr, "notebook"), strings.Contains(chassisStr, "laptop"),
				strings.Contains(chassisStr, "portable"):
				return "laptop"
			case strings.Contains(chassisStr, "desktop"), strings.Contains(chassisStr, "tower"),
				strings.Contains(chassisStr, "mini tower"), strings.Contains(chassisStr, "all in one"):
				return "desktop"
			case strings.Contains(chassisStr, "server"), strings.Contains(chassisStr, "rack"):
				return "server"
			case strings.Contains(chassisStr, "tablet"), strings.Contains(chassisStr, "convertible"):
				return "tablet"
			}
		}
	}

	switch chassisType {
	case 8, 9, 10, 14: // Portable, Laptop, Notebook, Sub Notebook
		return "laptop"
	case 30, 31, 32: // Tablet, Convertible, Detachable
		return "tablet"
	case 3, 4, 5, 6, 7, 13, 15, 16: // Desktop variants
		return "desktop"
	case 17, 23: // Main Server Chassis, Rack Mount
		return "server"
	}

	// Check for battery as laptop indicator
	if _, err := os.Stat("/sys/class/power_supply/BAT0"); err == nil {
		return "laptop"
	}
	if _, err := os.Stat("/sys/class/power_supply/BAT1"); err == nil {
		return "laptop"
	}

	// Check model name for hints
	modelLower := strings.ToLower(model)
	if strings.Contains(modelLower, "laptop") || strings.Contains(modelLower, "notebook") ||
		strings.Contains(modelLower, "thinkpad") || strings.Contains(modelLower, "latitude") ||
		strings.Contains(modelLower, "elitebook") || strings.Contains(modelLower, "probook") ||
		strings.Contains(modelLower, "macbook") {
		return "laptop"
	}

	if strings.Contains(modelLower, "poweredge") || strings.Contains(modelLower, "proliant") ||
		strings.Contains(modelLower, "bladecenter") {
		return "server"
	}

	// Default to desktop
	return "desktop"
}
