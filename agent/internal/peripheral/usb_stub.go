//go:build !windows && !linux

package peripheral

import (
	"context"
	"fmt"
)

// StubUSBDetector is a no-op detector for unsupported platforms
type StubUSBDetector struct{}

// EnumerateDevices returns empty list on unsupported platforms
func (d *StubUSBDetector) EnumerateDevices() ([]USBDevice, error) {
	return []USBDevice{}, nil
}

// StartMonitoring is a no-op on unsupported platforms
func (d *StubUSBDetector) StartMonitoring(ctx context.Context, eventChan chan *DeviceEvent) error {
	return fmt.Errorf("USB detection not supported on this platform")
}

// StopMonitoring is a no-op on unsupported platforms
func (d *StubUSBDetector) StopMonitoring() {}

// newPlatformDetector returns a stub detector for unsupported platforms
func newPlatformDetector() USBDetector {
	return &StubUSBDetector{}
}
