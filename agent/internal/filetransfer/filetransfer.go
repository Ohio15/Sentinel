package filetransfer

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	// ChunkSize is the size of each file chunk for transfer
	ChunkSize = 64 * 1024 // 64KB chunks
)

// LegacyFileInfo represents information about a file (legacy format for backward compat)
// NOTE: The new FileInfo type is defined in filetransfer_interface.go
type LegacyFileInfo struct {
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
	onProgress   func(progress TransferProgress)
	allowedBases []string // Allowed base directories for file operations
	deniedBases  []string // Protected directories that mutating ops may never touch
}

// New creates a new FileTransfer instance
func New(onProgress func(TransferProgress)) *FileTransfer {
	// Initialize with default allowed base paths
	allowedBases := getDefaultAllowedBases()

	return &FileTransfer{
		onProgress:   onProgress,
		allowedBases: allowedBases,
		deniedBases:  getDefaultDeniedBases(),
	}
}

// getDefaultDeniedBases returns directories that mutating file operations
// (write/create/delete/move-dest) must never target, regardless of the broad
// allowed-base set (AG-H write). This protects the agent's own binaries and
// identity material and core OS directories from a compromised or malicious
// server driving arbitrary SYSTEM writes.
func getDefaultDeniedBases() []string {
	bases := make([]string, 0)
	add := func(p string) {
		if p != "" {
			bases = append(bases, filepath.Clean(p))
		}
	}
	if runtime.GOOS == "windows" {
		add(os.Getenv("SystemRoot"))                                          // C:\Windows (incl. System32)
		add(filepath.Join(os.Getenv("ProgramFiles"), "Sentinel Agent"))       // agent install dir
		add(filepath.Join(os.Getenv("ProgramFiles(x86)"), "Sentinel Agent")) // 32-bit install dir
		add(filepath.Join(os.Getenv("ProgramData"), "Sentinel"))              // config, certs, secrets
	} else {
		add("/etc/sentinel") // agent config/certs
		add("/usr/local/bin/sentinel-agent")
		add("/usr/local/bin/sentinel-watchdog")
		add("/boot")
		add("/proc")
		add("/sys")
		add("/dev")
	}
	return bases
}

// isMutatingOperation reports whether the operation writes to, creates, deletes,
// or moves a destination path (as opposed to a read-only inspection). Only
// mutating operations are subject to the denied-base protection.
func isMutatingOperation(operation string) bool {
	switch operation {
	case "write_file", "write_file_parent",
		"delete_file",
		"create_directory",
		"move_file_dst", "move_file_dst_parent",
		"copy_file_dst", "copy_file_dst_parent":
		return true
	default:
		return false
	}
}

// getDefaultAllowedBases returns the default allowed base directories
func getDefaultAllowedBases() []string {
	bases := make([]string, 0)

	// For RMM functionality, we need full filesystem access
	// Security is still enforced via path traversal protection (no .., symlink resolution, etc.)
	if runtime.GOOS == "windows" {
		// Add all available drive letters on Windows
		for _, drive := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
			drivePath := string(drive) + ":\\"
			if _, err := os.Stat(drivePath); err == nil {
				bases = append(bases, drivePath)
			}
		}
	} else {
		// On Unix-like systems, allow root access
		bases = append(bases, "/")
	}

	return bases
}

// isSubPath checks if child path is within parent directory (directory-boundary aware)
// SECURITY FIX: This fixes the prefix-matching vulnerability where "admin-attacker" matched "admin"
func isSubPath(child, parent string) bool {
	// Clean both paths
	child = filepath.Clean(child)
	parent = filepath.Clean(parent)

	// Case-insensitive comparison on Windows
	if runtime.GOOS == "windows" {
		child = strings.ToLower(child)
		parent = strings.ToLower(parent)
	}

	// Exact match is allowed
	if child == parent {
		return true
	}

	// Ensure parent ends with separator for proper boundary check
	// This prevents "admin-attacker" from matching base "admin"
	parentWithSep := parent
	if !strings.HasSuffix(parentWithSep, string(filepath.Separator)) {
		parentWithSep = parentWithSep + string(filepath.Separator)
	}

	// Child must start with parent + separator to be a true subdirectory
	return strings.HasPrefix(child, parentWithSep)
}

// validatePath validates a path to prevent directory traversal attacks
func (ft *FileTransfer) validatePath(requestedPath string, operation string) (string, error) {
	// Reject empty paths
	if requestedPath == "" {
		log.Printf("[SECURITY] Rejected empty path for operation: %s", operation)
		return "", fmt.Errorf("path cannot be empty")
	}

	// SECURITY FIX: Apply comprehensive path security validation first
	// This handles Unicode normalization, 8.3 short names, reserved names, etc.
	securePath, err := SecurePathValidation(requestedPath)
	if err != nil {
		log.Printf("[SECURITY] Path security validation failed: %s, operation: %s, error: %v",
			requestedPath, operation, err)
		return "", fmt.Errorf("path validation failed: %w", err)
	}
	requestedPath = securePath

	// Check for UNC paths on Windows (e.g., \\server\share)
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(requestedPath, "\\\\") || strings.HasPrefix(requestedPath, "//") {
			log.Printf("[SECURITY] Rejected UNC path: %s for operation: %s", requestedPath, operation)
			return "", fmt.Errorf("UNC paths are not allowed")
		}
	}

	// Get absolute path
	absPath, err := filepath.Abs(requestedPath)
	if err != nil {
		log.Printf("[SECURITY] Failed to get absolute path for: %s, operation: %s, error: %v", requestedPath, operation, err)
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// Resolve symlinks to prevent symlink-based traversal
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// If the path doesn't exist yet (e.g., for write operations), validate the parent
		if os.IsNotExist(err) {
			// For non-existent paths, check the parent directory
			parentDir := filepath.Dir(absPath)
			if parentDir != absPath { // Avoid infinite loop at root
				resolvedParent, err := filepath.EvalSymlinks(parentDir)
				if err != nil && !os.IsNotExist(err) {
					log.Printf("[SECURITY] Failed to resolve parent symlinks for: %s, operation: %s, error: %v", absPath, operation, err)
					return "", fmt.Errorf("invalid path: cannot resolve parent directory")
				}
				// Reconstruct path with resolved parent
				resolvedPath = filepath.Join(resolvedParent, filepath.Base(absPath))
			} else {
				resolvedPath = absPath
			}
		} else {
			log.Printf("[SECURITY] Failed to resolve symlinks for: %s, operation: %s, error: %v", absPath, operation, err)
			return "", fmt.Errorf("invalid path: %w", err)
		}
	}

	// Clean the path to remove any .. or . components
	cleanPath := filepath.Clean(resolvedPath)

	// Check for .. in the cleaned path (shouldn't happen after Clean, but defense in depth)
	if strings.Contains(cleanPath, "..") {
		log.Printf("[SECURITY] Path contains .. after cleaning: %s, operation: %s", cleanPath, operation)
		return "", fmt.Errorf("path traversal detected")
	}

	// Verify the path is within allowed base directories
	allowed := false
	for _, base := range ft.allowedBases {
		// Resolve base path symlinks too
		resolvedBase, err := filepath.EvalSymlinks(base)
		if err != nil {
			// If base doesn't exist or can't be resolved, skip it
			continue
		}

		// Clean the base path
		cleanBase := filepath.Clean(resolvedBase)

		// SECURITY FIX: Use isSubPath instead of strings.HasPrefix
		// This properly validates directory boundaries
		if isSubPath(cleanPath, cleanBase) {
			allowed = true
			break
		}
	}

	if !allowed {
		log.Printf("[SECURITY] Path not within allowed bases: %s, operation: %s", cleanPath, operation)
		return "", fmt.Errorf("access denied: path is outside allowed directories")
	}

	// AG-H write: mutating operations must never touch protected directories
	// (agent install dir, agent data/certs, or core OS dirs), even though those
	// live under an allowed drive base.
	if isMutatingOperation(operation) {
		for _, denied := range ft.deniedBases {
			if isSubPath(cleanPath, denied) {
				log.Printf("[SECURITY] Rejected mutating operation on protected path: %s, operation: %s", cleanPath, operation)
				return "", fmt.Errorf("access denied: path is within a protected directory")
			}
		}
	}

	// Log successful validation
	log.Printf("[FILE ACCESS] Validated path: %s, operation: %s", cleanPath, operation)

	return cleanPath, nil
}

// SetAllowedBases sets custom allowed base directories
func (ft *FileTransfer) SetAllowedBases(bases []string) {
	ft.allowedBases = bases
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

	// Validate path before accessing
	validatedPath, err := ft.validatePath(path, "list_directory")
	if err != nil {
		return nil, fmt.Errorf("path validation failed: %w", err)
	}

	entries, err := os.ReadDir(validatedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	files := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fullPath := filepath.Join(validatedPath, entry.Name())

		files = append(files, FileInfo{
			Name:        entry.Name(),
			Path:        fullPath,
			Size:        info.Size(),
			IsDir:       entry.IsDir(),
			ModTime:     info.ModTime(),
			Permissions: info.Mode().String(),
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
	// Validate path before accessing
	validatedPath, err := ft.validatePath(path, "get_file_info")
	if err != nil {
		return nil, fmt.Errorf("path validation failed: %w", err)
	}

	info, err := os.Stat(validatedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	return &FileInfo{
		Name:        info.Name(),
		Path:        validatedPath,
		Size:        info.Size(),
		IsDir:       info.IsDir(),
		ModTime:     info.ModTime(),
		Permissions: info.Mode().String(),
	}, nil
}

// ReadFile reads a file and returns its contents as base64 chunks
func (ft *FileTransfer) ReadFile(ctx context.Context, path string, chunkHandler func(chunk string, offset int64, total int64) error) error {
	// Validate path before reading
	validatedPath, err := ft.validatePath(path, "read_file")
	if err != nil {
		return fmt.Errorf("path validation failed: %w", err)
	}

	file, err := os.Open(validatedPath)
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
					Filename:         filepath.Base(validatedPath),
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
	// Validate path before writing
	validatedPath, err := ft.validatePath(path, "write_file")
	if err != nil {
		return fmt.Errorf("path validation failed: %w", err)
	}

	// Validate parent directory (also enforces denied-base protection)
	dir := filepath.Dir(validatedPath)
	_, err = ft.validatePath(dir, "write_file_parent")
	if err != nil {
		return fmt.Errorf("parent directory validation failed: %w", err)
	}

	// AG-H write: route through the bounded writer so uploads are subject to
	// size limits, disk-quota checks, streaming decode (no full-buffer memory
	// blowup), no-follow / handle-identity verification, AND real-path
	// containment (the resolved handle must stay inside an allowed base and out
	// of every denied base, defeating intermediate-junction redirection).
	if err := WriteFileWithLimitsBounded(validatedPath, data, append, ft.allowedBases, ft.deniedBases); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// DeleteFile deletes a file or directory
func (ft *FileTransfer) DeleteFile(path string, recursive bool) error {
	// Validate path before deleting
	validatedPath, err := ft.validatePath(path, "delete_file")
	if err != nil {
		return fmt.Errorf("path validation failed: %w", err)
	}

	info, err := os.Stat(validatedPath)
	if err != nil {
		return fmt.Errorf("failed to stat path: %w", err)
	}

	if info.IsDir() {
		if recursive {
			return os.RemoveAll(validatedPath)
		}
		return os.Remove(validatedPath)
	}

	return os.Remove(validatedPath)
}

// CreateDirectory creates a directory
func (ft *FileTransfer) CreateDirectory(path string) error {
	// Validate path before creating
	validatedPath, err := ft.validatePath(path, "create_directory")
	if err != nil {
		return fmt.Errorf("path validation failed: %w", err)
	}

	return os.MkdirAll(validatedPath, 0755)
}

// MoveFile moves or renames a file
func (ft *FileTransfer) MoveFile(src, dst string) error {
	// Validate source path
	validatedSrc, err := ft.validatePath(src, "move_file_src")
	if err != nil {
		return fmt.Errorf("source path validation failed: %w", err)
	}

	// Validate destination path
	validatedDst, err := ft.validatePath(dst, "move_file_dst")
	if err != nil {
		return fmt.Errorf("destination path validation failed: %w", err)
	}

	// Validate destination parent directory
	dir := filepath.Dir(validatedDst)
	_, err = ft.validatePath(dir, "move_file_dst_parent")
	if err != nil {
		return fmt.Errorf("destination parent directory validation failed: %w", err)
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	return os.Rename(validatedSrc, validatedDst)
}

// CopyFile copies a file
func (ft *FileTransfer) CopyFile(ctx context.Context, src, dst string) error {
	// Validate source path
	validatedSrc, err := ft.validatePath(src, "copy_file_src")
	if err != nil {
		return fmt.Errorf("source path validation failed: %w", err)
	}

	// Validate destination path
	validatedDst, err := ft.validatePath(dst, "copy_file_dst")
	if err != nil {
		return fmt.Errorf("destination path validation failed: %w", err)
	}

	srcFile, err := os.Open(validatedSrc)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	// Validate and ensure destination directory exists
	dir := filepath.Dir(validatedDst)
	_, err = ft.validatePath(dir, "copy_file_dst_parent")
	if err != nil {
		return fmt.Errorf("destination parent directory validation failed: %w", err)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	dstFile, err := os.Create(validatedDst)
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
			os.Remove(validatedDst)
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
					Filename:         filepath.Base(validatedSrc),
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
	return os.Chmod(validatedDst, srcInfo.Mode())
}

// GetChecksum calculates SHA256 checksum of a file
func (ft *FileTransfer) GetChecksum(path string) (string, error) {
	// Validate path before reading
	validatedPath, err := ft.validatePath(path, "get_checksum")
	if err != nil {
		return "", fmt.Errorf("path validation failed: %w", err)
	}

	file, err := os.Open(validatedPath)
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

	// Validate path before scanning
	validatedRootPath, err := ft.validatePath(rootPath, "scan_directory")
	if err != nil {
		return nil, fmt.Errorf("path validation failed: %w", err)
	}

	result := &ScanResult{
		Files:    make([]FileInfo, 0),
		ScanPath: validatedRootPath,
	}

	var scannedFiles, scannedDirs int
	var totalSize int64
	lastProgress := time.Now()

	err = ft.scanDir(ctx, validatedRootPath, 0, maxDepth, &result.Files, func(path string) {
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
			CurrentPath:  validatedRootPath,
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

		fileInfo := FileInfo{
			Name:        entry.Name(),
			Path:        fullPath,
			Size:        info.Size(),
			IsDir:       entry.IsDir(),
			ModTime:     info.ModTime(),
			Permissions: info.Mode().String(),
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
