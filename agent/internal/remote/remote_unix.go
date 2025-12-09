//go:build !windows

package remote

import (
	"fmt"
	"log"
	"sync"
)

// Session represents a remote desktop session
type Session struct {
	ID      string
	Quality int
	Active  bool
	OnFrame func(data string, width, height int)
	mu      sync.Mutex
}

// Manager manages remote desktop sessions
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewManager creates a new remote desktop manager
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

// StartSession creates and starts a new remote desktop session
func (m *Manager) StartSession(sessionID string, quality string, onFrame func(data string, width, height int)) (*Session, error) {
	// Remote desktop not fully implemented on non-Windows platforms
	log.Printf("Remote desktop session requested on non-Windows platform")
	return nil, fmt.Errorf("remote desktop is only supported on Windows")
}

// StopSession stops a remote desktop session
func (m *Manager) StopSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// GetSession returns a session by ID
func (m *Manager) GetSession(sessionID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	return s, ok
}

// Stop stops the session
func (s *Session) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Active = false
}

// HandleInput processes mouse and keyboard input
func (s *Session) HandleInput(inputType string, data map[string]interface{}) {
	// Not implemented on non-Windows
}
