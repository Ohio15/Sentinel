// +build windows

package desktop

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Microsoft/go-winio"
)

// IPCServer manages named pipe connections from helper processes
type IPCServer struct {
	sessionID uint32
	pipeName  string
	listener  net.Listener
	handler   IPCHandler

	mu       sync.Mutex
	conn     net.Conn
	reader   *bufio.Reader
	writer   *bufio.Writer
	closed   bool
	stopChan chan struct{}
}

// IPCHandler processes incoming messages from the helper
type IPCHandler interface {
	OnAuth(msg *IPCMessage, payload *AuthPayload) error
	OnHeartbeat(msg *IPCMessage, payload *HeartbeatPayload) error
	OnSessionAnswer(msg *IPCMessage, payload *SessionAnswerPayload) error
	OnICECandidate(msg *IPCMessage, payload *ICECandidatePayload) error
	OnStatus(msg *IPCMessage, payload *StatusPayload) error
	OnDisconnect()
}

// NewIPCServer creates a new IPC server for the given session
func NewIPCServer(sessionID uint32, handler IPCHandler) (*IPCServer, error) {
	pipeName := fmt.Sprintf("%s%d", PipeNamePrefix, sessionID)

	// Create named pipe with security descriptor allowing SYSTEM and the session user
	config := &winio.PipeConfig{
		SecurityDescriptor: "", // Default: creator and SYSTEM have full access
		MessageMode:        false,
		InputBufferSize:    65536,
		OutputBufferSize:   65536,
	}

	listener, err := winio.ListenPipe(pipeName, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create named pipe %s: %w", pipeName, err)
	}

	log.Printf("[IPC] Server listening on %s", pipeName)

	return &IPCServer{
		sessionID: sessionID,
		pipeName:  pipeName,
		listener:  listener,
		handler:   handler,
		stopChan:  make(chan struct{}),
	}, nil
}

// Start begins accepting connections and processing messages
func (s *IPCServer) Start() {
	go s.acceptLoop()
}

// acceptLoop waits for helper connections
func (s *IPCServer) acceptLoop() {
	for {
		select {
		case <-s.stopChan:
			return
		default:
		}

		// Accept with timeout so we can check stopChan
		if dl, ok := s.listener.(interface{ SetDeadline(time.Time) error }); ok {
			dl.SetDeadline(time.Now().Add(1 * time.Second))
		}

		conn, err := s.listener.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-s.stopChan:
				return
			default:
				log.Printf("[IPC] Accept error: %v", err)
				continue
			}
		}

		log.Printf("[IPC] Helper connected to pipe")
		s.handleConnection(conn)
	}
}

// handleConnection processes messages from a single connection
func (s *IPCServer) handleConnection(conn net.Conn) {
	s.mu.Lock()
	// Close any existing connection
	if s.conn != nil {
		s.conn.Close()
	}
	s.conn = conn
	s.reader = bufio.NewReader(conn)
	s.writer = bufio.NewWriter(conn)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		if s.conn == conn {
			s.conn = nil
			s.reader = nil
			s.writer = nil
		}
		s.mu.Unlock()
		conn.Close()
		s.handler.OnDisconnect()
	}()

	for {
		select {
		case <-s.stopChan:
			return
		default:
		}

		// Set read deadline to detect disconnection
		conn.SetReadDeadline(time.Now().Add(HeartbeatTimeout))

		msg, err := s.readMessage()
		if err != nil {
			if err == io.EOF {
				log.Printf("[IPC] Helper disconnected")
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("[IPC] Heartbeat timeout - helper not responding")
			} else {
				log.Printf("[IPC] Read error: %v", err)
			}
			return
		}

		if err := s.dispatchMessage(msg); err != nil {
			log.Printf("[IPC] Error handling message type=%s: %v", msg.Type, err)
			// Send error response
			s.SendMessage(NewErrorMessage(msg.RequestID, err.Error()))
		}
	}
}

// readMessage reads a single JSON message from the pipe
func (s *IPCServer) readMessage() (*IPCMessage, error) {
	s.mu.Lock()
	reader := s.reader
	s.mu.Unlock()

	if reader == nil {
		return nil, fmt.Errorf("not connected")
	}

	// Read until newline (messages are newline-delimited JSON)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}

	var msg IPCMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	return &msg, nil
}

// dispatchMessage routes the message to the appropriate handler
func (s *IPCServer) dispatchMessage(msg *IPCMessage) error {
	switch msg.Type {
	case MsgTypeAuth:
		var payload AuthPayload
		if err := msg.ParsePayload(&payload); err != nil {
			return err
		}
		return s.handler.OnAuth(msg, &payload)

	case MsgTypeHeartbeat:
		var payload HeartbeatPayload
		if err := msg.ParsePayload(&payload); err != nil {
			return err
		}
		return s.handler.OnHeartbeat(msg, &payload)

	case MsgTypeSessionAnswer:
		var payload SessionAnswerPayload
		if err := msg.ParsePayload(&payload); err != nil {
			return err
		}
		return s.handler.OnSessionAnswer(msg, &payload)

	case MsgTypeICECandidate:
		var payload ICECandidatePayload
		if err := msg.ParsePayload(&payload); err != nil {
			return err
		}
		return s.handler.OnICECandidate(msg, &payload)

	case MsgTypeStatus:
		var payload StatusPayload
		if err := msg.ParsePayload(&payload); err != nil {
			return err
		}
		return s.handler.OnStatus(msg, &payload)

	default:
		return fmt.Errorf("unknown message type: %s", msg.Type)
	}
}

// SendMessage sends a message to the connected helper
func (s *IPCServer) SendMessage(msg *IPCMessage) error {
	s.mu.Lock()
	writer := s.writer
	conn := s.conn
	s.mu.Unlock()

	if writer == nil || conn == nil {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// Set write deadline
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.writer.Write(data); err != nil {
		return err
	}
	if _, err := s.writer.Write([]byte("\n")); err != nil {
		return err
	}
	return s.writer.Flush()
}

// SendStartSession sends a WebRTC start session request to the helper
func (s *IPCServer) SendStartSession(requestID, connectionID, sdpType, sdp string) error {
	msg, err := NewIPCMessage(MsgTypeStartSession, requestID, &StartSessionPayload{
		SDPType:      sdpType,
		SDP:          sdp,
		ConnectionID: connectionID,
	})
	if err != nil {
		return err
	}
	return s.SendMessage(msg)
}

// SendStopSession sends a stop session request to the helper
func (s *IPCServer) SendStopSession(requestID, connectionID, reason string) error {
	msg, err := NewIPCMessage(MsgTypeStopSession, requestID, &StopSessionPayload{
		ConnectionID: connectionID,
		Reason:       reason,
	})
	if err != nil {
		return err
	}
	return s.SendMessage(msg)
}

// SendICECandidate forwards an ICE candidate to the helper
func (s *IPCServer) SendICECandidate(connectionID, candidate, sdpMid string, sdpMLineIndex *int) error {
	msg, err := NewIPCMessage(MsgTypeICECandidate, "", &ICECandidatePayload{
		Candidate:     candidate,
		SDPMid:        sdpMid,
		SDPMLineIndex: sdpMLineIndex,
		ConnectionID:  connectionID,
	})
	if err != nil {
		return err
	}
	return s.SendMessage(msg)
}

// SendShutdown requests the helper to shutdown gracefully
func (s *IPCServer) SendShutdown(reason string, timeout time.Duration) error {
	msg, err := NewIPCMessage(MsgTypeShutdown, "", &ShutdownPayload{
		Reason:  reason,
		Timeout: timeout,
	})
	if err != nil {
		return err
	}
	return s.SendMessage(msg)
}

// IsConnected returns true if a helper is connected
func (s *IPCServer) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil
}

// Close shuts down the IPC server
func (s *IPCServer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.stopChan)

	if s.conn != nil {
		s.conn.Close()
	}
	s.mu.Unlock()

	return s.listener.Close()
}

// PipeName returns the full pipe name for this server
func (s *IPCServer) PipeName() string {
	return s.pipeName
}
