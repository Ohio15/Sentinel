// Package clipboard provides clipboard synchronization for remote desktop sessions
package clipboard

import (
	"errors"
	"time"
)

// Common errors
var (
	ErrNotInitialized  = errors.New("clipboard: not initialized")
	ErrEmpty           = errors.New("clipboard: empty")
	ErrFormatNotFound  = errors.New("clipboard: format not found")
	ErrSizeLimitExceeded = errors.New("clipboard: size limit exceeded")
	ErrRateLimited     = errors.New("clipboard: rate limited")
)

// Size limits
const (
	MaxTextSize  = 10 * 1024 * 1024  // 10 MB
	MaxImageSize = 50 * 1024 * 1024  // 50 MB
	MaxFormats   = 10                 // Max formats per clipboard content
)

// FormatType defines clipboard data format types
type FormatType string

const (
	FormatText     FormatType = "text/plain"
	FormatHTML     FormatType = "text/html"
	FormatRTF      FormatType = "text/rtf"
	FormatPNG      FormatType = "image/png"
	FormatJPEG     FormatType = "image/jpeg"
	FormatBitmap   FormatType = "image/bmp"
	FormatFiles    FormatType = "files"
	FormatCustom   FormatType = "custom"
)

// ClipboardFormat represents a single clipboard format
type ClipboardFormat struct {
	Type      FormatType `json:"type"`
	Size      int        `json:"size"`
	Data      string     `json:"data,omitempty"`      // Base64 for binary, raw for text
	Files     []FileRef  `json:"files,omitempty"`     // For file format
	Truncated bool       `json:"truncated,omitempty"` // True if data was truncated
	MimeType  string     `json:"mimeType,omitempty"`  // Full MIME type
}

// FileRef represents a file reference (not the file content)
type FileRef struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Path string `json:"-"`         // Server-side path, not exposed
	Hash string `json:"hash"`      // SHA-256 for integrity
}

// ClipboardContent represents clipboard data with multiple formats
type ClipboardContent struct {
	ID        string            `json:"id"`        // Unique ID for deduplication
	Timestamp int64             `json:"timestamp"` // Unix milliseconds
	Formats   []ClipboardFormat `json:"formats"`   // Available formats, priority ordered
	Source    string            `json:"source"`    // "host" or "viewer"
}

// ClipboardDirection controls sync direction
type ClipboardDirection string

const (
	DirectionBidirectional ClipboardDirection = "bidirectional"
	DirectionHostToViewer  ClipboardDirection = "host-to-viewer"
	DirectionViewerToHost  ClipboardDirection = "viewer-to-host"
	DirectionDisabled      ClipboardDirection = "disabled"
)

// ClipboardConfig holds clipboard synchronization configuration
type ClipboardConfig struct {
	Direction     ClipboardDirection `json:"direction"`
	MaxTextSize   int                `json:"maxTextSize"`
	MaxImageSize  int                `json:"maxImageSize"`
	SyncInterval  time.Duration      `json:"syncInterval"`  // Min time between syncs
	EnableText    bool               `json:"enableText"`
	EnableImages  bool               `json:"enableImages"`
	EnableFiles   bool               `json:"enableFiles"`   // File references only
	SensitiveData bool               `json:"sensitiveData"` // Warn on sensitive patterns
}

// DefaultClipboardConfig returns default configuration
func DefaultClipboardConfig() ClipboardConfig {
	return ClipboardConfig{
		Direction:     DirectionBidirectional,
		MaxTextSize:   MaxTextSize,
		MaxImageSize:  MaxImageSize,
		SyncInterval:  200 * time.Millisecond, // Max 5 updates/sec
		EnableText:    true,
		EnableImages:  true,
		EnableFiles:   true,
		SensitiveData: false,
	}
}

// IClipboard defines the interface for clipboard operations
type IClipboard interface {
	// Initialize sets up clipboard monitoring
	Initialize() error

	// GetContent retrieves current clipboard content
	GetContent() (*ClipboardContent, error)

	// SetContent sets clipboard content
	SetContent(content *ClipboardContent) error

	// GetText retrieves text from clipboard
	GetText() (string, error)

	// SetText sets text to clipboard
	SetText(text string) error

	// GetImage retrieves image from clipboard as PNG bytes
	GetImage() ([]byte, error)

	// SetImage sets image to clipboard from PNG bytes
	SetImage(png []byte) error

	// GetFiles retrieves file references from clipboard
	GetFiles() ([]FileRef, error)

	// Watch starts monitoring for clipboard changes
	Watch(callback func(content *ClipboardContent)) error

	// StopWatch stops monitoring
	StopWatch()

	// Clear clears the clipboard
	Clear() error

	// Release frees resources
	Release()
}

// Message types for clipboard protocol
const (
	ClipboardMsgUpdate   = "clipboard.update"
	ClipboardMsgRequest  = "clipboard.request"
	ClipboardMsgConfig   = "clipboard.config"
	ClipboardMsgAck      = "clipboard.ack"
	ClipboardMsgError    = "clipboard.error"
)

// ClipboardUpdateMessage carries clipboard content update
type ClipboardUpdateMessage struct {
	Type    string            `json:"type"`
	Content *ClipboardContent `json:"content"`
}

// ClipboardRequestMessage requests specific formats
type ClipboardRequestMessage struct {
	Type    string   `json:"type"`
	Formats []string `json:"formats"` // Requested format types
}

// ClipboardConfigMessage configures clipboard sync
type ClipboardConfigMessage struct {
	Type      string             `json:"type"`
	Direction ClipboardDirection `json:"direction"`
}

// ClipboardAckMessage acknowledges receipt
type ClipboardAckMessage struct {
	Type string `json:"type"`
	ID   string `json:"id"` // Content ID being acknowledged
}

// ClipboardErrorMessage reports an error
type ClipboardErrorMessage struct {
	Type    string `json:"type"`
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// ClipboardStats holds clipboard sync statistics
type ClipboardStats struct {
	SentCount     uint64 `json:"sentCount"`
	ReceivedCount uint64 `json:"receivedCount"`
	BytesSent     uint64 `json:"bytesSent"`
	BytesReceived uint64 `json:"bytesReceived"`
	ErrorCount    uint64 `json:"errorCount"`
	LastSyncTime  int64  `json:"lastSyncTime"`
}

// generateContentID generates a unique ID for clipboard content
func GenerateContentID() string {
	return time.Now().Format("20060102150405.000000")
}

// HasFormat checks if content has a specific format
func (c *ClipboardContent) HasFormat(formatType FormatType) bool {
	for _, f := range c.Formats {
		if f.Type == formatType {
			return true
		}
	}
	return false
}

// GetFormat retrieves a specific format from content
func (c *ClipboardContent) GetFormat(formatType FormatType) *ClipboardFormat {
	for i := range c.Formats {
		if c.Formats[i].Type == formatType {
			return &c.Formats[i]
		}
	}
	return nil
}

// TotalSize calculates total size of all formats
func (c *ClipboardContent) TotalSize() int {
	total := 0
	for _, f := range c.Formats {
		total += f.Size
	}
	return total
}
