//go:build linux && !arm64 && !arm

package desktop

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/sentinel/agent/internal/capture"
	"github.com/sentinel/agent/internal/desktop/helper"
	internalWebrtc "github.com/sentinel/agent/internal/webrtc"
)

// Manager coordinates remote desktop sessions on Linux
type Manager struct {
	mu             sync.Mutex
	sessions       map[uint32]*LinuxSession
	helperPath     string // Not used on Linux but kept for API compatibility

	// Callbacks for forwarding messages to server
	onSessionAnswer func(sessionID uint32, connectionID, sdpType, sdp string)
	onICECandidate  func(sessionID uint32, connectionID, candidate, sdpMid string, sdpMLineIndex *int)
	onStatusUpdate  func(sessionID uint32, state HelperState, message, connectionID string)
}

// LinuxSession represents an active remote desktop session on Linux
type LinuxSession struct {
	SessionID    uint32
	ConnectionID string

	// WebRTC components
	peerConnection *webrtc.PeerConnection
	videoTrack     *webrtc.TrackLocalStaticSample
	dataChannel    *webrtc.DataChannel

	// Capture components
	capture       *capture.X11Capture
	inputInjector *helper.InputInjector

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	stopCh chan struct{}
}

// NewManager creates a new desktop manager for Linux
func NewManager(helperPath string) *Manager {
	return &Manager{
		sessions:   make(map[uint32]*LinuxSession),
		helperPath: helperPath,
	}
}

// GetActiveConsoleSessionID returns 0 on Linux (no Windows session concept)
// On Linux, we use session 0 for the current X display
func GetActiveConsoleSessionID() uint32 {
	return 0
}

// SetCallbacks sets the callback functions for forwarding messages
func (m *Manager) SetCallbacks(
	onSessionAnswer func(sessionID uint32, connectionID, sdpType, sdp string),
	onICECandidate func(sessionID uint32, connectionID, candidate, sdpMid string, sdpMLineIndex *int),
	onStatusUpdate func(sessionID uint32, state HelperState, message, connectionID string),
) {
	m.onSessionAnswer = onSessionAnswer
	m.onICECandidate = onICECandidate
	m.onStatusUpdate = onStatusUpdate
}

// StartSession starts a WebRTC remote desktop session on Linux
func (m *Manager) StartSession(ctx context.Context, sessionID uint32, connectionID, sdpType, sdp string) (string, string, error) {
	log.Printf("[LinuxManager] StartSession called, sessionID=%d, connectionID=%s", sessionID, connectionID)

	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	if exists {
		// Clean up existing session
		session.cancel()
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	// Create new session
	sessionCtx, cancel := context.WithCancel(ctx)
	session = &LinuxSession{
		SessionID:    sessionID,
		ConnectionID: connectionID,
		ctx:          sessionCtx,
		cancel:       cancel,
		stopCh:       make(chan struct{}),
	}

	// Initialize screen capture
	screenCapture, err := capture.NewX11Capture("")
	if err != nil {
		cancel()
		log.Printf("[LinuxManager] Failed to create X11 capture: %v", err)
		return "", "", err
	}
	session.capture = screenCapture

	// Initialize input injector
	session.inputInjector = helper.NewInputInjector()
	if session.inputInjector != nil {
		width, height := screenCapture.GetDimensions()
		session.inputInjector.SetSourceDimensions(width, height, 0, 0)
	}

	// Create WebRTC peer connection
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		screenCapture.Release()
		cancel()
		return "", "", err
	}
	session.peerConnection = pc

	// Create video track
	// Get dimensions for input coordinate scaling
	captureWidth, captureHeight := screenCapture.GetDimensions()
	log.Printf("[LinuxManager] Capture dimensions: %dx%d", captureWidth, captureHeight)

	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeVP8,
			ClockRate: 90000,
		},
		"video",
		"desktop-video",
	)
	if err != nil {
		pc.Close()
		screenCapture.Release()
		cancel()
		return "", "", err
	}
	session.videoTrack = videoTrack

	_, err = pc.AddTrack(videoTrack)
	if err != nil {
		pc.Close()
		screenCapture.Release()
		cancel()
		return "", "", err
	}

	// Create data channel for input
	dc, err := pc.CreateDataChannel("input", nil)
	if err != nil {
		pc.Close()
		screenCapture.Release()
		cancel()
		return "", "", err
	}
	session.dataChannel = dc

	// Handle data channel messages (input events)
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if session.inputInjector == nil {
			return
		}

		var input internalWebrtc.InputEvent
		if err := json.Unmarshal(msg.Data, &input); err != nil {
			log.Printf("[LinuxManager] Failed to parse input: %v", err)
			return
		}

		// Handle viewer dimension updates
		if input.Type == "dimensions" {
			session.inputInjector.SetViewerDimensions(int(input.X), int(input.Y))
			return
		}

		session.inputInjector.InjectInput(input)
	})

	// Handle ICE candidates
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		if m.onICECandidate != nil {
			init := candidate.ToJSON()
			var sdpMLineIndex *int
			if init.SDPMLineIndex != nil {
				idx := int(*init.SDPMLineIndex)
				sdpMLineIndex = &idx
			}
			m.onICECandidate(sessionID, connectionID, init.Candidate, *init.SDPMid, sdpMLineIndex)
		}
	})

	// Handle connection state changes
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[LinuxManager] Connection state: %s", state.String())
		if m.onStatusUpdate != nil {
			m.onStatusUpdate(sessionID, HelperState(state.String()), "", connectionID)
		}

		if state == webrtc.PeerConnectionStateConnected {
			// Start capture loop
			go session.captureLoop()
		} else if state == webrtc.PeerConnectionStateFailed ||
			state == webrtc.PeerConnectionStateClosed ||
			state == webrtc.PeerConnectionStateDisconnected {
			session.cancel()
		}
	})

	// Set remote description (offer from viewer)
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		pc.Close()
		screenCapture.Release()
		cancel()
		return "", "", err
	}

	// Create answer
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		screenCapture.Release()
		cancel()
		return "", "", err
	}

	// Set local description
	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		screenCapture.Release()
		cancel()
		return "", "", err
	}

	// Store session
	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()

	log.Printf("[LinuxManager] Session started, answering with SDP (%d bytes)", len(answer.SDP))
	return "answer", answer.SDP, nil
}

// captureLoop captures and sends frames
func (s *LinuxSession) captureLoop() {
	log.Printf("[LinuxSession] Starting capture loop")

	ticker := time.NewTicker(33 * time.Millisecond) // ~30 FPS
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			frame, err := s.capture.CaptureFrame(100)
			if err != nil || frame == nil {
				continue
			}

			// Encode frame to VP8
			// Note: In a real implementation, you'd use a VP8 encoder
			// For now, we'll send raw frame data (this won't work with WebRTC directly)
			// This is a placeholder - proper VP8 encoding requires libvpx integration

			if s.videoTrack != nil {
				// VP8 encoding would happen here
				// For demonstration, we're skipping the encoding step
				// In production, integrate with libvpx or use a Go VP8 encoder

				sample := media.Sample{
					Data:     frame.Data[:min(len(frame.Data), 65535)], // Truncate for safety
					Duration: 33 * time.Millisecond,
				}
				if err := s.videoTrack.WriteSample(sample); err != nil {
					log.Printf("[LinuxSession] Write sample error: %v", err)
				}
			}
		}
	}
}

// AddICECandidate forwards an ICE candidate to the session
func (m *Manager) AddICECandidate(sessionID uint32, connectionID, candidate, sdpMid string, sdpMLineIndex *int) error {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	m.mu.Unlock()

	if !exists {
		log.Printf("[LinuxManager] Session %d not found for ICE candidate", sessionID)
		return nil
	}

	if session.peerConnection == nil {
		return nil
	}

	iceCandidate := webrtc.ICECandidateInit{
		Candidate:     candidate,
		SDPMid:        &sdpMid,
		SDPMLineIndex: (*uint16)(nil),
	}
	if sdpMLineIndex != nil {
		idx := uint16(*sdpMLineIndex)
		iceCandidate.SDPMLineIndex = &idx
	}

	return session.peerConnection.AddICECandidate(iceCandidate)
}

// StopSession stops the remote desktop session
func (m *Manager) StopSession(sessionID uint32, connectionID, reason string) error {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	if exists {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if !exists {
		return nil
	}

	log.Printf("[LinuxManager] Stopping session %d: %s", sessionID, reason)

	session.cancel()
	close(session.stopCh)

	if session.peerConnection != nil {
		session.peerConnection.Close()
	}

	if session.capture != nil {
		session.capture.Release()
	}

	if session.inputInjector != nil {
		session.inputInjector.Release()
	}

	return nil
}

// Shutdown gracefully shuts down all sessions
func (m *Manager) Shutdown() {
	m.mu.Lock()
	sessions := make([]*LinuxSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[uint32]*LinuxSession)
	m.mu.Unlock()

	for _, session := range sessions {
		log.Printf("[LinuxManager] Shutting down session %d", session.SessionID)
		session.cancel()
		close(session.stopCh)

		if session.peerConnection != nil {
			session.peerConnection.Close()
		}
		if session.capture != nil {
			session.capture.Release()
		}
		if session.inputInjector != nil {
			session.inputInjector.Release()
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
