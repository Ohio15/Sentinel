//go:build !windows
// +build !windows

// Package rdp provides RDP configuration and shadow session management
// This file provides stub implementations for non-Windows platforms
package rdp

import (
	"context"
	"fmt"
)

// Config manages RDP settings (stub for non-Windows)
type Config struct{}

// NewConfig creates a new RDP configuration manager
func NewConfig() *Config {
	return &Config{}
}

// IsRDPAvailable always returns false on non-Windows platforms
func (c *Config) IsRDPAvailable() bool {
	return false
}

// IsRDPEnabled always returns false on non-Windows platforms
func (c *Config) IsRDPEnabled() bool {
	return false
}

// EnableRDP is not supported on non-Windows platforms
func (c *Config) EnableRDP() error {
	return fmt.Errorf("RDP not supported on this platform")
}

// DisableRDP is not supported on non-Windows platforms
func (c *Config) DisableRDP() error {
	return fmt.Errorf("RDP not supported on this platform")
}

// EnableShadowing is not supported on non-Windows platforms
func (c *Config) EnableShadowing() error {
	return fmt.Errorf("RDP not supported on this platform")
}

// ConfigureFirewall is not supported on non-Windows platforms
func (c *Config) ConfigureFirewall() error {
	return fmt.Errorf("RDP not supported on this platform")
}

// GetActiveConsoleSessionID returns an error on non-Windows platforms
func (c *Config) GetActiveConsoleSessionID() (uint32, error) {
	return 0, fmt.Errorf("RDP not supported on this platform")
}

// GetWindowsEdition returns empty on non-Windows platforms
func (c *Config) GetWindowsEdition() string {
	return ""
}

// GetRDPPort returns 0 on non-Windows platforms
func (c *Config) GetRDPPort() int {
	return 0
}

// SetRDPPort is not supported on non-Windows platforms
func (c *Config) SetRDPPort(port int) error {
	return fmt.Errorf("RDP not supported on this platform")
}

// Credentials holds temporary credentials for RDP shadow access
type Credentials struct {
	Username string
	Password string
}

// CreateShadowCredentials is not supported on non-Windows platforms
func (c *Config) CreateShadowCredentials(username string) (*Credentials, error) {
	return nil, fmt.Errorf("RDP not supported on this platform")
}

// CleanupShadowCredentials is not supported on non-Windows platforms
func (c *Config) CleanupShadowCredentials(username string) error {
	return fmt.Errorf("RDP not supported on this platform")
}

// RestartTerminalServices is not supported on non-Windows platforms
func (c *Config) RestartTerminalServices() error {
	return fmt.Errorf("RDP not supported on this platform")
}

// SetShadowMode is not supported on non-Windows platforms
func (c *Config) SetShadowMode(mode ShadowMode) {}

// ShadowMode represents the RDP shadow permission level
type ShadowMode int

const (
	ShadowModeDisabled                  ShadowMode = 0
	ShadowModeFullControlWithPermission ShadowMode = 1
	ShadowModeFullControlNoPermission   ShadowMode = 2
	ShadowModeViewWithPermission        ShadowMode = 3
	ShadowModeViewNoPermission          ShadowMode = 4
)

// ShadowInfo contains information about an active shadow session
type ShadowInfo struct {
	SessionID   uint32
	Username    string
	Password    string
	TunnelHost  string
	TunnelPort  int
	Token       string
}

// ShadowSession manages RDP shadow connections (stub for non-Windows)
type ShadowSession struct{}

// NewShadowSession creates a new shadow session manager
func NewShadowSession() *ShadowSession {
	return &ShadowSession{}
}

// Capabilities describes the RDP capabilities of the machine
type Capabilities struct {
	RDPAvailable      bool   `json:"rdp_available"`
	RDPEnabled        bool   `json:"rdp_enabled"`
	FallbackAvailable bool   `json:"fallback_available"`
	PreferredMethod   string `json:"preferred_method"`
	WindowsEdition    string `json:"windows_edition"`
	RDPPort           int    `json:"rdp_port"`
}

// GetCapabilities returns the RDP capabilities (always unavailable on non-Windows)
func (s *ShadowSession) GetCapabilities() *Capabilities {
	return &Capabilities{
		RDPAvailable:      false,
		RDPEnabled:        false,
		FallbackAvailable: true,
		PreferredMethod:   "fallback",
	}
}

// PrepareForShadow is not supported on non-Windows platforms
func (s *ShadowSession) PrepareForShadow() error {
	return fmt.Errorf("RDP not supported on this platform")
}

// StartSession is not supported on non-Windows platforms
func (s *ShadowSession) StartSession(ctx context.Context, serverURL, agentID string) (*ShadowInfo, error) {
	return nil, fmt.Errorf("RDP not supported on this platform")
}

// StopSession is not supported on non-Windows platforms
func (s *ShadowSession) StopSession() error {
	return fmt.Errorf("RDP not supported on this platform")
}

// IsActive always returns false on non-Windows platforms
func (s *ShadowSession) IsActive() bool {
	return false
}

// GetInfo returns nil on non-Windows platforms
func (s *ShadowSession) GetInfo() *ShadowInfo {
	return nil
}

// RefreshCredentials is not supported on non-Windows platforms
func (s *ShadowSession) RefreshCredentials() error {
	return fmt.Errorf("RDP not supported on this platform")
}

// TunnelStats holds statistics about the tunnel
type TunnelStats struct {
	BytesSent     uint64
	BytesReceived uint64
}

// GetTunnelStats returns nil on non-Windows platforms
func (s *ShadowSession) GetTunnelStats() *TunnelStats {
	return nil
}

// SetOnConnect is a no-op on non-Windows platforms
func (s *ShadowSession) SetOnConnect(callback func(info *ShadowInfo)) {}

// SetOnDisconnect is a no-op on non-Windows platforms
func (s *ShadowSession) SetOnDisconnect(callback func(reason string)) {}

// SetOnError is a no-op on non-Windows platforms
func (s *ShadowSession) SetOnError(callback func(err error)) {}

// RemoteDesktopMethod indicates which remote desktop method to use
type RemoteDesktopMethod string

const (
	MethodRDP      RemoteDesktopMethod = "rdp"
	MethodFallback RemoteDesktopMethod = "fallback"
)

// RemoteDesktopCapabilities describes the remote desktop capabilities
type RemoteDesktopCapabilities struct {
	RDPAvailable      bool                `json:"rdp_available"`
	RDPEnabled        bool                `json:"rdp_enabled"`
	FallbackAvailable bool                `json:"fallback_available"`
	PreferredMethod   RemoteDesktopMethod `json:"preferred_method"`
	WindowsEdition    string              `json:"windows_edition"`
	RDPPort           int                 `json:"rdp_port"`
	Reason            string              `json:"reason,omitempty"`
}

// SessionInfo contains information about an active remote desktop session
type SessionInfo struct {
	SessionID      string              `json:"session_id"`
	Method         RemoteDesktopMethod `json:"method"`
	WindowsSession uint32              `json:"windows_session"`
	Active         bool                `json:"active"`
	RDPInfo        *ShadowInfo         `json:"rdp_info,omitempty"`
}

// Manager coordinates remote desktop sessions (stub for non-Windows)
type Manager struct{}

// NewManager creates a new remote desktop manager
func NewManager() *Manager {
	return &Manager{}
}

// SetOnCapabilitiesChange is a no-op on non-Windows platforms
func (m *Manager) SetOnCapabilitiesChange(callback func(*RemoteDesktopCapabilities)) {}

// GetCapabilities returns capabilities (RDP unavailable on non-Windows)
func (m *Manager) GetCapabilities() *RemoteDesktopCapabilities {
	return &RemoteDesktopCapabilities{
		RDPAvailable:      false,
		RDPEnabled:        false,
		FallbackAvailable: true,
		PreferredMethod:   MethodFallback,
		Reason:            "RDP not supported on this platform",
	}
}

// StartSession is not supported on non-Windows platforms
func (m *Manager) StartSession(ctx context.Context, serverURL, agentID string) (*SessionInfo, error) {
	return nil, fmt.Errorf("RDP not supported on this platform")
}

// StopSession is a no-op on non-Windows platforms
func (m *Manager) StopSession() error {
	return nil
}

// GetActiveSession returns nil on non-Windows platforms
func (m *Manager) GetActiveSession() *SessionInfo {
	return nil
}

// IsActive returns false on non-Windows platforms
func (m *Manager) IsActive() bool {
	return false
}

// GetPreferredMethod returns fallback on non-Windows platforms
func (m *Manager) GetPreferredMethod() RemoteDesktopMethod {
	return MethodFallback
}

// RefreshCapabilities returns capabilities on non-Windows platforms
func (m *Manager) RefreshCapabilities() *RemoteDesktopCapabilities {
	return m.GetCapabilities()
}

// Prepare is a no-op on non-Windows platforms
func (m *Manager) Prepare() error {
	return nil
}

// Shutdown is a no-op on non-Windows platforms
func (m *Manager) Shutdown() {}

// GenerateSessionToken generates a random session token
func GenerateSessionToken() (string, error) {
	return "", fmt.Errorf("not implemented on this platform")
}

// TunnelConfig holds configuration for the RDP tunnel
type TunnelConfig struct {
	ServerURL    string
	AgentID      string
	LocalRDPAddr string
}

// Tunnel represents an RDP tunnel (stub for non-Windows)
type Tunnel struct{}

// NewTunnel creates a new RDP tunnel (stub)
func NewTunnel(config TunnelConfig) *Tunnel {
	return &Tunnel{}
}

// SetOnStateChange is a no-op on non-Windows platforms
func (t *Tunnel) SetOnStateChange(callback func(connected bool, err error)) {}

// Start is not supported on non-Windows platforms
func (t *Tunnel) Start(ctx context.Context, sessionID string) error {
	return fmt.Errorf("RDP not supported on this platform")
}

// Stop is a no-op on non-Windows platforms
func (t *Tunnel) Stop() {}

// IsActive returns false on non-Windows platforms
func (t *Tunnel) IsActive() bool {
	return false
}

// GetStats returns empty stats on non-Windows platforms
func (t *Tunnel) GetStats() TunnelStats {
	return TunnelStats{}
}

// TunnelManager manages multiple RDP tunnels (stub for non-Windows)
type TunnelManager struct{}

// NewTunnelManager creates a new tunnel manager
func NewTunnelManager() *TunnelManager {
	return &TunnelManager{}
}

// StartTunnel is not supported on non-Windows platforms
func (m *TunnelManager) StartTunnel(ctx context.Context, sessionID string, config TunnelConfig) error {
	return fmt.Errorf("RDP not supported on this platform")
}

// StopTunnel is a no-op on non-Windows platforms
func (m *TunnelManager) StopTunnel(sessionID string) {}

// StopAll is a no-op on non-Windows platforms
func (m *TunnelManager) StopAll() {}

// GetActiveTunnels returns 0 on non-Windows platforms
func (m *TunnelManager) GetActiveTunnels() int {
	return 0
}

// ShadowManager manages shadow sessions (stub for non-Windows)
type ShadowManager struct{}

// NewShadowManager creates a new shadow manager
func NewShadowManager() *ShadowManager {
	return &ShadowManager{}
}

// GetOrCreateSession returns a new shadow session (stub)
func (m *ShadowManager) GetOrCreateSession(windowsSessionID uint32) *ShadowSession {
	return NewShadowSession()
}

// StopAllSessions is a no-op on non-Windows platforms
func (m *ShadowManager) StopAllSessions() {}
