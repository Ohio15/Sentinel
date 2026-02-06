//go:build windows
// +build windows

package desktop

import (
	"encoding/json"
	"log"
	"sync"
	"syscall"
	"unsafe"
)

// Windows API
var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
	procEnumDisplayDevicesW = user32.NewProc("EnumDisplayDevicesW")
)

// RECT structure
type RECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// MONITORINFOEXW structure
type MONITORINFOEXW struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
	SzDevice  [32]uint16
}

// DISPLAY_DEVICEW structure
type DISPLAY_DEVICEW struct {
	Cb           uint32
	DeviceName   [32]uint16
	DeviceString [128]uint16
	StateFlags   uint32
	DeviceID     [128]uint16
	DeviceKey    [128]uint16
}

// Display device state flags
const (
	DISPLAY_DEVICE_ATTACHED_TO_DESKTOP = 0x00000001
	DISPLAY_DEVICE_PRIMARY_DEVICE      = 0x00000004
	DISPLAY_DEVICE_ACTIVE              = 0x00000001
)

// Monitor represents a display monitor
type Monitor struct {
	Index        int    `json:"index"`
	ID           string `json:"id"`           // Device ID
	Name         string `json:"name"`         // Device name
	Description  string `json:"description"`  // Friendly name
	Left         int    `json:"left"`         // Left edge in virtual screen
	Top          int    `json:"top"`          // Top edge in virtual screen
	Width        int    `json:"width"`        // Monitor width
	Height       int    `json:"height"`       // Monitor height
	IsPrimary    bool   `json:"isPrimary"`    // Is primary monitor
	IsActive     bool   `json:"isActive"`     // Is currently active
	WorkLeft     int    `json:"workLeft"`     // Work area (excludes taskbar)
	WorkTop      int    `json:"workTop"`
	WorkWidth    int    `json:"workWidth"`
	WorkHeight   int    `json:"workHeight"`
	ScaleFactor  int    `json:"scaleFactor"`  // DPI scale (100, 125, 150, etc.)
	Orientation  int    `json:"orientation"`  // 0=landscape, 1=portrait, etc.
}

// MonitorLayout describes the complete monitor configuration
type MonitorLayout struct {
	Monitors       []Monitor `json:"monitors"`
	PrimaryIndex   int       `json:"primaryIndex"`
	VirtualLeft    int       `json:"virtualLeft"`   // Virtual screen bounds
	VirtualTop     int       `json:"virtualTop"`
	VirtualWidth   int       `json:"virtualWidth"`
	VirtualHeight  int       `json:"virtualHeight"`
	ActiveMonitor  int       `json:"activeMonitor"` // Currently selected monitor
}

// MonitorSelectionCallback is called when monitor selection changes
type MonitorSelectionCallback func(monitor *Monitor)

// MonitorManager handles multi-monitor enumeration and selection
type MonitorManager struct {
	layout           MonitorLayout
	onSelectionChange MonitorSelectionCallback
	onLayoutChange   func(layout *MonitorLayout)
	mu               sync.RWMutex
}

// NewMonitorManager creates a new monitor manager
func NewMonitorManager() *MonitorManager {
	mm := &MonitorManager{}
	mm.RefreshLayout()
	return mm
}

// RefreshLayout updates the monitor layout information
func (mm *MonitorManager) RefreshLayout() {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.layout.Monitors = nil

	// Get virtual screen bounds
	procGetSystemMetrics := user32.NewProc("GetSystemMetrics")
	left, _, _ := procGetSystemMetrics.Call(76)  // SM_XVIRTUALSCREEN
	top, _, _ := procGetSystemMetrics.Call(77)   // SM_YVIRTUALSCREEN
	width, _, _ := procGetSystemMetrics.Call(78) // SM_CXVIRTUALSCREEN
	height, _, _ := procGetSystemMetrics.Call(79) // SM_CYVIRTUALSCREEN

	mm.layout.VirtualLeft = int(left)
	mm.layout.VirtualTop = int(top)
	mm.layout.VirtualWidth = int(width)
	mm.layout.VirtualHeight = int(height)

	// Enumerate display devices first to get friendly names
	deviceNames := make(map[string]string)
	var deviceIndex uint32 = 0
	for {
		var device DISPLAY_DEVICEW
		device.Cb = uint32(unsafe.Sizeof(device))

		ret, _, _ := procEnumDisplayDevicesW.Call(
			0,
			uintptr(deviceIndex),
			uintptr(unsafe.Pointer(&device)),
			0,
		)

		if ret == 0 {
			break
		}

		if device.StateFlags&DISPLAY_DEVICE_ATTACHED_TO_DESKTOP != 0 {
			deviceName := syscall.UTF16ToString(device.DeviceName[:])
			friendlyName := syscall.UTF16ToString(device.DeviceString[:])
			deviceNames[deviceName] = friendlyName
		}

		deviceIndex++
	}

	// Enumerate monitors
	monitorIndex := 0
	callback := syscall.NewCallback(func(hMonitor, hdcMonitor, lpRect, dwData uintptr) uintptr {
		info := MONITORINFOEXW{}
		info.CbSize = uint32(unsafe.Sizeof(info))

		ret, _, _ := procGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&info)))
		if ret != 0 {
			isPrimary := (info.DwFlags & 1) != 0 // MONITORINFOF_PRIMARY
			deviceName := syscall.UTF16ToString(info.SzDevice[:])
			friendlyName := deviceNames[deviceName]
			if friendlyName == "" {
				friendlyName = deviceName
			}

			monitor := Monitor{
				Index:       monitorIndex,
				ID:          deviceName,
				Name:        deviceName,
				Description: friendlyName,
				Left:        int(info.RcMonitor.Left),
				Top:         int(info.RcMonitor.Top),
				Width:       int(info.RcMonitor.Right - info.RcMonitor.Left),
				Height:      int(info.RcMonitor.Bottom - info.RcMonitor.Top),
				IsPrimary:   isPrimary,
				IsActive:    true,
				WorkLeft:    int(info.RcWork.Left),
				WorkTop:     int(info.RcWork.Top),
				WorkWidth:   int(info.RcWork.Right - info.RcWork.Left),
				WorkHeight:  int(info.RcWork.Bottom - info.RcWork.Top),
				ScaleFactor: 100, // Default, would need GetDpiForMonitor for accurate value
				Orientation: 0,   // Default landscape
			}

			if isPrimary {
				mm.layout.PrimaryIndex = monitorIndex
			}

			mm.layout.Monitors = append(mm.layout.Monitors, monitor)
			monitorIndex++
		}
		return 1 // Continue enumeration
	})

	procEnumDisplayMonitors.Call(0, 0, callback, 0)

	// Set default active monitor to primary
	if len(mm.layout.Monitors) > 0 {
		mm.layout.ActiveMonitor = mm.layout.PrimaryIndex
	}

	log.Printf("[MonitorManager] Found %d monitor(s), primary=%d, virtual screen=(%d,%d) %dx%d",
		len(mm.layout.Monitors), mm.layout.PrimaryIndex,
		mm.layout.VirtualLeft, mm.layout.VirtualTop,
		mm.layout.VirtualWidth, mm.layout.VirtualHeight)

	for _, m := range mm.layout.Monitors {
		log.Printf("[MonitorManager] Monitor %d: %s (%s), %dx%d at (%d,%d), primary=%v",
			m.Index, m.Name, m.Description, m.Width, m.Height, m.Left, m.Top, m.IsPrimary)
	}
}

// GetLayout returns the current monitor layout
func (mm *MonitorManager) GetLayout() *MonitorLayout {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	layout := mm.layout
	monitors := make([]Monitor, len(mm.layout.Monitors))
	copy(monitors, mm.layout.Monitors)
	layout.Monitors = monitors

	return &layout
}

// GetMonitor returns information about a specific monitor
func (mm *MonitorManager) GetMonitor(index int) *Monitor {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	if index < 0 || index >= len(mm.layout.Monitors) {
		return nil
	}

	monitor := mm.layout.Monitors[index]
	return &monitor
}

// GetActiveMonitor returns the currently selected monitor
func (mm *MonitorManager) GetActiveMonitor() *Monitor {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	return mm.GetMonitor(mm.layout.ActiveMonitor)
}

// SelectMonitor selects a monitor for capture
func (mm *MonitorManager) SelectMonitor(index int) error {
	mm.mu.Lock()

	if index < 0 || index >= len(mm.layout.Monitors) {
		mm.mu.Unlock()
		return ErrMonitorNotFound
	}

	mm.layout.ActiveMonitor = index
	monitor := mm.layout.Monitors[index]
	callback := mm.onSelectionChange

	mm.mu.Unlock()

	log.Printf("[MonitorManager] Selected monitor %d: %s (%dx%d)", index, monitor.Name, monitor.Width, monitor.Height)

	if callback != nil {
		callback(&monitor)
	}

	return nil
}

// SelectPrimary selects the primary monitor
func (mm *MonitorManager) SelectPrimary() error {
	mm.mu.RLock()
	primaryIndex := mm.layout.PrimaryIndex
	mm.mu.RUnlock()

	return mm.SelectMonitor(primaryIndex)
}

// SelectAll selects all monitors (virtual screen)
func (mm *MonitorManager) SelectAll() error {
	mm.mu.Lock()
	mm.layout.ActiveMonitor = -1 // -1 indicates all monitors
	mm.mu.Unlock()

	log.Printf("[MonitorManager] Selected all monitors (virtual screen)")

	mm.mu.RLock()
	callback := mm.onSelectionChange
	mm.mu.RUnlock()

	if callback != nil {
		// Create a synthetic monitor representing the entire virtual screen
		virtualMonitor := &Monitor{
			Index:       -1,
			ID:          "virtual",
			Name:        "All Monitors",
			Description: "Virtual Screen (All Monitors)",
			Left:        mm.layout.VirtualLeft,
			Top:         mm.layout.VirtualTop,
			Width:       mm.layout.VirtualWidth,
			Height:      mm.layout.VirtualHeight,
			IsPrimary:   false,
			IsActive:    true,
		}
		callback(virtualMonitor)
	}

	return nil
}

// GetCaptureRect returns the capture rectangle for the current selection
func (mm *MonitorManager) GetCaptureRect() (left, top, width, height int) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	if mm.layout.ActiveMonitor == -1 {
		// All monitors
		return mm.layout.VirtualLeft, mm.layout.VirtualTop, mm.layout.VirtualWidth, mm.layout.VirtualHeight
	}

	if mm.layout.ActiveMonitor >= 0 && mm.layout.ActiveMonitor < len(mm.layout.Monitors) {
		m := mm.layout.Monitors[mm.layout.ActiveMonitor]
		return m.Left, m.Top, m.Width, m.Height
	}

	// Default to primary
	if mm.layout.PrimaryIndex >= 0 && mm.layout.PrimaryIndex < len(mm.layout.Monitors) {
		m := mm.layout.Monitors[mm.layout.PrimaryIndex]
		return m.Left, m.Top, m.Width, m.Height
	}

	// Fallback to virtual screen
	return mm.layout.VirtualLeft, mm.layout.VirtualTop, mm.layout.VirtualWidth, mm.layout.VirtualHeight
}

// OnSelectionChange sets the callback for monitor selection changes
func (mm *MonitorManager) OnSelectionChange(callback MonitorSelectionCallback) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.onSelectionChange = callback
}

// OnLayoutChange sets the callback for layout changes
func (mm *MonitorManager) OnLayoutChange(callback func(*MonitorLayout)) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.onLayoutChange = callback
}

// MonitorAtPoint returns the monitor containing the given point
func (mm *MonitorManager) MonitorAtPoint(x, y int) *Monitor {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	for i := range mm.layout.Monitors {
		m := &mm.layout.Monitors[i]
		if x >= m.Left && x < m.Left+m.Width &&
			y >= m.Top && y < m.Top+m.Height {
			return m
		}
	}

	return nil
}

// Common errors
var (
	ErrMonitorNotFound = errorString("monitor not found")
)

type errorString string

func (e errorString) Error() string {
	return string(e)
}

// Message types for monitor protocol
const (
	MonitorMsgGetLayout   = "monitor.getLayout"
	MonitorMsgLayout      = "monitor.layout"
	MonitorMsgSelect      = "monitor.select"
	MonitorMsgSelectAck   = "monitor.selectAck"
	MonitorMsgRefresh     = "monitor.refresh"
)

// MonitorGetLayoutMessage requests the current layout
type MonitorGetLayoutMessage struct {
	Type string `json:"type"`
}

// MonitorLayoutMessage contains the monitor layout
type MonitorLayoutMessage struct {
	Type   string         `json:"type"`
	Layout *MonitorLayout `json:"layout"`
}

// MonitorSelectMessage requests a monitor selection
type MonitorSelectMessage struct {
	Type  string `json:"type"`
	Index int    `json:"index"` // -1 for all monitors
}

// MonitorSelectAckMessage acknowledges a selection
type MonitorSelectAckMessage struct {
	Type    string   `json:"type"`
	Success bool     `json:"success"`
	Monitor *Monitor `json:"monitor,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// HandleMessage processes monitor-related messages
func (mm *MonitorManager) HandleMessage(msgType string, data []byte) ([]byte, error) {
	switch msgType {
	case MonitorMsgGetLayout:
		layout := mm.GetLayout()
		resp := MonitorLayoutMessage{
			Type:   MonitorMsgLayout,
			Layout: layout,
		}
		return json.Marshal(resp)

	case MonitorMsgSelect:
		var msg MonitorSelectMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}

		var err error
		if msg.Index == -1 {
			err = mm.SelectAll()
		} else {
			err = mm.SelectMonitor(msg.Index)
		}

		resp := MonitorSelectAckMessage{
			Type:    MonitorMsgSelectAck,
			Success: err == nil,
		}
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Monitor = mm.GetActiveMonitor()
		}
		return json.Marshal(resp)

	case MonitorMsgRefresh:
		mm.RefreshLayout()
		layout := mm.GetLayout()
		resp := MonitorLayoutMessage{
			Type:   MonitorMsgLayout,
			Layout: layout,
		}
		return json.Marshal(resp)
	}

	return nil, nil
}
