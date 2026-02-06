//go:build windows
// +build windows

package rdp

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// RemoteDesktopMethod indicates which remote desktop method to use
type RemoteDesktopMethod string

const (
	// MethodRDP uses native Windows RDP shadow mode
	MethodRDP RemoteDesktopMethod = "rdp"
	// MethodFallback uses screen capture fallback (for Windows Home)
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
	SessionID   string              `json:"session_id"`
	Method      RemoteDesktopMethod `json:"method"`
	WindowsSession uint32           `json:"windows_session"`
	Active      bool                `json:"active"`

	// RDP-specific info (nil if using fallback)
	RDPInfo *ShadowInfo `json:"rdp_info,omitempty"`
}

// Manager coordinates remote desktop sessions, choosing between RDP and fallback
type Manager struct {
	mu sync.Mutex

	config        *Config
	shadowSession *ShadowSession

	// Current active session
	activeSession *SessionInfo

	// Callbacks
	onCapabilitiesChange func(*RemoteDesktopCapabilities)
}

// NewManager creates a new remote desktop manager
func NewManager() *Manager {
	return &Manager{
		config:        NewConfig(),
		shadowSession: NewShadowSession(),
	}
}

// SetOnCapabilitiesChange sets the callback for capability changes
func (m *Manager) SetOnCapabilitiesChange(callback func(*RemoteDesktopCapabilities)) {
	m.onCapabilitiesChange = callback
}

// GetCapabilities returns the current remote desktop capabilities
func (m *Manager) GetCapabilities() *RemoteDesktopCapabilities {
	caps := &RemoteDesktopCapabilities{
		RDPAvailable:      m.config.IsRDPAvailable(),
		RDPEnabled:        m.config.IsRDPEnabled(),
		FallbackAvailable: true, // WebRTC fallback is always available
		WindowsEdition:    m.config.GetWindowsEdition(),
		RDPPort:           m.config.GetRDPPort(),
	}

	if caps.RDPAvailable {
		caps.PreferredMethod = MethodRDP
		if !caps.RDPEnabled {
			caps.Reason = "RDP available but not enabled (will be enabled on first connection)"
		}
	} else {
		caps.PreferredMethod = MethodFallback
		caps.Reason = "RDP not available on this Windows edition, using screen capture fallback"
	}

	return caps
}

// StartSession starts a remote desktop session using the best available method
func (m *Manager) StartSession(ctx context.Context, serverURL, agentID string) (*SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if session already active
	if m.activeSession != nil && m.activeSession.Active {
		return nil, fmt.Errorf("session already active")
	}

	caps := m.GetCapabilities()
	log.Printf("[RDP Manager] Starting session with method: %s (RDP available: %v)",
		caps.PreferredMethod, caps.RDPAvailable)

	if caps.PreferredMethod == MethodRDP {
		return m.startRDPSession(ctx, serverURL, agentID)
	}

	return m.startFallbackSession(ctx, serverURL, agentID)
}

// startRDPSession starts an RDP shadow session
func (m *Manager) startRDPSession(ctx context.Context, serverURL, agentID string) (*SessionInfo, error) {
	// Start shadow session
	shadowInfo, err := m.shadowSession.StartSession(ctx, serverURL, agentID)
	if err != nil {
		log.Printf("[RDP Manager] RDP session failed, falling back: %v", err)
		// Fall back to screen capture
		return m.startFallbackSession(ctx, serverURL, agentID)
	}

	m.activeSession = &SessionInfo{
		SessionID:      shadowInfo.Token,
		Method:         MethodRDP,
		WindowsSession: shadowInfo.SessionID,
		Active:         true,
		RDPInfo:        shadowInfo,
	}

	log.Printf("[RDP Manager] RDP session started: %s", m.activeSession.SessionID)
	return m.activeSession, nil
}

// startFallbackSession starts a screen capture fallback session
func (m *Manager) startFallbackSession(ctx context.Context, serverURL, agentID string) (*SessionInfo, error) {
	// Get active console session ID
	sessionID, err := m.config.GetActiveConsoleSessionID()
	if err != nil {
		return nil, fmt.Errorf("failed to get console session: %w", err)
	}

	// Generate a session token
	token, err := GenerateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	m.activeSession = &SessionInfo{
		SessionID:      token,
		Method:         MethodFallback,
		WindowsSession: sessionID,
		Active:         true,
	}

	log.Printf("[RDP Manager] Fallback session started: %s (use existing WebRTC handler)", m.activeSession.SessionID)

	// Note: The actual fallback session is handled by the existing WebRTC desktop helper
	// This just provides the session info for coordination

	return m.activeSession, nil
}

// StopSession stops the current remote desktop session
func (m *Manager) StopSession() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeSession == nil {
		return nil
	}

	log.Printf("[RDP Manager] Stopping session: %s (method: %s)",
		m.activeSession.SessionID, m.activeSession.Method)

	if m.activeSession.Method == MethodRDP {
		if err := m.shadowSession.StopSession(); err != nil {
			log.Printf("[RDP Manager] Warning: failed to stop shadow session: %v", err)
		}
	}

	// For fallback, the WebRTC handler manages its own lifecycle

	m.activeSession.Active = false
	m.activeSession = nil

	return nil
}

// GetActiveSession returns the currently active session, if any
func (m *Manager) GetActiveSession() *SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeSession == nil {
		return nil
	}

	// Return a copy
	info := *m.activeSession
	return &info
}

// IsActive returns whether a remote desktop session is currently active
func (m *Manager) IsActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeSession != nil && m.activeSession.Active
}

// GetPreferredMethod returns the preferred remote desktop method
func (m *Manager) GetPreferredMethod() RemoteDesktopMethod {
	if m.config.IsRDPAvailable() {
		return MethodRDP
	}
	return MethodFallback
}

// RefreshCapabilities re-checks the system capabilities
// Call this if system configuration might have changed
func (m *Manager) RefreshCapabilities() *RemoteDesktopCapabilities {
	caps := m.GetCapabilities()

	if m.onCapabilitiesChange != nil {
		m.onCapabilitiesChange(caps)
	}

	return caps
}

// Prepare prepares the system for remote desktop (enables RDP if available and not enabled)
func (m *Manager) Prepare() error {
	caps := m.GetCapabilities()

	if !caps.RDPAvailable {
		log.Printf("[RDP Manager] RDP not available, skipping preparation")
		return nil
	}

	if caps.RDPEnabled {
		log.Printf("[RDP Manager] RDP already enabled")
		return nil
	}

	log.Printf("[RDP Manager] Enabling RDP...")
	if err := m.config.EnableRDP(); err != nil {
		return fmt.Errorf("failed to enable RDP: %w", err)
	}

	log.Printf("[RDP Manager] Enabling shadow mode...")
	if err := m.config.EnableShadowing(); err != nil {
		return fmt.Errorf("failed to enable shadowing: %w", err)
	}

	log.Printf("[RDP Manager] Configuring firewall...")
	if err := m.config.ConfigureFirewall(); err != nil {
		log.Printf("[RDP Manager] Warning: firewall configuration failed: %v", err)
		// Non-fatal
	}

	m.RefreshCapabilities()
	return nil
}

// Shutdown gracefully shuts down the manager and any active sessions
func (m *Manager) Shutdown() {
	log.Printf("[RDP Manager] Shutting down...")
	m.StopSession()
}
