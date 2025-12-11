package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sentinel/agent/internal/config"
)

// Message types
const (
	MsgTypeAuth          = "auth"
	MsgTypeAuthResponse  = "auth_response"
	MsgTypeHandshake     = "handshake"
	MsgTypeHeartbeat     = "heartbeat"
	MsgTypeHeartbeatAck  = "heartbeat_ack"
	MsgTypeMetrics       = "metrics"
	MsgTypeResponse      = "response"
	MsgTypeExecuteCmd    = "execute_command"
	MsgTypeExecuteScript = "execute_script"
	MsgTypeStartTerminal = "start_terminal"
	MsgTypeTerminalInput = "terminal_input"
	MsgTypeTerminalOutput = "terminal_output"
	MsgTypeTerminalResize = "terminal_resize"
	MsgTypeCloseTerminal = "close_terminal"
	MsgTypeListFiles     = "list_files"
	MsgTypeDownloadFile  = "download_file"
	MsgTypeUploadFile    = "upload_file"
	MsgTypeFileData      = "file_data"
	MsgTypeStartRemote   = "start_remote"
	MsgTypeStopRemote    = "stop_remote"
	MsgTypeRemoteInput   = "remote_input"
	MsgTypeRemoteFrame   = "remote_frame"
	MsgTypeEvent         = "event"
	MsgTypeError              = "error"
	MsgTypeCollectDiagnostics = "collect_diagnostics"
	MsgTypeUninstallAgent     = "uninstall_agent"
)

// Message represents a WebSocket message
type Message struct {
	Type      string      `json:"type"`
	RequestID string      `json:"requestId,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Payload   interface{} `json:"payload,omitempty"`
	Success   bool        `json:"success,omitempty"`
	Error     string      `json:"error,omitempty"`
	Timestamp string      `json:"timestamp,omitempty"`
}

// MessageHandler is a function that handles incoming messages
type MessageHandler func(msg *Message) error

// Client manages the WebSocket connection to the server
type Client struct {
	config          *config.Config
	conn            *websocket.Conn
	handlers        map[string]MessageHandler
	authenticated   bool
	connected       bool
	reconnectDelay  time.Duration
	maxReconnect    time.Duration
	mu              sync.RWMutex
	done            chan struct{}
	sendQueue       chan []byte
	onConnect       func()
	onDisconnect    func()
	version         string
}

// New creates a new WebSocket client
func New(cfg *config.Config, version string) *Client {
	return &Client{
		config:         cfg,
		handlers:       make(map[string]MessageHandler),
		reconnectDelay: 5 * time.Second,
		maxReconnect:   5 * time.Minute,
		done:           make(chan struct{}),
		sendQueue:      make(chan []byte, 100),
		version:        version,
	}
}

// OnConnect sets the callback for successful connection
func (c *Client) OnConnect(fn func()) {
	c.onConnect = fn
}

// OnDisconnect sets the callback for disconnection
func (c *Client) OnDisconnect(fn func()) {
	c.onDisconnect = fn
}

// RegisterHandler registers a message handler for a specific message type
func (c *Client) RegisterHandler(msgType string, handler MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[msgType] = handler
}

// Connect establishes a WebSocket connection to the server
func (c *Client) Connect(ctx context.Context) error {
	wsURL := c.config.ServerURL
	if wsURL == "" {
		return fmt.Errorf("server URL not configured")
	}

	// Convert http:// to ws:// if needed
	if len(wsURL) > 7 && wsURL[:7] == "http://" {
		wsURL = "ws://" + wsURL[7:]
	} else if len(wsURL) > 8 && wsURL[:8] == "https://" {
		wsURL = "wss://" + wsURL[8:]
	}

	// Ensure /ws/agent path for agent connections
	if len(wsURL) < 9 || wsURL[len(wsURL)-9:] != "/ws/agent" {
		// Remove trailing /ws if present (wrong path)
		if len(wsURL) >= 3 && wsURL[len(wsURL)-3:] == "/ws" {
			wsURL = wsURL[:len(wsURL)-3]
		}
		wsURL = wsURL + "/ws/agent"
	}

	log.Printf("Connecting to %s", wsURL)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	headers := http.Header{}
	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	log.Println("WebSocket connected")

	// Send auth message immediately (server expects auth first)
	authMsg := map[string]interface{}{
		"type": MsgTypeAuth,
		"payload": map[string]interface{}{
			"agentId": c.config.AgentID,
			"token":   c.config.EnrollmentToken,
		},
	}
	authData, err := json.Marshal(authMsg)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to marshal auth message: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authData); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send auth message: %w", err)
	}
	log.Println("Auth message sent, waiting for response...")

	// Start message handlers
	go c.readLoop(ctx)
	go c.writeLoop(ctx)

	if c.onConnect != nil {
		c.onConnect()
	}

	return nil
}

// Authenticate sends authentication message to the server
func (c *Client) Authenticate() error {
	msg := map[string]interface{}{
		"type":    MsgTypeAuth,
		"agentId": c.config.AgentID,
		"token":   c.config.EnrollmentToken,
	}

	return c.SendJSON(msg)
}

// SendJSON sends a JSON message through the WebSocket
func (c *Client) SendJSON(v interface{}) error {
	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()

	if !connected {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	select {
	case c.sendQueue <- data:
		return nil
	default:
		return fmt.Errorf("send queue full")
	}
}

// SendResponse sends a response to a request
func (c *Client) SendResponse(requestID string, success bool, data interface{}, errMsg string) error {
	msg := map[string]interface{}{
		"type":      MsgTypeResponse,
		"requestId": requestID,
		"success":   success,
		"data":      data,
		"error":     errMsg,
	}
	return c.SendJSON(msg)
}

// SendMetrics sends system metrics to the server
func (c *Client) SendMetrics(metrics interface{}) error {
	msg := map[string]interface{}{
		"type": MsgTypeMetrics,
		"data": metrics,
	}
	return c.SendJSON(msg)
}

// SendHeartbeat sends a heartbeat message
func (c *Client) SendHeartbeat() error {
	msg := map[string]interface{}{
		"type":         MsgTypeHeartbeat,
		"timestamp":    time.Now().Format(time.RFC3339),
		"agentVersion": c.version,
	}
	return c.SendJSON(msg)
}

// SendTerminalOutput sends terminal output to the server
func (c *Client) SendTerminalOutput(sessionID string, data string) error {
	msg := map[string]interface{}{
		"type":      MsgTypeTerminalOutput,
		"sessionId": sessionID,
		"data":      data,
	}
	return c.SendJSON(msg)
}

// SendEvent sends an event notification to the server
func (c *Client) SendEvent(severity, title, message string) error {
	msg := map[string]interface{}{
		"type": MsgTypeEvent,
		"event": map[string]interface{}{
			"severity": severity,
			"title":    title,
			"message":  message,
		},
	}
	return c.SendJSON(msg)
}



// SendRemoteFrame sends a remote desktop frame to the server
func (c *Client) SendRemoteFrame(sessionID string, data string, width, height int) error {
	msg := map[string]interface{}{
		"type":      MsgTypeRemoteFrame,
		"sessionId": sessionID,
		"data":      data,
		"width":     width,
		"height":    height,
	}
	return c.SendJSON(msg)
}

// readLoop handles incoming messages
func (c *Client) readLoop(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		c.connected = false
		c.authenticated = false
		c.mu.Unlock()

		if c.onDisconnect != nil {
			c.onDisconnect()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		default:
		}

		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		if conn == nil {
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			return
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("Failed to parse message: %v", err)
			continue
		}

		// Handle authentication response
		if msg.Type == MsgTypeAuthResponse {
			// Server sends success/error inside Payload field
			var authResp struct {
				Success bool   `json:"success"`
				Error   string `json:"error"`
			}
			// Try to parse Payload if present
			if msg.Payload != nil {
				if payloadBytes, err := json.Marshal(msg.Payload); err == nil {
					json.Unmarshal(payloadBytes, &authResp)
				}
			}
			// Fall back to top-level Success/Error for backwards compatibility
			if !authResp.Success && msg.Success {
				authResp.Success = true
			}
			if authResp.Error == "" && msg.Error != "" {
				authResp.Error = msg.Error
			}

			if authResp.Success {
				c.mu.Lock()
				c.authenticated = true
				c.mu.Unlock()
				log.Println("Authentication successful")
			} else {
				log.Printf("Authentication failed: %s", authResp.Error)
			}
			continue
		}

		// Handle handshake
		if msg.Type == MsgTypeHandshake {
			log.Println("Received handshake, authenticating...")
			if err := c.Authenticate(); err != nil {
				log.Printf("Failed to authenticate: %v", err)
			}
			continue
		}

		// Dispatch to registered handler
		c.mu.RLock()
		handler, ok := c.handlers[msg.Type]
		c.mu.RUnlock()

		if ok {
			go func(m Message) {
				if err := handler(&m); err != nil {
					log.Printf("Handler error for %s: %v", m.Type, err)
				}
			}(msg)
		} else {
			log.Printf("No handler for message type: %s", msg.Type)
		}
	}
}

// writeLoop handles outgoing messages
func (c *Client) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case data := <-c.sendQueue:
			c.mu.RLock()
			conn := c.conn
			c.mu.RUnlock()

			if conn == nil {
				continue
			}

			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("Write error: %v", err)
				return
			}
		}
	}
}

// Close closes the WebSocket connection
func (c *Client) Close() error {
	close(c.done)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		err := c.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		if err != nil {
			log.Printf("Error sending close message: %v", err)
		}
		c.conn.Close()
		c.conn = nil
	}

	c.connected = false
	c.authenticated = false
	return nil
}

// IsConnected returns the connection status
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// IsAuthenticated returns the authentication status
func (c *Client) IsAuthenticated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.authenticated
}

// RunWithReconnect maintains a persistent connection with automatic reconnection
func (c *Client) RunWithReconnect(ctx context.Context) {
	delay := c.reconnectDelay

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := c.Connect(ctx)
		if err != nil {
			log.Printf("Connection failed: %v, retrying in %v", err, delay)

			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}

			// Exponential backoff
			delay = delay * 2
			if delay > c.maxReconnect {
				delay = c.maxReconnect
			}
			continue
		}

		// Reset delay on successful connection
		delay = c.reconnectDelay

		// Wait for disconnection
		<-c.done
		c.done = make(chan struct{})

		log.Println("Disconnected, reconnecting...")
	}
}
