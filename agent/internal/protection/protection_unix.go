//go:build !windows

package protection

import (
	"fmt"
	"log"
	"time"
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
	installPath  string
	serviceName  string
	uninstallKey string
	configPath   string
}

// NewManager creates a new protection manager
func NewManager(installPath, serviceName string) *Manager {
	return &Manager{
		installPath: installPath,
		serviceName: serviceName,
	}
}

// EnableAllProtections enables all available protection mechanisms
func (m *Manager) EnableAllProtections() error {
	log.Println("Protection features are Windows-only")
	return nil
}

// ProtectProcess makes the current process harder to terminate
func (m *Manager) ProtectProcess() error {
	return fmt.Errorf("not implemented on this platform")
}

// ProtectFiles sets restrictive ACLs on agent files
func (m *Manager) ProtectFiles() error {
	return fmt.Errorf("not implemented on this platform")
}

// ProtectRegistry protects the service registry keys
func (m *Manager) ProtectRegistry() error {
	return fmt.Errorf("not implemented on this platform")
}

// ConfigureServiceRecovery sets up automatic service restart on failure
func (m *Manager) ConfigureServiceRecovery() error {
	return fmt.Errorf("not implemented on this platform")
}

// GenerateUninstallKey creates a unique key required for uninstallation
func (m *Manager) GenerateUninstallKey() error {
	return nil
}

// GetUninstallKey returns the uninstall key
func (m *Manager) GetUninstallKey() string {
	return m.uninstallKey
}

// ValidateUninstallToken checks if a token is valid for uninstallation
func (m *Manager) ValidateUninstallToken(token *UninstallToken, deviceID string) bool {
	return true // No protection on non-Windows
}

// DisableProtections removes protection for legitimate uninstall
func (m *Manager) DisableProtections() error {
	return nil
}

// HideService attempts to hide the service from standard enumeration
func (m *Manager) HideService() error {
	return fmt.Errorf("not implemented on this platform")
}

// IsRunningAsService checks if we're running as a service
func IsRunningAsService() bool {
	return false
}

// PreventProcessHollowing enables mitigation policies
func (m *Manager) PreventProcessHollowing() error {
	return nil
}

// MonitorTamperAttempts watches for tampering
func (m *Manager) MonitorTamperAttempts(reportChan chan<- string) {
	// Not implemented on non-Windows
}
