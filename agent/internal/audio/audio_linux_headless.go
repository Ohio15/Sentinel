//go:build (linux && arm64) || (linux && arm)

package audio

import "errors"

// Headless stub for ARM Linux (Synology NAS, etc.)
// Audio capture not available without PulseAudio/X11

var ErrHeadlessMode = errors.New("audio capture not available in headless mode")

// AudioConfig defines audio settings
type AudioConfig struct {
	SampleRate  int
	Channels    int
	BitDepth    int
	FrameSize   int
	Bitrate     int
	Compression string
}

// AudioFormat represents audio format information
type AudioFormat struct {
	SampleRate int
	Channels   int
	BitDepth   int
}

// AudioSamples represents captured audio data
type AudioSamples struct {
	Data      []byte
	Timestamp int64
	Format    AudioFormat
}

// IAudioCapture interface for audio capture
type IAudioCapture interface {
	Start() error
	Stop() error
	SetCallback(func(*AudioSamples))
	GetFormat() AudioFormat
	SetVolume(float64) error
	GetVolume() float64
}

// IAudioEncoder interface for audio encoding
type IAudioEncoder interface {
	Encode(samples *AudioSamples) ([]byte, error)
	Close() error
}

// PulseAudioCapture stub for headless systems
type PulseAudioCapture struct{}

func NewPulseAudioCapture(config AudioConfig) (*PulseAudioCapture, error) {
	return nil, ErrHeadlessMode
}

func (p *PulseAudioCapture) Start() error {
	return ErrHeadlessMode
}

func (p *PulseAudioCapture) Stop() error {
	return nil
}

func (p *PulseAudioCapture) SetCallback(cb func(*AudioSamples)) {}

func (p *PulseAudioCapture) GetFormat() AudioFormat {
	return AudioFormat{}
}

func (p *PulseAudioCapture) SetVolume(vol float64) error {
	return ErrHeadlessMode
}

func (p *PulseAudioCapture) GetVolume() float64 {
	return 0
}

// AudioTrackManager stub for headless systems
type AudioTrackManager struct{}

func NewAudioTrackManager() *AudioTrackManager {
	return &AudioTrackManager{}
}

func (m *AudioTrackManager) Start() error {
	return ErrHeadlessMode
}

func (m *AudioTrackManager) Stop() error {
	return nil
}

func (m *AudioTrackManager) SetMuted(muted bool) {}

func (m *AudioTrackManager) IsMuted() bool {
	return true
}
