//go:build windows
// +build windows

package rdp

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	// DefaultShadowUsername is the default username for shadow credentials
	DefaultShadowUsername = "SentinelShadow"

	// CredentialTTL is how long shadow credentials remain valid
	CredentialTTL = 30 * time.Minute
)

// ShadowInfo contains information about an active shadow session
type ShadowInfo struct {
	SessionID   uint32
	Username    string
	Password    string
	TunnelHost  string
	TunnelPort  int
	Token       string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	TargetUser  string // The user being shadowed
}

// ShadowSession manages RDP shadow connections to the console session
type ShadowSession struct {
	mu sync.Mutex

	config    *Config
	tunnel    *Tunnel
	info      *ShadowInfo
	active    bool
	cleanedUp bool

	ctx    context.Context
	cancel context.CancelFunc

	// Callbacks
	onConnect    func(info *ShadowInfo)
	onDisconnect func(reason string)
	onError      func(err error)
}

// NewShadowSession creates a new shadow session manager
func NewShadowSession() *ShadowSession {
	return &ShadowSession{
		config: NewConfig(),
	}
}

// SetOnConnect sets the callback for when shadow connection is established
func (s *ShadowSession) SetOnConnect(callback func(info *ShadowInfo)) {
	s.onConnect = callback
}

// SetOnDisconnect sets the callback for when shadow disconnects
func (s *ShadowSession) SetOnDisconnect(callback func(reason string)) {
	s.onDisconnect = callback
}

// SetOnError sets the callback for errors
func (s *ShadowSession) SetOnError(callback func(err error)) {
	s.onError = callback
}

// GetCapabilities returns the RDP capabilities of this machine
func (s *ShadowSession) GetCapabilities() *Capabilities {
	caps := &Capabilities{
		RDPAvailable:      s.config.IsRDPAvailable(),
		RDPEnabled:        s.config.IsRDPEnabled(),
		FallbackAvailable: true, // Screen capture fallback is always available
		WindowsEdition:    s.config.GetWindowsEdition(),
		RDPPort:           s.config.GetRDPPort(),
	}

	if caps.RDPAvailable {
		caps.PreferredMethod = "rdp"
	} else {
		caps.PreferredMethod = "fallback"
	}

	return caps
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

// PrepareForShadow ensures the system is ready for RDP shadowing
func (s *ShadowSession) PrepareForShadow() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Printf("[ShadowSession] Preparing for shadow session")

	// Check if RDP is available (not Windows Home)
	if !s.config.IsRDPAvailable() {
		edition := s.config.GetWindowsEdition()
		return fmt.Errorf("RDP not available on this Windows edition (%s) - likely Windows Home", edition)
	}

	// Enable RDP if not already enabled
	if !s.config.IsRDPEnabled() {
		log.Printf("[ShadowSession] RDP not enabled, enabling...")
		if err := s.config.EnableRDP(); err != nil {
			return fmt.Errorf("failed to enable RDP: %w", err)
		}
	}

	// Enable shadow mode
	log.Printf("[ShadowSession] Enabling shadow mode...")
	if err := s.config.EnableShadowing(); err != nil {
		return fmt.Errorf("failed to enable shadowing: %w", err)
	}

	// Configure firewall
	log.Printf("[ShadowSession] Configuring firewall...")
	if err := s.config.ConfigureFirewall(); err != nil {
		log.Printf("[ShadowSession] Warning: failed to configure firewall: %v", err)
		// Non-fatal, continue
	}

	log.Printf("[ShadowSession] System prepared for shadow session")
	return nil
}

// StartSession starts a shadow session
func (s *ShadowSession) StartSession(ctx context.Context, serverURL, agentID string) (*ShadowInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active {
		return nil, fmt.Errorf("shadow session already active")
	}

	// Prepare system for shadowing
	s.mu.Unlock()
	if err := s.PrepareForShadow(); err != nil {
		s.mu.Lock()
		return nil, err
	}
	s.mu.Lock()

	// Get console session ID
	sessionID, err := s.config.GetActiveConsoleSessionID()
	if err != nil {
		return nil, fmt.Errorf("failed to get console session ID: %w", err)
	}

	log.Printf("[ShadowSession] Console session ID: %d", sessionID)

	// Create shadow credentials
	creds, err := s.config.CreateShadowCredentials(DefaultShadowUsername)
	if err != nil {
		return nil, fmt.Errorf("failed to create shadow credentials: %w", err)
	}

	// Generate session token
	token, err := GenerateSessionToken()
	if err != nil {
		s.config.CleanupShadowCredentials(DefaultShadowUsername)
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}

	// Create session info
	now := time.Now()
	s.info = &ShadowInfo{
		SessionID:  sessionID,
		Username:   creds.Username,
		Password:   creds.Password,
		TunnelHost: "127.0.0.1", // Tunnel endpoint
		TunnelPort: s.config.GetRDPPort(),
		Token:      token,
		CreatedAt:  now,
		ExpiresAt:  now.Add(CredentialTTL),
	}

	// Create context for this session
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Create and start tunnel
	tunnelConfig := TunnelConfig{
		ServerURL:    serverURL,
		AgentID:      agentID,
		LocalRDPAddr: fmt.Sprintf("127.0.0.1:%d", s.config.GetRDPPort()),
	}

	s.tunnel = NewTunnel(tunnelConfig)
	s.tunnel.SetOnStateChange(func(connected bool, err error) {
		if !connected {
			s.handleDisconnect("tunnel disconnected")
		}
	})

	if err := s.tunnel.Start(s.ctx, token); err != nil {
		s.config.CleanupShadowCredentials(DefaultShadowUsername)
		return nil, fmt.Errorf("failed to start tunnel: %w", err)
	}

	s.active = true
	s.cleanedUp = false

	// Start credential expiry monitor
	go s.monitorCredentialExpiry()

	log.Printf("[ShadowSession] Shadow session started, sessionID=%d, token=%s", sessionID, token[:16]+"...")

	if s.onConnect != nil {
		s.onConnect(s.info)
	}

	return s.info, nil
}

// StopSession stops the shadow session
func (s *ShadowSession) StopSession() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.stopSessionLocked("manual stop")
}

func (s *ShadowSession) stopSessionLocked(reason string) error {
	if !s.active {
		return nil
	}

	log.Printf("[ShadowSession] Stopping shadow session: %s", reason)

	s.active = false

	// Cancel context
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}

	// Stop tunnel
	if s.tunnel != nil {
		s.tunnel.Stop()
		s.tunnel = nil
	}

	// Cleanup credentials
	if !s.cleanedUp && s.info != nil {
		if err := s.config.CleanupShadowCredentials(s.info.Username); err != nil {
			log.Printf("[ShadowSession] Warning: failed to cleanup credentials: %v", err)
		}
		s.cleanedUp = true
	}

	s.info = nil

	log.Printf("[ShadowSession] Shadow session stopped")

	if s.onDisconnect != nil {
		s.onDisconnect(reason)
	}

	return nil
}

func (s *ShadowSession) handleDisconnect(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return
	}

	s.stopSessionLocked(reason)
}

// monitorCredentialExpiry watches for credential expiration
func (s *ShadowSession) monitorCredentialExpiry() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.info != nil && time.Now().After(s.info.ExpiresAt) {
				log.Printf("[ShadowSession] Credentials expired, stopping session")
				s.stopSessionLocked("credentials expired")
			}
			s.mu.Unlock()
		}
	}
}

// IsActive returns whether a shadow session is currently active
func (s *ShadowSession) IsActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// GetInfo returns information about the current shadow session
func (s *ShadowSession) GetInfo() *ShadowInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.info == nil {
		return nil
	}
	// Return a copy
	infoCopy := *s.info
	return &infoCopy
}

// RefreshCredentials extends the credential expiration
func (s *ShadowSession) RefreshCredentials() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active || s.info == nil {
		return fmt.Errorf("no active session")
	}

	// Update expiry time
	s.info.ExpiresAt = time.Now().Add(CredentialTTL)
	log.Printf("[ShadowSession] Credentials refreshed, new expiry: %v", s.info.ExpiresAt)

	return nil
}

// GetTunnelStats returns statistics about the tunnel
func (s *ShadowSession) GetTunnelStats() *TunnelStats {
	s.mu.Lock()
	tunnel := s.tunnel
	s.mu.Unlock()

	if tunnel == nil {
		return nil
	}

	stats := tunnel.GetStats()
	return &stats
}

// ShadowManager manages shadow sessions across multiple Windows sessions
type ShadowManager struct {
	mu       sync.Mutex
	sessions map[uint32]*ShadowSession
}

// NewShadowManager creates a new shadow manager
func NewShadowManager() *ShadowManager {
	return &ShadowManager{
		sessions: make(map[uint32]*ShadowSession),
	}
}

// GetOrCreateSession gets an existing session or creates a new one
func (m *ShadowManager) GetOrCreateSession(windowsSessionID uint32) *ShadowSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.sessions[windowsSessionID]; ok {
		return session
	}

	session := NewShadowSession()
	m.sessions[windowsSessionID] = session
	return session
}

// StopAllSessions stops all active shadow sessions
func (m *ShadowManager) StopAllSessions() {
	m.mu.Lock()
	sessions := make([]*ShadowSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[uint32]*ShadowSession)
	m.mu.Unlock()

	for _, s := range sessions {
		s.StopSession()
	}

	log.Printf("[ShadowManager] Stopped all sessions")
}
