package guacamole

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// getUpgrader returns a WebSocket upgrader with proper origin validation
func getUpgrader(environment string, allowedOrigins []string) websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  ReadBufferSize,
		WriteBufferSize: WriteBufferSize,
		CheckOrigin: func(req *http.Request) bool {
			if environment != "production" {
				return true
			}
			origin := req.Header.Get("Origin")

			// Agent-side tunnel connections — native apps don't send Origin
			if strings.Contains(req.URL.Path, "/agent/") || strings.Contains(req.URL.Path, "/tunnel") {
				return true
			}

			// Dashboard connections MUST have valid Origin
			if origin == "" {
				log.Printf("[SECURITY] Guacamole WebSocket rejected: no Origin header from %s on %s", req.RemoteAddr, req.URL.Path)
				return false
			}

			for _, allowed := range allowedOrigins {
				if origin == allowed {
					return true
				}
			}
			log.Printf("[SECURITY] Guacamole WebSocket rejected: invalid Origin %q from %s", origin, req.RemoteAddr)
			return false
		},
	}
}

// RDPSession represents an active RDP session
type RDPSession struct {
	ID          string
	AgentID     string
	UserID      string
	Config      RDPConnectionConfig
	GuacdConn   *GuacdConnection
	StartTime   time.Time
	LastActivity time.Time
	Active      bool
}

// ShadowInfo contains information received from the agent about the shadow session
type ShadowInfo struct {
	SessionID  uint32 `json:"session_id"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	TunnelHost string `json:"tunnel_host"`
	TunnelPort int    `json:"tunnel_port"`
	Token      string `json:"token"`
}

// AgentManager interface for interacting with agents
type AgentManager interface {
	GetAgentStatus(agentID string) (online bool, err error)
	PrepareRDPShadow(ctx context.Context, agentID string) (*ShadowInfo, error)
	HandleRDPTunnel(agentID string, sessionID string, ws *websocket.Conn) error
	GetRDPCapabilities(agentID string) (*Capabilities, error)
}

// Capabilities describes the RDP capabilities of an agent
type Capabilities struct {
	RDPAvailable      bool   `json:"rdp_available"`
	RDPEnabled        bool   `json:"rdp_enabled"`
	FallbackAvailable bool   `json:"fallback_available"`
	PreferredMethod   string `json:"preferred_method"`
	WindowsEdition    string `json:"windows_edition"`
	RDPPort           int    `json:"rdp_port"`
}

// RDPHandler manages RDP connections through Guacamole
type RDPHandler struct {
	guacdAddr      string
	agentManager   AgentManager
	environment    string
	allowedOrigins []string

	mu       sync.RWMutex
	sessions map[string]*RDPSession
}

// NewRDPHandler creates a new RDP handler
func NewRDPHandler(guacdAddr string, am AgentManager, environment string, allowedOrigins []string) *RDPHandler {
	if guacdAddr == "" {
		guacdAddr = DefaultGuacdAddr
	}

	handler := &RDPHandler{
		guacdAddr:      guacdAddr,
		agentManager:   am,
		environment:    environment,
		allowedOrigins: allowedOrigins,
		sessions:       make(map[string]*RDPSession),
	}

	// Start session cleanup goroutine
	go handler.cleanupSessions()

	return handler
}

// HandleRDPConnect handles WebSocket connections for RDP sessions from the browser
// Route: GET /api/rdp/connect
func (h *RDPHandler) HandleRDPConnect(c *gin.Context) {
	agentID := c.Query("agent_id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id required"})
		return
	}

	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// Check agent is online
	online, err := h.agentManager.GetAgentStatus(agentID)
	if err != nil || !online {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not online"})
		return
	}

	// Upgrade to WebSocket
	upgrader := getUpgrader(h.environment, h.allowedOrigins)
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[RDPHandler] WebSocket upgrade failed: %v", err)
		return
	}
	defer ws.Close()

	log.Printf("[RDPHandler] New RDP connection for agent %s from user %v", agentID, userID)

	// Request agent to prepare for RDP shadow
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	shadowInfo, err := h.agentManager.PrepareRDPShadow(ctx, agentID)
	if err != nil {
		log.Printf("[RDPHandler] Failed to prepare shadow: %v", err)
		sendGuacError(ws, "Failed to prepare shadow session: "+err.Error())
		return
	}

	log.Printf("[RDPHandler] Shadow info received: session=%d, tunnel=%s:%d",
		shadowInfo.SessionID, shadowInfo.TunnelHost, shadowInfo.TunnelPort)

	// Connect to guacd
	guacd, err := NewGuacdConnection(h.guacdAddr)
	if err != nil {
		log.Printf("[RDPHandler] Failed to connect to guacd: %v", err)
		sendGuacError(ws, "Failed to connect to remote desktop service")
		return
	}
	defer guacd.Close()

	// Build RDP configuration
	config := DefaultRDPConfig()
	config.Hostname = shadowInfo.TunnelHost
	config.Port = shadowInfo.TunnelPort
	config.Username = shadowInfo.Username
	config.Password = shadowInfo.Password
	config.ShadowSessionID = shadowInfo.SessionID
	config.ShadowControl = true // Full control for RMM

	// Parse display settings from query params if provided
	if w := c.Query("width"); w != "" {
		fmt.Sscanf(w, "%d", &config.Width)
	}
	if h := c.Query("height"); h != "" {
		fmt.Sscanf(h, "%d", &config.Height)
	}
	if dpi := c.Query("dpi"); dpi != "" {
		fmt.Sscanf(dpi, "%d", &config.DPI)
	}

	// Initiate RDP connection through guacd
	if err := guacd.InitiateRDPConnection(config); err != nil {
		log.Printf("[RDPHandler] RDP connection failed: %v", err)
		sendGuacError(ws, "RDP connection failed: "+err.Error())
		return
	}

	// Create session record
	sessionID := fmt.Sprintf("%s-%s-%d", agentID, userID, time.Now().UnixNano())
	session := &RDPSession{
		ID:           sessionID,
		AgentID:      agentID,
		UserID:       fmt.Sprintf("%v", userID),
		Config:       config,
		GuacdConn:    guacd,
		StartTime:    time.Now(),
		LastActivity: time.Now(),
		Active:       true,
	}

	h.mu.Lock()
	h.sessions[sessionID] = session
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.sessions, sessionID)
		h.mu.Unlock()
		log.Printf("[RDPHandler] Session %s ended", sessionID)
	}()

	log.Printf("[RDPHandler] Session %s started, bridging to guacd", sessionID)

	// Bridge WebSocket to guacd
	if err := guacd.Bridge(ws); err != nil {
		log.Printf("[RDPHandler] Bridge error: %v", err)
	}
}

// HandleAgentRDPTunnel handles the agent-side of the RDP tunnel
// This creates a WebSocket tunnel from the agent to the server
// Route: GET /api/agents/:id/rdp-tunnel
func (h *RDPHandler) HandleAgentRDPTunnel(c *gin.Context) {
	agentID := c.Param("id")
	sessionID := c.Query("session")

	if agentID == "" || sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id and session required"})
		return
	}

	// Upgrade to WebSocket
	upgrader := getUpgrader(h.environment, h.allowedOrigins)
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[RDPHandler] Agent tunnel WebSocket upgrade failed: %v", err)
		return
	}

	log.Printf("[RDPHandler] Agent %s tunnel connected for session %s", agentID, sessionID)

	// Forward RDP traffic between this WebSocket and the RDP client connection
	if err := h.agentManager.HandleRDPTunnel(agentID, sessionID, ws); err != nil {
		log.Printf("[RDPHandler] Agent tunnel error: %v", err)
	}
}

// GetCapabilities returns the RDP capabilities for an agent
// Route: GET /api/rdp/capabilities/:agent_id
func (h *RDPHandler) GetCapabilities(c *gin.Context) {
	agentID := c.Param("agent_id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id required"})
		return
	}

	caps, err := h.agentManager.GetRDPCapabilities(agentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, caps)
}

// GetActiveSessions returns information about active RDP sessions
// Route: GET /api/rdp/sessions
func (h *RDPHandler) GetActiveSessions(c *gin.Context) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	sessions := make([]gin.H, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, gin.H{
			"id":            s.ID,
			"agent_id":      s.AgentID,
			"user_id":       s.UserID,
			"start_time":    s.StartTime,
			"last_activity": s.LastActivity,
			"active":        s.Active,
		})
	}

	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

// DisconnectSession forcefully disconnects an RDP session
// Route: POST /api/rdp/sessions/:id/disconnect
func (h *RDPHandler) DisconnectSession(c *gin.Context) {
	sessionID := c.Param("id")

	h.mu.Lock()
	session, exists := h.sessions[sessionID]
	if exists {
		session.Active = false
		if session.GuacdConn != nil {
			session.GuacdConn.Close()
		}
		delete(h.sessions, sessionID)
	}
	h.mu.Unlock()

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "session disconnected"})
}

// cleanupSessions periodically cleans up stale sessions
func (h *RDPHandler) cleanupSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		h.mu.Lock()
		for id, session := range h.sessions {
			// Remove sessions inactive for more than 1 hour
			if time.Since(session.LastActivity) > time.Hour {
				log.Printf("[RDPHandler] Cleaning up stale session: %s", id)
				if session.GuacdConn != nil {
					session.GuacdConn.Close()
				}
				delete(h.sessions, id)
			}
		}
		h.mu.Unlock()
	}
}

// sendGuacError sends a Guacamole protocol error to the client
func sendGuacError(ws *websocket.Conn, message string) {
	// Guacamole error instruction format
	errInstruction := EncodeInstruction("error", message, "519") // 519 = SERVER_ERROR
	ws.WriteMessage(websocket.TextMessage, []byte(errInstruction))
}

// RegisterRoutes registers the RDP routes with the Gin router
func (h *RDPHandler) RegisterRoutes(r *gin.RouterGroup) {
	rdp := r.Group("/rdp")
	{
		rdp.GET("/connect", h.HandleRDPConnect)
		rdp.GET("/capabilities/:agent_id", h.GetCapabilities)
		rdp.GET("/sessions", h.GetActiveSessions)
		rdp.POST("/sessions/:id/disconnect", h.DisconnectSession)
	}

	// Agent tunnel endpoint (under agents group)
	r.GET("/agents/:id/rdp-tunnel", h.HandleAgentRDPTunnel)
}
