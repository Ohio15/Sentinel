package messaging

import (
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SubscriptionType defines what kind of messages a dashboard wants to receive
type SubscriptionType string

const (
	SubDeviceMetrics SubscriptionType = "device_metrics"
	SubDeviceStatus  SubscriptionType = "device_status"
	SubAlerts        SubscriptionType = "alerts"
	SubTerminal      SubscriptionType = "terminal"
	SubRDP           SubscriptionType = "rdp"
	SubFiles         SubscriptionType = "files"
	SubBroadcast     SubscriptionType = "broadcast" // Receives all messages (legacy mode)
)

// DashboardConnection represents a connected dashboard client
type DashboardConnection interface {
	// Send sends a message to the dashboard
	Send(message []byte) error
	// GetConnectionID returns the unique connection identifier
	GetConnectionID() string
	// IsConnected returns true if the connection is still active
	IsConnected() bool
	// Close closes the connection
	Close() error
}

// Subscription represents a dashboard's subscription to specific events
type Subscription struct {
	Type           SubscriptionType
	DeviceID       uuid.UUID // Which device (zero UUID means all devices)
	SessionID      string    // For session-specific subscriptions
	CreatedAt      time.Time
	LastActivityAt time.Time
}

// DashboardSession tracks a connected dashboard and its subscriptions
type DashboardSession struct {
	UserID         uuid.UUID
	OrganizationID int
	ConnectionID   string
	Connection     DashboardConnection
	Subscriptions  map[string]*Subscription // key = "type:deviceId:sessionId"
	ConnectedAt    time.Time
	LastActivityAt time.Time
	mu             sync.RWMutex
}

// NewDashboardSession creates a new dashboard session
func NewDashboardSession(userID uuid.UUID, orgID int, conn DashboardConnection) *DashboardSession {
	return &DashboardSession{
		UserID:         userID,
		OrganizationID: orgID,
		ConnectionID:   conn.GetConnectionID(),
		Connection:     conn,
		Subscriptions:  make(map[string]*Subscription),
		ConnectedAt:    time.Now(),
		LastActivityAt: time.Now(),
	}
}

// Subscribe adds a subscription for the dashboard
func (s *DashboardSession) Subscribe(subType SubscriptionType, deviceID uuid.UUID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := makeSubscriptionKey(subType, deviceID, sessionID)
	s.Subscriptions[key] = &Subscription{
		Type:           subType,
		DeviceID:       deviceID,
		SessionID:      sessionID,
		CreatedAt:      time.Now(),
		LastActivityAt: time.Now(),
	}
	s.LastActivityAt = time.Now()
}

// Unsubscribe removes a subscription
func (s *DashboardSession) Unsubscribe(subType SubscriptionType, deviceID uuid.UUID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := makeSubscriptionKey(subType, deviceID, sessionID)
	delete(s.Subscriptions, key)
	s.LastActivityAt = time.Now()
}

// HasSubscription checks if the dashboard has a specific subscription
func (s *DashboardSession) HasSubscription(subType SubscriptionType, deviceID uuid.UUID, sessionID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check exact match
	key := makeSubscriptionKey(subType, deviceID, sessionID)
	if _, ok := s.Subscriptions[key]; ok {
		return true
	}

	// Check wildcard device subscription (subscribed to all devices of this type)
	wildcardKey := makeSubscriptionKey(subType, uuid.UUID{}, "")
	if _, ok := s.Subscriptions[wildcardKey]; ok {
		return true
	}

	// Check broadcast subscription (receives everything)
	broadcastKey := makeSubscriptionKey(SubBroadcast, uuid.UUID{}, "")
	if _, ok := s.Subscriptions[broadcastKey]; ok {
		return true
	}

	return false
}

// Send sends a message to the dashboard connection
func (s *DashboardSession) Send(message []byte) error {
	s.mu.Lock()
	s.LastActivityAt = time.Now()
	s.mu.Unlock()

	return s.Connection.Send(message)
}

func makeSubscriptionKey(subType SubscriptionType, deviceID uuid.UUID, sessionID string) string {
	return string(subType) + ":" + deviceID.String() + ":" + sessionID
}

// DashboardRegistry manages all connected dashboards and their subscriptions.
// It enables targeted message routing instead of broadcast.
type DashboardRegistry struct {
	mu sync.RWMutex

	// Primary index: connectionID -> session
	byConnection map[string]*DashboardSession

	// Secondary indexes for efficient lookup
	byUser   map[uuid.UUID]map[string]*DashboardSession // userID -> connectionID -> session
	byDevice map[uuid.UUID]map[string]*DashboardSession // deviceID -> connectionID -> session (subscribed dashboards)
	bySession map[string]map[string]*DashboardSession   // sessionID -> connectionID -> session (terminal/RDP)

	// Metrics
	totalConnections int64
	totalMessages    int64
	totalDropped     int64
}

// NewDashboardRegistry creates a new dashboard registry
func NewDashboardRegistry() *DashboardRegistry {
	return &DashboardRegistry{
		byConnection: make(map[string]*DashboardSession),
		byUser:       make(map[uuid.UUID]map[string]*DashboardSession),
		byDevice:     make(map[uuid.UUID]map[string]*DashboardSession),
		bySession:    make(map[string]map[string]*DashboardSession),
	}
}

// Register adds a new dashboard connection to the registry
func (r *DashboardRegistry) Register(userID uuid.UUID, orgID int, conn DashboardConnection) *DashboardSession {
	session := NewDashboardSession(userID, orgID, conn)

	r.mu.Lock()
	defer r.mu.Unlock()

	connID := conn.GetConnectionID()

	// Add to primary index
	r.byConnection[connID] = session

	// Add to user index
	if r.byUser[userID] == nil {
		r.byUser[userID] = make(map[string]*DashboardSession)
	}
	r.byUser[userID][connID] = session

	r.totalConnections++

	log.Printf("[DashboardRegistry] Registered dashboard: user=%s conn=%s (total: %d)",
		userID, connID, len(r.byConnection))

	return session
}

// Unregister removes a dashboard connection from the registry
func (r *DashboardRegistry) Unregister(connID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, exists := r.byConnection[connID]
	if !exists {
		return
	}

	// Remove from user index
	if userSessions, ok := r.byUser[session.UserID]; ok {
		delete(userSessions, connID)
		if len(userSessions) == 0 {
			delete(r.byUser, session.UserID)
		}
	}

	// Remove from device indexes
	session.mu.RLock()
	for _, sub := range session.Subscriptions {
		if sub.DeviceID != (uuid.UUID{}) {
			if deviceSessions, ok := r.byDevice[sub.DeviceID]; ok {
				delete(deviceSessions, connID)
				if len(deviceSessions) == 0 {
					delete(r.byDevice, sub.DeviceID)
				}
			}
		}
		if sub.SessionID != "" {
			if sessionSubs, ok := r.bySession[sub.SessionID]; ok {
				delete(sessionSubs, connID)
				if len(sessionSubs) == 0 {
					delete(r.bySession, sub.SessionID)
				}
			}
		}
	}
	session.mu.RUnlock()

	// Remove from primary index
	delete(r.byConnection, connID)

	log.Printf("[DashboardRegistry] Unregistered dashboard: user=%s conn=%s (remaining: %d)",
		session.UserID, connID, len(r.byConnection))
}

// Subscribe adds a subscription for a dashboard
func (r *DashboardRegistry) Subscribe(connID string, subType SubscriptionType, deviceID uuid.UUID, sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, exists := r.byConnection[connID]
	if !exists {
		return false
	}

	session.Subscribe(subType, deviceID, sessionID)

	// Update device index
	if deviceID != (uuid.UUID{}) {
		if r.byDevice[deviceID] == nil {
			r.byDevice[deviceID] = make(map[string]*DashboardSession)
		}
		r.byDevice[deviceID][connID] = session
	}

	// Update session index
	if sessionID != "" {
		if r.bySession[sessionID] == nil {
			r.bySession[sessionID] = make(map[string]*DashboardSession)
		}
		r.bySession[sessionID][connID] = session
	}

	log.Printf("[DashboardRegistry] Subscription added: conn=%s type=%s device=%s session=%s",
		connID, subType, deviceID, sessionID)

	return true
}

// Unsubscribe removes a subscription from a dashboard
func (r *DashboardRegistry) Unsubscribe(connID string, subType SubscriptionType, deviceID uuid.UUID, sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, exists := r.byConnection[connID]
	if !exists {
		return false
	}

	session.Unsubscribe(subType, deviceID, sessionID)

	// Update device index
	if deviceID != (uuid.UUID{}) {
		// Check if session still has any subscriptions for this device
		hasDeviceSub := false
		session.mu.RLock()
		for _, sub := range session.Subscriptions {
			if sub.DeviceID == deviceID {
				hasDeviceSub = true
				break
			}
		}
		session.mu.RUnlock()

		if !hasDeviceSub {
			if deviceSessions, ok := r.byDevice[deviceID]; ok {
				delete(deviceSessions, connID)
				if len(deviceSessions) == 0 {
					delete(r.byDevice, deviceID)
				}
			}
		}
	}

	// Update session index
	if sessionID != "" {
		if sessionSubs, ok := r.bySession[sessionID]; ok {
			delete(sessionSubs, connID)
			if len(sessionSubs) == 0 {
				delete(r.bySession, sessionID)
			}
		}
	}

	return true
}

// GetSession returns the dashboard session for a connection ID
func (r *DashboardRegistry) GetSession(connID string) (*DashboardSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session, ok := r.byConnection[connID]
	return session, ok
}

// GetUserSessions returns all sessions for a user
func (r *DashboardRegistry) GetUserSessions(userID uuid.UUID) []*DashboardSession {
	r.mu.RLock()
	defer r.mu.RUnlock()

	userSessions, ok := r.byUser[userID]
	if !ok {
		return nil
	}

	sessions := make([]*DashboardSession, 0, len(userSessions))
	for _, s := range userSessions {
		sessions = append(sessions, s)
	}
	return sessions
}

// GetDeviceSubscribers returns all dashboards subscribed to a device
func (r *DashboardRegistry) GetDeviceSubscribers(deviceID uuid.UUID) []*DashboardSession {
	r.mu.RLock()
	defer r.mu.RUnlock()

	deviceSessions, ok := r.byDevice[deviceID]
	if !ok {
		return nil
	}

	sessions := make([]*DashboardSession, 0, len(deviceSessions))
	for _, s := range deviceSessions {
		sessions = append(sessions, s)
	}
	return sessions
}

// GetSessionOwners returns dashboards that own a specific terminal/RDP session
func (r *DashboardRegistry) GetSessionOwners(sessionID string) []*DashboardSession {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sessionSubs, ok := r.bySession[sessionID]
	if !ok {
		return nil
	}

	sessions := make([]*DashboardSession, 0, len(sessionSubs))
	for _, s := range sessionSubs {
		sessions = append(sessions, s)
	}
	return sessions
}

// GetAllSessions returns all connected dashboard sessions (for broadcast)
func (r *DashboardRegistry) GetAllSessions() []*DashboardSession {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sessions := make([]*DashboardSession, 0, len(r.byConnection))
	for _, s := range r.byConnection {
		sessions = append(sessions, s)
	}
	return sessions
}

// GetConnectedCount returns the number of connected dashboards
func (r *DashboardRegistry) GetConnectedCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byConnection)
}

// GetMetrics returns registry metrics
func (r *DashboardRegistry) GetMetrics() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return map[string]interface{}{
		"connected":        len(r.byConnection),
		"uniqueUsers":      len(r.byUser),
		"deviceWatchers":   len(r.byDevice),
		"sessionOwners":    len(r.bySession),
		"totalConnections": r.totalConnections,
		"totalMessages":    r.totalMessages,
		"totalDropped":     r.totalDropped,
	}
}

// IncrementMessages increments the message counter
func (r *DashboardRegistry) IncrementMessages() {
	r.mu.Lock()
	r.totalMessages++
	r.mu.Unlock()
}

// IncrementDropped increments the dropped message counter
func (r *DashboardRegistry) IncrementDropped() {
	r.mu.Lock()
	r.totalDropped++
	r.mu.Unlock()
}
