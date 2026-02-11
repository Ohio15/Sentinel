//go:build linux && !cgo && !arm64 && !arm

package helper

import (
	"log"

	"github.com/sentinel/agent/internal/webrtc"
)

// No-CGO stub for Linux x86/amd64 when CGO is not available
// This is used for cross-compilation scenarios
// Input injection requires X11 libraries which need CGO

// MonitorInfo describes a monitor/display
type MonitorInfo struct {
	Index   int
	Name    string
	X       int
	Y       int
	Width   int
	Height  int
	Primary bool
}

// CoordinateTransformer handles coordinate mapping (stub for no-CGO)
type CoordinateTransformer struct {
	sourceWidth   int
	sourceHeight  int
	sourceLeft    int
	sourceTop     int
	viewerWidth   int
	viewerHeight  int
	activeMonitor int
}

// NewCoordinateTransformer creates a new coordinate transformer
func NewCoordinateTransformer() *CoordinateTransformer {
	return &CoordinateTransformer{}
}

// SetSourceDimensions sets the source screen dimensions
func (t *CoordinateTransformer) SetSourceDimensions(width, height, left, top int) {
	t.sourceWidth = width
	t.sourceHeight = height
	t.sourceLeft = left
	t.sourceTop = top
}

// SetViewerDimensions sets the viewer dimensions
func (t *CoordinateTransformer) SetViewerDimensions(width, height int) {
	t.viewerWidth = width
	t.viewerHeight = height
}

// SetActiveMonitor sets the active monitor index
func (t *CoordinateTransformer) SetActiveMonitor(index int) {
	t.activeMonitor = index
}

// GetMonitors returns available monitors (empty without CGO)
func (t *CoordinateTransformer) GetMonitors() []MonitorInfo {
	return nil
}

// GetScaleFactors returns the current scale factors
func (t *CoordinateTransformer) GetScaleFactors() (scaleX, scaleY float64) {
	if t.viewerWidth == 0 || t.viewerHeight == 0 {
		return 1.0, 1.0
	}
	scaleX = float64(t.sourceWidth) / float64(t.viewerWidth)
	scaleY = float64(t.sourceHeight) / float64(t.viewerHeight)
	return
}

// TransformCoordinatesWithClamp transforms viewer coordinates to screen coordinates
func (t *CoordinateTransformer) TransformCoordinatesWithClamp(viewerX, viewerY float64) (screenX, screenY int) {
	scaleX, scaleY := t.GetScaleFactors()
	screenX = int(viewerX*scaleX) + t.sourceLeft
	screenY = int(viewerY*scaleY) + t.sourceTop
	return
}

// InputInjector stub for no-CGO builds
type InputInjector struct {
	sourceWidth    int
	sourceHeight   int
	sourceLeft     int
	sourceTop      int
	viewerWidth    int
	viewerHeight   int
	useTransformer bool
}

// NewInputInjector creates a new input injector stub (no-CGO)
func NewInputInjector() *InputInjector {
	log.Printf("[InputInjector] No-CGO mode: input injection not available (requires X11 libraries)")
	return &InputInjector{}
}

// SetSourceDimensions configures the source (captured screen) dimensions
func (i *InputInjector) SetSourceDimensions(width, height, left, top int) {
	i.sourceWidth = width
	i.sourceHeight = height
	i.sourceLeft = left
	i.sourceTop = top
	i.useTransformer = true
}

// SetViewerDimensions configures the viewer (displayed video) dimensions
func (i *InputInjector) SetViewerDimensions(width, height int) {
	i.viewerWidth = width
	i.viewerHeight = height
	i.useTransformer = true
}

// SetBoundsOffset sets the screen coordinate offset (legacy API)
func (i *InputInjector) SetBoundsOffset(x, y int) {
	i.sourceLeft = x
	i.sourceTop = y
}

// GetCoordinateTransformer returns a coordinate transformer
func (i *InputInjector) GetCoordinateTransformer() *CoordinateTransformer {
	t := NewCoordinateTransformer()
	t.SetSourceDimensions(i.sourceWidth, i.sourceHeight, i.sourceLeft, i.sourceTop)
	t.SetViewerDimensions(i.viewerWidth, i.viewerHeight)
	return t
}

// SetActiveMonitor sets which monitor is being captured (no-op without CGO)
func (i *InputInjector) SetActiveMonitor(index int) {
	// No-op without CGO
}

// GetMonitors returns the list of available monitors (empty without CGO)
func (i *InputInjector) GetMonitors() []MonitorInfo {
	return nil
}

// InjectInput processes an input event (no-op without CGO)
func (i *InputInjector) InjectInput(input webrtc.InputEvent) {
	// No-op without CGO - log for debugging
	log.Printf("[InputInjector] No-CGO mode: ignoring input type=%s", input.Type)
}

// Release frees all resources (no-op without CGO)
func (i *InputInjector) Release() {
	// No-op without CGO
}
