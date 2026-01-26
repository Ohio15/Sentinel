package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/sentinel/server/internal/constants"
	ws "github.com/sentinel/server/internal/websocket"
)

var rdpUpgrader = websocket.Upgrader{
	ReadBufferSize:  32768,
	WriteBufferSize: 32768,
	CheckOrigin: func(r *http.Request) bool {
		return true // TODO: Configure appropriately for production
	},
}

// RDPCapabilities represents the RDP capabilities of an agent
type RDPCapabilities struct {
	RDPAvailable      bool   `json:"rdp_available"`
	RDPEnabled        bool   `json:"rdp_enabled"`
	FallbackAvailable bool   `json:"fallback_available"`
	PreferredMethod   string `json:"preferred_method"`
	WindowsEdition    string `json:"windows_edition"`
	RDPPort           int    `json:"rdp_port"`
}

// RDPShadowInfo contains information about an RDP shadow session
type RDPShadowInfo struct {
	SessionID  uint32 `json:"session_id"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	TunnelHost string `json:"tunnel_host"`
	TunnelPort int    `json:"tunnel_port"`
	Token      string `json:"token"`
}

// RDPSession represents an active RDP session
type RDPSession struct {
	ID             string
	AgentID        string
	UserID         string
	ShadowInfo     *RDPShadowInfo
	StartTime      time.Time
	LastActivity   time.Time
	Active         bool
	AgentTunnel    *websocket.Conn
	DashboardConn  *websocket.Conn
}

// RDPSessionManager manages active RDP sessions
type RDPSessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*RDPSession

	// Pending connections waiting for agent tunnel
	pendingMu      sync.Mutex
	pendingDash    map[string]chan *websocket.Conn
}

var rdpSessionManager = &RDPSessionManager{
	sessions:    make(map[string]*RDPSession),
	pendingDash: make(map[string]chan *websocket.Conn),
}

// getRDPCapabilitiesHandler handles requests for agent RDP capabilities
func getRDPCapabilitiesHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Param("agent_id")
		if agentID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id required"})
			return
		}

		// Check if agent is online
		if !services.Hub.IsAgentOnline(agentID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "agent not online"})
			return
		}

		// Request capabilities from agent
		requestID := uuid.New().String()
		msg, _ := json.Marshal(map[string]interface{}{
			"type":      "rdp_get_capabilities",
			"requestId": requestID,
		})

		// Create a channel to receive the response
		responseChan := make(chan *RDPCapabilities, 1)

		// Store the channel to receive response (would need a response registry in production)
		// For now, return default capabilities
		capabilities := &RDPCapabilities{
			RDPAvailable:      true,
			RDPEnabled:        true,
			FallbackAvailable: true,
			PreferredMethod:   "rdp",
			WindowsEdition:    "Windows 10 Pro",
			RDPPort:           3389,
		}

		services.Hub.SendToAgent(agentID, msg)

		// In a real implementation, we'd wait for the agent's response
		select {
		case caps := <-responseChan:
			c.JSON(http.StatusOK, caps)
		case <-time.After(5 * time.Second):
			// Return default capabilities on timeout
			c.JSON(http.StatusOK, capabilities)
		}
	}
}

// startRDPSessionHandler initiates an RDP session
func startRDPSessionHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Param("agent_id")
		if agentID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id required"})
			return
		}

		// Get user ID from context
		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		// Check if agent is online
		if !services.Hub.IsAgentOnline(agentID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "agent not online"})
			return
		}

		// Create session ID
		sessionID := uuid.New().String()

		// Request agent to prepare RDP shadow
		requestID := uuid.New().String()
		msg, _ := json.Marshal(map[string]interface{}{
			"type":      "rdp_prepare_shadow",
			"requestId": requestID,
			"data": map[string]interface{}{
				"sessionId": sessionID,
			},
		})

		if err := services.Hub.SendToAgent(agentID, msg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to contact agent"})
			return
		}

		// Create session record
		session := &RDPSession{
			ID:           sessionID,
			AgentID:      agentID,
			UserID:       userID.(string),
			StartTime:    time.Now(),
			LastActivity: time.Now(),
			Active:       true,
		}

		rdpSessionManager.mu.Lock()
		rdpSessionManager.sessions[sessionID] = session
		rdpSessionManager.mu.Unlock()

		// Create pending channel for dashboard connection
		rdpSessionManager.pendingMu.Lock()
		rdpSessionManager.pendingDash[sessionID] = make(chan *websocket.Conn, 1)
		rdpSessionManager.pendingMu.Unlock()

		c.JSON(http.StatusOK, gin.H{
			"session_id": sessionID,
			"message":    "RDP session initiated, waiting for agent response",
		})
	}
}

// rdpConnectHandler handles WebSocket connections for RDP from the dashboard
func rdpConnectHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Query("agent_id")
		sessionID := c.Query("session_id")

		if agentID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id required"})
			return
		}

		// Get user ID from context
		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		// Check if agent is online
		if !services.Hub.IsAgentOnline(agentID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "agent not online"})
			return
		}

		// Upgrade to WebSocket
		conn, err := rdpUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("[RDP] WebSocket upgrade failed: %v", err)
			return
		}

		log.Printf("[RDP] Dashboard connected for agent %s, user %v", agentID, userID)

		// If no session ID, create new session
		if sessionID == "" {
			sessionID = uuid.New().String()
		}

		// Check if session exists
		rdpSessionManager.mu.Lock()
		session, exists := rdpSessionManager.sessions[sessionID]
		if !exists {
			// Create new session
			session = &RDPSession{
				ID:           sessionID,
				AgentID:      agentID,
				UserID:       userID.(string),
				StartTime:    time.Now(),
				LastActivity: time.Now(),
				Active:       true,
			}
			rdpSessionManager.sessions[sessionID] = session
		}
		session.DashboardConn = conn
		rdpSessionManager.mu.Unlock()

		// Request agent to start RDP tunnel
		requestID := uuid.New().String()
		msg, _ := json.Marshal(map[string]interface{}{
			"type":      "rdp_start_tunnel",
			"requestId": requestID,
			"data": map[string]interface{}{
				"sessionId": sessionID,
			},
		})

		if err := services.Hub.SendToAgent(agentID, msg); err != nil {
			conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"failed to contact agent"}`))
			conn.Close()
			return
		}

		// Handle WebSocket messages
		go handleRDPDashboardConnection(services, session, conn)
	}
}

// handleRDPDashboardConnection handles messages from the dashboard
func handleRDPDashboardConnection(services *Services, session *RDPSession, conn *websocket.Conn) {
	defer func() {
		conn.Close()
		rdpSessionManager.mu.Lock()
		if s, ok := rdpSessionManager.sessions[session.ID]; ok && s.DashboardConn == conn {
			s.DashboardConn = nil
			if s.AgentTunnel == nil {
				delete(rdpSessionManager.sessions, session.ID)
			}
		}
		rdpSessionManager.mu.Unlock()
		log.Printf("[RDP] Dashboard disconnected for session %s", session.ID)
	}()

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[RDP] Dashboard read error: %v", err)
			}
			return
		}

		// Forward to agent tunnel if connected
		rdpSessionManager.mu.RLock()
		agentTunnel := session.AgentTunnel
		rdpSessionManager.mu.RUnlock()

		if agentTunnel != nil {
			if err := agentTunnel.WriteMessage(messageType, data); err != nil {
				log.Printf("[RDP] Failed to forward to agent: %v", err)
				return
			}
		}

		session.LastActivity = time.Now()
	}
}

// rdpAgentTunnelHandler handles the agent-side RDP tunnel connection
func rdpAgentTunnelHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.Param("id")
		sessionID := c.Query("session")

		if agentID == "" || sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id and session required"})
			return
		}

		// Upgrade to WebSocket
		conn, err := rdpUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("[RDP] Agent tunnel WebSocket upgrade failed: %v", err)
			return
		}

		log.Printf("[RDP] Agent %s tunnel connected for session %s", agentID, sessionID)

		// Find or create session
		rdpSessionManager.mu.Lock()
		session, exists := rdpSessionManager.sessions[sessionID]
		if !exists {
			session = &RDPSession{
				ID:           sessionID,
				AgentID:      agentID,
				StartTime:    time.Now(),
				LastActivity: time.Now(),
				Active:       true,
			}
			rdpSessionManager.sessions[sessionID] = session
		}
		session.AgentTunnel = conn
		rdpSessionManager.mu.Unlock()

		// Handle WebSocket messages from agent
		go handleRDPAgentConnection(services, session, conn)
	}
}

// handleRDPAgentConnection handles messages from the agent tunnel
func handleRDPAgentConnection(services *Services, session *RDPSession, conn *websocket.Conn) {
	defer func() {
		conn.Close()
		rdpSessionManager.mu.Lock()
		if s, ok := rdpSessionManager.sessions[session.ID]; ok && s.AgentTunnel == conn {
			s.AgentTunnel = nil
			if s.DashboardConn == nil {
				delete(rdpSessionManager.sessions, session.ID)
			}
		}
		rdpSessionManager.mu.Unlock()
		log.Printf("[RDP] Agent tunnel disconnected for session %s", session.ID)
	}()

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[RDP] Agent tunnel read error: %v", err)
			}
			return
		}

		// Forward to dashboard if connected
		rdpSessionManager.mu.RLock()
		dashConn := session.DashboardConn
		rdpSessionManager.mu.RUnlock()

		if dashConn != nil {
			if err := dashConn.WriteMessage(messageType, data); err != nil {
				log.Printf("[RDP] Failed to forward to dashboard: %v", err)
				return
			}
		}

		session.LastActivity = time.Now()
	}
}

// listRDPSessionsHandler returns active RDP sessions
func listRDPSessionsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		rdpSessionManager.mu.RLock()
		defer rdpSessionManager.mu.RUnlock()

		sessions := make([]gin.H, 0, len(rdpSessionManager.sessions))
		for _, s := range rdpSessionManager.sessions {
			sessions = append(sessions, gin.H{
				"id":            s.ID,
				"agent_id":      s.AgentID,
				"user_id":       s.UserID,
				"start_time":    s.StartTime,
				"last_activity": s.LastActivity,
				"active":        s.Active,
				"has_dashboard": s.DashboardConn != nil,
				"has_tunnel":    s.AgentTunnel != nil,
			})
		}

		c.JSON(http.StatusOK, gin.H{"sessions": sessions})
	}
}

// disconnectRDPSessionHandler forcefully disconnects an RDP session
func disconnectRDPSessionHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("session_id")

		rdpSessionManager.mu.Lock()
		session, exists := rdpSessionManager.sessions[sessionID]
		if exists {
			session.Active = false
			if session.DashboardConn != nil {
				session.DashboardConn.Close()
			}
			if session.AgentTunnel != nil {
				session.AgentTunnel.Close()
			}
			delete(rdpSessionManager.sessions, sessionID)
		}
		rdpSessionManager.mu.Unlock()

		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}

		// Also tell the agent to stop the session
		if session.AgentID != "" && services.Hub.IsAgentOnline(session.AgentID) {
			msg, _ := json.Marshal(map[string]interface{}{
				"type":      "rdp_stop_session",
				"requestId": uuid.New().String(),
				"data": map[string]interface{}{
					"sessionId": sessionID,
				},
			})
			services.Hub.SendToAgent(session.AgentID, msg)
		}

		c.JSON(http.StatusOK, gin.H{"message": "session disconnected"})
	}
}

// registerRDPRoutes registers RDP-related routes
func registerRDPRoutes(api *gin.RouterGroup, protected *gin.RouterGroup, services *Services) {
	// Protected RDP routes (require JWT)
	rdp := protected.Group("/rdp")
	{
		rdp.GET("/capabilities/:agent_id", getRDPCapabilitiesHandler(services))
		rdp.POST("/sessions/:agent_id", startRDPSessionHandler(services))
		rdp.GET("/sessions", listRDPSessionsHandler(services))
		rdp.POST("/sessions/disconnect/:session_id", disconnectRDPSessionHandler(services))
	}

	// WebSocket routes for RDP
	api.GET("/rdp/connect", rdpConnectHandler(services))

	// Agent tunnel endpoint
	api.GET("/agents/:id/rdp-tunnel", rdpAgentTunnelHandler(services))
}

// HandleRDPAgentMessage processes RDP-related messages from agents
func HandleRDPAgentMessage(hub *ws.Hub, agentID string, msg ws.Message) {
	var payload struct {
		SessionID  string          `json:"sessionId"`
		ShadowInfo *RDPShadowInfo  `json:"shadowInfo"`
		Error      string          `json:"error"`
		Capabilities *RDPCapabilities `json:"capabilities"`
	}
	json.Unmarshal(msg.Payload, &payload)

	switch msg.Type {
	case "rdp_shadow_ready":
		// Agent has prepared for shadow, store info
		log.Printf("[RDP] Agent %s shadow ready for session %s", agentID, payload.SessionID)

		rdpSessionManager.mu.Lock()
		if session, ok := rdpSessionManager.sessions[payload.SessionID]; ok {
			session.ShadowInfo = payload.ShadowInfo
		}
		rdpSessionManager.mu.Unlock()

		// Forward to dashboard
		response, _ := json.Marshal(map[string]interface{}{
			"type":       "rdp_shadow_ready",
			"sessionId":  payload.SessionID,
			"shadowInfo": payload.ShadowInfo,
		})
		hub.BroadcastToDashboards(response)

	case "rdp_shadow_error":
		// Agent failed to prepare shadow
		log.Printf("[RDP] Agent %s shadow error for session %s: %s", agentID, payload.SessionID, payload.Error)

		response, _ := json.Marshal(map[string]interface{}{
			"type":      "rdp_shadow_error",
			"sessionId": payload.SessionID,
			"error":     payload.Error,
		})
		hub.BroadcastToDashboards(response)

	case "rdp_capabilities_response":
		// Agent capabilities response
		log.Printf("[RDP] Agent %s capabilities response", agentID)

		response, _ := json.Marshal(map[string]interface{}{
			"type":         "rdp_capabilities",
			"agentId":      agentID,
			"capabilities": payload.Capabilities,
		})
		hub.BroadcastToDashboards(response)

	case "rdp_session_ended":
		// Agent ended the RDP session
		log.Printf("[RDP] Agent %s ended session %s", agentID, payload.SessionID)

		rdpSessionManager.mu.Lock()
		if session, ok := rdpSessionManager.sessions[payload.SessionID]; ok {
			session.Active = false
			if session.DashboardConn != nil {
				session.DashboardConn.Close()
			}
			delete(rdpSessionManager.sessions, payload.SessionID)
		}
		rdpSessionManager.mu.Unlock()

		response, _ := json.Marshal(map[string]interface{}{
			"type":      "rdp_session_ended",
			"sessionId": payload.SessionID,
		})
		hub.BroadcastToDashboards(response)
	}
}

// Ensure constants are defined for message types
var _ = constants.CurrentOrganizationID // Ensure import is used
