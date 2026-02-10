//go:build (linux && arm64) || (linux && arm)

package clipboard

import "errors"

// Headless stub for ARM Linux (Synology NAS, etc.)
// Clipboard not available without X11 display

var ErrHeadlessMode = errors.New("clipboard not available in headless mode")

// ClipboardManager stub for headless systems
type ClipboardManager struct{}

// NewClipboardManager returns a stub manager for headless systems
func NewClipboardManager() (*ClipboardManager, error) {
	return &ClipboardManager{}, nil
}

// GetText returns empty string on headless systems
func (c *ClipboardManager) GetText() (string, error) {
	return "", ErrHeadlessMode
}

// SetText is a no-op on headless systems
func (c *ClipboardManager) SetText(text string) error {
	return ErrHeadlessMode
}

// Close is a no-op on headless systems
func (c *ClipboardManager) Close() error {
	return nil
}

// Watch is a no-op on headless systems
func (c *ClipboardManager) Watch(callback func(text string)) {
	// No-op for headless
}

// StopWatch is a no-op on headless systems
func (c *ClipboardManager) StopWatch() {
	// No-op for headless
}
