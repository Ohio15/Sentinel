//go:build (linux && arm64) || (linux && arm)

package capture

import "errors"

// Headless stub for ARM Linux (Synology NAS, Raspberry Pi, etc.)
// Screen capture not available without X11 display

// ErrHeadlessMode is returned when capture is attempted in headless mode
var ErrHeadlessMode = errors.New("screen capture not available in headless mode")

// X11Capture stub for headless systems
type X11Capture struct{}

// NewX11Capture returns a stub capturer for headless systems
func NewX11Capture(displayName string) (*X11Capture, error) {
	return nil, ErrHeadlessMode
}

// NewDXGICapture is an alias for NewX11Capture on Linux
func NewDXGICapture(monitorIndex int) (*X11Capture, error) {
	return nil, ErrHeadlessMode
}

// CaptureFrame returns an error in headless mode
func (c *X11Capture) CaptureFrame(timeoutMs int) (*CapturedFrame, error) {
	return nil, ErrHeadlessMode
}

// GetCursor returns empty cursor data in headless mode
func (c *X11Capture) GetCursor() CursorData {
	return CursorData{}
}

// GetDimensions returns 0,0 in headless mode
func (c *X11Capture) GetDimensions() (int, int) {
	return 0, 0
}

// Release is a no-op in headless mode
func (c *X11Capture) Release() {}

// DXGICapture is an alias for X11Capture on Linux
type DXGICapture = X11Capture
