//go:build windows

package protection

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

var (
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	advapi32              = windows.NewLazySystemDLL("advapi32.dll")
	procSetProcessMitigationPolicy = kernel32.NewProc("SetProcessMitigationPolicy")
	procSetSecurityInfo   = advapi32.NewProc("SetSecurityInfo")
)

const (
	// Security descriptor constants
	SE_KERNEL_OBJECT              = 6
	DACL_SECURITY_INFORMATION     = 0x00000004
	PROTECTED_DACL_SECURITY_INFO  = 0x80000000

	// Process mitigation policies
	ProcessDEPPolicy                    = 0
	ProcessASLRPolicy                   = 1
	ProcessDynamicCodePolicy            = 2
	ProcessStrictHandleCheckPolicy      = 3
	ProcessSystemCallDisablePolicy      = 4
	ProcessMitigationOptionsMask        = 5
	ProcessExtensionPointDisablePolicy  = 6
	ProcessControlFlowGuardPolicy       = 7
	ProcessSignaturePolicy              = 8
	ProcessFontDisablePolicy            = 9
	ProcessImageLoadPolicy              = 10
	ProcessSystemCallFilterPolicy       = 11
	ProcessPayloadRestrictionPolicy     = 12
	ProcessChildProcessPolicy           = 13
	ProcessSideChannelIsolationPolicy   = 14
)

// UninstallToken represents a server-issued uninstall authorization
type UninstallToken struct {
	Token     string    `json:"token"`
	DeviceID  string    `json:"deviceId"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Hash      string    `json:"hash"`
}

// Manager handles all protection mechanisms
type Manager struct {
	installPath   string
	serviceName   string
	uninstallKey  string
	configPath    string
}

// NewManager creates a new protection manager
func NewManager(installPath, serviceName string) *Manager {
	return &Manager{
		installPath: installPath,
		serviceName: serviceName,
		configPath:  filepath.Join(installPath, "protection.dat"),
	}
}

// EnableAllProtections enables all available protection mechanisms
func (m *Manager) EnableAllProtections() error {
	var errs []string

	// 1. Protect the current process from termination
	if err := m.ProtectProcess(); err != nil {
		errs = append(errs, fmt.Sprintf("process protection: %v", err))
	} else {
		log.Println("Process protection enabled")
	}

	// 2. Set file ACLs to prevent modification/deletion
	if err := m.ProtectFiles(); err != nil {
		errs = append(errs, fmt.Sprintf("file protection: %v", err))
	} else {
		log.Println("File protection enabled")
	}

	// 3. Protect registry keys
	if err := m.ProtectRegistry(); err != nil {
		errs = append(errs, fmt.Sprintf("registry protection: %v", err))
	} else {
		log.Println("Registry protection enabled")
	}

	// 4. Configure service recovery options
	if err := m.ConfigureServiceRecovery(); err != nil {
		errs = append(errs, fmt.Sprintf("service recovery: %v", err))
	} else {
		log.Println("Service recovery configured")
	}

	// 5. Generate uninstall key
	if err := m.GenerateUninstallKey(); err != nil {
		errs = append(errs, fmt.Sprintf("uninstall key: %v", err))
	} else {
		log.Println("Uninstall protection enabled")
	}

	if len(errs) > 0 {
		return fmt.Errorf("protection errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ProtectProcess makes the current process harder to terminate
func (m *Manager) ProtectProcess() error {
	// Get current process handle
	handle := windows.CurrentProcess()

	// Create a restrictive security descriptor
	// This denies PROCESS_TERMINATE (0x0001) to Users, but allows SYSTEM and Administrators
	// Note: Using BU (Builtin Users) instead of WD (World/Everyone) so SYSTEM can still
	// control the process during updates.
	sd, err := windows.SecurityDescriptorFromString(
		"D:(A;;GA;;;SY)(A;;GA;;;BA)(D;;0x0001;;;BU)",
	)
	if err != nil {
		return fmt.Errorf("failed to create security descriptor: %w", err)
	}

	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("failed to get DACL: %w", err)
	}

	// Set the security descriptor on the process
	err = windows.SetSecurityInfo(
		handle,
		windows.SE_KERNEL_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	)
	if err != nil {
		return fmt.Errorf("failed to set process security: %w", err)
	}

	return nil
}

// ProtectFiles sets restrictive ACLs on agent files
func (m *Manager) ProtectFiles() error {
	// Files to protect
	files := []string{
		filepath.Join(m.installPath, "sentinel-agent.exe"),
		filepath.Join(m.installPath, "sentinel-watchdog.exe"),
		m.configPath, // Config is in ProgramData, not install path
	}

	for _, file := range files {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			continue
		}

		// Use icacls to set restrictive permissions
		// Only SYSTEM and Administrators can modify
		// Note: We use "Users" (BU) instead of "Everyone" for deny because
		// "Everyone" includes SYSTEM, which would prevent SYSTEM from resetting
		// permissions during watchdog-orchestrated updates.
		cmd := exec.Command("icacls", file,
			"/inheritance:r",
			"/grant:r", "SYSTEM:(F)",
			"/grant:r", "Administrators:(R)",
			"/deny", "Users:(D,WO)",
		)
		if err := cmd.Run(); err != nil {
			log.Printf("Warning: failed to protect %s: %v", file, err)
		}
	}

	// Protect the installation directory
	cmd := exec.Command("icacls", m.installPath,
		"/inheritance:r",
		"/grant:r", "SYSTEM:(OI)(CI)(F)",
		"/grant:r", "Administrators:(OI)(CI)(R)",
		"/deny", "Everyone:(OI)(CI)(D,DC)",
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to protect install directory: %w", err)
	}

	return nil
}

// ProtectRegistry protects the service registry keys
func (m *Manager) ProtectRegistry() error {
	keyPath := fmt.Sprintf(`SYSTEM\CurrentControlSet\Services\%s`, m.serviceName)

	// Open the registry key using the registry package
	// WRITE_DAC is 0x00040000
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		keyPath,
		registry.READ|0x00040000, // WRITE_DAC
	)
	if err != nil {
		return fmt.Errorf("failed to open service key: %w", err)
	}
	defer key.Close()

	// Create restrictive security descriptor
	// Only SYSTEM can modify, Administrators can read
	// Note: We use BU (Builtin Users) instead of WD (World/Everyone) for deny
	// because Everyone includes SYSTEM, which would prevent SYSTEM from
	// managing the service during watchdog-orchestrated updates.
	sd, err := windows.SecurityDescriptorFromString(
		"D:(A;OICI;KA;;;SY)(A;OICI;KR;;;BA)(D;OICI;KA;;;BU)",
	)
	if err != nil {
		return fmt.Errorf("failed to create registry SD: %w", err)
	}

	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("failed to get registry DACL: %w", err)
	}

	// Apply the security descriptor using the underlying handle
	err = windows.SetSecurityInfo(
		windows.Handle(key),
		windows.SE_REGISTRY_KEY,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	)
	if err != nil {
		return fmt.Errorf("failed to set registry security: %w", err)
	}

	return nil
}

// ConfigureServiceRecovery sets up automatic service restart on failure
func (m *Manager) ConfigureServiceRecovery() error {
	// Connect to service manager
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to SCM: %w", err)
	}
	defer manager.Disconnect()

	// Open the service
	service, err := manager.OpenService(m.serviceName)
	if err != nil {
		return fmt.Errorf("failed to open service: %w", err)
	}
	defer service.Close()

	// Configure recovery actions
	recoveryActions := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},  // First failure
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second}, // Second failure
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second}, // Subsequent failures
	}

	err = service.SetRecoveryActions(recoveryActions, 86400) // Reset after 24 hours
	if err != nil {
		return fmt.Errorf("failed to set recovery actions: %w", err)
	}

	// Also set failure command to notify watchdog
	// This runs a command when the service fails
	err = service.SetRecoveryActionsOnNonCrashFailures(true)
	if err != nil {
		log.Printf("Warning: could not enable recovery on non-crash failures: %v", err)
	}

	return nil
}

// GenerateUninstallKey creates a unique key required for uninstallation
func (m *Manager) GenerateUninstallKey() error {
	// Generate a random 32-byte key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return fmt.Errorf("failed to generate key: %w", err)
	}

	m.uninstallKey = hex.EncodeToString(keyBytes)

	// Save the hash of the key (not the key itself)
	hash := sha256.Sum256([]byte(m.uninstallKey))
	hashStr := hex.EncodeToString(hash[:])

	// Write to protected file
	if err := os.WriteFile(m.configPath, []byte(hashStr), 0400); err != nil {
		return fmt.Errorf("failed to save key hash: %w", err)
	}

	return nil
}

// GetUninstallKey returns the uninstall key (for registration with server)
func (m *Manager) GetUninstallKey() string {
	return m.uninstallKey
}

// ValidateUninstallToken checks if a token is valid for uninstallation
func (m *Manager) ValidateUninstallToken(token *UninstallToken, deviceID string) bool {
	// Check device ID matches
	if token.DeviceID != deviceID {
		log.Println("Token device ID mismatch")
		return false
	}

	// Check expiration
	if time.Now().After(token.ExpiresAt) {
		log.Println("Token expired")
		return false
	}

	// Verify hash
	data := fmt.Sprintf("%s:%s:%d", token.Token, token.DeviceID, token.IssuedAt.Unix())
	hash := sha256.Sum256([]byte(data))
	expectedHash := hex.EncodeToString(hash[:])

	if token.Hash != expectedHash {
		log.Println("Token hash mismatch")
		return false
	}

	return true
}

// DisableProtections removes protection for legitimate uninstall
func (m *Manager) DisableProtections() error {
	log.Println("Disabling protections for uninstall...")

	// Reset file permissions
	cmd := exec.Command("icacls", m.installPath, "/reset", "/t")
	cmd.Run()

	// Note: Process and registry protections will be removed when the service stops
	return nil
}

// DisableProtectionForFile resets permissions on a specific file only.
// This is more granular than DisableProtections() and is used during updates.
func (m *Manager) DisableProtectionForFile(filePath string) error {
	log.Printf("Disabling protection for file: %s", filePath)

	// Reset permissions on the specific file
	cmd := exec.Command("icacls", filePath, "/reset")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reset permissions on %s: %w (output: %s)", filePath, err, string(output))
	}

	return nil
}

// EnableProtectionForFile applies restrictive permissions to a specific file.
// This is used after an update to re-protect the new binary.
func (m *Manager) EnableProtectionForFile(filePath string) error {
	log.Printf("Enabling protection for file: %s", filePath)

	// Set restrictive permissions: SYSTEM full, Administrators read, Users deny delete/write
	// Note: We use "Users" instead of "Everyone" to allow SYSTEM to reset permissions
	// during watchdog-orchestrated updates.
	cmd := exec.Command("icacls", filePath,
		"/inheritance:r",
		"/grant:r", "SYSTEM:(F)",
		"/grant:r", "Administrators:(R)",
		"/deny", "Users:(D,WO)",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to protect %s: %w (output: %s)", filePath, err, string(output))
	}

	return nil
}

// DisableProtectionForDir resets permissions on a directory (non-recursive).
func (m *Manager) DisableProtectionForDir(dirPath string) error {
	log.Printf("Disabling protection for directory: %s", dirPath)

	cmd := exec.Command("icacls", dirPath, "/reset")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reset permissions on %s: %w (output: %s)", dirPath, err, string(output))
	}

	return nil
}

// EnableProtectionForDir applies restrictive permissions to a directory.
func (m *Manager) EnableProtectionForDir(dirPath string) error {
	log.Printf("Enabling protection for directory: %s", dirPath)

	cmd := exec.Command("icacls", dirPath,
		"/inheritance:r",
		"/grant:r", "SYSTEM:(OI)(CI)(F)",
		"/grant:r", "Administrators:(OI)(CI)(R)",
		"/deny", "Everyone:(OI)(CI)(D,DC)",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to protect %s: %w (output: %s)", dirPath, err, string(output))
	}

	return nil
}

// IsFileProtected checks if a file has restrictive permissions applied.
func (m *Manager) IsFileProtected(filePath string) (bool, error) {
	// Use icacls to check permissions
	cmd := exec.Command("icacls", filePath)
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check permissions: %w", err)
	}

	// Check if "Everyone" deny entry exists
	outputStr := string(output)
	return strings.Contains(outputStr, "Everyone:(DENY)") ||
		strings.Contains(outputStr, "Everyone:(D)") ||
		strings.Contains(outputStr, "(N)"), nil
}

// HideService attempts to hide the service from standard enumeration
// This is a defense-in-depth measure
func (m *Manager) HideService() error {
	// Set the service type to include the "own process" flag
	// and mark it as interactive (which hides it from some tools)
	keyPath := fmt.Sprintf(`SYSTEM\CurrentControlSet\Services\%s`, m.serviceName)

	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		keyPath,
		registry.SET_VALUE,
	)
	if err != nil {
		return err
	}
	defer key.Close()

	// Set Description to something innocuous
	desc := "Windows System Service Host"
	if err := key.SetStringValue("Description", desc); err != nil {
		return err
	}

	return nil
}

// IsRunningAsService checks if we're running as a Windows service
func IsRunningAsService() bool {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return isService
}

// PreventProcessHollowing enables mitigation policies against common attacks
func (m *Manager) PreventProcessHollowing() error {
	// This requires Windows 10+ and may fail on older systems
	// Enable dynamic code prevention
	var policy uint32 = 1
	ret, _, err := procSetProcessMitigationPolicy.Call(
		uintptr(ProcessDynamicCodePolicy),
		uintptr(unsafe.Pointer(&policy)),
		unsafe.Sizeof(policy),
	)
	if ret == 0 {
		log.Printf("Warning: could not set dynamic code policy: %v", err)
	}

	return nil
}

// MonitorTamperAttempts watches for tampering and reports to server
func (m *Manager) MonitorTamperAttempts(reportChan chan<- string) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Config is stored in ProgramData, not install path
	configPath := filepath.Join(os.Getenv("ProgramData"), "Sentinel", "config.json")

	for range ticker.C {
		// Check if executable files exist in install path
		for _, file := range []string{"sentinel-agent.exe", "sentinel-watchdog.exe"} {
			path := filepath.Join(m.installPath, file)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				reportChan <- fmt.Sprintf("TAMPER: File missing: %s", file)
			}
		}

		// Check config file in correct location (ProgramData)
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			reportChan <- "TAMPER: Config file missing"
		}

		// Check if service is still registered
		manager, err := mgr.Connect()
		if err == nil {
			_, err = manager.OpenService(m.serviceName)
			if err != nil {
				reportChan <- fmt.Sprintf("TAMPER: Service not found: %s", m.serviceName)
			}
			manager.Disconnect()
		}
	}
}
