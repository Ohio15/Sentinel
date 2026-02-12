//go:build darwin

package filewatcher

import (
	"context"
	"log"
)

// darwinWatcher is a stub implementation for macOS
// TODO: Implement using FSEvents for macOS support
type darwinWatcher struct {
	*baseWatcher
}

// newPlatformWatcher creates a macOS-specific watcher (stub)
func newPlatformWatcher(path string, config Config) Watcher {
	return &darwinWatcher{
		baseWatcher: newBaseWatcher(path, config),
	}
}

// Start begins monitoring the directory (stub - logs warning)
func (w *darwinWatcher) Start(ctx context.Context) error {
	log.Printf("[FileWatcher] macOS file watching not yet implemented for: %s", w.path)
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.running = true
	return nil
}

// Stop stops monitoring and returns accumulated transfers
func (w *darwinWatcher) Stop() []FileTransfer {
	w.mu.Lock()
	w.running = false
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()

	return w.getTransfers()
}
