package filetransfer

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// =============================================================================
// CW-007 Security Fix: File Upload Size Limits and Streaming
// Prevents memory exhaustion and disk space abuse through upload limits
// =============================================================================

const (
	// MaxUploadSize is the maximum allowed file upload size (100MB)
	MaxUploadSize int64 = 100 * 1024 * 1024

	// MinDiskSpaceRequired is the minimum free disk space required (500MB)
	MinDiskSpaceRequired uint64 = 500 * 1024 * 1024

	// StreamingChunkSize is the size of chunks for streaming decode (64KB)
	StreamingChunkSize = 64 * 1024
)

// UploadSizeError represents a file size limit error
type UploadSizeError struct {
	RequestedSize int64
	MaxSize       int64
}

func (e *UploadSizeError) Error() string {
	return fmt.Sprintf("file size %d bytes exceeds maximum allowed %d bytes (%.2f MB)",
		e.RequestedSize, e.MaxSize, float64(e.MaxSize)/(1024*1024))
}

// DiskQuotaError represents a disk space error
type DiskQuotaError struct {
	Available uint64
	Required  uint64
	Path      string
}

func (e *DiskQuotaError) Error() string {
	return fmt.Sprintf("insufficient disk space: %d bytes available, %d bytes required on %s",
		e.Available, e.Required, e.Path)
}

// ValidateUploadSize checks if the upload size is within limits
// CW-007: Call before processing any upload to prevent memory exhaustion
func ValidateUploadSize(size int64) error {
	if size <= 0 {
		return fmt.Errorf("invalid file size: %d", size)
	}
	if size > MaxUploadSize {
		log.Printf("[SECURITY] Upload size limit exceeded: requested %d bytes, max %d bytes", size, MaxUploadSize)
		return &UploadSizeError{
			RequestedSize: size,
			MaxSize:       MaxUploadSize,
		}
	}
	return nil
}

// ValidateBase64UploadSize validates the expected decoded size from base64 data length
// Base64 encoding adds ~33% overhead, so decoded size = base64_len * 3 / 4
func ValidateBase64UploadSize(base64DataLen int) error {
	// Calculate approximate decoded size (base64 is 4/3 the size of binary)
	estimatedDecodedSize := int64(base64DataLen * 3 / 4)
	return ValidateUploadSize(estimatedDecodedSize)
}

// CheckDiskQuota verifies sufficient disk space is available
// CW-007: Call before writing files to prevent disk exhaustion attacks
func CheckDiskQuota(path string, requiredBytes int64) error {
	// Get the drive/mount point for the path
	dir := filepath.Dir(path)

	// Ensure directory exists for stat
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		// Check parent directory if target doesn't exist
		dir = filepath.Dir(dir)
	}

	available, err := getAvailableDiskSpace(dir)
	if err != nil {
		log.Printf("[WARNING] Could not check disk space for %s: %v", dir, err)
		// Don't fail the operation if we can't check disk space
		return nil
	}

	// Ensure we have at least MinDiskSpaceRequired plus the file size
	totalRequired := uint64(requiredBytes) + MinDiskSpaceRequired
	if available < totalRequired {
		log.Printf("[SECURITY] Disk quota check failed: available %d, required %d (file) + %d (reserve)",
			available, requiredBytes, MinDiskSpaceRequired)
		return &DiskQuotaError{
			Available: available,
			Required:  totalRequired,
			Path:      dir,
		}
	}

	return nil
}

// StreamingBase64Decoder provides streaming base64 decoding to prevent memory exhaustion
// CW-007: Use instead of loading entire base64 string into memory
type StreamingBase64Decoder struct {
	reader       io.Reader
	decodedSize  int64
	maxSize      int64
	bytesWritten int64
}

// NewStreamingBase64Decoder creates a new streaming decoder with size validation
func NewStreamingBase64Decoder(data string, maxSize int64) (*StreamingBase64Decoder, error) {
	// Estimate decoded size
	estimatedSize := int64(len(data) * 3 / 4)
	if estimatedSize > maxSize {
		return nil, &UploadSizeError{
			RequestedSize: estimatedSize,
			MaxSize:       maxSize,
		}
	}

	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(data))
	return &StreamingBase64Decoder{
		reader:      reader,
		decodedSize: estimatedSize,
		maxSize:     maxSize,
	}, nil
}

// WriteTo streams decoded data to a writer while enforcing size limits
func (d *StreamingBase64Decoder) WriteTo(w io.Writer) (int64, error) {
	buffer := make([]byte, StreamingChunkSize)
	var totalWritten int64

	for {
		n, readErr := d.reader.Read(buffer)
		if n > 0 {
			// Check if we would exceed max size
			if totalWritten+int64(n) > d.maxSize {
				log.Printf("[SECURITY] Streaming decode exceeded max size during write")
				return totalWritten, &UploadSizeError{
					RequestedSize: totalWritten + int64(n),
					MaxSize:       d.maxSize,
				}
			}

			written, writeErr := w.Write(buffer[:n])
			totalWritten += int64(written)

			if writeErr != nil {
				return totalWritten, writeErr
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return totalWritten, readErr
		}
	}

	d.bytesWritten = totalWritten
	return totalWritten, nil
}

// BytesWritten returns the total bytes written
func (d *StreamingBase64Decoder) BytesWritten() int64 {
	return d.bytesWritten
}

// WriteFileWithLimits writes base64-encoded data to a file with size limits and streaming
// CW-007: Replacement for WriteFile that implements all security measures
func WriteFileWithLimits(path string, data string, appendMode bool) error {
	// Step 1: Validate estimated size before any processing
	if err := ValidateBase64UploadSize(len(data)); err != nil {
		return fmt.Errorf("upload size validation failed: %w", err)
	}

	// Step 2: Calculate estimated decoded size for disk quota check
	estimatedSize := int64(len(data) * 3 / 4)

	// Step 3: Check disk quota
	if err := CheckDiskQuota(path, estimatedSize); err != nil {
		return fmt.Errorf("disk quota check failed: %w", err)
	}

	// Step 4: Create streaming decoder
	decoder, err := NewStreamingBase64Decoder(data, MaxUploadSize)
	if err != nil {
		return fmt.Errorf("failed to create decoder: %w", err)
	}

	// Step 5: Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Step 6: Open file with appropriate flags
	flags := os.O_WRONLY | os.O_CREATE
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	file, err := os.OpenFile(path, flags, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Step 7: Stream decode and write
	written, err := decoder.WriteTo(file)
	if err != nil {
		// Clean up partial write on error (only for new files)
		if !appendMode {
			os.Remove(path)
		}
		return fmt.Errorf("streaming write failed after %d bytes: %w", written, err)
	}

	log.Printf("[FILE TRANSFER] Successfully wrote %d bytes to %s", written, path)
	return nil
}
