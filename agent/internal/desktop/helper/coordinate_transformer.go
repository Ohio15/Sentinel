//go:build windows
// +build windows

package helper

import (
	"log"
	"sync"
	"syscall"
	"unsafe"
)

// Windows API for display information
var (
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
)

// System metrics constants
const (
	SM_XVIRTUALSCREEN  = 76
	SM_YVIRTUALSCREEN  = 77
	SM_CXVIRTUALSCREEN = 78
	SM_CYVIRTUALSCREEN = 79
	SM_CXSCREEN        = 0
	SM_CYSCREEN        = 1
)

// MONITORINFOEXW structure
type MONITORINFOEXW struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
	SzDevice  [32]uint16
}

// RECT structure
type RECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// MonitorInfo describes a display monitor
type MonitorInfo struct {
	Index       int    // Monitor index (0-based)
	Name        string // Device name
	Left        int    // Left edge of monitor in virtual screen coords
	Top         int    // Top edge of monitor in virtual screen coords
	Width       int    // Monitor width
	Height      int    // Monitor height
	IsPrimary   bool   // Is this the primary monitor
	WorkLeft    int    // Work area left (excludes taskbar)
	WorkTop     int    // Work area top
	WorkWidth   int    // Work area width
	WorkHeight  int    // Work area height
	ScaleFactor float64 // DPI scale factor (1.0 = 100%)
}

// CoordinateTransformer handles coordinate transformation between viewer and host
type CoordinateTransformer struct {
	// Source dimensions (actual screen being captured)
	sourceWidth  int
	sourceHeight int
	sourceLeft   int // Virtual screen offset
	sourceTop    int

	// Viewer dimensions (video being displayed)
	viewerWidth  int
	viewerHeight int

	// Scale factors
	scaleX float64
	scaleY float64

	// Monitor info for multi-monitor support
	monitors      []MonitorInfo
	activeMonitor int

	mu sync.RWMutex
}

// NewCoordinateTransformer creates a new coordinate transformer
func NewCoordinateTransformer() *CoordinateTransformer {
	ct := &CoordinateTransformer{
		scaleX: 1.0,
		scaleY: 1.0,
	}

	// Initialize monitor info
	ct.RefreshMonitors()

	return ct
}

// RefreshMonitors updates the monitor information
func (ct *CoordinateTransformer) RefreshMonitors() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.monitors = nil

	// Callback for EnumDisplayMonitors
	monitorIndex := 0
	callback := syscall.NewCallback(func(hMonitor, hdcMonitor, lpRect, dwData uintptr) uintptr {
		info := MONITORINFOEXW{}
		info.CbSize = uint32(104) // sizeof(MONITORINFOEXW)

		ret, _, _ := procGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&info)))
		if ret != 0 {
			isPrimary := (info.DwFlags & 1) != 0 // MONITORINFOF_PRIMARY

			monitor := MonitorInfo{
				Index:      monitorIndex,
				Name:       syscall.UTF16ToString(info.SzDevice[:]),
				Left:       int(info.RcMonitor.Left),
				Top:        int(info.RcMonitor.Top),
				Width:      int(info.RcMonitor.Right - info.RcMonitor.Left),
				Height:     int(info.RcMonitor.Bottom - info.RcMonitor.Top),
				IsPrimary:  isPrimary,
				WorkLeft:   int(info.RcWork.Left),
				WorkTop:    int(info.RcWork.Top),
				WorkWidth:  int(info.RcWork.Right - info.RcWork.Left),
				WorkHeight: int(info.RcWork.Bottom - info.RcWork.Top),
				ScaleFactor: 1.0, // Would need GetDpiForMonitor for accurate value
			}

			ct.monitors = append(ct.monitors, monitor)
			monitorIndex++
		}
		return 1 // Continue enumeration
	})

	procEnumDisplayMonitors.Call(0, 0, callback, 0)

	log.Printf("[CoordinateTransformer] Found %d monitor(s)", len(ct.monitors))
	for _, m := range ct.monitors {
		log.Printf("[CoordinateTransformer] Monitor %d: %s, %dx%d at (%d,%d), primary=%v",
			m.Index, m.Name, m.Width, m.Height, m.Left, m.Top, m.IsPrimary)
	}
}

// GetMonitors returns the list of available monitors
func (ct *CoordinateTransformer) GetMonitors() []MonitorInfo {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	monitors := make([]MonitorInfo, len(ct.monitors))
	copy(monitors, ct.monitors)
	return monitors
}

// SetActiveMonitor sets which monitor is being captured
func (ct *CoordinateTransformer) SetActiveMonitor(index int) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if index < 0 || index >= len(ct.monitors) {
		log.Printf("[CoordinateTransformer] Invalid monitor index: %d", index)
		return
	}

	ct.activeMonitor = index
	monitor := ct.monitors[index]

	ct.sourceLeft = monitor.Left
	ct.sourceTop = monitor.Top
	ct.sourceWidth = monitor.Width
	ct.sourceHeight = monitor.Height

	ct.updateScaleFactors()

	log.Printf("[CoordinateTransformer] Active monitor %d: %dx%d at (%d,%d)",
		index, monitor.Width, monitor.Height, monitor.Left, monitor.Top)
}

// SetSourceDimensions sets the source (captured screen) dimensions
func (ct *CoordinateTransformer) SetSourceDimensions(width, height, left, top int) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.sourceWidth = width
	ct.sourceHeight = height
	ct.sourceLeft = left
	ct.sourceTop = top

	ct.updateScaleFactors()

	log.Printf("[CoordinateTransformer] Source dimensions: %dx%d at (%d,%d)",
		width, height, left, top)
}

// SetViewerDimensions sets the viewer (displayed video) dimensions
func (ct *CoordinateTransformer) SetViewerDimensions(width, height int) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.viewerWidth = width
	ct.viewerHeight = height

	ct.updateScaleFactors()

	log.Printf("[CoordinateTransformer] Viewer dimensions: %dx%d, scale=(%.4f, %.4f)",
		width, height, ct.scaleX, ct.scaleY)
}

// updateScaleFactors recalculates scale factors (must be called with lock held)
func (ct *CoordinateTransformer) updateScaleFactors() {
	if ct.viewerWidth > 0 && ct.sourceWidth > 0 {
		ct.scaleX = float64(ct.sourceWidth) / float64(ct.viewerWidth)
	} else {
		ct.scaleX = 1.0
	}

	if ct.viewerHeight > 0 && ct.sourceHeight > 0 {
		ct.scaleY = float64(ct.sourceHeight) / float64(ct.viewerHeight)
	} else {
		ct.scaleY = 1.0
	}
}

// TransformCoordinates converts viewer coordinates to screen coordinates
// viewerX, viewerY: coordinates in the viewer's video element
// Returns screen coordinates that can be used with SetCursorPos/SendInput
func (ct *CoordinateTransformer) TransformCoordinates(viewerX, viewerY float64) (screenX, screenY int) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	// Scale from viewer to source dimensions
	scaledX := viewerX * ct.scaleX
	scaledY := viewerY * ct.scaleY

	// Add source offset (for multi-monitor or captured region)
	screenX = int(scaledX) + ct.sourceLeft
	screenY = int(scaledY) + ct.sourceTop

	return screenX, screenY
}

// TransformCoordinatesWithClamp same as TransformCoordinates but clamps to source bounds
func (ct *CoordinateTransformer) TransformCoordinatesWithClamp(viewerX, viewerY float64) (screenX, screenY int) {
	screenX, screenY = ct.TransformCoordinates(viewerX, viewerY)

	ct.mu.RLock()
	defer ct.mu.RUnlock()

	// Clamp to source bounds
	minX := ct.sourceLeft
	maxX := ct.sourceLeft + ct.sourceWidth - 1
	minY := ct.sourceTop
	maxY := ct.sourceTop + ct.sourceHeight - 1

	if screenX < minX {
		screenX = minX
	}
	if screenX > maxX {
		screenX = maxX
	}
	if screenY < minY {
		screenY = minY
	}
	if screenY > maxY {
		screenY = maxY
	}

	return screenX, screenY
}

// InverseTransform converts screen coordinates to viewer coordinates
func (ct *CoordinateTransformer) InverseTransform(screenX, screenY int) (viewerX, viewerY float64) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	// Remove source offset
	relX := float64(screenX - ct.sourceLeft)
	relY := float64(screenY - ct.sourceTop)

	// Scale from source to viewer dimensions
	if ct.scaleX > 0 {
		viewerX = relX / ct.scaleX
	}
	if ct.scaleY > 0 {
		viewerY = relY / ct.scaleY
	}

	return viewerX, viewerY
}

// GetScaleFactors returns the current X and Y scale factors
func (ct *CoordinateTransformer) GetScaleFactors() (scaleX, scaleY float64) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.scaleX, ct.scaleY
}

// GetSourceDimensions returns the source dimensions
func (ct *CoordinateTransformer) GetSourceDimensions() (width, height, left, top int) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.sourceWidth, ct.sourceHeight, ct.sourceLeft, ct.sourceTop
}

// GetViewerDimensions returns the viewer dimensions
func (ct *CoordinateTransformer) GetViewerDimensions() (width, height int) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.viewerWidth, ct.viewerHeight
}

// IsInBounds checks if viewer coordinates are within valid bounds
func (ct *CoordinateTransformer) IsInBounds(viewerX, viewerY float64) bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	return viewerX >= 0 && viewerX < float64(ct.viewerWidth) &&
		viewerY >= 0 && viewerY < float64(ct.viewerHeight)
}

// GetVirtualScreenBounds returns the bounds of the virtual screen (all monitors)
func GetVirtualScreenBounds() (left, top, width, height int) {
	l, _, _ := procGetSystemMetrics.Call(SM_XVIRTUALSCREEN)
	t, _, _ := procGetSystemMetrics.Call(SM_YVIRTUALSCREEN)
	w, _, _ := procGetSystemMetrics.Call(SM_CXVIRTUALSCREEN)
	h, _, _ := procGetSystemMetrics.Call(SM_CYVIRTUALSCREEN)
	return int(l), int(t), int(w), int(h)
}

// GetPrimaryScreenDimensions returns the primary screen dimensions
func GetPrimaryScreenDimensions() (width, height int) {
	w, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	h, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
	return int(w), int(h)
}
