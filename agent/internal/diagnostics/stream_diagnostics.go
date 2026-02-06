// Package diagnostics provides diagnostic and debugging capabilities
// This file contains remote desktop streaming diagnostics

package diagnostics

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"
)

// StreamDiagnosticLevel controls the verbosity of streaming diagnostics
type StreamDiagnosticLevel int

const (
	StreamLevelOff     StreamDiagnosticLevel = 0
	StreamLevelBasic   StreamDiagnosticLevel = 1
	StreamLevelVerbose StreamDiagnosticLevel = 2
	StreamLevelDebug   StreamDiagnosticLevel = 3
)

// StreamComponentStats holds statistics for a streaming component
type StreamComponentStats struct {
	Name         string                 `json:"name"`
	Enabled      bool                   `json:"enabled"`
	StartTime    time.Time              `json:"startTime"`
	Uptime       time.Duration          `json:"uptime"`
	ErrorCount   int64                  `json:"errorCount"`
	LastError    string                 `json:"lastError,omitempty"`
	LastErrorAt  time.Time              `json:"lastErrorAt,omitempty"`
	Metrics      map[string]interface{} `json:"metrics,omitempty"`
}

// CaptureStreamStats holds screen capture statistics
type CaptureStreamStats struct {
	FramesCaptured   uint64  `json:"framesCaptured"`
	FrameRate        float64 `json:"frameRate"`
	AverageCaptureMs float64 `json:"averageCaptureMs"`
	MinCaptureMs     float64 `json:"minCaptureMs"`
	MaxCaptureMs     float64 `json:"maxCaptureMs"`
	TotalBytes       uint64  `json:"totalBytes"`
	DirtyRects       uint64  `json:"dirtyRects"`
	FullFrames       uint64  `json:"fullFrames"`
	Resolution       string  `json:"resolution"`
	Monitor          string  `json:"monitor"`
	Backend          string  `json:"backend"` // "dxgi", "gdi", etc.
}

// EncoderStreamStats holds video encoder statistics
type EncoderStreamStats struct {
	FramesEncoded   uint64  `json:"framesEncoded"`
	FramesDropped   uint64  `json:"framesDropped"`
	AverageEncodeMs float64 `json:"averageEncodeMs"`
	MinEncodeMs     float64 `json:"minEncodeMs"`
	MaxEncodeMs     float64 `json:"maxEncodeMs"`
	BytesEncoded    uint64  `json:"bytesEncoded"`
	Bitrate         int     `json:"bitrate"`
	Codec           string  `json:"codec"`
	Profile         string  `json:"profile"`
	KeyFrames       uint64  `json:"keyFrames"`
	QP              int     `json:"qp"` // Quantization parameter
}

// NetworkStreamStats holds network/transport statistics
type NetworkStreamStats struct {
	BytesSent       uint64        `json:"bytesSent"`
	BytesReceived   uint64        `json:"bytesReceived"`
	PacketsSent     uint64        `json:"packetsSent"`
	PacketsReceived uint64        `json:"packetsReceived"`
	PacketsLost     uint64        `json:"packetsLost"`
	PacketLossRate  float64       `json:"packetLossRate"`
	RTT             time.Duration `json:"rtt"`
	Jitter          time.Duration `json:"jitter"`
	Bandwidth       int64         `json:"bandwidth"` // Estimated bandwidth in bps
	ICEState        string        `json:"iceState"`
	ConnectionState string        `json:"connectionState"`
	LocalCandidate  string        `json:"localCandidate,omitempty"`
	RemoteCandidate string        `json:"remoteCandidate,omitempty"`
}

// InputStreamStats holds input handling statistics
type InputStreamStats struct {
	MouseMoves     uint64  `json:"mouseMoves"`
	MouseClicks    uint64  `json:"mouseClicks"`
	KeyPresses     uint64  `json:"keyPresses"`
	WheelEvents    uint64  `json:"wheelEvents"`
	InputLatencyMs float64 `json:"inputLatencyMs"`
	DroppedInputs  uint64  `json:"droppedInputs"`
}

// AudioStreamStats holds audio streaming statistics
type AudioStreamStats struct {
	SamplesCaptured uint64  `json:"samplesCaptured"`
	PacketsEncoded  uint64  `json:"packetsEncoded"`
	PacketsSent     uint64  `json:"packetsSent"`
	DroppedFrames   uint64  `json:"droppedFrames"`
	SampleRate      int     `json:"sampleRate"`
	Channels        int     `json:"channels"`
	Bitrate         int     `json:"bitrate"`
	Enabled         bool    `json:"enabled"`
	Muted           bool    `json:"muted"`
	Volume          float64 `json:"volume"`
}

// ClipboardStreamStats holds clipboard sync statistics
type ClipboardStreamStats struct {
	SyncsSent     uint64 `json:"syncsSent"`
	SyncsReceived uint64 `json:"syncsReceived"`
	BytesSent     uint64 `json:"bytesSent"`
	BytesReceived uint64 `json:"bytesReceived"`
	Errors        uint64 `json:"errors"`
	Direction     string `json:"direction"`
}

// SystemStreamStats holds system resource statistics
type SystemStreamStats struct {
	CPUUsage       float64 `json:"cpuUsage"`
	MemoryUsed     uint64  `json:"memoryUsed"`
	MemoryTotal    uint64  `json:"memoryTotal"`
	MemoryPercent  float64 `json:"memoryPercent"`
	GoroutineCount int     `json:"goroutineCount"`
	GCPauseMs      float64 `json:"gcPauseMs"`
	HeapAlloc      uint64  `json:"heapAlloc"`
	HeapObjects    uint64  `json:"heapObjects"`
}

// FullStreamDiagnostics contains all streaming diagnostic information
type FullStreamDiagnostics struct {
	Timestamp    time.Time              `json:"timestamp"`
	SessionID    string                 `json:"sessionId"`
	AgentVersion string                 `json:"agentVersion"`
	Uptime       time.Duration          `json:"uptime"`
	Level        StreamDiagnosticLevel  `json:"level"`
	Capture      *CaptureStreamStats    `json:"capture,omitempty"`
	Encoder      *EncoderStreamStats    `json:"encoder,omitempty"`
	Network      *NetworkStreamStats    `json:"network,omitempty"`
	Input        *InputStreamStats      `json:"input,omitempty"`
	Audio        *AudioStreamStats      `json:"audio,omitempty"`
	Clipboard    *ClipboardStreamStats  `json:"clipboard,omitempty"`
	System       *SystemStreamStats     `json:"system,omitempty"`
	Components   []StreamComponentStats `json:"components,omitempty"`
	Warnings     []string               `json:"warnings,omitempty"`
	Errors       []string               `json:"errors,omitempty"`
}

// StreamLatencyMeasurement represents a single latency measurement
type StreamLatencyMeasurement struct {
	ID        string        `json:"id"`
	StartTime time.Time     `json:"startTime"`
	EndTime   time.Time     `json:"endTime,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
	Component string        `json:"component"`
	Success   bool          `json:"success"`
	Error     string        `json:"error,omitempty"`
}

// StreamDiagnosticsManager collects and reports streaming diagnostic information
type StreamDiagnosticsManager struct {
	level        StreamDiagnosticLevel
	startTime    time.Time
	sessionID    string
	agentVersion string

	// Stats
	capture   CaptureStreamStats
	encoder   EncoderStreamStats
	network   NetworkStreamStats
	input     InputStreamStats
	audio     AudioStreamStats
	clipboard ClipboardStreamStats

	// Component tracking
	components map[string]*StreamComponentStats

	// Latency measurements
	measurements map[string]*StreamLatencyMeasurement

	// Event log
	warnings []string
	errors   []string
	maxLogs  int

	// Callbacks
	onDiagnostics func(diag *FullStreamDiagnostics)

	mu sync.RWMutex
}

// NewStreamDiagnosticsManager creates a new streaming diagnostics manager
func NewStreamDiagnosticsManager(sessionID, agentVersion string) *StreamDiagnosticsManager {
	return &StreamDiagnosticsManager{
		level:        StreamLevelBasic,
		startTime:    time.Now(),
		sessionID:    sessionID,
		agentVersion: agentVersion,
		components:   make(map[string]*StreamComponentStats),
		measurements: make(map[string]*StreamLatencyMeasurement),
		maxLogs:      100,
	}
}

// SetLevel sets the diagnostic level
func (dm *StreamDiagnosticsManager) SetLevel(level StreamDiagnosticLevel) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.level = level
	log.Printf("[StreamDiagnostics] Level set to %d", level)
}

// GetLevel returns the current diagnostic level
func (dm *StreamDiagnosticsManager) GetLevel() StreamDiagnosticLevel {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return dm.level
}

// RegisterComponent registers a component for tracking
func (dm *StreamDiagnosticsManager) RegisterComponent(name string, enabled bool) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.components[name] = &StreamComponentStats{
		Name:      name,
		Enabled:   enabled,
		StartTime: time.Now(),
		Metrics:   make(map[string]interface{}),
	}

	log.Printf("[StreamDiagnostics] Registered component: %s (enabled=%v)", name, enabled)
}

// UpdateComponentMetric updates a metric for a component
func (dm *StreamDiagnosticsManager) UpdateComponentMetric(component, metric string, value interface{}) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if comp, ok := dm.components[component]; ok {
		comp.Metrics[metric] = value
	}
}

// RecordComponentError records an error for a component
func (dm *StreamDiagnosticsManager) RecordComponentError(component string, err error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if comp, ok := dm.components[component]; ok {
		comp.ErrorCount++
		comp.LastError = err.Error()
		comp.LastErrorAt = time.Now()
	}

	dm.addError(fmt.Sprintf("[%s] %v", component, err))
}

// RecordFrame records a captured frame
func (dm *StreamDiagnosticsManager) RecordFrame(captureMs float64, bytes uint64, isDirtyRect bool) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.capture.FramesCaptured++
	dm.capture.TotalBytes += bytes

	if isDirtyRect {
		dm.capture.DirtyRects++
	} else {
		dm.capture.FullFrames++
	}

	// Update min/max
	if dm.capture.MinCaptureMs == 0 || captureMs < dm.capture.MinCaptureMs {
		dm.capture.MinCaptureMs = captureMs
	}
	if captureMs > dm.capture.MaxCaptureMs {
		dm.capture.MaxCaptureMs = captureMs
	}

	// Running average
	if dm.capture.AverageCaptureMs == 0 {
		dm.capture.AverageCaptureMs = captureMs
	} else {
		dm.capture.AverageCaptureMs = dm.capture.AverageCaptureMs*0.9 + captureMs*0.1
	}
}

// RecordEncode records an encoded frame
func (dm *StreamDiagnosticsManager) RecordEncode(encodeMs float64, bytes uint64, isKeyFrame bool) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.encoder.FramesEncoded++
	dm.encoder.BytesEncoded += bytes

	if isKeyFrame {
		dm.encoder.KeyFrames++
	}

	// Update min/max
	if dm.encoder.MinEncodeMs == 0 || encodeMs < dm.encoder.MinEncodeMs {
		dm.encoder.MinEncodeMs = encodeMs
	}
	if encodeMs > dm.encoder.MaxEncodeMs {
		dm.encoder.MaxEncodeMs = encodeMs
	}

	// Running average
	if dm.encoder.AverageEncodeMs == 0 {
		dm.encoder.AverageEncodeMs = encodeMs
	} else {
		dm.encoder.AverageEncodeMs = dm.encoder.AverageEncodeMs*0.9 + encodeMs*0.1
	}
}

// UpdateNetworkStats updates network statistics
func (dm *StreamDiagnosticsManager) UpdateNetworkStats(stats NetworkStreamStats) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.network = stats
}

// RecordInput records an input event
func (dm *StreamDiagnosticsManager) RecordInput(inputType string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	switch inputType {
	case "move":
		dm.input.MouseMoves++
	case "click", "down", "up":
		dm.input.MouseClicks++
	case "keydown", "keyup":
		dm.input.KeyPresses++
	case "wheel":
		dm.input.WheelEvents++
	}
}

// StartMeasurement begins a latency measurement
func (dm *StreamDiagnosticsManager) StartMeasurement(id, component string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.measurements[id] = &StreamLatencyMeasurement{
		ID:        id,
		StartTime: time.Now(),
		Component: component,
	}
}

// EndMeasurement completes a latency measurement
func (dm *StreamDiagnosticsManager) EndMeasurement(id string, success bool, err error) time.Duration {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	m, ok := dm.measurements[id]
	if !ok {
		return 0
	}

	m.EndTime = time.Now()
	m.Duration = m.EndTime.Sub(m.StartTime)
	m.Success = success
	if err != nil {
		m.Error = err.Error()
	}

	return m.Duration
}

// AddWarning adds a warning message
func (dm *StreamDiagnosticsManager) AddWarning(msg string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.addWarning(msg)
}

func (dm *StreamDiagnosticsManager) addWarning(msg string) {
	if len(dm.warnings) >= dm.maxLogs {
		dm.warnings = dm.warnings[1:]
	}
	dm.warnings = append(dm.warnings, fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), msg))
}

// AddError adds an error message
func (dm *StreamDiagnosticsManager) AddError(msg string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.addError(msg)
}

func (dm *StreamDiagnosticsManager) addError(msg string) {
	if len(dm.errors) >= dm.maxLogs {
		dm.errors = dm.errors[1:]
	}
	dm.errors = append(dm.errors, fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), msg))
}

// GetDiagnostics returns full diagnostic information
func (dm *StreamDiagnosticsManager) GetDiagnostics() *FullStreamDiagnostics {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	diag := &FullStreamDiagnostics{
		Timestamp:    time.Now(),
		SessionID:    dm.sessionID,
		AgentVersion: dm.agentVersion,
		Uptime:       time.Since(dm.startTime),
		Level:        dm.level,
	}

	// Copy stats
	capture := dm.capture
	encoder := dm.encoder
	network := dm.network
	input := dm.input
	audio := dm.audio
	clipboard := dm.clipboard

	diag.Capture = &capture
	diag.Encoder = &encoder
	diag.Network = &network
	diag.Input = &input
	diag.Audio = &audio
	diag.Clipboard = &clipboard

	// Get system stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	diag.System = &SystemStreamStats{
		GoroutineCount: runtime.NumGoroutine(),
		HeapAlloc:      memStats.HeapAlloc,
		HeapObjects:    memStats.HeapObjects,
		GCPauseMs:      float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1e6,
		MemoryUsed:     memStats.Alloc,
		MemoryTotal:    memStats.Sys,
		MemoryPercent:  float64(memStats.Alloc) / float64(memStats.Sys) * 100,
	}

	// Copy components
	for _, comp := range dm.components {
		compCopy := *comp
		compCopy.Uptime = time.Since(comp.StartTime)
		diag.Components = append(diag.Components, compCopy)
	}

	// Copy logs
	diag.Warnings = make([]string, len(dm.warnings))
	copy(diag.Warnings, dm.warnings)
	diag.Errors = make([]string, len(dm.errors))
	copy(diag.Errors, dm.errors)

	return diag
}

// GetQuickStats returns a quick summary of stats
func (dm *StreamDiagnosticsManager) GetQuickStats() map[string]interface{} {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	return map[string]interface{}{
		"uptime":      time.Since(dm.startTime).String(),
		"frameRate":   dm.capture.FrameRate,
		"captureMs":   dm.capture.AverageCaptureMs,
		"encodeMs":    dm.encoder.AverageEncodeMs,
		"rtt":         dm.network.RTT.String(),
		"packetLoss":  dm.network.PacketLossRate,
		"bytesTotal":  dm.encoder.BytesEncoded,
		"framesTotal": dm.encoder.FramesEncoded,
		"goroutines":  runtime.NumGoroutine(),
	}
}

// StartPeriodicReport starts periodic diagnostic reporting
func (dm *StreamDiagnosticsManager) StartPeriodicReport(interval time.Duration, stopCh <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				dm.mu.RLock()
				callback := dm.onDiagnostics
				dm.mu.RUnlock()

				if callback != nil {
					diag := dm.GetDiagnostics()
					callback(diag)
				}
			}
		}
	}()
}

// OnDiagnostics sets the callback for periodic diagnostics
func (dm *StreamDiagnosticsManager) OnDiagnostics(callback func(*FullStreamDiagnostics)) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.onDiagnostics = callback
}

// Message types for streaming diagnostics protocol
const (
	StreamDiagMsgGetStats   = "streamDiag.getStats"
	StreamDiagMsgStats      = "streamDiag.stats"
	StreamDiagMsgSetLevel   = "streamDiag.setLevel"
	StreamDiagMsgPing       = "streamDiag.ping"
	StreamDiagMsgPong       = "streamDiag.pong"
	StreamDiagMsgQuickStats = "streamDiag.quickStats"
)

// StreamDiagPingMessage is a ping for latency measurement
type StreamDiagPingMessage struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp int64  `json:"timestamp"` // Unix milliseconds
}

// StreamDiagPongMessage is the response to a ping
type StreamDiagPongMessage struct {
	Type          string `json:"type"`
	ID            string `json:"id"`
	PingTimestamp int64  `json:"pingTimestamp"`
	PongTimestamp int64  `json:"pongTimestamp"`
}

// HandleMessage processes streaming diagnostic messages
func (dm *StreamDiagnosticsManager) HandleMessage(msgType string, data []byte) ([]byte, error) {
	switch msgType {
	case StreamDiagMsgGetStats:
		diag := dm.GetDiagnostics()
		return json.Marshal(map[string]interface{}{
			"type":        StreamDiagMsgStats,
			"diagnostics": diag,
		})

	case StreamDiagMsgSetLevel:
		var msg struct {
			Level StreamDiagnosticLevel `json:"level"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		dm.SetLevel(msg.Level)
		return json.Marshal(map[string]interface{}{
			"type":    "streamDiag.setLevelAck",
			"success": true,
			"level":   msg.Level,
		})

	case StreamDiagMsgPing:
		var msg StreamDiagPingMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		resp := StreamDiagPongMessage{
			Type:          StreamDiagMsgPong,
			ID:            msg.ID,
			PingTimestamp: msg.Timestamp,
			PongTimestamp: time.Now().UnixMilli(),
		}
		return json.Marshal(resp)

	case StreamDiagMsgQuickStats:
		stats := dm.GetQuickStats()
		return json.Marshal(map[string]interface{}{
			"type":  StreamDiagMsgQuickStats,
			"stats": stats,
		})
	}

	return nil, nil
}
