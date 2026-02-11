//go:build linux && !cgo && !arm64 && !arm

package capture

import "errors"

// No-CGO stub for Linux x86/amd64 when CGO is not available
// This is used for cross-compilation scenarios
// Screen capture requires X11 libraries which need CGO

// ErrNoCGO is returned when capture is attempted without CGO
var ErrNoCGO = errors.New("screen capture requires CGO (X11 libraries)")

// X11Capture stub for non-CGO builds
type X11Capture struct{}

// NewX11Capture returns an error when CGO is not available
func NewX11Capture(displayName string) (*X11Capture, error) {
	return nil, ErrNoCGO
}

// NewDXGICapture is an alias for NewX11Capture on Linux
func NewDXGICapture(monitorIndex int) (*X11Capture, error) {
	return nil, ErrNoCGO
}

// CaptureFrame returns an error without CGO
func (c *X11Capture) CaptureFrame(timeoutMs int) (*CapturedFrame, error) {
	return nil, ErrNoCGO
}

// GetCursor returns empty cursor data without CGO
func (c *X11Capture) GetCursor() CursorData {
	return CursorData{}
}

// GetDimensions returns 0,0 without CGO
func (c *X11Capture) GetDimensions() (int, int) {
	return 0, 0
}

// Release is a no-op without CGO
func (c *X11Capture) Release() {}

// DXGICapture is an alias for X11Capture on Linux
type DXGICapture = X11Capture
