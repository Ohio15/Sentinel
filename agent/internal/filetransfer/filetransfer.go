package filetransfer

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// ChunkSize is the size of each file chunk for transfer
	ChunkSize = 64 * 1024 // 64KB chunks
)

// FileInfo represents information about a file
type FileInfo struct {
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	IsDir        bool      `json:"is_dir"`
	Mode         string    `json:"mode"`
	ModifiedTime time.Time `json:"modified_time"`
	IsHidden     bool      `json:"is_hidden"`
}

// TransferProgress represents file transfer progress
type TransferProgress struct {
	Filename         string `json:"filename"`
	BytesTransferred int64  `json:"bytes_transferred"`
	TotalBytes       int64  `json:"total_bytes"`
	Percentage       int    `json:"percentage"`
}

// FileTransfer handles file operations
type FileTransfer struct {
	onProgress func(progress TransferProgress)
}

// New creates a new FileTransfer instance
func New(onProgress func(TransferProgress)) *FileTransfer {
	return &FileTransfer{
		onProgress: onProgress,
	}
}

// ListDirectory lists files in a directory
func (ft *FileTransfer) ListDirectory(path string) ([]FileInfo, error) {
	// Expand home directory
	if path == "" || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		path = home
	}

	// Clean and resolve path
	path = filepath.Clean(path)

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	files := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fullPath := filepath.Join(path, entry.Name())
		isHidden := entry.Name()[0] == '.'

		files = append(files, FileInfo{
			Name:         entry.Name(),
			Path:         fullPath,
			Size:         info.Size(),
			IsDir:        entry.IsDir(),
			Mode:         info.Mode().String(),
			ModifiedTime: info.ModTime(),
			IsHidden:     isHidden,
		})
	}

	// Sort: directories first, then by name
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return files[i].Name < files[j].Name
	})

	return files, nil
}

// GetFileInfo returns information about a specific file
func (ft *FileTransfer) GetFileInfo(path string) (*FileInfo, error) {
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	return &FileInfo{
		Name:         info.Name(),
		Path:         path,
		Size:         info.Size(),
		IsDir:        info.IsDir(),
		Mode:         info.Mode().String(),
		ModifiedTime: info.ModTime(),
		IsHidden:     info.Name()[0] == '.',
	}, nil
}

// ReadFile reads a file and returns its contents as base64 chunks
func (ft *FileTransfer) ReadFile(ctx context.Context, path string, chunkHandler func(chunk string, offset int64, total int64) error) error {
	path = filepath.Clean(path)

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	totalSize := info.Size()
	buffer := make([]byte, ChunkSize)
	var offset int64 = 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := file.Read(buffer)
		if n > 0 {
			chunk := base64.StdEncoding.EncodeToString(buffer[:n])
			if err := chunkHandler(chunk, offset, totalSize); err != nil {
				return fmt.Errorf("chunk handler error: %w", err)
			}
			offset += int64(n)

			if ft.onProgress != nil {
				ft.onProgress(TransferProgress{
					Filename:         filepath.Base(path),
					BytesTransferred: offset,
					TotalBytes:       totalSize,
					Percentage:       int(float64(offset) / float64(totalSize) * 100),
				})
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
	}

	return nil
}

// WriteFile writes base64-encoded data to a file
func (ft *FileTransfer) WriteFile(ctx context.Context, path string, data string, append bool) error {
	path = filepath.Clean(path)

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Decode base64 data
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("failed to decode data: %w", err)
	}

	flags := os.O_WRONLY | os.O_CREATE
	if append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	file, err := os.OpenFile(path, flags, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(decoded); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// DeleteFile deletes a file or directory
func (ft *FileTransfer) DeleteFile(path string, recursive bool) error {
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat path: %w", err)
	}

	if info.IsDir() {
		if recursive {
			return os.RemoveAll(path)
		}
		return os.Remove(path)
	}

	return os.Remove(path)
}

// CreateDirectory creates a directory
func (ft *FileTransfer) CreateDirectory(path string) error {
	path = filepath.Clean(path)
	return os.MkdirAll(path, 0755)
}

// MoveFile moves or renames a file
func (ft *FileTransfer) MoveFile(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	// Ensure destination directory exists
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	return os.Rename(src, dst)
}

// CopyFile copies a file
func (ft *FileTransfer) CopyFile(ctx context.Context, src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	// Ensure destination directory exists
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dstFile.Close()

	totalSize := srcInfo.Size()
	buffer := make([]byte, ChunkSize)
	var copied int64 = 0

	for {
		select {
		case <-ctx.Done():
			os.Remove(dst)
			return ctx.Err()
		default:
		}

		n, err := srcFile.Read(buffer)
		if n > 0 {
			written, writeErr := dstFile.Write(buffer[:n])
			if writeErr != nil {
				return fmt.Errorf("failed to write: %w", writeErr)
			}
			copied += int64(written)

			if ft.onProgress != nil {
				ft.onProgress(TransferProgress{
					Filename:         filepath.Base(src),
					BytesTransferred: copied,
					TotalBytes:       totalSize,
					Percentage:       int(float64(copied) / float64(totalSize) * 100),
				})
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read: %w", err)
		}
	}

	// Preserve file permissions
	return os.Chmod(dst, srcInfo.Mode())
}

// GetChecksum calculates SHA256 checksum of a file
func (ft *FileTransfer) GetChecksum(path string) (string, error) {
	path = filepath.Clean(path)

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to calculate checksum: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ScanProgress is sent to report scan progress
type ScanProgress struct {
	ScannedFiles  int   `json:"scanned_files"`
	ScannedDirs   int   `json:"scanned_dirs"`
	TotalSize     int64 `json:"total_size"`
	CurrentPath   string `json:"current_path"`
	Complete      bool  `json:"complete"`
}

// ScanResult contains the complete scan results
type ScanResult struct {
	Files       []FileInfo `json:"files"`
	TotalFiles  int        `json:"total_files"`
	TotalDirs   int        `json:"total_dirs"`
	TotalSize   int64      `json:"total_size"`
	ScanPath    string     `json:"scan_path"`
	Error       string     `json:"error,omitempty"`
}

// ScanDirectoryRecursive scans a directory recursively up to maxDepth
// onProgress is called periodically to report scan progress
func (ft *FileTransfer) ScanDirectoryRecursive(ctx context.Context, rootPath string, maxDepth int, onProgress func(ScanProgress)) (*ScanResult, error) {
	// Expand home directory
	if rootPath == "" || rootPath == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		rootPath = home
	}

	rootPath = filepath.Clean(rootPath)

	result := &ScanResult{
		Files:    make([]FileInfo, 0),
		ScanPath: rootPath,
	}

	var scannedFiles, scannedDirs int
	var totalSize int64
	lastProgress := time.Now()

	err := ft.scanDir(ctx, rootPath, 0, maxDepth, &result.Files, func(path string) {
		// Send progress updates at most every 100ms
		if time.Since(lastProgress) > 100*time.Millisecond && onProgress != nil {
			onProgress(ScanProgress{
				ScannedFiles: scannedFiles,
				ScannedDirs:  scannedDirs,
				TotalSize:    totalSize,
				CurrentPath:  path,
				Complete:     false,
			})
			lastProgress = time.Now()
		}
	}, &scannedFiles, &scannedDirs, &totalSize)

	if err != nil {
		result.Error = err.Error()
	}

	result.TotalFiles = scannedFiles
	result.TotalDirs = scannedDirs
	result.TotalSize = totalSize

	// Send final progress
	if onProgress != nil {
		onProgress(ScanProgress{
			ScannedFiles: scannedFiles,
			ScannedDirs:  scannedDirs,
			TotalSize:    totalSize,
			CurrentPath:  rootPath,
			Complete:     true,
		})
	}

	return result, err
}

func (ft *FileTransfer) scanDir(ctx context.Context, path string, depth, maxDepth int, files *[]FileInfo, onPath func(string), scannedFiles, scannedDirs *int, totalSize *int64) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if maxDepth > 0 && depth >= maxDepth {
		return nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		// Skip directories we can't read (permission denied, etc.)
		return nil
	}

	*scannedDirs++
	onPath(path)

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fullPath := filepath.Join(path, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		isHidden := len(entry.Name()) > 0 && entry.Name()[0] == '.'

		fileInfo := FileInfo{
			Name:         entry.Name(),
			Path:         fullPath,
			Size:         info.Size(),
			IsDir:        entry.IsDir(),
			Mode:         info.Mode().String(),
			ModifiedTime: info.ModTime(),
			IsHidden:     isHidden,
		}

		*files = append(*files, fileInfo)

		if entry.IsDir() {
			// Recursively scan subdirectory
			if err := ft.scanDir(ctx, fullPath, depth+1, maxDepth, files, onPath, scannedFiles, scannedDirs, totalSize); err != nil {
				return err
			}
		} else {
			*scannedFiles++
			*totalSize += info.Size()
		}
	}

	return nil
}
