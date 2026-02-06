//go:build windows

package mocks

import (
	"sync"
	"time"
)

// InputEvent represents an input event for testing
type InputEvent struct {
	Type      string
	Event     string
	X         float64
	Y         float64
	Button    int
	Key       string
	Code      string
	DeltaY    float64
	Modifiers struct {
		Ctrl  bool
		Alt   bool
		Shift bool
		Meta  bool
	}
	Timestamp time.Time
}

// MockInputInjector records input events for testing
type MockInputInjector struct {
	// Recorded events
	Events []InputEvent

	// Test control
	FailOnInject   bool
	InjectDelay    time.Duration
	RateLimitHits  int

	// Coordinate transformation
	ScreenOffsetX  int
	ScreenOffsetY  int
	DPIScale       float64

	// Last injected position
	LastX, LastY   int

	// Stats
	MoveCount      int
	ClickCount     int
	KeyCount       int
	WheelCount     int

	mu sync.Mutex
}

// NewMockInputInjector creates a new mock input injector
func NewMockInputInjector() *MockInputInjector {
	return &MockInputInjector{
		Events:   make([]InputEvent, 0),
		DPIScale: 1.0,
	}
}

// InjectInput records an input event
func (m *MockInputInjector) InjectInput(event InputEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.FailOnInject {
		return &InputError{Message: "injection failed"}
	}

	if m.InjectDelay > 0 {
		m.mu.Unlock()
		time.Sleep(m.InjectDelay)
		m.mu.Lock()
	}

	event.Timestamp = time.Now()
	m.Events = append(m.Events, event)

	// Update stats
	switch event.Type {
	case "move", "mousemove":
		m.MoveCount++
		m.LastX = int(event.X) + m.ScreenOffsetX
		m.LastY = int(event.Y) + m.ScreenOffsetY
	case "down", "up", "mousedown", "mouseup":
		m.ClickCount++
		m.LastX = int(event.X) + m.ScreenOffsetX
		m.LastY = int(event.Y) + m.ScreenOffsetY
	case "keydown", "keyup":
		m.KeyCount++
	case "wheel":
		m.WheelCount++
	}

	return nil
}

// MoveMouse simulates mouse movement
func (m *MockInputInjector) MoveMouse(x, y int) error {
	return m.InjectInput(InputEvent{
		Type: "move",
		X:    float64(x),
		Y:    float64(y),
	})
}

// MouseDown simulates mouse button press
func (m *MockInputInjector) MouseDown(button int) error {
	return m.InjectInput(InputEvent{
		Type:   "down",
		Button: button,
		X:      float64(m.LastX),
		Y:      float64(m.LastY),
	})
}

// MouseUp simulates mouse button release
func (m *MockInputInjector) MouseUp(button int) error {
	return m.InjectInput(InputEvent{
		Type:   "up",
		Button: button,
		X:      float64(m.LastX),
		Y:      float64(m.LastY),
	})
}

// MouseWheel simulates mouse wheel
func (m *MockInputInjector) MouseWheel(deltaX, deltaY int) error {
	return m.InjectInput(InputEvent{
		Type:   "wheel",
		DeltaY: float64(deltaY),
		X:      float64(m.LastX),
		Y:      float64(m.LastY),
	})
}

// KeyDown simulates key press
func (m *MockInputInjector) KeyDown(key string, modifiers Modifiers) error {
	event := InputEvent{
		Type: "keydown",
		Key:  key,
	}
	event.Modifiers.Ctrl = modifiers.Ctrl
	event.Modifiers.Alt = modifiers.Alt
	event.Modifiers.Shift = modifiers.Shift
	event.Modifiers.Meta = modifiers.Meta
	return m.InjectInput(event)
}

// KeyUp simulates key release
func (m *MockInputInjector) KeyUp(key string, modifiers Modifiers) error {
	event := InputEvent{
		Type: "keyup",
		Key:  key,
	}
	event.Modifiers.Ctrl = modifiers.Ctrl
	event.Modifiers.Alt = modifiers.Alt
	event.Modifiers.Shift = modifiers.Shift
	event.Modifiers.Meta = modifiers.Meta
	return m.InjectInput(event)
}

// SetScreenOffset sets the coordinate offset
func (m *MockInputInjector) SetScreenOffset(x, y int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ScreenOffsetX = x
	m.ScreenOffsetY = y
}

// SetDPIScale sets the DPI scale factor
func (m *MockInputInjector) SetDPIScale(scale float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DPIScale = scale
}

// GetEvents returns all recorded events
func (m *MockInputInjector) GetEvents() []InputEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]InputEvent, len(m.Events))
	copy(result, m.Events)
	return result
}

// GetLastEvent returns the most recent event
func (m *MockInputInjector) GetLastEvent() *InputEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Events) == 0 {
		return nil
	}
	event := m.Events[len(m.Events)-1]
	return &event
}

// GetEventCount returns event count by type
func (m *MockInputInjector) GetEventCount(eventType string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, e := range m.Events {
		if e.Type == eventType {
			count++
		}
	}
	return count
}

// WaitForEvent waits for an event of the specified type
func (m *MockInputInjector) WaitForEvent(eventType string, timeout time.Duration) *InputEvent {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		for i := len(m.Events) - 1; i >= 0; i-- {
			if m.Events[i].Type == eventType {
				event := m.Events[i]
				m.mu.Unlock()
				return &event
			}
		}
		m.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// Reset clears all recorded events
func (m *MockInputInjector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Events = m.Events[:0]
	m.MoveCount = 0
	m.ClickCount = 0
	m.KeyCount = 0
	m.WheelCount = 0
	m.RateLimitHits = 0
}

// GetStats returns injection statistics
func (m *MockInputInjector) GetStats() (moves, clicks, keys, wheels int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.MoveCount, m.ClickCount, m.KeyCount, m.WheelCount
}

// Modifiers represents keyboard modifier keys
type Modifiers struct {
	Ctrl  bool
	Alt   bool
	Shift bool
	Meta  bool
}

// InputError represents an input injection error
type InputError struct {
	Message string
}

func (e *InputError) Error() string {
	return e.Message
}

// CoordinateTransformer handles coordinate transformation
type CoordinateTransformer struct {
	// Video dimensions (what's encoded)
	VideoWidth, VideoHeight int

	// Screen dimensions (actual)
	ScreenWidth, ScreenHeight int

	// Screen offset in virtual desktop
	ScreenOffsetX, ScreenOffsetY int

	// Encoded dimensions (may differ from screen during quality adaptation)
	EncodedWidth, EncodedHeight int

	// DPI scale
	DPIScale float64
}

// NewCoordinateTransformer creates a new transformer
func NewCoordinateTransformer(screenW, screenH, encodedW, encodedH int) *CoordinateTransformer {
	return &CoordinateTransformer{
		VideoWidth:    encodedW,
		VideoHeight:   encodedH,
		ScreenWidth:   screenW,
		ScreenHeight:  screenH,
		EncodedWidth:  encodedW,
		EncodedHeight: encodedH,
		DPIScale:      1.0,
	}
}

// Transform converts input coordinates to screen coordinates
func (t *CoordinateTransformer) Transform(inputX, inputY float64) (screenX, screenY int) {
	// Scale from encoded/video space to screen space
	scaleX := float64(t.ScreenWidth) / float64(t.EncodedWidth)
	scaleY := float64(t.ScreenHeight) / float64(t.EncodedHeight)

	screenX = int(inputX*scaleX) + t.ScreenOffsetX
	screenY = int(inputY*scaleY) + t.ScreenOffsetY

	// Clamp to screen bounds
	if screenX < t.ScreenOffsetX {
		screenX = t.ScreenOffsetX
	}
	if screenX >= t.ScreenOffsetX+t.ScreenWidth {
		screenX = t.ScreenOffsetX + t.ScreenWidth - 1
	}
	if screenY < t.ScreenOffsetY {
		screenY = t.ScreenOffsetY
	}
	if screenY >= t.ScreenOffsetY+t.ScreenHeight {
		screenY = t.ScreenOffsetY + t.ScreenHeight - 1
	}

	return screenX, screenY
}

// SetEncodedDimensions updates the encoded dimensions (for quality adaptation)
func (t *CoordinateTransformer) SetEncodedDimensions(width, height int) {
	t.EncodedWidth = width
	t.EncodedHeight = height
}
