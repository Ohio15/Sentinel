package webrtc

import (
	"context"
	"log"
	"math"
	"math/rand"
	"sync"
	"time"
)

// ConnectionState represents the current connection state
type ConnectionState string

const (
	StateDisconnected ConnectionState = "disconnected"
	StateConnecting   ConnectionState = "connecting"
	StateConnected    ConnectionState = "connected"
	StateReconnecting ConnectionState = "reconnecting"
	StateFailed       ConnectionState = "failed"
)

// ReconnectionConfig holds reconnection settings
type ReconnectionConfig struct {
	// Backoff settings
	InitialDelay    time.Duration `json:"initialDelay"`    // First retry delay
	MaxDelay        time.Duration `json:"maxDelay"`        // Maximum backoff delay
	Multiplier      float64       `json:"multiplier"`      // Backoff multiplier
	JitterFactor    float64       `json:"jitterFactor"`    // Random jitter (0-1)
	MaxAttempts     int           `json:"maxAttempts"`     // Max reconnection attempts (0 = unlimited)

	// Connection settings
	ConnectionTimeout time.Duration `json:"connectionTimeout"` // Timeout for establishing connection
	ICETimeout        time.Duration `json:"iceTimeout"`        // Timeout for ICE gathering
	KeepAliveInterval time.Duration `json:"keepAliveInterval"` // Keepalive ping interval

	// Recovery settings
	PreserveSession   bool `json:"preserveSession"`   // Try to preserve session state
	AttemptICERestart bool `json:"attemptICERestart"` // Try ICE restart before full reconnect
}

// DefaultReconnectionConfig returns default reconnection settings
func DefaultReconnectionConfig() ReconnectionConfig {
	return ReconnectionConfig{
		InitialDelay:      1 * time.Second,
		MaxDelay:          30 * time.Second,
		Multiplier:        2.0,
		JitterFactor:      0.2,
		MaxAttempts:       10,
		ConnectionTimeout: 30 * time.Second,
		ICETimeout:        10 * time.Second,
		KeepAliveInterval: 5 * time.Second,
		PreserveSession:   true,
		AttemptICERestart: true,
	}
}

// ConnectionQuality represents connection quality metrics
type ConnectionQuality struct {
	RTT            time.Duration `json:"rtt"`            // Round-trip time
	PacketLoss     float64       `json:"packetLoss"`     // Packet loss percentage
	Jitter         time.Duration `json:"jitter"`         // Jitter
	BytesSent      uint64        `json:"bytesSent"`
	BytesReceived  uint64        `json:"bytesReceived"`
	FramesEncoded  uint64        `json:"framesEncoded"`
	FramesDecoded  uint64        `json:"framesDecoded"`
	FramesDropped  uint64        `json:"framesDropped"`
	QualityLevel   string        `json:"qualityLevel"`   // "excellent", "good", "fair", "poor"
	LastUpdate     time.Time     `json:"lastUpdate"`
}

// SessionState stores recoverable session state
type SessionState struct {
	SessionID       string            `json:"sessionId"`
	AgentID         string            `json:"agentId"`
	StartTime       time.Time         `json:"startTime"`
	MonitorIndex    int               `json:"monitorIndex"`
	AudioEnabled    bool              `json:"audioEnabled"`
	ClipboardSync   bool              `json:"clipboardSync"`
	QualitySettings map[string]interface{} `json:"qualitySettings"`
}

// ReconnectionEvent represents a reconnection-related event
type ReconnectionEvent struct {
	Type      string    `json:"type"`
	State     ConnectionState `json:"state"`
	Attempt   int       `json:"attempt,omitempty"`
	Delay     time.Duration `json:"delay,omitempty"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ReconnectionCallback is called for reconnection events
type ReconnectionCallback func(event ReconnectionEvent)

// ReconnectionManager handles automatic reconnection with exponential backoff
type ReconnectionManager struct {
	config   ReconnectionConfig
	state    ConnectionState
	attempts int
	quality  ConnectionQuality
	session  *SessionState

	// Callbacks
	onStateChange    func(state ConnectionState)
	onReconnect      func() error
	onICERestart     func() error
	onQualityUpdate  func(quality ConnectionQuality)
	onEvent          ReconnectionCallback

	// Control
	ctx        context.Context
	cancel     context.CancelFunc
	stopCh     chan struct{}
	reconnectCh chan struct{}

	// Keepalive
	lastPong   time.Time
	pingTicker *time.Ticker

	mu sync.RWMutex
}

// NewReconnectionManager creates a new reconnection manager
func NewReconnectionManager(config ReconnectionConfig) *ReconnectionManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &ReconnectionManager{
		config:      config,
		state:       StateDisconnected,
		ctx:         ctx,
		cancel:      cancel,
		stopCh:      make(chan struct{}),
		reconnectCh: make(chan struct{}, 1),
		lastPong:    time.Now(),
	}
}

// SetCallbacks configures the reconnection callbacks
func (rm *ReconnectionManager) SetCallbacks(
	onStateChange func(ConnectionState),
	onReconnect func() error,
	onICERestart func() error,
) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.onStateChange = onStateChange
	rm.onReconnect = onReconnect
	rm.onICERestart = onICERestart
}

// SetEventCallback sets the callback for reconnection events
func (rm *ReconnectionManager) SetEventCallback(callback ReconnectionCallback) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.onEvent = callback
}

// SetQualityCallback sets the callback for quality updates
func (rm *ReconnectionManager) SetQualityCallback(callback func(ConnectionQuality)) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.onQualityUpdate = callback
}

// Start begins the reconnection manager
func (rm *ReconnectionManager) Start() {
	go rm.reconnectionLoop()

	if rm.config.KeepAliveInterval > 0 {
		go rm.keepaliveLoop()
	}

	log.Printf("[ReconnectionManager] Started with config: initialDelay=%v, maxDelay=%v, maxAttempts=%d",
		rm.config.InitialDelay, rm.config.MaxDelay, rm.config.MaxAttempts)
}

// Stop stops the reconnection manager
func (rm *ReconnectionManager) Stop() {
	rm.cancel()
	close(rm.stopCh)

	if rm.pingTicker != nil {
		rm.pingTicker.Stop()
	}

	log.Printf("[ReconnectionManager] Stopped")
}

// NotifyConnected signals that connection is established
func (rm *ReconnectionManager) NotifyConnected() {
	rm.mu.Lock()
	rm.state = StateConnected
	rm.attempts = 0
	rm.lastPong = time.Now()
	rm.mu.Unlock()

	rm.emitEvent(ReconnectionEvent{
		Type:      "connected",
		State:     StateConnected,
		Timestamp: time.Now(),
	})

	rm.notifyStateChange(StateConnected)
}

// NotifyDisconnected signals that connection was lost
func (rm *ReconnectionManager) NotifyDisconnected(err error) {
	rm.mu.Lock()
	previousState := rm.state
	rm.state = StateDisconnected
	rm.mu.Unlock()

	errStr := ""
	if err != nil {
		errStr = err.Error()
	}

	rm.emitEvent(ReconnectionEvent{
		Type:      "disconnected",
		State:     StateDisconnected,
		Error:     errStr,
		Timestamp: time.Now(),
	})

	// Trigger reconnection if we were previously connected
	if previousState == StateConnected {
		rm.TriggerReconnect()
	}

	rm.notifyStateChange(StateDisconnected)
}

// NotifyICEFailure signals ICE connection failure
func (rm *ReconnectionManager) NotifyICEFailure() {
	rm.mu.Lock()
	rm.state = StateDisconnected
	rm.mu.Unlock()

	rm.emitEvent(ReconnectionEvent{
		Type:      "ice_failure",
		State:     StateDisconnected,
		Timestamp: time.Now(),
	})

	// Try ICE restart first
	if rm.config.AttemptICERestart {
		rm.attemptICERestart()
	} else {
		rm.TriggerReconnect()
	}
}

// TriggerReconnect initiates a reconnection attempt
func (rm *ReconnectionManager) TriggerReconnect() {
	select {
	case rm.reconnectCh <- struct{}{}:
	default:
		// Already triggered
	}
}

// UpdateQuality updates connection quality metrics
func (rm *ReconnectionManager) UpdateQuality(rtt time.Duration, packetLoss float64, jitter time.Duration) {
	rm.mu.Lock()
	rm.quality.RTT = rtt
	rm.quality.PacketLoss = packetLoss
	rm.quality.Jitter = jitter
	rm.quality.LastUpdate = time.Now()

	// Determine quality level
	switch {
	case rtt < 50*time.Millisecond && packetLoss < 0.1:
		rm.quality.QualityLevel = "excellent"
	case rtt < 100*time.Millisecond && packetLoss < 1.0:
		rm.quality.QualityLevel = "good"
	case rtt < 200*time.Millisecond && packetLoss < 5.0:
		rm.quality.QualityLevel = "fair"
	default:
		rm.quality.QualityLevel = "poor"
	}

	quality := rm.quality
	callback := rm.onQualityUpdate
	rm.mu.Unlock()

	if callback != nil {
		callback(quality)
	}
}

// UpdateStats updates connection statistics
func (rm *ReconnectionManager) UpdateStats(bytesSent, bytesReceived, framesEncoded, framesDecoded, framesDropped uint64) {
	rm.mu.Lock()
	rm.quality.BytesSent = bytesSent
	rm.quality.BytesReceived = bytesReceived
	rm.quality.FramesEncoded = framesEncoded
	rm.quality.FramesDecoded = framesDecoded
	rm.quality.FramesDropped = framesDropped
	rm.mu.Unlock()
}

// SaveSession saves the current session state for recovery
func (rm *ReconnectionManager) SaveSession(session *SessionState) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.session = session
}

// GetSession returns the saved session state
func (rm *ReconnectionManager) GetSession() *SessionState {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.session
}

// GetState returns the current connection state
func (rm *ReconnectionManager) GetState() ConnectionState {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.state
}

// GetQuality returns the current connection quality
func (rm *ReconnectionManager) GetQuality() ConnectionQuality {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.quality
}

// RecordPong records receipt of a pong response
func (rm *ReconnectionManager) RecordPong() {
	rm.mu.Lock()
	rm.lastPong = time.Now()
	rm.mu.Unlock()
}

// reconnectionLoop handles the reconnection state machine
func (rm *ReconnectionManager) reconnectionLoop() {
	for {
		select {
		case <-rm.stopCh:
			return
		case <-rm.reconnectCh:
			rm.performReconnection()
		}
	}
}

// performReconnection attempts to reconnect with exponential backoff
func (rm *ReconnectionManager) performReconnection() {
	rm.mu.Lock()
	rm.state = StateReconnecting
	rm.attempts = 0
	onReconnect := rm.onReconnect
	maxAttempts := rm.config.MaxAttempts
	rm.mu.Unlock()

	rm.notifyStateChange(StateReconnecting)

	for {
		select {
		case <-rm.stopCh:
			return
		default:
		}

		rm.mu.Lock()
		rm.attempts++
		attempt := rm.attempts
		rm.mu.Unlock()

		// Check max attempts
		if maxAttempts > 0 && attempt > maxAttempts {
			log.Printf("[ReconnectionManager] Max attempts (%d) reached, giving up", maxAttempts)
			rm.mu.Lock()
			rm.state = StateFailed
			rm.mu.Unlock()

			rm.emitEvent(ReconnectionEvent{
				Type:      "failed",
				State:     StateFailed,
				Attempt:   attempt,
				Error:     "max attempts reached",
				Timestamp: time.Now(),
			})

			rm.notifyStateChange(StateFailed)
			return
		}

		// Calculate backoff delay with jitter
		delay := rm.calculateBackoff(attempt)

		rm.emitEvent(ReconnectionEvent{
			Type:      "reconnecting",
			State:     StateReconnecting,
			Attempt:   attempt,
			Delay:     delay,
			Timestamp: time.Now(),
		})

		log.Printf("[ReconnectionManager] Reconnection attempt %d, waiting %v", attempt, delay)

		// Wait for backoff period
		select {
		case <-rm.stopCh:
			return
		case <-time.After(delay):
		}

		// Attempt reconnection
		if onReconnect != nil {
			err := onReconnect()
			if err == nil {
				log.Printf("[ReconnectionManager] Reconnection successful on attempt %d", attempt)
				rm.NotifyConnected()
				return
			}

			log.Printf("[ReconnectionManager] Reconnection attempt %d failed: %v", attempt, err)

			rm.emitEvent(ReconnectionEvent{
				Type:      "reconnect_failed",
				State:     StateReconnecting,
				Attempt:   attempt,
				Error:     err.Error(),
				Timestamp: time.Now(),
			})
		}
	}
}

// attemptICERestart tries to restart ICE connection
func (rm *ReconnectionManager) attemptICERestart() {
	rm.mu.RLock()
	onICERestart := rm.onICERestart
	rm.mu.RUnlock()

	if onICERestart == nil {
		rm.TriggerReconnect()
		return
	}

	rm.emitEvent(ReconnectionEvent{
		Type:      "ice_restart",
		State:     StateReconnecting,
		Timestamp: time.Now(),
	})

	log.Printf("[ReconnectionManager] Attempting ICE restart")

	err := onICERestart()
	if err != nil {
		log.Printf("[ReconnectionManager] ICE restart failed: %v, triggering full reconnect", err)
		rm.TriggerReconnect()
	} else {
		log.Printf("[ReconnectionManager] ICE restart initiated")
	}
}

// calculateBackoff calculates the backoff delay for a given attempt
func (rm *ReconnectionManager) calculateBackoff(attempt int) time.Duration {
	// Calculate base delay: initialDelay * multiplier^(attempt-1)
	delay := float64(rm.config.InitialDelay) * math.Pow(rm.config.Multiplier, float64(attempt-1))

	// Cap at max delay
	if delay > float64(rm.config.MaxDelay) {
		delay = float64(rm.config.MaxDelay)
	}

	// Add jitter
	if rm.config.JitterFactor > 0 {
		jitter := delay * rm.config.JitterFactor
		delay = delay - jitter + (rand.Float64() * 2 * jitter)
	}

	return time.Duration(delay)
}

// keepaliveLoop sends periodic keepalive pings
func (rm *ReconnectionManager) keepaliveLoop() {
	rm.pingTicker = time.NewTicker(rm.config.KeepAliveInterval)
	defer rm.pingTicker.Stop()

	for {
		select {
		case <-rm.stopCh:
			return
		case <-rm.pingTicker.C:
			rm.checkKeepalive()
		}
	}
}

// checkKeepalive checks if the connection is still alive
func (rm *ReconnectionManager) checkKeepalive() {
	rm.mu.RLock()
	state := rm.state
	lastPong := rm.lastPong
	rm.mu.RUnlock()

	if state != StateConnected {
		return
	}

	// Check if we've received a pong recently
	timeout := rm.config.KeepAliveInterval * 3
	if time.Since(lastPong) > timeout {
		log.Printf("[ReconnectionManager] Keepalive timeout, last pong was %v ago", time.Since(lastPong))
		rm.NotifyDisconnected(nil)
	}
}

// emitEvent sends a reconnection event to the callback
func (rm *ReconnectionManager) emitEvent(event ReconnectionEvent) {
	rm.mu.RLock()
	callback := rm.onEvent
	rm.mu.RUnlock()

	if callback != nil {
		callback(event)
	}
}

// notifyStateChange notifies the state change callback
func (rm *ReconnectionManager) notifyStateChange(state ConnectionState) {
	rm.mu.RLock()
	callback := rm.onStateChange
	rm.mu.RUnlock()

	if callback != nil {
		callback(state)
	}
}

// GetAttempts returns the current reconnection attempt count
func (rm *ReconnectionManager) GetAttempts() int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.attempts
}

// ResetAttempts resets the reconnection attempt counter
func (rm *ReconnectionManager) ResetAttempts() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.attempts = 0
}
