//go:build !windows

package terminal

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/sentinel/agent/internal/executor"
)

// PTYSession represents a PTY-based terminal session (Unix only)
type PTYSession struct {
	ID       string
	cmd      *exec.Cmd
	pty      *os.File
	done     chan struct{}
	mu       sync.Mutex
	onOutput func(data string)
	onClose  func()
}

// PTYManager manages PTY-based terminal sessions
type PTYManager struct {
	sessions map[string]*PTYSession
	mu       sync.RWMutex
}

// NewPTYManager creates a new PTY session manager
func NewPTYManager() *PTYManager {
	return &PTYManager{
		sessions: make(map[string]*PTYSession),
	}
}

// CreatePTYSession creates a new PTY-based terminal session
func (m *PTYManager) CreatePTYSession(ctx context.Context, sessionID string, onOutput func(data string), onClose func()) (*PTYSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[sessionID]; exists {
		return nil, fmt.Errorf("session already exists: %s", sessionID)
	}

	shell := executor.GetSystemShell()
	cmd := exec.CommandContext(ctx, shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start PTY: %w", err)
	}

	session := &PTYSession{
		ID:       sessionID,
		cmd:      cmd,
		pty:      ptmx,
		done:     make(chan struct{}),
		onOutput: onOutput,
		onClose:  onClose,
	}

	m.sessions[sessionID] = session

	// Read output from PTY
	go session.readOutput()

	// Wait for process to exit
	go func() {
		cmd.Wait()
		close(session.done)
		if onClose != nil {
			onClose()
		}
		m.removeSession(sessionID)
	}()

	return session, nil
}

// GetPTYSession returns a PTY session by ID
func (m *PTYManager) GetPTYSession(sessionID string) (*PTYSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[sessionID]
	return session, ok
}

// ClosePTYSession closes a PTY session
func (m *PTYManager) ClosePTYSession(sessionID string) error {
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session not found: %s", sessionID)
	}
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	return session.Close()
}

// CloseAllPTY closes all PTY sessions
func (m *PTYManager) CloseAllPTY() {
	m.mu.Lock()
	sessions := make([]*PTYSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*PTYSession)
	m.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}
}

func (m *PTYManager) removeSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// Write writes data to the PTY
func (s *PTYSession) Write(data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pty == nil {
		return fmt.Errorf("PTY closed")
	}

	_, err := s.pty.Write([]byte(data))
	return err
}

// Resize resizes the PTY
func (s *PTYSession) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pty == nil {
		return fmt.Errorf("PTY closed")
	}

	return pty.Setsize(s.pty, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}

// Close closes the PTY session
func (s *PTYSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pty != nil {
		s.pty.Close()
		s.pty = nil
	}

	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}

	return nil
}

// IsClosed returns true if the session is closed
func (s *PTYSession) IsClosed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *PTYSession) readOutput() {
	buf := make([]byte, 4096)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 && s.onOutput != nil {
			s.onOutput(string(buf[:n]))
		}
		if err != nil {
			if err != io.EOF {
				// Log error if not EOF
			}
			return
		}
	}
}
