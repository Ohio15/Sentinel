//go:build !windows

package filetransfer

import (
	"os"
	"path/filepath"
	"syscall"
)

// openFileNoFollow opens a file with O_NOFOLLOW so the kernel refuses to open
// the final path component if it is a symbolic link (AG-H write). This is the
// hard enforcement of the symlink TOCTOU guard on POSIX systems.
func openFileNoFollow(path string, flags int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flags|syscall.O_NOFOLLOW, perm)
}

// realPathFromHandle resolves the canonical real path of the opened file. On
// POSIX, intermediate-component symlinks are already resolved by the kernel at
// open time (and the final component is guarded by O_NOFOLLOW), so resolving the
// path with EvalSymlinks yields the same real path the handle refers to and is
// sufficient for the containment check.
func realPathFromHandle(f *os.File) (string, error) {
	return filepath.EvalSymlinks(f.Name())
}
