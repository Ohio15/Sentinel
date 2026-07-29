// Package selfupdate provides watchdog self-update capability using Windows Task Scheduler.
// Since the watchdog cannot replace its own binary while running, we use a scheduled task
// that runs independently to perform the update.
package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sentinel/agent/internal/ipc"
	"github.com/sentinel/agent/internal/updatesig"
)

const (
	// TaskName is the name of the scheduled task for watchdog updates
	TaskName = "SentinelWatchdogUpdate"

	// ScriptFileName is the name of the update batch script
	ScriptFileName = "watchdog-update.bat"

	// MaxRetries is the maximum number of update attempts
	MaxRetries = 3

	// RetryDelay is the delay between retry attempts
	RetryDelay = 5 * time.Second
)

// SelfUpdater handles watchdog self-update operations
type SelfUpdater struct {
	serviceName     string
	currentVersion  string
	installPath     string
	executablePath  string
	updateInProgress bool
}

// New creates a new SelfUpdater instance
func New(serviceName, currentVersion, installPath string) (*SelfUpdater, error) {
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}

	return &SelfUpdater{
		serviceName:    serviceName,
		currentVersion: currentVersion,
		installPath:    installPath,
		executablePath: exePath,
	}, nil
}

// CheckForPendingUpdate checks if there's a pending watchdog update request
func (s *SelfUpdater) CheckForPendingUpdate() (*ipc.WatchdogUpdateRequest, error) {
	return ipc.ReadWatchdogUpdateRequest()
}

// ApplySelfUpdate initiates the self-update process using Task Scheduler
func (s *SelfUpdater) ApplySelfUpdate(request *ipc.WatchdogUpdateRequest) error {
	if s.updateInProgress {
		return fmt.Errorf("update already in progress")
	}
	s.updateInProgress = true
	defer func() { s.updateInProgress = false }()

	log.Printf("[SelfUpdate] Starting self-update to version %s", request.Version)

	// Write status: applying
	status := &ipc.WatchdogUpdateStatus{
		State:       ipc.StateApplying,
		Version:     request.Version,
		PreviousVer: s.currentVersion,
		StartedAt:   time.Now(),
	}
	if err := ipc.WriteWatchdogUpdateStatus(status); err != nil {
		log.Printf("[SelfUpdate] Warning: failed to write status: %v", err)
	}

	// Step 1: Verify the staged file
	if err := s.verifyStagedFile(request); err != nil {
		s.failUpdate(status, fmt.Sprintf("staged file verification failed: %v", err))
		return err
	}
	log.Printf("[SelfUpdate] Staged file verified: %s", request.StagedPath)

	// Step 2: Create the update batch script
	scriptPath, err := s.createUpdateScript(request)
	if err != nil {
		s.failUpdate(status, fmt.Sprintf("failed to create update script: %v", err))
		return err
	}
	log.Printf("[SelfUpdate] Update script created: %s", scriptPath)

	// Step 3: Create and run the scheduled task
	if err := s.createAndRunScheduledTask(scriptPath); err != nil {
		s.failUpdate(status, fmt.Sprintf("failed to create scheduled task: %v", err))
		return err
	}
	log.Printf("[SelfUpdate] Scheduled task created and triggered")

	// The watchdog will be stopped by the scheduled task
	// When it restarts, it will check the update status
	log.Printf("[SelfUpdate] Update initiated, watchdog will restart with new version")
	return nil
}

// verifyStagedFile verifies the staged watchdog update file: its paths are
// constrained, and — the trust anchor — its Ed25519 signature validates over the
// exact staged bytes against the public key embedded in this binary. This is the
// last gate before the watchdog self-update swap (RW-1 / WD-H2, WD-H4). Empty or
// invalid signatures are rejected (fail closed); there is no checksum-only path.
func (s *SelfUpdater) verifyStagedFile(request *ipc.WatchdogUpdateRequest) error {
	// WD-H4 / AG-H5: path constraints. Staged file must live inside the staging
	// directory; target must be exactly this watchdog's executable path.
	stagedClean := filepath.Clean(request.StagedPath)
	stagingRoot := filepath.Clean(ipc.StagingDir)
	if stagedClean != stagingRoot && !strings.HasPrefix(stagedClean, stagingRoot+string(os.PathSeparator)) {
		return fmt.Errorf("staged path %q is outside the staging directory %q — rejecting", stagedClean, stagingRoot)
	}
	expectedTarget := filepath.Clean(s.executablePath)
	if request.TargetPath != "" && filepath.Clean(request.TargetPath) != expectedTarget {
		return fmt.Errorf("target path %q does not match the watchdog executable %q — rejecting", filepath.Clean(request.TargetPath), expectedTarget)
	}

	// Check file exists
	info, err := os.Stat(stagedClean)
	if err != nil {
		return fmt.Errorf("staged file not found: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("staged file is empty")
	}

	// Read once for both checksum (optional) and signature (mandatory).
	stagedBytes, err := os.ReadFile(stagedClean)
	if err != nil {
		return fmt.Errorf("failed to read staged file: %w", err)
	}

	// Transport-integrity checksum, when present (defense in depth only).
	if request.Checksum != "" {
		h := sha256.Sum256(stagedBytes)
		actualChecksum := hex.EncodeToString(h[:])
		if actualChecksum != request.Checksum {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", request.Checksum, actualChecksum)
		}
	}

	// RW-1: mandatory signature verification against the embedded public key.
	if err := updatesig.Verify(stagedBytes, request.Signature); err != nil {
		return fmt.Errorf("signature verification failed for staged watchdog v%s: %w", request.Version, err)
	}

	return nil
}

// createUpdateScript creates the batch script that will perform the update
func (s *SelfUpdater) createUpdateScript(request *ipc.WatchdogUpdateRequest) (string, error) {
	scriptDir := ipc.UpdateDir
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create script directory: %w", err)
	}

	scriptPath := filepath.Join(scriptDir, ScriptFileName)
	backupPath := s.executablePath + ".backup"
	logPath := filepath.Join(scriptDir, "watchdog-update.log")
	statusPath := ipc.WatchdogUpdateStatusPath()

	// Escape paths for batch script
	escapePath := func(p string) string {
		return strings.ReplaceAll(p, `"`, `\"`)
	}

	scriptContent := fmt.Sprintf(`@echo off
setlocal enabledelayedexpansion

:: Sentinel Watchdog Self-Update Script
:: Version: %s -> %s
:: Generated: %s

set LOG_FILE=%s
set STATUS_FILE=%s

echo [%%date%% %%time%%] ========================================== >> "%%LOG_FILE%%"
echo [%%date%% %%time%%] Starting watchdog self-update >> "%%LOG_FILE%%"
echo [%%date%% %%time%%] Target version: %s >> "%%LOG_FILE%%"
echo [%%date%% %%time%%] Current exe: %s >> "%%LOG_FILE%%"
echo [%%date%% %%time%%] Staged path: %s >> "%%LOG_FILE%%"

:: Wait a moment for the watchdog to prepare for shutdown
timeout /t 3 /nobreak > nul

:: Stop the watchdog service
echo [%%date%% %%time%%] Stopping %s service... >> "%%LOG_FILE%%"
net stop %s /y 2>> "%%LOG_FILE%%"
if %%errorlevel%% neq 0 (
    echo [%%date%% %%time%%] Warning: Service may already be stopped >> "%%LOG_FILE%%"
)

:: Wait for service to fully stop
timeout /t 5 /nobreak > nul

:: Verify service is stopped
sc query %s | find "STOPPED" > nul
if %%errorlevel%% neq 0 (
    echo [%%date%% %%time%%] Force stopping service by PID... >> "%%LOG_FILE%%"
    :: WD-M1/M2: resolve the watchdog PID via SCM and kill by PID, never a blind taskkill /IM
    for /f "tokens=2 delims=: " %%%%p in ('sc queryex %s ^| findstr /i /C:"PID"') do set WDPID=%%%%p
    if defined WDPID if not "!WDPID!"=="0" (
        echo [%%date%% %%time%%] Killing %s PID !WDPID!... >> "%%LOG_FILE%%"
        taskkill /PID !WDPID! /F 2>> "%%LOG_FILE%%"
    )
    timeout /t 2 /nobreak > nul
)

:: Remove old backup if exists
echo [%%date%% %%time%%] Cleaning up old backup... >> "%%LOG_FILE%%"
if exist "%s" del /f "%s" 2>> "%%LOG_FILE%%"

:: Create backup of current binary
echo [%%date%% %%time%%] Creating backup... >> "%%LOG_FILE%%"
copy /y "%s" "%s" 2>> "%%LOG_FILE%%"
if %%errorlevel%% neq 0 (
    echo [%%date%% %%time%%] WARNING: Failed to create backup, continuing anyway >> "%%LOG_FILE%%"
)

:: Replace the binary
echo [%%date%% %%time%%] Installing new binary... >> "%%LOG_FILE%%"
copy /y "%s" "%s" 2>> "%%LOG_FILE%%"
if %%errorlevel%% neq 0 (
    echo [%%date%% %%time%%] FAILED: Could not copy new binary >> "%%LOG_FILE%%"
    goto :rollback
)

:: Verify the new binary was copied
if not exist "%s" (
    echo [%%date%% %%time%%] FAILED: New binary not found after copy >> "%%LOG_FILE%%"
    goto :rollback
)

:: Start the watchdog service
echo [%%date%% %%time%%] Starting %s service... >> "%%LOG_FILE%%"
net start %s 2>> "%%LOG_FILE%%"
if %%errorlevel%% neq 0 (
    echo [%%date%% %%time%%] FAILED: Could not start service >> "%%LOG_FILE%%"
    goto :rollback
)

:: Wait and verify service is running
timeout /t 5 /nobreak > nul
sc query %s | find "RUNNING" > nul
if %%errorlevel%% neq 0 (
    echo [%%date%% %%time%%] FAILED: Service not running after start >> "%%LOG_FILE%%"
    goto :rollback
)

:: Success!
echo [%%date%% %%time%%] UPDATE SUCCESSFUL >> "%%LOG_FILE%%"

:: Write success status
echo {"state":"complete","version":"%s","previous_version":"%s","completed_at":"%s"} > "%%STATUS_FILE%%"

:: Clean up
echo [%%date%% %%time%%] Cleaning up... >> "%%LOG_FILE%%"
del /f "%s" 2>> "%%LOG_FILE%%"
del /f "%s" 2>> "%%LOG_FILE%%"

:: Delete the scheduled task
echo [%%date%% %%time%%] Removing scheduled task... >> "%%LOG_FILE%%"
schtasks /delete /tn "%s" /f 2>> "%%LOG_FILE%%"

echo [%%date%% %%time%%] Self-update complete >> "%%LOG_FILE%%"
goto :end

:rollback
echo [%%date%% %%time%%] Rolling back... >> "%%LOG_FILE%%"

:: Stop service if running
net stop %s /y 2>> "%%LOG_FILE%%"
timeout /t 2 /nobreak > nul

:: Restore from backup
if exist "%s" (
    echo [%%date%% %%time%%] Restoring from backup... >> "%%LOG_FILE%%"
    copy /y "%s" "%s" 2>> "%%LOG_FILE%%"
)

:: Restart service with old version
echo [%%date%% %%time%%] Restarting with old version... >> "%%LOG_FILE%%"
net start %s 2>> "%%LOG_FILE%%"

:: Write failure status
echo {"state":"rolled_back","version":"%s","rolled_back":true,"error":"Update failed, rolled back","completed_at":"%s"} > "%%STATUS_FILE%%"

:: Clean up scheduled task even on failure
schtasks /delete /tn "%s" /f 2>> "%%LOG_FILE%%"

echo [%%date%% %%time%%] Rollback complete >> "%%LOG_FILE%%"

:end
echo [%%date%% %%time%%] Script finished >> "%%LOG_FILE%%"
`,
		s.currentVersion, request.Version, time.Now().Format(time.RFC3339),
		escapePath(logPath), escapePath(statusPath),
		request.Version, escapePath(s.executablePath), escapePath(request.StagedPath),
		s.serviceName, s.serviceName,
		s.serviceName,
		s.serviceName, s.serviceName, // sc queryex %s + "Killing %s PID" (WD-M1/M2 force-stop by PID)
		escapePath(backupPath), escapePath(backupPath),
		escapePath(s.executablePath), escapePath(backupPath),
		escapePath(request.StagedPath), escapePath(s.executablePath),
		escapePath(s.executablePath),
		s.serviceName, s.serviceName,
		s.serviceName,
		request.Version, s.currentVersion, time.Now().Format(time.RFC3339),
		escapePath(request.StagedPath), escapePath(backupPath),
		TaskName,
		s.serviceName,
		escapePath(backupPath), escapePath(backupPath), escapePath(s.executablePath),
		s.serviceName,
		request.Version, time.Now().Format(time.RFC3339),
		TaskName,
	)

	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return "", fmt.Errorf("failed to write update script: %w", err)
	}

	return scriptPath, nil
}

// createAndRunScheduledTask creates a Windows scheduled task and runs it immediately
func (s *SelfUpdater) createAndRunScheduledTask(scriptPath string) error {
	// First, delete any existing task with the same name
	deleteCmd := exec.Command("schtasks", "/delete", "/tn", TaskName, "/f")
	deleteCmd.Run() // Ignore errors - task may not exist

	// Create a new scheduled task to run the script
	// Run as SYSTEM to ensure we have privileges to stop/start services
	createCmd := exec.Command("schtasks", "/create",
		"/tn", TaskName,
		"/tr", fmt.Sprintf(`cmd.exe /c "%s"`, scriptPath),
		"/sc", "once",
		"/st", time.Now().Add(1*time.Minute).Format("15:04"), // Start time (required but we'll run immediately)
		"/ru", "SYSTEM",
		"/rl", "HIGHEST",
		"/f", // Force overwrite if exists
	)

	output, err := createCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create scheduled task: %w - output: %s", err, string(output))
	}
	log.Printf("[SelfUpdate] Created scheduled task: %s", TaskName)

	// Run the task immediately
	runCmd := exec.Command("schtasks", "/run", "/tn", TaskName)
	output, err = runCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run scheduled task: %w - output: %s", err, string(output))
	}
	log.Printf("[SelfUpdate] Triggered scheduled task: %s", TaskName)

	return nil
}

// failUpdate marks the update as failed and cleans up
func (s *SelfUpdater) failUpdate(status *ipc.WatchdogUpdateStatus, reason string) {
	log.Printf("[SelfUpdate] Update failed: %s", reason)
	status.State = ipc.StateFailed
	status.Error = reason
	status.CompletedAt = time.Now()
	if err := ipc.WriteWatchdogUpdateStatus(status); err != nil {
		log.Printf("[SelfUpdate] Warning: failed to write failure status: %v", err)
	}

	// Clean up request file to prevent retry loops
	ipc.DeleteWatchdogUpdateRequest()
}

// CheckUpdateResult checks the result of a previous self-update attempt.
// This should be called on watchdog startup.
func (s *SelfUpdater) CheckUpdateResult() (*ipc.WatchdogUpdateStatus, error) {
	status, err := ipc.ReadWatchdogUpdateStatus()
	if err != nil {
		return nil, err
	}

	if status == nil {
		return nil, nil
	}

	// If the current version matches the target version and state is complete, update succeeded
	if status.State == ipc.StateComplete && status.Version == s.currentVersion {
		log.Printf("[SelfUpdate] Previous update to v%s confirmed successful", s.currentVersion)
		return status, nil
	}

	// If state is applying and we're running the new version, update succeeded but status wasn't written
	if status.State == ipc.StateApplying && status.Version == s.currentVersion {
		log.Printf("[SelfUpdate] Update to v%s appears successful (status was applying)", s.currentVersion)
		status.State = ipc.StateComplete
		status.CompletedAt = time.Now()
		ipc.WriteWatchdogUpdateStatus(status)
		return status, nil
	}

	return status, nil
}

// CleanupAfterUpdate cleans up files after a successful update verification
func (s *SelfUpdater) CleanupAfterUpdate() {
	// Delete update request and status files
	ipc.DeleteWatchdogUpdateRequest()
	ipc.DeleteWatchdogUpdateStatus()

	// Delete the update script if it exists
	scriptPath := filepath.Join(ipc.UpdateDir, ScriptFileName)
	os.Remove(scriptPath)

	// Clean up staging directory
	ipc.CleanupStagingDir()

	log.Printf("[SelfUpdate] Cleanup complete")
}

// WriteWatchdogInfo writes the current watchdog info to disk
func (s *SelfUpdater) WriteWatchdogInfo() error {
	info := &ipc.WatchdogInfo{
		Version:   s.currentVersion,
		StartedAt: time.Now(),
		PID:       os.Getpid(),
	}
	return ipc.WriteWatchdogInfo(info)
}
