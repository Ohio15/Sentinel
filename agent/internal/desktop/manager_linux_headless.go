//go:build (linux && arm64) || (linux && arm) || darwin

package desktop

import (
	"context"
	"errors"
)

// Headless stub for platforms without a supported desktop-capture backend:
//   - ARM Linux (Synology NAS, etc.) — no X11 display assumed
//   - macOS (darwin) — no native capture backend has been built; the Sentinel
//     server-side agent on Macs runs in headless / observability-only mode.
//     This stub lets `cmd/sentinel-agent` cross-compile cleanly for darwin
//     amd64 + arm64 (needed by build-installers.yml's GitHub Release matrix);
//     attempting StartSession returns ErrHeadlessMode rather than crashing.
// Remote desktop not available without X11 display (on Linux) or without a
// native capture stack (on macOS).

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
