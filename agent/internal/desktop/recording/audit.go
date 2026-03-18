package recording

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sentinel/agent/internal/logrotate"
)

// AuditLogger logs recording lifecycle events for compliance and security auditing.
// Each log entry is a structured line with timestamp, action, and key-value details.
type AuditLogger struct {
	mu       sync.Mutex
	writer   io.Writer
	closer   io.Closer
	filePath string
}

// NewAuditLogger creates an audit logger that writes to the specified directory.
// Uses log rotation: 5MB per file, 10 rotated files (50MB total).
func NewAuditLogger(logDir string) (*AuditLogger, error) {
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil, fmt.Errorf("create recording audit log dir: %w", err)
	}
	path := filepath.Join(logDir, "recording-audit.log")
	writer, err := logrotate.New(path, 5*1024*1024, 10)
	if err != nil {
		return nil, fmt.Errorf("create recording audit log rotator: %w", err)
	}
	return &AuditLogger{writer: writer, closer: writer, filePath: path}, nil
}

// LogStart records that a session recording was initiated.
func (a *AuditLogger) LogStart(sessionID, userID, deviceID string) {
	a.write("RECORDING_START", fmt.Sprintf(
		"session=%s user=%s device=%s",
		sessionID, userID, deviceID,
	))
}

// LogStop records that a session recording was stopped, including duration and frame count.
func (a *AuditLogger) LogStop(sessionID string, duration time.Duration, frameCount uint64) {
	a.write("RECORDING_STOP", fmt.Sprintf(
		"session=%s duration=%s frames=%d",
		sessionID, duration, frameCount,
	))
}

// LogAccess records that a recording file was accessed (viewed or downloaded).
func (a *AuditLogger) LogAccess(sessionID, accessedBy string) {
	a.write("RECORDING_ACCESS", fmt.Sprintf(
		"session=%s accessed_by=%s",
		sessionID, accessedBy,
	))
}

// LogDelete records that a recording file was deleted.
func (a *AuditLogger) LogDelete(sessionID, deletedBy string) {
	a.write("RECORDING_DELETE", fmt.Sprintf(
		"session=%s deleted_by=%s",
		sessionID, deletedBy,
	))
}

// LogPrune records that recordings were pruned by the storage manager.
func (a *AuditLogger) LogPrune(deletedCount int, freedBytes int64) {
	a.write("RECORDING_PRUNE", fmt.Sprintf(
		"deleted=%d freed_bytes=%d",
		deletedCount, freedBytes,
	))
}

// LogError records an error during recording operations.
func (a *AuditLogger) LogError(sessionID, operation, errMsg string) {
	a.write("RECORDING_ERROR", fmt.Sprintf(
		"session=%s op=%s error=%q",
		sessionID, operation, errMsg,
	))
}

func (a *AuditLogger) write(event, detail string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.writer != nil {
		fmt.Fprintf(a.writer, "[%s] %s %s\n",
			time.Now().UTC().Format(time.RFC3339), event, detail)
	}
}

// Close closes the audit log writer.
func (a *AuditLogger) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closer != nil {
		return a.closer.Close()
	}
	return nil
}
