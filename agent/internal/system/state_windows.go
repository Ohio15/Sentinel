//go:build windows

package system

import (
	"log"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/sentinel/agent/internal/winptr"
)

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	user32                      = syscall.NewLazyDLL("user32.dll")
	procSetThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")
	procOpenInputDesktop        = user32.NewProc("OpenInputDesktop")
	procCloseDesktop            = user32.NewProc("CloseDesktop")
	procGetUserObjectInformationW = user32.NewProc("GetUserObjectInformationW")
)

const (
	ES_CONTINUOUS       = 0x80000000
	ES_SYSTEM_REQUIRED  = 0x00000001
	ES_DISPLAY_REQUIRED = 0x00000002
	ES_AWAYMODE_REQUIRED = 0x00000040

	DESKTOP_READOBJECTS = 0x0001

	UOI_NAME = 2
)

// DesktopType represents the type of Windows desktop
type DesktopType int

const (
	DesktopDefault DesktopType = iota
	DesktopWinlogon // UAC, lock screen, etc.
	DesktopScreenSaver
	DesktopUnknown
)

// StateManager manages system state during remote sessions
type StateManager struct {
	active          bool
	previousState   uintptr
	mu              sync.Mutex
}

// NewStateManager creates a new system state manager
func NewStateManager() *StateManager {
	return &StateManager{}
}

// PreventSleep prevents the system from sleeping and the display from turning off
func (s *StateManager) PreventSleep() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active {
		return
	}

	// Prevent display off, system sleep, and away mode
	flags := uintptr(ES_CONTINUOUS | ES_SYSTEM_REQUIRED | ES_DISPLAY_REQUIRED | ES_AWAYMODE_REQUIRED)
	prev, _, _ := procSetThreadExecutionState.Call(flags)

	s.previousState = prev
	s.active = true

	log.Println("[System] Sleep prevention enabled")
}

// AllowSleep restores normal power management
func (s *StateManager) AllowSleep() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return
	}

	// Restore to continuous only (normal state)
	procSetThreadExecutionState.Call(ES_CONTINUOUS)

	s.active = false
	log.Println("[System] Sleep prevention disabled")
}

// IsActive returns whether sleep prevention is active
func (s *StateManager) IsActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// GetCurrentDesktop returns the current input desktop type
func GetCurrentDesktop() DesktopType {
	// Open the current input desktop
	desk, _, _ := procOpenInputDesktop.Call(0, 0, DESKTOP_READOBJECTS)
	if desk == 0 {
		// Can't access input desktop - likely secure desktop
		return DesktopWinlogon
	}
	defer procCloseDesktop.Call(desk)

	// Get desktop name
	name := getDesktopName(desk)

	switch name {
	case "Default":
		return DesktopDefault
	case "Winlogon":
		return DesktopWinlogon
	case "Screen-saver":
		return DesktopScreenSaver
	default:
		return DesktopUnknown
	}
}

func getDesktopName(desk uintptr) string {
	var nameLen uint32
	var name [256]uint16

	ret, _, _ := procGetUserObjectInformationW.Call(
		desk,
		UOI_NAME,
		uintptr(unsafe.Pointer(&name[0])),
		uintptr(len(name)*2),
		uintptr(unsafe.Pointer(&nameLen)),
	)

	if ret == 0 {
		return ""
	}

	return syscall.UTF16ToString(name[:])
}

// IsSecureDesktop returns true if we're on a secure desktop (UAC, lock screen, etc.)
func IsSecureDesktop() bool {
	desktop := GetCurrentDesktop()
	return desktop == DesktopWinlogon || desktop == DesktopScreenSaver
}

// CanCaptureScreen returns true if screen capture is possible on current desktop
func CanCaptureScreen() bool {
	return !IsSecureDesktop()
}

// CanInjectInput returns true if input injection is possible on current desktop
func CanInjectInput() bool {
	return !IsSecureDesktop()
}

// SessionInfo contains information about the current session
type SessionInfo struct {
	SessionID      uint32
	UserName       string
	DomainName     string
	IsRemoteSession bool
	IsConsoleSession bool
}

var (
	procWTSQuerySessionInformationW = syscall.NewLazyDLL("wtsapi32.dll").NewProc("WTSQuerySessionInformationW")
	procWTSFreeMemory               = syscall.NewLazyDLL("wtsapi32.dll").NewProc("WTSFreeMemory")
	procProcessIdToSessionId        = kernel32.NewProc("ProcessIdToSessionId")
	procGetCurrentProcessId         = kernel32.NewProc("GetCurrentProcessId")
)

const (
	WTSInitialProgram     = 0
	WTSApplicationName    = 1
	WTSWorkingDirectory   = 2
	WTSOEMId              = 3
	WTSSessionId          = 4
	WTSUserName           = 5
	WTSWinStationName     = 6
	WTSDomainName         = 7
	WTSConnectState       = 8
	WTSClientBuildNumber  = 9
	WTSClientName         = 10
	WTSClientDirectory    = 11
	WTSClientProductId    = 12
	WTSClientHardwareId   = 13
	WTSClientAddress      = 14
	WTSClientDisplay      = 15
	WTSClientProtocolType = 16
	WTSIdleTime           = 17
	WTSLogonTime          = 18
	WTSIncomingBytes      = 19
	WTSOutgoingBytes      = 20
	WTSIncomingFrames     = 21
	WTSOutgoingFrames     = 22
	WTSClientInfo         = 23
	WTSSessionInfo        = 24

	WTS_CURRENT_SERVER_HANDLE = 0
)

// GetSessionInfo returns information about the current session
func GetSessionInfo() (*SessionInfo, error) {
	info := &SessionInfo{}

	// Get current process session ID
	pid, _, _ := procGetCurrentProcessId.Call()
	var sessionID uint32
	ret, _, _ := procProcessIdToSessionId.Call(pid, uintptr(unsafe.Pointer(&sessionID)))
	if ret == 0 {
		return nil, syscall.GetLastError()
	}
	info.SessionID = sessionID

	// Console session is typically session 1 or higher, session 0 is services
	info.IsConsoleSession = sessionID > 0

	// Get user name
	info.UserName = querySessionString(sessionID, WTSUserName)
	info.DomainName = querySessionString(sessionID, WTSDomainName)

	// Check if remote session
	stationName := querySessionString(sessionID, WTSWinStationName)
	info.IsRemoteSession = stationName != "Console" && stationName != ""

	return info, nil
}

func querySessionString(sessionID uint32, infoClass int) string {
	var buffer uintptr
	var bytesReturned uint32

	ret, _, _ := procWTSQuerySessionInformationW.Call(
		WTS_CURRENT_SERVER_HANDLE,
		uintptr(sessionID),
		uintptr(infoClass),
		uintptr(unsafe.Pointer(&buffer)),
		uintptr(unsafe.Pointer(&bytesReturned)),
	)

	if ret == 0 || buffer == 0 {
		return ""
	}
	defer procWTSFreeMemory.Call(buffer)

	// Convert UTF-16 string
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(winptr.FromUintptr(buffer)), 1024))
}

// MonitorState continuously monitors system state and calls callbacks
type MonitorState struct {
	onDesktopChange func(DesktopType)
	stopCh          chan struct{}
	running         bool
	mu              sync.Mutex
}

// NewMonitorState creates a new state monitor
func NewMonitorState(onDesktopChange func(DesktopType)) *MonitorState {
	return &MonitorState{
		onDesktopChange: onDesktopChange,
		stopCh:          make(chan struct{}),
	}
}

// Start begins monitoring
func (m *MonitorState) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	go m.monitorLoop()
}

// Stop stops monitoring
func (m *MonitorState) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	close(m.stopCh)
	m.mu.Unlock()
}

func (m *MonitorState) monitorLoop() {
	lastDesktop := GetCurrentDesktop()

	for {
		select {
		case <-m.stopCh:
			return
		default:
			currentDesktop := GetCurrentDesktop()
			if currentDesktop != lastDesktop {
				lastDesktop = currentDesktop
				if m.onDesktopChange != nil {
					m.onDesktopChange(currentDesktop)
				}
			}
			// Check every 500ms
			time.Sleep(500 * time.Millisecond)
		}
	}
}
