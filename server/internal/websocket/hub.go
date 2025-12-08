package websocket

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/sentinel/server/pkg/cache"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512 * 1024 // 512KB
)

// Message types
const (
	MsgTypeAuth         = "auth"
	MsgTypeAuthResponse = "auth_response"
	MsgTypeHeartbeat    = "heartbeat"
	MsgTypeHeartbeatAck = "heartbeat_ack"
	MsgTypeMetrics      = "metrics"
	MsgTypeCommand      = "execute_command"
	MsgTypeScript       = "execute_script"
	MsgTypeResponse     = "response"
	MsgTypeTerminalStart = "start_terminal"
	MsgTypeTerminalInput = "terminal_input"
	MsgTypeTerminalOutput = "terminal_output"
	MsgTypeTerminalClose = "close_terminal"
	MsgTypeListFiles    = "list_files"
	MsgTypeFileContent  = "file_content"
)

type Message struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId,omitempty"`
	AgentID   string          `json:"agentId,omitempty"`
	Timestamp time.Time       `json:"timestamp,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type Client struct {
	hub       *Hub
	conn      *websocket.Conn
	send      chan []byte
	agentID   string
	deviceID  uuid.UUID
	isAgent   bool
	userID    uuid.UUID
}

type Hub struct {
	// Registered agent clients (agentID -> client)
	agents map[string]*Client

	// Registered dashboard clients (userID -> clients)
	dashboards map[uuid.UUID][]*Client

	// Channels
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte

	// Mutex for thread-safe access
	mu sync.RWMutex

	// Redis for pub/sub
	cache *cache.Cache
}

func NewHub(cache *cache.Cache) *Hub {
	return &Hub{
		agents:     make(map[string]*Client),
		dashboards: make(map[uuid.UUID][]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte),
		cache:      cache,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			if client.isAgent {
				h.agents[client.agentID] = client
				log.Printf("Agent connected: %s", client.agentID)
			} else {
				h.dashboards[client.userID] = append(h.dashboards[client.userID], client)
				log.Printf("Dashboard connected: %s", client.userID)
			}
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if client.isAgent {
				if _, ok := h.agents[client.agentID]; ok {
					delete(h.agents, client.agentID)
					close(client.send)
					log.Printf("Agent disconnected: %s", client.agentID)
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
					log.Printf("Dashboard disconnected: %s", client.userID)
				}
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			// Broadcast to all dashboard clients
			for _, clients := range h.dashboards {
				for _, client := range clients {
					select {
					case client.send <- message:
					default:
						close(client.send)
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) SendToAgent(agentID string, message []byte) error {
	h.mu.RLock()
	client, ok := h.agents[agentID]
	h.mu.RUnlock()

	if !ok {
		return ErrAgentNotConnected
	}

	select {
	case client.send <- message:
		return nil
	default:
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
		hub:      h,
		conn:     conn,
		send:     make(chan []byte, 256),
		agentID:  agentID,
		deviceID: deviceID,
		isAgent:  true,
	}
	h.register <- client
	return client
}

// RegisterDashboard registers a new dashboard client
func (h *Hub) RegisterDashboard(conn *websocket.Conn, userID uuid.UUID) *Client {
	client := &Client{
		hub:     h,
		conn:    conn,
		send:    make(chan []byte, 256),
		userID:  userID,
		isAgent: false,
	}
	h.register <- client
	return client
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

// Errors
var (
	ErrAgentNotConnected = &HubError{Message: "agent not connected"}
	ErrSendFailed        = &HubError{Message: "failed to send message"}
)

type HubError struct {
	Message string
}

func (e *HubError) Error() string {
	return e.Message
}
