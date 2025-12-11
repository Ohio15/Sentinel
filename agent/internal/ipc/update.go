// Package ipc provides inter-process communication primitives for the Sentinel agent
// and watchdog services, including update coordination via JSON state files and named pipes.
package ipc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Directory and file paths for update coordination
const (
	// BaseDir is the root directory for Sentinel data
	BaseDir = `C:\ProgramData\Sentinel`

	// UpdateDir contains update-related state files
	UpdateDir = `C:\ProgramData\Sentinel\update`

	// StagingDir is where downloaded updates are staged before installation
	StagingDir = `C:\ProgramData\Sentinel\update\staging`

	// PipeName is the named pipe for real-time agent-watchdog communication
	PipeName = `\\.\pipe\SentinelUpdate`

	// File names
	UpdateRequestFile = "update-request.json"
	UpdateStatusFile  = "update-status.json"
	AgentInfoFile     = "agent-info.json"
)

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
	StagedPath  string    `json:"staged_path"`
	Checksum    string    `json:"checksum"`
	RequestedAt time.Time `json:"requested_at"`
	RequestedBy string    `json:"requested_by"` // agent ID
	TargetPath  string    `json:"target_path"`  // path to executable being updated
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
}

// PipeMessage is used for real-time communication over the named pipe
type PipeMessage struct {
	Type    string `json:"type"`    // Message type: update_ready, update_complete, version_query, version_response
	Payload string `json:"payload"` // JSON-encoded data specific to message type
}

// Message types for named pipe communication
const (
	MsgUpdateReady    = "update_ready"
	MsgUpdateComplete = "update_complete"
	MsgVersionQuery   = "version_query"
	MsgVersionResp    = "version_response"
	MsgShutdown       = "shutdown"
)

// EnsureDirectories creates the necessary directories for update coordination
func EnsureDirectories() error {
	dirs := []string{BaseDir, UpdateDir, StagingDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
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
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write update request: %w", err)
	}

	return nil
}

// ReadUpdateRequest reads an update request from disk.
// Returns nil, nil if no request file exists.
func ReadUpdateRequest() (*UpdateRequest, error) {
	path := UpdateRequestPath()
	data, err := os.ReadFile(path)
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

// DeleteUpdateRequest removes the update request file
func DeleteUpdateRequest() error {
	path := UpdateRequestPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
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
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write update status: %w", err)
	}

	return nil
}

// ReadUpdateStatus reads an update status from disk.
// Returns nil, nil if no status file exists.
func ReadUpdateStatus() (*UpdateStatus, error) {
	path := UpdateStatusPath()
	data, err := os.ReadFile(path)
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

// DeleteUpdateStatus removes the update status file
func DeleteUpdateStatus() error {
	path := UpdateStatusPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
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
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write agent info: %w", err)
	}

	return nil
}

// ReadAgentInfo reads agent info from disk.
// Returns nil, nil if no info file exists.
func ReadAgentInfo() (*AgentInfo, error) {
	path := AgentInfoPath()
	data, err := os.ReadFile(path)
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

// StagingPath returns the path where a staged update should be stored
func StagingPath(version, platform, arch string) string {
	filename := fmt.Sprintf("sentinel-agent-%s-%s-%s.exe", version, platform, arch)
	return filepath.Join(StagingDir, filename)
}
