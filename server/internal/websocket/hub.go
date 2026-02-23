package websocket

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/sentinel/server/internal/messaging"
	"github.com/sentinel/server/internal/sessions"
	"github.com/sentinel/server/pkg/cache"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 120 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512 * 1024 // 512KB

	// Backpressure configuration
	sendBufferSize     = 1024
	sendTimeoutMs      = 100  // Block for max 100ms before considering backpressure
	backpressureWarnMs = 50   // Warn if send takes longer than 50ms
)

// Message types
const (
	MsgTypeAuth           = "auth"
	MsgTypeAuthResponse   = "auth_response"
	MsgTypeHeartbeat      = "heartbeat"
	MsgTypeHeartbeatAck   = "heartbeat_ack"
	MsgTypePing           = "ping"
	MsgTypePong           = "pong"
	MsgTypeMetrics        = "metrics"
	MsgTypeCommand        = "execute_command"
	MsgTypeScript         = "execute_script"
	MsgTypeResponse       = "response"
	MsgTypeTerminalStart  = "start_terminal"
	MsgTypeTerminalInput  = "terminal_input"
	MsgTypeTerminalOutput = "terminal_output"
	MsgTypeTerminalResize = "terminal_resize"
	MsgTypeTerminalClose  = "close_terminal"
	MsgTypeListFiles      = "list_files"
	MsgTypeFileContent    = "file_content"
	MsgTypeDownloadFile      = "download_file"
	MsgTypeUploadFile        = "upload_file"
	MsgTypeListDrives        = "list_drives"
	MsgTypeScanDirectory     = "scan_directory"
	MsgTypeScanProgress      = "scan_progress"
	MsgTypeSetMetricsInterval = "set_metrics_interval"
	MsgTypeUninstallAgent     = "uninstall_agent"

	// Power management message types
	MsgTypePowerAction = "power_action"

	// Metrics recording message types
	MsgTypeStartRecording = "start_recording"
	MsgTypeStopRecording  = "stop_recording"

	// Agent update message types
	MsgTypeCheckUpdate   = "check_update"
	MsgTypeUpdateAvailable = "update_available"
	MsgTypeUpdateProgress  = "update_progress"

	// Windows Update installation message types
	MsgTypeInstallUpdates  = "install_updates"
	MsgTypeInstallProgress = "install_progress"

	// Certificate message types
	MsgTypeUpdateCertificate = "update_certificate"
	MsgTypeCertUpdateAck     = "cert_update_ack"

	// WebRTC signaling message types
	MsgTypeWebRTCStart  = "webrtc_start"
	MsgTypeWebRTCSignal = "webrtc_signal"
	MsgTypeWebRTCStop   = "webrtc_stop"

	// USB/Peripheral device message types
	MsgTypeUSBDeviceEvent    = "usb_device_event"    // Agent sends USB device events
	MsgTypeUSBDeviceList     = "usb_device_list"     // Agent sends full USB device list
	MsgTypeUSBDeviceRequest  = "usb_device_request"  // Server requests USB device scan
	MsgTypeUSBPolicyUpdate   = "usb_policy_update"   // Server sends policy updates to agent

	// Agent alert message types
	MsgTypeAgentAlert = "agent_alert" // Agent sends alert (update failure, tamper, etc.)
)

type Message struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId,omitempty"`
	AgentID   string          `json:"agentId,omitempty"`
	Timestamp time.Time       `json:"timestamp,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type Client struct {
	hub          *Hub
	conn         *websocket.Conn
	send         chan []byte
	agentID      string
	deviceID     uuid.UUID
	isAgent      bool
	userID       uuid.UUID
	connectionID string // Unique identifier for this connection

	// Metrics
	messagesSent     atomic.Int64
	messagesDropped  atomic.Int64
	lastMessageAt    time.Time
	connectedAt      time.Time

	// Backpressure tracking
	backpressureCount atomic.Int64
}

type Hub struct {
	// Registered agent clients (agentID -> client)
	agents map[string]*Client

	// Registered dashboard clients (userID -> clients)
	dashboards map[uuid.UUID][]*Client

	// Connection ID index for targeted routing
	connections map[string]*Client // connectionID -> client

	// Channels
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte

	// Mutex for thread-safe access
	mu sync.RWMutex

	// Redis for pub/sub
	cache *cache.Cache

	// Messaging components (optional, for enhanced routing)
	registry        *messaging.DashboardRegistry
	router          *messaging.MessageRouter
	pendingRequests *messaging.PendingRequestManager
	sessionManager  *sessions.SessionManager

	// Metrics
	totalAgentConnections     atomic.Int64
	totalDashboardConnections atomic.Int64
	totalMessagesRouted       atomic.Int64
	totalMessagesDropped      atomic.Int64
	totalBackpressureEvents   atomic.Int64

	// Callbacks for session management
	onAgentDisconnect func(agentID string, deviceID uuid.UUID)
}

func NewHub(cache *cache.Cache) *Hub {
	h := &Hub{
		agents:      make(map[string]*Client),
		dashboards:  make(map[uuid.UUID][]*Client),
		connections: make(map[string]*Client),
		register:    make(chan *Client),
		unregister:  make(chan *Client),
		broadcast:   make(chan []byte, 256), // Buffered to prevent blocking
		cache:       cache,
	}

	// Initialize messaging components
	h.registry = messaging.NewDashboardRegistry()
	h.pendingRequests = messaging.NewPendingRequestManager(30 * time.Second)
	h.router = messaging.NewMessageRouter(h.registry, h.pendingRequests)

	return h
}

// SetSessionManager sets the session manager for session persistence
func (h *Hub) SetSessionManager(sm *sessions.SessionManager) {
	h.mu.Lock()
	h.sessionManager = sm
	h.mu.Unlock()
}

// SetOnAgentDisconnect sets a callback for when an agent disconnects
func (h *Hub) SetOnAgentDisconnect(fn func(agentID string, deviceID uuid.UUID)) {
	h.mu.Lock()
	h.onAgentDisconnect = fn
	h.mu.Unlock()
}

// GetRouter returns the message router for targeted routing
func (h *Hub) GetRouter() *messaging.MessageRouter {
	return h.router
}

// GetRegistry returns the dashboard registry
func (h *Hub) GetRegistry() *messaging.DashboardRegistry {
	return h.registry
}

// GetPendingRequests returns the pending request manager
func (h *Hub) GetPendingRequests() *messaging.PendingRequestManager {
	return h.pendingRequests
}

func (h *Hub) Run() {
	for {
		select {
		case client, ok := <-h.register:
			if !ok {
				// Channel closed, exit Run loop
				return
			}
			if client == nil {
				continue
			}
			h.mu.Lock()
			// Add to connection index
			h.connections[client.connectionID] = client

			if client.isAgent {
				h.agents[client.agentID] = client
				h.totalAgentConnections.Add(1)
				log.Printf("Agent connected: %s (conn: %s)", client.agentID, client.connectionID)
			} else {
				// Close excess dashboard connections for this user (keep max 2)
				const maxDashboardConns = 2
				existing := h.dashboards[client.userID]
				if len(existing) >= maxDashboardConns {
					excessCount := len(existing) - maxDashboardConns + 1
					for i := 0; i < excessCount && i < len(existing); i++ {
						old := existing[i]
						log.Printf("[Hub] Closing excess dashboard conn for user %s: conn=%s (had %d, max %d)",
							client.userID, old.connectionID, len(existing), maxDashboardConns)
						delete(h.connections, old.connectionID)
						if h.registry != nil {
							h.registry.Unregister(old.connectionID)
						}
						close(old.send)
						go old.conn.Close()
					}
					existing = existing[excessCount:]
				}

				h.dashboards[client.userID] = append(existing, client)
				h.totalDashboardConnections.Add(1)

				// Register with dashboard registry for targeted routing
				if h.registry != nil {
					h.registry.Register(client.userID, 0, &clientConnection{client: client})
				}

				log.Printf("Dashboard connected: %s (conn: %s)", client.userID, client.connectionID)
			}
			h.mu.Unlock()

		case client, ok := <-h.unregister:
			if !ok {
				// Channel closed, exit Run loop
				return
			}
			if client == nil {
				continue
			}
			h.mu.Lock()
			// Remove from connection index
			delete(h.connections, client.connectionID)

			if client.isAgent {
				// Only remove from agents map if this is the current registered connection
				// This prevents removing a newer connection when an old one closes
				if existing, ok := h.agents[client.agentID]; ok && existing.connectionID == client.connectionID {
					delete(h.agents, client.agentID)
					close(client.send)
					log.Printf("Agent disconnected: %s (sent: %d, dropped: %d)",
						client.agentID, client.messagesSent.Load(), client.messagesDropped.Load())

					// Cancel any pending requests for this agent
					if h.pendingRequests != nil {
						h.pendingRequests.CancelByTarget(client.agentID)
					}

					// Notify session manager
					if h.onAgentDisconnect != nil {
						go h.onAgentDisconnect(client.agentID, client.deviceID)
					}
				}
			} else {
				if clients, ok := h.dashboards[client.userID]; ok {
					for i, c := range clients {
						if c == client {
							h.dashboards[client.userID] = append(clients[:i], clients[i+1:]...)
							break
						}
					}
					if len(h.dashboards[client.userID]) == 0 {
						delete(h.dashboards, client.userID)
					}
					close(client.send)

					// Unregister from dashboard registry
					if h.registry != nil {
						h.registry.Unregister(client.connectionID)
					}

					// Cancel any pending requests from this dashboard
					if h.pendingRequests != nil {
						h.pendingRequests.CancelBySource(client.userID.String())
					}

					log.Printf("Dashboard disconnected: %s (sent: %d, dropped: %d)",
						client.userID, client.messagesSent.Load(), client.messagesDropped.Load())
				}
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.broadcastWithBackpressure(message)
		}
	}
}

// broadcastWithBackpressure sends to all dashboards with backpressure handling
func (h *Hub) broadcastWithBackpressure(message []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, clients := range h.dashboards {
		for _, client := range clients {
			h.sendWithBackpressure(client, message)
		}
	}
}

// sendWithBackpressure sends a message with timeout and backpressure tracking
func (h *Hub) sendWithBackpressure(client *Client, message []byte) bool {
	start := time.Now()

	select {
	case client.send <- message:
		client.messagesSent.Add(1)
		client.lastMessageAt = time.Now()
		h.totalMessagesRouted.Add(1)

		// Warn if send took too long (indicates backpressure)
		elapsed := time.Since(start)
		if elapsed > backpressureWarnMs*time.Millisecond {
			client.backpressureCount.Add(1)
			h.totalBackpressureEvents.Add(1)
			if client.backpressureCount.Load()%100 == 1 {
				log.Printf("[Hub] Backpressure warning for %s: send took %v (count: %d)",
					client.connectionID, elapsed, client.backpressureCount.Load())
			}
		}
		return true

	case <-time.After(sendTimeoutMs * time.Millisecond):
		// Send timed out - track as dropped
		client.messagesDropped.Add(1)
		h.totalMessagesDropped.Add(1)

		// Log periodically, not every drop
		if client.messagesDropped.Load()%100 == 1 {
			log.Printf("[Hub] Message dropped for %s (total drops: %d, backpressure: %d)",
				client.connectionID, client.messagesDropped.Load(), client.backpressureCount.Load())
		}
		return false
	}
}

func (h *Hub) SendToAgent(agentID string, message []byte) error {
	h.mu.RLock()
	client, ok := h.agents[agentID]
	h.mu.RUnlock()

	if !ok {
		log.Printf("[Hub] SendToAgent: agent %s not connected", agentID)
		return ErrAgentNotConnected
	}

	// Log the message being sent (truncated for readability)
	msgPreview := string(message)
	if len(msgPreview) > 200 {
		msgPreview = msgPreview[:200] + "..."
	}
	log.Printf("[Hub] SendToAgent %s: %s", agentID, msgPreview)

	select {
	case client.send <- message:
		return nil
	default:
		log.Printf("[Hub] SendToAgent: send channel full for agent %s", agentID)
		return ErrSendFailed
	}
}

func (h *Hub) BroadcastToDashboards(message []byte) {
	h.broadcast <- message
}

func (h *Hub) IsAgentOnline(agentID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.agents[agentID]
	return ok
}

func (h *Hub) GetOnlineAgents() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	agents := make([]string, 0, len(h.agents))
	for agentID := range h.agents {
		agents = append(agents, agentID)
	}
	return agents
}

// RegisterAgent registers a new agent client
func (h *Hub) RegisterAgent(conn *websocket.Conn, agentID string, deviceID uuid.UUID) *Client {
	client := &Client{
		hub:          h,
		conn:         conn,
		send:         make(chan []byte, sendBufferSize),
		agentID:      agentID,
		deviceID:     deviceID,
		isAgent:      true,
		connectionID: uuid.New().String(),
		connectedAt:  time.Now(),
	}
	h.register <- client
	return client
}

// RegisterDashboard registers a new dashboard client
func (h *Hub) RegisterDashboard(conn *websocket.Conn, userID uuid.UUID) *Client {
	client := &Client{
		hub:          h,
		conn:         conn,
		send:         make(chan []byte, sendBufferSize),
		userID:       userID,
		isAgent:      false,
		connectionID: uuid.New().String(),
		connectedAt:  time.Now(),
	}
	h.register <- client
	return client
}

// GetConnectionByID returns a client by connection ID
func (h *Hub) GetConnectionByID(connID string) (*Client, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	client, ok := h.connections[connID]
	return client, ok
}

// SendToConnection sends a message to a specific connection
func (h *Hub) SendToConnection(connID string, message []byte) error {
	h.mu.RLock()
	client, ok := h.connections[connID]
	h.mu.RUnlock()

	if !ok {
		return ErrConnectionNotFound
	}

	if h.sendWithBackpressure(client, message) {
		return nil
	}
	return ErrSendFailed
}

// SendToUser sends a message to all connections of a specific user
func (h *Hub) SendToUser(userID uuid.UUID, message []byte) error {
	h.mu.RLock()
	clients, ok := h.dashboards[userID]
	h.mu.RUnlock()

	if !ok || len(clients) == 0 {
		return ErrUserNotConnected
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	sent := 0
	for _, client := range clients {
		if h.sendWithBackpressure(client, message) {
			sent++
		}
	}

	if sent == 0 {
		return ErrSendFailed
	}
	return nil
}

func (c *Client) ReadPump(ctx context.Context, handler func([]byte)) {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	// Explicitly handle pings from clients - send pong response
	c.conn.SetPingHandler(func(message string) error {
		if c.isAgent {
			log.Printf("[Ping] Received ping from agent, sending pong")
		}
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return c.conn.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(writeWait))
	})

	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, message, err := c.conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket error: %v", err)
				}
				return
			}
			// Reset read deadline on ANY received data — not just pong frames.
			// This keeps connections alive when app-level messages flow (heartbeats, metrics, etc.)
			c.conn.SetReadDeadline(time.Now().Add(pongWait))
			if !c.isAgent {
				log.Printf("[ReadPump] Dashboard message received (%d bytes): %.200s", len(message), string(message))
			}
			handler(message)
		}
	}
}

func (c *Client) WritePump(ctx context.Context) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Send sends a message to the client's send channel
func (c *Client) Send(message []byte) error {
	select {
	case c.send <- message:
		c.messagesSent.Add(1)
		c.lastMessageAt = time.Now()
		return nil
	default:
		c.messagesDropped.Add(1)
		return ErrSendFailed
	}
}

// GetConnectionID returns the unique connection identifier
func (c *Client) GetConnectionID() string {
	return c.connectionID
}

// IsConnected returns true if the client is still connected
func (c *Client) IsConnected() bool {
	return c.conn != nil
}

// Close closes the client connection
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// GetMetrics returns client metrics
func (c *Client) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"connectionId":      c.connectionID,
		"connectedAt":       c.connectedAt,
		"messagesSent":      c.messagesSent.Load(),
		"messagesDropped":   c.messagesDropped.Load(),
		"backpressureCount": c.backpressureCount.Load(),
		"lastMessageAt":     c.lastMessageAt,
	}
}

// clientConnection adapts Client to messaging.DashboardConnection interface
type clientConnection struct {
	client *Client
}

func (c *clientConnection) Send(message []byte) error {
	return c.client.Send(message)
}

func (c *clientConnection) GetConnectionID() string {
	return c.client.connectionID
}

func (c *clientConnection) IsConnected() bool {
	return c.client.conn != nil
}

func (c *clientConnection) Close() error {
	if c.client.conn != nil {
		return c.client.conn.Close()
	}
	return nil
}

// GetMetrics returns hub-level metrics
func (h *Hub) GetMetrics() map[string]interface{} {
	h.mu.RLock()
	agentCount := len(h.agents)
	dashboardCount := 0
	for _, clients := range h.dashboards {
		dashboardCount += len(clients)
	}
	h.mu.RUnlock()

	metrics := map[string]interface{}{
		"agents":                    agentCount,
		"dashboards":                dashboardCount,
		"totalAgentConnections":     h.totalAgentConnections.Load(),
		"totalDashboardConnections": h.totalDashboardConnections.Load(),
		"totalMessagesRouted":       h.totalMessagesRouted.Load(),
		"totalMessagesDropped":      h.totalMessagesDropped.Load(),
		"totalBackpressureEvents":   h.totalBackpressureEvents.Load(),
	}

	// Add pending request metrics if available
	if h.pendingRequests != nil {
		metrics["pendingRequests"] = h.pendingRequests.GetMetrics()
	}

	// Add router metrics if available
	if h.router != nil {
		metrics["router"] = h.router.GetMetrics()
	}

	return metrics
}

// SubscribeToDevice subscribes a dashboard to device-specific messages
func (h *Hub) SubscribeToDevice(connID string, deviceID uuid.UUID) bool {
	if h.registry == nil {
		return false
	}
	return h.registry.Subscribe(connID, messaging.SubDeviceMetrics, deviceID, "")
}

// SubscribeToSession subscribes a dashboard to session-specific messages (terminal, RDP)
func (h *Hub) SubscribeToSession(connID, sessionID string, subType messaging.SubscriptionType) bool {
	if h.registry == nil {
		return false
	}
	return h.registry.Subscribe(connID, subType, uuid.UUID{}, sessionID)
}

// SendToSession sends a message to the owner of a terminal/RDP session
func (h *Hub) SendToSession(sessionID string, message []byte) error {
	if h.router == nil {
		// Fallback to broadcast
		h.BroadcastToDashboards(message)
		return nil
	}

	result := h.router.SendToSession(sessionID, message)
	if result.Sent == 0 {
		// Fallback to broadcast for backward compatibility
		h.BroadcastToDashboards(message)
	}
	return nil
}

// RouteResponse routes a response from an agent back to the requesting dashboard
func (h *Hub) RouteResponse(env *messaging.MessageEnvelope) bool {
	if h.router == nil {
		return false
	}
	return h.router.RouteResponse(env)
}

// Errors
var (
	ErrAgentNotConnected   = &HubError{Message: "agent not connected"}
	ErrSendFailed          = &HubError{Message: "failed to send message"}
	ErrConnectionNotFound  = &HubError{Message: "connection not found"}
	ErrUserNotConnected    = &HubError{Message: "user not connected"}
	ErrBackpressure        = &HubError{Message: "send buffer full (backpressure)"}
)

type HubError struct {
	Message string
}

func (e *HubError) Error() string {
	return e.Message
}
