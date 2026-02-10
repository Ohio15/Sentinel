// Package installer provides utilities for installer error handling,
// reference ID generation, and installation logging.
package installer

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Error codes for installer operations
const (
	// E1xx: Installation errors
	ErrInstallGeneralFailure     = "E100"
	ErrExtractFailed             = "E101"
	ErrDiskSpaceInsufficient     = "E102"
	ErrPermissionDenied          = "E103"
	ErrPathNotFound              = "E104"
	ErrPathNotWritable           = "E105"
	ErrFileInUse                 = "E106"
	ErrBinaryCorrupt             = "E107"
	ErrChecksumMismatch          = "E108"
	ErrPlatformMismatch          = "E109"
	ErrPrerequisitesMissing      = "E110"
	ErrTempDirCreationFailed     = "E111"
	ErrDownloadFailed            = "E112"
	ErrTimeout                   = "E113"
	ErrCleanupFailed             = "E114"
	ErrVersionConflict           = "E115"

	// E2xx: Service errors
	ErrServiceGeneralFailure     = "E200"
	ErrServiceCreateFailed       = "E201"
	ErrServiceStartFailed        = "E202"
	ErrServiceStopFailed         = "E203"
	ErrServiceAlreadyExists      = "E204"
	ErrServiceNotFound           = "E205"
	ErrServiceTimeout            = "E206"
	ErrServiceDependencyFailed   = "E207"
	ErrServiceDeleteFailed       = "E208"
	ErrServiceAccessDenied       = "E209"
	ErrSystemdReloadFailed       = "E210"
	ErrLaunchdLoadFailed         = "E211"
	ErrLaunchdUnloadFailed       = "E212"
	ErrServiceRegistryError      = "E213"
	ErrServiceHealthCheckFailed  = "E214"

	// E3xx: Configuration errors
	ErrConfigGeneralFailure      = "E300"
	ErrConfigInvalidJSON         = "E301"
	ErrConfigMissingServer       = "E302"
	ErrConfigMissingToken        = "E303"
	ErrConfigWriteFailed         = "E304"
	ErrConfigReadFailed          = "E305"
	ErrConfigParseError          = "E306"
	ErrConfigInvalidServerURL    = "E307"
	ErrConfigInvalidToken        = "E308"
	ErrConfigFileLocked          = "E309"
	ErrConfigBackupFailed        = "E310"
	ErrConfigRestoreFailed       = "E311"
	ErrConfigMigrationFailed     = "E312"
	ErrConfigEncryptionFailed    = "E313"
	ErrConfigDecryptionFailed    = "E314"

	// E4xx: Network errors
	ErrNetworkGeneralFailure     = "E400"
	ErrServerUnreachable         = "E401"
	ErrTokenInvalid              = "E402"
	ErrTokenExpired              = "E403"
	ErrTokenMaxUses              = "E404"
	ErrSSLCertificateError       = "E405"
	ErrSSLHandshakeFailed        = "E406"
	ErrDNSResolutionFailed       = "E407"
	ErrConnectionTimeout         = "E408"
	ErrConnectionRefused         = "E409"
	ErrProxyError                = "E410"
	ErrHTTPError401              = "E411"
	ErrHTTPError403              = "E412"
	ErrHTTPError404              = "E413"
	ErrHTTPError500              = "E414"
	ErrDownloadInterrupted       = "E415"
	ErrEnrollmentFailed          = "E416"

	// E5xx: Upgrade errors
	ErrUpgradeGeneralFailure     = "E500"
	ErrUpgradeStopFailed         = "E501"
	ErrUpgradeBackupFailed       = "E502"
	ErrUpgradeRollbackFailed     = "E503"
	ErrUpgradeVersionDowngrade   = "E504"
	ErrUpgradeConfigMigration    = "E505"
	ErrUpgradeFilesInUse         = "E506"
	ErrUpgradeIncomplete         = "E507"
	ErrUpgradeCleanupFailed      = "E508"
	ErrUpgradeVerificationFailed = "E509"
	ErrUpgradePermissionChanged  = "E510"
	ErrUpgradeDatabaseMigration  = "E511"

	// E6xx: Uninstall errors
	ErrUninstallGeneralFailure   = "E600"
	ErrUninstallServicesRunning  = "E601"
	ErrUninstallFilesInUse       = "E602"
	ErrUninstallPermissionDenied = "E603"
	ErrUninstallRegistryCleanup  = "E604"
	ErrUninstallServiceDelete    = "E605"
	ErrUninstallFilesRemaining   = "E606"
	ErrUninstallConfigPreserved  = "E607"
	ErrUninstallLogPreserved     = "E608"
	ErrUninstallIncomplete       = "E609"
)

// InstallerError represents an error with a reference ID and error code
type InstallerError struct {
	Code        string    `json:"code"`
	Message     string    `json:"message"`
	ReferenceID string    `json:"reference_id"`
	Details     string    `json:"details,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

func (e *InstallerError) Error() string {
	return fmt.Sprintf("[%s] %s (Ref: %s)", e.Code, e.Message, e.ReferenceID)
}

// NewInstallerError creates a new installer error with automatic reference ID
func NewInstallerError(code, message string) *InstallerError {
	return &InstallerError{
		Code:        code,
		Message:     message,
		ReferenceID: GenerateReferenceID(),
		Timestamp:   time.Now().UTC(),
	}
}

// NewInstallerErrorWithDetails creates an error with additional details
func NewInstallerErrorWithDetails(code, message, details string) *InstallerError {
	err := NewInstallerError(code, message)
	err.Details = details
	return err
}

// GenerateReferenceID creates a unique reference ID in the format INS-XXXXXX-YYYYMMDD
// The format includes:
//   - INS prefix for installer operations
//   - 6 random hexadecimal characters
//   - Date stamp in YYYYMMDD format
func GenerateReferenceID() string {
	// Generate 3 random bytes (6 hex characters)
	randomBytes := make([]byte, 3)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("INS-%06X-%s",
			time.Now().UnixNano()&0xFFFFFF,
			time.Now().Format("20060102"))
	}

	randomHex := strings.ToUpper(hex.EncodeToString(randomBytes))
	dateStamp := time.Now().Format("20060102")

	return fmt.Sprintf("INS-%s-%s", randomHex, dateStamp)
}

// GenerateReferenceIDWithPrefix creates a reference ID with a custom prefix
func GenerateReferenceIDWithPrefix(prefix string) string {
	randomBytes := make([]byte, 3)
	if _, err := rand.Read(randomBytes); err != nil {
		return fmt.Sprintf("%s-%06X-%s",
			prefix,
			time.Now().UnixNano()&0xFFFFFF,
			time.Now().Format("20060102"))
	}

	randomHex := strings.ToUpper(hex.EncodeToString(randomBytes))
	dateStamp := time.Now().Format("20060102")

	return fmt.Sprintf("%s-%s-%s", prefix, randomHex, dateStamp)
}

// LogLevel represents log severity levels
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	Level       LogLevel  `json:"level"`
	Message     string    `json:"message"`
	ReferenceID string    `json:"reference_id,omitempty"`
	ErrorCode   string    `json:"error_code,omitempty"`
}

// InstallerLog tracks installation progress and provides logging
type InstallerLog struct {
	ReferenceID  string      `json:"reference_id"`
	StartTime    time.Time   `json:"start_time"`
	EndTime      time.Time   `json:"end_time,omitempty"`
	Platform     string      `json:"platform"`
	Arch         string      `json:"arch"`
	Version      string      `json:"version"`
	Success      bool        `json:"success"`
	ErrorCode    string      `json:"error_code,omitempty"`
	ErrorMessage string      `json:"error_message,omitempty"`
	Steps        []StepEntry `json:"steps"`
	LogFilePath  string      `json:"log_file_path"`

	mu       sync.Mutex
	logFile  *os.File
	minLevel LogLevel
}

// StepEntry represents a single installation step
type StepEntry struct {
	Name      string    `json:"name"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time,omitempty"`
	Status    string    `json:"status"` // "pending", "running", "success", "failed", "skipped"
	Message   string    `json:"message,omitempty"`
}

// NewInstallerLog creates a new installer log with a unique reference ID
func NewInstallerLog(platform, arch, version string) *InstallerLog {
	return &InstallerLog{
		ReferenceID: GenerateReferenceID(),
		StartTime:   time.Now().UTC(),
		Platform:    platform,
		Arch:        arch,
		Version:     version,
		Steps:       make([]StepEntry, 0),
		minLevel:    LogLevelInfo,
	}
}

// SetLogLevel sets the minimum log level
func (l *InstallerLog) SetLogLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.minLevel = level
}

// InitLogFile initializes the log file for writing
func (l *InstallerLog) InitLogFile(logDir string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Create log directory if it doesn't exist
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Create log file with reference ID in name
	logFileName := fmt.Sprintf("install-%s.log", l.ReferenceID)
	l.LogFilePath = filepath.Join(logDir, logFileName)

	file, err := os.OpenFile(l.LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	l.logFile = file

	// Write header
	header := fmt.Sprintf(`========================================
Sentinel Agent Installer Log
Reference ID: %s
Started: %s
Platform: %s
Architecture: %s
Version: %s
========================================

`, l.ReferenceID, l.StartTime.Format(time.RFC3339), l.Platform, l.Arch, l.Version)

	if _, err := l.logFile.WriteString(header); err != nil {
		return fmt.Errorf("failed to write log header: %w", err)
	}

	return nil
}

// Close closes the log file
func (l *InstallerLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.logFile != nil {
		// Write footer
		l.EndTime = time.Now().UTC()
		duration := l.EndTime.Sub(l.StartTime)

		footer := fmt.Sprintf(`
========================================
Installation %s
Duration: %s
Reference ID: %s
========================================
`, map[bool]string{true: "COMPLETED", false: "FAILED"}[l.Success], duration, l.ReferenceID)

		l.logFile.WriteString(footer)
		return l.logFile.Close()
	}
	return nil
}

// Log writes a log entry
func (l *InstallerLog) Log(level LogLevel, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if level < l.minLevel {
		return
	}

	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logLine := fmt.Sprintf("%s | %-5s | %s\n", timestamp, level.String(), message)

	if l.logFile != nil {
		l.logFile.WriteString(logLine)
	}

	// Also print to stdout for interactive installs
	fmt.Print(logLine)
}

// Debug logs a debug message
func (l *InstallerLog) Debug(format string, args ...interface{}) {
	l.Log(LogLevelDebug, format, args...)
}

// Info logs an info message
func (l *InstallerLog) Info(format string, args ...interface{}) {
	l.Log(LogLevelInfo, format, args...)
}

// Warn logs a warning message
func (l *InstallerLog) Warn(format string, args ...interface{}) {
	l.Log(LogLevelWarn, format, args...)
}

// Error logs an error message with optional error code
func (l *InstallerLog) Error(format string, args ...interface{}) {
	l.Log(LogLevelError, format, args...)
}

// LogError logs an InstallerError
func (l *InstallerLog) LogError(err *InstallerError) {
	l.mu.Lock()
	l.ErrorCode = err.Code
	l.ErrorMessage = err.Message
	l.mu.Unlock()

	l.Error("[%s] %s", err.Code, err.Message)
	if err.Details != "" {
		l.Error("  Details: %s", err.Details)
	}
}

// StartStep records the start of an installation step
func (l *InstallerLog) StartStep(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	step := StepEntry{
		Name:      name,
		StartTime: time.Now().UTC(),
		Status:    "running",
	}
	l.Steps = append(l.Steps, step)

	l.Log(LogLevelInfo, "Step: %s - Started", name)
}

// CompleteStep marks the current step as complete
func (l *InstallerLog) CompleteStep(message string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.Steps) == 0 {
		return
	}

	idx := len(l.Steps) - 1
	l.Steps[idx].EndTime = time.Now().UTC()
	l.Steps[idx].Status = "success"
	l.Steps[idx].Message = message

	l.Log(LogLevelInfo, "Step: %s - Completed: %s", l.Steps[idx].Name, message)
}

// FailStep marks the current step as failed
func (l *InstallerLog) FailStep(err *InstallerError) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.Steps) == 0 {
		return
	}

	idx := len(l.Steps) - 1
	l.Steps[idx].EndTime = time.Now().UTC()
	l.Steps[idx].Status = "failed"
	l.Steps[idx].Message = err.Message

	l.Log(LogLevelError, "Step: %s - Failed [%s]: %s", l.Steps[idx].Name, err.Code, err.Message)
}

// SkipStep marks the current step as skipped
func (l *InstallerLog) SkipStep(reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.Steps) == 0 {
		return
	}

	idx := len(l.Steps) - 1
	l.Steps[idx].EndTime = time.Now().UTC()
	l.Steps[idx].Status = "skipped"
	l.Steps[idx].Message = reason

	l.Log(LogLevelInfo, "Step: %s - Skipped: %s", l.Steps[idx].Name, reason)
}

// SetSuccess marks the installation as successful
func (l *InstallerLog) SetSuccess(success bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Success = success
}

// GetSummary returns a summary of the installation
func (l *InstallerLog) GetSummary() map[string]interface{} {
	l.mu.Lock()
	defer l.mu.Unlock()

	totalSteps := len(l.Steps)
	successSteps := 0
	failedSteps := 0
	skippedSteps := 0

	for _, step := range l.Steps {
		switch step.Status {
		case "success":
			successSteps++
		case "failed":
			failedSteps++
		case "skipped":
			skippedSteps++
		}
	}

	duration := time.Duration(0)
	if !l.EndTime.IsZero() {
		duration = l.EndTime.Sub(l.StartTime)
	} else {
		duration = time.Since(l.StartTime)
	}

	return map[string]interface{}{
		"reference_id":  l.ReferenceID,
		"platform":      l.Platform,
		"arch":          l.Arch,
		"version":       l.Version,
		"success":       l.Success,
		"duration":      duration.String(),
		"total_steps":   totalSteps,
		"success_steps": successSteps,
		"failed_steps":  failedSteps,
		"skipped_steps": skippedSteps,
		"error_code":    l.ErrorCode,
		"error_message": l.ErrorMessage,
		"log_file":      l.LogFilePath,
	}
}

// ErrorCodeDescription returns a human-readable description for an error code
func ErrorCodeDescription(code string) string {
	descriptions := map[string]string{
		// E1xx
		ErrInstallGeneralFailure:     "General installation failure",
		ErrExtractFailed:             "Failed to extract installation files",
		ErrDiskSpaceInsufficient:     "Insufficient disk space",
		ErrPermissionDenied:          "Permission denied",
		ErrPathNotFound:              "Installation path not found",
		ErrPathNotWritable:           "Installation path is not writable",
		ErrFileInUse:                 "Installation files are in use",
		ErrBinaryCorrupt:             "Downloaded binary is corrupted",
		ErrChecksumMismatch:          "File checksum verification failed",
		ErrPlatformMismatch:          "Installer architecture mismatch",
		ErrPrerequisitesMissing:      "Required system components missing",
		ErrTempDirCreationFailed:     "Cannot create temporary directory",
		ErrDownloadFailed:            "Failed to download installer components",
		ErrTimeout:                   "Installation timed out",
		ErrCleanupFailed:             "Failed to clean up temporary files",
		ErrVersionConflict:           "Incompatible version detected",

		// E2xx
		ErrServiceGeneralFailure:     "General service operation failure",
		ErrServiceCreateFailed:       "Failed to create service",
		ErrServiceStartFailed:        "Failed to start service",
		ErrServiceStopFailed:         "Failed to stop service",
		ErrServiceAlreadyExists:      "Service with same name already exists",
		ErrServiceNotFound:           "Service does not exist",
		ErrServiceTimeout:            "Service operation timed out",
		ErrServiceDependencyFailed:   "Service dependency not met",
		ErrServiceDeleteFailed:       "Failed to delete service",
		ErrServiceAccessDenied:       "Insufficient privileges for service operation",
		ErrSystemdReloadFailed:       "Failed to reload systemd daemon",
		ErrLaunchdLoadFailed:         "Failed to load launchd plist",
		ErrLaunchdUnloadFailed:       "Failed to unload launchd plist",
		ErrServiceRegistryError:      "Windows service registry error",
		ErrServiceHealthCheckFailed:  "Service started but health check failed",

		// E3xx
		ErrConfigGeneralFailure:      "General configuration error",
		ErrConfigInvalidJSON:         "Configuration file is not valid JSON",
		ErrConfigMissingServer:       "Server URL not specified",
		ErrConfigMissingToken:        "Enrollment token not specified",
		ErrConfigWriteFailed:         "Failed to write configuration file",
		ErrConfigReadFailed:          "Failed to read configuration file",
		ErrConfigParseError:          "Failed to parse configuration",
		ErrConfigInvalidServerURL:    "Server URL format is invalid",
		ErrConfigInvalidToken:        "Enrollment token format invalid",
		ErrConfigFileLocked:          "Configuration file is locked",
		ErrConfigBackupFailed:        "Failed to backup existing configuration",
		ErrConfigRestoreFailed:       "Failed to restore configuration from backup",
		ErrConfigMigrationFailed:     "Failed to migrate configuration schema",
		ErrConfigEncryptionFailed:    "Failed to encrypt sensitive config data",
		ErrConfigDecryptionFailed:    "Failed to decrypt configuration",

		// E4xx
		ErrNetworkGeneralFailure:     "General network error",
		ErrServerUnreachable:         "Cannot reach Sentinel server",
		ErrTokenInvalid:              "Enrollment token is invalid",
		ErrTokenExpired:              "Enrollment token has expired",
		ErrTokenMaxUses:              "Enrollment token has reached maximum uses",
		ErrSSLCertificateError:       "SSL/TLS certificate verification failed",
		ErrSSLHandshakeFailed:        "TLS handshake failed",
		ErrDNSResolutionFailed:       "Cannot resolve server hostname",
		ErrConnectionTimeout:         "Connection to server timed out",
		ErrConnectionRefused:         "Server actively refused connection",
		ErrProxyError:                "Proxy configuration error",
		ErrHTTPError401:              "Authentication failed (Unauthorized)",
		ErrHTTPError403:              "Access forbidden",
		ErrHTTPError404:              "Endpoint not found",
		ErrHTTPError500:              "Server internal error",
		ErrDownloadInterrupted:       "Download was interrupted",
		ErrEnrollmentFailed:          "Device enrollment failed",

		// E5xx
		ErrUpgradeGeneralFailure:     "General upgrade failure",
		ErrUpgradeStopFailed:         "Failed to stop existing services",
		ErrUpgradeBackupFailed:       "Failed to backup existing installation",
		ErrUpgradeRollbackFailed:     "Failed to rollback after upgrade failure",
		ErrUpgradeVersionDowngrade:   "Cannot downgrade to older version",
		ErrUpgradeConfigMigration:    "Configuration migration required",
		ErrUpgradeFilesInUse:         "Upgrade files are in use",
		ErrUpgradeIncomplete:         "Previous upgrade was incomplete",
		ErrUpgradeCleanupFailed:      "Failed to clean up old version",
		ErrUpgradeVerificationFailed: "Post-upgrade verification failed",
		ErrUpgradePermissionChanged:  "File permissions changed during upgrade",
		ErrUpgradeDatabaseMigration:  "Local database migration failed",

		// E6xx
		ErrUninstallGeneralFailure:   "General uninstall failure",
		ErrUninstallServicesRunning:  "Cannot uninstall while services are running",
		ErrUninstallFilesInUse:       "Installation files are in use",
		ErrUninstallPermissionDenied: "Insufficient permissions for uninstall",
		ErrUninstallRegistryCleanup:  "Failed to clean registry entries",
		ErrUninstallServiceDelete:    "Failed to delete service",
		ErrUninstallFilesRemaining:   "Some files could not be deleted",
		ErrUninstallConfigPreserved:  "Configuration files were preserved",
		ErrUninstallLogPreserved:     "Log files were preserved",
		ErrUninstallIncomplete:       "Uninstallation was incomplete",
	}

	if desc, ok := descriptions[code]; ok {
		return desc
	}
	return "Unknown error code"
}
