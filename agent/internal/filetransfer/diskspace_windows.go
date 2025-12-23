//go:build windows

package filetransfer

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

// getAvailableDiskSpace returns available disk space in bytes for a path (Windows implementation)
func getAvailableDiskSpace(path string) (uint64, error) {
	// Get the drive letter
	absPath, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}

	// Extract drive letter (e.g., "C:" from "C:\Users\...")
	if len(absPath) < 2 || absPath[1] != ':' {
		return 0, fmt.Errorf("invalid Windows path: %s", absPath)
	}
	drive := absPath[:2] + "\\"

	var freeBytesAvailable uint64
	var totalBytes uint64
	var totalFreeBytes uint64

	// Use syscall to call GetDiskFreeSpaceExW
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	drivePtr, err := syscall.UTF16PtrFromString(drive)
	if err != nil {
		return 0, err
	}

	ret, _, callErr := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(drivePtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)

	if ret == 0 {
		return 0, fmt.Errorf("GetDiskFreeSpaceExW failed: %v", callErr)
	}

	return freeBytesAvailable, nil
}
