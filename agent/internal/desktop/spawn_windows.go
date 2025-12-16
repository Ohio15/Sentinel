// +build windows

package desktop

import (
	"fmt"
	"log"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modAdvapi32 = windows.NewLazySystemDLL("advapi32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")
	modUserenv  = windows.NewLazySystemDLL("userenv.dll")
	modWtsapi32 = windows.NewLazySystemDLL("wtsapi32.dll")

	procCreateProcessAsUserW    = modAdvapi32.NewProc("CreateProcessAsUserW")
	procWTSQueryUserToken       = modWtsapi32.NewProc("WTSQueryUserToken")
	procCreateEnvironmentBlock  = modUserenv.NewProc("CreateEnvironmentBlock")
	procDestroyEnvironmentBlock = modUserenv.NewProc("DestroyEnvironmentBlock")
	procDuplicateTokenEx        = modAdvapi32.NewProc("DuplicateTokenEx")
	procGetTokenInformation     = modAdvapi32.NewProc("GetTokenInformation")
	procLookupPrivilegeValueW   = modAdvapi32.NewProc("LookupPrivilegeValueW")
	procAdjustTokenPrivileges   = modAdvapi32.NewProc("AdjustTokenPrivileges")
)

// Token security levels
const (
	SecurityAnonymous      = 0
	SecurityIdentification = 1
	SecurityImpersonation  = 2
	SecurityDelegation     = 3
)

// Token types
const (
	TokenPrimary       = 1
	TokenImpersonation = 2
)

// Token information classes
const (
	TokenUser      = 1
	TokenSessionId = 12
)

// CREATE_PROCESS flags
const (
	CREATE_UNICODE_ENVIRONMENT = 0x00000400
	CREATE_NEW_CONSOLE         = 0x00000010
	CREATE_NO_WINDOW           = 0x08000000
)

// Privilege constants
const (
	SE_PRIVILEGE_ENABLED = 0x00000002
)

// Privilege names required for CreateProcessAsUser
const (
	SE_ASSIGNPRIMARYTOKEN_NAME = "SeAssignPrimaryTokenPrivilege"
	SE_INCREASE_QUOTA_NAME     = "SeIncreaseQuotaPrivilege"
	SE_TCB_NAME                = "SeTcbPrivilege"
)

// LUID structure for privilege lookup
type LUID struct {
	LowPart  uint32
	HighPart int32
}

// LUID_AND_ATTRIBUTES for token privileges
type LUID_AND_ATTRIBUTES struct {
	Luid       LUID
	Attributes uint32
}

// TOKEN_PRIVILEGES structure
type TOKEN_PRIVILEGES struct {
	PrivilegeCount uint32
	Privileges     [1]LUID_AND_ATTRIBUTES
}

// enablePrivilege enables a named privilege on the current process token
func enablePrivilege(privilegeName string) error {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token)
	if err != nil {
		return fmt.Errorf("OpenProcessToken failed: %w", err)
	}
	defer token.Close()

	privNamePtr, err := syscall.UTF16PtrFromString(privilegeName)
	if err != nil {
		return fmt.Errorf("failed to convert privilege name: %w", err)
	}

	var luid LUID
	ret, _, err := procLookupPrivilegeValueW.Call(
		0,
		uintptr(unsafe.Pointer(privNamePtr)),
		uintptr(unsafe.Pointer(&luid)),
	)
	if ret == 0 {
		return fmt.Errorf("LookupPrivilegeValue failed for %s: %w", privilegeName, err)
	}

	var tp TOKEN_PRIVILEGES
	tp.PrivilegeCount = 1
	tp.Privileges[0].Luid = luid
	tp.Privileges[0].Attributes = SE_PRIVILEGE_ENABLED

	ret, _, err = procAdjustTokenPrivileges.Call(
		uintptr(token),
		0,
		uintptr(unsafe.Pointer(&tp)),
		0,
		0,
		0,
	)
	if ret == 0 {
		return fmt.Errorf("AdjustTokenPrivileges failed for %s: %w", privilegeName, err)
	}

	lastErr := windows.GetLastError()
	if lastErr == windows.ERROR_NOT_ALL_ASSIGNED {
		return fmt.Errorf("privilege %s not held by process", privilegeName)
	}

	log.Printf("[Spawn] Enabled privilege: %s", privilegeName)
	return nil
}

// enableRequiredPrivileges enables all privileges needed for CreateProcessAsUser
func enableRequiredPrivileges() error {
	privileges := []string{
		SE_ASSIGNPRIMARYTOKEN_NAME,
		SE_INCREASE_QUOTA_NAME,
		SE_TCB_NAME,
	}

	for _, priv := range privileges {
		if err := enablePrivilege(priv); err != nil {
			log.Printf("[Spawn] Warning: could not enable %s: %v", priv, err)
		}
	}

	return nil
}

// SpawnInSession spawns a process in the specified Windows session
func SpawnInSession(sessionID uint32, exePath string, args []string) (*os.Process, error) {
	log.Printf("[Spawn] SpawnInSession called: sessionID=%d, exePath=%s", sessionID, exePath)

	// Enable required privileges first
	if err := enableRequiredPrivileges(); err != nil {
		log.Printf("[Spawn] Warning: enableRequiredPrivileges: %v", err)
	}

	var userToken windows.Token
	ret, _, err := procWTSQueryUserToken.Call(
		uintptr(sessionID),
		uintptr(unsafe.Pointer(&userToken)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("WTSQueryUserToken failed: %w", err)
	}
	defer userToken.Close()

	log.Printf("[Spawn] Got user token for session %d", sessionID)

	var duplicatedToken windows.Token
	ret, _, err = procDuplicateTokenEx.Call(
		uintptr(userToken),
		uintptr(windows.TOKEN_ALL_ACCESS),
		0,
		uintptr(SecurityImpersonation),
		uintptr(TokenPrimary),
		uintptr(unsafe.Pointer(&duplicatedToken)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("DuplicateTokenEx failed: %w", err)
	}
	defer duplicatedToken.Close()

	log.Printf("[Spawn] Duplicated token")

	var envBlock unsafe.Pointer
	ret, _, err = procCreateEnvironmentBlock.Call(
		uintptr(unsafe.Pointer(&envBlock)),
		uintptr(duplicatedToken),
		0,
	)
	if ret == 0 {
		return nil, fmt.Errorf("CreateEnvironmentBlock failed: %w", err)
	}
	defer procDestroyEnvironmentBlock.Call(uintptr(envBlock))

	log.Printf("[Spawn] Created environment block")

	cmdLine := buildCommandLine(exePath, args)
	cmdLinePtr, err := syscall.UTF16PtrFromString(cmdLine)
	if err != nil {
		return nil, fmt.Errorf("failed to convert command line: %w", err)
	}

	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Desktop, _ = syscall.UTF16PtrFromString("winsta0\\default")

	var pi windows.ProcessInformation

	log.Printf("[Spawn] Calling CreateProcessAsUserW: %s", cmdLine)

	ret, _, err = procCreateProcessAsUserW.Call(
		uintptr(duplicatedToken),
		0,
		uintptr(unsafe.Pointer(cmdLinePtr)),
		0,
		0,
		0,
		uintptr(CREATE_UNICODE_ENVIRONMENT|CREATE_NO_WINDOW),
		uintptr(envBlock),
		0,
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("CreateProcessAsUserW failed: %w", err)
	}

	log.Printf("[Spawn] Process created: PID=%d", pi.ProcessId)

	windows.CloseHandle(pi.Thread)

	proc, err := os.FindProcess(int(pi.ProcessId))
	if err != nil {
		windows.CloseHandle(pi.Process)
		return nil, fmt.Errorf("failed to find process: %w", err)
	}

	return proc, nil
}

func buildCommandLine(exePath string, args []string) string {
	cmdLine := "\"" + exePath + "\""

	for _, arg := range args {
		if containsSpace(arg) {
			cmdLine += " \"" + arg + "\""
		} else {
			cmdLine += " " + arg
		}
	}

	return cmdLine
}

func containsSpace(s string) bool {
	for _, c := range s {
		if c == ' ' || c == '	' {
			return true
		}
	}
	return false
}

func GetCurrentSessionID() (uint32, error) {
	var sessionID uint32
	err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &sessionID)
	if err != nil {
		return 0, fmt.Errorf("ProcessIdToSessionId failed: %w", err)
	}
	return sessionID, nil
}

func GetActiveConsoleSessionID() uint32 {
	modKernel32 := windows.NewLazySystemDLL("kernel32.dll")
	proc := modKernel32.NewProc("WTSGetActiveConsoleSessionId")
	ret, _, _ := proc.Call()
	return uint32(ret)
}

func IsServiceRunning() bool {
	sessionID, err := GetCurrentSessionID()
	if err != nil {
		return false
	}
	return sessionID == 0
}
