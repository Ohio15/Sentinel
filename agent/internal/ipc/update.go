// Package ipc provides inter-process communication primitives for the Sentinel agent
// and watchdog services, including update coordination via JSON state files and named pipes.
package ipc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// PipeName is the named pipe for real-time agent-watchdog communication (Windows only)
const PipeName = `\\.\pipe\SentinelUpdate`

// File names for update coordination
const (
	UpdateRequestFile         = "update-request.json"
	UpdateStatusFile          = "update-status.json"
	AgentInfoFile             = "agent-info.json"
	WatchdogUpdateRequestFile = "watchdog-update-request.json"
	WatchdogUpdateStatusFile  = "watchdog-update-status.json"
	WatchdogInfoFile          = "watchdog-info.json"
)

// Directory paths for update coordination - set per-OS at init time
var (
	BaseDir    string
	UpdateDir  string
	StagingDir string
)

func init() {
	if runtime.GOOS == "windows" {
		BaseDir = `C:\ProgramData\Sentinel`
	} else {
		BaseDir = "/var/lib/sentinel"
	}
	UpdateDir = filepath.Join(BaseDir, "update")
	StagingDir = filepath.Join(UpdateDir, "staging")
}

// UpdateState represents the current state of an update operation
type UpdateState string

const (
	StatePending    UpdateState = "pending"
	StateApplying   UpdateState = "applying"
	StateComplete   UpdateState = "complete"
	StateFailed     UpdateState = "failed"
	StateRolledBack UpdateState = "rolled_back"
)

// UpdateRequest is written by the agent when an update is downloaded and ready to apply.
// The watchdog reads this file to know when to perform an update.
type UpdateRequest struct {
	Version     string    `json:"version"`
	Platform    string    `json:"platform"` // required to rebuild the signed manifest
	Arch        string    `json:"arch"`     // required to rebuild the signed manifest
	StagedPath  string    `json:"staged_path"`
	Checksum    string    `json:"checksum"` // lowercase hex sha256 of the staged bytes
	RequestedAt time.Time `json:"requested_at"`
	RequestedBy string    `json:"requested_by"` // agent ID
	TargetPath  string    `json:"target_path"`  // path to executable being updated

	// Signature is the base64-encoded Ed25519 signature over the CANONICAL
	// MANIFEST (version+platform+arch+sha256+signedDowngrade), not the bare
	// bytes. The watchdog rebuilds the manifest from these fields and the sha256
	// of the staged bytes, then verifies it against the embedded public key
	// immediately before swapping (C1). Empty signature is rejected.
	Signature string `json:"signature"`

	// SignedDowngrade, when true, authorizes applying a target version that is
	// not strictly greater than the current version. It is trustworthy ONLY
	// because it is one of the fields covered by the manifest signature — the
	// watchdog must read it back only after VerifyManifest succeeds (C2/AG-H4).
	SignedDowngrade bool `json:"signed_downgrade,omitempty"`
}

// UpdateStatus is written by the watchdog to report update progress and outcome.
// The agent reads this on startup to report the result to the server.
type UpdateStatus struct {
	State         UpdateState `json:"state"`
	Version       string      `json:"version"`
	PreviousVer   string      `json:"previous_version,omitempty"`
	StartedAt     time.Time   `json:"started_at,omitempty"`
	CompletedAt   time.Time   `json:"completed_at,omitempty"`
	Error         string      `json:"error,omitempty"`
	RolledBack    bool        `json:"rolled_back,omitempty"`
	BackupPath    string      `json:"backup_path,omitempty"`
	AttemptCount  int         `json:"attempt_count,omitempty"`
}

// AgentInfo is written by the agent on startup to report its version and status.
// The watchdog reads this to verify an update was successful.
type AgentInfo struct {
	Version   string    `json:"version"`
	StartedAt time.Time `json:"started_at"`
	PID       int       `json:"pid"`
	AgentID   string    `json:"agent_id,omitempty"`
	LastSeen  time.Time `json:"last_seen,omitempty"`
}

// PipeMessage is used for real-time communication over the named pipe
type PipeMessage struct {
	Type    string `json:"type"`    // Message type: update_ready, update_complete, version_query, version_response
	Payload string `json:"payload"` // JSON-encoded data specific to message type
}

// Message types for named pipe communication
const (
	MsgUpdateReady          = "update_ready"
	MsgUpdateComplete       = "update_complete"
	MsgVersionQuery         = "version_query"
	MsgVersionResp          = "version_response"
	MsgShutdown             = "shutdown"
	MsgWatchdogUpdateReady  = "watchdog_update_ready"
	MsgWatchdogVersionQuery = "watchdog_version_query"
	MsgAlertRelay           = "alert_relay" // Watchdog asks agent to relay an alert to server
)

// AlertFile is the file name for pending alerts from the watchdog
const AlertFile = "pending-alert.json"

// AlertRelayPayload is used by the watchdog to request the agent send an alert to the server
type AlertRelayPayload struct {
	Severity  string    `json:"severity"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

// AlertFilePath returns the full path to the pending alert file
func AlertFilePath() string {
	return filepath.Join(UpdateDir, AlertFile)
}

// WriteAlert writes a pending alert for the agent to relay to the server
func WriteAlert(alert *AlertRelayPayload) error {
	if err := EnsureDirectories(); err != nil {
		return err
	}
	if alert.CreatedAt.IsZero() {
		alert.CreatedAt = time.Now()
	}
	data, err := json.MarshalIndent(alert, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}
	return secureWriteAndSign(AlertFilePath(), data)
}

// ReadAndDeleteAlert reads a pending alert and removes the file.
// Returns nil, nil if no pending alert exists.
// Verifies HMAC integrity before processing.
func ReadAndDeleteAlert() (*AlertRelayPayload, error) {
	path := AlertFilePath()
	data, err := secureReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read alert file: %w", err)
	}
	// Remove the file and its signature immediately to prevent duplicate processing
	deleteWithSignature(path)

	var alert AlertRelayPayload
	if err := json.Unmarshal(data, &alert); err != nil {
		return nil, fmt.Errorf("failed to unmarshal alert: %w", err)
	}
	return &alert, nil
}

// WatchdogUpdateRequest is written when a watchdog update is ready to apply.
// The watchdog reads this file and uses Task Scheduler to update itself.
type WatchdogUpdateRequest struct {
	Version     string    `json:"version"`
	Platform    string    `json:"platform"` // required to rebuild the signed manifest
	Arch        string    `json:"arch"`     // required to rebuild the signed manifest
	StagedPath  string    `json:"staged_path"`
	Checksum    string    `json:"checksum"` // lowercase hex sha256 of the staged bytes
	RequestedAt time.Time `json:"requested_at"`
	RequestedBy string    `json:"requested_by"` // agent ID or "server"
	TargetPath  string    `json:"target_path"`  // path to watchdog executable

	// Signature is the base64-encoded Ed25519 signature over the CANONICAL
	// MANIFEST (version+platform+arch+sha256+signedDowngrade), not the bare
	// bytes. Rebuilt and verified against the embedded public key before the
	// self-update swap (C1 / WD-H2). Empty is rejected.
	Signature string `json:"signature"`

	// SignedDowngrade authorizes a non-upgrade target when set. Trustworthy only
	// because it is covered by the manifest signature (C2/AG-H4).
	SignedDowngrade bool `json:"signed_downgrade,omitempty"`
}

// WatchdogUpdateStatus tracks the state of a watchdog self-update operation
type WatchdogUpdateStatus struct {
	State         UpdateState `json:"state"`
	Version       string      `json:"version"`
	PreviousVer   string      `json:"previous_version,omitempty"`
	StartedAt     time.Time   `json:"started_at,omitempty"`
	CompletedAt   time.Time   `json:"completed_at,omitempty"`
	Error         string      `json:"error,omitempty"`
	RolledBack    bool        `json:"rolled_back,omitempty"`
	BackupPath    string      `json:"backup_path,omitempty"`
	AttemptCount  int         `json:"attempt_count,omitempty"`
}

// WatchdogInfo is written by the watchdog on startup to report its version and status
type WatchdogInfo struct {
	Version   string    `json:"version"`
	StartedAt time.Time `json:"started_at"`
	PID       int       `json:"pid"`
}

// EnsureDirectories creates the necessary directories for update coordination
// with restrictive permissions (0700 + Windows ACLs for SYSTEM/Admins only).
func EnsureDirectories() error {
	dirs := []string{BaseDir, UpdateDir, StagingDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		secureDirectory(dir)
	}
	return nil
}

// UpdateRequestPath returns the full path to the update request file
func UpdateRequestPath() string {
	return filepath.Join(UpdateDir, UpdateRequestFile)
}

// UpdateStatusPath returns the full path to the update status file
func UpdateStatusPath() string {
	return filepath.Join(UpdateDir, UpdateStatusFile)
}

// AgentInfoPath returns the full path to the agent info file
func AgentInfoPath() string {
	return filepath.Join(BaseDir, AgentInfoFile)
}

// WatchdogUpdateRequestPath returns the full path to the watchdog update request file
func WatchdogUpdateRequestPath() string {
	return filepath.Join(UpdateDir, WatchdogUpdateRequestFile)
}

// WatchdogUpdateStatusPath returns the full path to the watchdog update status file
func WatchdogUpdateStatusPath() string {
	return filepath.Join(UpdateDir, WatchdogUpdateStatusFile)
}

// WatchdogInfoPath returns the full path to the watchdog info file
func WatchdogInfoPath() string {
	return filepath.Join(BaseDir, WatchdogInfoFile)
}

// WriteUpdateRequest writes an update request to disk
func WriteUpdateRequest(req *UpdateRequest) error {
	if err := EnsureDirectories(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal update request: %w", err)
	}

	path := UpdateRequestPath()
	if err := secureWriteAndSign(path, data); err != nil {
		return fmt.Errorf("failed to write update request: %w", err)
	}

	return nil
}

// ReadUpdateRequest reads an update request from disk.
// Returns nil, nil if no request file exists.
// Verifies HMAC integrity before processing.
func ReadUpdateRequest() (*UpdateRequest, error) {
	path := UpdateRequestPath()
	data, err := secureReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read update request: %w", err)
	}

	var req UpdateRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("failed to unmarshal update request: %w", err)
	}

	return &req, nil
}

// DeleteUpdateRequest removes the update request file and its signature
func DeleteUpdateRequest() error {
	path := UpdateRequestPath()
	if err := deleteWithSignature(path); err != nil {
		return fmt.Errorf("failed to delete update request: %w", err)
	}
	return nil
}

// WriteUpdateStatus writes an update status to disk
func WriteUpdateStatus(status *UpdateStatus) error {
	if err := EnsureDirectories(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal update status: %w", err)
	}

	path := UpdateStatusPath()
	if err := secureWriteAndSign(path, data); err != nil {
		return fmt.Errorf("failed to write update status: %w", err)
	}

	return nil
}

// ReadUpdateStatus reads an update status from disk.
// Returns nil, nil if no status file exists.
// Verifies HMAC integrity before processing.
func ReadUpdateStatus() (*UpdateStatus, error) {
	path := UpdateStatusPath()
	data, err := secureReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read update status: %w", err)
	}

	var status UpdateStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal update status: %w", err)
	}

	return &status, nil
}

// DeleteUpdateStatus removes the update status file and its signature
func DeleteUpdateStatus() error {
	path := UpdateStatusPath()
	if err := deleteWithSignature(path); err != nil {
		return fmt.Errorf("failed to delete update status: %w", err)
	}
	return nil
}

// WriteAgentInfo writes agent info to disk
func WriteAgentInfo(info *AgentInfo) error {
	if err := EnsureDirectories(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal agent info: %w", err)
	}

	path := AgentInfoPath()
	if err := secureWriteAndSign(path, data); err != nil {
		return fmt.Errorf("failed to write agent info: %w", err)
	}

	return nil
}

// ReadAgentInfo reads agent info from disk.
// Returns nil, nil if no info file exists.
// Verifies HMAC integrity before processing.
func ReadAgentInfo() (*AgentInfo, error) {
	path := AgentInfoPath()
	data, err := secureReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read agent info: %w", err)
	}

	var info AgentInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent info: %w", err)
	}

	return &info, nil
}

// CleanupStagingDir removes all files from the staging directory
func CleanupStagingDir() error {
	entries, err := os.ReadDir(StagingDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read staging dir: %w", err)
	}

	for _, entry := range entries {
		path := filepath.Join(StagingDir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}
	}

	return nil
}

// safeStagingComponent sanitizes an untrusted version/platform/arch value
// before it is interpolated into a staging filename. The download path opens
// (and truncates) the staging temp file BEFORE the signature is verified, so an
// attacker-controlled version-check response must not be able to steer that
// write outside StagingDir. We keep only [A-Za-z0-9._-] and neutralize any ".."
// sequence, guaranteeing filepath.Join cannot escape the staging directory.
// Content authenticity is still enforced separately by the signature check; this
// only constrains where the pre-verification bytes may land.
func safeStagingComponent(s string) string {
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == '.' || r == '_' || r == '-':
			return r
		default:
			return '_'
		}
	}, s)
	mapped = strings.ReplaceAll(mapped, "..", "__")
	if mapped == "" {
		return "unknown"
	}
	return mapped
}

// StagingPath returns the path where a staged update should be stored
func StagingPath(version, platform, arch string) string {
	version, platform, arch = safeStagingComponent(version), safeStagingComponent(platform), safeStagingComponent(arch)
	var filename string
	if runtime.GOOS == "windows" {
		filename = fmt.Sprintf("sentinel-agent-%s-%s-%s.exe", version, platform, arch)
	} else {
		filename = fmt.Sprintf("sentinel-agent-%s-%s-%s", version, platform, arch)
	}
	return filepath.Join(StagingDir, filename)
}

// WatchdogStagingPath returns the path where a staged watchdog update should be stored
func WatchdogStagingPath(version, platform, arch string) string {
	version, platform, arch = safeStagingComponent(version), safeStagingComponent(platform), safeStagingComponent(arch)
	var filename string
	if runtime.GOOS == "windows" {
		filename = fmt.Sprintf("sentinel-watchdog-%s-%s-%s.exe", version, platform, arch)
	} else {
		filename = fmt.Sprintf("sentinel-watchdog-%s-%s-%s", version, platform, arch)
	}
	return filepath.Join(StagingDir, filename)
}

// WriteWatchdogUpdateRequest writes a watchdog update request to disk
func WriteWatchdogUpdateRequest(req *WatchdogUpdateRequest) error {
	if err := EnsureDirectories(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal watchdog update request: %w", err)
	}

	path := WatchdogUpdateRequestPath()
	if err := secureWriteAndSign(path, data); err != nil {
		return fmt.Errorf("failed to write watchdog update request: %w", err)
	}

	return nil
}

// ReadWatchdogUpdateRequest reads a watchdog update request from disk.
// Returns nil, nil if no request file exists.
// Verifies HMAC integrity before processing.
func ReadWatchdogUpdateRequest() (*WatchdogUpdateRequest, error) {
	path := WatchdogUpdateRequestPath()
	data, err := secureReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read watchdog update request: %w", err)
	}

	var req WatchdogUpdateRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("failed to unmarshal watchdog update request: %w", err)
	}

	return &req, nil
}

// DeleteWatchdogUpdateRequest removes the watchdog update request file and its signature
func DeleteWatchdogUpdateRequest() error {
	path := WatchdogUpdateRequestPath()
	if err := deleteWithSignature(path); err != nil {
		return fmt.Errorf("failed to delete watchdog update request: %w", err)
	}
	return nil
}

// WriteWatchdogUpdateStatus writes a watchdog update status to disk
func WriteWatchdogUpdateStatus(status *WatchdogUpdateStatus) error {
	if err := EnsureDirectories(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal watchdog update status: %w", err)
	}

	path := WatchdogUpdateStatusPath()
	if err := secureWriteAndSign(path, data); err != nil {
		return fmt.Errorf("failed to write watchdog update status: %w", err)
	}

	return nil
}

// ReadWatchdogUpdateStatus reads a watchdog update status from disk.
// Returns nil, nil if no status file exists.
// Verifies HMAC integrity before processing.
func ReadWatchdogUpdateStatus() (*WatchdogUpdateStatus, error) {
	path := WatchdogUpdateStatusPath()
	data, err := secureReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read watchdog update status: %w", err)
	}

	var status WatchdogUpdateStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal watchdog update status: %w", err)
	}

	return &status, nil
}

// DeleteWatchdogUpdateStatus removes the watchdog update status file and its signature
func DeleteWatchdogUpdateStatus() error {
	path := WatchdogUpdateStatusPath()
	if err := deleteWithSignature(path); err != nil {
		return fmt.Errorf("failed to delete watchdog update status: %w", err)
	}
	return nil
}

// WriteWatchdogInfo writes watchdog info to disk
func WriteWatchdogInfo(info *WatchdogInfo) error {
	if err := EnsureDirectories(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal watchdog info: %w", err)
	}

	path := WatchdogInfoPath()
	if err := secureWriteAndSign(path, data); err != nil {
		return fmt.Errorf("failed to write watchdog info: %w", err)
	}

	return nil
}

// ReadWatchdogInfo reads watchdog info from disk.
// Returns nil, nil if no info file exists.
// Verifies HMAC integrity before processing.
func ReadWatchdogInfo() (*WatchdogInfo, error) {
	path := WatchdogInfoPath()
	data, err := secureReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read watchdog info: %w", err)
	}

	var info WatchdogInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal watchdog info: %w", err)
	}

	return &info, nil
}
