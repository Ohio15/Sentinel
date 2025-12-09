//go:build windows

package remote

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/jpeg"
	"log"
	"sync"
	"syscall"
	"time"

	"github.com/kbinani/screenshot"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procSetCursorPos = user32.NewProc("SetCursorPos")
	procMouseEvent   = user32.NewProc("mouse_event")
	procKeybd_event  = user32.NewProc("keybd_event")
)

const (
	MOUSEEVENTF_LEFTDOWN   = 0x0002
	MOUSEEVENTF_LEFTUP     = 0x0004
	MOUSEEVENTF_RIGHTDOWN  = 0x0008
	MOUSEEVENTF_RIGHTUP    = 0x0010
	MOUSEEVENTF_MIDDLEDOWN = 0x0020
	MOUSEEVENTF_MIDDLEUP   = 0x0040
	MOUSEEVENTF_MOVE       = 0x0001
	MOUSEEVENTF_ABSOLUTE   = 0x8000
	KEYEVENTF_KEYUP        = 0x0002
)

// Session represents a remote desktop session
type Session struct {
	ID         string
	Quality    int // 1-100 JPEG quality
	FrameRate  int // Frames per second
	Active     bool
	OnFrame    func(data string, width, height int)
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
}

// Manager manages remote desktop sessions
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewManager creates a new remote desktop manager
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

// StartSession creates and starts a new remote desktop session
func (m *Manager) StartSession(sessionID string, quality string, onFrame func(data string, width, height int)) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop existing session if any
	if existing, ok := m.sessions[sessionID]; ok {
		existing.Stop()
	}

	// Map quality string to JPEG quality
	jpegQuality := 50
	frameRate := 10
	switch quality {
	case "low":
		jpegQuality = 30
		frameRate = 5
	case "medium":
		jpegQuality = 50
		frameRate = 10
	case "high":
		jpegQuality = 80
		frameRate = 15
	}

	ctx, cancel := context.WithCancel(context.Background())
	session := &Session{
		ID:        sessionID,
		Quality:   jpegQuality,
		FrameRate: frameRate,
		Active:    true,
		OnFrame:   onFrame,
		ctx:       ctx,
		cancel:    cancel,
	}

	m.sessions[sessionID] = session

	// Start frame capture loop
	go session.captureLoop()

	log.Printf("Remote desktop session %s started (quality: %s)", sessionID, quality)
	return session, nil
}

// StopSession stops a remote desktop session
func (m *Manager) StopSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.sessions[sessionID]; ok {
		session.Stop()
		delete(m.sessions, sessionID)
		log.Printf("Remote desktop session %s stopped", sessionID)
	}
}

// GetSession returns a session by ID
func (m *Manager) GetSession(sessionID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	return s, ok
}

// Stop stops the session
func (s *Session) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Active = false
	if s.cancel != nil {
		s.cancel()
	}
}

// captureLoop continuously captures and sends screen frames
func (s *Session) captureLoop() {
	interval := time.Second / time.Duration(s.FrameRate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			active := s.Active
			s.mu.Unlock()

			if !active {
				return
			}

			// Capture screen
			bounds := screenshot.GetDisplayBounds(0)
			img, err := screenshot.CaptureRect(bounds)
			if err != nil {
				log.Printf("Screen capture error: %v", err)
				continue
			}

			// Encode as JPEG
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: s.Quality}); err != nil {
				log.Printf("JPEG encode error: %v", err)
				continue
			}

			// Send as base64
			data := base64.StdEncoding.EncodeToString(buf.Bytes())
			if s.OnFrame != nil {
				s.OnFrame(data, bounds.Dx(), bounds.Dy())
			}
		}
	}
}

// HandleInput processes mouse and keyboard input
func (s *Session) HandleInput(inputType string, data map[string]interface{}) {
	switch inputType {
	case "mouse":
		s.handleMouseInput(data)
	case "keyboard":
		s.handleKeyboardInput(data)
	}
}

func (s *Session) handleMouseInput(data map[string]interface{}) {
	x, _ := data["x"].(float64)
	y, _ := data["y"].(float64)
	event, _ := data["event"].(string)
	button, _ := data["button"].(float64)

	// Move cursor
	procSetCursorPos.Call(uintptr(int(x)), uintptr(int(y)))

	// Handle mouse events
	var flags uintptr
	switch event {
	case "mousedown":
		switch int(button) {
		case 0:
			flags = MOUSEEVENTF_LEFTDOWN
		case 1:
			flags = MOUSEEVENTF_MIDDLEDOWN
		case 2:
			flags = MOUSEEVENTF_RIGHTDOWN
		}
	case "mouseup":
		switch int(button) {
		case 0:
			flags = MOUSEEVENTF_LEFTUP
		case 1:
			flags = MOUSEEVENTF_MIDDLEUP
		case 2:
			flags = MOUSEEVENTF_RIGHTUP
		}
	case "mousemove":
		// Just move, no click
		return
	}

	if flags != 0 {
		procMouseEvent.Call(flags, 0, 0, 0, 0)
	}
}

func (s *Session) handleKeyboardInput(data map[string]interface{}) {
	key, _ := data["key"].(string)
	event, _ := data["event"].(string)
	modifiers, _ := data["modifiers"].([]interface{})

	// Convert key to virtual key code
	vk := keyToVirtualKey(key)
	if vk == 0 {
		return
	}

	// Handle modifiers
	for _, mod := range modifiers {
		modStr, _ := mod.(string)
		modVk := modifierToVirtualKey(modStr)
		if modVk != 0 && event == "keydown" {
			procKeybd_event.Call(uintptr(modVk), 0, 0, 0)
		}
	}

	// Handle key event
	var flags uintptr = 0
	if event == "keyup" {
		flags = KEYEVENTF_KEYUP
	}
	procKeybd_event.Call(uintptr(vk), 0, flags, 0)

	// Release modifiers on keyup
	if event == "keyup" {
		for _, mod := range modifiers {
			modStr, _ := mod.(string)
			modVk := modifierToVirtualKey(modStr)
			if modVk != 0 {
				procKeybd_event.Call(uintptr(modVk), 0, KEYEVENTF_KEYUP, 0)
			}
		}
	}
}

func keyToVirtualKey(key string) int {
	// Common key mappings
	keyMap := map[string]int{
		"Enter":      0x0D,
		"Escape":     0x1B,
		"Tab":        0x09,
		"Backspace":  0x08,
		"Delete":     0x2E,
		"Insert":     0x2D,
		"Home":       0x24,
		"End":        0x23,
		"PageUp":     0x21,
		"PageDown":   0x22,
		"ArrowUp":    0x26,
		"ArrowDown":  0x28,
		"ArrowLeft":  0x25,
		"ArrowRight": 0x27,
		"F1":         0x70,
		"F2":         0x71,
		"F3":         0x72,
		"F4":         0x73,
		"F5":         0x74,
		"F6":         0x75,
		"F7":         0x76,
		"F8":         0x77,
		"F9":         0x78,
		"F10":        0x79,
		"F11":        0x7A,
		"F12":        0x7B,
		" ":          0x20,
	}

	if vk, ok := keyMap[key]; ok {
		return vk
	}

	// Handle single character keys
	if len(key) == 1 {
		char := int(key[0])
		// A-Z
		if char >= 'a' && char <= 'z' {
			return char - 32 // Convert to uppercase for VK code
		}
		if char >= 'A' && char <= 'Z' {
			return char
		}
		// 0-9
		if char >= '0' && char <= '9' {
			return char
		}
	}

	return 0
}

func modifierToVirtualKey(mod string) int {
	switch mod {
	case "ctrl":
		return 0x11 // VK_CONTROL
	case "alt":
		return 0x12 // VK_MENU
	case "shift":
		return 0x10 // VK_SHIFT
	case "meta":
		return 0x5B // VK_LWIN
	}
	return 0
}
