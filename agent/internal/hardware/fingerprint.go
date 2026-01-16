package hardware

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"runtime"
	"sort"
	"strings"
)

// Fingerprint generates a stable hardware-based machine identifier.
// This ID persists across:
// - Agent reinstalls
// - Hostname changes
// - OS reinstalls (in most cases)
//
// The fingerprint is a SHA256 hash of multiple hardware identifiers,
// providing stability even if one identifier changes.
func Fingerprint() (string, error) {
	var components []string

	// Get platform-specific machine ID (most stable)
	if machineID, err := getMachineID(); err == nil && machineID != "" {
		components = append(components, "machine:"+machineID)
	}

	// Get BIOS/system serial number
	if serial, err := getBIOSSerial(); err == nil && serial != "" {
		components = append(components, "bios:"+serial)
	}

	// Get primary MAC address as fallback
	if mac, err := getPrimaryMAC(); err == nil && mac != "" {
		components = append(components, "mac:"+mac)
	}

	// Need at least one component for a valid fingerprint
	if len(components) == 0 {
		return "", fmt.Errorf("unable to collect any hardware identifiers")
	}

	// Sort for consistency
	sort.Strings(components)

	// Create a stable hash
	combined := strings.Join(components, "|")
	hash := sha256.Sum256([]byte(combined))

	// Return first 32 chars of hex (128 bits) formatted as UUID-like string
	hexStr := hex.EncodeToString(hash[:16])
	// Format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	fingerprint := fmt.Sprintf("%s-%s-%s-%s-%s",
		hexStr[0:8],
		hexStr[8:12],
		hexStr[12:16],
		hexStr[16:20],
		hexStr[20:32],
	)

	return fingerprint, nil
}

// getPrimaryMAC returns the MAC address of the primary network interface.
// This is a cross-platform fallback.
func getPrimaryMAC() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	// Find the first non-loopback, non-virtual interface with a MAC
	for _, iface := range interfaces {
		// Skip loopback, down, and virtual interfaces
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Skip common virtual interface names
		name := strings.ToLower(iface.Name)
		if strings.Contains(name, "virtual") ||
			strings.Contains(name, "vbox") ||
			strings.Contains(name, "vmware") ||
			strings.Contains(name, "docker") ||
			strings.Contains(name, "veth") ||
			strings.Contains(name, "br-") ||
			strings.Contains(name, "virbr") {
			continue
		}

		mac := iface.HardwareAddr.String()
		if mac != "" && mac != "00:00:00:00:00:00" {
			return strings.ToUpper(mac), nil
		}
	}

	return "", fmt.Errorf("no suitable network interface found")
}

// FingerprintWithFallback returns a hardware fingerprint, or generates
// a random UUID if hardware fingerprinting fails. This ensures the agent
// always has an ID, even in VMs or containers where hardware IDs may not
// be available.
func FingerprintWithFallback() string {
	if fp, err := Fingerprint(); err == nil {
		return fp
	}

	// Fallback: generate a random UUID (original behavior)
	// This should rarely happen on physical machines
	return generateRandomUUID()
}

// generateRandomUUID creates a random UUID v4
func generateRandomUUID() string {
	// Import here to avoid dependency if not needed
	// Using crypto/rand for security
	b := make([]byte, 16)
	_, _ = cryptoRandRead(b)

	// Set version (4) and variant (RFC 4122)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Platform check for documentation
func init() {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		// Log warning for unsupported platforms
		fmt.Printf("[WARNING] Hardware fingerprinting may not work on %s\n", runtime.GOOS)
	}
}
