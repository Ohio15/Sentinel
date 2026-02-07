// Package webrtc provides WebRTC session management for remote desktop
package webrtc

// KeyboardModifiers represents modifier key states
type KeyboardModifiers struct {
	Ctrl  bool `json:"ctrl,omitempty"`
	Alt   bool `json:"alt,omitempty"`
	Shift bool `json:"shift,omitempty"`
	Meta  bool `json:"meta,omitempty"`
}

// InputEvent represents a mouse or keyboard input event
type InputEvent struct {
	Type      string             `json:"type"` // "mouse" or "keyboard"
	Event     string             `json:"event"`
	X         float64            `json:"x,omitempty"`
	Y         float64            `json:"y,omitempty"`
	Button    int                `json:"button,omitempty"`
	Key       string             `json:"key,omitempty"`
	Code      string             `json:"code,omitempty"`
	Modifiers *KeyboardModifiers `json:"modifiers,omitempty"`
	DeltaY    float64            `json:"deltaY,omitempty"`
}

// ICEServer represents a STUN/TURN server configuration
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// SessionConfig contains configuration for a WebRTC session
type SessionConfig struct {
	SessionID  string      `json:"sessionId"`
	ICEServers []ICEServer `json:"iceServers"`
	Quality    string      `json:"quality"` // "low", "medium", "high"
}

// SignalMessage represents a signaling message (SDP or ICE candidate)
type SignalMessage struct {
	Type      string `json:"type"` // "offer", "answer", "candidate"
	SessionID string `json:"sessionId"`
	SDP       string `json:"sdp,omitempty"`
	Candidate string `json:"candidate,omitempty"`
}
