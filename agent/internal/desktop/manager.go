// +build windows

package desktop

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Manager coordinates desktop helper processes
type Manager struct {
	mu       sync.Mutex
	sessions map[uint32]*HelperSession
	helperPath string

	// Callbacks for forwarding messages to server
	onSessionAnswer func(sessionID uint32, connectionID, sdpType, sdp string)
	onICECandidate  func(sessionID uint32, connectionID, candidate, sdpMid string, sdpMLineIndex *int)
	onStatusUpdate  func(sessionID uint32, state HelperState, message, connectionID string)
}

// HelperSession represents a connection to a helper process
type HelperSession struct {
	SessionID    uint32
	ConnectionID string
	Server       *IPCServer
	Process      *os.Process
	cmd          *exec.Cmd // Only used when not running as service

	// Channels for synchronization
	answerChan   chan *SessionAnswerPayload
	stopChan     chan struct{}

	// Token for this session
	token        string
	tokenExpiry  time.Time
}

// NewManager creates a new desktop manager
func NewManager(helperPath string) *Manager {
	return &Manager{
		sessions:   make(map[uint32]*HelperSession),
		helperPath: helperPath,
	}
}

// SetCallbacks sets the callback functions for forwarding messages
func (m *Manager) SetCallbacks(
	onSessionAnswer func(sessionID uint32, connectionID, sdpType, sdp string),
	onICECandidate func(sessionID uint32, connectionID, candidate, sdpMid string, sdpMLineIndex *int),
	onStatusUpdate func(sessionID uint32, state HelperState, message, connectionID string),
) {
	m.onSessionAnswer = onSessionAnswer
	m.onICECandidate = onICECandidate
	m.onStatusUpdate = onStatusUpdate
}

// StartSession starts a WebRTC session for the given Windows session
func (m *Manager) StartSession(ctx context.Context, sessionID uint32, connectionID, sdpType, sdp string) (string, string, error) {
	log.Printf("[Manager] StartSession called, sessionID=%d, connectionID=%s", sessionID, connectionID)

	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	m.mu.Unlock()

	if !exists {
		// Need to spawn a new helper
		var err error
		session, err = m.spawnHelper(sessionID)
		if err != nil {
			return "", "", fmt.Errorf("failed to spawn helper: %w", err)
		}

		m.mu.Lock()
		m.sessions[sessionID] = session
		m.mu.Unlock()
	}

	session.ConnectionID = connectionID
	session.answerChan = make(chan *SessionAnswerPayload, 1)

	// Send start session command to helper
	if err := session.Server.SendStartSession("start-1", connectionID, sdpType, sdp); err != nil {
		return "", "", fmt.Errorf("failed to send start session: %w", err)
	}

	// Wait for answer with timeout
	select {
	case answer := <-session.answerChan:
		log.Printf("[Manager] Got answer from helper, sdpType=%s, sdp length=%d", answer.SDPType, len(answer.SDP))
		return answer.SDPType, answer.SDP, nil
	case <-time.After(30 * time.Second):
		return "", "", fmt.Errorf("timeout waiting for WebRTC answer from helper")
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}

// StopSession stops the WebRTC session for the given Windows session
func (m *Manager) StopSession(sessionID uint32, connectionID, reason string) error {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("no session for sessionID %d", sessionID)
	}

	return session.Server.SendStopSession("stop-1", connectionID, reason)
}

// AddICECandidate forwards an ICE candidate to the helper
func (m *Manager) AddICECandidate(sessionID uint32, connectionID, candidate, sdpMid string, sdpMLineIndex *int) error {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("no session for sessionID %d", sessionID)
	}

	return session.Server.SendICECandidate(connectionID, candidate, sdpMid, sdpMLineIndex)
}

// Shutdown gracefully shuts down all helpers
func (m *Manager) Shutdown() {
	m.mu.Lock()
	sessions := make([]*HelperSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()

	for _, session := range sessions {
		log.Printf("[Manager] Shutting down helper for session %d", session.SessionID)
		session.Server.SendShutdown("service shutdown", GracefulShutdownTimeout)
		session.Server.Close()

		// Kill process if it doesn't exit gracefully
		if session.Process != nil {
			go func(proc *os.Process) {
				time.Sleep(GracefulShutdownTimeout)
				// Check if process is still running by sending signal 0
				if err := proc.Signal(os.Signal(nil)); err == nil {
					log.Printf("[Manager] Force killing helper process")
					proc.Kill()
				}
			}(session.Process)
		}
	}
}

// spawnHelper spawns a new helper process for the session
func (m *Manager) spawnHelper(sessionID uint32) (*HelperSession, error) {
	log.Printf("[Manager] Spawning helper for session %d", sessionID)

	// Generate token for this session
	token := generateToken(sessionID)

	// Create IPC server first
	handler := &helperHandler{
		manager:   m,
		sessionID: sessionID,
	}

	server, err := NewIPCServer(sessionID, handler)
	if err != nil {
		return nil, fmt.Errorf("failed to create IPC server: %w", err)
	}

	// Start accepting connections
	server.Start()

	session := &HelperSession{
		SessionID:   sessionID,
		Server:      server,
		token:       token,
		tokenExpiry: time.Now().Add(TokenTTL),
		stopChan:    make(chan struct{}),
	}

	handler.session = session

	// Get helper path
	helperPath := m.helperPath
	if helperPath == "" {
		// Default to same directory as service
		exePath, err := os.Executable()
		if err != nil {
			exePath = os.Args[0]
		}
		helperPath = filepath.Join(filepath.Dir(exePath), "sentinel-desktop.exe")
	}

	args := []string{
		"--session-id", fmt.Sprintf("%d", sessionID),
		"--token", token,
	}

	log.Printf("[Manager] Launching helper: %s %v", helperPath, args)

	// Check if we're running as a Windows service (Session 0)
	if IsServiceRunning() {
		log.Printf("[Manager] Running as service, using CreateProcessAsUser")

		proc, err := SpawnInSession(sessionID, helperPath, args)
		if err != nil {
			server.Close()
			return nil, fmt.Errorf("failed to spawn helper in session: %w", err)
		}

		session.Process = proc

		// Monitor process
		go func() {
			state, err := proc.Wait()
			log.Printf("[Manager] Helper process exited: %v (state: %v)", err, state)

			m.mu.Lock()
			delete(m.sessions, sessionID)
			m.mu.Unlock()

			server.Close()
		}()
	} else {
		// Running interactively, use regular exec.Command
		log.Printf("[Manager] Running interactively, using exec.Command")

		cmd := exec.Command(helperPath, args...)
		if err := cmd.Start(); err != nil {
			server.Close()
			return nil, fmt.Errorf("failed to start helper: %w", err)
		}

		session.cmd = cmd
		session.Process = cmd.Process

		// Monitor process
		go func() {
			err := cmd.Wait()
			log.Printf("[Manager] Helper process exited: %v", err)

			m.mu.Lock()
			delete(m.sessions, sessionID)
			m.mu.Unlock()

			server.Close()
		}()
	}

	// Wait for helper to connect
	log.Printf("[Manager] Waiting for helper to connect...")
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			if session.Process != nil {
				session.Process.Kill()
			}
			server.Close()
			return nil, fmt.Errorf("timeout waiting for helper to connect")
		case <-ticker.C:
			if server.IsConnected() {
				log.Printf("[Manager] Helper connected")
				return session, nil
			}
		}
	}
}

// generateToken creates a simple token for the session
// In Phase 4, this will be replaced with proper HMAC-signed tokens
func generateToken(sessionID uint32) string {
	return fmt.Sprintf("token-%d-%d", sessionID, time.Now().UnixNano())
}

// helperHandler implements IPCHandler for processing helper messages
type helperHandler struct {
	manager   *Manager
	sessionID uint32
	session   *HelperSession
}

func (h *helperHandler) OnAuth(msg *IPCMessage, payload *AuthPayload) error {
	log.Printf("[Manager] Auth from helper: sessionID=%d, pid=%d", payload.SessionID, payload.PID)

	// Validate token
	if h.session.token != payload.Token {
		return fmt.Errorf("invalid token")
	}

	if time.Now().After(h.session.tokenExpiry) {
		return fmt.Errorf("token expired")
	}

	// Send auth OK
	response, err := NewIPCMessage(MsgTypeAuthOK, msg.RequestID, &AuthOKPayload{
		Capabilities: []string{"capture", "input"},
		ExpiresAt:    h.session.tokenExpiry,
	})
	if err != nil {
		return err
	}

	return h.session.Server.SendMessage(response)
}

func (h *helperHandler) OnHeartbeat(msg *IPCMessage, payload *HeartbeatPayload) error {
	// Send heartbeat ack
	response, err := NewIPCMessage(MsgTypeHeartbeatAck, msg.RequestID, &HeartbeatAckPayload{
		Timestamp: time.Now(),
		Continue:  true,
	})
	if err != nil {
		return err
	}

	return h.session.Server.SendMessage(response)
}

func (h *helperHandler) OnSessionAnswer(msg *IPCMessage, payload *SessionAnswerPayload) error {
	log.Printf("[Manager] Session answer from helper: connectionID=%s", payload.ConnectionID)

	// Forward to channel if waiting
	if h.session.answerChan != nil {
		select {
		case h.session.answerChan <- payload:
		default:
		}
	}

	// Also forward via callback
	if h.manager.onSessionAnswer != nil {
		h.manager.onSessionAnswer(h.sessionID, payload.ConnectionID, payload.SDPType, payload.SDP)
	}

	return nil
}

func (h *helperHandler) OnICECandidate(msg *IPCMessage, payload *ICECandidatePayload) error {
	log.Printf("[Manager] ICE candidate from helper: connectionID=%s", payload.ConnectionID)

	if h.manager.onICECandidate != nil {
		h.manager.onICECandidate(h.sessionID, payload.ConnectionID, payload.Candidate, payload.SDPMid, payload.SDPMLineIndex)
	}

	return nil
}

func (h *helperHandler) OnStatus(msg *IPCMessage, payload *StatusPayload) error {
	log.Printf("[Manager] Status from helper: state=%s, message=%s", payload.State, payload.Message)

	if h.manager.onStatusUpdate != nil {
		h.manager.onStatusUpdate(h.sessionID, payload.State, payload.Message, payload.ConnectionID)
	}

	return nil
}

func (h *helperHandler) OnDisconnect() {
	log.Printf("[Manager] Helper disconnected for session %d", h.sessionID)
}
