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
	kernel32                       = windows.NewLazySystemDLL("kernel32.dll")
	advapi32                       = windows.NewLazySystemDLL("advapi32.dll")
	procSetProcessMitigationPolicy = kernel32.NewProc("SetProcessMitigationPolicy")
	procSetSecurityInfo            = advapi32.NewProc("SetSecurityInfo")
)

const (
	SE_KERNEL_OBJECT             = 6
	DACL_SECURITY_INFORMATION    = 0x00000004
	PROTECTED_DACL_SECURITY_INFO = 0x80000000
	ProcessDEPPolicy                   = 0
	ProcessASLRPolicy                  = 1
	ProcessDynamicCodePolicy           = 2
	ProcessStrictHandleCheckPolicy     = 3
	ProcessSystemCallDisablePolicy     = 4
	ProcessMitigationOptionsMask       = 5
	ProcessExtensionPointDisablePolicy = 6
	ProcessControlFlowGuardPolicy      = 7
	ProcessSignaturePolicy             = 8
	ProcessFontDisablePolicy           = 9
	ProcessImageLoadPolicy             = 10
	ProcessSystemCallFilterPolicy      = 11
	ProcessPayloadRestrictionPolicy    = 12
	ProcessChildProcessPolicy          = 13
	ProcessSideChannelIsolationPolicy  = 14
)

type UninstallToken struct {
	Token     string    `json:"token"`
	DeviceID  string    `json:"deviceId"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Hash      string    `json:"hash"`
}

type Manager struct {
	installPath  string
	serviceName  string
	uninstallKey string
	configPath   string
}

func NewManager(installPath, serviceName string) *Manager {
	return &Manager{
		installPath: installPath,
		serviceName: serviceName,
		configPath:  filepath.Join(installPath, "protection.dat"),
	}
}

func (m *Manager) EnableAllProtections() error {
	var errs []string
	if err := m.ProtectProcess(); err != nil {
		errs = append(errs, fmt.Sprintf("process protection: %v", err))
	} else {
		log.Println("Process protection enabled")
	}
	if err := m.ProtectFiles(); err != nil {
		errs = append(errs, fmt.Sprintf("file protection: %v", err))
	} else {
		log.Println("File protection enabled")
	}
	if err := m.ProtectRegistry(); err != nil {
		errs = append(errs, fmt.Sprintf("registry protection: %v", err))
	} else {
		log.Println("Registry protection enabled")
	}
	if err := m.ConfigureServiceRecovery(); err != nil {
		errs = append(errs, fmt.Sprintf("service recovery: %v", err))
	} else {
		log.Println("Service recovery configured")
	}
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

func (m *Manager) ProtectProcess() error {
	handle := windows.CurrentProcess()
	sd, err := windows.SecurityDescriptorFromString("D:(A;;GA;;;SY)(A;;GA;;;BA)(D;;0x0001;;;BU)")
	if err != nil {
		return fmt.Errorf("failed to create security descriptor: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("failed to get DACL: %w", err)
	}
	err = windows.SetSecurityInfo(handle, windows.SE_KERNEL_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	if err != nil {
		return fmt.Errorf("failed to set process security: %w", err)
	}
	return nil
}

// isSafeInstallPath checks if the install path is a safe system directory
func (m *Manager) isSafeInstallPath() bool {
	path := strings.ToLower(filepath.Clean(m.installPath))
	safePrefixes := []string{`c:\program files\`, `c:\program files (x86)\`, `c:\programdata\`, `c:\windows\`}
	dangerousPaths := []string{`\users\`, `\desktop`, `\documents`, `\downloads`, `\onedrive`, `\dropbox`, `\google drive`, `\icloud`}
	for _, dangerous := range dangerousPaths {
		if strings.Contains(path, dangerous) {
			log.Printf("WARNING: Install path contains dangerous pattern '%s': %s", dangerous, path)
			return false
		}
	}
	for _, prefix := range safePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	log.Printf("WARNING: Install path is not in a known safe location: %s", path)
	return false
}

func (m *Manager) ProtectFiles() error {
	// SAFETY CHECK: Only protect known safe paths
	if !m.isSafeInstallPath() {
		log.Printf("SKIPPING file protection - install path %s is not a safe system directory", m.installPath)
		return nil
	}
	files := []string{
		filepath.Join(m.installPath, "sentinel-agent.exe"),
		filepath.Join(m.installPath, "sentinel-watchdog.exe"),
		m.configPath,
	}
	for _, file := range files {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			continue
		}
		// Use "Users" instead of "Everyone", give Admins full access
		cmd := exec.Command("icacls", file, "/inheritance:r", "/grant:r", "SYSTEM:(F)", "/grant:r", "Administrators:(F)", "/deny", "Users:(D,WO)")
		if err := cmd.Run(); err != nil {
			log.Printf("Warning: failed to protect %s: %v", file, err)
		}
	}
	// Use "Users" instead of "Everyone", give Admins full access
	cmd := exec.Command("icacls", m.installPath, "/inheritance:r", "/grant:r", "SYSTEM:(OI)(CI)(F)", "/grant:r", "Administrators:(OI)(CI)(F)", "/deny", "Users:(OI)(CI)(D,DC)")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to protect install directory: %w", err)
	}
	return nil
}

func (m *Manager) ProtectRegistry() error {
	keyPath := fmt.Sprintf(`SYSTEM\CurrentControlSet\Services\%s`, m.serviceName)
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, keyPath, registry.READ|0x00040000)
	if err != nil {
		return fmt.Errorf("failed to open service key: %w", err)
	}
	defer key.Close()
	sd, err := windows.SecurityDescriptorFromString("D:(A;OICI;KA;;;SY)(A;OICI;KR;;;BA)(D;OICI;KA;;;BU)")
	if err != nil {
		return fmt.Errorf("failed to create registry SD: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("failed to get registry DACL: %w", err)
	}
	err = windows.SetSecurityInfo(windows.Handle(key), windows.SE_REGISTRY_KEY,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	if err != nil {
		return fmt.Errorf("failed to set registry security: %w", err)
	}
	return nil
}

func (m *Manager) ConfigureServiceRecovery() error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to SCM: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(m.serviceName)
	if err != nil {
		return fmt.Errorf("failed to open service: %w", err)
	}
	defer service.Close()
	recoveryActions := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}
	err = service.SetRecoveryActions(recoveryActions, 86400)
	if err != nil {
		return fmt.Errorf("failed to set recovery actions: %w", err)
	}
	err = service.SetRecoveryActionsOnNonCrashFailures(true)
	if err != nil {
		log.Printf("Warning: could not enable recovery on non-crash failures: %v", err)
	}
	return nil
}

func (m *Manager) GenerateUninstallKey() error {
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return fmt.Errorf("failed to generate key: %w", err)
	}
	m.uninstallKey = hex.EncodeToString(keyBytes)
	hash := sha256.Sum256([]byte(m.uninstallKey))
	hashStr := hex.EncodeToString(hash[:])
	if err := os.WriteFile(m.configPath, []byte(hashStr), 0400); err != nil {
		return fmt.Errorf("failed to save key hash: %w", err)
	}
	return nil
}

func (m *Manager) GetUninstallKey() string { return m.uninstallKey }

func (m *Manager) ValidateUninstallToken(token *UninstallToken, deviceID string) bool {
	if token.DeviceID != deviceID {
		log.Println("Token device ID mismatch")
		return false
	}
	if time.Now().After(token.ExpiresAt) {
		log.Println("Token expired")
		return false
	}
	data := fmt.Sprintf("%s:%s:%d", token.Token, token.DeviceID, token.IssuedAt.Unix())
	hash := sha256.Sum256([]byte(data))
	expectedHash := hex.EncodeToString(hash[:])
	if token.Hash != expectedHash {
		log.Println("Token hash mismatch")
		return false
	}
	return true
}

func (m *Manager) DisableProtections() error {
	log.Println("Disabling protections for uninstall...")
	cmd := exec.Command("icacls", m.installPath, "/reset", "/t")
	cmd.Run()
	return nil
}

func (m *Manager) DisableProtectionForFile(filePath string) error {
	log.Printf("Disabling protection for file: %s", filePath)
	cmd := exec.Command("icacls", filePath, "/reset")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reset permissions on %s: %w (output: %s)", filePath, err, string(output))
	}
	return nil
}

func (m *Manager) EnableProtectionForFile(filePath string) error {
	log.Printf("Enabling protection for file: %s", filePath)
	cmd := exec.Command("icacls", filePath, "/inheritance:r", "/grant:r", "SYSTEM:(F)", "/grant:r", "Administrators:(F)", "/deny", "Users:(D,WO)")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to protect %s: %w (output: %s)", filePath, err, string(output))
	}
	return nil
}

func (m *Manager) DisableProtectionForDir(dirPath string) error {
	log.Printf("Disabling protection for directory: %s", dirPath)
	cmd := exec.Command("icacls", dirPath, "/reset")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reset permissions on %s: %w (output: %s)", dirPath, err, string(output))
	}
	return nil
}

func (m *Manager) EnableProtectionForDir(dirPath string) error {
	log.Printf("Enabling protection for directory: %s", dirPath)
	pathLower := strings.ToLower(filepath.Clean(dirPath))
	dangerousPaths := []string{`\users\`, `\desktop`, `\documents`, `\downloads`, `\onedrive`}
	for _, dangerous := range dangerousPaths {
		if strings.Contains(pathLower, dangerous) {
			log.Printf("REFUSING to protect dangerous path: %s", dirPath)
			return fmt.Errorf("refusing to protect user directory: %s", dirPath)
		}
	}
	cmd := exec.Command("icacls", dirPath, "/inheritance:r", "/grant:r", "SYSTEM:(OI)(CI)(F)", "/grant:r", "Administrators:(OI)(CI)(F)", "/deny", "Users:(OI)(CI)(D,DC)")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to protect %s: %w (output: %s)", dirPath, err, string(output))
	}
	return nil
}

func (m *Manager) IsFileProtected(filePath string) (bool, error) {
	cmd := exec.Command("icacls", filePath)
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check permissions: %w", err)
	}
	outputStr := string(output)
	return strings.Contains(outputStr, "(DENY)") || strings.Contains(outputStr, "(D)") || strings.Contains(outputStr, "(N)"), nil
}

func (m *Manager) HideService() error {
	keyPath := fmt.Sprintf(`SYSTEM\CurrentControlSet\Services\%s`, m.serviceName)
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, keyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue("Description", "Windows System Service Host")
}

func IsRunningAsService() bool {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return isService
}

func (m *Manager) PreventProcessHollowing() error {
	var policy uint32 = 1
	ret, _, err := procSetProcessMitigationPolicy.Call(uintptr(ProcessDynamicCodePolicy), uintptr(unsafe.Pointer(&policy)), unsafe.Sizeof(policy))
	if ret == 0 {
		log.Printf("Warning: could not set dynamic code policy: %v", err)
	}
	return nil
}

func (m *Manager) MonitorTamperAttempts(reportChan chan<- string) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	configPath := filepath.Join(os.Getenv("ProgramData"), "Sentinel", "config.json")
	for range ticker.C {
		for _, file := range []string{"sentinel-agent.exe", "sentinel-watchdog.exe"} {
			path := filepath.Join(m.installPath, file)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				reportChan <- fmt.Sprintf("TAMPER: File missing: %s", file)
			}
		}
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			reportChan <- "TAMPER: Config file missing"
		}
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
