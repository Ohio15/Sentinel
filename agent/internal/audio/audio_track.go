//go:build windows

package audio

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// AudioTrackManager manages audio streaming for a WebRTC session
type AudioTrackManager struct {
	// WebRTC components
	peerConnection *webrtc.PeerConnection
	audioTrack     *webrtc.TrackLocalStaticSample
	dataChannel    *webrtc.DataChannel

	// Audio components
	capture IAudioCapture
	encoder IAudioEncoder
	config  AudioConfig

	// State
	streaming bool
	muted     bool
	volume    float64

	// Stats
	stats AudioStats

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
}

// NewAudioTrackManager creates a new audio track manager
func NewAudioTrackManager(pc *webrtc.PeerConnection, dc *webrtc.DataChannel) *AudioTrackManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &AudioTrackManager{
		peerConnection: pc,
		dataChannel:    dc,
		config:         DefaultAudioConfig(),
		volume:         1.0,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Initialize sets up audio capture and encoding
func (m *AudioTrackManager) Initialize() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create WASAPI capture
	m.capture = NewWASAPICapture()
	if err := m.capture.Initialize(m.config.DeviceID); err != nil {
		return err
	}

	// Create Opus encoder
	m.encoder = NewOpusEncoder()
	format := m.capture.GetFormat()
	if err := m.encoder.Initialize(format.SampleRate, format.Channels, m.config.Bitrate); err != nil {
		m.capture.Release()
		return err
	}

	// Create WebRTC audio track
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: 48000,
			Channels:  2,
		},
		"audio",
		"desktop-audio",
	)
	if err != nil {
		m.encoder.Release()
		m.capture.Release()
		return err
	}
	m.audioTrack = track

	// Add track to peer connection
	_, err = m.peerConnection.AddTrack(track)
	if err != nil {
		m.encoder.Release()
		m.capture.Release()
		return err
	}

	// Set up capture callback
	m.capture.OnSamples(func(samples *AudioSamples) {
		m.handleSamples(samples)
	})

	log.Printf("[AudioTrack] Initialized")
	return nil
}

// Start begins audio streaming
func (m *AudioTrackManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.streaming {
		return nil
	}

	if m.capture == nil {
		return ErrNotInitialized
	}

	if err := m.capture.Start(); err != nil {
		return err
	}

	m.streaming = true
	m.sendStatus()

	log.Printf("[AudioTrack] Streaming started")
	return nil
}

// Stop stops audio streaming
func (m *AudioTrackManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.streaming {
		return nil
	}

	if m.capture != nil {
		m.capture.Stop()
	}

	m.streaming = false
	m.sendStatus()

	log.Printf("[AudioTrack] Streaming stopped")
	return nil
}

// handleSamples processes captured audio samples
func (m *AudioTrackManager) handleSamples(samples *AudioSamples) {
	m.mu.RLock()
	if !m.streaming || m.muted || m.encoder == nil || m.audioTrack == nil {
		m.mu.RUnlock()
		return
	}
	encoder := m.encoder
	track := m.audioTrack
	m.mu.RUnlock()

	// Encode samples
	packets, err := encoder.Encode(samples)
	if err != nil {
		log.Printf("[AudioTrack] Encode error: %v", err)
		return
	}

	// Send each packet
	for _, packet := range packets {
		if err := track.WriteSample(media.Sample{
			Data:     packet,
			Duration: 20 * time.Millisecond, // 20ms frame
		}); err != nil {
			log.Printf("[AudioTrack] Write sample error: %v", err)
			m.mu.Lock()
			m.stats.DropCount++
			m.mu.Unlock()
			continue
		}

		m.mu.Lock()
		m.stats.PacketsSent++
		m.stats.BytesEncoded += uint64(len(packet))
		m.mu.Unlock()
	}

	m.mu.Lock()
	m.stats.SamplesCaptured += uint64(samples.Samples)
	m.stats.PacketsEncoded += uint64(len(packets))
	m.mu.Unlock()
}

// HandleMessage processes audio control messages from the viewer
func (m *AudioTrackManager) HandleMessage(msgType string, data []byte) error {
	switch msgType {
	case AudioMsgSetVolume:
		var msg AudioVolumeMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
		return m.SetVolume(msg.Level)

	case AudioMsgMute:
		var msg AudioMuteMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
		return m.SetMuted(msg.Muted)

	case AudioMsgSelectDevice:
		var msg AudioDeviceSelectMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
		return m.SelectDevice(msg.DeviceID)

	default:
		log.Printf("[AudioTrack] Unknown message type: %s", msgType)
	}

	return nil
}

// SetVolume sets the audio volume
func (m *AudioTrackManager) SetVolume(level float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}

	m.volume = level
	if m.capture != nil {
		m.capture.SetVolume(level)
	}

	m.sendStatus()
	log.Printf("[AudioTrack] Volume set to %.1f", level)
	return nil
}

// SetMuted sets the mute state
func (m *AudioTrackManager) SetMuted(muted bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.muted = muted
	m.sendStatus()

	log.Printf("[AudioTrack] Muted: %v", muted)
	return nil
}

// SelectDevice switches to a different audio device
func (m *AudioTrackManager) SelectDevice(deviceID string) error {
	m.mu.Lock()
	wasStreaming := m.streaming
	m.mu.Unlock()

	// Stop current capture
	if wasStreaming {
		m.Stop()
	}

	m.mu.Lock()
	// Release old capture
	if m.capture != nil {
		m.capture.Release()
	}

	// Create new capture with specified device
	m.capture = NewWASAPICapture()
	m.config.DeviceID = deviceID
	m.mu.Unlock()

	if err := m.capture.Initialize(deviceID); err != nil {
		return err
	}

	// Set up callback
	m.capture.OnSamples(func(samples *AudioSamples) {
		m.handleSamples(samples)
	})

	// Restart if was streaming
	if wasStreaming {
		return m.Start()
	}

	m.sendStatus()
	return nil
}

// GetDevices returns available audio devices
func (m *AudioTrackManager) GetDevices() ([]AudioDevice, error) {
	m.mu.RLock()
	capture := m.capture
	m.mu.RUnlock()

	if capture == nil {
		return nil, ErrNotInitialized
	}

	devices, err := capture.GetDevices()
	if err != nil {
		return nil, err
	}

	// Send devices to viewer
	m.sendDevices(devices)
	return devices, nil
}

// GetStats returns audio streaming statistics
func (m *AudioTrackManager) GetStats() AudioStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stats
}

// sendStatus sends current audio status via DataChannel
func (m *AudioTrackManager) sendStatus() {
	if m.dataChannel == nil || m.dataChannel.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}

	msg := AudioStatusMessage{
		Type:      AudioMsgStatus,
		Streaming: m.streaming,
		DeviceID:  m.config.DeviceID,
		Muted:     m.muted,
		Volume:    m.volume,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	m.dataChannel.SendText(string(data))
}

// sendDevices sends device list via DataChannel
func (m *AudioTrackManager) sendDevices(devices []AudioDevice) {
	if m.dataChannel == nil || m.dataChannel.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}

	msg := AudioDevicesMessage{
		Type:    AudioMsgDevices,
		Devices: devices,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	m.dataChannel.SendText(string(data))
}

// Release frees all resources
func (m *AudioTrackManager) Release() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cancel()

	if m.streaming && m.capture != nil {
		m.capture.Stop()
	}
	m.streaming = false

	if m.encoder != nil {
		m.encoder.Release()
		m.encoder = nil
	}

	if m.capture != nil {
		m.capture.Release()
		m.capture = nil
	}

	log.Printf("[AudioTrack] Released")
}

// IsStreaming returns true if audio is streaming
func (m *AudioTrackManager) IsStreaming() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.streaming
}

// IsMuted returns true if audio is muted
func (m *AudioTrackManager) IsMuted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.muted
}
