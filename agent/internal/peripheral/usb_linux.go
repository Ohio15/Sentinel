//go:build linux

package peripheral

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LinuxUSBDetector implements USB detection for Linux
type LinuxUSBDetector struct {
	mu           sync.RWMutex
	knownDevices map[string]*USBDevice
	eventChan    chan DeviceEvent
	stopChan     chan struct{}
	pollInterval time.Duration
	isRunning    bool
	udevMonitor  *exec.Cmd
}

// NewUSBDetector creates a new Linux USB detector
func NewUSBDetector() *LinuxUSBDetector {
	return &LinuxUSBDetector{
		knownDevices: make(map[string]*USBDevice),
		eventChan:    make(chan DeviceEvent, 100),
		stopChan:     make(chan struct{}),
		pollInterval: 5 * time.Second,
	}
}

// newPlatformDetector creates the platform-specific USB detector (Linux version)
func newPlatformDetector() USBDetector {
	return NewUSBDetector()
}

// Start begins USB device monitoring
func (d *LinuxUSBDetector) Start(ctx context.Context) error {
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

	// Try to start udev monitor for real-time events
	go d.startUdevMonitor(ctx)

	// Fallback: polling for changes
	go d.pollLoop(ctx)

	return nil
}

// Stop stops USB device monitoring
func (d *LinuxUSBDetector) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.isRunning {
		return
	}
	d.isRunning = false
	close(d.stopChan)

	if d.udevMonitor != nil && d.udevMonitor.Process != nil {
		d.udevMonitor.Process.Kill()
	}
}

// StartMonitoring implements USBDetector interface - starts monitoring with event channel
func (d *LinuxUSBDetector) StartMonitoring(ctx context.Context, eventChan chan *DeviceEvent) error {
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
func (d *LinuxUSBDetector) StopMonitoring() {
	d.Stop()
}

// Events returns the event channel
func (d *LinuxUSBDetector) Events() <-chan DeviceEvent {
	return d.eventChan
}

// GetConnectedDevices returns currently connected USB devices
func (d *LinuxUSBDetector) GetConnectedDevices() []USBDevice {
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
func (d *LinuxUSBDetector) EnumerateDevices() ([]USBDevice, error) {
	devices := []USBDevice{}

	// Method 1: Parse /sys/bus/usb/devices
	sysfsDevices, err := d.parseSysfsUSB()
	if err != nil {
		log.Printf("[USB] sysfs parse failed: %v", err)
	} else {
		devices = append(devices, sysfsDevices...)
	}

	// Method 2: Get mount info for storage devices
	d.enrichWithMountInfo(devices)

	// Method 3: Use lsusb for additional details
	d.enrichWithLsusb(devices)

	return devices, nil
}

// parseSysfsUSB reads USB device info from /sys/bus/usb/devices
func (d *LinuxUSBDetector) parseSysfsUSB() ([]USBDevice, error) {
	devices := []USBDevice{}

	usbPath := "/sys/bus/usb/devices"
	entries, err := os.ReadDir(usbPath)
	if err != nil {
		return nil, fmt.Errorf("read sysfs: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		// Skip root hubs and interfaces (only want devices like "1-1", "2-1.2")
		if strings.HasPrefix(name, "usb") || strings.Contains(name, ":") {
			continue
		}

		devicePath := filepath.Join(usbPath, name)
		dev, err := d.parseUSBDevice(devicePath, name)
		if err != nil {
			continue
		}

		// Skip hubs
		if dev.DeviceClass == ClassHub && dev.ClassCode == 9 {
			continue
		}

		devices = append(devices, *dev)
	}

	return devices, nil
}

// parseUSBDevice reads a single USB device from sysfs
func (d *LinuxUSBDetector) parseUSBDevice(devicePath, name string) (*USBDevice, error) {
	dev := &USBDevice{
		InstancePath:   devicePath,
		IsConnected:    true,
		ConnectionTime: time.Now(),
		IsRemovable:    true,
	}

	// Read vendor ID
	if vid, err := d.readSysfsFile(filepath.Join(devicePath, "idVendor")); err == nil {
		dev.VendorID = "0x" + strings.ToUpper(strings.TrimSpace(vid))
	}

	// Read product ID
	if pid, err := d.readSysfsFile(filepath.Join(devicePath, "idProduct")); err == nil {
		dev.ProductID = "0x" + strings.ToUpper(strings.TrimSpace(pid))
	}

	// Read manufacturer
	if mfr, err := d.readSysfsFile(filepath.Join(devicePath, "manufacturer")); err == nil {
		dev.Manufacturer = strings.TrimSpace(mfr)
	}

	// Read product name
	if prod, err := d.readSysfsFile(filepath.Join(devicePath, "product")); err == nil {
		dev.ProductName = strings.TrimSpace(prod)
	}

	// Read serial number
	if serial, err := d.readSysfsFile(filepath.Join(devicePath, "serial")); err == nil {
		dev.SerialNumber = strings.TrimSpace(serial)
	}

	// Read device class
	if class, err := d.readSysfsFile(filepath.Join(devicePath, "bDeviceClass")); err == nil {
		classCode, _ := strconv.ParseInt(strings.TrimSpace(class), 16, 32)
		dev.ClassCode = int(classCode)
		dev.DeviceClass = GetDeviceClassFromCode(dev.ClassCode)
	}

	// Read subclass
	if subclass, err := d.readSysfsFile(filepath.Join(devicePath, "bDeviceSubClass")); err == nil {
		subclassCode, _ := strconv.ParseInt(strings.TrimSpace(subclass), 16, 32)
		dev.SubclassCode = int(subclassCode)
	}

	// Read speed
	if speed, err := d.readSysfsFile(filepath.Join(devicePath, "speed")); err == nil {
		speedMbps, _ := strconv.Atoi(strings.TrimSpace(speed))
		dev.DeviceSpeed = GetSpeedString(speedMbps)
	}

	// Read bus and device numbers
	if busnum, err := d.readSysfsFile(filepath.Join(devicePath, "busnum")); err == nil {
		dev.BusNumber, _ = strconv.Atoi(strings.TrimSpace(busnum))
	}
	if devnum, err := d.readSysfsFile(filepath.Join(devicePath, "devnum")); err == nil {
		dev.PortNumber, _ = strconv.Atoi(strings.TrimSpace(devnum))
	}

	// Check if it's a mass storage device by looking for block devices
	blockPath := filepath.Join(devicePath, "*", "*", "block")
	if matches, _ := filepath.Glob(blockPath); len(matches) > 0 {
		dev.DeviceClass = ClassMassStorage
	}

	// Generate unique device ID
	if dev.SerialNumber != "" {
		dev.DeviceID = fmt.Sprintf("%s:%s:%s", dev.VendorID, dev.ProductID, dev.SerialNumber)
	} else {
		dev.DeviceID = fmt.Sprintf("%s:%s:%s", dev.VendorID, dev.ProductID, name)
	}

	return dev, nil
}

// readSysfsFile reads a sysfs file
func (d *LinuxUSBDetector) readSysfsFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// enrichWithMountInfo adds mount information for storage devices
func (d *LinuxUSBDetector) enrichWithMountInfo(devices []USBDevice) {
	mounts, err := d.parseMounts()
	if err != nil {
		return
	}

	for i := range devices {
		if devices[i].DeviceClass != ClassMassStorage {
			continue
		}

		// Find block device for this USB device
		blockDevices := d.findBlockDevices(devices[i].InstancePath)
		for _, blockDev := range blockDevices {
			if mount, ok := mounts[blockDev]; ok {
				devices[i].MountPoint = mount.mountPoint
				devices[i].FileSystem = mount.fsType
				devices[i].VolumeLabel = mount.label

				// Get disk space
				if stat, err := d.getDiskSpace(mount.mountPoint); err == nil {
					devices[i].TotalSize = stat.total
					devices[i].FreeSpace = stat.free
				}
				break
			}
		}
	}
}

type mountInfo struct {
	mountPoint string
	fsType     string
	label      string
}

// parseMounts reads /proc/mounts
func (d *LinuxUSBDetector) parseMounts() (map[string]mountInfo, error) {
	mounts := make(map[string]mountInfo)

	file, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}

		device := fields[0]
		mountPoint := fields[1]
		fsType := fields[2]

		// Only track USB-related mounts (typically /dev/sd*)
		if strings.HasPrefix(device, "/dev/sd") {
			mounts[device] = mountInfo{
				mountPoint: mountPoint,
				fsType:     fsType,
			}
		}
	}

	return mounts, nil
}

// findBlockDevices finds block devices associated with a USB device
func (d *LinuxUSBDetector) findBlockDevices(usbPath string) []string {
	var devices []string

	// Look for block devices in the USB device path
	pattern := filepath.Join(usbPath, "*", "*", "block", "*")
	matches, _ := filepath.Glob(pattern)
	for _, match := range matches {
		devName := filepath.Base(match)
		devices = append(devices, "/dev/"+devName)

		// Also check for partitions
		partPattern := filepath.Join(match, devName+"*")
		partMatches, _ := filepath.Glob(partPattern)
		for _, partMatch := range partMatches {
			partName := filepath.Base(partMatch)
			devices = append(devices, "/dev/"+partName)
		}
	}

	return devices
}

type diskSpace struct {
	total int64
	free  int64
}

// getDiskSpace gets disk space for a mount point
func (d *LinuxUSBDetector) getDiskSpace(mountPoint string) (*diskSpace, error) {
	cmd := exec.Command("df", "-B1", mountPoint)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("unexpected df output")
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return nil, fmt.Errorf("unexpected df output format")
	}

	total, _ := strconv.ParseInt(fields[1], 10, 64)
	free, _ := strconv.ParseInt(fields[3], 10, 64)

	return &diskSpace{total: total, free: free}, nil
}

// enrichWithLsusb uses lsusb for additional device details
func (d *LinuxUSBDetector) enrichWithLsusb(devices []USBDevice) {
	cmd := exec.Command("lsusb", "-v")
	output, err := cmd.Output()
	if err != nil {
		return
	}

	// Parse lsusb output for additional details
	// This is a simplified parser
	lines := strings.Split(string(output), "\n")
	var currentVidPid string
	var currentMfr, currentProd string

	for _, line := range lines {
		if strings.HasPrefix(line, "Bus ") {
			// New device entry
			match := regexp.MustCompile(`ID ([0-9a-f]{4}):([0-9a-f]{4})`).FindStringSubmatch(line)
			if len(match) > 2 {
				currentVidPid = fmt.Sprintf("0x%s:0x%s", strings.ToUpper(match[1]), strings.ToUpper(match[2]))
			}
			currentMfr = ""
			currentProd = ""
		} else if strings.Contains(line, "iManufacturer") {
			parts := strings.SplitN(line, " ", 4)
			if len(parts) >= 4 {
				currentMfr = strings.TrimSpace(parts[3])
			}
		} else if strings.Contains(line, "iProduct") {
			parts := strings.SplitN(line, " ", 4)
			if len(parts) >= 4 {
				currentProd = strings.TrimSpace(parts[3])
			}
		}

		// Update devices with matching VID:PID
		for i := range devices {
			vidPid := fmt.Sprintf("%s:%s", devices[i].VendorID, devices[i].ProductID)
			if vidPid == currentVidPid {
				if currentMfr != "" && devices[i].Manufacturer == "" {
					devices[i].Manufacturer = currentMfr
				}
				if currentProd != "" && devices[i].ProductName == "" {
					devices[i].ProductName = currentProd
				}
			}
		}
	}
}

// startUdevMonitor starts monitoring udev events for real-time detection
func (d *LinuxUSBDetector) startUdevMonitor(ctx context.Context) {
	// Try to use udevadm monitor for real-time events
	cmd := exec.CommandContext(ctx, "udevadm", "monitor", "--kernel", "--subsystem-match=usb")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("[USB] Failed to start udev monitor: %v", err)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("[USB] Failed to start udev monitor: %v", err)
		return
	}

	d.mu.Lock()
	d.udevMonitor = cmd
	d.mu.Unlock()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "add") || strings.Contains(line, "remove") {
			// Device change detected, trigger a scan
			go d.checkForChanges()
		}
	}

	cmd.Wait()
}

// pollLoop continuously monitors for device changes
func (d *LinuxUSBDetector) pollLoop(ctx context.Context) {
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
func (d *LinuxUSBDetector) checkForChanges() {
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
func (d *LinuxUSBDetector) GetInventory() (*PeripheralInventory, error) {
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
func (d *LinuxUSBDetector) countUSBPorts() int {
	// Count USB host controllers
	pattern := "/sys/bus/usb/devices/usb*"
	matches, _ := filepath.Glob(pattern)
	return len(matches)
}
