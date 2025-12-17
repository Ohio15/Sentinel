package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
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
	MsgTypePing          = "ping"
	MsgTypePong          = "pong"
	MsgTypeMetrics       = "metrics"
	MsgTypeResponse      = "response"
	MsgTypeExecuteCmd    = "execute_command"
	MsgTypeExecuteScript = "execute_script"
	MsgTypeStartTerminal = "start_terminal"
	MsgTypeTerminalInput = "terminal_input"
	MsgTypeTerminalOutput = "terminal_output"
	MsgTypeTerminalResize = "terminal_resize"
	MsgTypeCloseTerminal = "close_terminal"
	MsgTypeListDrives    = "list_drives"
	MsgTypeListFiles     = "list_files"
	MsgTypeScanDirectory = "scan_directory"
	MsgTypeDownloadFile  = "download_file"
	MsgTypeUploadFile    = "upload_file"
	MsgTypeFileData      = "file_data"
	MsgTypeScanProgress  = "scan_progress"
	MsgTypeStartRemote   = "start_remote"
	MsgTypeStopRemote    = "stop_remote"
	MsgTypeRemoteInput   = "remote_input"
	MsgTypeRemoteFrame   = "remote_frame"
	MsgTypeEvent         = "event"
	MsgTypeError              = "error"
	MsgTypeCollectDiagnostics = "collect_diagnostics"
	MsgTypeUninstallAgent     = "uninstall_agent"
	// WebRTC signaling messages
	MsgTypeWebRTCStart     = "webrtc_start"
	MsgTypeWebRTCSignal    = "webrtc_signal"
	MsgTypeWebRTCStop      = "webrtc_stop"
	// Admin management messages
	MsgTypeAdminDiscover   = "admin_discover"
	MsgTypeAdminDemote     = "admin_demote"
	MsgTypeAdminEvent      = "admin_event"
	// Configuration messages
	MsgTypeSetMetricsInterval = "set_metrics_interval"
	// Certificate management messages
	MsgTypeUpdateCertificate = "update_certificate"
	MsgTypeCertUpdateAck     = "cert_update_ack"
	// System update status
	MsgTypeUpdateStatus      = "update_status"
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
	config            *config.Config
	conn              *websocket.Conn
	handlers          map[string]MessageHandler
	authenticated     bool
	connected         bool
	reconnectDelay    time.Duration
	maxReconnect      time.Duration
	mu                sync.RWMutex
	done              chan struct{}
	sendQueue         chan []byte
	onConnect         func()
	onDisconnect      func()
	onNeedsEnrollment func()
	version           string
	lastPong        time.Time
	pingInterval    time.Duration
	pongTimeout     time.Duration
	healthPollRate  time.Duration
	httpClient      *http.Client
}

// New creates a new WebSocket client
func New(cfg *config.Config, version string) *Client {
	return &Client{
		config:         cfg,
		handlers:       make(map[string]MessageHandler),
		reconnectDelay: 500 * time.Millisecond,
		maxReconnect:   2 * time.Second,
		done:           make(chan struct{}),
		sendQueue:      make(chan []byte, 100),
		version:        version,
		pingInterval:   5 * time.Second,
		pongTimeout:    5 * time.Second,
		healthPollRate: 250 * time.Millisecond,
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
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

// OnNeedsEnrollment sets the callback for when server indicates device not found
func (c *Client) OnNeedsEnrollment(fn func()) {
	c.onNeedsEnrollment = fn
}

// RegisterHandler registers a message handler for a specific message type
func (c *Client) RegisterHandler(msgType string, handler MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[msgType] = handler
}

// getHealthURL returns the HTTP health check URL from the server URL
func (c *Client) getHealthURL() string {
	serverURL := c.config.ServerURL
	if serverURL == "" {
		return ""
	}

	// Ensure http:// or https:// prefix
	if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		if strings.HasPrefix(serverURL, "ws://") {
			serverURL = "http://" + serverURL[5:]
		} else if strings.HasPrefix(serverURL, "wss://") {
			serverURL = "https://" + serverURL[6:]
		} else {
			serverURL = "http://" + serverURL
		}
	}

	// Remove any path suffix and add /health
	if idx := strings.Index(serverURL[8:], "/"); idx > 0 {
		serverURL = serverURL[:8+idx]
	}

	return serverURL + "/health"
}

// checkServerHealth performs an HTTP health check to see if server is available
func (c *Client) checkServerHealth() bool {
	healthURL := c.getHealthURL()
	if healthURL == "" {
		return false
	}

	resp, err := c.httpClient.Get(healthURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// waitForServer polls the server until it becomes available
func (c *Client) waitForServer(ctx context.Context) bool {
	log.Println("Waiting for server to become available...")

	// Try immediately first
	if c.checkServerHealth() {
		log.Println("Server is available!")
		return true
	}

	ticker := time.NewTicker(c.healthPollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if c.checkServerHealth() {
				log.Println("Server is available!")
				return true
			}
		}
	}
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
		HandshakeTimeout: 5 * time.Second,
	}

	headers := http.Header{}
	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.lastPong = time.Now()
	c.mu.Unlock()

	// Set up WebSocket-level pong handler to detect dead connections
	conn.SetPongHandler(func(appData string) error {
		c.mu.Lock()
		c.lastPong = time.Now()
		c.mu.Unlock()
		log.Println("Received WebSocket pong")
		return nil
	})

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
	go c.pingLoop(ctx)

	if c.onConnect != nil {
		c.onConnect()
	}

	return nil
}

// pingLoop sends WebSocket-level ping frames to detect dead connections
func (c *Client) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(c.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.RLock()
			conn := c.conn
			connected := c.connected
			lastPong := c.lastPong
			c.mu.RUnlock()

			if !connected || conn == nil {
				return
			}

			// Check if we've received a pong recently
			if time.Since(lastPong) > c.pingInterval+c.pongTimeout {
				log.Printf("No pong received for %v, connection appears dead", time.Since(lastPong))
				c.forceClose()
				return
			}

			// Send WebSocket-level ping
			if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second)); err != nil {
				log.Printf("Failed to send ping: %v", err)
				c.forceClose()
				return
			}
			log.Println("Sent WebSocket ping")
		}
	}
}

// forceClose forcefully closes the connection to trigger reconnection
func (c *Client) forceClose() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connected = false
	c.authenticated = false

	// Signal done to trigger reconnection
	select {
	case <-c.done:
		// Already closed
	default:
		close(c.done)
	}
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
		// Signal done channel to trigger reconnection in RunWithReconnect
		select {
		case <-c.done:
			// Already closed
		default:
			close(c.done)
		}
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

		// Set read deadline to detect dead connections
		conn.SetReadDeadline(time.Now().Add(65 * time.Second))

		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			} else {
				log.Printf("WebSocket read error (timeout or closed): %v", err)
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
				Success         bool   `json:"success"`
				Error           string `json:"error"`
				NeedsEnrollment bool   `json:"needsEnrollment"`
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

				// Check if server says we need to re-enroll
				if authResp.NeedsEnrollment {
					log.Println("Server indicates device not found - triggering re-enrollment")
					c.mu.RLock()
					cb := c.onNeedsEnrollment
					c.mu.RUnlock()
					if cb != nil {
						go cb()
					}
				}
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
// Uses HTTP health polling to detect server availability for immediate connection
func (c *Client) RunWithReconnect(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Reset done channel before connecting
		c.mu.Lock()
		select {
		case <-c.done:
			c.done = make(chan struct{})
		default:
		}
		c.mu.Unlock()

		// Phase 1: Wait for server to be available via HTTP health check
		// This ensures we connect IMMEDIATELY when server starts
		if !c.waitForServer(ctx) {
			return // Context cancelled
		}

		// Phase 2: Connect WebSocket immediately after server is detected
		log.Println("Attempting WebSocket connection...")
		err := c.Connect(ctx)
		if err != nil {
			log.Printf("WebSocket connection failed: %v, retrying...", err)
			// Brief pause before retry - server might still be starting up
			select {
			case <-ctx.Done():
				return
			case <-time.After(c.reconnectDelay):
			}
			continue
		}

		log.Println("Connection established successfully")

		// Phase 3: Wait for disconnection
		<-c.done

		log.Println("Disconnected, checking server availability...")
	}
}

// SendWebRTCSignal sends a WebRTC signaling message (SDP offer/answer or ICE candidate)
func (c *Client) SendWebRTCSignal(sessionID, signalType, sdp, candidate string) error {
	msg := map[string]interface{}{
		"type":      MsgTypeWebRTCSignal,
		"sessionId": sessionID,
		"signal": map[string]interface{}{
			"type":      signalType,
			"sessionId": sessionID,
			"sdp":       sdp,
			"candidate": candidate,
		},
	}
	return c.SendJSON(msg)
}

// SendAdminDiscovery sends the admin discovery results to the server
func (c *Client) SendAdminDiscovery(requestID string, admins interface{}, safetyCheck interface{}) error {
	msg := map[string]interface{}{
		"type":      MsgTypeAdminDiscover,
		"requestId": requestID,
		"data": map[string]interface{}{
			"admins":      admins,
			"safetyCheck": safetyCheck,
		},
	}
	return c.SendJSON(msg)
}

// SendAdminDemotionResult sends the result of an admin demotion operation
func (c *Client) SendAdminDemotionResult(requestID string, result interface{}) error {
	msg := map[string]interface{}{
		"type":      MsgTypeResponse,
		"requestId": requestID,
		"success":   true,
		"data":      result,
	}
	return c.SendJSON(msg)
}

// SendAdminEvent sends an admin management event (for telemetry)
func (c *Client) SendAdminEvent(event interface{}) error {
	msg := map[string]interface{}{
		"type":  MsgTypeAdminEvent,
		"event": event,
	}
	return c.SendJSON(msg)
}

// SendCertUpdateAck sends a certificate update acknowledgment to the server
func (c *Client) SendCertUpdateAck(certHash string, success bool, errMsg string) error {
	msg := map[string]interface{}{
		"type": MsgTypeCertUpdateAck,
		"data": map[string]interface{}{
			"certHash": certHash,
			"success":  success,
			"error":    errMsg,
		},
	}
	return c.SendJSON(msg)
}

// SendUpdateStatus sends system update status to the server
func (c *Client) SendUpdateStatus(status interface{}) error {
	msg := map[string]interface{}{
		"type": MsgTypeUpdateStatus,
		"data": status,
	}
	return c.SendJSON(msg)
}
