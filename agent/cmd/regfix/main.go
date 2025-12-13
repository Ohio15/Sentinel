//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	advapi32                     = windows.NewLazySystemDLL("advapi32.dll")
	procSetNamedSecurityInfoW    = advapi32.NewProc("SetNamedSecurityInfoW")
	procSetEntriesInAclW         = advapi32.NewProc("SetEntriesInAclW")
)

const (
	SE_REGISTRY_KEY         = 4
	DACL_SECURITY_INFORMATION = 0x00000004
	GRANT_ACCESS            = 1
	SET_ACCESS              = 2
	NO_INHERITANCE          = 0
	SUB_CONTAINERS_AND_OBJECTS_INHERIT = 3
)

type EXPLICIT_ACCESS struct {
	AccessPermissions uint32
	AccessMode        uint32
	Inheritance       uint32
	Trustee           TRUSTEE
}

type TRUSTEE struct {
	MultipleTrustee          *TRUSTEE
	MultipleTrusteeOperation uint32
	TrusteeForm              uint32
	TrusteeType              uint32
	TrusteeName              *uint16
}

func enablePrivilege(name string) error {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token)
	if err != nil {
		return err
	}
	defer token.Close()

	var luid windows.LUID
	err = windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr(name), &luid)
	if err != nil {
		return err
	}

	tp := windows.Tokenprivileges{
		PrivilegeCount: 1,
		Privileges: [1]windows.LUIDAndAttributes{
			{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED},
		},
	}

	return windows.AdjustTokenPrivileges(token, false, &tp, 0, nil, nil)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: regfix <keypath>")
		fmt.Println("Example: regfix SYSTEM\\CurrentControlSet\\Services\\SentinelAgent")
		os.Exit(1)
	}

	keyPath := os.Args[1]
	fullPath := "MACHINE\\" + keyPath
	log.Printf("Attempting to fix ACL for: HKLM\\%s", keyPath)

	// Enable required privileges
	privileges := []string{
		"SeRestorePrivilege",
		"SeBackupPrivilege",
		"SeTakeOwnershipPrivilege",
		"SeSecurityPrivilege",
	}

	for _, priv := range privileges {
		if err := enablePrivilege(priv); err != nil {
			log.Printf("Warning: Failed to enable %s: %v", priv, err)
		} else {
			log.Printf("Enabled privilege: %s", priv)
		}
	}

	// Try using SetNamedSecurityInfo to reset the DACL
	pathPtr, _ := windows.UTF16PtrFromString(fullPath)

	// Create Administrators SID
	adminSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		log.Fatalf("Failed to create Admin SID: %v", err)
	}

	// First, try to take ownership
	log.Println("Taking ownership...")
	ret, _, err := procSetNamedSecurityInfoW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(SE_REGISTRY_KEY),
		uintptr(windows.OWNER_SECURITY_INFORMATION),
		uintptr(unsafe.Pointer(adminSID)),
		0,
		0,
		0,
	)
	if ret != 0 {
		log.Printf("Warning: SetNamedSecurityInfo (owner) returned %d: %v", ret, err)
	} else {
		log.Println("Ownership taken successfully")
	}

	// Now set a new DACL that allows SYSTEM and Administrators full control
	log.Println("Setting new DACL...")

	// Create a new security descriptor string
	sdStr := "D:(A;OICI;KA;;;SY)(A;OICI;KA;;;BA)"
	sd, err := windows.SecurityDescriptorFromString(sdStr)
	if err != nil {
		log.Fatalf("Failed to create SD: %v", err)
	}

	dacl, _, err := sd.DACL()
	if err != nil {
		log.Fatalf("Failed to get DACL: %v", err)
	}

	// Set the DACL using SetNamedSecurityInfo
	ret, _, err = procSetNamedSecurityInfoW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(SE_REGISTRY_KEY),
		uintptr(DACL_SECURITY_INFORMATION),
		0,
		0,
		uintptr(unsafe.Pointer(dacl)),
		0,
	)
	if ret != 0 {
		log.Printf("SetNamedSecurityInfo (DACL) returned %d: %v", ret, err)

		// Try alternative: open with backup privilege and modify
		log.Println("Trying alternative method with registry API...")

		key, err := registry.OpenKey(
			registry.LOCAL_MACHINE,
			keyPath,
			registry.SET_VALUE|0x00040000, // WRITE_DAC
		)
		if err != nil {
			log.Printf("Failed to open key: %v", err)
		} else {
			defer key.Close()

			// Try to set Start value directly
			err = key.SetDWordValue("Start", 2)
			if err != nil {
				log.Printf("Failed to set Start: %v", err)
			} else {
				log.Println("Start value set to 2 (AUTO_START)")
			}
		}
	} else {
		log.Println("DACL set successfully")

		// Now set the Start value
		key, err := registry.OpenKey(
			registry.LOCAL_MACHINE,
			keyPath,
			registry.SET_VALUE,
		)
		if err != nil {
			log.Printf("Failed to open key for SET_VALUE: %v", err)
		} else {
			defer key.Close()
			err = key.SetDWordValue("Start", 2)
			if err != nil {
				log.Printf("Failed to set Start: %v", err)
			} else {
				log.Println("Start value set to 2 (AUTO_START)")
			}
		}
	}

	// Verify final state
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, keyPath, registry.READ)
	if err != nil {
		log.Printf("Cannot verify: %v", err)
		return
	}
	defer key.Close()

	start, _, err := key.GetIntegerValue("Start")
	if err != nil {
		log.Printf("Cannot read Start: %v", err)
	} else {
		log.Printf("Final Start value: %d", start)
	}
}
