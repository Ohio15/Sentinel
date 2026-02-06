// Package audio provides audio capture and encoding for remote desktop streaming
package audio

import (
	"errors"
	"time"
)

// Common errors
var (
	ErrNotInitialized    = errors.New("audio: not initialized")
	ErrDeviceNotFound    = errors.New("audio: device not found")
	ErrCaptureNotStarted = errors.New("audio: capture not started")
	ErrInvalidFormat     = errors.New("audio: invalid audio format")
	ErrEncoderFailed     = errors.New("audio: encoder failed")
)

// AudioDevice describes an audio device
type AudioDevice struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsDefault  bool   `json:"isDefault"`
	IsLoopback bool   `json:"isLoopback"` // True for system audio capture
	SampleRate int    `json:"sampleRate"`
	Channels   int    `json:"channels"`
	BitDepth   int    `json:"bitDepth"`
}

// AudioFormat describes audio format parameters
type AudioFormat struct {
	SampleRate   int // Samples per second (e.g., 48000)
	Channels     int // Number of channels (1=mono, 2=stereo)
	BitDepth     int // Bits per sample (16 or 32)
	SampleFormat SampleFormat
}

// SampleFormat defines the sample data format
type SampleFormat int

const (
	SampleFormatS16   SampleFormat = iota // Signed 16-bit integer
	SampleFormatS32                       // Signed 32-bit integer
	SampleFormatF32                       // 32-bit float
	SampleFormatF64                       // 64-bit float
)

// AudioSamples contains a buffer of audio samples
type AudioSamples struct {
	Data       []byte    // Raw sample data
	Samples    int       // Number of samples (per channel)
	Channels   int       // Number of channels
	SampleRate int       // Sample rate
	Format     SampleFormat
	Timestamp  time.Time // Capture timestamp
	Duration   time.Duration
}

// IAudioCapture defines the interface for audio capture
type IAudioCapture interface {
	// Initialize sets up audio capture for the specified device
	// Use empty deviceID for default device
	Initialize(deviceID string) error

	// Start begins audio capture
	Start() error

	// Stop stops audio capture
	Stop() error

	// SetVolume sets the capture volume (0.0 to 1.0)
	SetVolume(level float64) error

	// GetVolume returns the current capture volume
	GetVolume() float64

	// GetDevices returns available audio devices
	GetDevices() ([]AudioDevice, error)

	// GetFormat returns the current audio format
	GetFormat() AudioFormat

	// OnSamples sets the callback for received audio samples
	OnSamples(callback func(samples *AudioSamples))

	// IsCapturing returns true if capture is active
	IsCapturing() bool

	// Release frees all resources
	Release()
}

// IAudioEncoder defines the interface for audio encoding
type IAudioEncoder interface {
	// Initialize sets up the encoder with the specified parameters
	Initialize(sampleRate, channels, bitrate int) error

	// Encode encodes PCM samples to the target codec format
	// Returns encoded packets
	Encode(samples *AudioSamples) ([][]byte, error)

	// Flush returns any remaining encoded data
	Flush() ([][]byte, error)

	// SetBitrate adjusts the encoding bitrate
	SetBitrate(bitrate int) error

	// GetCodec returns the codec identifier
	GetCodec() string

	// Release frees encoder resources
	Release()
}

// AudioConfig holds audio streaming configuration
type AudioConfig struct {
	// Capture settings
	DeviceID      string  `json:"deviceId"`      // Empty for default
	UseLoopback   bool    `json:"useLoopback"`   // Capture system audio
	CaptureVolume float64 `json:"captureVolume"` // 0.0 to 1.0

	// Encoding settings
	Codec       string `json:"codec"`       // "opus" (default), "aac"
	SampleRate  int    `json:"sampleRate"`  // Target sample rate (48000)
	Channels    int    `json:"channels"`    // 1 or 2
	Bitrate     int    `json:"bitrate"`     // Target bitrate in bps
	FrameSizeMs int    `json:"frameSizeMs"` // Frame size in milliseconds (20)

	// Streaming settings
	Enabled bool `json:"enabled"` // Audio streaming enabled
}

// DefaultAudioConfig returns default audio configuration
func DefaultAudioConfig() AudioConfig {
	return AudioConfig{
		UseLoopback:   true,
		CaptureVolume: 1.0,
		Codec:         "opus",
		SampleRate:    48000,
		Channels:      2,
		Bitrate:       64000, // 64 kbps
		FrameSizeMs:   20,    // 20ms frames
		Enabled:       true,
	}
}

// AudioStats holds audio streaming statistics
type AudioStats struct {
	SamplesCaptured   uint64        `json:"samplesCaptured"`
	PacketsEncoded    uint64        `json:"packetsEncoded"`
	BytesEncoded      uint64        `json:"bytesEncoded"`
	PacketsSent       uint64        `json:"packetsSent"`
	DropCount         uint64        `json:"dropCount"`
	AverageLatency    time.Duration `json:"averageLatency"`
	CurrentBitrate    int           `json:"currentBitrate"`
	CaptureDeviceName string        `json:"captureDeviceName"`
}

// AudioMessage types for DataChannel communication
const (
	AudioMsgSetVolume     = "audio.setVolume"
	AudioMsgMute          = "audio.mute"
	AudioMsgSelectDevice  = "audio.selectDevice"
	AudioMsgDevices       = "audio.devices"
	AudioMsgStatus        = "audio.status"
)

// AudioVolumeMessage represents a volume change request
type AudioVolumeMessage struct {
	Type  string  `json:"type"`
	Level float64 `json:"level"` // 0.0 to 1.0
}

// AudioMuteMessage represents a mute/unmute request
type AudioMuteMessage struct {
	Type  string `json:"type"`
	Muted bool   `json:"muted"`
}

// AudioDeviceSelectMessage represents a device selection request
type AudioDeviceSelectMessage struct {
	Type     string `json:"type"`
	DeviceID string `json:"deviceId"`
}

// AudioDevicesMessage contains the list of available devices
type AudioDevicesMessage struct {
	Type    string        `json:"type"`
	Devices []AudioDevice `json:"devices"`
}

// AudioStatusMessage contains current audio streaming status
type AudioStatusMessage struct {
	Type      string `json:"type"`
	Streaming bool   `json:"streaming"`
	DeviceID  string `json:"deviceId"`
	Muted     bool   `json:"muted"`
	Volume    float64 `json:"volume"`
}
