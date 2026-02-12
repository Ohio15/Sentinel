// Package filewatcher provides file system monitoring for USB drives
// to track files transferred to removable storage devices.
package filewatcher

import (
	"context"
	"sync"
	"time"
)

// FileTransfer represents a file written to a USB drive
type FileTransfer struct {
	FileName     string    `json:"fileName"`
	FilePath     string    `json:"filePath"`     // Relative path on USB
	FileSize     int64     `json:"fileSize"`
	TransferTime time.Time `json:"transferTime"`
	Operation    string    `json:"operation"`    // write, rename, copy
}

// Watcher monitors a directory for file changes
type Watcher interface {
	// Start begins monitoring the directory
	Start(ctx context.Context) error

	// Stop stops monitoring and returns accumulated transfers
	Stop() []FileTransfer

	// Path returns the monitored directory path
	Path() string

	// Transfers returns current accumulated transfers without stopping
	Transfers() []FileTransfer
}

// Config holds configuration for the file watcher
type Config struct {
	// MaxFiles limits the number of files tracked per session (default: 1000)
	MaxFiles int

	// DebounceMs is the debounce time in milliseconds to wait for file writes to complete (default: 500)
	DebounceMs int

	// ExcludePatterns are glob patterns for files to ignore
	ExcludePatterns []string
}

// DefaultConfig returns the default watcher configuration
func DefaultConfig() Config {
	return Config{
		MaxFiles:   1000,
		DebounceMs: 500,
		ExcludePatterns: []string{
			"~$*",           // Office temp files
			"*.tmp",         // Temp files
			"*.TMP",
			"Thumbs.db",     // Windows thumbnail cache
			"desktop.ini",   // Windows folder settings
			".DS_Store",     // macOS
			"*.part",        // Partial downloads
			"*.crdownload",  // Chrome downloads
		},
	}
}

// baseWatcher provides common functionality for platform-specific watchers
type baseWatcher struct {
	path      string
	config    Config
	transfers []FileTransfer
	pending   map[string]*pendingFile // Files being written (debounce)
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	running   bool
}

// pendingFile tracks a file that's being written
type pendingFile struct {
	path      string
	size      int64
	lastWrite time.Time
}

// newBaseWatcher creates a new base watcher
func newBaseWatcher(path string, config Config) *baseWatcher {
	if config.MaxFiles == 0 {
		config.MaxFiles = DefaultConfig().MaxFiles
	}
	if config.DebounceMs == 0 {
		config.DebounceMs = DefaultConfig().DebounceMs
	}
	if len(config.ExcludePatterns) == 0 {
		config.ExcludePatterns = DefaultConfig().ExcludePatterns
	}

	return &baseWatcher{
		path:      path,
		config:    config,
		transfers: make([]FileTransfer, 0),
		pending:   make(map[string]*pendingFile),
	}
}

// Path returns the monitored directory
func (w *baseWatcher) Path() string {
	return w.path
}

// Transfers returns current accumulated transfers
func (w *baseWatcher) Transfers() []FileTransfer {
	w.mu.Lock()
	defer w.mu.Unlock()

	result := make([]FileTransfer, len(w.transfers))
	copy(result, w.transfers)
	return result
}

// addTransfer adds a file transfer to the list (thread-safe)
func (w *baseWatcher) addTransfer(transfer FileTransfer) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check max files limit
	if len(w.transfers) >= w.config.MaxFiles {
		return
	}

	w.transfers = append(w.transfers, transfer)
}

// getTransfers returns and clears the transfer list
func (w *baseWatcher) getTransfers() []FileTransfer {
	w.mu.Lock()
	defer w.mu.Unlock()

	result := w.transfers
	w.transfers = make([]FileTransfer, 0)
	return result
}

// shouldExclude checks if a file should be excluded based on patterns
func (w *baseWatcher) shouldExclude(filename string) bool {
	for _, pattern := range w.config.ExcludePatterns {
		if matched, _ := matchPattern(pattern, filename); matched {
			return true
		}
	}
	return false
}

// matchPattern performs simple glob matching
func matchPattern(pattern, name string) (bool, error) {
	// Simple glob matching for common patterns
	if len(pattern) == 0 {
		return false, nil
	}

	// Handle prefix wildcard (e.g., "~$*")
	if pattern[0] != '*' && len(pattern) > 1 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			return true, nil
		}
	}

	// Handle suffix wildcard (e.g., "*.tmp")
	if pattern[0] == '*' && len(pattern) > 1 {
		suffix := pattern[1:]
		if len(name) >= len(suffix) && name[len(name)-len(suffix):] == suffix {
			return true, nil
		}
	}

	// Exact match
	return pattern == name, nil
}

// New creates a new file watcher for the given path
func New(path string, config Config) Watcher {
	return newPlatformWatcher(path, config)
}

// NewWithDefaults creates a new file watcher with default configuration
func NewWithDefaults(path string) Watcher {
	return New(path, DefaultConfig())
}
