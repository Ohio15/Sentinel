//go:build (linux && arm64) || (linux && arm)

package capture

import "errors"

// Headless stub for ARM Linux (Synology NAS, etc.)
// Screen capture not available without X11 display

var ErrHeadlessMode = errors.New("screen capture not available in headless mode")

// X11Capture stub for headless systems
type X11Capture struct{}

// NewX11Capture returns a stub capturer for headless systems
func NewX11Capture() (*X11Capture, error) {
	return nil, ErrHeadlessMode
}

func (c *X11Capture) CaptureFrame() ([]byte, int, int, error) {
	return nil, 0, 0, ErrHeadlessMode
}

func (c *X11Capture) GetDimensions() (int, int) {
	return 0, 0
}

func (c *X11Capture) Close() error {
	return nil
}
