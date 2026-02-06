//go:build windows

package mocks

import (
	"errors"
	"image"
	"sync"
	"time"
)

// MockEncoder provides a controllable mock H.264 encoder for testing
type MockEncoder struct {
	// Configuration
	Width      int
	Height     int
	Bitrate    int
	FrameRate  int
	IsHardware bool

	// Test control
	EncodeDelay     time.Duration
	FailOnEncode    bool
	FailOnFrame     int  // Fail on specific frame (-1 = never)
	ForceKeyframe   bool
	OutputSize      int  // Fixed output size (0 = calculated)

	// Stats
	EncodeCount     int
	KeyframeCount   int
	TotalBytesOut   int64
	LastEncodeTime  time.Duration

	mu       sync.Mutex
	closed   bool
	nalUnits [][]byte // Pre-configured NAL units to return
}

// NewMockEncoder creates a new mock encoder
func NewMockEncoder(width, height, bitrate int) *MockEncoder {
	return &MockEncoder{
		Width:       width,
		Height:      height,
		Bitrate:     bitrate,
		FrameRate:   30,
		FailOnFrame: -1,
	}
}

// Encode encodes a YCbCr frame to H.264 NAL units
func (m *MockEncoder) Encode(ycbcr *image.YCbCr) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, errors.New("encoder closed")
	}

	m.EncodeCount++

	// Simulate encode delay
	if m.EncodeDelay > 0 {
		start := time.Now()
		m.mu.Unlock()
		time.Sleep(m.EncodeDelay)
		m.mu.Lock()
		m.LastEncodeTime = time.Since(start)
	}

	// Simulate failure
	if m.FailOnEncode || m.EncodeCount == m.FailOnFrame {
		return nil, errors.New("simulated encode failure")
	}

	// Return pre-configured NAL units if available
	if len(m.nalUnits) > 0 {
		idx := (m.EncodeCount - 1) % len(m.nalUnits)
		return m.nalUnits[idx], nil
	}

	// Generate mock NAL units
	var outputSize int
	if m.OutputSize > 0 {
		outputSize = m.OutputSize
	} else {
		// Estimate based on bitrate and framerate
		outputSize = m.Bitrate / (m.FrameRate * 8)
		if outputSize < 100 {
			outputSize = 100
		}
	}

	// Create mock NAL unit data
	data := make([]byte, outputSize)

	// Determine if keyframe
	isKeyframe := m.ForceKeyframe || m.EncodeCount == 1 || m.EncodeCount%60 == 0
	if isKeyframe {
		m.KeyframeCount++
		// Start with SPS NAL (0x67)
		data[0] = 0x00
		data[1] = 0x00
		data[2] = 0x00
		data[3] = 0x01
		data[4] = 0x67 // SPS
		// Add PPS NAL (0x68)
		if len(data) > 20 {
			data[15] = 0x00
			data[16] = 0x00
			data[17] = 0x00
			data[18] = 0x01
			data[19] = 0x68 // PPS
		}
		// Add IDR NAL (0x65)
		if len(data) > 30 {
			data[25] = 0x00
			data[26] = 0x00
			data[27] = 0x00
			data[28] = 0x01
			data[29] = 0x65 // IDR
		}
	} else {
		// Non-IDR NAL (0x41)
		data[0] = 0x00
		data[1] = 0x00
		data[2] = 0x00
		data[3] = 0x01
		data[4] = 0x41 // Non-IDR P-frame
	}

	// Fill rest with pseudo-random data
	for i := 5; i < len(data); i++ {
		data[i] = byte((m.EncodeCount*i) % 256)
	}

	m.ForceKeyframe = false
	m.TotalBytesOut += int64(len(data))

	return data, nil
}

// EncodeBGRA encodes BGRA directly
func (m *MockEncoder) EncodeBGRA(bgra []byte, width, height, stride int, forceKeyframe bool) ([]byte, error) {
	// For mock, just call Encode with nil (we don't need actual conversion)
	m.mu.Lock()
	if forceKeyframe {
		m.ForceKeyframe = true
	}
	m.mu.Unlock()
	return m.Encode(nil)
}

// ForceNextKeyframe forces the next frame to be a keyframe
func (m *MockEncoder) ForceNextKeyframe() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ForceKeyframe = true
}

// SetBitrate adjusts target bitrate
func (m *MockEncoder) SetBitrate(bps int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Bitrate = bps
	return nil
}

// SetFrameRate adjusts target frame rate
func (m *MockEncoder) SetFrameRate(fps int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FrameRate = fps
	return nil
}

// GetWidth returns configured width
func (m *MockEncoder) GetWidth() int {
	return m.Width
}

// GetHeight returns configured height
func (m *MockEncoder) GetHeight() int {
	return m.Height
}

// GetIsHardware returns true if using hardware encoder
func (m *MockEncoder) GetIsHardware() bool {
	return m.IsHardware
}

// Close releases encoder resources
func (m *MockEncoder) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

// SetNALUnits sets pre-configured NAL units to return
func (m *MockEncoder) SetNALUnits(nalUnits [][]byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nalUnits = nalUnits
}

// Reset resets the mock state
func (m *MockEncoder) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EncodeCount = 0
	m.KeyframeCount = 0
	m.TotalBytesOut = 0
	m.ForceKeyframe = false
	m.FailOnEncode = false
	m.closed = false
}

// GetStats returns encoding statistics
func (m *MockEncoder) GetStats() (encodeCount, keyframeCount int, totalBytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.EncodeCount, m.KeyframeCount, m.TotalBytesOut
}

// CreateMockNALUnit creates a mock NAL unit for testing
func CreateMockNALUnit(nalType byte, size int) []byte {
	data := make([]byte, size)
	// Start code
	data[0] = 0x00
	data[1] = 0x00
	data[2] = 0x00
	data[3] = 0x01
	data[4] = nalType
	return data
}

// NAL unit types
const (
	NALTypeNonIDR = 0x41
	NALTypeIDR    = 0x65
	NALTypeSPS    = 0x67
	NALTypePPS    = 0x68
)
