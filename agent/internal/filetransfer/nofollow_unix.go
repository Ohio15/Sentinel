//go:build !windows

package filetransfer

import (
	"os"
	"syscall"
)

// openFileNoFollow opens a file with O_NOFOLLOW so the kernel refuses to open
// the final path component if it is a symbolic link (AG-H write). This is the
// hard enforcement of the symlink TOCTOU guard on POSIX systems.
func openFileNoFollow(path string, flags int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flags|syscall.O_NOFOLLOW, perm)
}
