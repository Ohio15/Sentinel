//go:build windows

package filetransfer

import (
	"fmt"
	"syscall"
	"unsafe"
)

// DriveInfo represents information about a drive
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

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	getLogicalDrives     = kernel32.NewProc("GetLogicalDrives")
	getDriveTypeW        = kernel32.NewProc("GetDriveTypeW")
	getVolumeInformationW = kernel32.NewProc("GetVolumeInformationW")
	getDiskFreeSpaceExW  = kernel32.NewProc("GetDiskFreeSpaceExW")
)

const (
	DRIVE_UNKNOWN     = 0
	DRIVE_NO_ROOT_DIR = 1
	DRIVE_REMOVABLE   = 2
	DRIVE_FIXED       = 3
	DRIVE_REMOTE      = 4
	DRIVE_CDROM       = 5
	DRIVE_RAMDISK     = 6
)

// ListDrives returns a list of available drives on Windows
func (ft *FileTransfer) ListDrives() ([]DriveInfo, error) {
	var drives []DriveInfo

	// Get bitmask of available drives
	ret, _, _ := getLogicalDrives.Call()
	if ret == 0 {
		return nil, fmt.Errorf("failed to get logical drives")
	}

	bitmask := uint32(ret)

	for i := 0; i < 26; i++ {
		if bitmask&(1<<i) != 0 {
			driveLetter := string(rune('A' + i))
			drivePath := driveLetter + ":\\"

			driveInfo := DriveInfo{
				Name: driveLetter + ":",
				Path: drivePath,
			}

			// Get drive type
			drivePathPtr, _ := syscall.UTF16PtrFromString(drivePath)
			driveType, _, _ := getDriveTypeW.Call(uintptr(unsafe.Pointer(drivePathPtr)))

			switch driveType {
			case DRIVE_REMOVABLE:
				driveInfo.DriveType = "Removable"
			case DRIVE_FIXED:
				driveInfo.DriveType = "Fixed"
			case DRIVE_REMOTE:
				driveInfo.DriveType = "Network"
			case DRIVE_CDROM:
				driveInfo.DriveType = "CD-ROM"
			case DRIVE_RAMDISK:
				driveInfo.DriveType = "RAM Disk"
			default:
				driveInfo.DriveType = "Unknown"
			}

			// Get volume information
			volumeNameBuffer := make([]uint16, 256)
			fileSystemBuffer := make([]uint16, 256)
			var serialNumber, maxComponentLen, fileSystemFlags uint32

			ret, _, _ := getVolumeInformationW.Call(
				uintptr(unsafe.Pointer(drivePathPtr)),
				uintptr(unsafe.Pointer(&volumeNameBuffer[0])),
				uintptr(len(volumeNameBuffer)),
				uintptr(unsafe.Pointer(&serialNumber)),
				uintptr(unsafe.Pointer(&maxComponentLen)),
				uintptr(unsafe.Pointer(&fileSystemFlags)),
				uintptr(unsafe.Pointer(&fileSystemBuffer[0])),
				uintptr(len(fileSystemBuffer)),
			)

			if ret != 0 {
				driveInfo.Label = syscall.UTF16ToString(volumeNameBuffer)
				driveInfo.FileSystem = syscall.UTF16ToString(fileSystemBuffer)
			}

			// Get disk space
			var freeBytesAvailable, totalBytes, totalFreeBytes uint64
			ret, _, _ = getDiskFreeSpaceExW.Call(
				uintptr(unsafe.Pointer(drivePathPtr)),
				uintptr(unsafe.Pointer(&freeBytesAvailable)),
				uintptr(unsafe.Pointer(&totalBytes)),
				uintptr(unsafe.Pointer(&totalFreeBytes)),
			)

			if ret != 0 {
				driveInfo.TotalSize = totalBytes
				driveInfo.FreeSpace = totalFreeBytes
				driveInfo.UsedSpace = totalBytes - totalFreeBytes
			}

			drives = append(drives, driveInfo)
		}
	}

	return drives, nil
}
