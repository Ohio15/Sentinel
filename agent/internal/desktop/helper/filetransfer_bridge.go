//go:build windows

package helper

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/sentinel/agent/internal/filetransfer"
)

const (
	// dcHighWaterMark is the bufferedAmount threshold to pause sending (1MB)
	dcHighWaterMark = 1 * 1024 * 1024
	// dcLowWaterMark is the bufferedAmount threshold to resume sending (256KB)
	dcLowWaterMark = 256 * 1024
	// flowControlPollInterval is how often to check bufferedAmount when paused
	flowControlPollInterval = 5 * time.Millisecond
	// progressReportInterval is the minimum time between progress reports
	progressReportInterval = 250 * time.Millisecond
)

// FileTransferBridge bridges the filetransfer subsystem to a WebRTC data channel.
// It handles incoming file.* messages from the viewer, delegates to the IFileTransfer
// implementation, and sends responses/chunks back over the data channel.
type FileTransferBridge struct {
	ft              filetransfer.IFileTransfer
	dc              *webrtc.DataChannel
	mu              sync.Mutex
	activeTransfers map[string]context.CancelFunc
	stopped         bool
}

// NewFileTransferBridge creates a new bridge wrapping the given file transfer implementation.
func NewFileTransferBridge(ft filetransfer.IFileTransfer) *FileTransferBridge {
	return &FileTransferBridge{
		ft:              ft,
		activeTransfers: make(map[string]context.CancelFunc),
	}
}

// Start binds the bridge to a WebRTC data channel and registers progress/complete callbacks.
func (b *FileTransferBridge) Start(dc *webrtc.DataChannel) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.dc = dc
	b.stopped = false

	// Register progress callback: forward transfer progress to the viewer
	b.ft.OnProgress(func(transfer *filetransfer.Transfer) {
		b.sendMessage(&filetransfer.ProgressMessage{
			Type:     filetransfer.FileMsgProgress,
			Transfer: transfer,
		})
	})

	// Register completion callback: notify viewer when a transfer completes
	b.ft.OnComplete(func(transfer *filetransfer.Transfer) {
		b.sendMessage(&filetransfer.ProgressMessage{
			Type:     filetransfer.FileMsgComplete,
			Transfer: transfer,
		})

		// Clean up the cancel func for completed transfers
		b.mu.Lock()
		delete(b.activeTransfers, transfer.ID)
		b.mu.Unlock()
	})

	log.Printf("[FileTransferBridge] Started on data channel")
}

// Stop cancels all active transfers and clears state.
func (b *FileTransferBridge) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.stopped = true

	for id, cancel := range b.activeTransfers {
		log.Printf("[FileTransferBridge] Canceling transfer %s on stop", id)
		cancel()
		delete(b.activeTransfers, id)
	}

	b.dc = nil
	log.Printf("[FileTransferBridge] Stopped")
}

// envelope is the generic wrapper used to peek at message type before full deserialization
type envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"-"`
}

// HandleMessage routes an incoming file.* message from the viewer to the appropriate handler.
// msgType is the already-parsed "type" field; data is the full raw JSON message body.
func (b *FileTransferBridge) HandleMessage(msgType string, data json.RawMessage) {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	switch msgType {
	case filetransfer.FileMsgListDir:
		b.handleListDir(data)

	case filetransfer.FileMsgInfo:
		b.handleFileInfo(data)

	case filetransfer.FileMsgUploadStart:
		b.handleUploadStart(data)

	case filetransfer.FileMsgDownloadStart:
		b.handleDownloadStart(data)

	case filetransfer.FileMsgChunk:
		b.handleChunk(data)

	case filetransfer.FileMsgPause:
		b.handlePause(data)

	case filetransfer.FileMsgResume:
		b.handleResume(data)

	case filetransfer.FileMsgCancel:
		b.handleCancel(data)

	default:
		log.Printf("[FileTransferBridge] Unknown message type: %s", msgType)
	}
}

// handleListDir processes a file.listDir request
func (b *FileTransferBridge) handleListDir(data json.RawMessage) {
	var msg filetransfer.ListDirMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		b.sendError("", "Invalid listDir message: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listing, err := b.ft.ListDirectory(ctx, msg.Path)
	if err != nil {
		b.sendMessage(&filetransfer.ListDirRespMessage{
			Type:  filetransfer.FileMsgListDirResp,
			Error: err.Error(),
		})
		return
	}

	b.sendMessage(&filetransfer.ListDirRespMessage{
		Type:    filetransfer.FileMsgListDirResp,
		Listing: listing,
	})
}

// handleFileInfo processes a file.info request
func (b *FileTransferBridge) handleFileInfo(data json.RawMessage) {
	var msg filetransfer.FileInfoMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		b.sendError("", "Invalid fileInfo message: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	info, err := b.ft.GetFileInfo(ctx, msg.Path)
	if err != nil {
		b.sendMessage(&filetransfer.FileInfoRespMessage{
			Type:  filetransfer.FileMsgInfoResp,
			Error: err.Error(),
		})
		return
	}

	b.sendMessage(&filetransfer.FileInfoRespMessage{
		Type: filetransfer.FileMsgInfoResp,
		Info: info,
	})
}

// handleUploadStart processes a file.uploadStart request (viewer -> host)
func (b *FileTransferBridge) handleUploadStart(data json.RawMessage) {
	var msg filetransfer.TransferStartMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		b.sendError("", "Invalid uploadStart message: "+err.Error())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	transfer, err := b.ft.StartUpload(ctx, msg.Request)
	if err != nil {
		cancel()
		b.sendMessage(&filetransfer.TransferRespMessage{
			Type:  filetransfer.FileMsgTransferResp,
			Error: err.Error(),
		})
		return
	}

	b.mu.Lock()
	b.activeTransfers[transfer.ID] = cancel
	b.mu.Unlock()

	b.sendMessage(&filetransfer.TransferRespMessage{
		Type:     filetransfer.FileMsgTransferResp,
		Transfer: transfer,
	})
}

// handleDownloadStart processes a file.downloadStart request (host -> viewer)
// It starts the download, then launches a goroutine to send chunks.
func (b *FileTransferBridge) handleDownloadStart(data json.RawMessage) {
	var msg filetransfer.TransferStartMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		b.sendError("", "Invalid downloadStart message: "+err.Error())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	transfer, err := b.ft.StartDownload(ctx, msg.Request)
	if err != nil {
		cancel()
		b.sendMessage(&filetransfer.TransferRespMessage{
			Type:  filetransfer.FileMsgTransferResp,
			Error: err.Error(),
		})
		return
	}

	b.mu.Lock()
	b.activeTransfers[transfer.ID] = cancel
	b.mu.Unlock()

	// Send initial transfer response
	b.sendMessage(&filetransfer.TransferRespMessage{
		Type:     filetransfer.FileMsgTransferResp,
		Transfer: transfer,
	})

	// Start sending chunks in a goroutine
	go b.sendChunks(transfer.ID, ctx)
}

// handleChunk processes a file.chunk message (incoming chunk during upload)
func (b *FileTransferBridge) handleChunk(data json.RawMessage) {
	var msg filetransfer.ChunkMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		b.sendError("", "Invalid chunk message: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), filetransfer.ChunkTimeout)
	defer cancel()

	ack, err := b.ft.WriteChunk(ctx, msg.Chunk)
	if err != nil {
		log.Printf("[FileTransferBridge] WriteChunk error: %v", err)
	}

	if ack != nil {
		b.sendMessage(&filetransfer.ChunkAckMessage{
			Type: filetransfer.FileMsgChunkAck,
			Ack:  ack,
		})
	}
}

// handlePause processes a file.pause message
func (b *FileTransferBridge) handlePause(data json.RawMessage) {
	var msg filetransfer.TransferControlMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		b.sendError("", "Invalid pause message: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := b.ft.PauseTransfer(ctx, msg.TransferID); err != nil {
		b.sendError(msg.TransferID, "Failed to pause: "+err.Error())
		return
	}

	log.Printf("[FileTransferBridge] Paused transfer %s", msg.TransferID)
}

// handleResume processes a file.resume message
func (b *FileTransferBridge) handleResume(data json.RawMessage) {
	var msg filetransfer.TransferControlMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		b.sendError("", "Invalid resume message: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := b.ft.ResumeTransfer(ctx, msg.TransferID); err != nil {
		b.sendError(msg.TransferID, "Failed to resume: "+err.Error())
		return
	}

	log.Printf("[FileTransferBridge] Resumed transfer %s", msg.TransferID)
}

// handleCancel processes a file.cancel message
func (b *FileTransferBridge) handleCancel(data json.RawMessage) {
	var msg filetransfer.TransferControlMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		b.sendError("", "Invalid cancel message: "+err.Error())
		return
	}

	// Cancel the context for this transfer (stops sendChunks goroutine)
	b.mu.Lock()
	if cancelFn, ok := b.activeTransfers[msg.TransferID]; ok {
		cancelFn()
		delete(b.activeTransfers, msg.TransferID)
	}
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := b.ft.CancelTransfer(ctx, msg.TransferID); err != nil {
		b.sendError(msg.TransferID, "Failed to cancel: "+err.Error())
		return
	}

	log.Printf("[FileTransferBridge] Canceled transfer %s", msg.TransferID)
}

// sendChunks reads chunks from the file transfer manager and sends them over the data channel.
// It respects data channel backpressure by pausing when bufferedAmount exceeds the high water mark.
func (b *FileTransferBridge) sendChunks(transferID string, ctx context.Context) {
	index := 0

	for {
		select {
		case <-ctx.Done():
			log.Printf("[FileTransferBridge] sendChunks canceled for %s", transferID)
			return
		default:
		}

		// Check if the transfer is still in progress (may be paused)
		transfer, err := b.ft.GetTransfer(ctx, transferID)
		if err != nil {
			log.Printf("[FileTransferBridge] GetTransfer error for %s: %v", transferID, err)
			return
		}

		if transfer.State == filetransfer.StatePaused {
			// Wait and recheck
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
				continue
			}
		}

		if transfer.State != filetransfer.StateInProgress {
			// Transfer completed, failed, or canceled
			log.Printf("[FileTransferBridge] Transfer %s state is %s, stopping chunk sender", transferID, transfer.State)
			return
		}

		// Read the next chunk
		chunk, err := b.ft.ReadChunk(ctx, transferID, index)
		if err != nil {
			log.Printf("[FileTransferBridge] ReadChunk error for %s index %d: %v", transferID, index, err)
			b.sendError(transferID, "Failed to read chunk: "+err.Error())
			return
		}

		// Flow control: wait if the data channel buffer is too full
		b.waitForBufferDrain(ctx)

		// Send the chunk
		b.sendMessage(&filetransfer.ChunkMessage{
			Type:  filetransfer.FileMsgChunk,
			Chunk: chunk,
		})

		if chunk.IsLast {
			log.Printf("[FileTransferBridge] All chunks sent for transfer %s (%d chunks)", transferID, index+1)
			return
		}

		index++
	}
}

// waitForBufferDrain blocks until the data channel's bufferedAmount drops below the low water mark.
func (b *FileTransferBridge) waitForBufferDrain(ctx context.Context) {
	for {
		b.mu.Lock()
		dc := b.dc
		b.mu.Unlock()

		if dc == nil {
			return
		}

		if dc.BufferedAmount() <= dcHighWaterMark {
			return
		}

		// Buffer is above high water mark, wait for it to drain
		select {
		case <-ctx.Done():
			return
		case <-time.After(flowControlPollInterval):
			// Check again
		}

		// Once above high water mark, wait until we drop below low water mark
		// to avoid oscillation
		b.mu.Lock()
		dc = b.dc
		b.mu.Unlock()

		if dc == nil {
			return
		}

		if dc.BufferedAmount() <= dcLowWaterMark {
			return
		}
	}
}

// sendMessage serializes msg to JSON and sends it over the data channel.
func (b *FileTransferBridge) sendMessage(msg interface{}) {
	b.mu.Lock()
	dc := b.dc
	b.mu.Unlock()

	if dc == nil {
		return
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[FileTransferBridge] Failed to marshal message: %v", err)
		return
	}

	if err := dc.Send(payload); err != nil {
		log.Printf("[FileTransferBridge] Failed to send message: %v", err)
	}
}

// sendError sends an error message over the data channel.
func (b *FileTransferBridge) sendError(transferID string, errMsg string) {
	b.sendMessage(&filetransfer.ErrorMessage{
		Type:       filetransfer.FileMsgError,
		TransferID: transferID,
		Error:      errMsg,
	})
}
