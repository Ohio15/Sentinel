package recording

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Metadata describes a recording file and its associated session details.
// It is stored as a JSON sidecar alongside the .h264 file.
type Metadata struct {
	SessionID    string        `json:"sessionId"`
	UserID       string        `json:"userId"`
	DeviceID     string        `json:"deviceId,omitempty"`
	StartTime    time.Time     `json:"startTime"`
	EndTime      time.Time     `json:"endTime,omitempty"`
	Duration     time.Duration `json:"duration,omitempty"`
	Width        int           `json:"width"`
	Height       int           `json:"height"`
	FrameCount   uint64        `json:"frameCount"`
	BytesWritten int64         `json:"bytesWritten"`
	FilePath     string        `json:"filePath"`
	Codec        string        `json:"codec"`
	Format       string        `json:"format"`
}

// metadataJSON is the JSON-friendly representation that serializes Duration as a string.
type metadataJSON struct {
	SessionID    string `json:"sessionId"`
	UserID       string `json:"userId"`
	DeviceID     string `json:"deviceId,omitempty"`
	StartTime    string `json:"startTime"`
	EndTime      string `json:"endTime,omitempty"`
	DurationSec  float64 `json:"durationSec"`
	DurationStr  string `json:"duration"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FrameCount   uint64 `json:"frameCount"`
	BytesWritten int64  `json:"bytesWritten"`
	FilePath     string `json:"filePath"`
	Codec        string `json:"codec"`
	Format       string `json:"format"`
}

// NewMetadata creates a new Metadata for a recording session.
func NewMetadata(sessionID, userID string, width, height int) *Metadata {
	return &Metadata{
		SessionID: sessionID,
		UserID:    userID,
		StartTime: time.Now().UTC(),
		Width:     width,
		Height:    height,
		Codec:     "h264",
		Format:    "annexb",
	}
}

// Finalize sets the end time and final statistics on the metadata.
func (m *Metadata) Finalize(endTime time.Time, frameCount uint64, bytesWritten int64) {
	m.EndTime = endTime.UTC()
	m.Duration = m.EndTime.Sub(m.StartTime)
	m.FrameCount = frameCount
	m.BytesWritten = bytesWritten
}

// Save writes the metadata as a JSON file at the given path.
func (m *Metadata) Save(path string) error {
	mj := metadataJSON{
		SessionID:    m.SessionID,
		UserID:       m.UserID,
		DeviceID:     m.DeviceID,
		StartTime:    m.StartTime.Format(time.RFC3339),
		Width:        m.Width,
		Height:       m.Height,
		FrameCount:   m.FrameCount,
		BytesWritten: m.BytesWritten,
		FilePath:     m.FilePath,
		Codec:        m.Codec,
		Format:       m.Format,
		DurationSec:  m.Duration.Seconds(),
		DurationStr:  m.Duration.String(),
	}
	if !m.EndTime.IsZero() {
		mj.EndTime = m.EndTime.Format(time.RFC3339)
	}

	data, err := json.MarshalIndent(mj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write metadata file: %w", err)
	}
	return nil
}

// LoadMetadata reads a Metadata from a JSON sidecar file.
func LoadMetadata(path string) (*Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metadata file: %w", err)
	}

	var mj metadataJSON
	if err := json.Unmarshal(data, &mj); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}

	startTime, err := time.Parse(time.RFC3339, mj.StartTime)
	if err != nil {
		return nil, fmt.Errorf("parse start time: %w", err)
	}

	m := &Metadata{
		SessionID:    mj.SessionID,
		UserID:       mj.UserID,
		DeviceID:     mj.DeviceID,
		StartTime:    startTime,
		Width:        mj.Width,
		Height:       mj.Height,
		FrameCount:   mj.FrameCount,
		BytesWritten: mj.BytesWritten,
		FilePath:     mj.FilePath,
		Codec:        mj.Codec,
		Format:       mj.Format,
		Duration:     time.Duration(mj.DurationSec * float64(time.Second)),
	}

	if mj.EndTime != "" {
		endTime, err := time.Parse(time.RFC3339, mj.EndTime)
		if err != nil {
			return nil, fmt.Errorf("parse end time: %w", err)
		}
		m.EndTime = endTime
	}

	return m, nil
}
