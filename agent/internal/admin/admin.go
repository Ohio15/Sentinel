// Package admin provides local administrator management functionality
// including enumeration, demotion, and safety validation.
package admin

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// AccountType represents the type of user account
type AccountType string

const (
	AccountTypeLocal  AccountType = "LocalUser"
	AccountTypeDomain AccountType = "DomainUser"
	AccountTypeBuiltIn AccountType = "BuiltIn"
)

// AdminAccount represents a local administrator account
type AdminAccount struct {
	Name        string      `json:"name"`
	SID         string      `json:"sid"`
	Type        AccountType `json:"type"`
	IsBuiltIn   bool        `json:"isBuiltIn"`
	IsDisabled  bool        `json:"isDisabled"`
	IsCurrent   bool        `json:"isCurrent"`
	FullName    string      `json:"fullName,omitempty"`
	Description string      `json:"description,omitempty"`
}

// DemotionRequest represents a request to demote administrators
type DemotionRequest struct {
	AccountsToDemote []string `json:"accountsToDemote"` // SIDs of accounts to demote
	Confirmed        bool     `json:"confirmed"`
}

// DemotionResult represents the result of a demotion operation
type DemotionResult struct {
	Success         bool     `json:"success"`
	DemotedAccounts []string `json:"demotedAccounts"`
	RemainingAdmins []string `json:"remainingAdmins"`
	Errors          []string `json:"errors,omitempty"`
	RolledBack      bool     `json:"rolledBack,omitempty"`
}

// DemotionEvent represents a logged demotion event for telemetry
type DemotionEvent struct {
	Timestamp        time.Time `json:"timestamp"`
	DeviceName       string    `json:"device"`
	DemotedUsers     []string  `json:"demotedUsers"`
	RemainingAdmins  []string  `json:"remainingAdmins"`
	InstallerVersion string    `json:"installerVersion"`
	Success          bool      `json:"success"`
	ErrorMessage     string    `json:"errorMessage,omitempty"`
}

// SafetyCheck represents the result of safety validation
type SafetyCheck struct {
	Safe             bool     `json:"safe"`
	CanProceed       bool     `json:"canProceed"`
	CurrentUser      string   `json:"currentUser"`
	CurrentUserSID   string   `json:"currentUserSid"`
	TotalAdmins      int      `json:"totalAdmins"`
	DemotableAdmins  int      `json:"demotableAdmins"`
	Warnings         []string `json:"warnings,omitempty"`
	BlockingReasons  []string `json:"blockingReasons,omitempty"`
	IsDomainJoined   bool     `json:"isDomainJoined"`
	HasDomainAdmins  bool     `json:"hasDomainAdmins"`
}

// Manager handles admin account operations
type Manager struct {
	version string
	logger  *log.Logger
}

// NewManager creates a new admin manager
func NewManager(version string) *Manager {
	return &Manager{
		version: version,
		logger:  log.Default(),
	}
}

// SetLogger sets a custom logger
func (m *Manager) SetLogger(logger *log.Logger) {
	m.logger = logger
}

// DiscoverAdmins returns all local administrator accounts
// This is implemented in admin_windows.go
func (m *Manager) DiscoverAdmins() ([]AdminAccount, error) {
	return m.discoverAdmins()
}

// GetCurrentUser returns the currently logged-in interactive user
func (m *Manager) GetCurrentUser() (*AdminAccount, error) {
	return m.getCurrentUser()
}

// ValidateSafety performs safety checks before demotion
func (m *Manager) ValidateSafety(admins []AdminAccount) (*SafetyCheck, error) {
	check := &SafetyCheck{
		TotalAdmins: len(admins),
	}

	// Get current user
	currentUser, err := m.GetCurrentUser()
	if err != nil {
		check.BlockingReasons = append(check.BlockingReasons,
			fmt.Sprintf("Failed to identify current user: %v", err))
		return check, nil
	}
	check.CurrentUser = currentUser.Name
	check.CurrentUserSID = currentUser.SID

	// Mark current user in admin list
	for i := range admins {
		if admins[i].SID == currentUser.SID {
			admins[i].IsCurrent = true
		}
	}

	// Check domain status
	check.IsDomainJoined = m.isDomainJoined()

	// Count demotable admins (excluding built-in and disabled)
	demotable := 0
	hasOtherAdmins := false
	for _, admin := range admins {
		if admin.IsBuiltIn || admin.IsDisabled {
			continue
		}
		demotable++
		if admin.SID != currentUser.SID {
			hasOtherAdmins = true
		}
		if admin.Type == AccountTypeDomain {
			check.HasDomainAdmins = true
		}
	}
	check.DemotableAdmins = demotable

	// Safety validation
	if demotable == 0 {
		check.BlockingReasons = append(check.BlockingReasons,
			"No administrator accounts available for management")
		return check, nil
	}

	if demotable == 1 && !hasOtherAdmins {
		check.BlockingReasons = append(check.BlockingReasons,
			"You are the only administrator on this device. Demotion would remove all administrative access.")
		check.Warnings = append(check.Warnings,
			"Consider creating a secondary admin account before proceeding")
		return check, nil
	}

	// Check if GPO might block changes (domain-joined machines)
	if check.IsDomainJoined {
		check.Warnings = append(check.Warnings,
			"This machine is domain-joined. Group Policy may restrict local group changes.")
	}

	check.Safe = true
	check.CanProceed = hasOtherAdmins
	return check, nil
}

// Demote removes the specified accounts from the Administrators group
func (m *Manager) Demote(request *DemotionRequest, admins []AdminAccount) (*DemotionResult, error) {
	if !request.Confirmed {
		return &DemotionResult{
			Success: false,
			Errors:  []string{"Demotion not confirmed by user"},
		}, nil
	}

	// Build map of admins by SID
	adminMap := make(map[string]*AdminAccount)
	for i := range admins {
		adminMap[admins[i].SID] = &admins[i]
	}

	// Validate we won't remove all admins
	remainingCount := len(admins)
	for _, sid := range request.AccountsToDemote {
		if admin, ok := adminMap[sid]; ok && !admin.IsBuiltIn && !admin.IsDisabled {
			remainingCount--
		}
	}
	if remainingCount < 1 {
		return &DemotionResult{
			Success: false,
			Errors:  []string{"Cannot demote all administrators. At least one must remain."},
		}, nil
	}

	result := &DemotionResult{
		Success: true,
	}

	// Track original state for rollback
	demoted := []string{}

	// Execute demotion for each account
	for _, sid := range request.AccountsToDemote {
		admin, ok := adminMap[sid]
		if !ok {
			result.Errors = append(result.Errors, fmt.Sprintf("Account %s not found", sid))
			continue
		}
		if admin.IsBuiltIn {
			result.Errors = append(result.Errors, fmt.Sprintf("Cannot demote built-in account %s", admin.Name))
			continue
		}

		m.logger.Printf("[Admin] Demoting account: %s (%s)", admin.Name, admin.SID)

		// Remove from Administrators group
		if err := m.removeFromAdministrators(admin.SID); err != nil {
			result.Success = false
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to demote %s: %v", admin.Name, err))
			// Rollback on failure
			m.rollback(demoted)
			result.RolledBack = true
			return result, nil
		}

		// Ensure in Users group
		if err := m.addToUsers(admin.SID); err != nil {
			m.logger.Printf("[Admin] Warning: Failed to add %s to Users group: %v", admin.Name, err)
			// Non-fatal, continue
		}

		demoted = append(demoted, admin.Name)
	}

	result.DemotedAccounts = demoted

	// Verify remaining admins
	remaining, err := m.DiscoverAdmins()
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Warning: Could not verify remaining admins: %v", err))
	} else {
		for _, admin := range remaining {
			if !admin.IsDisabled {
				result.RemainingAdmins = append(result.RemainingAdmins, admin.Name)
			}
		}
		if len(result.RemainingAdmins) == 0 {
			// Critical: No admins left, rollback
			m.logger.Printf("[Admin] CRITICAL: No admins remaining, rolling back")
			m.rollback(demoted)
			result.Success = false
			result.RolledBack = true
			result.Errors = append(result.Errors, "Rollback triggered: No administrators remaining after demotion")
		}
	}

	return result, nil
}

// rollback re-adds demoted accounts to Administrators group
func (m *Manager) rollback(accounts []string) {
	m.logger.Printf("[Admin] Rolling back demotion for %d accounts", len(accounts))
	for _, name := range accounts {
		if err := m.addToAdministratorsByName(name); err != nil {
			m.logger.Printf("[Admin] Rollback failed for %s: %v", name, err)
		} else {
			m.logger.Printf("[Admin] Rollback successful for %s", name)
		}
	}
}

// CreateDemotionEvent creates a telemetry event for the demotion
func (m *Manager) CreateDemotionEvent(result *DemotionResult, deviceName string) *DemotionEvent {
	event := &DemotionEvent{
		Timestamp:        time.Now().UTC(),
		DeviceName:       deviceName,
		DemotedUsers:     result.DemotedAccounts,
		RemainingAdmins:  result.RemainingAdmins,
		InstallerVersion: m.version,
		Success:          result.Success,
	}
	if len(result.Errors) > 0 {
		event.ErrorMessage = result.Errors[0]
	}
	return event
}

// ToJSON serializes the event to JSON
func (e *DemotionEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}
