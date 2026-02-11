//go:build (linux && arm64) || (linux && arm)

package helper

import (
	"log"

	"github.com/sentinel/agent/internal/webrtc"
)

// Headless stub for ARM Linux (Synology NAS, Raspberry Pi, etc.)
// These systems typically don't have X11 displays, so desktop features are disabled

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

// CoordinateTransformer handles coordinate mapping (stub for headless)
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

// GetMonitors returns available monitors (empty for headless)
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

// InputInjector stub for headless systems
type InputInjector struct {
	sourceWidth    int
	sourceHeight   int
	sourceLeft     int
	sourceTop      int
	viewerWidth    int
	viewerHeight   int
	useTransformer bool
}

// NewInputInjector creates a new input injector stub for headless systems
func NewInputInjector() *InputInjector {
	log.Printf("[InputInjector] Headless mode: input injection not available")
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

// SetActiveMonitor sets which monitor is being captured (no-op for headless)
func (i *InputInjector) SetActiveMonitor(index int) {
	// No-op for headless systems
}

// GetMonitors returns the list of available monitors (empty for headless)
func (i *InputInjector) GetMonitors() []MonitorInfo {
	return nil
}

// InjectInput processes an input event (no-op for headless systems)
func (i *InputInjector) InjectInput(input webrtc.InputEvent) {
	// No-op for headless systems - log for debugging
	log.Printf("[InputInjector] Headless mode: ignoring input type=%s", input.Type)
}

// Release frees all resources (no-op for headless)
func (i *InputInjector) Release() {
	// No-op for headless systems
}
