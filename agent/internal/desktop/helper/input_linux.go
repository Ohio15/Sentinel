//go:build linux

package helper

/*
#cgo LDFLAGS: -lX11 -lXtst
#include <X11/Xlib.h>
#include <X11/keysym.h>
#include <X11/XF86keysym.h>
#include <X11/extensions/XTest.h>
#include <stdlib.h>

// Helper function to find keycode for a keysym
static KeyCode get_keycode(Display *display, KeySym keysym) {
    return XKeysymToKeycode(display, keysym);
}

// X11 Keysym constants - defined here for cgo access
#define XK_SHIFT_L 0xffe1
#define XK_SHIFT_R 0xffe2
#define XK_CONTROL_L 0xffe3
#define XK_CONTROL_R 0xffe4
#define XK_ALT_L 0xffe9
#define XK_ALT_R 0xffea
#define XK_SUPER_L 0xffeb
#define XK_SUPER_R 0xffec
#define XK_UP 0xff52
#define XK_DOWN 0xff54
#define XK_LEFT 0xff51
#define XK_RIGHT 0xff53
#define XK_HOME 0xff50
#define XK_END 0xff57
#define XK_PAGE_UP 0xff55
#define XK_PAGE_DOWN 0xff56
#define XK_BACKSPACE 0xff08
#define XK_DELETE 0xffff
#define XK_INSERT 0xff63
#define XK_TAB 0xff09
#define XK_RETURN 0xff0d
#define XK_ESCAPE 0xff1b
#define XK_SPACE 0x0020
#define XK_F1 0xffbe
#define XK_F2 0xffbf
#define XK_F3 0xffc0
#define XK_F4 0xffc1
#define XK_F5 0xffc2
#define XK_F6 0xffc3
#define XK_F7 0xffc4
#define XK_F8 0xffc5
#define XK_F9 0xffc6
#define XK_F10 0xffc7
#define XK_F11 0xffc8
#define XK_F12 0xffc9
#define XK_CAPS_LOCK 0xffe5
#define XK_NUM_LOCK 0xff7f
#define XK_SCROLL_LOCK 0xff14
#define XK_PRINT 0xff61
#define XK_PAUSE 0xff13
*/
import "C"

import (
	"errors"
	"log"
	"sync"

	internalWebrtc "github.com/sentinel/agent/internal/webrtc"
)

// InputInjector handles injecting mouse and keyboard input on Linux using XTEST
type InputInjector struct {
	display       *C.Display
	rootWindow    C.Window
	screenWidth   int
	screenHeight  int

	// Coordinate transformation
	sourceWidth   int
	sourceHeight  int
	sourceLeft    int
	sourceTop     int
	viewerWidth   int
	viewerHeight  int
	useTransformer bool

	mu          sync.Mutex
	initialized bool
}

// MonitorInfo describes a monitor/display
type MonitorInfo struct {
	Index   int
	Name    string
	X       int
	Y       int
	Width   int
	Height  int
	Primary bool
}

// CoordinateTransformer handles coordinate mapping (simplified for Linux)
type CoordinateTransformer struct {
	sourceWidth  int
	sourceHeight int
	sourceLeft   int
	sourceTop    int
	viewerWidth  int
	viewerHeight int
	activeMonitor int
}

// NewCoordinateTransformer creates a new coordinate transformer
func NewCoordinateTransformer() *CoordinateTransformer {
	return &CoordinateTransformer{}
}

// SetSourceDimensions sets the source screen dimensions
func (t *CoordinateTransformer) SetSourceDimensions(width, height, left, top int) {
	t.sourceWidth = width
	t.sourceHeight = height
	t.sourceLeft = left
	t.sourceTop = top
}

// SetViewerDimensions sets the viewer dimensions
func (t *CoordinateTransformer) SetViewerDimensions(width, height int) {
	t.viewerWidth = width
	t.viewerHeight = height
}

// SetActiveMonitor sets the active monitor index
func (t *CoordinateTransformer) SetActiveMonitor(index int) {
	t.activeMonitor = index
}

// GetMonitors returns available monitors (placeholder)
func (t *CoordinateTransformer) GetMonitors() []MonitorInfo {
	return nil
}

// GetScaleFactors returns the current scale factors
func (t *CoordinateTransformer) GetScaleFactors() (scaleX, scaleY float64) {
	if t.viewerWidth == 0 || t.viewerHeight == 0 {
		return 1.0, 1.0
	}
	scaleX = float64(t.sourceWidth) / float64(t.viewerWidth)
	scaleY = float64(t.sourceHeight) / float64(t.viewerHeight)
	return
}

// TransformCoordinatesWithClamp transforms viewer coordinates to screen coordinates
func (t *CoordinateTransformer) TransformCoordinatesWithClamp(viewerX, viewerY float64) (screenX, screenY int) {
	scaleX, scaleY := t.GetScaleFactors()
	screenX = int(viewerX*scaleX) + t.sourceLeft
	screenY = int(viewerY*scaleY) + t.sourceTop

	// Clamp to source bounds
	if screenX < t.sourceLeft {
		screenX = t.sourceLeft
	}
	if screenX >= t.sourceLeft+t.sourceWidth {
		screenX = t.sourceLeft + t.sourceWidth - 1
	}
	if screenY < t.sourceTop {
		screenY = t.sourceTop
	}
	if screenY >= t.sourceTop+t.sourceHeight {
		screenY = t.sourceTop + t.sourceHeight - 1
	}
	return
}

// NewInputInjector creates a new input injector for Linux
func NewInputInjector() *InputInjector {
	i := &InputInjector{}

	if err := i.initialize(); err != nil {
		log.Printf("[InputInjector] Failed to initialize: %v", err)
		return nil
	}

	return i
}

func (i *InputInjector) initialize() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Open X display
	i.display = C.XOpenDisplay(nil)
	if i.display == nil {
		return errors.New("failed to open X display")
	}

	// Get default screen info
	screen := C.XDefaultScreen(i.display)
	i.rootWindow = C.XRootWindow(i.display, screen)
	i.screenWidth = int(C.XDisplayWidth(i.display, screen))
	i.screenHeight = int(C.XDisplayHeight(i.display, screen))

	// Check if XTEST extension is available
	var eventBase, errorBase, majorVersion, minorVersion C.int
	if C.XTestQueryExtension(i.display, &eventBase, &errorBase, &majorVersion, &minorVersion) == 0 {
		C.XCloseDisplay(i.display)
		return errors.New("XTEST extension not available")
	}

	log.Printf("[InputInjector] Initialized with screen %dx%d, XTEST v%d.%d",
		i.screenWidth, i.screenHeight, majorVersion, minorVersion)

	i.initialized = true
	return nil
}

// SetSourceDimensions configures the source (captured screen) dimensions
func (i *InputInjector) SetSourceDimensions(width, height, left, top int) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.sourceWidth = width
	i.sourceHeight = height
	i.sourceLeft = left
	i.sourceTop = top
	i.useTransformer = true
}

// SetViewerDimensions configures the viewer (displayed video) dimensions
func (i *InputInjector) SetViewerDimensions(width, height int) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.viewerWidth = width
	i.viewerHeight = height
	i.useTransformer = true
}

// SetBoundsOffset sets the screen coordinate offset (legacy API)
func (i *InputInjector) SetBoundsOffset(x, y int) {
	i.SetSourceDimensions(i.screenWidth, i.screenHeight, x, y)
}

// GetCoordinateTransformer returns a coordinate transformer
func (i *InputInjector) GetCoordinateTransformer() *CoordinateTransformer {
	t := NewCoordinateTransformer()
	t.SetSourceDimensions(i.sourceWidth, i.sourceHeight, i.sourceLeft, i.sourceTop)
	t.SetViewerDimensions(i.viewerWidth, i.viewerHeight)
	return t
}

// SetActiveMonitor sets which monitor is being captured
func (i *InputInjector) SetActiveMonitor(index int) {
	// Multi-monitor support would go here
}

// GetMonitors returns the list of available monitors
func (i *InputInjector) GetMonitors() []MonitorInfo {
	// Would use Xinerama or XRandR to get multi-monitor info
	return []MonitorInfo{
		{
			Index:   0,
			Name:    "Primary",
			Width:   i.screenWidth,
			Height:  i.screenHeight,
			Primary: true,
		},
	}
}

// InjectInput processes an input event and injects it into X11
func (i *InputInjector) InjectInput(input internalWebrtc.InputEvent) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if !i.initialized {
		return
	}

	switch input.Type {
	case "move", "mousemove":
		input.Event = "move"
		i.handleMouseInput(input)
	case "down", "mousedown":
		input.Event = "down"
		i.handleMouseInput(input)
	case "up", "mouseup":
		input.Event = "up"
		i.handleMouseInput(input)
	case "wheel":
		input.Event = "wheel"
		i.handleMouseInput(input)
	case "keydown":
		input.Event = "down"
		i.handleKeyboardInput(input)
	case "keyup":
		input.Event = "up"
		i.handleKeyboardInput(input)
	case "mouse":
		i.handleMouseInput(input)
	case "keyboard":
		i.handleKeyboardInput(input)
	case "ping":
		// Ignore heartbeat pings
	default:
		log.Printf("[InputInjector] Unknown input type: %s", input.Type)
	}
}

func (i *InputInjector) handleMouseInput(input internalWebrtc.InputEvent) {
	var screenX, screenY int

	if i.useTransformer && i.viewerWidth > 0 && i.viewerHeight > 0 {
		// Transform viewer coordinates to screen coordinates
		scaleX := float64(i.sourceWidth) / float64(i.viewerWidth)
		scaleY := float64(i.sourceHeight) / float64(i.viewerHeight)
		screenX = int(input.X*scaleX) + i.sourceLeft
		screenY = int(input.Y*scaleY) + i.sourceTop

		// Clamp to bounds
		if screenX < i.sourceLeft {
			screenX = i.sourceLeft
		}
		if screenX >= i.sourceLeft+i.sourceWidth {
			screenX = i.sourceLeft + i.sourceWidth - 1
		}
		if screenY < i.sourceTop {
			screenY = i.sourceTop
		}
		if screenY >= i.sourceTop+i.sourceHeight {
			screenY = i.sourceTop + i.sourceHeight - 1
		}
	} else {
		screenX = int(input.X) + i.sourceLeft
		screenY = int(input.Y) + i.sourceTop
	}

	switch input.Event {
	case "move":
		// Move pointer using XTEST
		C.XTestFakeMotionEvent(i.display, C.XDefaultScreen(i.display),
			C.int(screenX), C.int(screenY), 0)
		C.XFlush(i.display)

	case "down":
		// First move to position, then press button
		C.XTestFakeMotionEvent(i.display, C.XDefaultScreen(i.display),
			C.int(screenX), C.int(screenY), 0)

		button := i.mapButton(input.Button)
		C.XTestFakeButtonEvent(i.display, C.uint(button), C.True, 0)
		C.XFlush(i.display)

	case "up":
		button := i.mapButton(input.Button)
		C.XTestFakeButtonEvent(i.display, C.uint(button), C.False, 0)
		C.XFlush(i.display)

	case "wheel":
		// Move to position first
		C.XTestFakeMotionEvent(i.display, C.XDefaultScreen(i.display),
			C.int(screenX), C.int(screenY), 0)

		// X11 uses button 4 for scroll up, button 5 for scroll down
		if input.DeltaY != 0 {
			var button C.uint = 4 // Scroll up
			if input.DeltaY > 0 {
				button = 5 // Scroll down
			}
			// Send multiple clicks for larger deltas
			clicks := int(abs(input.DeltaY) / 30)
			if clicks < 1 {
				clicks = 1
			}
			for j := 0; j < clicks; j++ {
				C.XTestFakeButtonEvent(i.display, button, C.True, 0)
				C.XTestFakeButtonEvent(i.display, button, C.False, 0)
			}
		}
		C.XFlush(i.display)
	}
}

func (i *InputInjector) mapButton(button int) int {
	// X11 button mapping:
	// 1 = left, 2 = middle, 3 = right
	// Browser button mapping:
	// 0 = left, 1 = middle, 2 = right
	switch button {
	case 0:
		return 1 // Left
	case 1:
		return 2 // Middle
	case 2:
		return 3 // Right
	default:
		return 1
	}
}

func (i *InputInjector) handleKeyboardInput(input internalWebrtc.InputEvent) {
	keySym := i.keyToKeySym(input.Key)
	if keySym == 0 {
		log.Printf("[InputInjector] Unknown key: %s", input.Key)
		return
	}

	keyCode := C.get_keycode(i.display, keySym)
	if keyCode == 0 {
		log.Printf("[InputInjector] No keycode for keysym: 0x%x", keySym)
		return
	}

	switch input.Event {
	case "down":
		C.XTestFakeKeyEvent(i.display, C.uint(keyCode), C.True, 0)
	case "up":
		C.XTestFakeKeyEvent(i.display, C.uint(keyCode), C.False, 0)
	}
	C.XFlush(i.display)
}

func (i *InputInjector) keyToKeySym(key string) C.KeySym {
	// Map JavaScript key names to X11 keysyms
	keyMap := map[string]C.KeySym{
		// Modifier keys
		"Shift":        C.XK_SHIFT_L,
		"ShiftLeft":    C.XK_SHIFT_L,
		"ShiftRight":   C.XK_SHIFT_R,
		"Control":      C.XK_CONTROL_L,
		"ControlLeft":  C.XK_CONTROL_L,
		"ControlRight": C.XK_CONTROL_R,
		"Alt":          C.XK_ALT_L,
		"AltLeft":      C.XK_ALT_L,
		"AltRight":     C.XK_ALT_R,
		"Meta":         C.XK_SUPER_L,
		"MetaLeft":     C.XK_SUPER_L,
		"MetaRight":    C.XK_SUPER_R,

		// Navigation
		"ArrowUp":    C.XK_UP,
		"ArrowDown":  C.XK_DOWN,
		"ArrowLeft":  C.XK_LEFT,
		"ArrowRight": C.XK_RIGHT,
		"Home":       C.XK_HOME,
		"End":        C.XK_END,
		"PageUp":     C.XK_PAGE_UP,
		"PageDown":   C.XK_PAGE_DOWN,

		// Editing
		"Backspace": C.XK_BACKSPACE,
		"Delete":    C.XK_DELETE,
		"Insert":    C.XK_INSERT,
		"Tab":       C.XK_TAB,
		"Enter":     C.XK_RETURN,
		"Escape":    C.XK_ESCAPE,
		"Space":     C.XK_SPACE,

		// Function keys
		"F1":  C.XK_F1,
		"F2":  C.XK_F2,
		"F3":  C.XK_F3,
		"F4":  C.XK_F4,
		"F5":  C.XK_F5,
		"F6":  C.XK_F6,
		"F7":  C.XK_F7,
		"F8":  C.XK_F8,
		"F9":  C.XK_F9,
		"F10": C.XK_F10,
		"F11": C.XK_F11,
		"F12": C.XK_F12,

		// Lock keys
		"CapsLock":   C.XK_CAPS_LOCK,
		"NumLock":    C.XK_NUM_LOCK,
		"ScrollLock": C.XK_SCROLL_LOCK,

		// Other
		"PrintScreen": C.XK_PRINT,
		"Pause":       C.XK_PAUSE,
	}

	if sym, ok := keyMap[key]; ok {
		return sym
	}

	// Single character - convert to keysym
	if len(key) == 1 {
		r := []rune(key)[0]
		if r >= 'a' && r <= 'z' {
			return C.KeySym(r)
		}
		if r >= 'A' && r <= 'Z' {
			return C.KeySym(r)
		}
		if r >= '0' && r <= '9' {
			return C.KeySym(r)
		}
		// Other ASCII characters
		if r >= 32 && r < 127 {
			return C.KeySym(r)
		}
	}

	return 0
}

// Release frees all resources
func (i *InputInjector) Release() {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.display != nil {
		C.XCloseDisplay(i.display)
		i.display = nil
	}
	i.initialized = false
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
