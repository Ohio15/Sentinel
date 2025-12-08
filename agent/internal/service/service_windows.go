//go:build windows

package service

import (
	"golang.org/x/sys/windows"
)

// isWindowsAdmin checks if the current process has administrator privileges
func isWindowsAdmin() bool {
	var sid *windows.SID

	// Although this looks scary, it is directly copied from the
	// temporary fix in https://github.com/golang/go/issues/28804#issuecomment-438838144
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}

	return member
}
