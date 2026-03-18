//go:build windows

package helper

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/sentinel/agent/internal/clipboard"
)

// ClipboardBridge wires the clipboard subsystem to a WebRTC data channel,
// enabling clipboard synchronization between the host and a remote viewer.
// It is disabled by default and must be explicitly enabled per session.
type ClipboardBridge struct {
	clip      clipboard.IClipboard
	dc        *webrtc.DataChannel
	enabled   bool
	direction clipboard.ClipboardDirection
	mu        sync.Mutex
	stopChan  chan struct{}
	stopped   atomic.Bool
	stats     clipboard.ClipboardStats
}

// dataChannelMessage is the envelope for all messages sent/received on the data channel.
type dataChannelMessage struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content,omitempty"`
}

// NewClipboardBridge creates a new bridge with the given clipboard implementation.
// The bridge starts disabled with direction "disabled" - it must be explicitly
// started and configured per session for security.
func NewClipboardBridge(clip clipboard.IClipboard) *ClipboardBridge {
	return &ClipboardBridge{
		clip:      clip,
		direction: clipboard.DirectionDisabled,
		enabled:   false,
		stopChan:  make(chan struct{}),
	}
}

// Start activates the clipboard bridge on the given data channel. It begins
// watching for local clipboard changes and forwarding them to the viewer
// according to the configured direction.
func (b *ClipboardBridge) Start(dc *webrtc.DataChannel) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.enabled {
		log.Printf("[ClipboardBridge] Already started, stopping previous session")
		b.stopLocked()
	}

	b.dc = dc
	b.stopChan = make(chan struct{})
	b.stopped.Store(false)
	b.enabled = true

	log.Printf("[ClipboardBridge] Started, direction=%s", b.direction)

	// Start watching for local clipboard changes.
	// The Watch callback fires on the clipboard subsystem's internal goroutine
	// whenever the host clipboard content changes.
	if err := b.clip.Watch(b.onLocalClipboardChange); err != nil {
		log.Printf("[ClipboardBridge] Failed to start clipboard watch: %v", err)
	}
}

// Stop tears down the bridge: stops the clipboard watcher, marks the bridge
// as disabled, and closes the stop channel.
func (b *ClipboardBridge) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopLocked()
}

// stopLocked performs the actual stop. Caller must hold b.mu.
func (b *ClipboardBridge) stopLocked() {
	if !b.enabled {
		return
	}

	b.clip.StopWatch()
	b.enabled = false

	if !b.stopped.Swap(true) {
		close(b.stopChan)
	}

	log.Printf("[ClipboardBridge] Stopped (sent=%d, received=%d, errors=%d)",
		b.stats.SentCount, b.stats.ReceivedCount, b.stats.ErrorCount)
}

// HandleMessage routes incoming data channel messages related to clipboard
// operations. It should be called from the data channel's OnMessage handler
// for any message whose type starts with "clipboard.".
func (b *ClipboardBridge) HandleMessage(msgType string, data json.RawMessage) {
	b.mu.Lock()
	if !b.enabled {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	switch msgType {
	case clipboard.ClipboardMsgUpdate:
		b.handleUpdate(data)
	case clipboard.ClipboardMsgConfig:
		b.handleConfig(data)
	case clipboard.ClipboardMsgRequest:
		b.handleRequest(data)
	default:
		log.Printf("[ClipboardBridge] Unknown message type: %s", msgType)
	}
}

// handleUpdate processes an incoming clipboard.update message from the viewer.
// It sets the host clipboard to the received content if the direction allows
// incoming data (bidirectional or viewer-to-host).
func (b *ClipboardBridge) handleUpdate(data json.RawMessage) {
	b.mu.Lock()
	direction := b.direction
	b.mu.Unlock()

	if !allowsIncoming(direction) {
		log.Printf("[ClipboardBridge] Ignoring incoming clipboard update: direction=%s does not allow incoming", direction)
		return
	}

	var msg clipboard.ClipboardUpdateMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("[ClipboardBridge] Failed to parse clipboard update: %v", err)
		b.sendError("parse_error", fmt.Sprintf("failed to parse update: %v", err))
		atomic.AddUint64(&b.stats.ErrorCount, 1)
		return
	}

	if msg.Content == nil || len(msg.Content.Formats) == 0 {
		log.Printf("[ClipboardBridge] Ignoring empty clipboard update")
		return
	}

	// Validate content size
	totalSize := msg.Content.TotalSize()
	if totalSize > clipboard.MaxTextSize {
		log.Printf("[ClipboardBridge] Rejecting clipboard update: size %d exceeds limit %d", totalSize, clipboard.MaxTextSize)
		b.sendError("size_exceeded", fmt.Sprintf("content size %d exceeds limit", totalSize))
		atomic.AddUint64(&b.stats.ErrorCount, 1)
		return
	}

	// Tag the source as viewer
	msg.Content.Source = "viewer"

	if err := b.clip.SetContent(msg.Content); err != nil {
		log.Printf("[ClipboardBridge] Failed to set clipboard content: %v", err)
		b.sendError("set_failed", fmt.Sprintf("failed to set content: %v", err))
		atomic.AddUint64(&b.stats.ErrorCount, 1)
		return
	}

	atomic.AddUint64(&b.stats.ReceivedCount, 1)
	atomic.AddUint64(&b.stats.BytesReceived, uint64(totalSize))
	b.stats.LastSyncTime = time.Now().UnixMilli()

	log.Printf("[ClipboardBridge] Applied viewer clipboard update: id=%s, formats=%d, size=%d",
		msg.Content.ID, len(msg.Content.Formats), totalSize)

	// Send acknowledgment
	b.sendAck(msg.Content.ID)
}

// handleConfig processes a clipboard.config message that updates the sync
// direction and enabled state. This is the mechanism for the viewer to
// opt-in to clipboard sharing.
func (b *ClipboardBridge) handleConfig(data json.RawMessage) {
	var msg clipboard.ClipboardConfigMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("[ClipboardBridge] Failed to parse clipboard config: %v", err)
		atomic.AddUint64(&b.stats.ErrorCount, 1)
		return
	}

	b.mu.Lock()
	oldDirection := b.direction
	b.direction = msg.Direction
	b.mu.Unlock()

	log.Printf("[ClipboardBridge] Direction changed: %s -> %s", oldDirection, msg.Direction)
}

// handleRequest processes a clipboard.request message from the viewer asking
// for the current host clipboard content.
func (b *ClipboardBridge) handleRequest(data json.RawMessage) {
	b.mu.Lock()
	direction := b.direction
	b.mu.Unlock()

	if !allowsOutgoing(direction) {
		log.Printf("[ClipboardBridge] Ignoring clipboard request: direction=%s does not allow outgoing", direction)
		return
	}

	var msg clipboard.ClipboardRequestMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("[ClipboardBridge] Failed to parse clipboard request: %v", err)
		atomic.AddUint64(&b.stats.ErrorCount, 1)
		return
	}

	content, err := b.clip.GetContent()
	if err != nil {
		log.Printf("[ClipboardBridge] Failed to get clipboard content for request: %v", err)
		b.sendError("get_failed", fmt.Sprintf("failed to get content: %v", err))
		atomic.AddUint64(&b.stats.ErrorCount, 1)
		return
	}

	// If specific formats were requested, filter to only those
	if len(msg.Formats) > 0 {
		content = filterFormats(content, msg.Formats)
	}

	if content == nil || len(content.Formats) == 0 {
		b.sendError("empty", "clipboard is empty or requested formats not available")
		return
	}

	content.Source = "host"

	b.sendClipboardUpdate(content)

	log.Printf("[ClipboardBridge] Responded to clipboard request: id=%s, formats=%d",
		content.ID, len(content.Formats))
}

// onLocalClipboardChange is called by the clipboard subsystem whenever the
// host's clipboard content changes. If the direction allows outgoing data
// and the data channel is open, the new content is sent to the viewer.
func (b *ClipboardBridge) onLocalClipboardChange(content *clipboard.ClipboardContent) {
	b.mu.Lock()
	enabled := b.enabled
	direction := b.direction
	dc := b.dc
	b.mu.Unlock()

	if !enabled || dc == nil {
		return
	}

	if !allowsOutgoing(direction) {
		return
	}

	if dc.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}

	// Tag source as host
	content.Source = "host"

	b.sendClipboardUpdate(content)

	totalSize := content.TotalSize()
	atomic.AddUint64(&b.stats.SentCount, 1)
	atomic.AddUint64(&b.stats.BytesSent, uint64(totalSize))
	b.stats.LastSyncTime = time.Now().UnixMilli()

	log.Printf("[ClipboardBridge] Sent local clipboard change to viewer: id=%s, formats=%d, size=%d",
		content.ID, len(content.Formats), totalSize)
}

// sendClipboardUpdate serializes and sends a clipboard update message over
// the data channel.
func (b *ClipboardBridge) sendClipboardUpdate(content *clipboard.ClipboardContent) {
	msg := clipboard.ClipboardUpdateMessage{
		Type:    clipboard.ClipboardMsgUpdate,
		Content: content,
	}
	b.sendMessage(msg)
}

// sendAck sends a clipboard acknowledgment for the given content ID.
func (b *ClipboardBridge) sendAck(contentID string) {
	msg := clipboard.ClipboardAckMessage{
		Type: clipboard.ClipboardMsgAck,
		ID:   contentID,
	}
	b.sendMessage(msg)
}

// sendError sends a clipboard error message to the viewer.
func (b *ClipboardBridge) sendError(errType string, details string) {
	msg := clipboard.ClipboardErrorMessage{
		Type:    clipboard.ClipboardMsgError,
		Error:   errType,
		Details: details,
	}
	b.sendMessage(msg)
}

// sendMessage marshals the message to JSON and sends it through the data channel.
// Errors are logged but not returned, as send failures are non-fatal.
func (b *ClipboardBridge) sendMessage(msg interface{}) {
	b.mu.Lock()
	dc := b.dc
	b.mu.Unlock()

	if dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[ClipboardBridge] Failed to marshal message: %v", err)
		atomic.AddUint64(&b.stats.ErrorCount, 1)
		return
	}

	if err := dc.SendText(string(data)); err != nil {
		log.Printf("[ClipboardBridge] Failed to send message: %v", err)
		atomic.AddUint64(&b.stats.ErrorCount, 1)
		return
	}
}

// GetStats returns a snapshot of the clipboard sync statistics.
func (b *ClipboardBridge) GetStats() clipboard.ClipboardStats {
	return clipboard.ClipboardStats{
		SentCount:     atomic.LoadUint64(&b.stats.SentCount),
		ReceivedCount: atomic.LoadUint64(&b.stats.ReceivedCount),
		BytesSent:     atomic.LoadUint64(&b.stats.BytesSent),
		BytesReceived: atomic.LoadUint64(&b.stats.BytesReceived),
		ErrorCount:    atomic.LoadUint64(&b.stats.ErrorCount),
		LastSyncTime:  b.stats.LastSyncTime,
	}
}

// GetDirection returns the current clipboard sync direction.
func (b *ClipboardBridge) GetDirection() clipboard.ClipboardDirection {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.direction
}

// SetDirection updates the clipboard sync direction.
func (b *ClipboardBridge) SetDirection(dir clipboard.ClipboardDirection) {
	b.mu.Lock()
	defer b.mu.Unlock()
	old := b.direction
	b.direction = dir
	log.Printf("[ClipboardBridge] Direction set: %s -> %s", old, dir)
}

// IsEnabled reports whether the clipboard bridge is currently active.
func (b *ClipboardBridge) IsEnabled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.enabled
}

// allowsIncoming returns true if the direction permits data flowing from
// the viewer to the host.
func allowsIncoming(dir clipboard.ClipboardDirection) bool {
	return dir == clipboard.DirectionBidirectional || dir == clipboard.DirectionViewerToHost
}

// allowsOutgoing returns true if the direction permits data flowing from
// the host to the viewer.
func allowsOutgoing(dir clipboard.ClipboardDirection) bool {
	return dir == clipboard.DirectionBidirectional || dir == clipboard.DirectionHostToViewer
}

// filterFormats returns a copy of content containing only the formats whose
// types match the requested list. Returns nil if no formats match.
func filterFormats(content *clipboard.ClipboardContent, requestedFormats []string) *clipboard.ClipboardContent {
	if content == nil {
		return nil
	}

	wanted := make(map[string]bool, len(requestedFormats))
	for _, f := range requestedFormats {
		wanted[f] = true
	}

	filtered := &clipboard.ClipboardContent{
		ID:        content.ID,
		Timestamp: content.Timestamp,
		Source:    content.Source,
		Formats:   make([]clipboard.ClipboardFormat, 0, len(content.Formats)),
	}

	for _, f := range content.Formats {
		if wanted[string(f.Type)] {
			filtered.Formats = append(filtered.Formats, f)
		}
	}

	if len(filtered.Formats) == 0 {
		return nil
	}

	return filtered
}
