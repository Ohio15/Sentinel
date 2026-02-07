//go:build linux

package audio

/*
#cgo pkg-config: libpulse-simple libpulse
#include <pulse/simple.h>
#include <pulse/error.h>
#include <stdlib.h>
#include <string.h>
*/
import "C"

import (
	"errors"
	"log"
	"sync"
	"time"
	"unsafe"
)

// PulseAudioCapture implements IAudioCapture for Linux using PulseAudio
type PulseAudioCapture struct {
	pa         *C.pa_simple
	config     AudioConfig
	format     AudioFormat
	volume     float64
	capturing  bool
	callback   func(*AudioSamples)
	stopCh     chan struct{}
	mu         sync.RWMutex
}

// NewPulseAudioCapture creates a new PulseAudio capture instance
func NewPulseAudioCapture() *PulseAudioCapture {
	return &PulseAudioCapture{
		config: DefaultAudioConfig(),
		volume: 1.0,
		format: AudioFormat{
			SampleRate:   48000,
			Channels:     2,
			BitDepth:     16,
			SampleFormat: SampleFormatS16,
		},
	}
}

// NewWASAPICapture is an alias for PulseAudioCapture on Linux to maintain API compatibility
func NewWASAPICapture() *PulseAudioCapture {
	return NewPulseAudioCapture()
}

// Initialize sets up audio capture
func (c *PulseAudioCapture) Initialize(deviceID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// PulseAudio sample spec
	var ss C.pa_sample_spec
	ss.format = C.PA_SAMPLE_S16LE
	ss.rate = C.uint32_t(c.format.SampleRate)
	ss.channels = C.uint8_t(c.format.Channels)

	// Device name (nil for default)
	var device *C.char
	if deviceID != "" {
		device = C.CString(deviceID)
		defer C.free(unsafe.Pointer(device))
	}

	// Application name
	appName := C.CString("sentinel-agent")
	defer C.free(unsafe.Pointer(appName))

	// Stream name
	streamName := C.CString("Desktop Audio Capture")
	defer C.free(unsafe.Pointer(streamName))

	var err C.int

	// Create capture stream
	// Use monitor source for loopback capture
	c.pa = C.pa_simple_new(
		nil,               // Server (default)
		appName,           // Application name
		C.PA_STREAM_RECORD, // Direction
		device,            // Device (nil = default monitor)
		streamName,        // Stream name
		&ss,               // Sample spec
		nil,               // Channel map (default)
		nil,               // Buffer attributes (default)
		&err,              // Error code
	)

	if c.pa == nil {
		errStr := C.pa_strerror(err)
		return errors.New("failed to initialize PulseAudio: " + C.GoString(errStr))
	}

	log.Printf("[PulseAudioCapture] Initialized with %d Hz, %d channels, S16LE",
		c.format.SampleRate, c.format.Channels)

	return nil
}

// Start begins audio capture
func (c *PulseAudioCapture) Start() error {
	c.mu.Lock()
	if c.capturing {
		c.mu.Unlock()
		return nil
	}
	if c.pa == nil {
		c.mu.Unlock()
		return ErrNotInitialized
	}
	c.capturing = true
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	go c.captureLoop()

	log.Printf("[PulseAudioCapture] Started capturing")
	return nil
}

// Stop stops audio capture
func (c *PulseAudioCapture) Stop() error {
	c.mu.Lock()
	if !c.capturing {
		c.mu.Unlock()
		return nil
	}
	c.capturing = false
	close(c.stopCh)
	c.mu.Unlock()

	log.Printf("[PulseAudioCapture] Stopped capturing")
	return nil
}

func (c *PulseAudioCapture) captureLoop() {
	// Buffer for 20ms of audio at 48kHz stereo 16-bit
	// 48000 * 2 channels * 2 bytes * 0.020 seconds = 3840 bytes
	frameSizeBytes := c.format.SampleRate * c.format.Channels * 2 * 20 / 1000
	buffer := make([]byte, frameSizeBytes)

	for {
		select {
		case <-c.stopCh:
			return
		default:
		}

		c.mu.RLock()
		pa := c.pa
		callback := c.callback
		volume := c.volume
		c.mu.RUnlock()

		if pa == nil {
			return
		}

		// Read audio data
		var err C.int
		ret := C.pa_simple_read(pa, unsafe.Pointer(&buffer[0]), C.size_t(len(buffer)), &err)
		if ret < 0 {
			log.Printf("[PulseAudioCapture] Read error: %s", C.GoString(C.pa_strerror(err)))
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// Apply volume if not 1.0
		if volume != 1.0 {
			c.applyVolume(buffer, volume)
		}

		// Create samples
		samples := &AudioSamples{
			Data:       make([]byte, len(buffer)),
			Samples:    frameSizeBytes / (c.format.Channels * 2), // Samples per channel
			Channels:   c.format.Channels,
			SampleRate: c.format.SampleRate,
			Format:     SampleFormatS16,
			Timestamp:  time.Now(),
			Duration:   20 * time.Millisecond,
		}
		copy(samples.Data, buffer)

		// Call callback
		if callback != nil {
			callback(samples)
		}
	}
}

func (c *PulseAudioCapture) applyVolume(buffer []byte, volume float64) {
	// Process 16-bit samples
	for i := 0; i < len(buffer)-1; i += 2 {
		sample := int16(buffer[i]) | int16(buffer[i+1])<<8
		sample = int16(float64(sample) * volume)
		buffer[i] = byte(sample)
		buffer[i+1] = byte(sample >> 8)
	}
}

// SetVolume sets the capture volume
func (c *PulseAudioCapture) SetVolume(level float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	c.volume = level

	return nil
}

// GetVolume returns the current volume
func (c *PulseAudioCapture) GetVolume() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.volume
}

// GetDevices returns available audio devices
func (c *PulseAudioCapture) GetDevices() ([]AudioDevice, error) {
	// Getting device list requires the async PulseAudio API
	// For simplicity, return default device
	devices := []AudioDevice{
		{
			ID:         "",
			Name:       "Default Monitor",
			IsDefault:  true,
			IsLoopback: true,
			SampleRate: 48000,
			Channels:   2,
			BitDepth:   16,
		},
	}

	return devices, nil
}

// GetFormat returns the current audio format
func (c *PulseAudioCapture) GetFormat() AudioFormat {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.format
}

// OnSamples sets the callback for audio samples
func (c *PulseAudioCapture) OnSamples(callback func(*AudioSamples)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.callback = callback
}

// IsCapturing returns true if capture is active
func (c *PulseAudioCapture) IsCapturing() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.capturing
}

// Release frees all resources
func (c *PulseAudioCapture) Release() {
	c.Stop()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pa != nil {
		C.pa_simple_free(c.pa)
		c.pa = nil
	}

	log.Printf("[PulseAudioCapture] Released")
}

// Compile-time interface check
var _ IAudioCapture = (*PulseAudioCapture)(nil)
