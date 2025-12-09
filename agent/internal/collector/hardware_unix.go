//go:build !windows

package collector

import (
	"os"
	"os/exec"
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
