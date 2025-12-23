// Package audit provides audit logging functionality for security-critical operations
// DC-003 FIX: Implements comprehensive audit trail for security operations
package audit

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Action constants for audit logging
const (
	// Authentication actions
	ActionLoginSuccess   = "login_success"
	ActionLoginFailed    = "login_failed"
	ActionLogout         = "logout"
	ActionTokenRefresh   = "token_refresh"
	ActionPasswordChange = "password_change"

	// Device actions
	ActionDeviceDisabled  = "device_disabled"
	ActionDeviceEnabled   = "device_enabled"
	ActionDeviceDeleted   = "device_deleted"
	ActionDeviceUninstall = "device_uninstall"

	// User management actions
	ActionUserCreated = "user_created"
	ActionUserUpdated = "user_updated"
	ActionUserDeleted = "user_deleted"
	ActionRoleChanged = "role_changed"

	// Admin actions
	ActionSettingsUpdated   = "settings_updated"
	ActionAlertAcknowledged = "alert_acknowledged"
	ActionAlertResolved     = "alert_resolved"
	ActionScriptExecuted    = "script_executed"
	ActionCommandExecuted   = "command_executed"
)

// ResourceType constants
const (
	ResourceTypeUser    = "user"
	ResourceTypeDevice  = "device"
	ResourceTypeScript  = "script"
	ResourceTypeAlert   = "alert"
	ResourceTypeSetting = "setting"
	ResourceTypeCommand = "command"
	ResourceTypeSession = "session"
)

// Logger provides audit logging functionality
type Logger struct {
	pool *pgxpool.Pool
}

// NewLogger creates a new audit logger
func NewLogger(pool *pgxpool.Pool) *Logger {
	return &Logger{pool: pool}
}

// Entry represents an audit log entry
type Entry struct {
	UserID       *uuid.UUID             `json:"userId,omitempty"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resourceType,omitempty"`
	ResourceID   *uuid.UUID             `json:"resourceId,omitempty"`
	Details      map[string]interface{} `json:"details,omitempty"`
	IPAddress    string                 `json:"ipAddress,omitempty"`
	UserAgent    string                 `json:"userAgent,omitempty"`
}

// Log writes an audit entry to the database
func (l *Logger) Log(ctx context.Context, entry Entry) error {
	var detailsJSON []byte
	var err error

	if entry.Details != nil {
		detailsJSON, err = json.Marshal(entry.Details)
		if err != nil {
			log.Printf("Error marshaling audit details: %v", err)
			detailsJSON = []byte("{}")
		}
	}

	_, err = l.pool.Exec(ctx, `
		INSERT INTO audit_log (user_id, action, resource_type, resource_id, details, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, entry.UserID, entry.Action, entry.ResourceType, entry.ResourceID, detailsJSON, entry.IPAddress, entry.UserAgent)

	if err != nil {
		log.Printf("Error writing audit log: %v", err)
		return err
	}

	return nil
}

// LogFromContext logs an audit entry extracting common data from gin context
// P0-3 FIX: Capture all context values before spawning goroutine, use timeout context
func (l *Logger) LogFromContext(c *gin.Context, action, resourceType string, resourceID *uuid.UUID, details map[string]interface{}) {
	// P0-3 FIX: Capture all gin context values BEFORE the goroutine
	// to avoid accessing gin context from within the goroutine
	ipAddress := c.ClientIP()
	userAgent := c.GetHeader("User-Agent")

	entry := Entry{
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      details,
		IPAddress:    ipAddress,
		UserAgent:    userAgent,
	}

	// Get user ID if authenticated - capture before goroutine
	if userID, exists := c.Get("userId"); exists {
		if uid, ok := userID.(uuid.UUID); ok {
			entry.UserID = &uid
		}
	}

	// P0-3 FIX: Run audit logging in goroutine with timeout context
	// This ensures the HTTP response is not blocked by audit logging
	// and prevents goroutine leaks if the database is slow
	go func(entry Entry) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := l.Log(ctx, entry); err != nil {
			log.Printf("Failed to write audit log for action %s: %v", action, err)
		}
	}(entry)
}

// LogSecurityEvent is a convenience method for logging security-related events
func (l *Logger) LogSecurityEvent(c *gin.Context, action string, success bool, details map[string]interface{}) {
	if details == nil {
		details = make(map[string]interface{})
	}
	details["success"] = success

	l.LogFromContext(c, action, ResourceTypeSession, nil, details)
}

// LogDeviceAction logs device-related actions
func (l *Logger) LogDeviceAction(c *gin.Context, action string, deviceID uuid.UUID, details map[string]interface{}) {
	l.LogFromContext(c, action, ResourceTypeDevice, &deviceID, details)
}

// LogUserAction logs user management actions
func (l *Logger) LogUserAction(c *gin.Context, action string, targetUserID uuid.UUID, details map[string]interface{}) {
	l.LogFromContext(c, action, ResourceTypeUser, &targetUserID, details)
}

// LogAdminAction logs administrative actions
func (l *Logger) LogAdminAction(c *gin.Context, action, resourceType string, resourceID *uuid.UUID, details map[string]interface{}) {
	l.LogFromContext(c, action, resourceType, resourceID, details)
}
