package filetransfer

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"hash"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileTransferManager implements IFileTransfer
type FileTransferManager struct {
	config           TransferConfig
	transfers        map[string]*activeTransfer
	stats            TransferStats
	onProgress       func(transfer *Transfer)
	onComplete       func(transfer *Transfer)
	mu               sync.RWMutex
}

// activeTransfer tracks an in-progress transfer
type activeTransfer struct {
	transfer     *Transfer
	file         *os.File
	hasher       hash.Hash
	ctx          context.Context
	cancel       context.CancelFunc
	lastActivity time.Time
	mu           sync.Mutex
}

// NewFileTransferManager creates a new file transfer manager
func NewFileTransferManager() *FileTransferManager {
	return &FileTransferManager{
		transfers: make(map[string]*activeTransfer),
	}
}

// Initialize sets up the file transfer manager
func (m *FileTransferManager) Initialize(config TransferConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = config
	log.Printf("[FileTransfer] Initialized with config: chunkSize=%d, maxSize=%d, upload=%v, download=%v",
		config.ChunkSize, config.MaxFileSize, config.AllowUpload, config.AllowDownload)

	return nil
}

// ListDirectory lists files in a directory
func (m *FileTransferManager) ListDirectory(ctx context.Context, path string) (*DirectoryListing, error) {
	// Validate path
	if err := m.validatePath(path); err != nil {
		return nil, err
	}

	// Clean and resolve path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	// Read directory
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	listing := &DirectoryListing{
		Path:  absPath,
		Files: make([]FileInfo, 0, len(entries)),
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		listing.Files = append(listing.Files, FileInfo{
			Name:    info.Name(),
			Path:    filepath.Join(absPath, info.Name()),
			Size:    info.Size(),
			IsDir:   info.IsDir(),
			ModTime: info.ModTime(),
		})
	}

	// Sort: directories first, then alphabetically
	sort.Slice(listing.Files, func(i, j int) bool {
		if listing.Files[i].IsDir != listing.Files[j].IsDir {
			return listing.Files[i].IsDir
		}
		return strings.ToLower(listing.Files[i].Name) < strings.ToLower(listing.Files[j].Name)
	})

	return listing, nil
}

// GetFileInfo returns information about a file
func (m *FileTransferManager) GetFileInfo(ctx context.Context, path string) (*FileInfo, error) {
	if err := m.validatePath(path); err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	return &FileInfo{
		Name:    info.Name(),
		Path:    absPath,
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
	}, nil
}

// StartUpload initiates a file upload (viewer -> host)
func (m *FileTransferManager) StartUpload(ctx context.Context, req *TransferRequest) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if uploads allowed
	if !m.config.AllowUpload {
		return nil, ErrAccessDenied
	}

	// Validate destination path
	if err := m.validatePath(req.DestPath); err != nil {
		return nil, err
	}

	// Check file extension
	if err := m.validateExtension(req.DestPath); err != nil {
		return nil, err
	}

	// Check file size
	if req.FileSize > m.config.MaxFileSize {
		return nil, ErrFileTooLarge
	}

	// Check concurrent transfers
	if len(m.transfers) >= m.config.MaxConcurrent {
		return nil, ErrTransferInProgress
	}

	// Check if file exists
	absPath, err := filepath.Abs(req.DestPath)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if _, err := os.Stat(absPath); err == nil && !req.Overwrite {
		return nil, ErrAlreadyExists
	}

	// Create directory if needed
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Open file for writing
	var file *os.File
	var resumeOffset int64

	if req.ResumeFrom > 0 && m.config.ResumeEnabled {
		// Try to resume
		file, err = os.OpenFile(absPath, os.O_WRONLY, 0644)
		if err == nil {
			// Verify file size matches resume point
			info, _ := file.Stat()
			if info.Size() >= req.ResumeFrom {
				resumeOffset = req.ResumeFrom
				file.Seek(resumeOffset, io.SeekStart)
			} else {
				file.Close()
				file = nil
			}
		}
	}

	if file == nil {
		file, err = os.Create(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create file: %w", err)
		}
		resumeOffset = 0
	}

	// Generate transfer ID
	transferID := req.TransferID
	if transferID == "" {
		transferID = GenerateTransferID()
	}

	// Calculate chunks
	chunkSize := m.config.ChunkSize
	chunksTotal := int((req.FileSize + int64(chunkSize) - 1) / int64(chunkSize))

	transfer := &Transfer{
		ID:          transferID,
		Direction:   DirectionUpload,
		SourcePath:  req.SourcePath,
		DestPath:    absPath,
		FileSize:    req.FileSize,
		Transferred: resumeOffset,
		State:       StateInProgress,
		StartTime:   time.Now(),
		ChunksTotal: chunksTotal,
		ChunksSent:  int(resumeOffset / int64(chunkSize)),
	}

	// Calculate initial progress
	if req.FileSize > 0 {
		transfer.Progress = float64(transfer.Transferred) / float64(req.FileSize) * 100
	}

	transferCtx, cancel := context.WithTimeout(ctx, m.config.TransferTimeout)

	m.transfers[transferID] = &activeTransfer{
		transfer:     transfer,
		file:         file,
		hasher:       sha256.New(),
		ctx:          transferCtx,
		cancel:       cancel,
		lastActivity: time.Now(),
	}

	m.stats.TotalTransfers++
	m.stats.ActiveTransfers++

	log.Printf("[FileTransfer] Upload started: %s -> %s (size=%d, resume=%d)",
		req.SourcePath, absPath, req.FileSize, resumeOffset)

	return transfer, nil
}

// StartDownload initiates a file download (host -> viewer)
func (m *FileTransferManager) StartDownload(ctx context.Context, req *TransferRequest) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if downloads allowed
	if !m.config.AllowDownload {
		return nil, ErrAccessDenied
	}

	// Validate source path
	if err := m.validatePath(req.SourcePath); err != nil {
		return nil, err
	}

	// Check concurrent transfers
	if len(m.transfers) >= m.config.MaxConcurrent {
		return nil, ErrTransferInProgress
	}

	// Get file info
	absPath, err := filepath.Abs(req.SourcePath)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("file not found: %w", err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("cannot download directory")
	}

	// Check file size
	if info.Size() > m.config.MaxFileSize {
		return nil, ErrFileTooLarge
	}

	// Open file for reading
	file, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	// Handle resume
	var resumeOffset int64
	if req.ResumeFrom > 0 && m.config.ResumeEnabled {
		if req.ResumeFrom < info.Size() {
			resumeOffset = req.ResumeFrom
			file.Seek(resumeOffset, io.SeekStart)
		}
	}

	// Generate transfer ID
	transferID := req.TransferID
	if transferID == "" {
		transferID = GenerateTransferID()
	}

	// Calculate chunks
	chunkSize := m.config.ChunkSize
	chunksTotal := int((info.Size() + int64(chunkSize) - 1) / int64(chunkSize))

	transfer := &Transfer{
		ID:          transferID,
		Direction:   DirectionDownload,
		SourcePath:  absPath,
		DestPath:    req.DestPath,
		FileSize:    info.Size(),
		Transferred: resumeOffset,
		State:       StateInProgress,
		StartTime:   time.Now(),
		ChunksTotal: chunksTotal,
		ChunksSent:  int(resumeOffset / int64(chunkSize)),
	}

	if info.Size() > 0 {
		transfer.Progress = float64(transfer.Transferred) / float64(info.Size()) * 100
	}

	transferCtx, cancel := context.WithTimeout(ctx, m.config.TransferTimeout)

	m.transfers[transferID] = &activeTransfer{
		transfer:     transfer,
		file:         file,
		hasher:       sha256.New(),
		ctx:          transferCtx,
		cancel:       cancel,
		lastActivity: time.Now(),
	}

	m.stats.TotalTransfers++
	m.stats.ActiveTransfers++

	log.Printf("[FileTransfer] Download started: %s -> %s (size=%d, resume=%d)",
		absPath, req.DestPath, info.Size(), resumeOffset)

	return transfer, nil
}

// WriteChunk writes a chunk of data during upload
func (m *FileTransferManager) WriteChunk(ctx context.Context, chunk *Chunk) (*ChunkAck, error) {
	m.mu.RLock()
	at, exists := m.transfers[chunk.TransferID]
	m.mu.RUnlock()

	if !exists {
		return &ChunkAck{
			TransferID: chunk.TransferID,
			Index:      chunk.Index,
			Success:    false,
			Error:      "transfer not found",
		}, ErrTransferNotFound
	}

	at.mu.Lock()
	defer at.mu.Unlock()

	// Check if transfer is still active
	if at.transfer.State != StateInProgress {
		return &ChunkAck{
			TransferID: chunk.TransferID,
			Index:      chunk.Index,
			Success:    false,
			Error:      "transfer not in progress",
		}, nil
	}

	// Verify chunk integrity if hash provided
	if chunk.Hash != "" && m.config.IntegrityCheck {
		actualHash := sha256.Sum256(chunk.Data)
		if fmt.Sprintf("%x", actualHash) != chunk.Hash {
			return &ChunkAck{
				TransferID: chunk.TransferID,
				Index:      chunk.Index,
				Success:    false,
				Error:      "chunk integrity check failed",
			}, ErrIntegrityFailed
		}
	}

	// Seek to correct position
	if _, err := at.file.Seek(chunk.Offset, io.SeekStart); err != nil {
		return &ChunkAck{
			TransferID: chunk.TransferID,
			Index:      chunk.Index,
			Success:    false,
			Error:      err.Error(),
		}, err
	}

	// Write chunk data
	n, err := at.file.Write(chunk.Data)
	if err != nil {
		return &ChunkAck{
			TransferID: chunk.TransferID,
			Index:      chunk.Index,
			Success:    false,
			Error:      err.Error(),
		}, err
	}

	// Update hasher
	at.hasher.Write(chunk.Data)

	// Update transfer stats
	at.transfer.Transferred += int64(n)
	at.transfer.ChunksSent++
	at.lastActivity = time.Now()

	// Calculate progress and speed
	if at.transfer.FileSize > 0 {
		at.transfer.Progress = float64(at.transfer.Transferred) / float64(at.transfer.FileSize) * 100
	}
	elapsed := time.Since(at.transfer.StartTime).Seconds()
	if elapsed > 0 {
		at.transfer.Speed = float64(at.transfer.Transferred) / elapsed
	}

	// Notify progress
	if m.onProgress != nil {
		go m.onProgress(at.transfer)
	}

	// Check if complete
	if chunk.IsLast || at.transfer.Transferred >= at.transfer.FileSize {
		at.transfer.State = StateCompleted
		at.transfer.EndTime = time.Now()
		at.transfer.Hash = fmt.Sprintf("%x", at.hasher.Sum(nil))

		// Verify final hash if provided
		if m.config.IntegrityCheck && at.transfer.Hash != "" {
			// Hash verification would be done here
		}

		at.file.Close()

		m.mu.Lock()
		m.stats.ActiveTransfers--
		m.stats.CompletedCount++
		m.stats.TotalBytesUp += at.transfer.Transferred
		m.mu.Unlock()

		if m.onComplete != nil {
			go m.onComplete(at.transfer)
		}

		log.Printf("[FileTransfer] Upload completed: %s (size=%d, hash=%s)",
			at.transfer.DestPath, at.transfer.Transferred, at.transfer.Hash)
	}

	return &ChunkAck{
		TransferID: chunk.TransferID,
		Index:      chunk.Index,
		Success:    true,
	}, nil
}

// ReadChunk reads a chunk of data during download
func (m *FileTransferManager) ReadChunk(ctx context.Context, transferID string, index int) (*Chunk, error) {
	m.mu.RLock()
	at, exists := m.transfers[transferID]
	m.mu.RUnlock()

	if !exists {
		return nil, ErrTransferNotFound
	}

	at.mu.Lock()
	defer at.mu.Unlock()

	if at.transfer.State != StateInProgress {
		return nil, fmt.Errorf("transfer not in progress")
	}

	chunkSize := m.config.ChunkSize
	offset := int64(index) * int64(chunkSize)

	// Seek to chunk position
	if _, err := at.file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	// Read chunk
	data := make([]byte, chunkSize)
	n, err := at.file.Read(data)
	if err != nil && err != io.EOF {
		return nil, err
	}

	data = data[:n]

	// Calculate chunk hash
	chunkHash := sha256.Sum256(data)

	// Update hasher
	at.hasher.Write(data)

	// Update transfer stats
	at.transfer.Transferred += int64(n)
	at.transfer.ChunksSent++
	at.lastActivity = time.Now()

	if at.transfer.FileSize > 0 {
		at.transfer.Progress = float64(at.transfer.Transferred) / float64(at.transfer.FileSize) * 100
	}
	elapsed := time.Since(at.transfer.StartTime).Seconds()
	if elapsed > 0 {
		at.transfer.Speed = float64(at.transfer.Transferred) / elapsed
	}

	// Check if last chunk
	isLast := at.transfer.Transferred >= at.transfer.FileSize

	chunk := &Chunk{
		TransferID: transferID,
		Index:      index,
		Offset:     offset,
		Size:       n,
		Data:       data,
		Hash:       fmt.Sprintf("%x", chunkHash),
		IsLast:     isLast,
	}

	// Notify progress
	if m.onProgress != nil {
		go m.onProgress(at.transfer)
	}

	// Complete if last chunk
	if isLast {
		at.transfer.State = StateCompleted
		at.transfer.EndTime = time.Now()
		at.transfer.Hash = fmt.Sprintf("%x", at.hasher.Sum(nil))

		at.file.Close()

		m.mu.Lock()
		m.stats.ActiveTransfers--
		m.stats.CompletedCount++
		m.stats.TotalBytesDown += at.transfer.Transferred
		m.mu.Unlock()

		if m.onComplete != nil {
			go m.onComplete(at.transfer)
		}

		log.Printf("[FileTransfer] Download completed: %s (size=%d, hash=%s)",
			at.transfer.SourcePath, at.transfer.Transferred, at.transfer.Hash)
	}

	return chunk, nil
}

// PauseTransfer pauses an active transfer
func (m *FileTransferManager) PauseTransfer(ctx context.Context, transferID string) error {
	m.mu.RLock()
	at, exists := m.transfers[transferID]
	m.mu.RUnlock()

	if !exists {
		return ErrTransferNotFound
	}

	at.mu.Lock()
	defer at.mu.Unlock()

	if at.transfer.State != StateInProgress {
		return fmt.Errorf("transfer not in progress")
	}

	at.transfer.State = StatePaused
	log.Printf("[FileTransfer] Paused: %s", transferID)

	return nil
}

// ResumeTransfer resumes a paused transfer
func (m *FileTransferManager) ResumeTransfer(ctx context.Context, transferID string) error {
	m.mu.RLock()
	at, exists := m.transfers[transferID]
	m.mu.RUnlock()

	if !exists {
		return ErrTransferNotFound
	}

	at.mu.Lock()
	defer at.mu.Unlock()

	if at.transfer.State != StatePaused {
		return fmt.Errorf("transfer not paused")
	}

	at.transfer.State = StateInProgress
	at.lastActivity = time.Now()
	log.Printf("[FileTransfer] Resumed: %s", transferID)

	return nil
}

// CancelTransfer cancels an active transfer
func (m *FileTransferManager) CancelTransfer(ctx context.Context, transferID string) error {
	m.mu.Lock()
	at, exists := m.transfers[transferID]
	if !exists {
		m.mu.Unlock()
		return ErrTransferNotFound
	}
	delete(m.transfers, transferID)
	m.stats.ActiveTransfers--
	m.mu.Unlock()

	at.mu.Lock()
	defer at.mu.Unlock()

	at.transfer.State = StateCanceled
	at.transfer.EndTime = time.Now()
	at.cancel()

	if at.file != nil {
		at.file.Close()
	}

	// Delete partial upload file
	if at.transfer.Direction == DirectionUpload && at.transfer.State != StateCompleted {
		os.Remove(at.transfer.DestPath)
	}

	log.Printf("[FileTransfer] Canceled: %s", transferID)

	return nil
}

// GetTransfer returns the current state of a transfer
func (m *FileTransferManager) GetTransfer(ctx context.Context, transferID string) (*Transfer, error) {
	m.mu.RLock()
	at, exists := m.transfers[transferID]
	m.mu.RUnlock()

	if !exists {
		return nil, ErrTransferNotFound
	}

	at.mu.Lock()
	defer at.mu.Unlock()

	// Return a copy
	transfer := *at.transfer
	return &transfer, nil
}

// GetActiveTransfers returns all active transfers
func (m *FileTransferManager) GetActiveTransfers(ctx context.Context) ([]*Transfer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	transfers := make([]*Transfer, 0, len(m.transfers))
	for _, at := range m.transfers {
		at.mu.Lock()
		transfer := *at.transfer
		at.mu.Unlock()
		transfers = append(transfers, &transfer)
	}

	return transfers, nil
}

// GetStats returns transfer statistics
func (m *FileTransferManager) GetStats() *TransferStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := m.stats
	return &stats
}

// OnProgress sets a callback for transfer progress updates
func (m *FileTransferManager) OnProgress(callback func(transfer *Transfer)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onProgress = callback
}

// OnComplete sets a callback for transfer completion
func (m *FileTransferManager) OnComplete(callback func(transfer *Transfer)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onComplete = callback
}

// Release frees resources
func (m *FileTransferManager) Release() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, at := range m.transfers {
		at.cancel()
		if at.file != nil {
			at.file.Close()
		}
		delete(m.transfers, id)
	}

	log.Printf("[FileTransfer] Released")
}

// validatePath checks if the path is allowed.
//
// AG-M path: the previous implementation only rejected a literal ".." substring
// and used strings.HasPrefix for allowed-path matching, which both misses
// traversal via symlinks/8.3-names/Unicode and lets "C:\allowed-evil" match
// base "C:\allowed". Route through the same sound validation used by
// FileTransfer: SecurePathValidation (Unicode/short-name/reserved-name/abs
// normalization) plus isSubPath for directory-boundary-aware containment.
func (m *FileTransferManager) validatePath(path string) error {
	if path == "" {
		return ErrInvalidPath
	}

	securePath, err := SecurePathValidation(path)
	if err != nil {
		return ErrInvalidPath
	}

	// Check allowed paths if configured
	if len(m.config.AllowedPaths) > 0 {
		allowed := false
		for _, allowedPath := range m.config.AllowedPaths {
			allowedAbs, absErr := filepath.Abs(allowedPath)
			if absErr != nil {
				continue
			}
			if isSubPath(securePath, filepath.Clean(allowedAbs)) {
				allowed = true
				break
			}
		}
		if !allowed {
			return ErrAccessDenied
		}
	}

	return nil
}

// validateExtension checks if the file extension is allowed
func (m *FileTransferManager) validateExtension(path string) error {
	ext := strings.ToLower(filepath.Ext(path))

	for _, blocked := range m.config.BlockedExtensions {
		if ext == strings.ToLower(blocked) {
			return ErrAccessDenied
		}
	}

	return nil
}

// EncodeChunkData encodes chunk data to base64 for JSON transport
func EncodeChunkData(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// DecodeChunkData decodes base64 chunk data
func DecodeChunkData(encoded string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encoded)
}

// Compile-time interface check
var _ IFileTransfer = (*FileTransferManager)(nil)
