//go:build windows

package mocks

import (
	"errors"
	"image"
	"sync"
	"time"
)

// CapturedFrame matches the real CapturedFrame structure
type CapturedFrame struct {
	Data       []byte
	Width      int
	Height     int
	Stride     int
	DirtyRects []image.Rectangle
	Timestamp  time.Time
	FrameID    uint64
}

// MonitorInfo describes a display
type MonitorInfo struct {
	Index       int
	Name        string
	Bounds      image.Rectangle
	IsPrimary   bool
	ScaleFactor float64
}

// MockCapture provides a controllable mock screen capture for testing
type MockCapture struct {
	// Configuration
	Width        int
	Height       int
	Monitors     []MonitorInfo
	CaptureDelay time.Duration

	// Test control
	Frames       []*CapturedFrame
	FrameIndex   int
	ErrorOnFrame int  // Return error on this frame number (-1 = never)
	ErrorToReturn error

	// Access lost simulation
	SimulateAccessLost   bool
	AccessLostOnFrame    int
	AccessLostRecovered  bool

	// Resolution change simulation
	SimulateResChange    bool
	ResChangeOnFrame     int
	NewWidth, NewHeight  int

	// Stats
	CaptureCount int

	mu sync.Mutex
	initialized bool
	currentMonitor int
}

// NewMockCapture creates a new mock capture with default settings
func NewMockCapture(width, height int) *MockCapture {
	return &MockCapture{
		Width:        width,
		Height:       height,
		Monitors: []MonitorInfo{
			{Index: 0, Name: "Primary", Bounds: image.Rect(0, 0, width, height), IsPrimary: true, ScaleFactor: 1.0},
		},
		ErrorOnFrame: -1,
		AccessLostOnFrame: -1,
		ResChangeOnFrame: -1,
	}
}

// Initialize initializes the mock capture for the specified monitor
func (m *MockCapture) Initialize(monitorIndex int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if monitorIndex >= len(m.Monitors) {
		return errors.New("monitor not found")
	}

	m.currentMonitor = monitorIndex
	m.initialized = true
	return nil
}

// CaptureFrame captures a single frame
func (m *MockCapture) CaptureFrame(timeoutMs int) (*CapturedFrame, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.initialized {
		return nil, errors.New("capture not initialized")
	}

	m.CaptureCount++

	// Simulate capture delay
	if m.CaptureDelay > 0 {
		m.mu.Unlock()
		time.Sleep(m.CaptureDelay)
		m.mu.Lock()
	}

	// Simulate access lost error
	if m.SimulateAccessLost && m.CaptureCount == m.AccessLostOnFrame {
		m.AccessLostRecovered = false
		return nil, errors.New("access lost: 0x887A0026")
	}

	// Simulate resolution change
	if m.SimulateResChange && m.CaptureCount == m.ResChangeOnFrame {
		m.Width = m.NewWidth
		m.Height = m.NewHeight
	}

	// Return configured error
	if m.ErrorOnFrame == m.CaptureCount {
		if m.ErrorToReturn != nil {
			return nil, m.ErrorToReturn
		}
		return nil, errors.New("simulated capture error")
	}

	// Return pre-configured frame if available
	if len(m.Frames) > 0 {
		frame := m.Frames[m.FrameIndex % len(m.Frames)]
		m.FrameIndex++
		return frame, nil
	}

	// Generate test frame (BGRA format)
	stride := m.Width * 4
	data := make([]byte, stride*m.Height)

	// Fill with test pattern (gradient)
	for y := 0; y < m.Height; y++ {
		for x := 0; x < m.Width; x++ {
			offset := y*stride + x*4
			data[offset+0] = byte(x % 256)     // B
			data[offset+1] = byte(y % 256)     // G
			data[offset+2] = byte((x+y) % 256) // R
			data[offset+3] = 255               // A
		}
	}

	frame := &CapturedFrame{
		Data:      data,
		Width:     m.Width,
		Height:    m.Height,
		Stride:    stride,
		DirtyRects: []image.Rectangle{image.Rect(0, 0, m.Width, m.Height)},
		Timestamp: time.Now(),
		FrameID:   uint64(m.CaptureCount),
	}

	m.FrameIndex++
	return frame, nil
}

// GetDimensions returns current capture dimensions
func (m *MockCapture) GetDimensions() (width, height int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Width, m.Height
}

// GetMonitorInfo returns information about available monitors
func (m *MockCapture) GetMonitorInfo() []MonitorInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Monitors
}

// Release frees all resources
func (m *MockCapture) Release() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initialized = false
}

// SetTestFrames sets pre-configured frames for testing
func (m *MockCapture) SetTestFrames(frames []*CapturedFrame) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Frames = frames
	m.FrameIndex = 0
}

// AddMonitor adds a monitor to the mock
func (m *MockCapture) AddMonitor(info MonitorInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Monitors = append(m.Monitors, info)
}

// Reset resets the mock state
func (m *MockCapture) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FrameIndex = 0
	m.CaptureCount = 0
	m.AccessLostRecovered = false
}

// RecoverFromAccessLost simulates recovery after access lost
func (m *MockCapture) RecoverFromAccessLost() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AccessLostRecovered = true
	m.SimulateAccessLost = false
	return nil
}

// CreateTestFrame creates a test frame with specific properties
func CreateTestFrame(width, height int, frameID uint64, pattern string) *CapturedFrame {
	stride := width * 4
	data := make([]byte, stride*height)

	switch pattern {
	case "black":
		// Already zeroed
	case "white":
		for i := 0; i < len(data); i += 4 {
			data[i+0] = 255 // B
			data[i+1] = 255 // G
			data[i+2] = 255 // R
			data[i+3] = 255 // A
		}
	case "gradient":
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				offset := y*stride + x*4
				data[offset+0] = byte(x % 256)
				data[offset+1] = byte(y % 256)
				data[offset+2] = byte((x+y) % 256)
				data[offset+3] = 255
			}
		}
	case "checkerboard":
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				offset := y*stride + x*4
				if (x/10+y/10)%2 == 0 {
					data[offset+0] = 255
					data[offset+1] = 255
					data[offset+2] = 255
				}
				data[offset+3] = 255
			}
		}
	}

	return &CapturedFrame{
		Data:       data,
		Width:      width,
		Height:     height,
		Stride:     stride,
		DirtyRects: []image.Rectangle{image.Rect(0, 0, width, height)},
		Timestamp:  time.Now(),
		FrameID:    frameID,
	}
}
