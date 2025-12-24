package websocket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func setupTestHub() *Hub {
	// Use in-memory hub for testing (no Redis dependency)
	return NewHub(nil)
}

func TestHubCreation(t *testing.T) {
	hub := setupTestHub()

	if hub == nil {
		t.Fatal("Hub should not be nil")
	}

	if hub.agents == nil {
		t.Error("Hub agents map should be initialized")
	}

	if hub.dashboards == nil {
		t.Error("Hub dashboards map should be initialized")
	}

	if hub.register == nil {
		t.Error("Hub register channel should be initialized")
	}

	if hub.unregister == nil {
		t.Error("Hub unregister channel should be initialized")
	}

	if hub.broadcast == nil {
		t.Error("Hub broadcast channel should be initialized")
	}
}

func TestRegisterAgent(t *testing.T) {
	hub := setupTestHub()
	go hub.Run()
	defer close(hub.register)

	agentID := "test-agent-123"
	deviceID := uuid.New()

	// Create mock connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
		}
		defer conn.Close()

		client := hub.RegisterAgent(conn, agentID, deviceID)
		if client == nil {
			t.Error("RegisterAgent returned nil")
		}

		// Wait for registration to complete
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial websocket: %v", err)
	}
	defer conn.Close()

	// Wait for registration
	time.Sleep(200 * time.Millisecond)

	if !hub.IsAgentOnline(agentID) {
		t.Error("Agent should be online after registration")
	}

	onlineAgents := hub.GetOnlineAgents()
	if len(onlineAgents) != 1 {
		t.Errorf("Expected 1 online agent, got %d", len(onlineAgents))
	}
}

func TestUnregisterAgent(t *testing.T) {
	hub := setupTestHub()
	go hub.Run()
	defer close(hub.register)

	agentID := "test-agent-456"
	deviceID := uuid.New()

	// Create and close connection quickly to trigger unregister
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
		}

		client := hub.RegisterAgent(conn, agentID, deviceID)
		time.Sleep(100 * time.Millisecond)

		// Unregister by closing
		hub.unregister <- client
		conn.Close()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial websocket: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	conn.Close()

	time.Sleep(200 * time.Millisecond)

	if hub.IsAgentOnline(agentID) {
		t.Error("Agent should be offline after unregistration")
	}
}

func TestSendToAgent_AgentOnline(t *testing.T) {
	hub := setupTestHub()
	go hub.Run()
	defer close(hub.register)

	agentID := "test-agent-789"
	deviceID := uuid.New()
	messageReceived := make(chan bool, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
		}
		defer conn.Close()

		client := hub.RegisterAgent(conn, agentID, deviceID)

		// Start write pump to handle messages
		go func() {
			for msg := range client.send {
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}
			}
		}()

		// Wait for message
		time.Sleep(100 * time.Millisecond)

		_, msg, err := conn.ReadMessage()
		if err == nil {
			var parsed Message
			if json.Unmarshal(msg, &parsed) == nil {
				if parsed.Type == "test_message" {
					messageReceived <- true
				}
			}
		}

		time.Sleep(500 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial websocket: %v", err)
	}
	defer conn.Close()

	time.Sleep(200 * time.Millisecond)

	// Send message to agent
	testMsg := Message{
		Type:      "test_message",
		Timestamp: time.Now(),
	}
	msgBytes, _ := json.Marshal(testMsg)

	err = hub.SendToAgent(agentID, msgBytes)
	if err != nil {
		t.Errorf("Failed to send message to agent: %v", err)
	}

	// Wait for confirmation
	select {
	case <-messageReceived:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Message was not received by agent")
	}
}

func TestSendToAgent_AgentOffline(t *testing.T) {
	hub := setupTestHub()
	go hub.Run()
	defer close(hub.register)

	agentID := "offline-agent"

	testMsg := Message{
		Type:      "test_message",
		Timestamp: time.Now(),
	}
	msgBytes, _ := json.Marshal(testMsg)

	err := hub.SendToAgent(agentID, msgBytes)
	if err == nil {
		t.Error("Expected error when sending to offline agent")
	}

	if err != ErrAgentNotConnected {
		t.Errorf("Expected ErrAgentNotConnected, got %v", err)
	}
}

func TestBroadcastToDashboards(t *testing.T) {
	hub := setupTestHub()
	go hub.Run()
	defer close(hub.register)

	userID := uuid.New()
	messageReceived := make(chan bool, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
		}
		defer conn.Close()

		client := hub.RegisterDashboard(conn, userID)

		// Start write pump
		go func() {
			for msg := range client.send {
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}
			}
		}()

		time.Sleep(100 * time.Millisecond)

		// Read broadcast message
		_, msg, err := conn.ReadMessage()
		if err == nil {
			var parsed map[string]interface{}
			if json.Unmarshal(msg, &parsed) == nil {
				if parsed["type"] == "broadcast_test" {
					messageReceived <- true
				}
			}
		}

		time.Sleep(500 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial websocket: %v", err)
	}
	defer conn.Close()

	time.Sleep(200 * time.Millisecond)

	// Broadcast message
	broadcastMsg := map[string]interface{}{
		"type":    "broadcast_test",
		"message": "test broadcast",
	}
	msgBytes, _ := json.Marshal(broadcastMsg)

	hub.BroadcastToDashboards(msgBytes)

	select {
	case <-messageReceived:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Broadcast message was not received by dashboard")
	}
}

func TestMessageValidation(t *testing.T) {
	tests := []struct {
		name      string
		msgType   string
		payload   interface{}
		shouldErr bool
	}{
		{"Valid heartbeat", MsgTypeHeartbeat, nil, false},
		{"Valid metrics", MsgTypeMetrics, map[string]interface{}{"cpu": 50}, false},
		{"Valid command", MsgTypeCommand, map[string]string{"cmd": "ls"}, false},
		{"Empty type", "", nil, false},
		{"Unknown type", "unknown_type", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payloadBytes, _ := json.Marshal(tt.payload)
			msg := Message{
				Type:      tt.msgType,
				Timestamp: time.Now(),
				Payload:   payloadBytes,
			}

			msgBytes, err := json.Marshal(msg)
			if err != nil && !tt.shouldErr {
				t.Errorf("Failed to marshal message: %v", err)
			}

			// Try to unmarshal back
			var parsed Message
			err = json.Unmarshal(msgBytes, &parsed)
			if err != nil && !tt.shouldErr {
				t.Errorf("Failed to unmarshal message: %v", err)
			}

			if !tt.shouldErr && parsed.Type != tt.msgType {
				t.Errorf("Expected type %s, got %s", tt.msgType, parsed.Type)
			}
		})
	}
}

func TestConcurrentAgentConnections(t *testing.T) {
	hub := setupTestHub()
	go hub.Run()
	defer close(hub.register)

	numAgents := 10
	agentIDs := make([]string, numAgents)
	for i := 0; i < numAgents; i++ {
		agentIDs[i] = uuid.New().String()
	}

	done := make(chan bool, numAgents)

	// Create multiple concurrent connections
	for i := 0; i < numAgents; i++ {
		go func(idx int) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Errorf("Failed to upgrade connection: %v", err)
					return
				}
				defer conn.Close()

				deviceID := uuid.New()
				hub.RegisterAgent(conn, agentIDs[idx], deviceID)
				time.Sleep(500 * time.Millisecond)
			}))
			defer server.Close()

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Errorf("Failed to dial websocket: %v", err)
				return
			}
			defer conn.Close()

			time.Sleep(600 * time.Millisecond)
			done <- true
		}(i)
	}

	// Wait for all connections
	timeout := time.After(5 * time.Second)
	count := 0
	for count < numAgents {
		select {
		case <-done:
			count++
		case <-timeout:
			t.Fatalf("Timeout waiting for concurrent connections, got %d/%d", count, numAgents)
		}
	}

	time.Sleep(200 * time.Millisecond)

	onlineAgents := hub.GetOnlineAgents()
	if len(onlineAgents) != numAgents {
		t.Errorf("Expected %d online agents, got %d", numAgents, len(onlineAgents))
	}
}

func TestMessageTypes(t *testing.T) {
	expectedTypes := []string{
		MsgTypeAuth,
		MsgTypeAuthResponse,
		MsgTypeHeartbeat,
		MsgTypeHeartbeatAck,
		MsgTypePing,
		MsgTypePong,
		MsgTypeMetrics,
		MsgTypeCommand,
		MsgTypeScript,
		MsgTypeResponse,
		MsgTypeTerminalStart,
		MsgTypeTerminalInput,
		MsgTypeTerminalOutput,
		MsgTypeTerminalResize,
		MsgTypeTerminalClose,
		MsgTypeListFiles,
		MsgTypeFileContent,
		MsgTypeDownloadFile,
		MsgTypeUploadFile,
		MsgTypeListDrives,
		MsgTypeScanDirectory,
		MsgTypeScanProgress,
		MsgTypeSetMetricsInterval,
		MsgTypeStartRemote,
		MsgTypeStopRemote,
		MsgTypeRemoteInput,
		MsgTypeRemoteFrame,
		MsgTypeUninstallAgent,
		MsgTypeCheckUpdate,
		MsgTypeUpdateAvailable,
		MsgTypeUpdateProgress,
	}

	for _, msgType := range expectedTypes {
		if msgType == "" {
			t.Errorf("Message type should not be empty")
		}
	}
}

func TestMaxMessageSize(t *testing.T) {
	hub := setupTestHub()
	go hub.Run()
	defer close(hub.register)

	// Verify max message size constant
	if maxMessageSize != 512*1024 {
		t.Errorf("Expected maxMessageSize to be 512KB, got %d", maxMessageSize)
	}

	// Test message larger than max size
	largePayload := make([]byte, maxMessageSize+1)
	for i := range largePayload {
		largePayload[i] = 'A'
	}

	msg := Message{
		Type:    MsgTypeMetrics,
		Payload: json.RawMessage(largePayload),
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal large message: %v", err)
	}

	// Message should be larger than max
	if len(msgBytes) <= maxMessageSize {
		t.Errorf("Test message should be larger than maxMessageSize")
	}
}

func TestPingPongTimeout(t *testing.T) {
	// Verify timeout constants are reasonable
	if writeWait != 10*time.Second {
		t.Errorf("Expected writeWait to be 10s, got %v", writeWait)
	}

	if pongWait != 60*time.Second {
		t.Errorf("Expected pongWait to be 60s, got %v", pongWait)
	}

	if pingPeriod != (pongWait*9)/10 {
		t.Errorf("Expected pingPeriod to be 9/10 of pongWait, got %v", pingPeriod)
	}
}
