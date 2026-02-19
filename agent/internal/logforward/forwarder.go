// Package logforward provides a ring buffer log forwarder that captures log entries,
// batches them, and sends them to the server via WebSocket.
package logforward

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"
)

// Entry represents a single log entry to be forwarded
type Entry struct {
	Level    string                 `json:"level"`
	Source   string                 `json:"source"`
	Message  string                 `json:"message"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	LoggedAt time.Time              `json:"loggedAt"`
}

// Sender is the interface for sending JSON messages to the server
type Sender interface {
	SendJSON(msg interface{}) error
	IsConnected() bool
	IsAuthenticated() bool
}

// Forwarder collects log entries in a ring buffer and periodically flushes them to the server
type Forwarder struct {
	buffer   []Entry
	mu       sync.Mutex
	maxSize  int
	sender   Sender
	interval time.Duration
}

// New creates a new log forwarder
func New(sender Sender) *Forwarder {
	return &Forwarder{
		buffer:   make([]Entry, 0, 500),
		maxSize:  500,
		sender:   sender,
		interval: 30 * time.Second,
	}
}

// Log adds a log entry to the buffer
func (f *Forwarder) Log(level, source, message string, metadata map[string]interface{}) {
	f.mu.Lock()
	defer f.mu.Unlock()

	entry := Entry{
		Level:    level,
		Source:   source,
		Message:  message,
		Metadata: metadata,
		LoggedAt: time.Now(),
	}

	if len(f.buffer) >= f.maxSize {
		// Ring buffer: drop oldest entry
		f.buffer = f.buffer[1:]
	}
	f.buffer = append(f.buffer, entry)
}

// Start begins the background flush loop
func (f *Forwarder) Start(ctx context.Context) {
	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final flush on shutdown
			f.Flush()
			return
		case <-ticker.C:
			f.Flush()
		}
	}
}

// Flush sends all buffered entries to the server and clears the buffer
func (f *Forwarder) Flush() {
	f.mu.Lock()
	if len(f.buffer) == 0 {
		f.mu.Unlock()
		return
	}

	// Take ownership of buffer entries
	entries := make([]Entry, len(f.buffer))
	copy(entries, f.buffer)
	f.buffer = f.buffer[:0]
	f.mu.Unlock()

	if f.sender == nil || !f.sender.IsConnected() || !f.sender.IsAuthenticated() {
		// Not connected, re-buffer the entries (they'll be lost if buffer is full)
		f.mu.Lock()
		remaining := f.maxSize - len(f.buffer)
		if remaining > 0 {
			if len(entries) > remaining {
				entries = entries[len(entries)-remaining:]
			}
			f.buffer = append(entries, f.buffer...)
		}
		f.mu.Unlock()
		return
	}

	msg := map[string]interface{}{
		"type": "agent_logs",
		"logs": entries,
	}

	if err := f.sender.SendJSON(msg); err != nil {
		log.Printf("[LogForward] Failed to send %d log entries: %v", len(entries), err)
		// Re-buffer on failure (best effort)
		f.mu.Lock()
		remaining := f.maxSize - len(f.buffer)
		if remaining > 0 {
			if len(entries) > remaining {
				entries = entries[len(entries)-remaining:]
			}
			f.buffer = append(entries, f.buffer...)
		}
		f.mu.Unlock()
	}
}

// LogWriter wraps the forwarder as an io.Writer for use with log.SetOutput
// This captures standard log output and forwards it
type LogWriter struct {
	forwarder *Forwarder
	source    string
	original  *log.Logger
}

// NewLogWriter creates a log writer that tees output to both the original logger and the forwarder
func NewLogWriter(forwarder *Forwarder, source string) *LogWriter {
	return &LogWriter{
		forwarder: forwarder,
		source:    source,
	}
}

// Write implements io.Writer, capturing log lines
func (w *LogWriter) Write(p []byte) (n int, err error) {
	msg := string(p)
	// Determine log level from message prefix
	level := "info"
	if len(msg) > 0 {
		switch {
		case contains(msg, "ERROR") || contains(msg, "CRITICAL") || contains(msg, "FATAL"):
			level = "error"
		case contains(msg, "WARN") || contains(msg, "Warning"):
			level = "warn"
		case contains(msg, "DEBUG"):
			level = "debug"
		}
	}
	w.forwarder.Log(level, w.source, msg, nil)
	return len(p), nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// MarshalJSON implements custom JSON marshaling for Entry
func (e Entry) MarshalJSON() ([]byte, error) {
	type Alias Entry
	return json.Marshal(&struct {
		Alias
	}{
		Alias: (Alias)(e),
	})
}
