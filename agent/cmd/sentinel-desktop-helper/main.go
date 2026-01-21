//go:build windows
// +build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"image"
	"image/jpeg"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"github.com/kbinani/screenshot"
)

// Message types for IPC
const (
	MsgTypeFrame    = "frame"
	MsgTypeInput    = "input"
	MsgTypeStop     = "stop"
	MsgTypeStarted  = "started"
	MsgTypeError    = "error"
	MsgTypeMonitors = "monitors"
)

// IPCMessage represents a message sent over the named pipe
type IPCMessage struct {
	Type      string      `json:"type"`
	SessionID string      `json:"sessionId,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
}

// FrameData contains screen capture data
type FrameData struct {
	Data   string `json:"data"`   // base64 encoded JPEG
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// InputData contains mouse/keyboard input
type InputData struct {
	InputType string                 `json:"inputType"` // mouse or keyboard
	Data      map[string]interface{} `json:"data"`
}

// MonitorInfo contains display information
type MonitorInfo struct {
	Index   int    `json:"index"`
	Name    string `json:"name"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Primary bool   `json:"primary"`
}

// Windows API constants for input injection
var (
	user32                    = syscall.NewLazyDLL("user32.dll")
	shcore                    = syscall.NewLazyDLL("shcore.dll")
	procSetCursorPos          = user32.NewProc("SetCursorPos")
	procSendInput             = user32.NewProc("SendInput")
	procMouseEvent            = user32.NewProc("mouse_event")
	procKeybdEvent            = user32.NewProc("keybd_event")
	procMapVirtualKey         = user32.NewProc("MapVirtualKeyW")
	procVkKeyScan             = user32.NewProc("VkKeyScanW")
	procGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	procSetProcessDPIAware    = user32.NewProc("SetProcessDPIAware")
	procSetProcessDpiAwareness = shcore.NewProc("SetProcessDpiAwareness")
)

// DPI awareness constants
const (
	PROCESS_DPI_UNAWARE           = 0
	PROCESS_SYSTEM_DPI_AWARE      = 1
	PROCESS_PER_MONITOR_DPI_AWARE = 2
)

// SendInput structure types
const (
	INPUT_TYPE_MOUSE    = 0
	INPUT_TYPE_KEYBOARD = 1
)

type MOUSEINPUT struct {
	Dx          int32
	Dy          int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type KEYBDINPUT struct {
	Vk          uint16
	Scan        uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type INPUT_MOUSE struct {
	Type uint32
	Mi   MOUSEINPUT
	// Padding to match union size
	Padding [8]byte
}

type INPUT_KBD struct {
	Type uint32
	Ki   KEYBDINPUT
	// Padding to match union size
	Padding [16]byte
}

// Alias for backwards compatibility
type INPUT = INPUT_MOUSE

func init() {
	// Set DPI awareness for accurate coordinate mapping
	// Try per-monitor awareness first (Windows 8.1+), fall back to system-aware
	if procSetProcessDpiAwareness.Find() == nil {
		procSetProcessDpiAwareness.Call(uintptr(PROCESS_PER_MONITOR_DPI_AWARE))
		log.Println("[DPI] Set per-monitor DPI awareness")
	} else if procSetProcessDPIAware.Find() == nil {
		procSetProcessDPIAware.Call()
		log.Println("[DPI] Set system DPI awareness")
	}
}

// Mouse event flags
const (
	MOUSEEVENTF_MOVE       = 0x0001
	MOUSEEVENTF_LEFTDOWN   = 0x0002
	MOUSEEVENTF_LEFTUP     = 0x0004
	MOUSEEVENTF_RIGHTDOWN  = 0x0008
	MOUSEEVENTF_RIGHTUP    = 0x0010
	MOUSEEVENTF_MIDDLEDOWN = 0x0020
	MOUSEEVENTF_MIDDLEUP   = 0x0040
	MOUSEEVENTF_WHEEL      = 0x0800
	MOUSEEVENTF_ABSOLUTE   = 0x8000
)

// Keyboard event flags
const (
	KEYEVENTF_KEYDOWN     = 0x0000
	KEYEVENTF_EXTENDEDKEY = 0x0001
	KEYEVENTF_KEYUP       = 0x0002
	KEYEVENTF_SCANCODE    = 0x0008
	KEYEVENTF_UNICODE     = 0x0004
)

// Virtual key codes
var vkCodes = map[string]uint16{
	"Backspace":    0x08,
	"Tab":          0x09,
	"Enter":        0x0D,
	"Shift":        0x10,
	"Control":      0x11,
	"Alt":          0x12,
	"Pause":        0x13,
	"CapsLock":     0x14,
	"Escape":       0x1B,
	"Space":        0x20,
	"PageUp":       0x21,
	"PageDown":     0x22,
	"End":          0x23,
	"Home":         0x24,
	"ArrowLeft":    0x25,
	"ArrowUp":      0x26,
	"ArrowRight":   0x27,
	"ArrowDown":    0x28,
	"PrintScreen":  0x2C,
	"Insert":       0x2D,
	"Delete":       0x2E,
	"F1":           0x70,
	"F2":           0x71,
	"F3":           0x72,
	"F4":           0x73,
	"F5":           0x74,
	"F6":           0x75,
	"F7":           0x76,
	"F8":           0x77,
	"F9":           0x78,
	"F10":          0x79,
	"F11":          0x7A,
	"F12":          0x7B,
	"NumLock":      0x90,
	"ScrollLock":   0x91,
	"ShiftLeft":    0xA0,
	"ShiftRight":   0xA1,
	"ControlLeft":  0xA2,
	"ControlRight": 0xA3,
	"AltLeft":      0xA4,
	"AltRight":     0xA5,
}

type DesktopHelper struct {
	sessionID    string
	pipeName     string
	conn         net.Conn
	quality      int
	frameRate    int
	monitorIndex int
	active       bool
	mu           sync.Mutex
	stopCh       chan struct{}
	// Capture bounds offset - needed to convert image coords to screen coords
	boundsOffsetX int
	boundsOffsetY int
}

func main() {
	pipeName := flag.String("pipe", "", "Named pipe path for IPC")
	sessionID := flag.String("session", "", "Remote session ID")
	quality := flag.Int("quality", 50, "JPEG quality (1-100)")
	frameRate := flag.Int("fps", 30, "Frames per second")
	flag.Parse()

	if *pipeName == "" || *sessionID == "" {
		log.Fatal("Usage: sentinel-desktop-helper -pipe <pipepath> -session <sessionid>")
	}

	log.Printf("Desktop helper starting for session %s", *sessionID)
	log.Printf("Connecting to pipe: %s", *pipeName)

	helper := &DesktopHelper{
		sessionID:    *sessionID,
		pipeName:     *pipeName,
		quality:      *quality,
		frameRate:    *frameRate,
		monitorIndex: -1, // All monitors
		stopCh:       make(chan struct{}),
	}

	if err := helper.Run(); err != nil {
		log.Fatalf("Helper error: %v", err)
	}
}

func (h *DesktopHelper) Run() error {
	// Connect to named pipe using winio for Windows named pipes
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := winio.DialPipeContext(ctx, h.pipeName)
	if err != nil {
		return err
	}
	h.conn = conn
	defer conn.Close()

	log.Println("Connected to service pipe")

	// Send started message with monitor info
	monitors := h.getMonitorList()
	h.sendMessage(IPCMessage{
		Type:      MsgTypeStarted,
		SessionID: h.sessionID,
		Data: map[string]interface{}{
			"monitors": monitors,
		},
	})

	h.active = true

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start capture loop
	go h.captureLoop()

	// Read commands from pipe
	go h.readCommands()

	// Wait for stop signal
	select {
	case <-h.stopCh:
		log.Println("Stop signal received")
	case sig := <-sigCh:
		log.Printf("Signal received: %v", sig)
	}

	h.mu.Lock()
	h.active = false
	h.mu.Unlock()

	log.Println("Desktop helper shutting down")
	return nil
}

func (h *DesktopHelper) readCommands() {
	reader := bufio.NewReader(h.conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			log.Printf("Pipe read error: %v", err)
			close(h.stopCh)
			return
		}

		var msg IPCMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Printf("Failed to parse message: %v", err)
			continue
		}

		switch msg.Type {
		case MsgTypeStop:
			log.Println("Stop command received")
			close(h.stopCh)
			return

		case MsgTypeInput:
			h.handleInput(msg)
		}
	}
}

func (h *DesktopHelper) handleInput(msg IPCMessage) {
	log.Printf("[Helper] handleInput received: %+v", msg)

	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		log.Printf("[Helper] Invalid data type: %T", msg.Data)
		return
	}

	inputType, _ := data["inputType"].(string)
	inputData, _ := data["data"].(map[string]interface{})

	log.Printf("[Helper] inputType=%s, inputData=%+v", inputType, inputData)

	switch inputType {
	case "mouse":
		log.Printf("[Helper] Processing mouse input")
		h.handleMouseInput(inputData)
	case "mouse_relative":
		log.Printf("[Helper] Processing relative mouse input")
		h.handleRelativeMouseInput(inputData)
	case "keyboard":
		log.Printf("[Helper] Processing keyboard input")
		h.handleKeyboardInput(inputData)
	}
}

// getVirtualScreenMetrics returns the virtual screen dimensions for absolute coordinate conversion
func getVirtualScreenMetrics() (int, int, int, int) {
	// SM_XVIRTUALSCREEN (76) - left of virtual screen
	// SM_YVIRTUALSCREEN (77) - top of virtual screen
	// SM_CXVIRTUALSCREEN (78) - width of virtual screen
	// SM_CYVIRTUALSCREEN (79) - height of virtual screen
	left, _, _ := procGetSystemMetrics.Call(76)
	top, _, _ := procGetSystemMetrics.Call(77)
	width, _, _ := procGetSystemMetrics.Call(78)
	height, _, _ := procGetSystemMetrics.Call(79)
	return int(left), int(top), int(width), int(height)
}

// toAbsoluteCoords converts screen coordinates to absolute coordinates for SendInput
// Absolute coordinates range from 0 to 65535 across the virtual screen
func toAbsoluteCoords(screenX, screenY int) (int32, int32) {
	left, top, width, height := getVirtualScreenMetrics()
	// Normalize to virtual screen origin
	relX := screenX - left
	relY := screenY - top
	// Convert to 0-65535 range
	absX := int32((relX * 65535) / width)
	absY := int32((relY * 65535) / height)
	return absX, absY
}

// sendMouseInput uses the SendInput API for accurate mouse positioning
func (h *DesktopHelper) sendMouseInput(absX, absY int32, flags uint32, mouseData uint32) {
	input := INPUT{
		Type: INPUT_TYPE_MOUSE,
		Mi: MOUSEINPUT{
			Dx:          absX,
			Dy:          absY,
			DwFlags:     flags,
			MouseData:   mouseData,
			Time:        0,
			DwExtraInfo: 0,
		},
	}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&input)), uintptr(unsafe.Sizeof(input)))
}

func (h *DesktopHelper) handleMouseInput(data map[string]interface{}) {
	eventType, _ := data["type"].(string)
	x, _ := data["x"].(float64)
	y, _ := data["y"].(float64)
	button, _ := data["button"].(float64)

	// Get bounds offset to convert image coords to screen coords
	h.mu.Lock()
	offsetX := h.boundsOffsetX
	offsetY := h.boundsOffsetY
	h.mu.Unlock()

	// Convert image coordinates to screen coordinates
	screenX := int(x) + offsetX
	screenY := int(y) + offsetY

	log.Printf("[Mouse] Image coords (%d,%d) + offset (%d,%d) = screen coords (%d,%d)",
		int(x), int(y), offsetX, offsetY, screenX, screenY)

	// Convert to absolute coordinates for accurate positioning
	absX, absY := toAbsoluteCoords(screenX, screenY)

	// Use SendInput for accurate cursor positioning with absolute coordinates
	var flags uint32 = MOUSEEVENTF_MOVE | MOUSEEVENTF_ABSOLUTE

	switch eventType {
	case "move":
		// Just move the cursor
		h.sendMouseInput(absX, absY, flags, 0)
		return
	case "down":
		// Move + button down
		switch int(button) {
		case 0:
			flags |= MOUSEEVENTF_LEFTDOWN
		case 1:
			flags |= MOUSEEVENTF_MIDDLEDOWN
		case 2:
			flags |= MOUSEEVENTF_RIGHTDOWN
		}
		h.sendMouseInput(absX, absY, flags, 0)
		return
	case "up":
		// Move + button up
		switch int(button) {
		case 0:
			flags |= MOUSEEVENTF_LEFTUP
		case 1:
			flags |= MOUSEEVENTF_MIDDLEUP
		case 2:
			flags |= MOUSEEVENTF_RIGHTUP
		}
		h.sendMouseInput(absX, absY, flags, 0)
		return
	case "wheel":
		deltaY, _ := data["deltaY"].(float64)
		// Move to position first, then send wheel
		h.sendMouseInput(absX, absY, MOUSEEVENTF_MOVE|MOUSEEVENTF_ABSOLUTE, 0)
		// Wheel delta is in units of 120
		wheelDelta := int32(-deltaY / 2)
		procMouseEvent.Call(
			uintptr(MOUSEEVENTF_WHEEL),
			0, 0,
			uintptr(wheelDelta),
			0,
		)
		return
	}
}

func (h *DesktopHelper) handleKeyboardInput(data map[string]interface{}) {
	eventType, _ := data["type"].(string)
	key, _ := data["key"].(string)
	code, _ := data["code"].(string)

	var vk uint16
	var scan uint16
	var flags uint32

	// Check for special keys first
	if vkCode, ok := vkCodes[key]; ok {
		vk = vkCode
	} else if vkCode, ok := vkCodes[code]; ok {
		vk = vkCode
	} else if len(key) == 1 {
		// Single character - use VkKeyScan
		r := []rune(key)[0]
		ret, _, _ := procVkKeyScan.Call(uintptr(r))
		vk = uint16(ret & 0xFF)
	}

	if vk == 0 {
		return
	}

	// Get scan code for the virtual key
	scanRet, _, _ := procMapVirtualKey.Call(uintptr(vk), 0) // MAPVK_VK_TO_VSC
	scan = uint16(scanRet)

	// Check for extended keys (arrows, home, end, insert, delete, etc.)
	isExtended := false
	switch vk {
	case 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x2D, 0x2E: // Page Up/Down, End, Home, Arrows, Insert, Delete
		isExtended = true
	case 0x6A, 0x6B, 0x6D, 0x6F: // Numpad *, +, -, /
		isExtended = false
	}
	// Extended keys also include right Ctrl, right Alt, etc.
	if code == "ControlRight" || code == "AltRight" || code == "NumpadEnter" {
		isExtended = true
	}

	if isExtended {
		flags |= KEYEVENTF_EXTENDEDKEY
	}

	// Build keyboard input
	input := INPUT_KBD{
		Type: INPUT_TYPE_KEYBOARD,
		Ki: KEYBDINPUT{
			Vk:          vk,
			Scan:        scan,
			DwFlags:     flags,
			Time:        0,
			DwExtraInfo: 0,
		},
	}

	switch eventType {
	case "down":
		// flags already set for keydown (0)
		procSendInput.Call(1, uintptr(unsafe.Pointer(&input)), uintptr(unsafe.Sizeof(input)))
	case "up":
		input.Ki.DwFlags |= KEYEVENTF_KEYUP
		procSendInput.Call(1, uintptr(unsafe.Pointer(&input)), uintptr(unsafe.Sizeof(input)))
	}
}

// handleRelativeMouseInput handles relative mouse movement (for pointer lock)
func (h *DesktopHelper) handleRelativeMouseInput(data map[string]interface{}) {
	deltaX, _ := data["deltaX"].(float64)
	deltaY, _ := data["deltaY"].(float64)

	// Use mouse_event with relative movement (no MOUSEEVENTF_ABSOLUTE)
	procMouseEvent.Call(
		MOUSEEVENTF_MOVE,
		uintptr(int32(deltaX)),
		uintptr(int32(deltaY)),
		0, 0,
	)
}

func (h *DesktopHelper) captureLoop() {
	interval := time.Second / time.Duration(h.frameRate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.mu.Lock()
			active := h.active
			monitorIndex := h.monitorIndex
			h.mu.Unlock()

			if !active {
				return
			}

			var img *image.RGBA
			var bounds image.Rectangle

			if monitorIndex < 0 {
				img, bounds = h.captureAllMonitors()
			} else {
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

			// Store bounds offset for mouse coordinate translation
			h.mu.Lock()
			h.boundsOffsetX = bounds.Min.X
			h.boundsOffsetY = bounds.Min.Y
			h.mu.Unlock()

			// Encode as JPEG
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: h.quality}); err != nil {
				log.Printf("JPEG encode error: %v", err)
				continue
			}

			// Send frame
			frameData := FrameData{
				Data:   base64.StdEncoding.EncodeToString(buf.Bytes()),
				Width:  bounds.Dx(),
				Height: bounds.Dy(),
			}

			h.sendMessage(IPCMessage{
				Type:      MsgTypeFrame,
				SessionID: h.sessionID,
				Data:      frameData,
			})
		}
	}
}

func (h *DesktopHelper) captureAllMonitors() (*image.RGBA, image.Rectangle) {
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

	totalBounds := image.Rect(minX, minY, maxX, maxY)
	combined := image.NewRGBA(totalBounds)

	// Capture each monitor and composite
	for i := 0; i < numDisplays; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			continue
		}

		// Copy to combined image
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				combined.Set(x, y, img.At(x-bounds.Min.X, y-bounds.Min.Y))
			}
		}
	}

	return combined, totalBounds
}

func (h *DesktopHelper) getMonitorList() []MonitorInfo {
	numDisplays := screenshot.NumActiveDisplays()
	monitors := make([]MonitorInfo, numDisplays)

	for i := 0; i < numDisplays; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		monitors[i] = MonitorInfo{
			Index:   i,
			Name:    "",
			Width:   bounds.Dx(),
			Height:  bounds.Dy(),
			X:       bounds.Min.X,
			Y:       bounds.Min.Y,
			Primary: i == 0,
		}
	}

	return monitors
}

func (h *DesktopHelper) sendMessage(msg IPCMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal message: %v", err)
		return
	}

	data = append(data, '\n')
	if _, err := h.conn.Write(data); err != nil {
		log.Printf("Failed to send message: %v", err)
	}
}
