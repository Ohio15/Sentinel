//go:build windows

package filewatcher

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	FILE_NOTIFY_CHANGE_FILE_NAME  = 0x00000001
	FILE_NOTIFY_CHANGE_DIR_NAME   = 0x00000002
	FILE_NOTIFY_CHANGE_SIZE       = 0x00000008
	FILE_NOTIFY_CHANGE_LAST_WRITE = 0x00000010

	FILE_ACTION_ADDED            = 0x00000001
	FILE_ACTION_REMOVED          = 0x00000002
	FILE_ACTION_MODIFIED         = 0x00000003
	FILE_ACTION_RENAMED_OLD_NAME = 0x00000004
	FILE_ACTION_RENAMED_NEW_NAME = 0x00000005
)

type FILE_NOTIFY_INFORMATION struct {
	NextEntryOffset uint32
	Action          uint32
	FileNameLength  uint32
	FileName        [1]uint16
}

// windowsWatcher implements Watcher for Windows using ReadDirectoryChangesW
type windowsWatcher struct {
	*baseWatcher
	handle    windows.Handle
	stopChan  chan struct{}
	stoppedWg sync.WaitGroup
}

// newPlatformWatcher creates a Windows-specific watcher
func newPlatformWatcher(path string, config Config) Watcher {
	return &windowsWatcher{
		baseWatcher: newBaseWatcher(path, config),
		handle:      windows.InvalidHandle,
		stopChan:    make(chan struct{}),
	}
}

// Start begins monitoring the directory
func (w *windowsWatcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.mu.Unlock()

	// Open directory handle
	pathPtr, err := windows.UTF16PtrFromString(w.path)
	if err != nil {
		return err
	}

	handle, err := windows.CreateFile(
		pathPtr,
		windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		log.Printf("[FileWatcher] Failed to open directory %s: %v", w.path, err)
		return err
	}
	w.handle = handle

	// Start monitoring goroutine
	w.stoppedWg.Add(1)
	go w.watchLoop()

	// Start debounce processor
	w.stoppedWg.Add(1)
	go w.debounceLoop()

	log.Printf("[FileWatcher] Started monitoring: %s", w.path)
	return nil
}

// Stop stops monitoring and returns accumulated transfers
func (w *windowsWatcher) Stop() []FileTransfer {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return w.getTransfers()
	}
	w.running = false
	w.mu.Unlock()

	// Signal stop
	close(w.stopChan)
	if w.cancel != nil {
		w.cancel()
	}

	// Cancel pending I/O
	if w.handle != windows.InvalidHandle {
		windows.CancelIoEx(w.handle, nil)
	}

	// Wait for goroutines
	w.stoppedWg.Wait()

	// Close handle
	if w.handle != windows.InvalidHandle {
		windows.CloseHandle(w.handle)
		w.handle = windows.InvalidHandle
	}

	// Finalize any pending files
	w.finalizePending()

	log.Printf("[FileWatcher] Stopped monitoring: %s (captured %d files)", w.path, len(w.transfers))
	return w.getTransfers()
}

// watchLoop monitors directory changes
func (w *windowsWatcher) watchLoop() {
	defer w.stoppedWg.Done()

	buffer := make([]byte, 64*1024) // 64KB buffer
	var bytesReturned uint32

	for {
		select {
		case <-w.stopChan:
			return
		case <-w.ctx.Done():
			return
		default:
		}

		// Create overlapped structure for async I/O
		overlapped := &windows.Overlapped{}
		event, err := windows.CreateEvent(nil, 1, 0, nil)
		if err != nil {
			log.Printf("[FileWatcher] CreateEvent error: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		overlapped.HEvent = event

		err = windows.ReadDirectoryChanges(
			w.handle,
			&buffer[0],
			uint32(len(buffer)),
			true, // Watch subtree
			FILE_NOTIFY_CHANGE_FILE_NAME|FILE_NOTIFY_CHANGE_SIZE|FILE_NOTIFY_CHANGE_LAST_WRITE,
			&bytesReturned,
			overlapped,
			0,
		)

		if err != nil && err != windows.ERROR_IO_PENDING {
			windows.CloseHandle(event)
			// Check if we should stop
			select {
			case <-w.stopChan:
				return
			default:
				log.Printf("[FileWatcher] ReadDirectoryChanges error: %v", err)
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}

		// Wait for completion or stop signal
		waitResult, _ := windows.WaitForSingleObject(event, 1000) // 1 second timeout
		windows.CloseHandle(event)

		if waitResult == uint32(windows.WAIT_TIMEOUT) {
			continue
		}

		if waitResult != uint32(windows.WAIT_OBJECT_0) {
			continue
		}

		// Get overlapped result
		err = windows.GetOverlappedResult(w.handle, overlapped, &bytesReturned, false)
		if err != nil {
			continue
		}

		if bytesReturned == 0 {
			continue
		}

		// Process notifications
		w.processNotifications(buffer[:bytesReturned])
	}
}

// processNotifications parses FILE_NOTIFY_INFORMATION structures
func (w *windowsWatcher) processNotifications(buffer []byte) {
	offset := uint32(0)

	for {
		if offset >= uint32(len(buffer)) {
			break
		}

		info := (*FILE_NOTIFY_INFORMATION)(unsafe.Pointer(&buffer[offset]))

		// Extract filename
		nameLen := info.FileNameLength / 2 // UTF-16 characters
		if nameLen > 0 && offset+12+info.FileNameLength <= uint32(len(buffer)) {
			namePtr := (*[1 << 20]uint16)(unsafe.Pointer(&info.FileName[0]))[:nameLen:nameLen]
			filename := windows.UTF16ToString(namePtr)

			w.handleFileEvent(filename, info.Action)
		}

		if info.NextEntryOffset == 0 {
			break
		}
		offset += info.NextEntryOffset
	}
}

// handleFileEvent processes a single file event
func (w *windowsWatcher) handleFileEvent(relativePath string, action uint32) {
	// Get just the filename for exclusion check
	filename := filepath.Base(relativePath)

	// Skip excluded files
	if w.shouldExclude(filename) {
		return
	}

	// Skip directories (they don't have extensions typically, but check if it's a dir)
	fullPath := filepath.Join(w.path, relativePath)

	switch action {
	case FILE_ACTION_ADDED, FILE_ACTION_MODIFIED, FILE_ACTION_RENAMED_NEW_NAME:
		// Track this file as pending (debounce)
		w.mu.Lock()
		if len(w.transfers)+len(w.pending) < w.config.MaxFiles {
			w.pending[relativePath] = &pendingFile{
				path:      relativePath,
				lastWrite: time.Now(),
			}
		}
		w.mu.Unlock()

	case FILE_ACTION_REMOVED:
		// Remove from pending if it was there
		w.mu.Lock()
		delete(w.pending, relativePath)
		w.mu.Unlock()
	}

	_ = fullPath // Used for potential directory check in future
}

// debounceLoop finalizes pending files after debounce period
func (w *windowsWatcher) debounceLoop() {
	defer w.stoppedWg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	debounceTime := time.Duration(w.config.DebounceMs) * time.Millisecond

	for {
		select {
		case <-w.stopChan:
			return
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.processPending(debounceTime)
		}
	}
}

// processPending checks pending files and finalizes those past debounce time
func (w *windowsWatcher) processPending(debounceTime time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	toFinalize := make([]string, 0)

	for path, pending := range w.pending {
		if now.Sub(pending.lastWrite) >= debounceTime {
			toFinalize = append(toFinalize, path)
		}
	}

	for _, path := range toFinalize {
		w.finalizeFile(path)
		delete(w.pending, path)
	}
}

// finalizeFile records a completed file transfer
func (w *windowsWatcher) finalizeFile(relativePath string) {
	fullPath := filepath.Join(w.path, relativePath)

	// Get file info
	info, err := os.Stat(fullPath)
	if err != nil {
		// File may have been deleted
		return
	}

	// Skip directories
	if info.IsDir() {
		return
	}

	// Skip zero-byte files (likely temp files)
	if info.Size() == 0 {
		return
	}

	transfer := FileTransfer{
		FileName:     info.Name(),
		FilePath:     normalizeSlashes(relativePath),
		FileSize:     info.Size(),
		TransferTime: time.Now(),
		Operation:    "write",
	}

	w.transfers = append(w.transfers, transfer)
	log.Printf("[FileWatcher] File transferred: %s (%d bytes)", relativePath, info.Size())
}

// finalizePending finalizes all remaining pending files
func (w *windowsWatcher) finalizePending() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for path := range w.pending {
		w.finalizeFile(path)
	}
	w.pending = make(map[string]*pendingFile)
}

// normalizeSlashes converts backslashes to forward slashes
func normalizeSlashes(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}
