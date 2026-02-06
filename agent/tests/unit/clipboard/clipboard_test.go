package clipboard_test

import (
	"testing"
	"time"

	"github.com/sentinel/agent/internal/clipboard"
)

func TestClipboardConfig(t *testing.T) {
	t.Run("DefaultConfig", func(t *testing.T) {
		config := clipboard.DefaultClipboardConfig()

		if config.Direction != clipboard.DirectionBidirectional {
			t.Errorf("Expected direction %s, got %s", clipboard.DirectionBidirectional, config.Direction)
		}

		if config.MaxTextSize != clipboard.MaxTextSize {
			t.Errorf("Expected MaxTextSize %d, got %d", clipboard.MaxTextSize, config.MaxTextSize)
		}

		if config.MaxImageSize != clipboard.MaxImageSize {
			t.Errorf("Expected MaxImageSize %d, got %d", clipboard.MaxImageSize, config.MaxImageSize)
		}

		if !config.EnableText {
			t.Error("Expected EnableText to be true")
		}

		if !config.EnableImages {
			t.Error("Expected EnableImages to be true")
		}

		if !config.EnableFiles {
			t.Error("Expected EnableFiles to be true")
		}

		if config.SyncInterval != 200*time.Millisecond {
			t.Errorf("Expected SyncInterval 200ms, got %v", config.SyncInterval)
		}
	})
}

func TestClipboardContent(t *testing.T) {
	t.Run("HasFormat", func(t *testing.T) {
		content := &clipboard.ClipboardContent{
			ID:        "test-id",
			Timestamp: time.Now().UnixMilli(),
			Source:    "host",
			Formats: []clipboard.ClipboardFormat{
				{Type: clipboard.FormatText, Data: "test", Size: 4},
				{Type: clipboard.FormatHTML, Data: "<p>test</p>", Size: 11},
			},
		}

		if !content.HasFormat(clipboard.FormatText) {
			t.Error("Expected content to have FormatText")
		}

		if !content.HasFormat(clipboard.FormatHTML) {
			t.Error("Expected content to have FormatHTML")
		}

		if content.HasFormat(clipboard.FormatPNG) {
			t.Error("Expected content NOT to have FormatPNG")
		}
	})

	t.Run("GetFormat", func(t *testing.T) {
		content := &clipboard.ClipboardContent{
			Formats: []clipboard.ClipboardFormat{
				{Type: clipboard.FormatText, Data: "test", Size: 4},
				{Type: clipboard.FormatHTML, Data: "<p>test</p>", Size: 11},
			},
		}

		format := content.GetFormat(clipboard.FormatText)
		if format == nil {
			t.Fatal("Expected to get FormatText")
		}
		if format.Data != "test" {
			t.Errorf("Expected data 'test', got '%s'", format.Data)
		}

		format = content.GetFormat(clipboard.FormatPNG)
		if format != nil {
			t.Error("Expected nil for FormatPNG")
		}
	})

	t.Run("TotalSize", func(t *testing.T) {
		content := &clipboard.ClipboardContent{
			Formats: []clipboard.ClipboardFormat{
				{Type: clipboard.FormatText, Size: 100},
				{Type: clipboard.FormatHTML, Size: 200},
				{Type: clipboard.FormatRTF, Size: 150},
			},
		}

		total := content.TotalSize()
		if total != 450 {
			t.Errorf("Expected total size 450, got %d", total)
		}
	})
}

func TestGenerateContentID(t *testing.T) {
	id1 := clipboard.GenerateContentID()
	time.Sleep(1 * time.Microsecond)
	id2 := clipboard.GenerateContentID()

	if id1 == "" {
		t.Error("Expected non-empty ID")
	}

	if id1 == id2 {
		t.Error("Expected different IDs for sequential calls")
	}

	// Check format (should be timestamp-based)
	if len(id1) < 10 {
		t.Errorf("Expected ID with at least 10 characters, got %d", len(id1))
	}
}

func TestClipboardFormatTypes(t *testing.T) {
	formats := []clipboard.FormatType{
		clipboard.FormatText,
		clipboard.FormatHTML,
		clipboard.FormatRTF,
		clipboard.FormatPNG,
		clipboard.FormatJPEG,
		clipboard.FormatBitmap,
		clipboard.FormatFiles,
		clipboard.FormatCustom,
	}

	// Verify all format types are unique
	seen := make(map[clipboard.FormatType]bool)
	for _, f := range formats {
		if seen[f] {
			t.Errorf("Duplicate format type: %s", f)
		}
		seen[f] = true
	}
}

func TestClipboardDirection(t *testing.T) {
	directions := []clipboard.ClipboardDirection{
		clipboard.DirectionBidirectional,
		clipboard.DirectionHostToViewer,
		clipboard.DirectionViewerToHost,
		clipboard.DirectionDisabled,
	}

	// Verify all directions are unique
	seen := make(map[clipboard.ClipboardDirection]bool)
	for _, d := range directions {
		if seen[d] {
			t.Errorf("Duplicate direction: %s", d)
		}
		seen[d] = true
	}
}

func TestClipboardErrors(t *testing.T) {
	errors := []error{
		clipboard.ErrNotInitialized,
		clipboard.ErrEmpty,
		clipboard.ErrFormatNotFound,
		clipboard.ErrSizeLimitExceeded,
		clipboard.ErrRateLimited,
	}

	// Verify error messages
	for _, err := range errors {
		if err.Error() == "" {
			t.Error("Expected non-empty error message")
		}
	}
}

func TestFileRef(t *testing.T) {
	ref := clipboard.FileRef{
		Name: "test.txt",
		Size: 1024,
		Path: "/tmp/test.txt",
		Hash: "abc123",
	}

	if ref.Name != "test.txt" {
		t.Errorf("Expected name 'test.txt', got '%s'", ref.Name)
	}

	if ref.Size != 1024 {
		t.Errorf("Expected size 1024, got %d", ref.Size)
	}
}

func TestClipboardMessageTypes(t *testing.T) {
	// Verify message types are defined and unique
	types := []string{
		clipboard.ClipboardMsgUpdate,
		clipboard.ClipboardMsgRequest,
		clipboard.ClipboardMsgConfig,
		clipboard.ClipboardMsgAck,
		clipboard.ClipboardMsgError,
	}

	seen := make(map[string]bool)
	for _, typ := range types {
		if typ == "" {
			t.Error("Expected non-empty message type")
		}
		if seen[typ] {
			t.Errorf("Duplicate message type: %s", typ)
		}
		seen[typ] = true
	}
}

func TestClipboardStats(t *testing.T) {
	stats := clipboard.ClipboardStats{
		SentCount:     10,
		ReceivedCount: 5,
		BytesSent:     1024,
		BytesReceived: 512,
		ErrorCount:    1,
		LastSyncTime:  time.Now().UnixMilli(),
	}

	if stats.SentCount != 10 {
		t.Errorf("Expected SentCount 10, got %d", stats.SentCount)
	}

	if stats.BytesSent != 1024 {
		t.Errorf("Expected BytesSent 1024, got %d", stats.BytesSent)
	}
}
