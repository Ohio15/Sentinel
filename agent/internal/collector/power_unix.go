//go:build !windows

package collector

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// getPowerManagement detects WoL and Intel AMT capabilities on Unix/Linux
func (c *Collector) getPowerManagement(macAddress string) *PowerManagement {
	pm := &PowerManagement{
		MACAddress: macAddress,
	}

	// Detect Wake-on-LAN support via ethtool
	pm.WoLSupported, pm.WoLEnabled, pm.WoLModes = detectWoLUnix()

	// Detect Intel AMT
	pm.AMTSupported, pm.AMTProvisioned, pm.AMTVersion = detectAMTUnix()

	return pm
}

// detectWoLUnix checks Wake-on-LAN support using ethtool
func detectWoLUnix() (supported, enabled bool, modes string) {
	// Get list of network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		return false, false, ""
	}

	for _, iface := range interfaces {
		// Skip loopback and virtual interfaces
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.HardwareAddr == nil || len(iface.HardwareAddr) == 0 {
			continue
		}

		// Use ethtool to check WoL settings
		cmd := exec.Command("ethtool", iface.Name)
		output, err := cmd.Output()
		if err != nil {
			continue
		}

		outputStr := string(output)
		lines := strings.Split(outputStr, "\n")

		var supportsWoL, wolEnabled string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Supports Wake-on:") {
				supportsWoL = strings.TrimPrefix(line, "Supports Wake-on:")
				supportsWoL = strings.TrimSpace(supportsWoL)
			} else if strings.HasPrefix(line, "Wake-on:") {
				wolEnabled = strings.TrimPrefix(line, "Wake-on:")
				wolEnabled = strings.TrimSpace(wolEnabled)
			}
		}

		// WoL modes: d=disabled, p=PHY, u=unicast, m=multicast, b=broadcast, a=ARP, g=magic packet, s=SecureOn
		if supportsWoL != "" && supportsWoL != "d" {
			supported = true
			modes = supportsWoL

			// Check if magic packet (g) is enabled
			if strings.Contains(wolEnabled, "g") {
				enabled = true
			}

			// Found a capable interface, return
			return supported, enabled, modes
		}
	}

	return false, false, ""
}

// detectAMTUnix checks for Intel AMT/vPro support on Linux
func detectAMTUnix() (supported, provisioned bool, version string) {
	// Check 1: Look for MEI (Management Engine Interface) device
	if _, err := os.Stat("/dev/mei0"); err == nil {
		supported = true
	} else if _, err := os.Stat("/dev/mei"); err == nil {
		supported = true
	}

	// Check 2: Check lspci for Management Engine
	if !supported {
		cmd := exec.Command("lspci")
		output, err := cmd.Output()
		if err == nil {
			outputLower := strings.ToLower(string(output))
			if strings.Contains(outputLower, "management engine") ||
				strings.Contains(outputLower, "heci") ||
				strings.Contains(outputLower, "mei") {
				supported = true
			}
		}
	}

	// Check 3: Check dmesg for MEI messages
	if !supported {
		cmd := exec.Command("dmesg")
		output, err := cmd.Output()
		if err == nil {
			outputLower := strings.ToLower(string(output))
			if strings.Contains(outputLower, "mei_me") ||
				strings.Contains(outputLower, "intel management engine") {
				supported = true
			}
		}
	}

	// Check 4: Try to connect to AMT ports
	if supported {
		// Try HTTP port (16992)
		conn, err := net.DialTimeout("tcp", "127.0.0.1:16992", 2*time.Second)
		if err == nil {
			conn.Close()
			provisioned = true
		}

		// Try HTTPS port (16993)
		conn, err = net.DialTimeout("tcp", "127.0.0.1:16993", 2*time.Second)
		if err == nil {
			conn.Close()
			provisioned = true
		}
	}

	// Try to get version from MEI
	if supported {
		// Read from sysfs if available
		if data, err := os.ReadFile("/sys/class/mei/mei0/fw_ver"); err == nil {
			version = strings.TrimSpace(string(data))
		}

		if version == "" && provisioned {
			version = "detected"
		}
	}

	// Check CPU info for vPro
	if !supported {
		if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
			cpuInfo := strings.ToLower(string(data))
			if strings.Contains(cpuInfo, "vpro") {
				supported = true
				version = "vPro CPU detected"
			}
		}
	}

	return supported, provisioned, version
}

// SendWakeOnLAN sends a WoL magic packet to wake a machine
func SendWakeOnLAN(macAddress string, broadcastIP string) error {
	// Parse MAC address
	mac, err := net.ParseMAC(macAddress)
	if err != nil {
		return fmt.Errorf("invalid MAC address: %v", err)
	}

	// Build magic packet: 6 bytes of 0xFF followed by MAC address repeated 16 times
	packet := make([]byte, 102)
	for i := 0; i < 6; i++ {
		packet[i] = 0xFF
	}
	for i := 0; i < 16; i++ {
		copy(packet[6+i*6:], mac)
	}

	// Default broadcast address
	if broadcastIP == "" {
		broadcastIP = "255.255.255.255"
	}

	// Send UDP packet to port 9 (or 7)
	addr, err := net.ResolveUDPAddr("udp", broadcastIP+":9")
	if err != nil {
		return fmt.Errorf("failed to resolve address: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to connect: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write(packet)
	if err != nil {
		return fmt.Errorf("failed to send packet: %v", err)
	}

	return nil
}
