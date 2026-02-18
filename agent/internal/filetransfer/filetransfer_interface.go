// Package filetransfer provides file transfer capabilities for remote desktop sessions
package filetransfer

import (
	"context"
	"errors"
	"time"
)

// Common errors
var (
	ErrNotInitialized    = errors.New("filetransfer: not initialized")
	ErrTransferNotFound  = errors.New("filetransfer: transfer not found")
	ErrTransferCanceled  = errors.New("filetransfer: transfer canceled")
	ErrFileTooLarge      = errors.New("filetransfer: file too large")
	ErrAccessDenied      = errors.New("filetransfer: access denied")
	ErrIntegrityFailed   = errors.New("filetransfer: integrity check failed")
	ErrInvalidPath       = errors.New("filetransfer: invalid path")
	ErrAlreadyExists     = errors.New("filetransfer: file already exists")
	ErrTransferInProgress = errors.New("filetransfer: transfer already in progress")
)

// Size limits and defaults
const (
	DefaultChunkSize     = 64 * 1024         // 64 KB chunks
	MaxChunkSize         = 1024 * 1024       // 1 MB max chunk
	MaxFileSize          = 10 * 1024 * 1024 * 1024 // 10 GB max file
	MaxConcurrentTransfers = 5
	ChunkTimeout         = 30 * time.Second
	TransferTimeout      = 24 * time.Hour
)

// TransferDirection indicates upload or download
type TransferDirection string

const (
	DirectionUpload   TransferDirection = "upload"   // Viewer -> Host
	DirectionDownload TransferDirection = "download" // Host -> Viewer
)

// TransferState represents the current state of a transfer
type TransferState string

const (
	StateQueued     TransferState = "queued"
	StateInProgress TransferState = "in_progress"
	StatePaused     TransferState = "paused"
	StateCompleted  TransferState = "completed"
	StateFailed     TransferState = "failed"
	StateCanceled   TransferState = "canceled"
)

// TransferConfig holds file transfer configuration
type TransferConfig struct {
	ChunkSize          int           `json:"chunkSize"`
	MaxFileSize        int64         `json:"maxFileSize"`
	MaxConcurrent      int           `json:"maxConcurrent"`
	AllowUpload        bool          `json:"allowUpload"`
	AllowDownload      bool          `json:"allowDownload"`
	AllowedPaths       []string      `json:"allowedPaths,omitempty"`   // Restricted paths (empty = all)
	BlockedExtensions  []string      `json:"blockedExtensions,omitempty"` // Blocked file extensions
	ResumeEnabled      bool          `json:"resumeEnabled"`
	IntegrityCheck     bool          `json:"integrityCheck"`           // SHA-256 verification
	TransferTimeout    time.Duration `json:"transferTimeout"`
}

// DefaultTransferConfig returns default configuration
func DefaultTransferConfig() TransferConfig {
	return TransferConfig{
		ChunkSize:       DefaultChunkSize,
		MaxFileSize:     MaxFileSize,
		MaxConcurrent:   MaxConcurrentTransfers,
		AllowUpload:     true,
		AllowDownload:   true,
		ResumeEnabled:   true,
		IntegrityCheck:  true,
		TransferTimeout: TransferTimeout,
		BlockedExtensions: []string{
			".exe", ".bat", ".cmd", ".com", ".scr", ".pif",
			".msi", ".msp", ".dll", ".sys", ".vbs", ".js",
			".ps1", ".psm1", ".psd1",
		},
	}
}

// FileInfo contains information about a file
type FileInfo struct {
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	IsDir        bool      `json:"is_dir"`
	ModTime      time.Time `json:"modified_time"`
	Permissions  string    `json:"mode,omitempty"`
	Hash         string    `json:"hash,omitempty"` // SHA-256, computed on demand
}

// DirectoryListing contains a directory listing
type DirectoryListing struct {
	Path    string     `json:"path"`
	Files   []FileInfo `json:"files"`
	HasMore bool       `json:"hasMore,omitempty"` // For pagination
}

// TransferRequest initiates a file transfer
type TransferRequest struct {
	TransferID  string            `json:"transferId"`
	Direction   TransferDirection `json:"direction"`
	SourcePath  string            `json:"sourcePath"`  // Local path for upload, remote for download
	DestPath    string            `json:"destPath"`    // Remote path for upload, local for download
	FileSize    int64             `json:"fileSize"`
	FileHash    string            `json:"fileHash,omitempty"` // Expected SHA-256
	Overwrite   bool              `json:"overwrite"`
	ResumeFrom  int64             `json:"resumeFrom,omitempty"` // Byte offset to resume from
}

// Transfer represents an active file transfer
type Transfer struct {
	ID           string            `json:"id"`
	Direction    TransferDirection `json:"direction"`
	SourcePath   string            `json:"sourcePath"`
	DestPath     string            `json:"destPath"`
	FileSize     int64             `json:"fileSize"`
	Transferred  int64             `json:"transferred"`
	State        TransferState     `json:"state"`
	Error        string            `json:"error,omitempty"`
	StartTime    time.Time         `json:"startTime"`
	EndTime      time.Time         `json:"endTime,omitempty"`
	Speed        float64           `json:"speed"`         // Bytes per second
	Progress     float64           `json:"progress"`      // 0-100
	Hash         string            `json:"hash,omitempty"` // Final SHA-256
	ChunksTotal  int               `json:"chunksTotal"`
	ChunksSent   int               `json:"chunksSent"`
}

// Chunk represents a file chunk for transfer
type Chunk struct {
	TransferID string `json:"transferId"`
	Index      int    `json:"index"`
	Offset     int64  `json:"offset"`
	Size       int    `json:"size"`
	Data       []byte `json:"data,omitempty"` // Base64 encoded in JSON
	Hash       string `json:"hash,omitempty"` // SHA-256 of chunk data
	IsLast     bool   `json:"isLast"`
}

// ChunkAck acknowledges receipt of a chunk
type ChunkAck struct {
	TransferID string `json:"transferId"`
	Index      int    `json:"index"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

// TransferStats holds transfer statistics
type TransferStats struct {
	TotalTransfers    int64   `json:"totalTransfers"`
	CompletedCount    int64   `json:"completedCount"`
	FailedCount       int64   `json:"failedCount"`
	TotalBytesUp      int64   `json:"totalBytesUp"`
	TotalBytesDown    int64   `json:"totalBytesDown"`
	ActiveTransfers   int     `json:"activeTransfers"`
	AverageSpeed      float64 `json:"averageSpeed"`
}

// IFileTransfer defines the interface for file transfer operations
type IFileTransfer interface {
	// Initialize sets up the file transfer manager
	Initialize(config TransferConfig) error

	// ListDirectory lists files in a directory
	ListDirectory(ctx context.Context, path string) (*DirectoryListing, error)

	// GetFileInfo returns information about a file
	GetFileInfo(ctx context.Context, path string) (*FileInfo, error)

	// StartUpload initiates a file upload (viewer -> host)
	StartUpload(ctx context.Context, req *TransferRequest) (*Transfer, error)

	// StartDownload initiates a file download (host -> viewer)
	StartDownload(ctx context.Context, req *TransferRequest) (*Transfer, error)

	// WriteChunk writes a chunk of data during upload
	WriteChunk(ctx context.Context, chunk *Chunk) (*ChunkAck, error)

	// ReadChunk reads a chunk of data during download
	ReadChunk(ctx context.Context, transferID string, index int) (*Chunk, error)

	// PauseTransfer pauses an active transfer
	PauseTransfer(ctx context.Context, transferID string) error

	// ResumeTransfer resumes a paused transfer
	ResumeTransfer(ctx context.Context, transferID string) error

	// CancelTransfer cancels an active transfer
	CancelTransfer(ctx context.Context, transferID string) error

	// GetTransfer returns the current state of a transfer
	GetTransfer(ctx context.Context, transferID string) (*Transfer, error)

	// GetActiveTransfers returns all active transfers
	GetActiveTransfers(ctx context.Context) ([]*Transfer, error)

	// GetStats returns transfer statistics
	GetStats() *TransferStats

	// OnProgress sets a callback for transfer progress updates
	OnProgress(callback func(transfer *Transfer))

	// OnComplete sets a callback for transfer completion
	OnComplete(callback func(transfer *Transfer))

	// Release frees resources
	Release()
}

// Message types for file transfer protocol
const (
	FileMsgListDir      = "file.listDir"
	FileMsgListDirResp  = "file.listDirResp"
	FileMsgInfo         = "file.info"
	FileMsgInfoResp     = "file.infoResp"
	FileMsgUploadStart  = "file.uploadStart"
	FileMsgDownloadStart = "file.downloadStart"
	FileMsgTransferResp = "file.transferResp"
	FileMsgChunk        = "file.chunk"
	FileMsgChunkAck     = "file.chunkAck"
	FileMsgPause        = "file.pause"
	FileMsgResume       = "file.resume"
	FileMsgCancel       = "file.cancel"
	FileMsgProgress     = "file.progress"
	FileMsgComplete     = "file.complete"
	FileMsgError        = "file.error"
)

// ListDirMessage requests a directory listing
type ListDirMessage struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// ListDirRespMessage contains directory listing response
type ListDirRespMessage struct {
	Type    string            `json:"type"`
	Listing *DirectoryListing `json:"listing,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// FileInfoMessage requests file information
type FileInfoMessage struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// FileInfoRespMessage contains file info response
type FileInfoRespMessage struct {
	Type  string    `json:"type"`
	Info  *FileInfo `json:"info,omitempty"`
	Error string    `json:"error,omitempty"`
}

// TransferStartMessage initiates a transfer
type TransferStartMessage struct {
	Type    string           `json:"type"`
	Request *TransferRequest `json:"request"`
}

// TransferRespMessage responds to transfer start
type TransferRespMessage struct {
	Type     string    `json:"type"`
	Transfer *Transfer `json:"transfer,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// ChunkMessage carries chunk data
type ChunkMessage struct {
	Type  string `json:"type"`
	Chunk *Chunk `json:"chunk"`
}

// ChunkAckMessage acknowledges chunk receipt
type ChunkAckMessage struct {
	Type string    `json:"type"`
	Ack  *ChunkAck `json:"ack"`
}

// TransferControlMessage controls transfer state
type TransferControlMessage struct {
	Type       string `json:"type"`
	TransferID string `json:"transferId"`
}

// ProgressMessage reports transfer progress
type ProgressMessage struct {
	Type     string    `json:"type"`
	Transfer *Transfer `json:"transfer"`
}

// ErrorMessage reports an error
type ErrorMessage struct {
	Type       string `json:"type"`
	TransferID string `json:"transferId,omitempty"`
	Error      string `json:"error"`
	Details    string `json:"details,omitempty"`
}

// GenerateTransferID generates a unique transfer ID
func GenerateTransferID() string {
	return time.Now().Format("20060102150405.000000")
}
