//go:build (linux && arm64) || (linux && arm)

package desktop

import (
	"context"
	"errors"
)

// Headless stub for ARM Linux (Synology NAS, etc.)
// Remote desktop not available without X11 display

var ErrHeadlessMode = errors.New("remote desktop not available in headless mode")

// Manager stub for headless systems
type Manager struct {
	helperPath string
}

// NewManager creates a stub manager for headless systems
func NewManager(helperPath string) *Manager {
	return &Manager{helperPath: helperPath}
}

// SetCallbacks sets the callback functions (no-op on headless)
func (m *Manager) SetCallbacks(
	onSessionAnswer func(sessionID uint32, connectionID, sdpType, sdp string),
	onICECandidate func(sessionID uint32, connectionID, candidate, sdpMid string, sdpMLineIndex *int),
	onStatusUpdate func(sessionID uint32, state HelperState, message, connectionID string),
) {
	// No-op for headless
}

// StartSession returns an error on headless systems - remote desktop not available
func (m *Manager) StartSession(ctx context.Context, sessionID uint32, connectionID, sdpType, sdp string) (string, string, error) {
	return "", "", ErrHeadlessMode
}

// StopSession is a no-op on headless systems
func (m *Manager) StopSession(sessionID uint32, connectionID, reason string) error {
	return nil
}

// AddICECandidate returns an error on headless systems
func (m *Manager) AddICECandidate(sessionID uint32, connectionID, candidate, sdpMid string, sdpMLineIndex *int) error {
	return ErrHeadlessMode
}

// Shutdown gracefully shuts down all sessions (no-op on headless)
func (m *Manager) Shutdown() {
	// No-op for headless
}

// StopAllSessions stops all active sessions (no-op on headless)
func (m *Manager) StopAllSessions() {
	// No-op for headless
}

// IsHeadless returns true for headless systems
func (m *Manager) IsHeadless() bool {
	return true
}

// GetActiveConsoleSessionID returns 0 on headless systems (no console session concept)
func GetActiveConsoleSessionID() uint32 {
	return 0
}
