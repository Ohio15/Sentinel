package peripheral

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"
)

// EventCallback is called when a USB device event occurs
type EventCallback func(event *DeviceEvent)

// Manager manages peripheral device monitoring
type Manager struct {
	detector      USBDetector
	callback      EventCallback
	pollInterval  time.Duration
	lastDevices   map[string]*USBDevice
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	running       bool
	initialScan   bool
}

// NewManager creates a new peripheral manager
func NewManager(callback EventCallback) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	// Create platform-specific detector
	// Note: NewUSBDetector() is defined in each platform-specific file
	var detector USBDetector
	detector = newPlatformDetector()

	return &Manager{
		detector:     detector,
		callback:     callback,
		pollInterval: 5 * time.Second,
		lastDevices:  make(map[string]*USBDevice),
		ctx:          ctx,
		cancel:       cancel,
		initialScan:  true,
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
			event := &DeviceEvent{
				EventType: "connected",
				Device:    current,
				Timestamp: time.Now(),
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

			event := &DeviceEvent{
				EventType: "disconnected",
				Device:    prev,
				Timestamp: disconnectTime,
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
		log.Printf("[Peripheral] USB device connected (real-time): %s (%s %s)",
			event.Device.DeviceID, event.Device.Manufacturer, event.Device.ProductName)

	case "disconnected":
		if prev, exists := m.lastDevices[event.Device.DeviceID]; exists {
			disconnectTime := time.Now()
			prev.IsConnected = false
			prev.DisconnectionTime = &disconnectTime
			event.Device = prev
		}
		delete(m.lastDevices, event.Device.DeviceID)
		log.Printf("[Peripheral] USB device disconnected (real-time): %s",
			event.Device.DeviceID)
	}

	if m.callback != nil {
		go m.callback(event)
	}
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
