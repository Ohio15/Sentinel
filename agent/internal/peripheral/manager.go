package peripheral

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sentinel/agent/internal/filewatcher"
)

// EventCallback is called when a USB device event occurs
type EventCallback func(event *DeviceEvent)

// usbSession tracks an active USB mass storage session
type usbSession struct {
	sessionID string
	deviceID  string
	watcher   filewatcher.Watcher
	startTime time.Time
}

// Manager manages peripheral device monitoring
type Manager struct {
	detector       USBDetector
	callback       EventCallback
	pollInterval   time.Duration
	lastDevices    map[string]*USBDevice
	activeSessions map[string]*usbSession // keyed by device ID
	mu             sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	running        bool
	initialScan    bool
}

// NewManager creates a new peripheral manager
func NewManager(callback EventCallback) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	// Create platform-specific detector
	// Note: NewUSBDetector() is defined in each platform-specific file
	var detector USBDetector
	detector = newPlatformDetector()

	return &Manager{
		detector:       detector,
		callback:       callback,
		pollInterval:   5 * time.Second,
		lastDevices:    make(map[string]*USBDevice),
		activeSessions: make(map[string]*usbSession),
		ctx:            ctx,
		cancel:         cancel,
		initialScan:    true,
	}
}

// SetPollInterval sets the polling interval for change detection
func (m *Manager) SetPollInterval(interval time.Duration) {
	m.mu.Lock()
	m.pollInterval = interval
	m.mu.Unlock()
}

// Start begins peripheral monitoring
func (m *Manager) Start() error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = true
	m.mu.Unlock()

	if m.detector == nil {
		log.Println("[Peripheral] No USB detector available for this platform")
		return nil
	}

	// Perform initial scan
	m.performScan()

	// Start monitoring
	go m.monitorLoop()

	log.Printf("[Peripheral] USB device monitoring started (poll interval: %v)", m.pollInterval)
	return nil
}

// Stop stops peripheral monitoring
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	m.running = false
	m.cancel()

	// Stop all active file watchers
	for deviceID := range m.activeSessions {
		m.stopFileWatcher(deviceID)
	}

	if m.detector != nil {
		m.detector.StopMonitoring()
	}

	log.Println("[Peripheral] USB device monitoring stopped")
}

// GetConnectedDevices returns all currently connected USB devices
func (m *Manager) GetConnectedDevices() []*USBDevice {
	m.mu.RLock()
	defer m.mu.RUnlock()

	devices := make([]*USBDevice, 0, len(m.lastDevices))
	for _, device := range m.lastDevices {
		if device.IsConnected {
			devices = append(devices, device)
		}
	}
	return devices
}

// GetInventory returns a complete peripheral inventory
func (m *Manager) GetInventory() *PeripheralInventory {
	devices := m.GetConnectedDevices()
	usbDevices := make([]USBDevice, len(devices))
	for i, d := range devices {
		usbDevices[i] = *d
	}

	return &PeripheralInventory{
		USBDevices:    usbDevices,
		TotalUSBPorts: 0, // Could be enumerated but not essential
		Timestamp:     time.Now(),
	}
}

// GetDeviceJSON returns JSON representation of connected devices
func (m *Manager) GetDeviceJSON() ([]byte, error) {
	devices := m.GetConnectedDevices()
	return json.Marshal(devices)
}

// monitorLoop runs the background monitoring
func (m *Manager) monitorLoop() {
	// Try to start real-time monitoring first
	eventChan := make(chan *DeviceEvent, 100)
	go func() {
		for event := range eventChan {
			m.handleEvent(event)
		}
	}()

	if m.detector != nil {
		err := m.detector.StartMonitoring(m.ctx, eventChan)
		if err != nil {
			log.Printf("[Peripheral] Real-time monitoring failed, using polling: %v", err)
		}
	}

	// Fallback polling loop for change detection
	m.mu.RLock()
	interval := m.pollInterval
	m.mu.RUnlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			close(eventChan)
			return
		case <-ticker.C:
			m.performScan()
		}
	}
}

// performScan scans for USB devices and detects changes
func (m *Manager) performScan() {
	if m.detector == nil {
		return
	}

	devices, err := m.detector.EnumerateDevices()
	if err != nil {
		log.Printf("[Peripheral] Failed to enumerate devices: %v", err)
		return
	}

	// Build map of current devices
	currentDevices := make(map[string]*USBDevice)
	for _, device := range devices {
		d := device // Create copy to avoid pointer issues
		currentDevices[d.DeviceID] = &d
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Detect new/changed devices
	for id, current := range currentDevices {
		if prev, exists := m.lastDevices[id]; exists {
			// Check for changes (e.g., mount point changed)
			if hasDeviceChanged(prev, current) {
				event := &DeviceEvent{
					EventType:     "changed",
					Device:        current,
					Timestamp:     time.Now(),
					PreviousState: prev,
				}
				if !m.initialScan && m.callback != nil {
					go m.callback(event)
				}
			}
		} else {
			// New device connected
			sessionID := ""

			// Start file watcher for mass storage devices with drive letter
			if current.DeviceClass == ClassMassStorage && current.DriveLetter != "" {
				sessionID = uuid.New().String()
				m.startFileWatcher(current, sessionID)
			}

			event := &DeviceEvent{
				EventType: "connected",
				Device:    current,
				Timestamp: time.Now(),
				SessionID: sessionID,
			}
			if !m.initialScan && m.callback != nil {
				go m.callback(event)
			}
			log.Printf("[Peripheral] USB device connected: %s (%s %s)",
				current.DeviceID, current.Manufacturer, current.ProductName)
		}
	}

	// Detect disconnected devices
	for id, prev := range m.lastDevices {
		if _, exists := currentDevices[id]; !exists {
			// Device disconnected
			disconnectTime := time.Now()
			prev.IsConnected = false
			prev.DisconnectionTime = &disconnectTime

			// Stop file watcher and collect transfers
			var fileTransfers []FileTransfer
			sessionID := ""
			if session, ok := m.activeSessions[id]; ok {
				sessionID = session.sessionID
				fileTransfers = m.stopFileWatcher(id)
			}

			event := &DeviceEvent{
				EventType:     "disconnected",
				Device:        prev,
				Timestamp:     disconnectTime,
				SessionID:     sessionID,
				FileTransfers: fileTransfers,
			}
			if !m.initialScan && m.callback != nil {
				go m.callback(event)
			}
			log.Printf("[Peripheral] USB device disconnected: %s (%s %s)",
				prev.DeviceID, prev.Manufacturer, prev.ProductName)
		}
	}

	// Update last devices map
	m.lastDevices = currentDevices
	m.initialScan = false
}

// handleEvent processes events from real-time monitoring
func (m *Manager) handleEvent(event *DeviceEvent) {
	if event == nil || event.Device == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	switch event.EventType {
	case "connected":
		m.lastDevices[event.Device.DeviceID] = event.Device

		// Start file watcher for mass storage devices with drive letter
		if event.Device.DeviceClass == ClassMassStorage && event.Device.DriveLetter != "" {
			sessionID := uuid.New().String()
			m.startFileWatcher(event.Device, sessionID)
			event.SessionID = sessionID
		}

		log.Printf("[Peripheral] USB device connected (real-time): %s (%s %s)",
			event.Device.DeviceID, event.Device.Manufacturer, event.Device.ProductName)

	case "disconnected":
		if prev, exists := m.lastDevices[event.Device.DeviceID]; exists {
			disconnectTime := time.Now()
			prev.IsConnected = false
			prev.DisconnectionTime = &disconnectTime
			event.Device = prev
		}

		// Stop file watcher and collect transfers
		if session, ok := m.activeSessions[event.Device.DeviceID]; ok {
			event.SessionID = session.sessionID
			event.FileTransfers = m.stopFileWatcher(event.Device.DeviceID)
		}

		delete(m.lastDevices, event.Device.DeviceID)
		log.Printf("[Peripheral] USB device disconnected (real-time): %s",
			event.Device.DeviceID)
	}

	if m.callback != nil {
		go m.callback(event)
	}
}

// startFileWatcher starts monitoring files on a USB mass storage device
// Must be called with m.mu held
func (m *Manager) startFileWatcher(device *USBDevice, sessionID string) {
	if device.DriveLetter == "" {
		return
	}

	// Build the path (e.g., "E:\")
	watchPath := device.DriveLetter
	if !hasTrailingSlash(watchPath) {
		watchPath += "\\"
	}

	watcher := filewatcher.NewWithDefaults(watchPath)
	if err := watcher.Start(m.ctx); err != nil {
		log.Printf("[Peripheral] Failed to start file watcher for %s: %v", watchPath, err)
		return
	}

	m.activeSessions[device.DeviceID] = &usbSession{
		sessionID: sessionID,
		deviceID:  device.DeviceID,
		watcher:   watcher,
		startTime: time.Now(),
	}

	log.Printf("[Peripheral] Started file watcher for USB device %s at %s (session: %s)",
		device.DeviceID, watchPath, sessionID)
}

// stopFileWatcher stops monitoring a USB device and returns accumulated file transfers
// Must be called with m.mu held
func (m *Manager) stopFileWatcher(deviceID string) []FileTransfer {
	session, exists := m.activeSessions[deviceID]
	if !exists {
		return nil
	}

	// Stop the watcher and get file transfers
	watcherTransfers := session.watcher.Stop()

	// Convert filewatcher.FileTransfer to peripheral.FileTransfer
	transfers := make([]FileTransfer, len(watcherTransfers))
	for i, t := range watcherTransfers {
		transfers[i] = FileTransfer{
			FileName:     t.FileName,
			FilePath:     t.FilePath,
			FileSize:     t.FileSize,
			TransferTime: t.TransferTime,
			Operation:    t.Operation,
		}
	}

	delete(m.activeSessions, deviceID)

	log.Printf("[Peripheral] Stopped file watcher for device %s (session: %s, files: %d)",
		deviceID, session.sessionID, len(transfers))

	return transfers
}

// hasTrailingSlash checks if a path ends with a slash
func hasTrailingSlash(path string) bool {
	if len(path) == 0 {
		return false
	}
	lastChar := path[len(path)-1]
	return lastChar == '/' || lastChar == '\\'
}

// hasDeviceChanged checks if a device's properties have changed
func hasDeviceChanged(prev, current *USBDevice) bool {
	if prev == nil || current == nil {
		return true
	}

	// Check for meaningful changes
	if prev.MountPoint != current.MountPoint {
		return true
	}
	if prev.DriveLetter != current.DriveLetter {
		return true
	}
	if prev.VolumeLabel != current.VolumeLabel {
		return true
	}
	if prev.TotalSize != current.TotalSize {
		return true
	}
	if prev.IsConnected != current.IsConnected {
		return true
	}

	return false
}

// ClassifySecurityRisk returns a security risk assessment for a device
func ClassifySecurityRisk(device *USBDevice) string {
	if device == nil {
		return "unknown"
	}

	// High risk: mass storage devices
	if device.DeviceClass == ClassMassStorage {
		if device.IsBootable {
			return "critical" // Bootable USB drives are highest risk
		}
		return "high"
	}

	// Medium risk: network adapters (could be rogue)
	if device.DeviceClass == ClassNetwork || device.DeviceClass == ClassWireless {
		return "medium"
	}

	// Low risk: HID devices (keyboards, mice)
	if device.DeviceClass == ClassHID {
		// Could still be BadUSB, but generally lower risk
		return "low"
	}

	// Informational: other devices
	return "info"
}

// IsDeviceApproved checks if a device is in an approved list
func IsDeviceApproved(device *USBDevice, approvedVendors, approvedProducts, approvedSerials []string) bool {
	if device == nil {
		return false
	}

	// Check serial number first (most specific)
	for _, serial := range approvedSerials {
		if device.SerialNumber != "" && device.SerialNumber == serial {
			return true
		}
	}

	// Check VID:PID combination
	vidPid := device.VendorID + ":" + device.ProductID
	for _, product := range approvedProducts {
		if product == vidPid {
			return true
		}
	}

	// Check vendor ID
	for _, vendor := range approvedVendors {
		if device.VendorID == vendor {
			return true
		}
	}

	return false
}
