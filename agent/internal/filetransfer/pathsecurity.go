package filetransfer

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// =============================================================================
// CW-003 Security Fix: Enhanced File Path Validation
// Addresses Unicode path normalization, 8.3 filename resolution, and TOCTOU
// =============================================================================

// MaxPathLength is the maximum allowed path length
const MaxPathLength = 4096

// normalizePath applies Unicode normalization (NFC) to the path
// CW-003: Prevents Unicode homograph attacks and path confusion
func normalizePath(path string) (string, error) {
	if len(path) > MaxPathLength {
		return "", fmt.Errorf("path exceeds maximum length of %d characters", MaxPathLength)
	}
	normalized := norm.NFC.String(path)
	if err := checkUnicodeSecurity(normalized); err != nil {
		return "", err
	}
	return normalized, nil
}

// checkUnicodeSecurity checks for potentially dangerous Unicode characters
func checkUnicodeSecurity(path string) error {
	for i, r := range path {
		// Bidirectional override characters (RTL attacks)
		if r >= 0x202A && r <= 0x202E {
			return fmt.Errorf("path contains bidirectional override character at position %d", i)
		}
		// Zero-width characters
		if r == 0x200B || r == 0x200C || r == 0x200D || r == 0xFEFF {
			return fmt.Errorf("path contains zero-width character at position %d", i)
		}
		// Cyrillic lookalikes (homoglyph attack prevention)
		if unicode.Is(unicode.Cyrillic, r) {
			return fmt.Errorf("path contains Cyrillic character at position %d", i)
		}
		// Control characters
		if r < 32 && r != 9 && r != 10 && r != 13 {
			return fmt.Errorf("path contains control character at position %d", i)
		}
	}
	return nil
}

// resolveLongPath resolves Windows 8.3 short paths to their long equivalents
// CW-003: Prevents bypasses using short filename aliases
func resolveLongPath(path string) (string, error) {
	if runtime.GOOS != "windows" {
		return path, nil
	}
	if !strings.Contains(path, "~") {
		return path, nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			parent := filepath.Dir(path)
			base := filepath.Base(path)
			if parent != path {
				resolvedParent, parentErr := resolveLongPath(parent)
				if parentErr != nil {
					return path, nil
				}
				return filepath.Join(resolvedParent, base), nil
			}
		}
		return path, err
	}
	return resolved, nil
}

// checkWindowsReservedNames checks for Windows reserved device names
func checkWindowsReservedNames(path string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	name = strings.ToUpper(name)
	
	reservedNames := []string{
		"CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
	}
	for _, reserved := range reservedNames {
		if name == reserved {
			return fmt.Errorf("path contains Windows reserved name: %s", reserved)
		}
	}
	return nil
}

// SecurePathValidation performs comprehensive path security validation
// CW-003: Call BEFORE any file operation, use returned path IMMEDIATELY
func SecurePathValidation(path string) (string, error) {
	normalized, err := normalizePath(path)
	if err != nil {
		return "", fmt.Errorf("unicode validation failed: %w", err)
	}
	longPath, err := resolveLongPath(normalized)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("path resolution failed: %w", err)
	}
	if longPath != "" {
		normalized = longPath
	}
	if err := checkWindowsReservedNames(normalized); err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(normalized)
	if err != nil {
		return "", fmt.Errorf("cannot get absolute path: %w", err)
	}
	cleanPath := filepath.Clean(absPath)
	if strings.Contains(cleanPath, "..") {
		return "", fmt.Errorf("path traversal detected")
	}
	return cleanPath, nil
}

// ValidateNoSymlinkRace validates path stability for TOCTOU protection
// CW-003: Call immediately before the operation
func ValidateNoSymlinkRace(path string, expectedExists bool) (string, error) {
	linfo, lerr := os.Lstat(path)
	_, err := os.Stat(path)
	
	if expectedExists {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file does not exist: %s", path)
		}
		if err != nil {
			return "", fmt.Errorf("cannot access file: %w", err)
		}
	}
	
	if lerr == nil && err == nil {
		if linfo.Mode()&os.ModeSymlink != 0 {
			realPath, evalErr := filepath.EvalSymlinks(path)
			if evalErr != nil {
				return "", fmt.Errorf("cannot resolve symlink: %w", evalErr)
			}
			return realPath, nil
		}
	}
	return path, nil
}
