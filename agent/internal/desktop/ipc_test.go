// +build windows

package desktop

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockIPCHandler captures messages for testing
type mockIPCHandler struct {
	mu            sync.Mutex
	authCalls     []*AuthPayload
	heartbeatCalls []*HeartbeatPayload
	answerCalls   []*SessionAnswerPayload
	iceCalls      []*ICECandidatePayload
	statusCalls   []*StatusPayload
	disconnects   int

	server *IPCServer
}

func (h *mockIPCHandler) OnAuth(msg *IPCMessage, payload *AuthPayload) error {
	h.mu.Lock()
	h.authCalls = append(h.authCalls, payload)
	h.mu.Unlock()

	// Send auth OK response
	response, _ := NewIPCMessage(MsgTypeAuthOK, msg.RequestID, &AuthOKPayload{
		Capabilities: []string{"capture", "input"},
		ExpiresAt:    time.Now().Add(time.Minute),
	})
	return h.server.SendMessage(response)
}

func (h *mockIPCHandler) OnHeartbeat(msg *IPCMessage, payload *HeartbeatPayload) error {
	h.mu.Lock()
	h.heartbeatCalls = append(h.heartbeatCalls, payload)
	h.mu.Unlock()

	response, _ := NewIPCMessage(MsgTypeHeartbeatAck, msg.RequestID, &HeartbeatAckPayload{
		Timestamp: time.Now(),
		Continue:  true,
	})
	return h.server.SendMessage(response)
}

func (h *mockIPCHandler) OnSessionAnswer(msg *IPCMessage, payload *SessionAnswerPayload) error {
	h.mu.Lock()
	h.answerCalls = append(h.answerCalls, payload)
	h.mu.Unlock()
	return nil
}

func (h *mockIPCHandler) OnICECandidate(msg *IPCMessage, payload *ICECandidatePayload) error {
	h.mu.Lock()
	h.iceCalls = append(h.iceCalls, payload)
	h.mu.Unlock()
	return nil
}

func (h *mockIPCHandler) OnStatus(msg *IPCMessage, payload *StatusPayload) error {
	h.mu.Lock()
	h.statusCalls = append(h.statusCalls, payload)
	h.mu.Unlock()
	return nil
}

func (h *mockIPCHandler) OnDisconnect() {
	h.mu.Lock()
	h.disconnects++
	h.mu.Unlock()
}

func TestIPCServerClientCommunication(t *testing.T) {
	sessionID := uint32(12345)

	// Create mock handler
	handler := &mockIPCHandler{}

	// Create server
	server, err := NewIPCServer(sessionID, handler)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	handler.server = server
	defer server.Close()

	// Start server
	server.Start()

	// Create client
	client := NewIPCClient(sessionID, "test-token")
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Connect client
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Authenticate
	if err := client.Authenticate(); err != nil {
		t.Fatalf("Failed to authenticate: %v", err)
	}

	// Verify auth was received
	handler.mu.Lock()
	if len(handler.authCalls) != 1 {
		t.Errorf("Expected 1 auth call, got %d", len(handler.authCalls))
	}
	if handler.authCalls[0].Token != "test-token" {
		t.Errorf("Wrong token: %s", handler.authCalls[0].Token)
	}
	handler.mu.Unlock()

	// Send status
	if err := client.SendStatus(StateReady, "test status", "conn-1"); err != nil {
		t.Fatalf("Failed to send status: %v", err)
	}

	// Give time for message to be received
	time.Sleep(100 * time.Millisecond)

	// Verify status was received
	handler.mu.Lock()
	if len(handler.statusCalls) != 1 {
		t.Errorf("Expected 1 status call, got %d", len(handler.statusCalls))
	} else {
		if handler.statusCalls[0].State != StateReady {
			t.Errorf("Wrong state: %s", handler.statusCalls[0].State)
		}
		if handler.statusCalls[0].Message != "test status" {
			t.Errorf("Wrong message: %s", handler.statusCalls[0].Message)
		}
	}
	handler.mu.Unlock()

	// Send ICE candidate from server to client
	if err := server.SendICECandidate("conn-1", "candidate:test", "audio", nil); err != nil {
		t.Fatalf("Failed to send ICE candidate: %v", err)
	}

	// Give time for message to be processed
	time.Sleep(100 * time.Millisecond)

	t.Log("IPC communication test passed")
}

func TestIPCMessageCreation(t *testing.T) {
	// Test creating a start session message
	msg, err := NewIPCMessage(MsgTypeStartSession, "req-1", &StartSessionPayload{
		SDPType:      "offer",
		SDP:          "v=0\r\n...",
		ConnectionID: "conn-123",
	})
	if err != nil {
		t.Fatalf("Failed to create message: %v", err)
	}

	if msg.Type != MsgTypeStartSession {
		t.Errorf("Wrong type: %s", msg.Type)
	}
	if msg.RequestID != "req-1" {
		t.Errorf("Wrong request ID: %s", msg.RequestID)
	}

	// Parse payload back
	var payload StartSessionPayload
	if err := msg.ParsePayload(&payload); err != nil {
		t.Fatalf("Failed to parse payload: %v", err)
	}

	if payload.SDPType != "offer" {
		t.Errorf("Wrong SDP type: %s", payload.SDPType)
	}
	if payload.ConnectionID != "conn-123" {
		t.Errorf("Wrong connection ID: %s", payload.ConnectionID)
	}
}

func TestIPCErrorMessage(t *testing.T) {
	msg := NewErrorMessage("req-1", "test error")

	if msg.Type != "error" {
		t.Errorf("Wrong type: %s", msg.Type)
	}
	if msg.RequestID != "req-1" {
		t.Errorf("Wrong request ID: %s", msg.RequestID)
	}
	if msg.Error != "test error" {
		t.Errorf("Wrong error: %s", msg.Error)
	}
}
