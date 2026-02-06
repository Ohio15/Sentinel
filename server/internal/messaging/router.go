package messaging

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// RoutingMode determines how a message should be routed
type RoutingMode int

const (
	// RouteBroadcast sends to all dashboards (legacy behavior)
	RouteBroadcast RoutingMode = iota

	// RouteTargeted sends only to the specified target
	RouteTargeted

	// RouteDevice sends to all dashboards watching a specific device
	RouteDevice

	// RouteSession sends only to the owner of a terminal/RDP session
	RouteSession

	// RouteUser sends to all connections of a specific user
	RouteUser
)

// DeliveryResult tracks the outcome of message delivery
type DeliveryResult struct {
	Sent      int
	Failed    int
	Dropped   int
	Duration  time.Duration
}

// MessageRouter handles targeted message delivery to dashboards.
// It replaces the broadcast-only approach with intelligent routing
// based on subscriptions and session ownership.
type MessageRouter struct {
	registry        *DashboardRegistry
	pendingRequests *PendingRequestManager

	// Configuration
	sendTimeout     time.Duration
	enableBroadcast bool // Fallback to broadcast for old dashboards

	// Metrics
	mu             sync.RWMutex
	messagesSent   int64
	messagesRouted int64
	messagesFailed int64
}

// NewMessageRouter creates a new message router
func NewMessageRouter(registry *DashboardRegistry, pendingRequests *PendingRequestManager) *MessageRouter {
	return &MessageRouter{
		registry:        registry,
		pendingRequests: pendingRequests,
		sendTimeout:     5 * time.Second,
		enableBroadcast: true, // Enable broadcast fallback for backward compatibility
	}
}

// RouteMessage routes a message based on its envelope metadata
func (r *MessageRouter) RouteMessage(env *MessageEnvelope) DeliveryResult {
	start := time.Now()
	result := DeliveryResult{}

	// Determine routing mode based on envelope
	mode := r.determineRoutingMode(env)

	// Get target sessions based on routing mode
	sessions := r.getTargetSessions(env, mode)

	if len(sessions) == 0 {
		// No specific targets - fall back to broadcast if enabled
		if r.enableBroadcast {
			return r.broadcast(env)
		}
		return result
	}

	// Serialize message
	data, err := env.Marshal()
	if err != nil {
		log.Printf("[Router] Failed to marshal message: %v", err)
		result.Failed = 1
		return result
	}

	// Send to each target
	for _, session := range sessions {
		if err := session.Send(data); err != nil {
			result.Failed++
			r.registry.IncrementDropped()
		} else {
			result.Sent++
			r.registry.IncrementMessages()
		}
	}

	result.Duration = time.Since(start)

	r.mu.Lock()
	r.messagesRouted++
	r.messagesSent += int64(result.Sent)
	r.messagesFailed += int64(result.Failed)
	r.mu.Unlock()

	return result
}

// determineRoutingMode decides how to route a message based on its content
func (r *MessageRouter) determineRoutingMode(env *MessageEnvelope) RoutingMode {
	// If specific target is set, use targeted routing
	if env.TargetID != "" {
		return RouteTargeted
	}

	// Session-specific messages go only to session owner
	if env.SessionID != "" {
		msgType := env.Type
		if msgType == "terminal_output" || msgType == "rdp_frame" ||
			msgType == "file_progress" || msgType == "rdp_shadow_ready" ||
			msgType == "rdp_shadow_error" || msgType == "rdp_session_ended" {
			return RouteSession
		}
	}

	// Device-specific messages go to device watchers
	if env.DeviceID != "" {
		msgType := env.Type
		if msgType == "device_metrics" || msgType == "device_status" ||
			msgType == "response" || msgType == "scan_progress" {
			return RouteDevice
		}
	}

	// Default to broadcast for everything else
	return RouteBroadcast
}

// getTargetSessions returns the dashboard sessions that should receive a message
func (r *MessageRouter) getTargetSessions(env *MessageEnvelope, mode RoutingMode) []*DashboardSession {
	switch mode {
	case RouteTargeted:
		// Send to specific connection
		if session, ok := r.registry.GetSession(env.TargetID); ok {
			return []*DashboardSession{session}
		}
		// Target might be a user ID
		userID, err := uuid.Parse(env.TargetID)
		if err == nil {
			return r.registry.GetUserSessions(userID)
		}
		return nil

	case RouteSession:
		// Send only to session owner
		return r.registry.GetSessionOwners(env.SessionID)

	case RouteDevice:
		// Send to all dashboards watching this device
		deviceID, err := uuid.Parse(env.DeviceID)
		if err != nil {
			return nil
		}
		return r.registry.GetDeviceSubscribers(deviceID)

	case RouteUser:
		userID, err := uuid.Parse(env.TargetID)
		if err != nil {
			return nil
		}
		return r.registry.GetUserSessions(userID)

	case RouteBroadcast:
		return r.registry.GetAllSessions()

	default:
		return nil
	}
}

// broadcast sends a message to all connected dashboards
func (r *MessageRouter) broadcast(env *MessageEnvelope) DeliveryResult {
	start := time.Now()
	result := DeliveryResult{}

	sessions := r.registry.GetAllSessions()
	if len(sessions) == 0 {
		return result
	}

	data, err := env.Marshal()
	if err != nil {
		log.Printf("[Router] Failed to marshal broadcast message: %v", err)
		result.Failed = 1
		return result
	}

	for _, session := range sessions {
		if err := session.Send(data); err != nil {
			result.Failed++
			r.registry.IncrementDropped()
		} else {
			result.Sent++
			r.registry.IncrementMessages()
		}
	}

	result.Duration = time.Since(start)
	return result
}

// BroadcastRaw broadcasts a raw message to all dashboards (legacy compatibility)
func (r *MessageRouter) BroadcastRaw(data []byte) DeliveryResult {
	start := time.Now()
	result := DeliveryResult{}

	sessions := r.registry.GetAllSessions()
	for _, session := range sessions {
		if err := session.Send(data); err != nil {
			result.Failed++
			r.registry.IncrementDropped()
		} else {
			result.Sent++
			r.registry.IncrementMessages()
		}
	}

	result.Duration = time.Since(start)
	return result
}

// SendToSession sends a message to dashboards owning a specific session
func (r *MessageRouter) SendToSession(sessionID string, data []byte) DeliveryResult {
	start := time.Now()
	result := DeliveryResult{}

	sessions := r.registry.GetSessionOwners(sessionID)
	for _, session := range sessions {
		if err := session.Send(data); err != nil {
			result.Failed++
		} else {
			result.Sent++
		}
	}

	result.Duration = time.Since(start)
	return result
}

// SendToDevice sends a message to dashboards watching a specific device
func (r *MessageRouter) SendToDevice(deviceID uuid.UUID, data []byte) DeliveryResult {
	start := time.Now()
	result := DeliveryResult{}

	sessions := r.registry.GetDeviceSubscribers(deviceID)
	for _, session := range sessions {
		if err := session.Send(data); err != nil {
			result.Failed++
		} else {
			result.Sent++
		}
	}

	result.Duration = time.Since(start)
	return result
}

// SendToUser sends a message to all connections of a specific user
func (r *MessageRouter) SendToUser(userID uuid.UUID, data []byte) DeliveryResult {
	start := time.Now()
	result := DeliveryResult{}

	sessions := r.registry.GetUserSessions(userID)
	for _, session := range sessions {
		if err := session.Send(data); err != nil {
			result.Failed++
		} else {
			result.Sent++
		}
	}

	result.Duration = time.Since(start)
	return result
}

// SendToConnection sends a message to a specific connection
func (r *MessageRouter) SendToConnection(connID string, data []byte) error {
	session, ok := r.registry.GetSession(connID)
	if !ok {
		return ErrConnectionNotFound
	}
	return session.Send(data)
}

// RouteResponse routes a response back to the original requester
func (r *MessageRouter) RouteResponse(env *MessageEnvelope) bool {
	// First, try to route through pending request manager
	if r.pendingRequests != nil && r.pendingRequests.HandleResponse(env) {
		return true
	}

	// Fallback: route based on envelope metadata
	result := r.RouteMessage(env)
	return result.Sent > 0
}

// HandleAgentResponse processes a response from an agent and routes it appropriately
func (r *MessageRouter) HandleAgentResponse(agentID string, deviceID uuid.UUID, data []byte) {
	// Try to parse as envelope
	env, err := NormalizeMessage(data, agentID)
	if err != nil {
		log.Printf("[Router] Failed to parse agent response: %v", err)
		return
	}

	// Set device context
	env.DeviceID = deviceID.String()

	// Route the response
	r.RouteResponse(env)
}

// SendTerminalOutput routes terminal output to the session owner
func (r *MessageRouter) SendTerminalOutput(sessionID, deviceID, agentID, data string) {
	msg := map[string]interface{}{
		"type":      "terminal_output",
		"deviceId":  deviceID,
		"agentId":   agentID,
		"sessionId": sessionID,
		"data":      data,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[Router] Failed to marshal terminal output: %v", err)
		return
	}

	result := r.SendToSession(sessionID, msgBytes)
	if result.Sent == 0 && r.enableBroadcast {
		// Fallback to broadcast if no session owner found
		r.BroadcastRaw(msgBytes)
	}
}

// SendDeviceMetrics routes metrics to device watchers
func (r *MessageRouter) SendDeviceMetrics(deviceID uuid.UUID, metrics interface{}) {
	msg := map[string]interface{}{
		"type":     "device_metrics",
		"deviceId": deviceID.String(),
		"metrics":  metrics,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[Router] Failed to marshal device metrics: %v", err)
		return
	}

	result := r.SendToDevice(deviceID, msgBytes)
	if result.Sent == 0 && r.enableBroadcast {
		// Fallback to broadcast for backward compatibility
		r.BroadcastRaw(msgBytes)
	}
}

// SendDeviceStatus routes status changes to device watchers
func (r *MessageRouter) SendDeviceStatus(deviceID uuid.UUID, status string) {
	msg := map[string]interface{}{
		"type":     "device_status",
		"deviceId": deviceID.String(),
		"status":   status,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[Router] Failed to marshal device status: %v", err)
		return
	}

	result := r.SendToDevice(deviceID, msgBytes)
	if result.Sent == 0 && r.enableBroadcast {
		// Fallback to broadcast for backward compatibility
		r.BroadcastRaw(msgBytes)
	}
}

// GetMetrics returns router metrics
func (r *MessageRouter) GetMetrics() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	registryMetrics := r.registry.GetMetrics()

	return map[string]interface{}{
		"messagesRouted":    r.messagesRouted,
		"messagesSent":      r.messagesSent,
		"messagesFailed":    r.messagesFailed,
		"registry":          registryMetrics,
		"enableBroadcast":   r.enableBroadcast,
	}
}

// SetBroadcastFallback enables or disables broadcast fallback
func (r *MessageRouter) SetBroadcastFallback(enabled bool) {
	r.enableBroadcast = enabled
}

// Errors
var (
	ErrConnectionNotFound = &RouterError{Message: "connection not found"}
	ErrSendTimeout        = &RouterError{Message: "send timeout"}
	ErrBufferFull         = &RouterError{Message: "send buffer full"}
)

type RouterError struct {
	Message string
}

func (e *RouterError) Error() string {
	return e.Message
}
