// Package recording provides session recording for remote desktop streams.
// It taps the H.264 encoded stream and writes raw Annex B bitstream files
// alongside JSON metadata sidecars for playback and compliance.
package recording

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RecordingConfig controls recording behavior.
type RecordingConfig struct {
	// OutputDir is the directory where recordings are stored.
	OutputDir string

	// MaxSizeMB is the maximum file size in megabytes before auto-stop (default 500).
	MaxSizeMB int64

	// MaxDurationMin is the maximum recording duration in minutes before auto-stop (default 120).
	MaxDurationMin int

	// Format is the output format. Currently only "annexb" (raw H.264 Annex B bitstream).
	Format string
}

// DefaultConfig returns a RecordingConfig with sensible defaults.
func DefaultConfig(outputDir string) RecordingConfig {
	return RecordingConfig{
		OutputDir:      outputDir,
		MaxSizeMB:      500,
		MaxDurationMin: 120,
		Format:         "annexb",
	}
}

// RecordingStats contains live statistics about the current recording.
type RecordingStats struct {
	FrameCount   uint64        `json:"frameCount"`
	BytesWritten int64         `json:"bytesWritten"`
	Duration     time.Duration `json:"duration"`
	FilePath     string        `json:"filePath"`
	Active       bool          `json:"active"`
}

// Recorder captures an H.264 stream to disk as a raw Annex B bitstream file.
type Recorder struct {
	mu           sync.Mutex
	active       bool
	file         *os.File
	writer       *annexBWriter
	sessionID    string
	startTime    time.Time
	frameCount   uint64
	bytesWritten int64
	config       RecordingConfig
	metadata     *Metadata
}

// NewRecorder creates a new Recorder with the given configuration.
// It applies defaults for any zero-valued config fields.
func NewRecorder(config RecordingConfig) *Recorder {
	if config.MaxSizeMB <= 0 {
		config.MaxSizeMB = 500
	}
	if config.MaxDurationMin <= 0 {
		config.MaxDurationMin = 120
	}
	if config.Format == "" {
		config.Format = "annexb"
	}
	return &Recorder{
		config: config,
	}
}

// Start begins recording a session. The output file is created at
// {OutputDir}/{sessionID}_{timestamp}.h264 with a JSON metadata sidecar.
func (r *Recorder) Start(sessionID string, width, height int, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.active {
		return fmt.Errorf("recording already active for session %s", r.sessionID)
	}

	if err := os.MkdirAll(r.config.OutputDir, 0700); err != nil {
		return fmt.Errorf("create recording output dir: %w", err)
	}

	timestamp := time.Now().UTC().Format("20060102T150405Z")
	filename := fmt.Sprintf("%s_%s.h264", sessionID, timestamp)
	filePath := filepath.Join(r.config.OutputDir, filename)

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create recording file: %w", err)
	}

	r.file = f
	r.writer = newAnnexBWriter(f)
	r.sessionID = sessionID
	r.startTime = time.Now()
	r.frameCount = 0
	r.bytesWritten = 0
	r.active = true

	r.metadata = NewMetadata(sessionID, userID, width, height)
	r.metadata.FilePath = filePath

	// Write initial metadata sidecar so it exists even if we crash
	metaPath := filePath + ".json"
	if err := r.metadata.Save(metaPath); err != nil {
		// Non-fatal: recording can proceed without metadata
		fmt.Fprintf(os.Stderr, "recording: failed to write initial metadata: %v\n", err)
	}

	return nil
}

// WriteFrame writes NAL units to the recording file. Each call represents
// one access unit (frame). The nalUnits slice contains one or more NAL units
// that will each be prefixed with a 4-byte Annex B start code.
//
// Returns an error if recording is not active or limits are exceeded.
// When a limit is hit, the recording is automatically stopped and
// ErrLimitReached is returned.
func (r *Recorder) WriteFrame(nalUnits []byte, pts time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.active {
		return nil // Silently discard if not recording
	}

	// Check duration limit
	elapsed := time.Since(r.startTime)
	maxDuration := time.Duration(r.config.MaxDurationMin) * time.Minute
	if elapsed >= maxDuration {
		r.stopLocked()
		return ErrLimitReached
	}

	// Check size limit
	maxBytes := r.config.MaxSizeMB * 1024 * 1024
	if r.bytesWritten >= maxBytes {
		r.stopLocked()
		return ErrLimitReached
	}

	n, err := r.writer.WriteNALUnits(nalUnits)
	if err != nil {
		return fmt.Errorf("write frame: %w", err)
	}

	r.frameCount++
	r.bytesWritten += int64(n)

	return nil
}

// Stop finalizes the recording, writes final metadata, and closes the file.
func (r *Recorder) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopLocked()
}

// stopLocked performs the actual stop. Caller must hold r.mu.
func (r *Recorder) stopLocked() error {
	if !r.active {
		return nil
	}

	r.active = false

	// Flush the buffered writer
	if r.writer != nil {
		if err := r.writer.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "recording: flush error: %v\n", err)
		}
	}

	// Finalize metadata
	if r.metadata != nil {
		r.metadata.Finalize(time.Now(), r.frameCount, r.bytesWritten)
		metaPath := r.metadata.FilePath + ".json"
		if err := r.metadata.Save(metaPath); err != nil {
			fmt.Fprintf(os.Stderr, "recording: failed to save metadata: %v\n", err)
		}
	}

	// Close the file
	var closeErr error
	if r.file != nil {
		closeErr = r.file.Close()
		r.file = nil
	}
	r.writer = nil

	return closeErr
}

// IsActive returns whether the recorder is currently capturing.
func (r *Recorder) IsActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// GetStats returns current recording statistics.
func (r *Recorder) GetStats() RecordingStats {
	r.mu.Lock()
	defer r.mu.Unlock()

	var duration time.Duration
	var filePath string
	if r.active {
		duration = time.Since(r.startTime)
	}
	if r.metadata != nil {
		filePath = r.metadata.FilePath
		if !r.active {
			duration = r.metadata.Duration
		}
	}

	return RecordingStats{
		FrameCount:   r.frameCount,
		BytesWritten: r.bytesWritten,
		Duration:     duration,
		FilePath:     filePath,
		Active:       r.active,
	}
}

// ErrLimitReached is returned by WriteFrame when the recording was auto-stopped
// because a size or duration limit was hit.
var ErrLimitReached = fmt.Errorf("recording limit reached, recording stopped")

// annexBWriter wraps a file writer to produce raw H.264 Annex B bitstream output.
// It prepends the 4-byte start code (0x00 0x00 0x00 0x01) to each NAL unit and
// extracts SPS/PPS parameter sets for reference.
type annexBWriter struct {
	bw  *bufio.Writer
	sps []byte // Most recent Sequence Parameter Set
	pps []byte // Most recent Picture Parameter Set
}

// NAL unit type constants (5-bit field in the first byte after start code)
const (
	nalTypeSPS = 7
	nalTypePPS = 8
)

// annexBStartCode is the 4-byte Annex B start code prefix.
var annexBStartCode = []byte{0x00, 0x00, 0x00, 0x01}

func newAnnexBWriter(f *os.File) *annexBWriter {
	return &annexBWriter{
		bw: bufio.NewWriterSize(f, 256*1024), // 256KB buffer for write performance
	}
}

// WriteNALUnits writes raw NAL unit data to the bitstream. The input nalUnits
// may contain one or more NAL units. If the data already contains Annex B start
// codes, it is written as-is. Otherwise, each NAL unit is prefixed with a start code.
//
// Returns the total number of bytes written.
func (w *annexBWriter) WriteNALUnits(nalUnits []byte) (int, error) {
	if len(nalUnits) == 0 {
		return 0, nil
	}

	// Check if the data already has Annex B start codes
	if hasStartCode(nalUnits) {
		// Data is already in Annex B format - write directly and extract params
		w.extractParams(nalUnits)
		n, err := w.bw.Write(nalUnits)
		return n, err
	}

	// No start codes present - treat as a single NAL unit, prepend start code
	w.extractParamsSingle(nalUnits)

	total := 0
	n, err := w.bw.Write(annexBStartCode)
	total += n
	if err != nil {
		return total, err
	}

	n, err = w.bw.Write(nalUnits)
	total += n
	return total, err
}

// Flush flushes the buffered writer to disk.
func (w *annexBWriter) Flush() error {
	return w.bw.Flush()
}

// GetSPS returns the most recently seen Sequence Parameter Set, or nil.
func (w *annexBWriter) GetSPS() []byte {
	return w.sps
}

// GetPPS returns the most recently seen Picture Parameter Set, or nil.
func (w *annexBWriter) GetPPS() []byte {
	return w.pps
}

// hasStartCode checks if the byte slice begins with a 3-byte or 4-byte start code.
func hasStartCode(data []byte) bool {
	if len(data) >= 4 && data[0] == 0 && data[1] == 0 && data[2] == 0 && data[3] == 1 {
		return true
	}
	if len(data) >= 3 && data[0] == 0 && data[1] == 0 && data[2] == 1 {
		return true
	}
	return false
}

// extractParams scans Annex B formatted data for SPS/PPS NAL units and stores them.
func (w *annexBWriter) extractParams(data []byte) {
	// Walk through the bitstream finding start codes
	i := 0
	for i < len(data) {
		// Find next start code
		scLen := 0
		if i+3 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			scLen = 4
		} else if i+2 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			scLen = 3
		} else {
			i++
			continue
		}

		nalStart := i + scLen
		if nalStart >= len(data) {
			break
		}

		// Find the end of this NAL unit (next start code or end of data)
		nalEnd := len(data)
		for j := nalStart + 1; j < len(data)-2; j++ {
			if data[j] == 0 && data[j+1] == 0 && (data[j+2] == 1 || (j+3 < len(data) && data[j+2] == 0 && data[j+3] == 1)) {
				nalEnd = j
				break
			}
		}

		nalType := data[nalStart] & 0x1F
		nalData := data[nalStart:nalEnd]

		switch nalType {
		case nalTypeSPS:
			w.sps = make([]byte, len(nalData))
			copy(w.sps, nalData)
		case nalTypePPS:
			w.pps = make([]byte, len(nalData))
			copy(w.pps, nalData)
		}

		i = nalEnd
	}
}

// extractParamsSingle checks a single NAL unit (without start code) for SPS/PPS.
func (w *annexBWriter) extractParamsSingle(nalUnit []byte) {
	if len(nalUnit) == 0 {
		return
	}
	nalType := nalUnit[0] & 0x1F
	switch nalType {
	case nalTypeSPS:
		w.sps = make([]byte, len(nalUnit))
		copy(w.sps, nalUnit)
	case nalTypePPS:
		w.pps = make([]byte, len(nalUnit))
		copy(w.pps, nalUnit)
	}
}
