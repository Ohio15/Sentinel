package peripheral

import (
	"context"
	"time"
)

// USBDetector is the interface for platform-specific USB detection
type USBDetector interface {
	// EnumerateDevices returns all currently connected USB devices
	EnumerateDevices() ([]USBDevice, error)

	// StartMonitoring begins monitoring for USB device events
	// Events are sent to the provided channel
	StartMonitoring(ctx context.Context, eventChan chan *DeviceEvent) error

	// StopMonitoring stops monitoring for USB device events
	StopMonitoring()
}

// DeviceType represents the type of peripheral device
type DeviceType string

const (
	DeviceTypeUSB       DeviceType = "usb"
	DeviceTypeBluetooth DeviceType = "bluetooth"
	DeviceTypeThunderbolt DeviceType = "thunderbolt"
	DeviceTypePCIe      DeviceType = "pcie"
)

// DeviceClass represents USB device class
type DeviceClass string

const (
	ClassMassStorage   DeviceClass = "mass_storage"
	ClassHID           DeviceClass = "hid"           // Keyboard, mouse
	ClassAudio         DeviceClass = "audio"
	ClassVideo         DeviceClass = "video"
	ClassPrinter       DeviceClass = "printer"
	ClassNetwork       DeviceClass = "network"
	ClassHub           DeviceClass = "hub"
	ClassSmartCard     DeviceClass = "smart_card"
	ClassWireless      DeviceClass = "wireless"
	ClassMiscellaneous DeviceClass = "miscellaneous"
	ClassUnknown       DeviceClass = "unknown"
)

// USBDevice represents a connected USB device
type USBDevice struct {
	// Identification
	DeviceID     string `json:"deviceId"`     // Unique identifier (VID:PID:Serial or instance path)
	InstancePath string `json:"instancePath"` // Windows: device instance path, Linux: sysfs path
	VendorID     string `json:"vendorId"`     // USB Vendor ID (e.g., "0x8086")
	ProductID    string `json:"productId"`    // USB Product ID (e.g., "0x1234")
	SerialNumber string `json:"serialNumber"` // Device serial number if available

	// Descriptive info
	Manufacturer string      `json:"manufacturer"`
	ProductName  string      `json:"productName"`
	DeviceClass  DeviceClass `json:"deviceClass"`
	ClassCode    int         `json:"classCode"`    // Raw USB class code
	SubclassCode int         `json:"subclassCode"` // Raw USB subclass code
	ProtocolCode int         `json:"protocolCode"` // Raw USB protocol code

	// Connection details
	BusNumber    int    `json:"busNumber"`
	PortNumber   int    `json:"portNumber"`
	DeviceSpeed  string `json:"deviceSpeed"` // low, full, high, super, super+
	ParentDevice string `json:"parentDevice"` // Parent hub if any

	// Drive info (for mass storage)
	DriveLetter  string `json:"driveLetter,omitempty"`  // Windows only
	MountPoint   string `json:"mountPoint,omitempty"`   // Linux mount point
	VolumeLabel  string `json:"volumeLabel,omitempty"`
	FileSystem   string `json:"fileSystem,omitempty"`
	TotalSize    int64  `json:"totalSize,omitempty"`    // Bytes
	FreeSpace    int64  `json:"freeSpace,omitempty"`    // Bytes

	// State
	IsConnected      bool      `json:"isConnected"`
	ConnectionTime   time.Time `json:"connectionTime"`
	DisconnectionTime *time.Time `json:"disconnectionTime,omitempty"`

	// Security flags
	IsRemovable bool `json:"isRemovable"`
	IsBootable  bool `json:"isBootable"`
	IsEncrypted bool `json:"isEncrypted"`
}

// FileTransfer represents a file written to a USB drive
type FileTransfer struct {
	FileName     string    `json:"fileName"`
	FilePath     string    `json:"filePath"`     // Relative path on USB
	FileSize     int64     `json:"fileSize"`
	TransferTime time.Time `json:"transferTime"`
	Operation    string    `json:"operation"`    // write, rename, copy
}

// DeviceEvent represents a peripheral connection/disconnection event
type DeviceEvent struct {
	EventType     string         `json:"eventType"` // connected, disconnected, changed
	Device        *USBDevice     `json:"device"`
	Timestamp     time.Time      `json:"timestamp"`
	PreviousState *USBDevice     `json:"previousState,omitempty"` // For change events
	SessionID     string         `json:"sessionId,omitempty"`     // Unique session ID for USB connection
	FileTransfers []FileTransfer `json:"fileTransfers,omitempty"` // Files transferred during session (on disconnect)
}

// PeripheralInventory contains all connected peripherals
type PeripheralInventory struct {
	USBDevices    []USBDevice `json:"usbDevices"`
	TotalUSBPorts int         `json:"totalUsbPorts"`
	Timestamp     time.Time   `json:"timestamp"`
}

// DevicePolicy defines rules for allowed/blocked devices
type DevicePolicy struct {
	AllowedVendors  []string `json:"allowedVendors"`  // VID whitelist
	BlockedVendors  []string `json:"blockedVendors"`  // VID blacklist
	AllowedProducts []string `json:"allowedProducts"` // VID:PID whitelist
	BlockedProducts []string `json:"blockedProducts"` // VID:PID blacklist
	AllowedSerials  []string `json:"allowedSerials"`  // Serial whitelist
	BlockedClasses  []DeviceClass `json:"blockedClasses"` // Block by device class
	AllowMassStorage bool     `json:"allowMassStorage"`
	AllowHID         bool     `json:"allowHid"`
	AlertOnNew       bool     `json:"alertOnNew"` // Alert on any new device
}

// GetDeviceClassFromCode returns the DeviceClass for a USB class code
func GetDeviceClassFromCode(classCode int) DeviceClass {
	switch classCode {
	case 0x00:
		return ClassUnknown // Defined at interface level
	case 0x01:
		return ClassAudio
	case 0x03:
		return ClassHID
	case 0x06:
		return ClassVideo // Still Image
	case 0x07:
		return ClassPrinter
	case 0x08:
		return ClassMassStorage
	case 0x09:
		return ClassHub
	case 0x0A:
		return ClassNetwork // CDC-Data
	case 0x0B:
		return ClassSmartCard
	case 0x0E:
		return ClassVideo
	case 0xE0:
		return ClassWireless
	case 0xEF:
		return ClassMiscellaneous
	default:
		return ClassUnknown
	}
}

// GetSpeedString returns human-readable USB speed
func GetSpeedString(speedMbps int) string {
	switch {
	case speedMbps <= 1:
		return "low"
	case speedMbps <= 12:
		return "full"
	case speedMbps <= 480:
		return "high"
	case speedMbps <= 5000:
		return "super"
	case speedMbps <= 10000:
		return "super+"
	case speedMbps <= 20000:
		return "super+2"
	default:
		return "unknown"
	}
}
