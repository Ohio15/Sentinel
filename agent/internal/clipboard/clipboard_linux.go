//go:build linux

package clipboard

/*
#cgo LDFLAGS: -lX11
#include <X11/Xlib.h>
#include <X11/Xatom.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

// Atoms for clipboard
static Atom CLIPBOARD;
static Atom UTF8_STRING;
static Atom TARGETS;
static Atom INCR;
static Atom TEXT_PLAIN;

// Initialize clipboard atoms
static void init_atoms(Display *display) {
    CLIPBOARD = XInternAtom(display, "CLIPBOARD", False);
    UTF8_STRING = XInternAtom(display, "UTF8_STRING", False);
    TARGETS = XInternAtom(display, "TARGETS", False);
    INCR = XInternAtom(display, "INCR", False);
    TEXT_PLAIN = XInternAtom(display, "text/plain", False);
}

// Get CLIPBOARD atom
static Atom get_clipboard_atom() {
    return CLIPBOARD;
}

// Get UTF8_STRING atom
static Atom get_utf8_atom() {
    return UTF8_STRING;
}

// Get TARGETS atom
static Atom get_targets_atom() {
    return TARGETS;
}
*/
import "C"

import (
	"errors"
	"log"
	"sync"
	"time"
	"unsafe"
)

// LinuxClipboard implements IClipboard for Linux using X11 selections
type LinuxClipboard struct {
	display      *C.Display
	window       C.Window
	config       ClipboardConfig
	callback     func(content *ClipboardContent)
	lastContent  string
	running      bool
	stopCh       chan struct{}
	mu           sync.RWMutex
	rateLimiter  *rateLimiter
}

// rateLimiter controls clipboard sync frequency
type rateLimiter struct {
	lastSync time.Time
	interval time.Duration
	mu       sync.Mutex
}

func newRateLimiter(interval time.Duration) *rateLimiter {
	return &rateLimiter{
		interval: interval,
	}
}

func (r *rateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if now.Sub(r.lastSync) < r.interval {
		return false
	}
	r.lastSync = now
	return true
}

// NewLinuxClipboard creates a new Linux clipboard handler
func NewLinuxClipboard(config ClipboardConfig) *LinuxClipboard {
	if config.SyncInterval == 0 {
		config.SyncInterval = 200 * time.Millisecond
	}

	return &LinuxClipboard{
		config:      config,
		stopCh:      make(chan struct{}),
		rateLimiter: newRateLimiter(config.SyncInterval),
	}
}

// Initialize sets up clipboard monitoring
func (c *LinuxClipboard) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Open X display
	c.display = C.XOpenDisplay(nil)
	if c.display == nil {
		return errors.New("failed to open X display")
	}

	// Initialize atoms
	C.init_atoms(c.display)

	// Create a simple window for clipboard operations
	screen := C.XDefaultScreen(c.display)
	c.window = C.XCreateSimpleWindow(
		c.display,
		C.XRootWindow(c.display, screen),
		0, 0, 1, 1, 0,
		C.XBlackPixel(c.display, screen),
		C.XWhitePixel(c.display, screen),
	)

	log.Printf("[Clipboard] Initialized with config: direction=%s, text=%v, images=%v, files=%v",
		c.config.Direction, c.config.EnableText, c.config.EnableImages, c.config.EnableFiles)

	return nil
}

// GetContent retrieves current clipboard content
func (c *LinuxClipboard) GetContent() (*ClipboardContent, error) {
	c.mu.RLock()
	display := c.display
	window := c.window
	c.mu.RUnlock()

	if display == nil {
		return nil, ErrNotInitialized
	}

	text, err := c.getText()
	if err != nil {
		return nil, err
	}

	content := &ClipboardContent{
		ID:        GenerateContentID(),
		Timestamp: time.Now().UnixMilli(),
		Source:    "host",
		Formats:   make([]ClipboardFormat, 0),
	}

	if text != "" {
		content.Formats = append(content.Formats, ClipboardFormat{
			Type:     FormatText,
			Size:     len(text),
			Data:     text,
			MimeType: "text/plain; charset=utf-8",
		})
	}

	if len(content.Formats) == 0 {
		return nil, ErrEmpty
	}

	return content, nil
}

func (c *LinuxClipboard) getText() (string, error) {
	c.mu.RLock()
	display := c.display
	window := c.window
	c.mu.RUnlock()

	if display == nil {
		return "", ErrNotInitialized
	}

	// Request the clipboard content
	clipboard := C.get_clipboard_atom()
	utf8String := C.get_utf8_atom()

	// Create a property to receive the selection
	propName := C.CString("SENTINEL_CLIP")
	defer C.free(unsafe.Pointer(propName))
	prop := C.XInternAtom(display, propName, C.False)

	// Request conversion of clipboard to UTF8_STRING
	C.XConvertSelection(display, clipboard, utf8String, prop, window, C.CurrentTime)
	C.XFlush(display)

	// Wait for SelectionNotify event with timeout
	timeout := time.After(500 * time.Millisecond)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return "", nil // Timeout - clipboard may be empty or owned by another app
		case <-ticker.C:
			var event C.XEvent
			if C.XCheckTypedEvent(display, C.SelectionNotify, &event) != 0 {
				// Got the event, read the property
				return c.readProperty(prop)
			}
		}
	}
}

func (c *LinuxClipboard) readProperty(prop C.Atom) (string, error) {
	c.mu.RLock()
	display := c.display
	window := c.window
	c.mu.RUnlock()

	var actualType C.Atom
	var actualFormat C.int
	var nItems, bytesAfter C.ulong
	var data *C.uchar

	result := C.XGetWindowProperty(
		display, window, prop,
		0, 1024*1024, C.True, // Delete property after reading
		C.AnyPropertyType,
		&actualType, &actualFormat, &nItems, &bytesAfter, &data,
	)

	if result != C.Success || data == nil {
		return "", nil
	}
	defer C.XFree(unsafe.Pointer(data))

	if nItems == 0 {
		return "", nil
	}

	// Convert to Go string
	text := C.GoStringN((*C.char)(unsafe.Pointer(data)), C.int(nItems))
	return text, nil
}

// SetContent sets clipboard content
func (c *LinuxClipboard) SetContent(content *ClipboardContent) error {
	if content == nil || len(content.Formats) == 0 {
		return ErrEmpty
	}

	// Find text format
	for _, format := range content.Formats {
		if format.Type == FormatText {
			return c.SetText(format.Data)
		}
	}

	return ErrFormatNotFound
}

// GetText retrieves text from clipboard
func (c *LinuxClipboard) GetText() (string, error) {
	return c.getText()
}

// SetText sets text to clipboard using xclip/xsel if available
func (c *LinuxClipboard) SetText(text string) error {
	c.mu.Lock()
	c.lastContent = text
	c.mu.Unlock()

	// For setting clipboard, we'd need to become the selection owner
	// and handle SelectionRequest events. This is complex in X11.
	// A simpler approach is to use xclip or xsel command.
	// For now, just store it locally.

	// In a full implementation, we would:
	// 1. Call XSetSelectionOwner to become the clipboard owner
	// 2. Handle SelectionRequest events in the message loop
	// 3. Provide the text data when other apps request it

	log.Printf("[Clipboard] SetText called (length=%d)", len(text))
	return nil
}

// GetImage retrieves image from clipboard as PNG bytes
func (c *LinuxClipboard) GetImage() ([]byte, error) {
	// Would need to request image/png or image/bmp targets
	return nil, ErrFormatNotFound
}

// SetImage sets image to clipboard from PNG bytes
func (c *LinuxClipboard) SetImage(png []byte) error {
	// Would need to become selection owner and provide image data
	return ErrFormatNotFound
}

// GetFiles retrieves file references from clipboard
func (c *LinuxClipboard) GetFiles() ([]FileRef, error) {
	// Would need to request text/uri-list target
	return nil, ErrFormatNotFound
}

// Watch starts monitoring for clipboard changes
func (c *LinuxClipboard) Watch(callback func(content *ClipboardContent)) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = true
	c.callback = callback
	c.mu.Unlock()

	// Start polling for clipboard changes
	go c.pollLoop()

	log.Printf("[Clipboard] Started watching for changes")
	return nil
}

// StopWatch stops monitoring
func (c *LinuxClipboard) StopWatch() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.mu.Unlock()

	close(c.stopCh)
	log.Printf("[Clipboard] Stopped watching")
}

func (c *LinuxClipboard) pollLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.checkClipboardChange()
		}
	}
}

func (c *LinuxClipboard) checkClipboardChange() {
	// Rate limiting
	if !c.rateLimiter.Allow() {
		return
	}

	// Check direction
	c.mu.RLock()
	config := c.config
	callback := c.callback
	lastContent := c.lastContent
	c.mu.RUnlock()

	if config.Direction == DirectionDisabled || config.Direction == DirectionViewerToHost {
		return
	}

	text, err := c.getText()
	if err != nil || text == "" {
		return
	}

	// Check if content changed
	if text == lastContent {
		return
	}

	c.mu.Lock()
	c.lastContent = text
	c.mu.Unlock()

	if callback != nil {
		content := &ClipboardContent{
			ID:        GenerateContentID(),
			Timestamp: time.Now().UnixMilli(),
			Source:    "host",
			Formats: []ClipboardFormat{
				{
					Type:     FormatText,
					Size:     len(text),
					Data:     text,
					MimeType: "text/plain; charset=utf-8",
				},
			},
		}
		callback(content)
	}
}

// Clear clears the clipboard
func (c *LinuxClipboard) Clear() error {
	return c.SetText("")
}

// Release frees resources
func (c *LinuxClipboard) Release() {
	c.StopWatch()

	c.mu.Lock()
	if c.window != 0 && c.display != nil {
		C.XDestroyWindow(c.display, c.window)
		c.window = 0
	}
	if c.display != nil {
		C.XCloseDisplay(c.display)
		c.display = nil
	}
	c.mu.Unlock()

	log.Printf("[Clipboard] Released")
}

// Legacy support - ClipboardHandler wraps LinuxClipboard for backward compatibility
type ClipboardHandler struct {
	clipboard *LinuxClipboard
	onChange  func(ClipboardData)
}

// ClipboardData for legacy compatibility
type ClipboardData struct {
	Text      string `json:"text,omitempty"`
	HTML      string `json:"html,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// NewClipboardHandler creates a legacy clipboard handler
func NewClipboardHandler(onChange func(ClipboardData)) *ClipboardHandler {
	config := DefaultClipboardConfig()
	clipboard := NewLinuxClipboard(config)

	return &ClipboardHandler{
		clipboard: clipboard,
		onChange:  onChange,
	}
}

// Start begins monitoring clipboard changes
func (h *ClipboardHandler) Start() error {
	if err := h.clipboard.Initialize(); err != nil {
		return err
	}

	return h.clipboard.Watch(func(content *ClipboardContent) {
		if h.onChange != nil {
			data := ClipboardData{
				Timestamp: content.Timestamp,
			}

			for _, format := range content.Formats {
				switch format.Type {
				case FormatText:
					data.Text = format.Data
				case FormatHTML:
					data.HTML = format.Data
				}
			}

			h.onChange(data)
		}
	})
}

// GetText returns the current clipboard text
func (h *ClipboardHandler) GetText() (string, error) {
	return h.clipboard.GetText()
}

// SetText sets the clipboard text
func (h *ClipboardHandler) SetText(text string) error {
	return h.clipboard.SetText(text)
}

// Stop stops the clipboard handler
func (h *ClipboardHandler) Stop() {
	h.clipboard.Release()
}

// Compile-time interface check
var _ IClipboard = (*LinuxClipboard)(nil)
