//go:build windows

package ipc

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// Well-known SIDs permitted to own protected identity/secret files.
const (
	sidLocalSystem    = "S-1-5-18"     // NT AUTHORITY\SYSTEM
	sidAdministrators = "S-1-5-32-544" // BUILTIN\Administrators
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
	if TestDisableACL {
		return nil
	}
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
	if TestDisableACL {
		return nil
	}
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

// SecureWriteFileStrict writes data to a file with a SYSTEM+Administrators-only
// DACL and FAILS if the DACL cannot be applied. Unlike secureWriteFile it does
// NOT log-and-continue: a secret written without its protective ACL inherits
// the world-readable ProgramData ACL, so if the DACL cannot be set the file is
// removed and an error is returned. Use this for all identity/secret files
// (mTLS client key, client cert, config, kill-token, machine key material).
func SecureWriteFileStrict(path string, data []byte, perm os.FileMode) error {
	if err := os.WriteFile(path, data, perm); err != nil {
		return err
	}
	if err := setFileACL(path); err != nil {
		// Do not leave a secret behind with an inherited (world-readable) ACL.
		_ = os.Remove(path)
		return fmt.Errorf("refusing to persist %s without protective ACL: %w", path, err)
	}
	return nil
}

// SecureFileStrict applies the SYSTEM+Administrators-only DACL to an existing
// file and returns an error if it cannot be applied. Used when a file was
// written atomically elsewhere (e.g. rename-into-place) and needs its ACL
// sealed before it is trusted.
func SecureFileStrict(path string) error {
	return setFileACL(path)
}

// SecureDirStrict applies the SYSTEM+Administrators-only DACL to an existing
// directory and returns an error if it cannot be applied.
func SecureDirStrict(path string) error {
	return setDirectoryACL(path)
}

// EnsureSecureDir creates the directory tree (if needed) and applies the
// SYSTEM+Administrators-only DACL, failing if the DACL cannot be set.
func EnsureSecureDir(path string, perm os.FileMode) error {
	if err := os.MkdirAll(path, perm); err != nil {
		return err
	}
	if err := setDirectoryACL(path); err != nil {
		return fmt.Errorf("failed to secure directory %s: %w", path, err)
	}
	return nil
}

// VerifyFileSecurity confirms that a file is owned by SYSTEM or Administrators
// and carries the expected protected (SYSTEM+Administrators-only) DACL. It is
// used to detect a pre-planted / attacker-controlled file (e.g. the IPC HMAC
// key) before its contents are trusted. Returns an error describing the first
// discrepancy found.
func VerifyFileSecurity(path string) error {
	if TestDisableACL {
		return nil
	}
	sd, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("failed to read security info for %s: %w", path, err)
	}

	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("failed to read owner of %s: %w", path, err)
	}
	ownerSID := owner.String()
	if ownerSID != sidLocalSystem && ownerSID != sidAdministrators {
		return fmt.Errorf("untrusted owner %s on %s (expected SYSTEM or Administrators)", ownerSID, path)
	}

	sddl := sd.String()
	dacl := extractDACL(sddl)
	if !strings.HasPrefix(dacl, "P") {
		return fmt.Errorf("DACL on %s is not protected (inheritance enabled): %q", path, dacl)
	}
	// Exactly two full-access ACEs, one for SYSTEM and one for Administrators,
	// and nothing else.
	if strings.Count(dacl, "(") != 2 ||
		!strings.Contains(dacl, "(A;;FA;;;SY)") ||
		!strings.Contains(dacl, "(A;;FA;;;BA)") {
		return fmt.Errorf("unexpected DACL on %s: %q (expected SYSTEM+Administrators full access only)", path, dacl)
	}
	return nil
}

// extractDACL returns the "D:"-prefixed DACL portion of an SDDL string,
// stripping the "D:" tag itself. It stops at the next section tag (S: SACL)
// if present. Returns an empty string if no DACL section exists.
func extractDACL(sddl string) string {
	idx := strings.Index(sddl, "D:")
	if idx < 0 {
		return ""
	}
	rest := sddl[idx+2:]
	// A SACL section ("S:") would follow the DACL; cut it off if present.
	if s := strings.Index(rest, "S:"); s >= 0 {
		rest = rest[:s]
	}
	return rest
}
