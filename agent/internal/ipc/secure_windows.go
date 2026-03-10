//go:build windows

package ipc

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// fileSecurityDescriptor restricts file access to SYSTEM and Administrators only.
// D: = DACL
// (A;;FA;;;SY) = Allow File All Access to SYSTEM
// (A;;FA;;;BA) = Allow File All Access to Builtin Administrators
const fileSecurityDescriptor = "D:P(A;;FA;;;SY)(A;;FA;;;BA)"

// dirSecurityDescriptor restricts directory access to SYSTEM and Administrators only,
// with inheritance enabled for child objects.
// (A;OICI;FA;;;SY) = Allow File All Access to SYSTEM, inherit to child objects and containers
// (A;OICI;FA;;;BA) = Allow File All Access to Builtin Administrators, inherit to child objects and containers
const dirSecurityDescriptor = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"

// setFileACL applies a restrictive Windows DACL to the given file path,
// allowing access only to SYSTEM and Builtin Administrators.
func setFileACL(path string) error {
	sd, err := windows.SecurityDescriptorFromString(fileSecurityDescriptor)
	if err != nil {
		return fmt.Errorf("failed to parse file security descriptor: %w", err)
	}

	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("failed to get file DACL: %w", err)
	}

	err = windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	)
	if err != nil {
		return fmt.Errorf("failed to set file ACL on %s: %w", path, err)
	}

	return nil
}

// setDirectoryACL applies a restrictive Windows DACL to the given directory,
// allowing access only to SYSTEM and Builtin Administrators, with inheritance.
func setDirectoryACL(path string) error {
	sd, err := windows.SecurityDescriptorFromString(dirSecurityDescriptor)
	if err != nil {
		return fmt.Errorf("failed to parse directory security descriptor: %w", err)
	}

	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("failed to get directory DACL: %w", err)
	}

	err = windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	)
	if err != nil {
		return fmt.Errorf("failed to set directory ACL on %s: %w", path, err)
	}

	return nil
}

// secureWriteFile writes data to a file with restrictive Unix permissions and Windows ACLs.
func secureWriteFile(path string, data []byte, perm os.FileMode) error {
	if err := os.WriteFile(path, data, perm); err != nil {
		return err
	}
	// Apply Windows DACL - log but don't fail if ACL can't be set (e.g., running as non-admin during dev)
	if err := setFileACL(path); err != nil {
		// Log the error but still return nil since the file was written successfully
		// The 0600 Unix permission is still applied as a baseline
		fmt.Printf("Warning: could not set Windows ACL on %s: %v\n", path, err)
	}
	return nil
}

// secureDirectory applies restrictive Windows ACLs to a directory.
func secureDirectory(path string) {
	if err := setDirectoryACL(path); err != nil {
		fmt.Printf("Warning: could not set Windows ACL on directory %s: %v\n", path, err)
	}
}
