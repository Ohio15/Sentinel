package recording

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// StorageConfig controls retention and cleanup of recorded sessions.
type StorageConfig struct {
	// MaxTotalSizeMB is the maximum total size of all recordings in megabytes (default 5000).
	MaxTotalSizeMB int64

	// MaxRetentionDays is the maximum age of recordings in days (default 30).
	MaxRetentionDays int

	// OutputDir is the directory containing recordings.
	OutputDir string
}

// DefaultStorageConfig returns a StorageConfig with sensible defaults.
func DefaultStorageConfig(outputDir string) StorageConfig {
	return StorageConfig{
		MaxTotalSizeMB:   5000,
		MaxRetentionDays: 30,
		OutputDir:        outputDir,
	}
}

// RecordingInfo describes a recording on disk.
type RecordingInfo struct {
	FilePath     string        `json:"filePath"`
	MetadataPath string        `json:"metadataPath"`
	Size         int64         `json:"size"`
	ModTime      time.Time     `json:"modTime"`
	Metadata     *Metadata     `json:"metadata,omitempty"`
	Duration     time.Duration `json:"duration,omitempty"`
}

// StorageManager handles listing, pruning, and deleting recordings.
type StorageManager struct {
	config StorageConfig
}

// NewStorageManager creates a StorageManager with the given configuration.
func NewStorageManager(config StorageConfig) *StorageManager {
	if config.MaxTotalSizeMB <= 0 {
		config.MaxTotalSizeMB = 5000
	}
	if config.MaxRetentionDays <= 0 {
		config.MaxRetentionDays = 30
	}
	return &StorageManager{config: config}
}

// GetRecordings returns all recordings in the output directory with their metadata.
func (s *StorageManager) GetRecordings() ([]RecordingInfo, error) {
	entries, err := os.ReadDir(s.config.OutputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read recordings dir: %w", err)
	}

	var recordings []RecordingInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".h264") {
			continue
		}

		filePath := filepath.Join(s.config.OutputDir, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}

		rec := RecordingInfo{
			FilePath:     filePath,
			MetadataPath: filePath + ".json",
			Size:         info.Size(),
			ModTime:      info.ModTime(),
		}

		// Try to load metadata sidecar
		meta, err := LoadMetadata(rec.MetadataPath)
		if err == nil {
			rec.Metadata = meta
			rec.Duration = meta.Duration
		}

		recordings = append(recordings, rec)
	}

	// Sort by modification time, newest first
	sort.Slice(recordings, func(i, j int) bool {
		return recordings[i].ModTime.After(recordings[j].ModTime)
	})

	return recordings, nil
}

// Prune deletes recordings that exceed the retention period or total size limit.
// Oldest recordings are deleted first when the size limit is exceeded.
func (s *StorageManager) Prune() error {
	recordings, err := s.GetRecordings()
	if err != nil {
		return err
	}

	if len(recordings) == 0 {
		return nil
	}

	now := time.Now()
	cutoff := now.AddDate(0, 0, -s.config.MaxRetentionDays)

	// Phase 1: Delete recordings older than retention period
	var remaining []RecordingInfo
	for _, rec := range recordings {
		if rec.ModTime.Before(cutoff) {
			if err := s.deleteFiles(rec); err != nil {
				fmt.Fprintf(os.Stderr, "recording: prune age delete %s: %v\n", rec.FilePath, err)
			}
		} else {
			remaining = append(remaining, rec)
		}
	}

	// Phase 2: Delete oldest recordings if total size exceeds limit
	maxBytes := s.config.MaxTotalSizeMB * 1024 * 1024
	var totalSize int64
	for _, rec := range remaining {
		totalSize += rec.Size
	}

	if totalSize <= maxBytes {
		return nil
	}

	// Sort remaining by mod time ascending (oldest first) for deletion
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i].ModTime.Before(remaining[j].ModTime)
	})

	for _, rec := range remaining {
		if totalSize <= maxBytes {
			break
		}
		totalSize -= rec.Size
		if err := s.deleteFiles(rec); err != nil {
			fmt.Fprintf(os.Stderr, "recording: prune size delete %s: %v\n", rec.FilePath, err)
		}
	}

	return nil
}

// DeleteRecording deletes a recording file and its metadata sidecar.
func (s *StorageManager) DeleteRecording(path string) error {
	rec := RecordingInfo{
		FilePath:     path,
		MetadataPath: path + ".json",
	}
	return s.deleteFiles(rec)
}

// deleteFiles removes the .h264 file and its .json sidecar.
func (s *StorageManager) deleteFiles(rec RecordingInfo) error {
	var firstErr error

	if err := os.Remove(rec.FilePath); err != nil && !os.IsNotExist(err) {
		firstErr = err
	}
	if err := os.Remove(rec.MetadataPath); err != nil && !os.IsNotExist(err) {
		if firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		return fmt.Errorf("delete recording files: %w", firstErr)
	}
	return nil
}
