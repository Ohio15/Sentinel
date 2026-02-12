//go:build linux

package filewatcher

import (
	"context"
	"log"
)

// linuxWatcher is a stub implementation for Linux
// TODO: Implement using inotify for Linux support
type linuxWatcher struct {
	*baseWatcher
}

// newPlatformWatcher creates a Linux-specific watcher (stub)
func newPlatformWatcher(path string, config Config) Watcher {
	return &linuxWatcher{
		baseWatcher: newBaseWatcher(path, config),
	}
}

// Start begins monitoring the directory (stub - logs warning)
func (w *linuxWatcher) Start(ctx context.Context) error {
	log.Printf("[FileWatcher] Linux file watching not yet implemented for: %s", w.path)
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.running = true
	return nil
}

// Stop stops monitoring and returns accumulated transfers
func (w *linuxWatcher) Stop() []FileTransfer {
	w.mu.Lock()
	w.running = false
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()

	return w.getTransfers()
}
