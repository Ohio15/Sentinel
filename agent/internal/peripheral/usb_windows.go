//go:build windows

package peripheral

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// WindowsUSBDetector implements USB detection for Windows
type WindowsUSBDetector struct {
	mu              sync.RWMutex
	knownDevices    map[string]*USBDevice
	eventChan       chan DeviceEvent
	stopChan        chan struct{}
	pollInterval    time.Duration
	isRunning       bool
}

// NewUSBDetector creates a new Windows USB detector
func NewUSBDetector() *WindowsUSBDetector {
	return &WindowsUSBDetector{
		knownDevices: make(map[string]*USBDevice),
		eventChan:    make(chan DeviceEvent, 100),
		stopChan:     make(chan struct{}),
		pollInterval: 5 * time.Second,
	}
}

// newPlatformDetector creates the platform-specific USB detector (Windows version)
func newPlatformDetector() USBDetector {
	return NewUSBDetector()
}

// Start begins USB device monitoring
func (d *WindowsUSBDetector) Start(ctx context.Context) error {
	d.mu.Lock()
	if d.isRunning {
		d.mu.Unlock()
		return nil
	}
	d.isRunning = true
	d.mu.Unlock()

	// Initial scan
	devices, err := d.EnumerateDevices()
	if err != nil {
		log.Printf("[USB] Initial enumeration failed: %v", err)
	} else {
		d.mu.Lock()
		for _, dev := range devices {
			devCopy := dev
			d.knownDevices[dev.DeviceID] = &devCopy
		}
		d.mu.Unlock()
		log.Printf("[USB] Initial scan found %d devices", len(devices))
	}

	// Start polling for changes
	go d.pollLoop(ctx)

	return nil
}

// Stop stops USB device monitoring
func (d *WindowsUSBDetector) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.isRunning {
		return
	}
	d.isRunning = false
	close(d.stopChan)
}

// StartMonitoring implements USBDetector interface - starts monitoring with event channel
func (d *WindowsUSBDetector) StartMonitoring(ctx context.Context, eventChan chan *DeviceEvent) error {
	// Set up external event channel and start
	go func() {
		for event := range d.eventChan {
			select {
			case eventChan <- &event:
			default:
				// Channel full, skip
			}
		}
	}()
	return d.Start(ctx)
}

// StopMonitoring implements USBDetector interface
func (d *WindowsUSBDetector) StopMonitoring() {
	d.Stop()
}

// Events returns the event channel
func (d *WindowsUSBDetector) Events() <-chan DeviceEvent {
	return d.eventChan
}

// GetConnectedDevices returns currently connected USB devices
func (d *WindowsUSBDetector) GetConnectedDevices() []USBDevice {
	d.mu.RLock()
	defer d.mu.RUnlock()

	devices := make([]USBDevice, 0, len(d.knownDevices))
	for _, dev := range d.knownDevices {
		if dev.IsConnected {
			devices = append(devices, *dev)
		}
	}
	return devices
}

// EnumerateDevices scans for all connected USB devices
func (d *WindowsUSBDetector) EnumerateDevices() ([]USBDevice, error) {
	devices := []USBDevice{}

	// Method 1: WMI query for USB devices
	wmiDevices, err := d.queryWMIUSBDevices()
	if err != nil {
		log.Printf("[USB] WMI query failed: %v", err)
	} else {
		devices = append(devices, wmiDevices...)
	}

	// Method 2: Query USB storage devices specifically
	storageDevices, err := d.queryUSBStorageDevices()
	if err != nil {
		log.Printf("[USB] Storage query failed: %v", err)
	} else {
		// Merge storage info into existing devices
		d.mergeStorageInfo(devices, storageDevices)
	}

	// Method 3: Registry enumeration for additional details
	d.enrichFromRegistry(devices)

	return devices, nil
}

// queryWMIUSBDevices uses WMI to get USB device information
func (d *WindowsUSBDetector) queryWMIUSBDevices() ([]USBDevice, error) {
	devices := []USBDevice{}

	// Query Win32_USBControllerDevice to get USB device associations
	cmd := exec.Command("powershell", "-NoProfile", "-Command", `
		Get-CimInstance -ClassName Win32_PnPEntity | Where-Object {
			$_.DeviceID -like 'USB*' -or $_.DeviceID -like 'USBSTOR*'
		} | Select-Object DeviceID, Caption, Description, Manufacturer, Status, PNPClass, ClassGuid |
		ConvertTo-Json -Compress
	`)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("powershell query failed: %w", err)
	}

	// Parse the JSON output
	devices = d.parseWMIOutput(string(output))

	return devices, nil
}

// parseWMIOutput parses PowerShell JSON output into USBDevice structs
func (d *WindowsUSBDetector) parseWMIOutput(output string) []USBDevice {
	devices := []USBDevice{}

	// Handle empty or null output
	output = strings.TrimSpace(output)
	if output == "" || output == "null" {
		return devices
	}

	// Simple JSON parsing (avoiding external dependency)
	// Split by device entries
	lines := strings.Split(output, "},{")

	for _, line := range lines {
		line = strings.Trim(line, "[]{}")
		if line == "" {
			continue
		}

		dev := USBDevice{
			IsConnected:    true,
			ConnectionTime: time.Now(),
			IsRemovable:    true,
		}

		// Extract DeviceID
		if match := regexp.MustCompile(`"DeviceID"\s*:\s*"([^"]+)"`).FindStringSubmatch(line); len(match) > 1 {
			dev.InstancePath = strings.ReplaceAll(match[1], "\\\\", "\\")
			dev.DeviceID = dev.InstancePath

			// Parse VID and PID from DeviceID
			vidPid := regexp.MustCompile(`VID_([0-9A-Fa-f]{4})&PID_([0-9A-Fa-f]{4})`)
			if m := vidPid.FindStringSubmatch(dev.InstancePath); len(m) > 2 {
				dev.VendorID = "0x" + strings.ToUpper(m[1])
				dev.ProductID = "0x" + strings.ToUpper(m[2])
			}

			// Extract serial number if present
			serialMatch := regexp.MustCompile(`\\([^\\]+)$`).FindStringSubmatch(dev.InstancePath)
			if len(serialMatch) > 1 && !strings.Contains(serialMatch[1], "&") {
				dev.SerialNumber = serialMatch[1]
			}
		}

		// Extract Caption/ProductName
		if match := regexp.MustCompile(`"Caption"\s*:\s*"([^"]+)"`).FindStringSubmatch(line); len(match) > 1 {
			dev.ProductName = match[1]
		}

		// Extract Manufacturer
		if match := regexp.MustCompile(`"Manufacturer"\s*:\s*"([^"]+)"`).FindStringSubmatch(line); len(match) > 1 {
			dev.Manufacturer = match[1]
		}

		// Extract PNPClass to determine device class
		if match := regexp.MustCompile(`"PNPClass"\s*:\s*"([^"]+)"`).FindStringSubmatch(line); len(match) > 1 {
			dev.DeviceClass = d.pnpClassToDeviceClass(match[1])
		}

		// Determine if this is a storage device
		if strings.Contains(dev.InstancePath, "USBSTOR") {
			dev.DeviceClass = ClassMassStorage
		}

		// Skip hubs and root hubs for cleaner output
		if strings.Contains(strings.ToLower(dev.ProductName), "root hub") {
			continue
		}

		// Generate unique device ID
		if dev.SerialNumber != "" {
			dev.DeviceID = fmt.Sprintf("%s:%s:%s", dev.VendorID, dev.ProductID, dev.SerialNumber)
		} else if dev.VendorID != "" && dev.ProductID != "" {
			dev.DeviceID = fmt.Sprintf("%s:%s:%s", dev.VendorID, dev.ProductID, dev.InstancePath)
		}

		if dev.DeviceID != "" {
			devices = append(devices, dev)
		}
	}

	return devices
}

// queryUSBStorageDevices gets USB storage device details
func (d *WindowsUSBDetector) queryUSBStorageDevices() ([]USBDevice, error) {
	devices := []USBDevice{}

	cmd := exec.Command("powershell", "-NoProfile", "-Command", `
		Get-CimInstance -ClassName Win32_DiskDrive | Where-Object { $_.InterfaceType -eq 'USB' } |
		ForEach-Object {
			$disk = $_
			$partitions = Get-CimAssociatedInstance -InputObject $disk -ResultClassName Win32_DiskPartition
			foreach ($partition in $partitions) {
				$logicalDisks = Get-CimAssociatedInstance -InputObject $partition -ResultClassName Win32_LogicalDisk
				foreach ($logicalDisk in $logicalDisks) {
					[PSCustomObject]@{
						DeviceID = $disk.DeviceID
						PNPDeviceID = $disk.PNPDeviceID
						Model = $disk.Model
						SerialNumber = $disk.SerialNumber
						Size = $disk.Size
						DriveLetter = $logicalDisk.DeviceID
						VolumeLabel = $logicalDisk.VolumeName
						FileSystem = $logicalDisk.FileSystem
						FreeSpace = $logicalDisk.FreeSpace
						TotalSize = $logicalDisk.Size
					}
				}
			}
		} | ConvertTo-Json -Compress
	`)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("storage query failed: %w", err)
	}

	// Parse storage devices
	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" || outputStr == "null" {
		return devices, nil
	}

	// Parse JSON entries
	entries := strings.Split(outputStr, "},{")
	for _, entry := range entries {
		entry = strings.Trim(entry, "[]{}")
		if entry == "" {
			continue
		}

		dev := USBDevice{
			DeviceClass:    ClassMassStorage,
			IsConnected:    true,
			ConnectionTime: time.Now(),
			IsRemovable:    true,
		}

		if match := regexp.MustCompile(`"PNPDeviceID"\s*:\s*"([^"]+)"`).FindStringSubmatch(entry); len(match) > 1 {
			dev.InstancePath = strings.ReplaceAll(match[1], "\\\\", "\\")
		}

		if match := regexp.MustCompile(`"Model"\s*:\s*"([^"]+)"`).FindStringSubmatch(entry); len(match) > 1 {
			dev.ProductName = match[1]
		}

		if match := regexp.MustCompile(`"SerialNumber"\s*:\s*"([^"]+)"`).FindStringSubmatch(entry); len(match) > 1 {
			dev.SerialNumber = strings.TrimSpace(match[1])
		}

		if match := regexp.MustCompile(`"DriveLetter"\s*:\s*"([^"]+)"`).FindStringSubmatch(entry); len(match) > 1 {
			dev.DriveLetter = match[1]
		}

		if match := regexp.MustCompile(`"VolumeLabel"\s*:\s*"([^"]+)"`).FindStringSubmatch(entry); len(match) > 1 {
			dev.VolumeLabel = match[1]
		}

		if match := regexp.MustCompile(`"FileSystem"\s*:\s*"([^"]+)"`).FindStringSubmatch(entry); len(match) > 1 {
			dev.FileSystem = match[1]
		}

		if match := regexp.MustCompile(`"FreeSpace"\s*:\s*(\d+)`).FindStringSubmatch(entry); len(match) > 1 {
			dev.FreeSpace, _ = strconv.ParseInt(match[1], 10, 64)
		}

		if match := regexp.MustCompile(`"TotalSize"\s*:\s*(\d+)`).FindStringSubmatch(entry); len(match) > 1 {
			dev.TotalSize, _ = strconv.ParseInt(match[1], 10, 64)
		}

		// Parse VID/PID from PNPDeviceID
		vidPid := regexp.MustCompile(`VID_([0-9A-Fa-f]{4})&PID_([0-9A-Fa-f]{4})`)
		if m := vidPid.FindStringSubmatch(dev.InstancePath); len(m) > 2 {
			dev.VendorID = "0x" + strings.ToUpper(m[1])
			dev.ProductID = "0x" + strings.ToUpper(m[2])
		}

		if dev.SerialNumber != "" {
			dev.DeviceID = fmt.Sprintf("%s:%s:%s", dev.VendorID, dev.ProductID, dev.SerialNumber)
		} else {
			dev.DeviceID = fmt.Sprintf("storage:%s", dev.InstancePath)
		}

		devices = append(devices, dev)
	}

	return devices, nil
}

// mergeStorageInfo merges storage device info into the main device list
func (d *WindowsUSBDetector) mergeStorageInfo(devices []USBDevice, storageDevices []USBDevice) {
	storageBySerial := make(map[string]*USBDevice)
	for i := range storageDevices {
		if storageDevices[i].SerialNumber != "" {
			storageBySerial[storageDevices[i].SerialNumber] = &storageDevices[i]
		}
	}

	for i := range devices {
		if devices[i].SerialNumber != "" {
			if storage, ok := storageBySerial[devices[i].SerialNumber]; ok {
				devices[i].DriveLetter = storage.DriveLetter
				devices[i].VolumeLabel = storage.VolumeLabel
				devices[i].FileSystem = storage.FileSystem
				devices[i].TotalSize = storage.TotalSize
				devices[i].FreeSpace = storage.FreeSpace
				devices[i].DeviceClass = ClassMassStorage
			}
		}
	}
}

// enrichFromRegistry adds additional info from Windows registry
func (d *WindowsUSBDetector) enrichFromRegistry(devices []USBDevice) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Enum\USB`, registry.READ)
	if err != nil {
		return
	}
	defer key.Close()

	// Read vendor names from USB.org database could be done here
	// For now, we rely on WMI data
}

// pnpClassToDeviceClass converts Windows PnP class to our DeviceClass
func (d *WindowsUSBDetector) pnpClassToDeviceClass(pnpClass string) DeviceClass {
	switch strings.ToLower(pnpClass) {
	case "diskdrive", "volume", "cdrom":
		return ClassMassStorage
	case "keyboard", "mouse", "hidclass":
		return ClassHID
	case "media", "audioendpoint":
		return ClassAudio
	case "camera", "image":
		return ClassVideo
	case "printer":
		return ClassPrinter
	case "net":
		return ClassNetwork
	case "usb":
		return ClassHub
	case "smartcardreader":
		return ClassSmartCard
	case "bluetooth":
		return ClassWireless
	default:
		return ClassUnknown
	}
}

// pollLoop continuously monitors for device changes
func (d *WindowsUSBDetector) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopChan:
			return
		case <-ticker.C:
			d.checkForChanges()
		}
	}
}

// checkForChanges compares current devices to known devices
func (d *WindowsUSBDetector) checkForChanges() {
	currentDevices, err := d.EnumerateDevices()
	if err != nil {
		log.Printf("[USB] Enumeration error during poll: %v", err)
		return
	}

	currentMap := make(map[string]*USBDevice)
	for i := range currentDevices {
		currentMap[currentDevices[i].DeviceID] = &currentDevices[i]
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Check for new devices
	for id, dev := range currentMap {
		if _, exists := d.knownDevices[id]; !exists {
			// New device connected
			devCopy := *dev
			d.knownDevices[id] = &devCopy

			log.Printf("[USB] Device connected: %s (%s)", dev.ProductName, dev.DeviceID)

			select {
			case d.eventChan <- DeviceEvent{
				EventType: "connected",
				Device:    &devCopy,
				Timestamp: time.Now(),
			}:
			default:
				log.Printf("[USB] Event channel full, dropping event")
			}
		}
	}

	// Check for removed devices
	for id, dev := range d.knownDevices {
		if _, exists := currentMap[id]; !exists && dev.IsConnected {
			// Device disconnected
			dev.IsConnected = false
			now := time.Now()
			dev.DisconnectionTime = &now

			log.Printf("[USB] Device disconnected: %s (%s)", dev.ProductName, dev.DeviceID)

			select {
			case d.eventChan <- DeviceEvent{
				EventType: "disconnected",
				Device:    dev,
				Timestamp: now,
			}:
			default:
				log.Printf("[USB] Event channel full, dropping event")
			}
		}
	}
}

// GetInventory returns a complete peripheral inventory
func (d *WindowsUSBDetector) GetInventory() (*PeripheralInventory, error) {
	devices, err := d.EnumerateDevices()
	if err != nil {
		return nil, err
	}

	return &PeripheralInventory{
		USBDevices:    devices,
		TotalUSBPorts: d.countUSBPorts(),
		Timestamp:     time.Now(),
	}, nil
}

// countUSBPorts counts available USB ports
func (d *WindowsUSBDetector) countUSBPorts() int {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-CimInstance -ClassName Win32_USBController).NumberOfPorts | Measure-Object -Sum | Select-Object -ExpandProperty Sum`)
	output, err := cmd.Output()
	if err != nil {
		return 0
	}
	count, _ := strconv.Atoi(strings.TrimSpace(string(output)))
	return count
}

// Ensure WindowsUSBDetector is safe for concurrent use
var _ = (*WindowsUSBDetector)(nil)

// Helper for windows API
func init() {
	// Ensure we can use Windows APIs
	_ = windows.ERROR_SUCCESS
}
