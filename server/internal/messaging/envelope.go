// Package messaging provides robust message handling with request/response correlation,
// targeted routing, and backpressure management for the Sentinel server.
package messaging

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Priority levels for message delivery
const (
	PriorityLow      = 0
	PriorityNormal   = 5
	PriorityHigh     = 10
	PriorityCritical = 15
)

// MessageEnvelope wraps all WebSocket messages with correlation and routing metadata.
// This enables request/response tracking, targeted delivery, and session affinity.
type MessageEnvelope struct {
	// MessageID is a unique identifier for this message instance
	MessageID string `json:"messageId"`

	// RequestID links responses back to their originating requests
	// When a dashboard sends a command, it includes a requestId.
	// The agent's response includes the same requestId for correlation.
	RequestID string `json:"requestId,omitempty"`

	// Type is the message type (e.g., "terminal_output", "execute_command")
	Type string `json:"type"`

	// SourceID identifies the sender (agentID for agents, userID for dashboards)
	SourceID string `json:"sourceId"`

	// TargetID specifies the intended recipient (for targeted routing)
	// If empty, routing is determined by message type
	TargetID string `json:"targetId,omitempty"`

	// SessionID links messages to a specific terminal/RDP/file session
	// Enables session-scoped routing and persistence
	SessionID string `json:"sessionId,omitempty"`

	// DeviceID is the device this message relates to (for device-scoped subscriptions)
	DeviceID string `json:"deviceId,omitempty"`

	// RequiresAck indicates the sender expects an acknowledgment
	RequiresAck bool `json:"requiresAck,omitempty"`

	// Priority affects delivery order and timeout handling
	Priority int `json:"priority,omitempty"`

	// Timestamp when the message was created
	Timestamp time.Time `json:"timestamp"`

	// Payload contains the actual message data
	Payload json.RawMessage `json:"payload,omitempty"`
}

// NewEnvelope creates a new message envelope with defaults
func NewEnvelope(msgType string, sourceID string) *MessageEnvelope {
	return &MessageEnvelope{
		MessageID: uuid.New().String(),
		Type:      msgType,
		SourceID:  sourceID,
		Priority:  PriorityNormal,
		Timestamp: time.Now(),
	}
}

// NewRequest creates a new envelope configured as a request that expects a response
func NewRequest(msgType string, sourceID string, payload interface{}) (*MessageEnvelope, error) {
	env := NewEnvelope(msgType, sourceID)
	env.RequestID = uuid.New().String()
	env.RequiresAck = true

	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		env.Payload = data
	}

	return env, nil
}

// NewResponse creates a response envelope linked to the original request
func NewResponse(request *MessageEnvelope, success bool, payload interface{}, errMsg string) (*MessageEnvelope, error) {
	env := NewEnvelope("response", "")
	env.RequestID = request.RequestID
	env.TargetID = request.SourceID
	env.SessionID = request.SessionID
	env.DeviceID = request.DeviceID

	responsePayload := map[string]interface{}{
		"success": success,
	}
	if payload != nil {
		responsePayload["data"] = payload
	}
	if errMsg != "" {
		responsePayload["error"] = errMsg
	}

	data, err := json.Marshal(responsePayload)
	if err != nil {
		return nil, err
	}
	env.Payload = data

	return env, nil
}

// WithSession adds session context to the envelope
func (e *MessageEnvelope) WithSession(sessionID string) *MessageEnvelope {
	e.SessionID = sessionID
	return e
}

// WithDevice adds device context to the envelope
func (e *MessageEnvelope) WithDevice(deviceID string) *MessageEnvelope {
	e.DeviceID = deviceID
	return e
}

// WithTarget specifies a specific recipient
func (e *MessageEnvelope) WithTarget(targetID string) *MessageEnvelope {
	e.TargetID = targetID
	return e
}

// WithPriority sets the message priority
func (e *MessageEnvelope) WithPriority(priority int) *MessageEnvelope {
	e.Priority = priority
	return e
}

// WithPayload sets the message payload
func (e *MessageEnvelope) WithPayload(payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	e.Payload = data
	return nil
}

// Marshal serializes the envelope to JSON
func (e *MessageEnvelope) Marshal() ([]byte, error) {
	return json.Marshal(e)
}

// ParseEnvelope attempts to parse a message as an envelope.
// Returns nil if the message doesn't match the envelope format.
func ParseEnvelope(data []byte) (*MessageEnvelope, error) {
	var env MessageEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	return &env, nil
}

// LegacyMessage represents the existing message format for backward compatibility
type LegacyMessage struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId,omitempty"`
	AgentID   string          `json:"agentId,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	DeviceID  string          `json:"deviceId,omitempty"`
	Timestamp time.Time       `json:"timestamp,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Success   bool            `json:"success,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// ToEnvelope converts a legacy message to an envelope format
func (m *LegacyMessage) ToEnvelope() *MessageEnvelope {
	env := &MessageEnvelope{
		MessageID: uuid.New().String(),
		Type:      m.Type,
		RequestID: m.RequestID,
		SourceID:  m.AgentID,
		SessionID: m.SessionID,
		DeviceID:  m.DeviceID,
		Timestamp: m.Timestamp,
		Priority:  PriorityNormal,
	}

	// Use Payload if present, otherwise use Data
	if m.Payload != nil && len(m.Payload) > 0 {
		env.Payload = m.Payload
	} else if m.Data != nil && len(m.Data) > 0 {
		env.Payload = m.Data
	}

	if env.Timestamp.IsZero() {
		env.Timestamp = time.Now()
	}

	return env
}

// ParseLegacyMessage parses a message in the legacy format
func ParseLegacyMessage(data []byte) (*LegacyMessage, error) {
	var msg LegacyMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// IsEnvelopeFormat checks if the message uses the new envelope format
// (has messageId field which legacy messages don't have)
func IsEnvelopeFormat(data []byte) bool {
	var check struct {
		MessageID string `json:"messageId"`
	}
	if err := json.Unmarshal(data, &check); err != nil {
		return false
	}
	return check.MessageID != ""
}

// NormalizeMessage converts any incoming message to envelope format.
// Supports both new envelope format and legacy format for backward compatibility.
func NormalizeMessage(data []byte, defaultSourceID string) (*MessageEnvelope, error) {
	if IsEnvelopeFormat(data) {
		return ParseEnvelope(data)
	}

	// Parse as legacy and convert
	legacy, err := ParseLegacyMessage(data)
	if err != nil {
		return nil, err
	}

	env := legacy.ToEnvelope()
	if env.SourceID == "" {
		env.SourceID = defaultSourceID
	}

	return env, nil
}

// DeliveryStatus tracks the state of message delivery
type DeliveryStatus int

const (
	DeliveryPending DeliveryStatus = iota
	DeliveryQueued
	DeliverySent
	DeliveryAcknowledged
	DeliveryFailed
	DeliveryTimeout
)

func (s DeliveryStatus) String() string {
	switch s {
	case DeliveryPending:
		return "pending"
	case DeliveryQueued:
		return "queued"
	case DeliverySent:
		return "sent"
	case DeliveryAcknowledged:
		return "acknowledged"
	case DeliveryFailed:
		return "failed"
	case DeliveryTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// DeliveryReceipt tracks delivery of a specific message
type DeliveryReceipt struct {
	MessageID  string         `json:"messageId"`
	Status     DeliveryStatus `json:"status"`
	SentAt     time.Time      `json:"sentAt,omitempty"`
	AckedAt    time.Time      `json:"ackedAt,omitempty"`
	FailedAt   time.Time      `json:"failedAt,omitempty"`
	Error      string         `json:"error,omitempty"`
	RetryCount int            `json:"retryCount"`
}
