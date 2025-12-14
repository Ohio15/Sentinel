//go:build !windows

package filetransfer

import (
	"bufio"
	"os"
	"strings"
	"syscall"
)

// DriveInfo represents information about a drive/mount point
type DriveInfo struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Label      string `json:"label"`
	DriveType  string `json:"drive_type"`
	FileSystem string `json:"file_system"`
	TotalSize  uint64 `json:"total_size"`
	FreeSpace  uint64 `json:"free_space"`
	UsedSpace  uint64 `json:"used_space"`
}

// ListDrives returns a list of mounted filesystems on Unix-like systems
func (ft *FileTransfer) ListDrives() ([]DriveInfo, error) {
	var drives []DriveInfo

	// Read /proc/mounts to get mounted filesystems
	file, err := os.Open("/proc/mounts")
	if err != nil {
		// Fallback: just return root
		drives = append(drives, DriveInfo{
			Name:      "/",
			Path:      "/",
			DriveType: "Fixed",
		})
		return drives, nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	seen := make(map[string]bool)

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		mountPoint := fields[1]
		fsType := fields[2]

		// Skip virtual filesystems
		if strings.HasPrefix(fsType, "sys") ||
			strings.HasPrefix(fsType, "proc") ||
			strings.HasPrefix(fsType, "dev") ||
			strings.HasPrefix(fsType, "run") ||
			strings.HasPrefix(fsType, "cgroup") ||
			fsType == "tmpfs" ||
			fsType == "overlay" ||
			fsType == "squashfs" {
			continue
		}

		// Skip if already seen
		if seen[mountPoint] {
			continue
		}
		seen[mountPoint] = true

		driveInfo := DriveInfo{
			Name:       mountPoint,
			Path:       mountPoint,
			FileSystem: fsType,
			DriveType:  "Fixed",
		}

		// Get disk space using statfs
		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountPoint, &stat); err == nil {
			driveInfo.TotalSize = stat.Blocks * uint64(stat.Bsize)
			driveInfo.FreeSpace = stat.Bfree * uint64(stat.Bsize)
			driveInfo.UsedSpace = driveInfo.TotalSize - driveInfo.FreeSpace
		}

		drives = append(drives, driveInfo)
	}

	// If no drives found, add root
	if len(drives) == 0 {
		drives = append(drives, DriveInfo{
			Name:      "/",
			Path:      "/",
			DriveType: "Fixed",
		})
	}

	return drives, nil
}
