//go:build windows

package remote

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"log"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/kbinani/screenshot"
	"golang.org/x/sys/windows"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procSetCursorPos = user32.NewProc("SetCursorPos")
	procMouseEvent   = user32.NewProc("mouse_event")
	procKeybd_event  = user32.NewProc("keybd_event")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")

	kernel32        = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalAlloc = kernel32.NewProc("GlobalAlloc")
	procGlobalLock  = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procGlobalSize  = kernel32.NewProc("GlobalSize")
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
	MOUSEEVENTF_WHEEL      = 0x0800
	KEYEVENTF_KEYUP        = 0x0002

	CF_TEXT    = 1
	CF_UNICODETEXT = 13
	GMEM_MOVEABLE = 0x0002
)

// MonitorInfo contains information about a display monitor
type MonitorInfo struct {
	Index   int    `json:"index"`
	Name    string `json:"name"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Primary bool   `json:"primary"`
}

// Session represents a remote desktop session
type Session struct {
	ID              string
	Quality         int // 1-100 JPEG quality
	FrameRate       int // Frames per second
	Active          bool
	MonitorIndex    int // -1 for all monitors, 0+ for specific monitor
	Monitors        []MonitorInfo
	ClipboardSync   bool
	lastClipboard   string
	OnFrame         func(data string, width, height int)
	OnClipboard     func(text string)
	OnMonitorList   func(monitors []MonitorInfo)
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.Mutex
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
		ID:            sessionID,
		Quality:       jpegQuality,
		FrameRate:     frameRate,
		Active:        true,
		MonitorIndex:  -1, // All monitors by default
		ClipboardSync: true,
		OnFrame:       onFrame,
		ctx:           ctx,
		cancel:        cancel,
	}

	// Get monitor information
	session.Monitors = getMonitorList()

	m.sessions[sessionID] = session

	// Start frame capture loop
	go session.captureLoop()

	// Start clipboard sync loop
	go session.clipboardLoop()

	log.Printf("Remote desktop session %s started (quality: %s, monitors: %d)", sessionID, quality, len(session.Monitors))
	return session, nil
}

// getMonitorList returns information about all connected monitors
func getMonitorList() []MonitorInfo {
	numDisplays := screenshot.NumActiveDisplays()
	monitors := make([]MonitorInfo, numDisplays)

	for i := 0; i < numDisplays; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		monitors[i] = MonitorInfo{
			Index:   i,
			Name:    fmt.Sprintf("Display %d", i+1),
			Width:   bounds.Dx(),
			Height:  bounds.Dy(),
			X:       bounds.Min.X,
			Y:       bounds.Min.Y,
			Primary: i == 0, // First monitor is typically primary
		}
	}

	return monitors
}

// SetMonitor changes which monitor to capture
func (s *Session) SetMonitor(index int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MonitorIndex = index
	log.Printf("Session %s switched to monitor %d", s.ID, index)
}

// SetClipboardSync enables or disables clipboard synchronization
func (s *Session) SetClipboardSync(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ClipboardSync = enabled
}

// GetMonitors returns the list of available monitors
func (s *Session) GetMonitors() []MonitorInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Monitors
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
			monitorIndex := s.MonitorIndex
			s.mu.Unlock()

			if !active {
				return
			}

			var img *image.RGBA
			var bounds image.Rectangle

			if monitorIndex < 0 {
				// Capture all monitors combined
				img, bounds = captureAllMonitors()
			} else {
				// Capture specific monitor
				numDisplays := screenshot.NumActiveDisplays()
				if monitorIndex >= numDisplays {
					monitorIndex = 0
				}
				bounds = screenshot.GetDisplayBounds(monitorIndex)
				var err error
				img, err = screenshot.CaptureRect(bounds)
				if err != nil {
					log.Printf("Screen capture error: %v", err)
					continue
				}
			}

			if img == nil {
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

// captureAllMonitors captures all monitors into a single image
func captureAllMonitors() (*image.RGBA, image.Rectangle) {
	numDisplays := screenshot.NumActiveDisplays()
	if numDisplays == 0 {
		return nil, image.Rectangle{}
	}

	if numDisplays == 1 {
		bounds := screenshot.GetDisplayBounds(0)
		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			return nil, image.Rectangle{}
		}
		return img, bounds
	}

	// Calculate total bounds
	var minX, minY, maxX, maxY int
	for i := 0; i < numDisplays; i++ {
		b := screenshot.GetDisplayBounds(i)
		if i == 0 || b.Min.X < minX {
			minX = b.Min.X
		}
		if i == 0 || b.Min.Y < minY {
			minY = b.Min.Y
		}
		if i == 0 || b.Max.X > maxX {
			maxX = b.Max.X
		}
		if i == 0 || b.Max.Y > maxY {
			maxY = b.Max.Y
		}
	}

	totalBounds := image.Rect(0, 0, maxX-minX, maxY-minY)
	combined := image.NewRGBA(totalBounds)

	// Capture each display and draw to combined image
	for i := 0; i < numDisplays; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			continue
		}

		// Calculate position in combined image
		destPt := image.Pt(bounds.Min.X-minX, bounds.Min.Y-minY)
		draw.Draw(combined, image.Rectangle{
			Min: destPt,
			Max: destPt.Add(img.Bounds().Size()),
		}, img, image.Point{}, draw.Src)
	}

	return combined, totalBounds
}

// clipboardLoop monitors clipboard changes and syncs them
func (s *Session) clipboardLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			active := s.Active
			syncEnabled := s.ClipboardSync
			lastClip := s.lastClipboard
			s.mu.Unlock()

			if !active || !syncEnabled {
				continue
			}

			// Get current clipboard content
			text := getClipboardText()
			if text != "" && text != lastClip {
				s.mu.Lock()
				s.lastClipboard = text
				s.mu.Unlock()

				if s.OnClipboard != nil {
					s.OnClipboard(text)
				}
			}
		}
	}
}

// getClipboardText retrieves text from the Windows clipboard
func getClipboardText() string {
	ret, _, _ := procOpenClipboard.Call(0)
	if ret == 0 {
		return ""
	}
	defer procCloseClipboard.Call()

	// Check if text is available
	ret, _, _ = procIsClipboardFormatAvailable.Call(CF_UNICODETEXT)
	if ret == 0 {
		return ""
	}

	// Get clipboard data
	h, _, _ := procGetClipboardData.Call(CF_UNICODETEXT)
	if h == 0 {
		return ""
	}

	// Lock the memory
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)

	// Get size
	size, _, _ := procGlobalSize.Call(h)
	if size == 0 {
		return ""
	}

	// Convert UTF-16 to string
	u16 := make([]uint16, size/2)
	for i := range u16 {
		u16[i] = *(*uint16)(unsafe.Pointer(ptr + uintptr(i*2)))
		if u16[i] == 0 {
			u16 = u16[:i]
			break
		}
	}

	return string(utf16.Decode(u16))
}

// SetClipboardText sets text in the Windows clipboard
func SetClipboardText(text string) error {
	ret, _, err := procOpenClipboard.Call(0)
	if ret == 0 {
		return fmt.Errorf("failed to open clipboard: %v", err)
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()

	// Convert string to UTF-16
	u16 := utf16.Encode([]rune(text + "\x00"))
	size := len(u16) * 2

	// Allocate global memory
	h, _, err := procGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(size))
	if h == 0 {
		return fmt.Errorf("failed to allocate memory: %v", err)
	}

	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return fmt.Errorf("failed to lock memory")
	}

	// Copy data
	for i, v := range u16 {
		*(*uint16)(unsafe.Pointer(ptr + uintptr(i*2))) = v
	}

	procGlobalUnlock.Call(h)

	ret, _, err = procSetClipboardData.Call(CF_UNICODETEXT, h)
	if ret == 0 {
		return fmt.Errorf("failed to set clipboard data: %v", err)
	}

	return nil
}

// HandleInput processes mouse, keyboard, and other remote input
func (s *Session) HandleInput(inputType string, data map[string]interface{}) {
	switch inputType {
	case "mouse":
		s.handleMouseInput(data)
	case "keyboard":
		s.handleKeyboardInput(data)
	case "setMonitor":
		if idx, ok := data["index"].(float64); ok {
			s.SetMonitor(int(idx))
		}
	case "getMonitors":
		// Return monitor list via callback
		if s.OnMonitorList != nil {
			s.OnMonitorList(s.GetMonitors())
		}
	case "setClipboard":
		if text, ok := data["text"].(string); ok {
			if err := SetClipboardText(text); err != nil {
				log.Printf("Failed to set clipboard: %v", err)
			}
		}
	case "setClipboardSync":
		if enabled, ok := data["enabled"].(bool); ok {
			s.SetClipboardSync(enabled)
		}
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
	var mouseData uintptr
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
	case "wheel":
		// Mouse wheel scroll
		flags = MOUSEEVENTF_WHEEL
		delta, _ := data["deltaY"].(float64)
		// Standard wheel delta is 120 per notch
		mouseData = uintptr(int32(-delta * 120))
	}

	if flags != 0 {
		procMouseEvent.Call(flags, 0, 0, mouseData, 0)
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
