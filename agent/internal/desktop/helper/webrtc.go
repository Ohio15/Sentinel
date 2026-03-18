// +build windows

// Package helper provides the user-mode desktop helper functionality
package helper

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	pionwebrtc "github.com/pion/webrtc/v4"
	"github.com/sentinel/agent/internal/desktop"
	"github.com/sentinel/agent/internal/desktop/recording"
	"github.com/sentinel/agent/internal/webrtc"
)

// WebRTCHandler manages WebRTC sessions in the helper process
type WebRTCHandler struct {
	mu              sync.Mutex
	manager         *webrtc.Manager
	session         *webrtc.Session
	client          *desktop.IPCClient
	connectionID    string
	injector        *InputInjector
	clipboardBridge *ClipboardBridge
	ftBridge        *FileTransferBridge
	recorder        *recording.Recorder
}

// NewWebRTCHandler creates a new WebRTC handler
func NewWebRTCHandler(client *desktop.IPCClient) *WebRTCHandler {
	return &WebRTCHandler{
		client:   client,
		manager:  webrtc.NewManager(),
		injector: NewInputInjector(),
	}
}

// SetClipboardBridge attaches a clipboard bridge to be wired into WebRTC sessions.
func (h *WebRTCHandler) SetClipboardBridge(cb *ClipboardBridge) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clipboardBridge = cb
}

// SetFileTransferBridge attaches a file transfer bridge to be wired into WebRTC sessions.
func (h *WebRTCHandler) SetFileTransferBridge(ftb *FileTransferBridge) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ftBridge = ftb
}

// SetRecorder attaches a session recorder to be wired into WebRTC sessions.
func (h *WebRTCHandler) SetRecorder(rec *recording.Recorder) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recorder = rec
}

// HandleStartSession processes a start session request from the service
func (h *WebRTCHandler) HandleStartSession(ctx context.Context, payload *desktop.StartSessionPayload) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	log.Printf("[WebRTCHandler] Starting session, connectionID=%s, sdpType=%s", payload.ConnectionID, payload.SDPType)

	h.connectionID = payload.ConnectionID

	// Configure input injector for primary monitor (index 0)
	// This sets up coordinate transformation for the captured screen area
	h.injector.SetActiveMonitor(0)

	// Get the primary screen dimensions and configure the injector
	// Video is captured at screen resolution, so viewer = source
	screenWidth, screenHeight := GetPrimaryScreenDimensions()
	log.Printf("[WebRTCHandler] Configuring injector for primary screen: %dx%d", screenWidth, screenHeight)
	h.injector.SetSourceDimensions(screenWidth, screenHeight, 0, 0)
	h.injector.SetViewerDimensions(screenWidth, screenHeight)

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

	// Wire SAS callback — forward to service via IPC
	session.OnSAS = func() {
		if h.client != nil {
			// SAS request uses a uint32 session ID; use 0 since we don't track numeric IDs here
			h.client.SendSASRequest(0, "user_request")
		}
	}

	// Wire monitor list request — enumerate monitors and send back via data channel
	session.OnRequestMonitors = func() {
		ct := NewCoordinateTransformer()
		monitors := ct.GetMonitors()

		type monitorEntry struct {
			Index   int    `json:"index"`
			Name    string `json:"name"`
			X       int    `json:"x"`
			Y       int    `json:"y"`
			Width   int    `json:"width"`
			Height  int    `json:"height"`
			Primary bool   `json:"primary"`
		}

		entries := make([]monitorEntry, len(monitors))
		for i, m := range monitors {
			entries[i] = monitorEntry{
				Index:   m.Index,
				Name:    m.Name,
				X:       m.Left,
				Y:       m.Top,
				Width:   m.Width,
				Height:  m.Height,
				Primary: m.IsPrimary,
			}
		}

		msg, err := json.Marshal(map[string]interface{}{
			"type":     "monitorList",
			"monitors": entries,
		})
		if err != nil {
			log.Printf("[WebRTCHandler] Failed to marshal monitor list: %v", err)
			return
		}

		if dc := session.DataChannel; dc != nil && dc.ReadyState() == pionwebrtc.DataChannelStateOpen {
			if err := dc.SendText(string(msg)); err != nil {
				log.Printf("[WebRTCHandler] Failed to send monitor list: %v", err)
			}
		}
	}

	// Wire monitor selection
	session.OnMonitorSelect = func(index int) {
		log.Printf("[WebRTCHandler] Monitor switch requested: index=%d", index)
		h.injector.SetActiveMonitor(index)
		// TODO: Implement DXGI capture switch when monitor_manager is available
	}

	// Wire clipboard bridge
	if h.clipboardBridge != nil {
		session.OnClipboard = func(msgType string, data []byte) {
			h.clipboardBridge.HandleMessage(msgType, json.RawMessage(data))
		}
	}

	// Wire file transfer bridge
	if h.ftBridge != nil {
		session.OnFileTransfer = func(msgType string, data []byte) {
			h.ftBridge.HandleMessage(msgType, json.RawMessage(data))
		}
	}

	// Wire recording
	if h.recorder != nil {
		session.OnRecording = func(msgType string, data []byte) {
			var msg struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				log.Printf("[Helper] Failed to parse recording message: %v", err)
				return
			}

			switch msg.Type {
			case "recording.start":
				width, height := 0, 0
				if ve := session.GetVideoEncoder(); ve != nil {
					width = ve.GetWidth()
					height = ve.GetHeight()
				}
				if err := h.recorder.Start(session.ID, width, height, ""); err != nil {
					log.Printf("[Helper] Recording start failed: %v", err)
				} else {
					statusMsg, _ := json.Marshal(map[string]interface{}{
						"type":   "recording.status",
						"active": true,
					})
					if dc := session.DataChannel; dc != nil && dc.ReadyState() == pionwebrtc.DataChannelStateOpen {
						dc.SendText(string(statusMsg))
					}
				}
			case "recording.stop":
				h.recorder.Stop()
				statusMsg, _ := json.Marshal(map[string]interface{}{
					"type": "recording.stopped",
				})
				if dc := session.DataChannel; dc != nil && dc.ReadyState() == pionwebrtc.DataChannelStateOpen {
					dc.SendText(string(statusMsg))
				}
			}
		}
	}

	// Wire audio control messages
	session.OnAudio = func(msgType string, data []byte) {
		// Audio is managed at the WebRTC track level.
		// Control messages (mute, volume, device selection) are handled here.
		log.Printf("[Helper] Audio control message: %s", msgType)
		// Audio track manager integration point — will be wired when audio capture starts
	}

	// Wire handler for additional data channels (e.g. file transfer)
	session.OnDataChannelOpen = func(label string, dc *pionwebrtc.DataChannel) {
		log.Printf("[WebRTCHandler] Extra data channel opened: %s", label)
		switch label {
		case "filetransfer":
			if h.ftBridge != nil {
				dc.OnOpen(func() {
					h.ftBridge.Start(dc)
				})
			}
		default:
			log.Printf("[WebRTCHandler] Unhandled data channel label: %s", label)
		}
	}

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

	// Stop subsystem bridges before tearing down the session
	if h.clipboardBridge != nil {
		h.clipboardBridge.Stop()
	}
	if h.ftBridge != nil {
		h.ftBridge.Stop()
	}
	if h.recorder != nil {
		h.recorder.Stop()
	}

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

	// Log the actual candidate value to help debug ICE issues
	log.Printf("[WebRTCHandler] Adding ICE candidate: %s", payload.Candidate)
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
	log.Printf("[WebRTCHandler] Input: type=%s, event=%s, x=%.1f, y=%.1f, button=%d, key=%s",
		input.Type, input.Event, input.X, input.Y, input.Button, input.Key)
	// Inject the input into Windows
	if h.injector != nil {
		h.injector.InjectInput(input)
	}
}

// Close cleans up resources
func (h *WebRTCHandler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Stop subsystem bridges
	if h.clipboardBridge != nil {
		h.clipboardBridge.Stop()
	}
	if h.ftBridge != nil {
		h.ftBridge.Stop()
	}
	if h.recorder != nil {
		h.recorder.Stop()
	}

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
