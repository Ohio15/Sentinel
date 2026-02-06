//go:build windows
// +build windows

// Package rdp provides RDP configuration and shadow session management for Windows
package rdp

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

var (
	wtsapi32                         = syscall.NewLazyDLL("wtsapi32.dll")
	procWTSGetActiveConsoleSessionId = wtsapi32.NewProc("WTSGetActiveConsoleSessionId")
)

// ShadowMode represents the RDP shadow permission level
type ShadowMode int

const (
	// ShadowModeDisabled disables remote control
	ShadowModeDisabled ShadowMode = 0
	// ShadowModeFullControlWithPermission allows full control with user's permission
	ShadowModeFullControlWithPermission ShadowMode = 1
	// ShadowModeFullControlNoPermission allows full control without user's permission (for unattended RMM)
	ShadowModeFullControlNoPermission ShadowMode = 2
	// ShadowModeViewWithPermission allows view-only with user's permission
	ShadowModeViewWithPermission ShadowMode = 3
	// ShadowModeViewNoPermission allows view-only without user's permission
	ShadowModeViewNoPermission ShadowMode = 4
)

// Config manages RDP settings on the target machine
type Config struct {
	shadowMode ShadowMode
}

// NewConfig creates a new RDP configuration manager
func NewConfig() *Config {
	return &Config{
		shadowMode: ShadowModeFullControlNoPermission, // Default for RMM
	}
}

// SetShadowMode sets the shadow mode to use when enabling shadowing
func (c *Config) SetShadowMode(mode ShadowMode) {
	c.shadowMode = mode
}

// IsRDPAvailable checks if RDP is available (Pro/Enterprise/Server, not Windows Home)
func (c *Config) IsRDPAvailable() bool {
	// Check Terminal Server registry key - Windows Home lacks proper TS support
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Terminal Server`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		log.Printf("[RDP Config] Failed to open Terminal Server key: %v", err)
		return false
	}
	defer key.Close()

	// Check if TSAppCompat exists - absent on Windows Home
	_, _, err = key.GetIntegerValue("TSAppCompat")
	if err != nil {
		log.Printf("[RDP Config] TSAppCompat not found, likely Windows Home: %v", err)
		return false
	}

	// Additional check: verify we can read fDenyTSConnections
	_, _, err = key.GetIntegerValue("fDenyTSConnections")
	if err != nil {
		log.Printf("[RDP Config] fDenyTSConnections not found: %v", err)
		return false
	}

	return true
}

// IsRDPEnabled checks if RDP is currently enabled
func (c *Config) IsRDPEnabled() bool {
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Terminal Server`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return false
	}
	defer key.Close()

	val, _, err := key.GetIntegerValue("fDenyTSConnections")
	if err != nil {
		return false
	}

	// fDenyTSConnections = 0 means RDP is enabled
	return val == 0
}

// EnableRDP enables Remote Desktop on the machine
// Requires: Agent running as SYSTEM with admin privileges
func (c *Config) EnableRDP() error {
	// Open Terminal Server registry key
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Terminal Server`,
		registry.SET_VALUE,
	)
	if err != nil {
		return fmt.Errorf("failed to open Terminal Server registry key: %w", err)
	}
	defer key.Close()

	// Set fDenyTSConnections = 0 (enable RDP)
	if err := key.SetDWordValue("fDenyTSConnections", 0); err != nil {
		return fmt.Errorf("failed to enable RDP (fDenyTSConnections): %w", err)
	}

	log.Printf("[RDP Config] Enabled RDP (set fDenyTSConnections=0)")

	// Enable Network Level Authentication (more secure)
	if err := key.SetDWordValue("UserAuthentication", 1); err != nil {
		log.Printf("[RDP Config] Warning: failed to enable NLA: %v", err)
		// Non-fatal, continue
	} else {
		log.Printf("[RDP Config] Enabled Network Level Authentication")
	}

	return nil
}

// DisableRDP disables Remote Desktop on the machine
func (c *Config) DisableRDP() error {
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Terminal Server`,
		registry.SET_VALUE,
	)
	if err != nil {
		return fmt.Errorf("failed to open Terminal Server registry key: %w", err)
	}
	defer key.Close()

	// Set fDenyTSConnections = 1 (disable RDP)
	if err := key.SetDWordValue("fDenyTSConnections", 1); err != nil {
		return fmt.Errorf("failed to disable RDP: %w", err)
	}

	log.Printf("[RDP Config] Disabled RDP")
	return nil
}

// EnableShadowing enables RDP shadow/remote control capability
func (c *Config) EnableShadowing() error {
	// Open or create Terminal Services policy key
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Policies\Microsoft\Windows NT\Terminal Services`,
		registry.SET_VALUE|registry.CREATE_SUB_KEY,
	)
	if err != nil {
		// Key might not exist, try to create it
		key, _, err = registry.CreateKey(
			registry.LOCAL_MACHINE,
			`SOFTWARE\Policies\Microsoft\Windows NT\Terminal Services`,
			registry.SET_VALUE,
		)
		if err != nil {
			return fmt.Errorf("failed to open/create Terminal Services policy key: %w", err)
		}
	}
	defer key.Close()

	// Set shadow mode
	if err := key.SetDWordValue("Shadow", uint32(c.shadowMode)); err != nil {
		return fmt.Errorf("failed to set shadow policy: %w", err)
	}

	log.Printf("[RDP Config] Enabled shadow mode: %d", c.shadowMode)
	return nil
}

// ConfigureFirewall allows RDP through Windows Firewall
func (c *Config) ConfigureFirewall() error {
	// Enable the Remote Desktop firewall rule group
	cmd := exec.Command("netsh", "advfirewall", "firewall", "set", "rule",
		"group=remote desktop", "new", "enable=Yes")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try alternative approach - enable specific rules
		log.Printf("[RDP Config] Failed to enable firewall group rule: %v, output: %s", err, string(output))

		// Try enabling specific rule
		cmd = exec.Command("netsh", "advfirewall", "firewall", "set", "rule",
			"name=Remote Desktop - User Mode (TCP-In)", "new", "enable=Yes")
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to configure firewall: %w (output: %s)", err, string(output))
		}
	}

	log.Printf("[RDP Config] Configured firewall for RDP")
	return nil
}

// GetActiveConsoleSessionID returns the session ID of the active console session
// This is the session we need to shadow
func (c *Config) GetActiveConsoleSessionID() (uint32, error) {
	ret, _, _ := procWTSGetActiveConsoleSessionId.Call()

	// 0xFFFFFFFF indicates no active console session
	if ret == 0xFFFFFFFF {
		return 0, fmt.Errorf("no active console session")
	}

	return uint32(ret), nil
}

// GetWindowsEdition returns a string describing the Windows edition
func (c *Config) GetWindowsEdition() string {
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return "Unknown"
	}
	defer key.Close()

	productName, _, err := key.GetStringValue("ProductName")
	if err != nil {
		return "Unknown"
	}

	return productName
}

// GetRDPPort returns the configured RDP port (default 3389)
func (c *Config) GetRDPPort() int {
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Terminal Server\WinStations\RDP-Tcp`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return 3389 // Default
	}
	defer key.Close()

	port, _, err := key.GetIntegerValue("PortNumber")
	if err != nil {
		return 3389 // Default
	}

	return int(port)
}

// SetRDPPort sets the RDP listening port
func (c *Config) SetRDPPort(port int) error {
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Terminal Server\WinStations\RDP-Tcp`,
		registry.SET_VALUE,
	)
	if err != nil {
		return fmt.Errorf("failed to open RDP-Tcp registry key: %w", err)
	}
	defer key.Close()

	if err := key.SetDWordValue("PortNumber", uint32(port)); err != nil {
		return fmt.Errorf("failed to set RDP port: %w", err)
	}

	log.Printf("[RDP Config] Set RDP port to %d", port)
	return nil
}

// Credentials holds temporary credentials for RDP shadow access
type Credentials struct {
	Username string
	Password string
}

// CreateShadowCredentials creates temporary credentials for shadow access
// The credentials can be used by the remote viewer to authenticate
func (c *Config) CreateShadowCredentials(username string) (*Credentials, error) {
	// Generate secure random password
	password, err := generateSecurePassword(24)
	if err != nil {
		return nil, fmt.Errorf("failed to generate password: %w", err)
	}

	// Try to create the user account
	cmd := exec.Command("net", "user", username, password, "/add")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Account might already exist, try to update password
		log.Printf("[RDP Config] User creation failed (may exist): %s", string(output))

		cmd = exec.Command("net", "user", username, password)
		output, err = cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("failed to create/update shadow user: %w (output: %s)", err, string(output))
		}
	}

	log.Printf("[RDP Config] Created/updated shadow user: %s", username)

	// Add to Remote Desktop Users group
	cmd = exec.Command("net", "localgroup", "Remote Desktop Users", username, "/add")
	output, _ = cmd.CombinedOutput() // Ignore error if already member
	log.Printf("[RDP Config] Remote Desktop Users group: %s", string(output))

	// Add to Administrators group (required for shadow without consent in some scenarios)
	cmd = exec.Command("net", "localgroup", "Administrators", username, "/add")
	output, _ = cmd.CombinedOutput() // Ignore error if already member
	log.Printf("[RDP Config] Administrators group: %s", string(output))

	return &Credentials{
		Username: username,
		Password: password,
	}, nil
}

// CleanupShadowCredentials removes the temporary shadow account
func (c *Config) CleanupShadowCredentials(username string) error {
	cmd := exec.Command("net", "user", username, "/delete")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete shadow user: %w (output: %s)", err, string(output))
	}

	log.Printf("[RDP Config] Deleted shadow user: %s", username)
	return nil
}

// generateSecurePassword creates a cryptographically secure random password
func generateSecurePassword(length int) (string, error) {
	// Use a character set without confusing characters
	const charset = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789!@#$%^&*"

	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	// Convert random bytes to password characters
	for i := range bytes {
		bytes[i] = charset[bytes[i]%byte(len(charset))]
	}

	return string(bytes), nil
}

// GenerateSessionToken creates a random token for session identification
func GenerateSessionToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// RestartTerminalServices restarts the Terminal Services service
// This may be needed after configuration changes
func (c *Config) RestartTerminalServices() error {
	// Stop the service
	cmd := exec.Command("net", "stop", "TermService", "/y")
	cmd.Run() // Ignore error

	// Start the service
	cmd = exec.Command("net", "start", "TermService")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to restart Terminal Services: %w (output: %s)", err, string(output))
	}

	log.Printf("[RDP Config] Restarted Terminal Services")
	return nil
}

// Marker for unsafe usage - required for syscall but not used in this file directly
var _ = unsafe.Sizeof(0)
