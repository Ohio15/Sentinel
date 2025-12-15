// Package desktop provides IPC communication between the Sentinel service
// and the desktop helper process for WebRTC remote desktop functionality.
package desktop

import (
	"encoding/json"
	"time"
)

// Message types for IPC communication
const (
	// Authentication
	MsgTypeAuth   = "auth"    // Helper -> Service: authenticate with token
	MsgTypeAuthOK = "auth_ok" // Service -> Helper: token validated

	// Health monitoring
	MsgTypeHeartbeat    = "heartbeat"     // Helper -> Service: health check
	MsgTypeHeartbeatAck = "heartbeat_ack" // Service -> Helper: acknowledgment

	// Session control
	MsgTypeStartSession  = "start_session"  // Service -> Helper: start WebRTC with SDP offer
	MsgTypeSessionAnswer = "session_answer" // Helper -> Service: SDP answer
	MsgTypeICECandidate  = "ice_candidate"  // Bidirectional: ICE candidate exchange
	MsgTypeStopSession   = "stop_session"   // Service -> Helper: stop current session

	// Status updates
	MsgTypeStatus = "status" // Helper -> Service: status update

	// Lifecycle
	MsgTypeShutdown = "shutdown" // Service -> Helper: graceful shutdown request
)

// IPCMessage is the envelope for all IPC communication
type IPCMessage struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// AuthPayload is sent by the helper to authenticate with the service
type AuthPayload struct {
	Token     string `json:"token"`
	SessionID uint32 `json:"sessionId"`
	PID       int    `json:"pid"`
}

// AuthOKPayload is sent by the service to confirm authentication
type AuthOKPayload struct {
	Capabilities []string  `json:"capabilities"` // e.g., ["capture", "input"]
	ExpiresAt    time.Time `json:"expiresAt"`
}

// HeartbeatPayload contains health information from the helper
type HeartbeatPayload struct {
	Timestamp   time.Time `json:"timestamp"`
	CPUPercent  float64   `json:"cpuPercent,omitempty"`
	MemoryMB    float64   `json:"memoryMB,omitempty"`
	SessionID   uint32    `json:"sessionId"`
	WebRTCState string    `json:"webrtcState,omitempty"` // "", "connecting", "connected", "disconnected"
}

// HeartbeatAckPayload is the service's response to a heartbeat
type HeartbeatAckPayload struct {
	Timestamp time.Time `json:"timestamp"`
	Continue  bool      `json:"continue"` // false = helper should shutdown
}

// StartSessionPayload contains the WebRTC offer from the browser
type StartSessionPayload struct {
	SDPType      string `json:"sdpType"` // "offer"
	SDP          string `json:"sdp"`
	ConnectionID string `json:"connectionId"`
}

// SessionAnswerPayload contains the WebRTC answer from the helper
type SessionAnswerPayload struct {
	SDPType      string `json:"sdpType"` // "answer"
	SDP          string `json:"sdp"`
	ConnectionID string `json:"connectionId"`
}

// ICECandidatePayload contains an ICE candidate
type ICECandidatePayload struct {
	Candidate     string `json:"candidate"`
	SDPMid        string `json:"sdpMid,omitempty"`
	SDPMLineIndex *int   `json:"sdpMLineIndex,omitempty"`
	ConnectionID  string `json:"connectionId"`
}

// StopSessionPayload requests the helper to stop the WebRTC session
type StopSessionPayload struct {
	ConnectionID string `json:"connectionId"`
	Reason       string `json:"reason,omitempty"`
}

// StatusPayload reports helper status to the service
type StatusPayload struct {
	State        HelperState `json:"state"`
	Message      string      `json:"message,omitempty"`
	ConnectionID string      `json:"connectionId,omitempty"`
}

// HelperState represents the current state of the helper
type HelperState string

const (
	StateInitializing   HelperState = "initializing"
	StateReady          HelperState = "ready"
	StateConnecting     HelperState = "connecting"
	StateConnected      HelperState = "connected"
	StateDisconnected   HelperState = "disconnected"
	StateSecureDesktop  HelperState = "secure_desktop" // UAC prompt active
	StateError          HelperState = "error"
	StateShuttingDown   HelperState = "shutting_down"
)

// ShutdownPayload requests graceful shutdown
type ShutdownPayload struct {
	Reason  string        `json:"reason"`
	Timeout time.Duration `json:"timeout"` // Time allowed for graceful shutdown
}

// Helper functions to create messages

// NewIPCMessage creates a new IPC message with the given type and payload
func NewIPCMessage(msgType string, requestID string, payload interface{}) (*IPCMessage, error) {
	var payloadBytes json.RawMessage
	if payload != nil {
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	return &IPCMessage{
		Type:      msgType,
		RequestID: requestID,
		Payload:   payloadBytes,
	}, nil
}

// NewErrorMessage creates an error response message
func NewErrorMessage(requestID string, errMsg string) *IPCMessage {
	return &IPCMessage{
		Type:      "error",
		RequestID: requestID,
		Error:     errMsg,
	}
}

// ParsePayload unmarshals the payload into the given type
func (m *IPCMessage) ParsePayload(v interface{}) error {
	if m.Payload == nil {
		return nil
	}
	return json.Unmarshal(m.Payload, v)
}

// Constants for IPC configuration
const (
	// PipeNamePrefix is the prefix for named pipes (sessionId appended)
	PipeNamePrefix = `\\.\pipe\SentinelDesktop_`

	// MutexNamePrefix is the prefix for session-scoped mutexes
	MutexNamePrefix = `Global\SentinelDesktop_`

	// HeartbeatInterval is how often the helper sends heartbeats
	HeartbeatInterval = 5 * time.Second

	// HeartbeatTimeout is how long the service waits for a heartbeat
	HeartbeatTimeout = 15 * time.Second

	// TokenTTL is how long an authorization token is valid
	TokenTTL = 60 * time.Second

	// GracefulShutdownTimeout is how long to wait for graceful shutdown
	GracefulShutdownTimeout = 10 * time.Second
)

// Exit codes for the helper process
const (
	ExitCodeSuccess           = 0
	ExitCodeIPCDisconnected   = 1
	ExitCodeTokenExpired      = 2
	ExitCodeDesktopInvalid    = 3
	ExitCodeSecureDesktop     = 4
	ExitCodeIdleTimeout       = 5
	ExitCodeShutdownRequested = 6
	ExitCodeMutexConflict     = 7
	ExitCodeAuthFailed        = 8
	ExitCodeInternalError     = 99
)
