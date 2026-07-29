//go:build windows

package filetransfer

import "os"

// openFileNoFollow opens a file on Windows, where the POSIX O_NOFOLLOW flag is
// unavailable. The symlink/junction TOCTOU guard is enforced by the pre-open
// Lstat rejection and the post-open verifyOpenedRegularFile identity check in
// WriteFileWithLimits rather than at the syscall (AG-H write).
func openFileNoFollow(path string, flags int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flags, perm)
}
