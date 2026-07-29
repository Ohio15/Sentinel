//go:build windows

package filetransfer

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// openFileNoFollow opens a file on Windows, where the POSIX O_NOFOLLOW flag is
// unavailable. The symlink/junction TOCTOU guard is enforced by the pre-open
// Lstat rejection, the post-open verifyOpenedRegularFile identity check, and the
// GetFinalPathNameByHandle real-path containment check in OpenFileHardened
// rather than at the syscall (AG-H write).
func openFileNoFollow(path string, flags int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flags, perm)
}

// realPathFromHandle resolves the canonical real path of an open file handle via
// GetFinalPathNameByHandle. Because it resolves the handle (not the input path),
// it sees through junctions/symlinks planted on ANY path component, which is
// what closes the Windows intermediate-junction TOCTOU (AG-H write).
func realPathFromHandle(f *os.File) (string, error) {
	h := windows.Handle(f.Fd())
	// FILE_NAME_NORMALIZED (0x0) | VOLUME_NAME_DOS (0x0): return the normalized,
	// drive-letter form. These flag values are not exported by this x/sys
	// version, so they are defined inline.
	const flags = 0x0

	n, err := windows.GetFinalPathNameByHandle(h, nil, 0, flags)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, n)
	n, err = windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), flags)
	if err != nil {
		return "", err
	}
	p := windows.UTF16ToString(buf[:n])
	// Strip the extended-length prefixes GetFinalPathNameByHandle returns.
	p = strings.TrimPrefix(p, `\\?\UNC\`)
	p = strings.TrimPrefix(p, `\\?\`)
	return p, nil
}
