// +build windows

// Package helper provides the user-mode desktop helper functionality
package helper

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"github.com/sentinel/agent/internal/desktop"
	"github.com/sentinel/agent/internal/webrtc"
)

// WebRTCHandler manages WebRTC sessions in the helper process
type WebRTCHandler struct {
	mu          sync.Mutex
	manager     *webrtc.Manager
	session     *webrtc.Session
	client      *desktop.IPCClient
	connectionID string
}

// NewWebRTCHandler creates a new WebRTC handler
func NewWebRTCHandler(client *desktop.IPCClient) *WebRTCHandler {
	return &WebRTCHandler{
		client:  client,
		manager: webrtc.NewManager(),
	}
}

// HandleStartSession processes a start session request from the service
func (h *WebRTCHandler) HandleStartSession(ctx context.Context, payload *desktop.StartSessionPayload) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	log.Printf("[WebRTCHandler] Starting session, connectionID=%s, sdpType=%s", payload.ConnectionID, payload.SDPType)

	h.connectionID = payload.ConnectionID

	// Update status
	h.client.SendStatus(desktop.StateConnecting, "Creating WebRTC session", payload.ConnectionID)

	// Create session config
	config := webrtc.SessionConfig{
		SessionID: payload.ConnectionID,
		Quality:   "medium", // TODO: make configurable
	}

	// Create session with callbacks for signaling
	session, err := h.manager.CreateSession(config,
		func(signal webrtc.SignalMessage) {
			h.onSignal(signal)
		},
		func(input webrtc.InputEvent) {
			h.onInput(input)
		},
	)
	if err != nil {
		log.Printf("[WebRTCHandler] Failed to create session: %v", err)
		h.client.SendStatus(desktop.StateError, err.Error(), payload.ConnectionID)
		return err
	}

	h.session = session

	// Set remote description (the offer from browser)
	log.Printf("[WebRTCHandler] Setting remote description...")
	if err := session.SetRemoteDescription(payload.SDPType, payload.SDP); err != nil {
		log.Printf("[WebRTCHandler] Failed to set remote description: %v", err)
		h.client.SendStatus(desktop.StateError, err.Error(), payload.ConnectionID)
		return err
	}

	// Create answer
	log.Printf("[WebRTCHandler] Creating answer...")
	answer, err := session.CreateAnswer()
	if err != nil {
		log.Printf("[WebRTCHandler] Failed to create answer: %v", err)
		h.client.SendStatus(desktop.StateError, err.Error(), payload.ConnectionID)
		return err
	}

	log.Printf("[WebRTCHandler] Sending answer, length=%d", len(answer))

	// Send answer back to service
	if err := h.client.SendSessionAnswer(payload.ConnectionID, "answer", answer); err != nil {
		log.Printf("[WebRTCHandler] Failed to send answer: %v", err)
		return err
	}

	h.client.SendStatus(desktop.StateConnecting, "Answer sent, waiting for connection", payload.ConnectionID)

	return nil
}

// HandleStopSession processes a stop session request
func (h *WebRTCHandler) HandleStopSession(payload *desktop.StopSessionPayload) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	log.Printf("[WebRTCHandler] Stopping session, connectionID=%s", payload.ConnectionID)

	if h.session != nil {
		h.session.Stop()
		h.session = nil
	}

	h.client.SendStatus(desktop.StateDisconnected, "Session stopped", payload.ConnectionID)
	return nil
}

// HandleICECandidate processes an ICE candidate from the service
func (h *WebRTCHandler) HandleICECandidate(payload *desktop.ICECandidatePayload) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.session == nil {
		log.Printf("[WebRTCHandler] Received ICE candidate but no active session")
		return nil
	}

	log.Printf("[WebRTCHandler] Adding ICE candidate")
	return h.session.AddICECandidate(payload.Candidate)
}

// onSignal is called when there's an outgoing signal (ICE candidate)
func (h *WebRTCHandler) onSignal(signal webrtc.SignalMessage) {
	log.Printf("[WebRTCHandler] Signal: type=%s", signal.Type)

	if signal.Type == "candidate" && signal.Candidate != "" {
		// Parse the candidate JSON to extract components
		var candidateInit struct {
			Candidate     string  `json:"candidate"`
			SDPMid        *string `json:"sdpMid"`
			SDPMLineIndex *int    `json:"sdpMLineIndex"`
		}

		if err := json.Unmarshal([]byte(signal.Candidate), &candidateInit); err != nil {
			log.Printf("[WebRTCHandler] Failed to parse ICE candidate: %v", err)
			return
		}

		sdpMid := ""
		if candidateInit.SDPMid != nil {
			sdpMid = *candidateInit.SDPMid
		}

		if err := h.client.SendICECandidate(h.connectionID, signal.Candidate, sdpMid, candidateInit.SDPMLineIndex); err != nil {
			log.Printf("[WebRTCHandler] Failed to send ICE candidate: %v", err)
		}
	}
}

// onInput is called when input events are received from the browser
func (h *WebRTCHandler) onInput(input webrtc.InputEvent) {
	log.Printf("[WebRTCHandler] Input: type=%s, event=%s", input.Type, input.Event)
	// Input handling is done in the webrtc package - the callbacks handle it
}

// Close cleans up resources
func (h *WebRTCHandler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.session != nil {
		h.session.Stop()
		h.session = nil
	}
}

// IsConnected returns true if there's an active WebRTC connection
func (h *WebRTCHandler) IsConnected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.session == nil {
		return false
	}
	return h.session.Connected
}

// GetState returns the current WebRTC state
func (h *WebRTCHandler) GetState() desktop.HelperState {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.session == nil {
		return desktop.StateReady
	}

	if h.session.Connected {
		return desktop.StateConnected
	}

	if h.session.Active {
		return desktop.StateConnecting
	}

	return desktop.StateDisconnected
}
