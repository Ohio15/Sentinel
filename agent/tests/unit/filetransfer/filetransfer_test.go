package filetransfer_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sentinel/agent/internal/filetransfer"
)

func TestDefaultTransferConfig(t *testing.T) {
	config := filetransfer.DefaultTransferConfig()

	if config.ChunkSize != filetransfer.DefaultChunkSize {
		t.Errorf("Expected ChunkSize %d, got %d", filetransfer.DefaultChunkSize, config.ChunkSize)
	}

	if config.MaxFileSize != filetransfer.MaxFileSize {
		t.Errorf("Expected MaxFileSize %d, got %d", filetransfer.MaxFileSize, config.MaxFileSize)
	}

	if config.MaxConcurrent != filetransfer.MaxConcurrentTransfers {
		t.Errorf("Expected MaxConcurrent %d, got %d", filetransfer.MaxConcurrentTransfers, config.MaxConcurrent)
	}

	if !config.AllowUpload {
		t.Error("Expected AllowUpload to be true")
	}

	if !config.AllowDownload {
		t.Error("Expected AllowDownload to be true")
	}

	if !config.ResumeEnabled {
		t.Error("Expected ResumeEnabled to be true")
	}

	if !config.IntegrityCheck {
		t.Error("Expected IntegrityCheck to be true")
	}

	// Check blocked extensions
	if len(config.BlockedExtensions) == 0 {
		t.Error("Expected some blocked extensions")
	}
}

func TestFileTransferManager_Initialize(t *testing.T) {
	manager := filetransfer.NewFileTransferManager()
	config := filetransfer.DefaultTransferConfig()

	err := manager.Initialize(config)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Should be able to initialize multiple times
	err = manager.Initialize(config)
	if err != nil {
		t.Fatalf("Re-initialize failed: %v", err)
	}
}

func TestFileTransferManager_ListDirectory(t *testing.T) {
	manager := filetransfer.NewFileTransferManager()
	config := filetransfer.DefaultTransferConfig()
	manager.Initialize(config)

	ctx := context.Background()

	// List the temp directory
	tmpDir := os.TempDir()
	listing, err := manager.ListDirectory(ctx, tmpDir)
	if err != nil {
		t.Fatalf("ListDirectory failed: %v", err)
	}

	if listing == nil {
		t.Fatal("Expected non-nil listing")
	}

	if listing.Path == "" {
		t.Error("Expected non-empty path")
	}

	// Files should be sorted (dirs first)
	prevIsDir := true
	for _, f := range listing.Files {
		if prevIsDir && !f.IsDir {
			prevIsDir = false
		} else if !prevIsDir && f.IsDir {
			t.Error("Expected directories before files")
			break
		}
	}
}

func TestFileTransferManager_GetFileInfo(t *testing.T) {
	manager := filetransfer.NewFileTransferManager()
	config := filetransfer.DefaultTransferConfig()
	manager.Initialize(config)

	ctx := context.Background()

	// Create a temp file
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("test content")
	tmpFile.Close()

	info, err := manager.GetFileInfo(ctx, tmpFile.Name())
	if err != nil {
		t.Fatalf("GetFileInfo failed: %v", err)
	}

	if info == nil {
		t.Fatal("Expected non-nil FileInfo")
	}

	if info.Size != 12 { // "test content"
		t.Errorf("Expected size 12, got %d", info.Size)
	}

	if info.IsDir {
		t.Error("Expected IsDir to be false")
	}
}

func TestFileTransferManager_StartDownload(t *testing.T) {
	manager := filetransfer.NewFileTransferManager()
	config := filetransfer.DefaultTransferConfig()
	manager.Initialize(config)

	ctx := context.Background()

	// Create a temp file
	tmpFile, err := os.CreateTemp("", "test-download-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("download test content")
	tmpFile.Close()

	req := &filetransfer.TransferRequest{
		Direction:  filetransfer.DirectionDownload,
		SourcePath: tmpFile.Name(),
		DestPath:   "/remote/path",
	}

	transfer, err := manager.StartDownload(ctx, req)
	if err != nil {
		t.Fatalf("StartDownload failed: %v", err)
	}

	if transfer == nil {
		t.Fatal("Expected non-nil transfer")
	}

	if transfer.ID == "" {
		t.Error("Expected non-empty transfer ID")
	}

	if transfer.State != filetransfer.StateInProgress {
		t.Errorf("Expected state %s, got %s", filetransfer.StateInProgress, transfer.State)
	}

	if transfer.FileSize != 21 { // "download test content"
		t.Errorf("Expected file size 21, got %d", transfer.FileSize)
	}

	// Cleanup
	manager.CancelTransfer(ctx, transfer.ID)
}

func TestFileTransferManager_StartUpload(t *testing.T) {
	manager := filetransfer.NewFileTransferManager()
	config := filetransfer.DefaultTransferConfig()
	manager.Initialize(config)

	ctx := context.Background()

	// Create temp dir for upload
	tmpDir, err := os.MkdirTemp("", "test-upload")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	destPath := filepath.Join(tmpDir, "uploaded.txt")

	req := &filetransfer.TransferRequest{
		Direction:  filetransfer.DirectionUpload,
		SourcePath: "/remote/source.txt",
		DestPath:   destPath,
		FileSize:   100,
	}

	transfer, err := manager.StartUpload(ctx, req)
	if err != nil {
		t.Fatalf("StartUpload failed: %v", err)
	}

	if transfer == nil {
		t.Fatal("Expected non-nil transfer")
	}

	if transfer.State != filetransfer.StateInProgress {
		t.Errorf("Expected state %s, got %s", filetransfer.StateInProgress, transfer.State)
	}

	// Cleanup
	manager.CancelTransfer(ctx, transfer.ID)
}

func TestFileTransferManager_BlockedExtensions(t *testing.T) {
	manager := filetransfer.NewFileTransferManager()
	config := filetransfer.DefaultTransferConfig()
	manager.Initialize(config)

	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "test-blocked")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Try to upload a blocked extension
	req := &filetransfer.TransferRequest{
		Direction:  filetransfer.DirectionUpload,
		SourcePath: "/remote/malware.exe",
		DestPath:   filepath.Join(tmpDir, "test.exe"),
		FileSize:   100,
	}

	_, err = manager.StartUpload(ctx, req)
	if err != filetransfer.ErrAccessDenied {
		t.Errorf("Expected ErrAccessDenied for .exe, got %v", err)
	}
}

func TestFileTransferManager_ReadChunk(t *testing.T) {
	manager := filetransfer.NewFileTransferManager()
	config := filetransfer.DefaultTransferConfig()
	config.ChunkSize = 10 // Small chunk for testing
	manager.Initialize(config)

	ctx := context.Background()

	// Create a temp file with content
	tmpFile, err := os.CreateTemp("", "test-chunk-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("0123456789ABCDEF") // 16 bytes
	tmpFile.Close()

	req := &filetransfer.TransferRequest{
		Direction:  filetransfer.DirectionDownload,
		SourcePath: tmpFile.Name(),
		DestPath:   "/remote/path",
	}

	transfer, err := manager.StartDownload(ctx, req)
	if err != nil {
		t.Fatalf("StartDownload failed: %v", err)
	}

	// Read first chunk
	chunk, err := manager.ReadChunk(ctx, transfer.ID, 0)
	if err != nil {
		t.Fatalf("ReadChunk failed: %v", err)
	}

	if chunk == nil {
		t.Fatal("Expected non-nil chunk")
	}

	if chunk.Index != 0 {
		t.Errorf("Expected chunk index 0, got %d", chunk.Index)
	}

	if chunk.Size != 10 {
		t.Errorf("Expected chunk size 10, got %d", chunk.Size)
	}

	if string(chunk.Data) != "0123456789" {
		t.Errorf("Expected chunk data '0123456789', got '%s'", string(chunk.Data))
	}

	if chunk.Hash == "" {
		t.Error("Expected non-empty chunk hash")
	}

	// Read second chunk
	chunk, err = manager.ReadChunk(ctx, transfer.ID, 1)
	if err != nil {
		t.Fatalf("ReadChunk failed: %v", err)
	}

	if string(chunk.Data) != "ABCDEF" {
		t.Errorf("Expected chunk data 'ABCDEF', got '%s'", string(chunk.Data))
	}

	if !chunk.IsLast {
		t.Error("Expected chunk to be last")
	}
}

func TestFileTransferManager_WriteChunk(t *testing.T) {
	manager := filetransfer.NewFileTransferManager()
	config := filetransfer.DefaultTransferConfig()
	config.ChunkSize = 10
	manager.Initialize(config)

	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "test-write")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	destPath := filepath.Join(tmpDir, "uploaded.txt")

	req := &filetransfer.TransferRequest{
		Direction:  filetransfer.DirectionUpload,
		SourcePath: "/remote/source.txt",
		DestPath:   destPath,
		FileSize:   20,
	}

	transfer, err := manager.StartUpload(ctx, req)
	if err != nil {
		t.Fatalf("StartUpload failed: %v", err)
	}

	// Write first chunk
	ack, err := manager.WriteChunk(ctx, &filetransfer.Chunk{
		TransferID: transfer.ID,
		Index:      0,
		Offset:     0,
		Size:       10,
		Data:       []byte("0123456789"),
	})

	if err != nil {
		t.Fatalf("WriteChunk failed: %v", err)
	}

	if !ack.Success {
		t.Errorf("Expected success, got error: %s", ack.Error)
	}

	// Write second chunk (last)
	ack, err = manager.WriteChunk(ctx, &filetransfer.Chunk{
		TransferID: transfer.ID,
		Index:      1,
		Offset:     10,
		Size:       10,
		Data:       []byte("ABCDEFGHIJ"),
		IsLast:     true,
	})

	if err != nil {
		t.Fatalf("WriteChunk failed: %v", err)
	}

	if !ack.Success {
		t.Errorf("Expected success, got error: %s", ack.Error)
	}

	// Check transfer completed
	updatedTransfer, _ := manager.GetTransfer(ctx, transfer.ID)
	if updatedTransfer.State != filetransfer.StateCompleted {
		t.Errorf("Expected state %s, got %s", filetransfer.StateCompleted, updatedTransfer.State)
	}

	// Verify file content
	content, _ := os.ReadFile(destPath)
	if string(content) != "0123456789ABCDEFGHIJ" {
		t.Errorf("Expected file content '0123456789ABCDEFGHIJ', got '%s'", string(content))
	}
}

func TestFileTransferManager_CancelTransfer(t *testing.T) {
	manager := filetransfer.NewFileTransferManager()
	config := filetransfer.DefaultTransferConfig()
	manager.Initialize(config)

	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "test-cancel")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	req := &filetransfer.TransferRequest{
		Direction:  filetransfer.DirectionUpload,
		SourcePath: "/remote/source.txt",
		DestPath:   filepath.Join(tmpDir, "cancelled.txt"),
		FileSize:   1000,
	}

	transfer, err := manager.StartUpload(ctx, req)
	if err != nil {
		t.Fatalf("StartUpload failed: %v", err)
	}

	err = manager.CancelTransfer(ctx, transfer.ID)
	if err != nil {
		t.Fatalf("CancelTransfer failed: %v", err)
	}

	// Should not be able to get canceled transfer
	_, err = manager.GetTransfer(ctx, transfer.ID)
	if err != filetransfer.ErrTransferNotFound {
		t.Errorf("Expected ErrTransferNotFound, got %v", err)
	}
}

func TestFileTransferManager_GetActiveTransfers(t *testing.T) {
	manager := filetransfer.NewFileTransferManager()
	config := filetransfer.DefaultTransferConfig()
	manager.Initialize(config)

	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "test-active")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Start multiple transfers
	for i := 0; i < 3; i++ {
		req := &filetransfer.TransferRequest{
			Direction:  filetransfer.DirectionUpload,
			SourcePath: "/remote/source.txt",
			DestPath:   filepath.Join(tmpDir, "file%d.txt"),
			FileSize:   1000,
		}
		manager.StartUpload(ctx, req)
	}

	transfers, err := manager.GetActiveTransfers(ctx)
	if err != nil {
		t.Fatalf("GetActiveTransfers failed: %v", err)
	}

	if len(transfers) != 3 {
		t.Errorf("Expected 3 active transfers, got %d", len(transfers))
	}

	// Cleanup
	for _, tr := range transfers {
		manager.CancelTransfer(ctx, tr.ID)
	}
}

func TestGenerateTransferID(t *testing.T) {
	id1 := filetransfer.GenerateTransferID()
	time.Sleep(1 * time.Microsecond)
	id2 := filetransfer.GenerateTransferID()

	if id1 == "" {
		t.Error("Expected non-empty ID")
	}

	if id1 == id2 {
		t.Error("Expected different IDs")
	}
}

func TestTransferDirection(t *testing.T) {
	if filetransfer.DirectionUpload != "upload" {
		t.Errorf("Expected DirectionUpload 'upload', got '%s'", filetransfer.DirectionUpload)
	}

	if filetransfer.DirectionDownload != "download" {
		t.Errorf("Expected DirectionDownload 'download', got '%s'", filetransfer.DirectionDownload)
	}
}

func TestTransferState(t *testing.T) {
	states := []filetransfer.TransferState{
		filetransfer.StateQueued,
		filetransfer.StateInProgress,
		filetransfer.StatePaused,
		filetransfer.StateCompleted,
		filetransfer.StateFailed,
		filetransfer.StateCanceled,
	}

	// Verify all states are unique
	seen := make(map[filetransfer.TransferState]bool)
	for _, s := range states {
		if seen[s] {
			t.Errorf("Duplicate state: %s", s)
		}
		seen[s] = true
	}
}

func TestChunkEncodeDecode(t *testing.T) {
	original := []byte("test data for encoding")

	encoded := filetransfer.EncodeChunkData(original)
	if encoded == "" {
		t.Error("Expected non-empty encoded data")
	}

	decoded, err := filetransfer.DecodeChunkData(encoded)
	if err != nil {
		t.Fatalf("DecodeChunkData failed: %v", err)
	}

	if string(decoded) != string(original) {
		t.Errorf("Expected '%s', got '%s'", string(original), string(decoded))
	}
}
