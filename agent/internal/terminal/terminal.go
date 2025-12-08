package terminal

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"github.com/sentinel/agent/internal/executor"
)

// Session represents a terminal session
type Session struct {
	ID       string
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	stderr   io.ReadCloser
	done     chan struct{}
	mu       sync.Mutex
	onOutput func(data string)
	onClose  func()
}

// Manager manages multiple terminal sessions
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewManager creates a new terminal session manager
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

// CreateSession creates a new terminal session
func (m *Manager) CreateSession(ctx context.Context, sessionID string, onOutput func(data string), onClose func()) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[sessionID]; exists {
		return nil, fmt.Errorf("session already exists: %s", sessionID)
	}

	shell := executor.GetSystemShell()
	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		// On Windows, use cmd or powershell
		if shell == "powershell" {
			cmd = exec.CommandContext(ctx, "powershell", "-NoLogo", "-NoProfile", "-NoExit")
		} else {
			cmd = exec.CommandContext(ctx, "cmd")
		}
	} else {
		// On Unix, use the shell
		cmd = exec.CommandContext(ctx, shell)
	}

	// Set up environment
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	session := &Session{
		ID:       sessionID,
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
		done:     make(chan struct{}),
		onOutput: onOutput,
		onClose:  onClose,
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("failed to start shell: %w", err)
	}

	m.sessions[sessionID] = session

	// Start output readers
	go session.readOutput(stdout)
	go session.readOutput(stderr)

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

// GetSession returns a session by ID
func (m *Manager) GetSession(sessionID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[sessionID]
	return session, ok
}

// CloseSession closes a terminal session
func (m *Manager) CloseSession(sessionID string) error {
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

// CloseAll closes all terminal sessions
func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}
}

func (m *Manager) removeSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// Write writes data to the terminal
func (s *Session) Write(data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stdin == nil {
		return fmt.Errorf("terminal closed")
	}

	_, err := s.stdin.Write([]byte(data))
	return err
}

// Resize resizes the terminal (no-op for basic pipes, needed for PTY)
func (s *Session) Resize(cols, rows int) error {
	// For basic pipe-based terminals, resize is not supported
	// This would be implemented with PTY on Unix systems
	return nil
}

// Close closes the terminal session
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stdin != nil {
		s.stdin.Close()
		s.stdin = nil
	}

	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}

	return nil
}

// IsClosed returns true if the session is closed
func (s *Session) IsClosed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *Session) readOutput(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 && s.onOutput != nil {
			s.onOutput(string(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}
