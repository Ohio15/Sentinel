package terminal

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditLogger logs terminal session events for security auditing
type AuditLogger struct {
	mu       sync.Mutex
	file     *os.File
	filePath string
}

// NewAuditLogger creates an audit logger that writes to the specified directory
func NewAuditLogger(logDir string) (*AuditLogger, error) {
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil, fmt.Errorf("create audit log dir: %w", err)
	}
	path := filepath.Join(logDir, "terminal-audit.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	return &AuditLogger{file: f, filePath: path}, nil
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
	if a.file != nil {
		fmt.Fprintf(a.file, "[%s] %s %s\n", time.Now().UTC().Format(time.RFC3339), event, detail)
	}
}

// Close closes the audit log file
func (a *AuditLogger) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.file != nil {
		return a.file.Close()
	}
	return nil
}
