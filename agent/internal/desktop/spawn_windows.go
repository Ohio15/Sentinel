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
	modAdvapi32  = windows.NewLazySystemDLL("advapi32.dll")
	modKernel32  = windows.NewLazySystemDLL("kernel32.dll")
	modUserenv   = windows.NewLazySystemDLL("userenv.dll")
	modWtsapi32  = windows.NewLazySystemDLL("wtsapi32.dll")

	procCreateProcessAsUserW     = modAdvapi32.NewProc("CreateProcessAsUserW")
	procWTSQueryUserToken        = modWtsapi32.NewProc("WTSQueryUserToken")
	procCreateEnvironmentBlock   = modUserenv.NewProc("CreateEnvironmentBlock")
	procDestroyEnvironmentBlock  = modUserenv.NewProc("DestroyEnvironmentBlock")
	procDuplicateTokenEx         = modAdvapi32.NewProc("DuplicateTokenEx")
	procGetTokenInformation      = modAdvapi32.NewProc("GetTokenInformation")
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
	TokenUser                  = 1
	TokenSessionId             = 12
)

// CREATE_PROCESS flags
const (
	CREATE_UNICODE_ENVIRONMENT = 0x00000400
	CREATE_NEW_CONSOLE         = 0x00000010
	CREATE_NO_WINDOW           = 0x08000000
)

// SpawnInSession spawns a process in the specified Windows session
func SpawnInSession(sessionID uint32, exePath string, args []string) (*os.Process, error) {
	log.Printf("[Spawn] SpawnInSession called: sessionID=%d, exePath=%s", sessionID, exePath)

	// Get the user token for the session
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

	// Duplicate the token to create a primary token
	var duplicatedToken windows.Token
	ret, _, err = procDuplicateTokenEx.Call(
		uintptr(userToken),
		uintptr(windows.TOKEN_ALL_ACCESS),
		0, // No security attributes
		uintptr(SecurityImpersonation),
		uintptr(TokenPrimary),
		uintptr(unsafe.Pointer(&duplicatedToken)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("DuplicateTokenEx failed: %w", err)
	}
	defer duplicatedToken.Close()

	log.Printf("[Spawn] Duplicated token")

	// Create environment block for the user
	var envBlock unsafe.Pointer
	ret, _, err = procCreateEnvironmentBlock.Call(
		uintptr(unsafe.Pointer(&envBlock)),
		uintptr(duplicatedToken),
		0, // Don't inherit from current process
	)
	if ret == 0 {
		return nil, fmt.Errorf("CreateEnvironmentBlock failed: %w", err)
	}
	defer procDestroyEnvironmentBlock.Call(uintptr(envBlock))

	log.Printf("[Spawn] Created environment block")

	// Build command line
	cmdLine := buildCommandLine(exePath, args)
	cmdLinePtr, err := syscall.UTF16PtrFromString(cmdLine)
	if err != nil {
		return nil, fmt.Errorf("failed to convert command line: %w", err)
	}

	// Setup startup info
	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Desktop, _ = syscall.UTF16PtrFromString("winsta0\\default")

	var pi windows.ProcessInformation

	log.Printf("[Spawn] Calling CreateProcessAsUserW: %s", cmdLine)

	// Create the process
	ret, _, err = procCreateProcessAsUserW.Call(
		uintptr(duplicatedToken),
		0, // Application name (use command line)
		uintptr(unsafe.Pointer(cmdLinePtr)),
		0, // Process security attributes
		0, // Thread security attributes
		0, // Don't inherit handles
		uintptr(CREATE_UNICODE_ENVIRONMENT|CREATE_NO_WINDOW),
		uintptr(envBlock),
		0, // Use current directory
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("CreateProcessAsUserW failed: %w", err)
	}

	log.Printf("[Spawn] Process created: PID=%d", pi.ProcessId)

	// Close thread handle, keep process handle
	windows.CloseHandle(pi.Thread)

	// Wrap in os.Process
	proc, err := os.FindProcess(int(pi.ProcessId))
	if err != nil {
		windows.CloseHandle(pi.Process)
		return nil, fmt.Errorf("failed to find process: %w", err)
	}

	return proc, nil
}

// buildCommandLine builds a Windows command line string from executable and arguments
func buildCommandLine(exePath string, args []string) string {
	// Quote the executable path
	cmdLine := "\"" + exePath + "\""

	// Add arguments
	for _, arg := range args {
		// Quote arguments that contain spaces
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
		if c == ' ' || c == '\t' {
			return true
		}
	}
	return false
}

// GetCurrentSessionID returns the Windows session ID of the current process
func GetCurrentSessionID() (uint32, error) {
	var sessionID uint32
	err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &sessionID)
	if err != nil {
		return 0, fmt.Errorf("ProcessIdToSessionId failed: %w", err)
	}
	return sessionID, nil
}

// GetActiveConsoleSessionID returns the session ID of the active console session
func GetActiveConsoleSessionID() uint32 {
	modKernel32 := windows.NewLazySystemDLL("kernel32.dll")
	proc := modKernel32.NewProc("WTSGetActiveConsoleSessionId")
	ret, _, _ := proc.Call()
	return uint32(ret)
}

// IsServiceRunning checks if we're running as a Windows service (Session 0)
func IsServiceRunning() bool {
	sessionID, err := GetCurrentSessionID()
	if err != nil {
		return false
	}
	return sessionID == 0
}
