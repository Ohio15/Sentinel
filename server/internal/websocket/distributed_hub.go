package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

const (
	// Redis key prefixes for distributed state
	agentLocationPrefix = "sentinel:agent:location:"  // Maps agentID -> serverID
	serverAgentsPrefix  = "sentinel:server:agents:"   // Set of agents on each server
	agentChannelPrefix  = "sentinel:agent:channel:"   // Pub/sub channel per agent
	broadcastChannel    = "sentinel:broadcast"        // Global broadcast channel

	// TTL for agent location (should be > heartbeat interval)
	agentLocationTTL = 2 * time.Minute
)

// AgentLocation stores where an agent is connected
type AgentLocation struct {
	ServerID    string    `json:"serverId"`
	DeviceID    uuid.UUID `json:"deviceId"`
	ConnectedAt time.Time `json:"connectedAt"`
}

// DistributedMessage wraps messages for cross-server routing
type DistributedMessage struct {
	TargetAgentID string          `json:"targetAgentId,omitempty"`
	TargetUserID  string          `json:"targetUserId,omitempty"`
	Broadcast     bool            `json:"broadcast,omitempty"`
	SourceServer  string          `json:"sourceServer,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

// DistributedHub extends Hub with cross-server message routing via Redis
type DistributedHub struct {
	// Embedded local hub for local connections
	*Hub

	// Server identification
	serverID string

	// Redis client for distributed state
	redis *redis.Client

	// Context for background goroutines
	ctx    context.Context
	cancel context.CancelFunc

	// Track subscriptions to avoid duplicates
	subscriptions map[string]context.CancelFunc
	subMu         sync.Mutex
}

// NewDistributedHub creates a new distributed hub with Redis backend
func NewDistributedHub(redisClient *redis.Client, serverID string) *DistributedHub {
	ctx, cancel := context.WithCancel(context.Background())

	hub := &DistributedHub{
		Hub:           NewHub(nil),
		serverID:      serverID,
		redis:         redisClient,
		ctx:           ctx,
		cancel:        cancel,
		subscriptions: make(map[string]context.CancelFunc),
	}

	return hub
}

// Run starts the distributed hub and all background processes
func (h *DistributedHub) Run() {
	// Start the base hub
	go h.Hub.Run()

	// Start Redis subscribers
	go h.subscribeToBroadcast()
	go h.heartbeatLoop()

	log.Printf("Distributed hub started for server: %s", h.serverID)
}

// RegisterAgentDistributed registers agent and publishes location to Redis
func (h *DistributedHub) RegisterAgentDistributed(conn *websocket.Conn, agentID string, deviceID uuid.UUID) *Client {
	// Register locally first
	client := h.Hub.RegisterAgent(conn, agentID, deviceID)

	// Store agent location in Redis
	ctx := context.Background()
	location := AgentLocation{
		ServerID:    h.serverID,
		DeviceID:    deviceID,
		ConnectedAt: time.Now(),
	}
	locationJSON, _ := json.Marshal(location)

	pipe := h.redis.Pipeline()
	pipe.Set(ctx, agentLocationPrefix+agentID, locationJSON, agentLocationTTL)
	pipe.SAdd(ctx, serverAgentsPrefix+h.serverID, agentID)
	_, err := pipe.Exec(ctx)
	if err != nil {
		log.Printf("Failed to register agent in Redis: %v", err)
	}

	// Subscribe to this agent's channel for cross-server messages
	h.subscribeToAgentChannel(agentID)

	log.Printf("Agent %s registered on server %s (distributed)", agentID, h.serverID)
	return client
}

// UnregisterAgentDistributed removes agent from Redis
func (h *DistributedHub) UnregisterAgentDistributed(agentID string) {
	ctx := context.Background()

	// Cancel the subscription for this agent
	h.subMu.Lock()
	if cancel, ok := h.subscriptions[agentID]; ok {
		cancel()
		delete(h.subscriptions, agentID)
	}
	h.subMu.Unlock()

	// Remove from Redis
	pipe := h.redis.Pipeline()
	pipe.Del(ctx, agentLocationPrefix+agentID)
	pipe.SRem(ctx, serverAgentsPrefix+h.serverID, agentID)
	_, err := pipe.Exec(ctx)
	if err != nil {
		log.Printf("Failed to unregister agent from Redis: %v", err)
	}

	log.Printf("Agent %s unregistered from server %s (distributed)", agentID, h.serverID)
}

// SendToAgentDistributed routes message to agent regardless of which server it's on
func (h *DistributedHub) SendToAgentDistributed(agentID string, message []byte) error {
	// First, try local delivery
	if err := h.Hub.SendToAgent(agentID, message); err == nil {
		return nil
	}

	// Agent not local - look up location in Redis
	ctx := context.Background()
	locationJSON, err := h.redis.Get(ctx, agentLocationPrefix+agentID).Bytes()
	if err == redis.Nil {
		return ErrAgentNotConnected
	}
	if err != nil {
		return fmt.Errorf("redis lookup failed: %w", err)
	}

	var location AgentLocation
	if err := json.Unmarshal(locationJSON, &location); err != nil {
		return fmt.Errorf("invalid agent location data: %w", err)
	}

	// Publish to agent's channel for the target server to pick up
	msg := DistributedMessage{
		TargetAgentID: agentID,
		SourceServer:  h.serverID,
		Payload:       message,
	}
	msgJSON, _ := json.Marshal(msg)

	err = h.redis.Publish(ctx, agentChannelPrefix+agentID, msgJSON).Err()
	if err != nil {
		return fmt.Errorf("failed to publish to agent channel: %w", err)
	}

	return nil
}

// BroadcastToDashboardsDistributed sends to all dashboards across all servers
func (h *DistributedHub) BroadcastToDashboardsDistributed(message []byte) {
	// Local broadcast
	h.Hub.BroadcastToDashboards(message)

	// Distributed broadcast via Redis pub/sub
	ctx := context.Background()
	msg := DistributedMessage{
		Broadcast:    true,
		SourceServer: h.serverID,
		Payload:      message,
	}
	msgJSON, _ := json.Marshal(msg)

	err := h.redis.Publish(ctx, broadcastChannel, msgJSON).Err()
	if err != nil {
		log.Printf("Failed to publish broadcast: %v", err)
	}
}

// subscribeToAgentChannel listens for messages to a specific agent
func (h *DistributedHub) subscribeToAgentChannel(agentID string) {
	h.subMu.Lock()
	if _, exists := h.subscriptions[agentID]; exists {
		h.subMu.Unlock()
		return // Already subscribed
	}

	subCtx, cancel := context.WithCancel(h.ctx)
	h.subscriptions[agentID] = cancel
	h.subMu.Unlock()

	go func() {
		pubsub := h.redis.Subscribe(subCtx, agentChannelPrefix+agentID)
		defer pubsub.Close()

		ch := pubsub.Channel()
		for {
			select {
			case <-subCtx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}

				var dm DistributedMessage
				if err := json.Unmarshal([]byte(msg.Payload), &dm); err != nil {
					log.Printf("Failed to unmarshal distributed message: %v", err)
					continue
				}

				// Don't process our own messages
				if dm.SourceServer == h.serverID {
					continue
				}

				// Deliver locally
				if err := h.Hub.SendToAgent(agentID, dm.Payload); err != nil {
					log.Printf("Failed to deliver message to agent %s: %v", agentID, err)
				}
			}
		}
	}()
}

// subscribeToBroadcast listens for dashboard broadcasts from other servers
func (h *DistributedHub) subscribeToBroadcast() {
	pubsub := h.redis.Subscribe(h.ctx, broadcastChannel)
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case <-h.ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}

			var dm DistributedMessage
			if err := json.Unmarshal([]byte(msg.Payload), &dm); err != nil {
				log.Printf("Failed to unmarshal broadcast message: %v", err)
				continue
			}

			// Don't process our own broadcasts
			if dm.SourceServer == h.serverID {
				continue
			}

			if dm.Broadcast {
				h.Hub.BroadcastToDashboards(dm.Payload)
			}
		}
	}
}

// heartbeatLoop maintains agent location TTL in Redis
func (h *DistributedHub) heartbeatLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.refreshAgentLocations()
		}
	}
}

// refreshAgentLocations extends TTL for all local agents in Redis
func (h *DistributedHub) refreshAgentLocations() {
	ctx := context.Background()

	h.mu.RLock()
	agents := make([]string, 0, len(h.agents))
	for agentID := range h.agents {
		agents = append(agents, agentID)
	}
	h.mu.RUnlock()

	if len(agents) == 0 {
		return
	}

	pipe := h.redis.Pipeline()
	for _, agentID := range agents {
		pipe.Expire(ctx, agentLocationPrefix+agentID, agentLocationTTL)
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		log.Printf("Failed to refresh agent locations: %v", err)
	}
}

// IsAgentOnlineGlobal checks if agent is online on any server
func (h *DistributedHub) IsAgentOnlineGlobal(agentID string) bool {
	// Check local first
	if h.Hub.IsAgentOnline(agentID) {
		return true
	}

	// Check Redis
	ctx := context.Background()
	exists, err := h.redis.Exists(ctx, agentLocationPrefix+agentID).Result()
	if err != nil {
		log.Printf("Failed to check agent online status: %v", err)
		return false
	}
	return exists > 0
}

// GetAgentServer returns which server an agent is connected to
func (h *DistributedHub) GetAgentServer(agentID string) (string, error) {
	ctx := context.Background()
	locationJSON, err := h.redis.Get(ctx, agentLocationPrefix+agentID).Bytes()
	if err == redis.Nil {
		return "", ErrAgentNotConnected
	}
	if err != nil {
		return "", fmt.Errorf("redis lookup failed: %w", err)
	}

	var location AgentLocation
	if err := json.Unmarshal(locationJSON, &location); err != nil {
		return "", fmt.Errorf("invalid agent location data: %w", err)
	}

	return location.ServerID, nil
}

// GetGlobalOnlineAgents returns all online agents across all servers
func (h *DistributedHub) GetGlobalOnlineAgents() ([]string, error) {
	ctx := context.Background()

	// Get all agent location keys
	keys, err := h.redis.Keys(ctx, agentLocationPrefix+"*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get agent keys: %w", err)
	}

	agents := make([]string, 0, len(keys))
	for _, key := range keys {
		agentID := key[len(agentLocationPrefix):]
		agents = append(agents, agentID)
	}

	return agents, nil
}

// GetServerStats returns statistics about this server's connections
func (h *DistributedHub) GetServerStats() map[string]interface{} {
	h.mu.RLock()
	agentCount := len(h.agents)
	dashboardCount := 0
	for _, clients := range h.dashboards {
		dashboardCount += len(clients)
	}
	h.mu.RUnlock()

	return map[string]interface{}{
		"server_id":        h.serverID,
		"local_agents":     agentCount,
		"local_dashboards": dashboardCount,
	}
}

// Close cleanly shuts down the distributed hub
func (h *DistributedHub) Close() {
	log.Printf("Shutting down distributed hub for server: %s", h.serverID)

	// Cancel all background goroutines
	h.cancel()

	// Clean up all agent registrations from Redis
	ctx := context.Background()
	h.mu.RLock()
	for agentID := range h.agents {
		h.redis.Del(ctx, agentLocationPrefix+agentID)
	}
	h.mu.RUnlock()

	// Remove this server from the server agents set
	h.redis.Del(ctx, serverAgentsPrefix+h.serverID)

	log.Printf("Distributed hub shutdown complete for server: %s", h.serverID)
}

// WebSocketHub interface implementation wrappers
func (h *DistributedHub) SendToAgent(agentID string, message []byte) error {
	return h.SendToAgentDistributed(agentID, message)
}

func (h *DistributedHub) BroadcastToDashboards(message []byte) {
	h.BroadcastToDashboardsDistributed(message)
}

func (h *DistributedHub) IsAgentOnline(agentID string) bool {
	return h.IsAgentOnlineGlobal(agentID)
}

func (h *DistributedHub) RegisterAgent(conn *websocket.Conn, agentID string, deviceID uuid.UUID) *Client {
	return h.RegisterAgentDistributed(conn, agentID, deviceID)
}

func (h *DistributedHub) RegisterDashboard(conn *websocket.Conn, userID uuid.UUID) *Client {
	// DistributedHub doesn't have dashboard registration yet - create local client
	client := &Client{
		hub:      nil,
		conn:     conn,
		send:     make(chan []byte, 256),
		agentID:  "",
		deviceID: uuid.Nil,
		userID:   userID,
		isAgent:  false,
	}
	return client
}
