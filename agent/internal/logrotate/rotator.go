package logrotate

import (
	"fmt"
	"os"
	"sync"
)

const (
	// DefaultMaxSize is the default maximum log file size (10MB)
	DefaultMaxSize = 10 * 1024 * 1024
	// DefaultMaxFiles is the default number of rotated files to keep
	DefaultMaxFiles = 5
)

// RotatingWriter is an io.Writer that automatically rotates log files
// when they exceed a configured maximum size. It is safe for concurrent use.
type RotatingWriter struct {
	mu       sync.Mutex
	file     *os.File
	filePath string
	maxSize  int64
	maxFiles int
	size     int64
}

// New creates a new RotatingWriter.
//   - filePath: path to the primary log file (e.g. /var/log/sentinel/agent.log)
//   - maxSize: maximum file size in bytes before rotation (0 uses DefaultMaxSize)
//   - maxFiles: number of rotated files to keep (0 uses DefaultMaxFiles)
func New(filePath string, maxSize int64, maxFiles int) (*RotatingWriter, error) {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFiles
	}

	rw := &RotatingWriter{
		filePath: filePath,
		maxSize:  maxSize,
		maxFiles: maxFiles,
	}

	if err := rw.openFile(); err != nil {
		return nil, err
	}

	return rw, nil
}

// openFile opens (or creates) the log file and records its current size.
func (rw *RotatingWriter) openFile() error {
	f, err := os.OpenFile(rw.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", rw.filePath, err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("stat log file %s: %w", rw.filePath, err)
	}

	rw.file = f
	rw.size = info.Size()
	return nil
}

// Write implements io.Writer. If the write would cause the file to exceed
// maxSize, the file is rotated first.
func (rw *RotatingWriter) Write(p []byte) (n int, err error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	if rw.size+int64(len(p)) > rw.maxSize {
		if rotErr := rw.rotate(); rotErr != nil {
			// If rotation fails, still try to write to the current file
			// so we don't lose log data silently
			_ = rotErr
		}
	}

	n, err = rw.file.Write(p)
	rw.size += int64(n)
	return n, err
}

// rotate closes the current file and shifts existing rotated files:
//
//	agent.log.4 -> deleted
//	agent.log.3 -> agent.log.4
//	agent.log.2 -> agent.log.3
//	agent.log.1 -> agent.log.2
//	agent.log   -> agent.log.1
//
// Then opens a fresh agent.log.
func (rw *RotatingWriter) rotate() error {
	// Close current file
	if rw.file != nil {
		rw.file.Close()
		rw.file = nil
	}

	// Remove the oldest rotated file if it exists
	oldest := fmt.Sprintf("%s.%d", rw.filePath, rw.maxFiles)
	os.Remove(oldest)

	// Shift existing rotated files
	for i := rw.maxFiles - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", rw.filePath, i)
		dst := fmt.Sprintf("%s.%d", rw.filePath, i+1)
		os.Rename(src, dst)
	}

	// Move current log to .1
	os.Rename(rw.filePath, fmt.Sprintf("%s.1", rw.filePath))

	// Open fresh file
	return rw.openFile()
}

// Close closes the underlying file.
func (rw *RotatingWriter) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.file != nil {
		err := rw.file.Close()
		rw.file = nil
		return err
	}
	return nil
}
