//go:build windows

package collector

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

// getPowerManagement detects WoL and Intel AMT capabilities on Windows
func (c *Collector) getPowerManagement(macAddress string) *PowerManagement {
	pm := &PowerManagement{
		MACAddress: macAddress,
	}

	// Detect Wake-on-LAN support via PowerShell
	pm.WoLSupported, pm.WoLEnabled, pm.WoLModes = detectWoLWindows()

	// Detect Intel AMT
	pm.AMTSupported, pm.AMTProvisioned, pm.AMTVersion = detectAMTWindows()

	return pm
}

// detectWoLWindows checks Wake-on-LAN support using PowerShell
func detectWoLWindows() (supported, enabled bool, modes string) {
	// Use PowerShell to query network adapter power management settings
	// This checks if any adapter has WoL capabilities
	cmd := exec.Command("powershell", "-NoProfile", "-Command", `
		$adapters = Get-NetAdapter | Where-Object { $_.Status -eq 'Up' -and $_.PhysicalMediaType -ne 'Unspecified' }
		foreach ($adapter in $adapters) {
			try {
				$pm = Get-NetAdapterPowerManagement -Name $adapter.Name -ErrorAction SilentlyContinue
				if ($pm) {
					$wol = @()
					if ($pm.WakeOnMagicPacket -ne 'Unsupported') { $wol += 'MagicPacket' }
					if ($pm.WakeOnPattern -ne 'Unsupported') { $wol += 'Pattern' }
					if ($wol.Count -gt 0) {
						$enabled = ($pm.WakeOnMagicPacket -eq 'Enabled') -or ($pm.WakeOnPattern -eq 'Enabled')
						Write-Output "SUPPORTED:true"
						Write-Output "ENABLED:$enabled"
						Write-Output "MODES:$($wol -join ',')"
						exit
					}
				}
			} catch {}
		}
		Write-Output "SUPPORTED:false"
		Write-Output "ENABLED:false"
		Write-Output "MODES:"
	`)

	output, err := cmd.Output()
	if err != nil {
		return false, false, ""
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SUPPORTED:") {
			supported = strings.TrimPrefix(line, "SUPPORTED:") == "true" ||
				strings.TrimPrefix(line, "SUPPORTED:") == "True"
		} else if strings.HasPrefix(line, "ENABLED:") {
			enabled = strings.TrimPrefix(line, "ENABLED:") == "true" ||
				strings.TrimPrefix(line, "ENABLED:") == "True"
		} else if strings.HasPrefix(line, "MODES:") {
			modes = strings.TrimPrefix(line, "MODES:")
		}
	}

	return supported, enabled, modes
}

// detectAMTWindows checks for Intel AMT/vPro support
func detectAMTWindows() (supported, provisioned bool, version string) {
	// Check 1: Look for Intel Management Engine Interface driver
	cmd := exec.Command("powershell", "-NoProfile", "-Command", `
		$mei = Get-WmiObject Win32_PnPEntity | Where-Object { $_.Name -like '*Intel*Management Engine*' -or $_.Name -like '*Intel*MEI*' }
		if ($mei) { Write-Output "MEI:true" } else { Write-Output "MEI:false" }
	`)

	output, err := cmd.Output()
	if err == nil && strings.Contains(string(output), "MEI:true") {
		supported = true
	}

	// Check 2: Look for Intel LMS (Local Management Service)
	cmd = exec.Command("sc", "query", "LMS")
	output, err = cmd.Output()
	if err == nil && strings.Contains(string(output), "RUNNING") {
		supported = true
	}

	// Check 3: Try to connect to AMT HTTP port (16992) - indicates provisioned
	if supported {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:16992", 2*time.Second)
		if err == nil {
			conn.Close()
			provisioned = true
		}

		// Also check HTTPS port (16993)
		conn, err = net.DialTimeout("tcp", "127.0.0.1:16993", 2*time.Second)
		if err == nil {
			conn.Close()
			provisioned = true
		}
	}

	// Try to get AMT version from registry
	if supported {
		cmd = exec.Command("reg", "query", `HKLM\SOFTWARE\Intel\AMT`, "/v", "AMT")
		output, err = cmd.Output()
		if err == nil {
			// Parse version from output
			for _, line := range strings.Split(string(output), "\n") {
				if strings.Contains(line, "REG_SZ") {
					parts := strings.Fields(line)
					if len(parts) >= 3 {
						version = parts[len(parts)-1]
					}
				}
			}
		}

		// Alternative: Try MEInfo tool if available
		if version == "" {
			cmd = exec.Command("powershell", "-NoProfile", "-Command", `
				$mei = Get-WmiObject -Namespace root\Intel_ME -Class ME_System -ErrorAction SilentlyContinue
				if ($mei) { Write-Output $mei.FWVersion }
			`)
			output, err = cmd.Output()
			if err == nil {
				version = strings.TrimSpace(string(output))
			}
		}

		if version == "" && provisioned {
			version = "detected"
		}
	}

	// Check for vPro-capable CPU
	if !supported {
		cmd = exec.Command("wmic", "cpu", "get", "name", "/format:value")
		output, err = cmd.Output()
		if err == nil {
			cpuName := strings.ToLower(string(output))
			// vPro is typically on i5/i7/i9 vPro processors
			if strings.Contains(cpuName, "vpro") {
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

// ExecutePowerAction executes a power action on Windows
func ExecutePowerAction(action string) error {
	var cmd *exec.Cmd

	switch action {
	case "shutdown":
		// shutdown /s /t 0 - immediate shutdown
		cmd = exec.Command("shutdown", "/s", "/t", "0")
	case "restart":
		// shutdown /r /t 0 - immediate restart
		cmd = exec.Command("shutdown", "/r", "/t", "0")
	default:
		return fmt.Errorf("unsupported power action: %s", action)
	}

	return cmd.Run()
}
