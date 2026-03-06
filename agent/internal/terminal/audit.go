package terminal

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sentinel/agent/internal/logrotate"
)

// AuditLogger logs terminal session events for security auditing
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
		return nil, fmt.Errorf("create audit log dir: %w", err)
	}
	path := filepath.Join(logDir, "terminal-audit.log")
	writer, err := logrotate.New(path, 5*1024*1024, 10) // 5MB per file, 10 rotated files
	if err != nil {
		return nil, fmt.Errorf("create audit log rotator: %w", err)
	}
	return &AuditLogger{writer: writer, closer: writer, filePath: path}, nil
}

// LogSessionStart records that a terminal session was initiated
func (a *AuditLogger) LogSessionStart(sessionID, requestedBy string) {
	a.write("SESSION_START", fmt.Sprintf("session=%s requested_by=%s", sessionID, requestedBy))
}

// LogInput records a command sent to a terminal session
func (a *AuditLogger) LogInput(sessionID, input string) {
	a.write("INPUT", fmt.Sprintf("session=%s cmd=%q", sessionID, input))
}

// LogSessionEnd records that a terminal session has ended
func (a *AuditLogger) LogSessionEnd(sessionID string, duration time.Duration, cmdCount int) {
	a.write("SESSION_END", fmt.Sprintf("session=%s duration=%s commands=%d", sessionID, duration, cmdCount))
}

func (a *AuditLogger) write(event, detail string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.writer != nil {
		fmt.Fprintf(a.writer, "[%s] %s %s\n", time.Now().UTC().Format(time.RFC3339), event, detail)
	}
}

// Close closes the audit log writer
func (a *AuditLogger) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closer != nil {
		return a.closer.Close()
	}
	return nil
}
