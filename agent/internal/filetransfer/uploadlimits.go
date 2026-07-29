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

// WriteFileWithLimits writes base64-encoded data to a file with size limits and
// streaming. CW-007. No base containment is enforced by this entry point.
func WriteFileWithLimits(path string, data string, appendMode bool) error {
	return WriteFileWithLimitsBounded(path, data, appendMode, nil, nil)
}

// WriteFileWithLimitsBounded is WriteFileWithLimits with real-path containment:
// after the handle is opened, the resolved real path of the open file must be
// inside one of allowedBases (when non-empty) and outside every deniedBases
// entry. This closes the Windows intermediate-junction TOCTOU (AG-H write) where
// an attacker-planted junction on a parent directory redirects the write even
// though the final component check passes.
func WriteFileWithLimitsBounded(path string, data string, appendMode bool, allowedBases, deniedBases []string) error {
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

	// Step 6: Open the target with the full symlink/junction TOCTOU guard.
	file, err := OpenFileHardened(path, appendMode, allowedBases, deniedBases)
	if err != nil {
		return err
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

// OpenFileHardened opens path for writing with the complete symlink/junction
// TOCTOU guard (AG-H write, AG-M path):
//   - pre-open rejection of an existing symlink/reparse target;
//   - no-follow open (O_NOFOLLOW on POSIX);
//   - post-open handle-identity verification (regular file, SameFile);
//   - real-path containment: the resolved real path of the open handle must be
//     inside allowedBases (when provided) and outside every deniedBases entry,
//     defeating an attacker-planted junction on an intermediate directory.
//
// The returned file is positioned per appendMode (append vs truncate) and the
// caller owns closing it.
func OpenFileHardened(path string, appendMode bool, allowedBases, deniedBases []string) (*os.File, error) {
	// Pre-open symlink/reparse-point rejection. Refuse to write when the target
	// is already a symlink/junction so we never truncate or overwrite through
	// one; short-circuits before O_TRUNC touches any target.
	if li, lerr := os.Lstat(path); lerr == nil {
		if li.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("refusing to write through symlink/reparse point: %s", path)
		}
	} else if !os.IsNotExist(lerr) {
		return nil, fmt.Errorf("failed to stat target: %w", lerr)
	}

	flags := os.O_WRONLY | os.O_CREATE
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	file, err := openFileNoFollow(path, flags, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	if err := verifyOpenedRegularFile(file, path); err != nil {
		file.Close()
		if !appendMode {
			os.Remove(path)
		}
		return nil, err
	}

	// Real-path containment via the open handle (GetFinalPathNameByHandle on
	// Windows) — catches intermediate-junction redirection that the final
	// component check cannot see.
	realPath, rerr := realPathFromHandle(file)
	if rerr != nil || realPath == "" {
		// Fail CLOSED: if we cannot resolve the open handle's real path we cannot
		// prove containment, so we must not hand back a possibly-escaped handle.
		file.Close()
		if !appendMode {
			os.Remove(path)
		}
		if rerr == nil {
			rerr = fmt.Errorf("empty real path")
		}
		return nil, fmt.Errorf("refusing write to %s: could not resolve real path for containment check: %w", path, rerr)
	}
	if cerr := verifyRealPathContainment(realPath, allowedBases, deniedBases); cerr != nil {
		file.Close()
		if !appendMode {
			os.Remove(path)
		}
		return nil, cerr
	}

	return file, nil
}

// verifyRealPathContainment enforces that realPath is inside an allowed base
// (when allowedBases is non-empty) and outside every denied base.
func verifyRealPathContainment(realPath string, allowedBases, deniedBases []string) error {
	// Canonicalize the candidate AND every base the same way (resolve symlinks
	// and 8.3 short names) so comparisons are consistent regardless of how each
	// side is spelled — otherwise denied bases silently miss and legitimate
	// allowed paths falsely "escape".
	clean := resolveForCompare(realPath)

	for _, denied := range deniedBases {
		if denied == "" {
			continue
		}
		if isSubPath(clean, resolveForCompare(denied)) {
			return fmt.Errorf("resolved real path is within a protected directory: %s", clean)
		}
	}

	if len(allowedBases) > 0 {
		allowed := false
		for _, base := range allowedBases {
			if base == "" {
				continue
			}
			if isSubPath(clean, resolveForCompare(base)) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("resolved real path escaped allowed directories: %s", clean)
		}
	}
	return nil
}

// verifyOpenedRegularFile confirms the opened file descriptor refers to a
// regular file that is the same filesystem object as the path resolves via
// Lstat. This closes the symlink/junction TOCTOU window (AG-H write): if an
// attacker swapped in a symlink after path validation, the Lstat mode or the
// SameFile identity comparison will detect it.
func verifyOpenedRegularFile(f *os.File, path string) error {
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat opened file: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("refusing to write non-regular file: %s", path)
	}

	li, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("failed to lstat target after open: %w", err)
	}
	if li.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("target became a symlink/reparse point after open: %s", path)
	}
	if !os.SameFile(fi, li) {
		return fmt.Errorf("target identity changed between validation and open (possible TOCTOU): %s", path)
	}
	return nil
}
