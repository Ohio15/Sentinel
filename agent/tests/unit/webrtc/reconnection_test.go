package webrtc_test

import (
	"sync"
	"testing"
	"time"

	"github.com/sentinel/agent/internal/webrtc"
)

func TestDefaultReconnectionConfig(t *testing.T) {
	config := webrtc.DefaultReconnectionConfig()

	if config.InitialDelay != 1*time.Second {
		t.Errorf("Expected InitialDelay 1s, got %v", config.InitialDelay)
	}

	if config.MaxDelay != 30*time.Second {
		t.Errorf("Expected MaxDelay 30s, got %v", config.MaxDelay)
	}

	if config.Multiplier != 2.0 {
		t.Errorf("Expected Multiplier 2.0, got %f", config.Multiplier)
	}

	if config.MaxAttempts != 10 {
		t.Errorf("Expected MaxAttempts 10, got %d", config.MaxAttempts)
	}

	if config.ConnectionTimeout != 30*time.Second {
		t.Errorf("Expected ConnectionTimeout 30s, got %v", config.ConnectionTimeout)
	}

	if !config.PreserveSession {
		t.Error("Expected PreserveSession to be true")
	}

	if !config.AttemptICERestart {
		t.Error("Expected AttemptICERestart to be true")
	}
}

func TestReconnectionManager_StateTransitions(t *testing.T) {
	config := webrtc.DefaultReconnectionConfig()
	manager := webrtc.NewReconnectionManager(config)

	// Initial state
	if manager.GetState() != webrtc.StateDisconnected {
		t.Errorf("Expected initial state %s, got %s", webrtc.StateDisconnected, manager.GetState())
	}

	// Track state changes
	var states []webrtc.ConnectionState
	var mu sync.Mutex

	manager.SetCallbacks(
		func(state webrtc.ConnectionState) {
			mu.Lock()
			states = append(states, state)
			mu.Unlock()
		},
		nil,
		nil,
	)

	manager.Start()
	defer manager.Stop()

	// Notify connected
	manager.NotifyConnected()

	time.Sleep(50 * time.Millisecond)

	if manager.GetState() != webrtc.StateConnected {
		t.Errorf("Expected state %s, got %s", webrtc.StateConnected, manager.GetState())
	}

	mu.Lock()
	if len(states) == 0 || states[len(states)-1] != webrtc.StateConnected {
		t.Error("Expected StateConnected in state history")
	}
	mu.Unlock()
}

func TestReconnectionManager_AttemptCounter(t *testing.T) {
	config := webrtc.DefaultReconnectionConfig()
	config.InitialDelay = 10 * time.Millisecond
	config.MaxAttempts = 3
	manager := webrtc.NewReconnectionManager(config)

	// Track reconnection attempts
	var attempts int
	var mu sync.Mutex

	manager.SetCallbacks(
		nil,
		func() error {
			mu.Lock()
			attempts++
			mu.Unlock()
			return nil // Simulate successful reconnect
		},
		nil,
	)

	manager.Start()
	defer manager.Stop()

	// Notify connected first
	manager.NotifyConnected()
	time.Sleep(20 * time.Millisecond)

	// Initial attempts should be 0
	if manager.GetAttempts() != 0 {
		t.Errorf("Expected 0 attempts, got %d", manager.GetAttempts())
	}

	// Reset attempts
	manager.ResetAttempts()
	if manager.GetAttempts() != 0 {
		t.Errorf("Expected 0 attempts after reset, got %d", manager.GetAttempts())
	}
}

func TestReconnectionManager_QualityUpdates(t *testing.T) {
	config := webrtc.DefaultReconnectionConfig()
	manager := webrtc.NewReconnectionManager(config)

	var quality webrtc.ConnectionQuality
	var received bool

	manager.SetQualityCallback(func(q webrtc.ConnectionQuality) {
		quality = q
		received = true
	})

	manager.Start()
	defer manager.Stop()

	// Update quality
	manager.UpdateQuality(50*time.Millisecond, 0.5, 5*time.Millisecond)

	time.Sleep(20 * time.Millisecond)

	if !received {
		t.Error("Expected quality callback to be called")
	}

	if quality.RTT != 50*time.Millisecond {
		t.Errorf("Expected RTT 50ms, got %v", quality.RTT)
	}

	if quality.PacketLoss != 0.5 {
		t.Errorf("Expected PacketLoss 0.5, got %f", quality.PacketLoss)
	}

	if quality.Jitter != 5*time.Millisecond {
		t.Errorf("Expected Jitter 5ms, got %v", quality.Jitter)
	}

	// Check quality level
	q := manager.GetQuality()
	if q.QualityLevel == "" {
		t.Error("Expected non-empty quality level")
	}
}

func TestReconnectionManager_QualityLevels(t *testing.T) {
	config := webrtc.DefaultReconnectionConfig()
	manager := webrtc.NewReconnectionManager(config)
	manager.Start()
	defer manager.Stop()

	tests := []struct {
		rtt       time.Duration
		loss      float64
		expected  string
	}{
		{20 * time.Millisecond, 0.05, "excellent"},
		{80 * time.Millisecond, 0.5, "good"},
		{150 * time.Millisecond, 3.0, "fair"},
		{300 * time.Millisecond, 10.0, "poor"},
	}

	for _, tc := range tests {
		manager.UpdateQuality(tc.rtt, tc.loss, 0)
		q := manager.GetQuality()
		if q.QualityLevel != tc.expected {
			t.Errorf("RTT=%v, Loss=%.1f: expected %s, got %s", tc.rtt, tc.loss, tc.expected, q.QualityLevel)
		}
	}
}

func TestReconnectionManager_SessionState(t *testing.T) {
	config := webrtc.DefaultReconnectionConfig()
	manager := webrtc.NewReconnectionManager(config)

	session := &webrtc.SessionState{
		SessionID:     "test-session",
		AgentID:       "test-agent",
		StartTime:     time.Now(),
		MonitorIndex:  1,
		AudioEnabled:  true,
		ClipboardSync: true,
	}

	manager.SaveSession(session)

	retrieved := manager.GetSession()
	if retrieved == nil {
		t.Fatal("Expected non-nil session")
	}

	if retrieved.SessionID != "test-session" {
		t.Errorf("Expected SessionID 'test-session', got '%s'", retrieved.SessionID)
	}

	if retrieved.MonitorIndex != 1 {
		t.Errorf("Expected MonitorIndex 1, got %d", retrieved.MonitorIndex)
	}

	if !retrieved.AudioEnabled {
		t.Error("Expected AudioEnabled to be true")
	}
}

func TestReconnectionManager_EventCallback(t *testing.T) {
	config := webrtc.DefaultReconnectionConfig()
	manager := webrtc.NewReconnectionManager(config)

	var events []webrtc.ReconnectionEvent
	var mu sync.Mutex

	manager.SetEventCallback(func(event webrtc.ReconnectionEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})

	manager.Start()
	defer manager.Stop()

	// Trigger events
	manager.NotifyConnected()
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	connectedFound := false
	for _, e := range events {
		if e.Type == "connected" && e.State == webrtc.StateConnected {
			connectedFound = true
			break
		}
	}
	mu.Unlock()

	if !connectedFound {
		t.Error("Expected 'connected' event")
	}
}

func TestReconnectionManager_StatsUpdate(t *testing.T) {
	config := webrtc.DefaultReconnectionConfig()
	manager := webrtc.NewReconnectionManager(config)

	manager.UpdateStats(1000, 500, 30, 30, 2)

	quality := manager.GetQuality()

	if quality.BytesSent != 1000 {
		t.Errorf("Expected BytesSent 1000, got %d", quality.BytesSent)
	}

	if quality.BytesReceived != 500 {
		t.Errorf("Expected BytesReceived 500, got %d", quality.BytesReceived)
	}

	if quality.FramesEncoded != 30 {
		t.Errorf("Expected FramesEncoded 30, got %d", quality.FramesEncoded)
	}

	if quality.FramesDropped != 2 {
		t.Errorf("Expected FramesDropped 2, got %d", quality.FramesDropped)
	}
}

func TestReconnectionManager_Pong(t *testing.T) {
	config := webrtc.DefaultReconnectionConfig()
	manager := webrtc.NewReconnectionManager(config)

	before := time.Now()
	time.Sleep(10 * time.Millisecond)
	manager.RecordPong()

	// Pong should update lastPong time
	// We can't directly access lastPong, but we can verify keepalive doesn't trigger disconnect
	manager.NotifyConnected()
	time.Sleep(20 * time.Millisecond)

	if manager.GetState() != webrtc.StateConnected {
		t.Errorf("Expected state %s after pong, got %s", webrtc.StateConnected, manager.GetState())
	}

	_ = before // Used for timing reference
}

func TestConnectionState(t *testing.T) {
	states := []webrtc.ConnectionState{
		webrtc.StateDisconnected,
		webrtc.StateConnecting,
		webrtc.StateConnected,
		webrtc.StateReconnecting,
		webrtc.StateFailed,
	}

	// Verify all states are unique
	seen := make(map[webrtc.ConnectionState]bool)
	for _, s := range states {
		if s == "" {
			t.Error("Expected non-empty state")
		}
		if seen[s] {
			t.Errorf("Duplicate state: %s", s)
		}
		seen[s] = true
	}
}

func TestReconnectionManager_Stop(t *testing.T) {
	config := webrtc.DefaultReconnectionConfig()
	manager := webrtc.NewReconnectionManager(config)

	manager.Start()

	// Should not panic on stop
	manager.Stop()

	// Should not panic on double stop
	manager.Stop()
}
