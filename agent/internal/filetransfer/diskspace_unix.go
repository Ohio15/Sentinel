//go:build !windows

package filetransfer

import (
	"syscall"
)

// getAvailableDiskSpace returns available disk space in bytes for a path (Unix implementation)
func getAvailableDiskSpace(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	// Available blocks * block size
	return stat.Bavail * uint64(stat.Bsize), nil
}
