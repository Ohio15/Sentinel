// +build windows

package admin

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modNetapi32 = windows.NewLazySystemDLL("netapi32.dll")
	modAdvapi32 = windows.NewLazySystemDLL("advapi32.dll")
	modSecur32  = windows.NewLazySystemDLL("secur32.dll")
	modWtsapi32 = windows.NewLazySystemDLL("wtsapi32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procNetLocalGroupGetMembers     = modNetapi32.NewProc("NetLocalGroupGetMembers")
	procNetLocalGroupDelMembers     = modNetapi32.NewProc("NetLocalGroupDelMembers")
	procNetLocalGroupAddMembers     = modNetapi32.NewProc("NetLocalGroupAddMembers")
	procNetApiBufferFree            = modNetapi32.NewProc("NetApiBufferFree")
	procNetUserGetInfo              = modNetapi32.NewProc("NetUserGetInfo")
	procNetGetJoinInformation       = modNetapi32.NewProc("NetGetJoinInformation")
	procLookupAccountSidW           = modAdvapi32.NewProc("LookupAccountSidW")
	procConvertSidToStringSidW      = modAdvapi32.NewProc("ConvertSidToStringSidW")
	procConvertStringSidToSidW      = modAdvapi32.NewProc("ConvertStringSidToSidW")
	procGetUserNameExW              = modSecur32.NewProc("GetUserNameExW")
	procWTSQuerySessionInformationW = modWtsapi32.NewProc("WTSQuerySessionInformationW")
	procWTSFreeMemory               = modWtsapi32.NewProc("WTSFreeMemory")
)

const (
	NERR_Success          = 0
	MAX_PREFERRED_LENGTH  = 0xFFFFFFFF
	FILTER_NORMAL_ACCOUNT = 0x0002

	// LOCALGROUP_MEMBERS_INFO levels
	LMIL_0 = 0 // SID only
	LMIL_1 = 1 // SID and SID attributes
	LMIL_2 = 2 // SID and domain/name
	LMIL_3 = 3 // Domain and name only (for add/del)

	// SID usage types
	SidTypeUser           = 1
	SidTypeGroup          = 2
	SidTypeDomain         = 3
	SidTypeAlias          = 4
	SidTypeWellKnownGroup = 5
	SidTypeDeletedAccount = 6
	SidTypeInvalid        = 7
	SidTypeUnknown        = 8
	SidTypeComputer       = 9
	SidTypeLabel          = 10

	// Join status
	NetSetupUnknownStatus   = 0
	NetSetupUnjoined        = 1
	NetSetupWorkgroupName   = 2
	NetSetupDomainName      = 3

	// NameFormat for GetUserNameExW
	NameSamCompatible = 2
	
	// WTS info class
	WTSUserName = 5
)

// LOCALGROUP_MEMBERS_INFO_2 structure
type LOCALGROUP_MEMBERS_INFO_2 struct {
	Sid        *windows.SID
	SidUsage   uint32
	DomainName *uint16
}

// LOCALGROUP_MEMBERS_INFO_3 structure (for add/del)
type LOCALGROUP_MEMBERS_INFO_3 struct {
	DomainAndName *uint16
}

// USER_INFO_1 structure
type USER_INFO_1 struct {
	Name        *uint16
	Password    *uint16
	PasswordAge uint32
	Priv        uint32
	HomeDir     *uint16
	Comment     *uint16
	Flags       uint32
	ScriptPath  *uint16
}

const (
	UF_ACCOUNTDISABLE = 0x0002
)

// discoverAdmins enumerates all members of the local Administrators group
func (m *Manager) discoverAdmins() ([]AdminAccount, error) {
	groupName, err := syscall.UTF16PtrFromString("Administrators")
	if err != nil {
		return nil, err
	}

	var buffer uintptr
	var entriesRead, totalEntries uint32
	var resumeHandle uintptr

	ret, _, _ := procNetLocalGroupGetMembers.Call(
		0, // local computer
		uintptr(unsafe.Pointer(groupName)),
		LMIL_2, // Get SID and domain/name
		uintptr(unsafe.Pointer(&buffer)),
		MAX_PREFERRED_LENGTH,
		uintptr(unsafe.Pointer(&entriesRead)),
		uintptr(unsafe.Pointer(&totalEntries)),
		uintptr(unsafe.Pointer(&resumeHandle)),
	)

	if ret != NERR_Success {
		return nil, fmt.Errorf("NetLocalGroupGetMembers failed with error %d", ret)
	}
	defer procNetApiBufferFree.Call(buffer)

	var admins []AdminAccount
	memberSize := unsafe.Sizeof(LOCALGROUP_MEMBERS_INFO_2{})

	for i := uint32(0); i < entriesRead; i++ {
		member := (*LOCALGROUP_MEMBERS_INFO_2)(unsafe.Pointer(buffer + uintptr(i)*memberSize))

		// Convert SID to string
		sidStr, err := sidToString(member.Sid)
		if err != nil {
			m.logger.Printf("[Admin] Failed to convert SID: %v", err)
			continue
		}

		// Get account name from SID
		name, domain, sidType, err := lookupAccountSid(member.Sid)
		if err != nil {
			m.logger.Printf("[Admin] Failed to lookup account for SID %s: %v", sidStr, err)
			continue
		}

		admin := AdminAccount{
			SID: sidStr,
		}

		// Determine account type and name
		if domain != "" && !strings.EqualFold(domain, getComputerName()) {
			// Domain account
			admin.Name = domain + "\\" + name
			admin.Type = AccountTypeDomain
		} else {
			admin.Name = name
			admin.Type = AccountTypeLocal
		}

		// Check if built-in Administrator (SID ends in -500)
		if strings.HasSuffix(sidStr, "-500") {
			admin.IsBuiltIn = true
			admin.Type = AccountTypeBuiltIn
		}

		// Check if SYSTEM or service account
		if sidStr == "S-1-5-18" || // SYSTEM
			sidStr == "S-1-5-19" || // LOCAL SERVICE
			sidStr == "S-1-5-20" { // NETWORK SERVICE
			admin.IsBuiltIn = true
			admin.Type = AccountTypeBuiltIn
		}

		// Check if account is disabled (for local accounts)
		if admin.Type == AccountTypeLocal && !admin.IsBuiltIn {
			admin.IsDisabled = isAccountDisabled(name)
		}

		// Skip certain SID types
		if sidType == SidTypeDeletedAccount || sidType == SidTypeInvalid || sidType == SidTypeUnknown {
			continue
		}

		admins = append(admins, admin)
	}

	return admins, nil
}

// getCurrentUser returns the currently logged-in interactive user
func (m *Manager) getCurrentUser() (*AdminAccount, error) {
	// Get the username in DOMAIN\User format
	var size uint32 = 256
	buf := make([]uint16, size)

	ret, _, err := procGetUserNameExW.Call(
		NameSamCompatible,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)

	if ret == 0 {
		return nil, fmt.Errorf("GetUserNameExW failed: %v", err)
	}

	username := syscall.UTF16ToString(buf)

	// Get current process token to extract SID
	var token windows.Token
	process := windows.CurrentProcess()
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return nil, fmt.Errorf("OpenProcessToken failed: %v", err)
	}
	defer token.Close()

	// Get token user
	tokenUser, err := token.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("GetTokenUser failed: %v", err)
	}

	sidStr, err := sidToString(tokenUser.User.Sid)
	if err != nil {
		return nil, fmt.Errorf("failed to convert SID: %v", err)
	}

	// Determine account type
	accountType := AccountTypeLocal
	parts := strings.Split(username, "\\")
	if len(parts) == 2 && !strings.EqualFold(parts[0], getComputerName()) {
		accountType = AccountTypeDomain
	}

	return &AdminAccount{
		Name:      username,
		SID:       sidStr,
		Type:      accountType,
		IsCurrent: true,
	}, nil
}

// isDomainJoined checks if the machine is joined to a domain
func (m *Manager) isDomainJoined() bool {
	var buffer *uint16
	var joinStatus uint32

	ret, _, _ := procNetGetJoinInformation.Call(
		0,
		uintptr(unsafe.Pointer(&buffer)),
		uintptr(unsafe.Pointer(&joinStatus)),
	)

	if ret == NERR_Success && buffer != nil {
		defer procNetApiBufferFree.Call(uintptr(unsafe.Pointer(buffer)))
	}

	return joinStatus == NetSetupDomainName
}

// removeFromAdministrators removes a user from the Administrators group by SID
func (m *Manager) removeFromAdministrators(sidStr string) error {
	// Convert string SID to SID structure
	sid, err := stringToSid(sidStr)
	if err != nil {
		return fmt.Errorf("invalid SID: %v", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(sid)))

	// Lookup account name
	name, domain, _, err := lookupAccountSid(sid)
	if err != nil {
		return fmt.Errorf("failed to lookup account: %v", err)
	}

	// Build domain\name string
	fullName := name
	if domain != "" {
		fullName = domain + "\\" + name
	}

	return removeFromLocalGroup("Administrators", fullName)
}

// addToUsers adds a user to the Users group by SID
func (m *Manager) addToUsers(sidStr string) error {
	// Convert string SID to SID structure
	sid, err := stringToSid(sidStr)
	if err != nil {
		return fmt.Errorf("invalid SID: %v", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(sid)))

	// Lookup account name
	name, domain, _, err := lookupAccountSid(sid)
	if err != nil {
		return fmt.Errorf("failed to lookup account: %v", err)
	}

	// Build domain\name string
	fullName := name
	if domain != "" {
		fullName = domain + "\\" + name
	}

	return addToLocalGroup("Users", fullName)
}

// addToAdministratorsByName adds a user back to Administrators (for rollback)
func (m *Manager) addToAdministratorsByName(name string) error {
	return addToLocalGroup("Administrators", name)
}

// removeFromLocalGroup removes a user from a local group
func removeFromLocalGroup(groupName, memberName string) error {
	groupNamePtr, err := syscall.UTF16PtrFromString(groupName)
	if err != nil {
		return err
	}

	memberNamePtr, err := syscall.UTF16PtrFromString(memberName)
	if err != nil {
		return err
	}

	member := LOCALGROUP_MEMBERS_INFO_3{
		DomainAndName: memberNamePtr,
	}

	ret, _, _ := procNetLocalGroupDelMembers.Call(
		0, // local computer
		uintptr(unsafe.Pointer(groupNamePtr)),
		3, // LMIL_3
		uintptr(unsafe.Pointer(&member)),
		1, // count
	)

	if ret != NERR_Success {
		return fmt.Errorf("NetLocalGroupDelMembers failed with error %d", ret)
	}

	return nil
}

// addToLocalGroup adds a user to a local group
func addToLocalGroup(groupName, memberName string) error {
	groupNamePtr, err := syscall.UTF16PtrFromString(groupName)
	if err != nil {
		return err
	}

	memberNamePtr, err := syscall.UTF16PtrFromString(memberName)
	if err != nil {
		return err
	}

	member := LOCALGROUP_MEMBERS_INFO_3{
		DomainAndName: memberNamePtr,
	}

	ret, _, _ := procNetLocalGroupAddMembers.Call(
		0, // local computer
		uintptr(unsafe.Pointer(groupNamePtr)),
		3, // LMIL_3
		uintptr(unsafe.Pointer(&member)),
		1, // count
	)

	if ret != NERR_Success && ret != 1378 { // 1378 = already member
		return fmt.Errorf("NetLocalGroupAddMembers failed with error %d", ret)
	}

	return nil
}

// sidToString converts a SID to its string representation
func sidToString(sid *windows.SID) (string, error) {
	var strSid *uint16
	ret, _, err := procConvertSidToStringSidW.Call(
		uintptr(unsafe.Pointer(sid)),
		uintptr(unsafe.Pointer(&strSid)),
	)
	if ret == 0 {
		return "", fmt.Errorf("ConvertSidToStringSidW failed: %v", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(strSid)))

	return syscall.UTF16ToString((*[256]uint16)(unsafe.Pointer(strSid))[:]), nil
}

// stringToSid converts a string SID to a SID structure
func stringToSid(sidStr string) (*windows.SID, error) {
	sidStrPtr, err := syscall.UTF16PtrFromString(sidStr)
	if err != nil {
		return nil, err
	}

	var sid *windows.SID
	ret, _, err := procConvertStringSidToSidW.Call(
		uintptr(unsafe.Pointer(sidStrPtr)),
		uintptr(unsafe.Pointer(&sid)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("ConvertStringSidToSidW failed: %v", err)
	}

	return sid, nil
}

// lookupAccountSid looks up account name and domain from a SID
func lookupAccountSid(sid *windows.SID) (name, domain string, sidType uint32, err error) {
	var nameSize, domainSize uint32 = 256, 256
	nameBuf := make([]uint16, nameSize)
	domainBuf := make([]uint16, domainSize)

	ret, _, lastErr := procLookupAccountSidW.Call(
		0, // local computer
		uintptr(unsafe.Pointer(sid)),
		uintptr(unsafe.Pointer(&nameBuf[0])),
		uintptr(unsafe.Pointer(&nameSize)),
		uintptr(unsafe.Pointer(&domainBuf[0])),
		uintptr(unsafe.Pointer(&domainSize)),
		uintptr(unsafe.Pointer(&sidType)),
	)

	if ret == 0 {
		return "", "", 0, lastErr
	}

	return syscall.UTF16ToString(nameBuf), syscall.UTF16ToString(domainBuf), sidType, nil
}

// getComputerName returns the local computer name
func getComputerName() string {
	var size uint32 = 256
	buf := make([]uint16, size)
	windows.GetComputerName(&buf[0], &size)
	return syscall.UTF16ToString(buf)
}

// isAccountDisabled checks if a local account is disabled
func isAccountDisabled(username string) bool {
	usernamePtr, err := syscall.UTF16PtrFromString(username)
	if err != nil {
		return false
	}

	var buffer uintptr
	ret, _, _ := procNetUserGetInfo.Call(
		0, // local computer
		uintptr(unsafe.Pointer(usernamePtr)),
		1, // USER_INFO_1
		uintptr(unsafe.Pointer(&buffer)),
	)

	if ret != NERR_Success {
		return false
	}
	defer procNetApiBufferFree.Call(buffer)

	info := (*USER_INFO_1)(unsafe.Pointer(buffer))
	return (info.Flags & UF_ACCOUNTDISABLE) != 0
}

// GetActiveSessionUser gets the user of the active console session
func GetActiveSessionUser() (string, uint32, error) {
	// Get active console session
	proc := modKernel32.NewProc("WTSGetActiveConsoleSessionId")
	ret, _, _ := proc.Call()
	sessionID := uint32(ret)
	
	if sessionID == 0xFFFFFFFF {
		return "", 0, fmt.Errorf("no active console session")
	}

	// Query session for username
	var buffer uintptr
	var bytesReturned uint32

	ret, _, err := procWTSQuerySessionInformationW.Call(
		0, // local server (WTS_CURRENT_SERVER_HANDLE)
		uintptr(sessionID),
		WTSUserName,
		uintptr(unsafe.Pointer(&buffer)),
		uintptr(unsafe.Pointer(&bytesReturned)),
	)

	if ret == 0 {
		return "", 0, fmt.Errorf("WTSQuerySessionInformationW failed: %v", err)
	}
	defer procWTSFreeMemory.Call(buffer)

	username := syscall.UTF16ToString((*[256]uint16)(unsafe.Pointer(buffer))[:])
	return username, sessionID, nil
}
