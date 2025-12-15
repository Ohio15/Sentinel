// +build windows

package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/Microsoft/go-winio"
)

// IPCClient connects to the service's named pipe
type IPCClient struct {
	pipeName  string
	sessionID uint32
	token     string

	mu       sync.Mutex
	conn     net.Conn
	reader   *bufio.Reader
	writer   *bufio.Writer
	closed   bool
	stopChan chan struct{}

	// Callbacks for handling messages from service
	onStartSession func(*StartSessionPayload) error
	onStopSession  func(*StopSessionPayload) error
	onICECandidate func(*ICECandidatePayload) error
	onShutdown     func(*ShutdownPayload)
}

// NewIPCClient creates a new IPC client for connecting to the service
func NewIPCClient(sessionID uint32, token string) *IPCClient {
	return &IPCClient{
		pipeName:  fmt.Sprintf("%s%d", PipeNamePrefix, sessionID),
		sessionID: sessionID,
		token:     token,
		stopChan:  make(chan struct{}),
	}
}

// SetHandlers sets the callback handlers for service messages
func (c *IPCClient) SetHandlers(
	onStartSession func(*StartSessionPayload) error,
	onStopSession func(*StopSessionPayload) error,
	onICECandidate func(*ICECandidatePayload) error,
	onShutdown func(*ShutdownPayload),
) {
	c.onStartSession = onStartSession
	c.onStopSession = onStopSession
	c.onICECandidate = onICECandidate
	c.onShutdown = onShutdown
}

// Connect establishes connection to the service pipe
func (c *IPCClient) Connect(ctx context.Context) error {
	log.Printf("[IPC Client] Connecting to %s", c.pipeName)

	// Try to connect with timeout
	timeout := 10 * time.Second
	deadline := time.Now().Add(timeout)

	var conn net.Conn
	var err error

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn, err = winio.DialPipe(c.pipeName, &timeout)
		if err == nil {
			break
		}

		// Pipe might not exist yet, retry
		log.Printf("[IPC Client] Connection attempt failed: %v, retrying...", err)
		time.Sleep(500 * time.Millisecond)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to pipe after %v: %w", timeout, err)
	}

	log.Printf("[IPC Client] Connected to service pipe")

	c.mu.Lock()
	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.writer = bufio.NewWriter(conn)
	c.mu.Unlock()

	return nil
}

// Authenticate sends the auth message and waits for confirmation
func (c *IPCClient) Authenticate() error {
	log.Printf("[IPC Client] Authenticating with service")

	msg, err := NewIPCMessage(MsgTypeAuth, "auth-1", &AuthPayload{
		Token:     c.token,
		SessionID: c.sessionID,
		PID:       os.Getpid(),
	})
	if err != nil {
		return err
	}

	if err := c.SendMessage(msg); err != nil {
		return fmt.Errorf("failed to send auth: %w", err)
	}

	// Wait for auth response with timeout
	c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	response, err := c.readMessage()
	if err != nil {
		return fmt.Errorf("failed to read auth response: %w", err)
	}

	if response.Error != "" {
		return fmt.Errorf("auth failed: %s", response.Error)
	}

	if response.Type != MsgTypeAuthOK {
		return fmt.Errorf("unexpected response type: %s", response.Type)
	}

	var payload AuthOKPayload
	if err := response.ParsePayload(&payload); err != nil {
		return fmt.Errorf("failed to parse auth response: %w", err)
	}

	log.Printf("[IPC Client] Authenticated, capabilities: %v, expires: %v", payload.Capabilities, payload.ExpiresAt)
	return nil
}

// Start begins the message processing loop
func (c *IPCClient) Start(ctx context.Context) error {
	// Start heartbeat goroutine
	go c.heartbeatLoop(ctx)

	// Process incoming messages
	return c.messageLoop(ctx)
}

// heartbeatLoop sends periodic heartbeats to the service
func (c *IPCClient) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		case <-ticker.C:
			if err := c.sendHeartbeat(); err != nil {
				log.Printf("[IPC Client] Failed to send heartbeat: %v", err)
			}
		}
	}
}

// sendHeartbeat sends a single heartbeat message
func (c *IPCClient) sendHeartbeat() error {
	msg, err := NewIPCMessage(MsgTypeHeartbeat, "", &HeartbeatPayload{
		Timestamp: time.Now(),
		SessionID: c.sessionID,
	})
	if err != nil {
		return err
	}
	return c.SendMessage(msg)
}

// messageLoop processes incoming messages from the service
func (c *IPCClient) messageLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.stopChan:
			return nil
		default:
		}

		// Set read deadline to allow checking context
		c.conn.SetReadDeadline(time.Now().Add(1 * time.Second))

		msg, err := c.readMessage()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if err == io.EOF {
				log.Printf("[IPC Client] Service disconnected")
				return fmt.Errorf("service disconnected")
			}
			return err
		}

		if err := c.handleMessage(msg); err != nil {
			log.Printf("[IPC Client] Error handling message type=%s: %v", msg.Type, err)
			c.SendMessage(NewErrorMessage(msg.RequestID, err.Error()))
		}
	}
}

// readMessage reads a single message from the pipe
func (c *IPCClient) readMessage() (*IPCMessage, error) {
	c.mu.Lock()
	reader := c.reader
	c.mu.Unlock()

	if reader == nil {
		return nil, fmt.Errorf("not connected")
	}

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

// handleMessage processes a message from the service
func (c *IPCClient) handleMessage(msg *IPCMessage) error {
	switch msg.Type {
	case MsgTypeHeartbeatAck:
		// Heartbeat acknowledged, nothing to do
		return nil

	case MsgTypeStartSession:
		if c.onStartSession == nil {
			return fmt.Errorf("no start session handler")
		}
		var payload StartSessionPayload
		if err := msg.ParsePayload(&payload); err != nil {
			return err
		}
		return c.onStartSession(&payload)

	case MsgTypeStopSession:
		if c.onStopSession == nil {
			return fmt.Errorf("no stop session handler")
		}
		var payload StopSessionPayload
		if err := msg.ParsePayload(&payload); err != nil {
			return err
		}
		return c.onStopSession(&payload)

	case MsgTypeICECandidate:
		if c.onICECandidate == nil {
			return fmt.Errorf("no ICE candidate handler")
		}
		var payload ICECandidatePayload
		if err := msg.ParsePayload(&payload); err != nil {
			return err
		}
		return c.onICECandidate(&payload)

	case MsgTypeShutdown:
		var payload ShutdownPayload
		if err := msg.ParsePayload(&payload); err != nil {
			return err
		}
		if c.onShutdown != nil {
			c.onShutdown(&payload)
		}
		return nil

	case MsgTypeAuthOK:
		// Already handled during authentication
		return nil

	default:
		log.Printf("[IPC Client] Unknown message type: %s", msg.Type)
		return nil
	}
}

// SendMessage sends a message to the service
func (c *IPCClient) SendMessage(msg *IPCMessage) error {
	c.mu.Lock()
	writer := c.writer
	conn := c.conn
	c.mu.Unlock()

	if writer == nil || conn == nil {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.writer.Write(data); err != nil {
		return err
	}
	if _, err := c.writer.Write([]byte("\n")); err != nil {
		return err
	}
	return c.writer.Flush()
}

// SendSessionAnswer sends the WebRTC answer to the service
func (c *IPCClient) SendSessionAnswer(connectionID, sdpType, sdp string) error {
	msg, err := NewIPCMessage(MsgTypeSessionAnswer, "", &SessionAnswerPayload{
		SDPType:      sdpType,
		SDP:          sdp,
		ConnectionID: connectionID,
	})
	if err != nil {
		return err
	}
	return c.SendMessage(msg)
}

// SendICECandidate sends an ICE candidate to the service
func (c *IPCClient) SendICECandidate(connectionID, candidate, sdpMid string, sdpMLineIndex *int) error {
	msg, err := NewIPCMessage(MsgTypeICECandidate, "", &ICECandidatePayload{
		Candidate:     candidate,
		SDPMid:        sdpMid,
		SDPMLineIndex: sdpMLineIndex,
		ConnectionID:  connectionID,
	})
	if err != nil {
		return err
	}
	return c.SendMessage(msg)
}

// SendStatus sends a status update to the service
func (c *IPCClient) SendStatus(state HelperState, message, connectionID string) error {
	msg, err := NewIPCMessage(MsgTypeStatus, "", &StatusPayload{
		State:        state,
		Message:      message,
		ConnectionID: connectionID,
	})
	if err != nil {
		return err
	}
	return c.SendMessage(msg)
}

// Close shuts down the client connection
func (c *IPCClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	close(c.stopChan)

	conn := c.conn
	c.mu.Unlock()

	if conn != nil {
		return conn.Close()
	}
	return nil
}
