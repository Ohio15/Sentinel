//go:build windows
// +build windows

package helper

import (
	"fmt"
	"log"
	"syscall"
	"unsafe"

	"github.com/sentinel/agent/internal/webrtc"
)

// Windows API constants for input injection
var (
	user32                     = syscall.NewLazyDLL("user32.dll")
	shcore                     = syscall.NewLazyDLL("shcore.dll")
	procSetCursorPos           = user32.NewProc("SetCursorPos")
	procSendInput              = user32.NewProc("SendInput")
	procMouseEvent             = user32.NewProc("mouse_event")
	procKeybdEvent             = user32.NewProc("keybd_event")
	procMapVirtualKey          = user32.NewProc("MapVirtualKeyW")
	procVkKeyScan              = user32.NewProc("VkKeyScanW")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procSetProcessDPIAware     = user32.NewProc("SetProcessDPIAware")
	procSetProcessDpiAwareness = shcore.NewProc("SetProcessDpiAwareness")
	// Desktop attachment APIs - needed for service-spawned processes
	procOpenInputDesktop       = user32.NewProc("OpenInputDesktop")
	procSetThreadDesktop       = user32.NewProc("SetThreadDesktop")
	procCloseDesktop           = user32.NewProc("CloseDesktop")
	procGetThreadDesktop       = user32.NewProc("GetThreadDesktop")
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

// INPUT_MOUSE is the Windows INPUT structure for mouse events
// On 64-bit Windows, the struct must be 40 bytes:
// - Type (4 bytes) + Padding (4 bytes) + MOUSEINPUT (32 bytes) = 40 bytes
type INPUT_MOUSE struct {
	Type     uint32
	Padding1 uint32 // Padding for 64-bit alignment of the union
	Mi       MOUSEINPUT
}

// INPUT_KBD is the Windows INPUT structure for keyboard events
// On 64-bit Windows, the struct must be 40 bytes:
// - Type (4 bytes) + Padding (4 bytes) + KEYBDINPUT (24 bytes) + Padding (8 bytes) = 40 bytes
type INPUT_KBD struct {
	Type     uint32
	Padding1 uint32  // Padding for 64-bit alignment of the union
	Ki       KEYBDINPUT
	Padding2 [8]byte // Padding to match INPUT size (union is 32 bytes)
}

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

// InputInjector handles injecting mouse and keyboard input into Windows
type InputInjector struct {
	boundsOffsetX int
	boundsOffsetY int
	transformer   *CoordinateTransformer
	useTransformer bool // When true, use transformer for coordinate mapping
}

// Desktop access rights
const (
	DESKTOP_CREATEMENU      = 0x0004
	DESKTOP_CREATEWINDOW    = 0x0002
	DESKTOP_ENUMERATE       = 0x0040
	DESKTOP_HOOKCONTROL     = 0x0008
	DESKTOP_JOURNALPLAYBACK = 0x0020
	DESKTOP_JOURNALRECORD   = 0x0010
	DESKTOP_READOBJECTS     = 0x0001
	DESKTOP_SWITCHDESKTOP   = 0x0100
	DESKTOP_WRITEOBJECTS    = 0x0080
	GENERIC_ALL             = 0x10000000
)

// AttachToInputDesktop attaches the current thread to the user's input desktop
// This is CRITICAL for processes spawned by services to be able to inject input
func AttachToInputDesktop() error {
	// Open the desktop that receives user input
	// dwFlags=0 means don't inherit, fInherit=false
	// dwDesiredAccess needs read/write for input injection
	desiredAccess := uintptr(DESKTOP_CREATEMENU | DESKTOP_CREATEWINDOW | DESKTOP_ENUMERATE |
		DESKTOP_HOOKCONTROL | DESKTOP_JOURNALPLAYBACK | DESKTOP_JOURNALRECORD |
		DESKTOP_READOBJECTS | DESKTOP_SWITCHDESKTOP | DESKTOP_WRITEOBJECTS)

	hDesktop, _, err := procOpenInputDesktop.Call(0, 0, desiredAccess)
	if hDesktop == 0 {
		log.Printf("[InputInjector] OpenInputDesktop failed: %v", err)
		// Try with GENERIC_ALL
		hDesktop, _, err = procOpenInputDesktop.Call(0, 0, GENERIC_ALL)
		if hDesktop == 0 {
			return fmt.Errorf("OpenInputDesktop failed: %v", err)
		}
	}
	log.Printf("[InputInjector] OpenInputDesktop succeeded, handle=0x%X", hDesktop)

	// Set this desktop for the current thread
	ret, _, err := procSetThreadDesktop.Call(hDesktop)
	if ret == 0 {
		procCloseDesktop.Call(hDesktop)
		return fmt.Errorf("SetThreadDesktop failed: %v", err)
	}
	log.Printf("[InputInjector] SetThreadDesktop succeeded - attached to user's input desktop")

	// Don't close the desktop handle - we need it for the lifetime of the process
	return nil
}

// NewInputInjector creates a new input injector
func NewInputInjector() *InputInjector {
	// CRITICAL: Attach to the user's input desktop FIRST
	// This is required for service-spawned processes to inject input
	if err := AttachToInputDesktop(); err != nil {
		log.Printf("[InputInjector] WARNING: Failed to attach to input desktop: %v", err)
		log.Printf("[InputInjector] Input injection may not work correctly")
	}

	// Set DPI awareness for accurate coordinate mapping
	if procSetProcessDpiAwareness.Find() == nil {
		procSetProcessDpiAwareness.Call(uintptr(PROCESS_PER_MONITOR_DPI_AWARE))
		log.Println("[InputInjector] Set per-monitor DPI awareness")
	} else if procSetProcessDPIAware.Find() == nil {
		procSetProcessDPIAware.Call()
		log.Println("[InputInjector] Set system DPI awareness")
	}

	return &InputInjector{
		transformer: NewCoordinateTransformer(),
	}
}

// SetBoundsOffset sets the screen coordinate offset (for multi-monitor)
// Deprecated: Use SetSourceDimensions and SetViewerDimensions instead
func (i *InputInjector) SetBoundsOffset(x, y int) {
	i.boundsOffsetX = x
	i.boundsOffsetY = y
}

// SetSourceDimensions configures the source (captured screen) dimensions
func (i *InputInjector) SetSourceDimensions(width, height, left, top int) {
	i.transformer.SetSourceDimensions(width, height, left, top)
	i.useTransformer = true
	// Also update legacy offsets for compatibility
	i.boundsOffsetX = left
	i.boundsOffsetY = top
}

// SetViewerDimensions configures the viewer (displayed video) dimensions
// This must be called when the viewer's video element size changes
func (i *InputInjector) SetViewerDimensions(width, height int) {
	i.transformer.SetViewerDimensions(width, height)
	i.useTransformer = true
}

// GetCoordinateTransformer returns the underlying coordinate transformer
func (i *InputInjector) GetCoordinateTransformer() *CoordinateTransformer {
	return i.transformer
}

// SetActiveMonitor sets which monitor is being captured
func (i *InputInjector) SetActiveMonitor(index int) {
	i.transformer.SetActiveMonitor(index)
	i.useTransformer = true
}

// GetMonitors returns the list of available monitors
func (i *InputInjector) GetMonitors() []MonitorInfo {
	return i.transformer.GetMonitors()
}

// InjectInput processes an input event from the browser and injects it into Windows
func (i *InputInjector) InjectInput(input webrtc.InputEvent) {
	// The browser sends type directly as the event
	// Handle both short forms (move, down, up) and long forms (mousemove, mousedown, mouseup)
	switch input.Type {
	case "move", "mousemove":
		// Mouse move
		input.Event = "move"
		input.Type = "mouse"
		i.handleMouseInput(input)
	case "down", "mousedown":
		// Mouse button down
		input.Event = "down"
		input.Type = "mouse"
		i.handleMouseInput(input)
	case "up", "mouseup":
		// Mouse button up
		input.Event = "up"
		input.Type = "mouse"
		i.handleMouseInput(input)
	case "wheel":
		// Mouse wheel
		input.Event = "wheel"
		input.Type = "mouse"
		i.handleMouseInput(input)
	case "keydown":
		// Keyboard key down
		input.Event = "down"
		input.Type = "keyboard"
		i.handleKeyboardInput(input)
	case "keyup":
		// Keyboard key up
		input.Event = "up"
		input.Type = "keyboard"
		i.handleKeyboardInput(input)
	case "mouse":
		// Legacy format with separate event field
		i.handleMouseInput(input)
	case "keyboard":
		// Legacy format with separate event field
		i.handleKeyboardInput(input)
	case "ping":
		// Heartbeat ping - ignore, handled at WebRTC layer
	default:
		log.Printf("[InputInjector] Unknown input type: %s", input.Type)
	}
}

// getVirtualScreenMetrics returns the virtual screen dimensions for absolute coordinate conversion
func getVirtualScreenMetrics() (int, int, int, int) {
	left, _, _ := procGetSystemMetrics.Call(76)  // SM_XVIRTUALSCREEN
	top, _, _ := procGetSystemMetrics.Call(77)   // SM_YVIRTUALSCREEN
	width, _, _ := procGetSystemMetrics.Call(78) // SM_CXVIRTUALSCREEN
	height, _, _ := procGetSystemMetrics.Call(79) // SM_CYVIRTUALSCREEN
	return int(left), int(top), int(width), int(height)
}

// toAbsoluteCoords converts screen coordinates to absolute coordinates for SendInput
func toAbsoluteCoords(screenX, screenY int) (int32, int32) {
	left, top, width, height := getVirtualScreenMetrics()
	relX := screenX - left
	relY := screenY - top
	absX := int32((relX * 65535) / width)
	absY := int32((relY * 65535) / height)
	return absX, absY
}

// sendMouseInput uses the SendInput API for accurate mouse positioning
func sendMouseInput(absX, absY int32, flags uint32, mouseData uint32) {
	input := INPUT_MOUSE{
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
	ret, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&input)), uintptr(unsafe.Sizeof(input)))
	if ret == 0 {
		log.Printf("[InputInjector] SendInput failed: %v (absX=%d, absY=%d, flags=0x%X)", err, absX, absY, flags)
	}
}

func (i *InputInjector) handleMouseInput(input webrtc.InputEvent) {
	var screenX, screenY int

	// Use transformer if enabled (handles video downscaling properly)
	if i.useTransformer {
		// Transform viewer coordinates to screen coordinates with bounds clamping
		screenX, screenY = i.transformer.TransformCoordinatesWithClamp(input.X, input.Y)
		scaleX, scaleY := i.transformer.GetScaleFactors()
		log.Printf("[InputInjector] Mouse: event=%s, viewer=(%.1f,%.1f), scale=(%.4f,%.4f), screen=(%d,%d)",
			input.Event, input.X, input.Y, scaleX, scaleY, screenX, screenY)
	} else {
		// Legacy: simple offset without scaling
		screenX = int(input.X) + i.boundsOffsetX
		screenY = int(input.Y) + i.boundsOffsetY
		log.Printf("[InputInjector] Mouse: event=%s, input=(%.1f,%.1f), offset=(%d,%d), screen=(%d,%d)",
			input.Event, input.X, input.Y, i.boundsOffsetX, i.boundsOffsetY, screenX, screenY)
	}

	switch input.Event {
	case "move":
		// Use SetCursorPos for movement - simpler and more reliable
		ret, _, err := procSetCursorPos.Call(uintptr(screenX), uintptr(screenY))
		if ret == 0 {
			log.Printf("[InputInjector] SetCursorPos failed: %v (x=%d, y=%d)", err, screenX, screenY)
		} else {
			log.Printf("[InputInjector] SetCursorPos SUCCESS: (%d, %d)", screenX, screenY)
		}

	case "down":
		// Use mouse_event with ABSOLUTE coordinates - more reliable for service-spawned processes
		var buttonFlags uint32
		switch input.Button {
		case 0:
			buttonFlags = MOUSEEVENTF_LEFTDOWN
		case 1:
			buttonFlags = MOUSEEVENTF_MIDDLEDOWN
		case 2:
			buttonFlags = MOUSEEVENTF_RIGHTDOWN
		}
		// Convert screen coordinates to absolute (0-65535 range)
		smCxScreen := int32(1920)
		smCyScreen := int32(1200)
		if ret, _, _ := procGetSystemMetrics.Call(0); ret != 0 {
			smCxScreen = int32(ret)
		}
		if ret, _, _ := procGetSystemMetrics.Call(1); ret != 0 {
			smCyScreen = int32(ret)
		}
		absX := uint32((float64(screenX) * 65535.0 / float64(smCxScreen)) + 0.5)
		absY := uint32((float64(screenY) * 65535.0 / float64(smCyScreen)) + 0.5)

		// First move to position using mouse_event with ABSOLUTE
		procMouseEvent.Call(
			uintptr(MOUSEEVENTF_MOVE|MOUSEEVENTF_ABSOLUTE),
			uintptr(absX),
			uintptr(absY),
			0,
			0,
		)
		// Then send the button down event
		procMouseEvent.Call(
			uintptr(buttonFlags|MOUSEEVENTF_ABSOLUTE),
			uintptr(absX),
			uintptr(absY),
			0,
			0,
		)
		log.Printf("[InputInjector] mouse_event down: button=%d at screen=(%d,%d) abs=(%d,%d)", input.Button, screenX, screenY, absX, absY)

	case "up":
		// Use mouse_event with ABSOLUTE coordinates
		var buttonFlags uint32
		switch input.Button {
		case 0:
			buttonFlags = MOUSEEVENTF_LEFTUP
		case 1:
			buttonFlags = MOUSEEVENTF_MIDDLEUP
		case 2:
			buttonFlags = MOUSEEVENTF_RIGHTUP
		}
		// Convert screen coordinates to absolute (0-65535 range)
		smCxScreen := int32(1920)
		smCyScreen := int32(1200)
		if ret, _, _ := procGetSystemMetrics.Call(0); ret != 0 {
			smCxScreen = int32(ret)
		}
		if ret, _, _ := procGetSystemMetrics.Call(1); ret != 0 {
			smCyScreen = int32(ret)
		}
		absX := uint32((float64(screenX) * 65535.0 / float64(smCxScreen)) + 0.5)
		absY := uint32((float64(screenY) * 65535.0 / float64(smCyScreen)) + 0.5)

		// Send button up event
		procMouseEvent.Call(
			uintptr(buttonFlags|MOUSEEVENTF_ABSOLUTE),
			uintptr(absX),
			uintptr(absY),
			0,
			0,
		)
		log.Printf("[InputInjector] mouse_event up: button=%d at screen=(%d,%d) abs=(%d,%d)", input.Button, screenX, screenY, absX, absY)

	case "wheel":
		// Move to position first, then send wheel
		procSetCursorPos.Call(uintptr(screenX), uintptr(screenY))
		// Wheel delta is in units of 120
		wheelDelta := int32(-input.DeltaY / 2)
		procMouseEvent.Call(
			uintptr(MOUSEEVENTF_WHEEL),
			0, 0,
			uintptr(wheelDelta),
			0,
		)
	}
}

func (i *InputInjector) handleKeyboardInput(input webrtc.InputEvent) {
	var vk uint16
	var scan uint16
	var flags uint32

	// Check for special keys first
	if vkCode, ok := vkCodes[input.Key]; ok {
		vk = vkCode
	} else if len(input.Key) == 1 {
		// Single character - use VkKeyScan
		r := []rune(input.Key)[0]
		ret, _, _ := procVkKeyScan.Call(uintptr(r))
		vk = uint16(ret & 0xFF)
	}

	if vk == 0 {
		log.Printf("[InputInjector] Unknown key: %s", input.Key)
		return
	}

	// Get scan code for the virtual key
	scanRet, _, _ := procMapVirtualKey.Call(uintptr(vk), 0)
	scan = uint16(scanRet)

	// Check for extended keys
	isExtended := false
	switch vk {
	case 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x2D, 0x2E:
		isExtended = true
	}

	if isExtended {
		flags |= KEYEVENTF_EXTENDEDKEY
	}

	// Build keyboard input
	kbdInput := INPUT_KBD{
		Type: INPUT_TYPE_KEYBOARD,
		Ki: KEYBDINPUT{
			Vk:          vk,
			Scan:        scan,
			DwFlags:     flags,
			Time:        0,
			DwExtraInfo: 0,
		},
	}

	switch input.Event {
	case "down":
		procSendInput.Call(1, uintptr(unsafe.Pointer(&kbdInput)), uintptr(unsafe.Sizeof(kbdInput)))
	case "up":
		kbdInput.Ki.DwFlags |= KEYEVENTF_KEYUP
		procSendInput.Call(1, uintptr(unsafe.Pointer(&kbdInput)), uintptr(unsafe.Sizeof(kbdInput)))
	}

	log.Printf("[InputInjector] Keyboard: event=%s, key=%s, vk=0x%X", input.Event, input.Key, vk)
}
