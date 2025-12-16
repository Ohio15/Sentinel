// +build windows

package desktop

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	TaskName = "SentinelDesktopHelper"
)

// HelperConfig is written to a file for the helper to read
type HelperConfig struct {
	SessionID uint32 `json:"sessionId"`
	Token     string `json:"token"`
	PipeName  string `json:"pipeName"`
}

// EnsureScheduledTask creates the scheduled task if it doesn't exist
func EnsureScheduledTask(helperPath string) error {
	// Check if task already exists
	checkCmd := exec.Command("schtasks", "/Query", "/TN", TaskName)
	if err := checkCmd.Run(); err == nil {
		log.Printf("[Spawn] Scheduled task %s already exists", TaskName)
		return nil
	}

	log.Printf("[Spawn] Creating scheduled task %s...", TaskName)

	// Create the task
	// /SC ONCE /ST 00:00 creates a task that won't run automatically
	// /RL LIMITED runs with normal user privileges
	// /F forces creation if exists
	createCmd := exec.Command("schtasks", "/Create",
		"/TN", TaskName,
		"/TR", fmt.Sprintf(`"%s" --from-task`, helperPath),
		"/SC", "ONCE",
		"/ST", "00:00",
		"/SD", "01/01/2099",
		"/RL", "LIMITED",
		"/F",
	)

	output, err := createCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create scheduled task: %w, output: %s", err, string(output))
	}

	log.Printf("[Spawn] Created scheduled task: %s", string(output))
	return nil
}

// SpawnViaScheduledTask launches the helper using a scheduled task
func SpawnViaScheduledTask(sessionID uint32, helperPath string, token string) error {
	// Write config file for the helper to read
	configPath := filepath.Join(os.TempDir(), fmt.Sprintf("sentinel-helper-%d.json", sessionID))

	config := HelperConfig{
		SessionID: sessionID,
		Token:     token,
		PipeName:  fmt.Sprintf(`\\.\pipe\SentinelDesktop_%d`, sessionID),
	}

	configData, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	log.Printf("[Spawn] Wrote helper config to %s", configPath)

	// Run the scheduled task
	// /I flag runs it interactively in the current user's session
	runCmd := exec.Command("schtasks", "/Run", "/TN", TaskName, "/I")
	output, err := runCmd.CombinedOutput()
	if err != nil {
		os.Remove(configPath)
		return fmt.Errorf("failed to run scheduled task: %w, output: %s", err, string(output))
	}

	log.Printf("[Spawn] Triggered scheduled task: %s", strings.TrimSpace(string(output)))
	return nil
}

// SpawnInSession spawns a process in the specified Windows session
// Now uses scheduled task approach which is more reliable
func SpawnInSession(sessionID uint32, exePath string, args []string) (*os.Process, error) {
	log.Printf("[Spawn] SpawnInSession called: sessionID=%d, exePath=%s", sessionID, exePath)

	// Extract token from args
	var token string
	for i, arg := range args {
		if arg == "--token" && i+1 < len(args) {
			token = args[i+1]
			break
		}
	}

	// Ensure the scheduled task exists
	if err := EnsureScheduledTask(exePath); err != nil {
		log.Printf("[Spawn] Warning: failed to ensure scheduled task: %v", err)
		// Fall through to try direct spawn
	} else {
		// Try scheduled task approach first
		if err := SpawnViaScheduledTask(sessionID, exePath, token); err != nil {
			log.Printf("[Spawn] Scheduled task failed: %v, trying direct spawn...", err)
		} else {
			log.Printf("[Spawn] Scheduled task triggered successfully")
			// Return nil process - the helper will connect via IPC
			// We can't get the PID from schtasks easily
			return nil, nil
		}
	}

	// Fallback: try CreateProcessAsUser (may fail but worth trying)
	log.Printf("[Spawn] Falling back to CreateProcessAsUser...")
	return spawnDirect(sessionID, exePath, args)
}

// spawnDirect tries to spawn using CreateProcessAsUser (fallback)
func spawnDirect(sessionID uint32, exePath string, args []string) (*os.Process, error) {
	// Enable privileges
	enableRequiredPrivileges()

	// Get user token
	var userToken windows.Token
	ret, _, err := procWTSQueryUserToken.Call(
		uintptr(sessionID),
		uintptr(unsafe.Pointer(&userToken)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("WTSQueryUserToken failed: %w", err)
	}
	defer userToken.Close()

	// Create environment block
	var envBlock unsafe.Pointer
	ret, _, err = procCreateEnvironmentBlock.Call(
		uintptr(unsafe.Pointer(&envBlock)),
		uintptr(userToken),
		0,
	)
	if ret == 0 {
		return nil, fmt.Errorf("CreateEnvironmentBlock failed: %w", err)
	}
	defer procDestroyEnvironmentBlock.Call(uintptr(envBlock))

	// Prepare command
	exePathPtr, _ := syscall.UTF16PtrFromString(exePath)
	workingDir := filepath.Dir(exePath)
	workingDirPtr, _ := syscall.UTF16PtrFromString(workingDir)
	cmdLine := buildCommandLine(exePath, args)
	cmdLinePtr, _ := syscall.UTF16PtrFromString(cmdLine)

	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Desktop, _ = syscall.UTF16PtrFromString("winsta0\\default")

	var pi windows.ProcessInformation

	ret, _, err = procCreateProcessAsUserW.Call(
		uintptr(userToken),
		uintptr(unsafe.Pointer(exePathPtr)),
		uintptr(unsafe.Pointer(cmdLinePtr)),
		0, 0, 0,
		uintptr(CREATE_UNICODE_ENVIRONMENT|CREATE_NEW_CONSOLE),
		uintptr(envBlock),
		uintptr(unsafe.Pointer(workingDirPtr)),
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("CreateProcessAsUserW failed: %w (LastError=%d)", err, windows.GetLastError())
	}

	log.Printf("[Spawn] CreateProcessAsUserW succeeded: PID=%d", pi.ProcessId)
	windows.CloseHandle(pi.Thread)
	proc, _ := os.FindProcess(int(pi.ProcessId))
	return proc, nil
}

var (
	modAdvapi32 = windows.NewLazySystemDLL("advapi32.dll")
	modUserenv  = windows.NewLazySystemDLL("userenv.dll")
	modWtsapi32 = windows.NewLazySystemDLL("wtsapi32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procCreateProcessAsUserW    = modAdvapi32.NewProc("CreateProcessAsUserW")
	procWTSQueryUserToken       = modWtsapi32.NewProc("WTSQueryUserToken")
	procCreateEnvironmentBlock  = modUserenv.NewProc("CreateEnvironmentBlock")
	procDestroyEnvironmentBlock = modUserenv.NewProc("DestroyEnvironmentBlock")
	procLookupPrivilegeValueW   = modAdvapi32.NewProc("LookupPrivilegeValueW")
	procAdjustTokenPrivileges   = modAdvapi32.NewProc("AdjustTokenPrivileges")
)

const (
	CREATE_UNICODE_ENVIRONMENT = 0x00000400
	CREATE_NEW_CONSOLE         = 0x00000010
	SE_PRIVILEGE_ENABLED       = 0x00000002
)

type LUID struct {
	LowPart  uint32
	HighPart int32
}

type LUID_AND_ATTRIBUTES struct {
	Luid       LUID
	Attributes uint32
}

type TOKEN_PRIVILEGES struct {
	PrivilegeCount uint32
	Privileges     [1]LUID_AND_ATTRIBUTES
}

func enableRequiredPrivileges() {
	privileges := []string{
		"SeAssignPrimaryTokenPrivilege",
		"SeIncreaseQuotaPrivilege",
		"SeTcbPrivilege",
		"SeImpersonatePrivilege",
	}

	for _, priv := range privileges {
		enablePrivilege(priv)
	}
}

func enablePrivilege(privilegeName string) error {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token)
	if err != nil {
		return err
	}
	defer token.Close()

	privNamePtr, _ := syscall.UTF16PtrFromString(privilegeName)

	var luid LUID
	ret, _, err := procLookupPrivilegeValueW.Call(0, uintptr(unsafe.Pointer(privNamePtr)), uintptr(unsafe.Pointer(&luid)))
	if ret == 0 {
		return err
	}

	var tp TOKEN_PRIVILEGES
	tp.PrivilegeCount = 1
	tp.Privileges[0].Luid = luid
	tp.Privileges[0].Attributes = SE_PRIVILEGE_ENABLED

	procAdjustTokenPrivileges.Call(uintptr(token), 0, uintptr(unsafe.Pointer(&tp)), 0, 0, 0)
	return nil
}

func buildCommandLine(exePath string, args []string) string {
	cmdLine := "\"" + exePath + "\""
	for _, arg := range args {
		if strings.ContainsAny(arg, " \t") {
			cmdLine += " \"" + arg + "\""
		} else {
			cmdLine += " " + arg
		}
	}
	return cmdLine
}

func GetCurrentSessionID() (uint32, error) {
	var sessionID uint32
	err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &sessionID)
	return sessionID, err
}

func GetActiveConsoleSessionID() uint32 {
	proc := modKernel32.NewProc("WTSGetActiveConsoleSessionId")
	ret, _, _ := proc.Call()
	return uint32(ret)
}

func IsServiceRunning() bool {
	sessionID, _ := GetCurrentSessionID()
	return sessionID == 0
}
