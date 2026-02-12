package updater

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/sentinel/agent/internal/ipc"
	"github.com/sentinel/agent/internal/mtls"
	"github.com/sentinel/agent/internal/protection"
)

const (
	StateIdle        = "idle"
	StatePending     = "pending"
	StateDownloading = "downloading"
	StateVerifying   = "verifying"
	StateStaging     = "staging"
	StateRestarting  = "restarting"
	StateCompleted   = "completed"
	StateFailed      = "failed"
	StateRolledBack  = "rolled_back"
)

type VersionInfo struct {
	Version     string `json:"version"`
	Platform    string `json:"platform"`
	Arch        string `json:"arch"`
	DownloadURL string `json:"downloadUrl"`
	Checksum    string `json:"checksum"`
	Size        int64  `json:"size"`
	ReleaseDate string `json:"releaseDate"`
	Changelog   string `json:"changelog"`
	Required    bool   `json:"required"`
}

// WatchdogVersionInfo contains version information for watchdog updates
type WatchdogVersionInfo struct {
	Version     string `json:"version"`
	Platform    string `json:"platform"`
	Arch        string `json:"arch"`
	DownloadURL string `json:"downloadUrl"`
	Checksum    string `json:"checksum"`
	Size        int64  `json:"size"`
}

// WatchdogUpdateResult contains the result of checking for watchdog updates
type WatchdogUpdateResult struct {
	Available        bool                 `json:"available"`
	CurrentVersion   string               `json:"currentVersion"`
	LatestVersion    string               `json:"latestVersion"`
	VersionInfo      *WatchdogVersionInfo `json:"versionInfo,omitempty"`
	Error            string               `json:"error,omitempty"`
}

type UpdateResult struct {
	Available      bool         `json:"available"`
	CurrentVersion string       `json:"currentVersion"`
	LatestVersion  string       `json:"latestVersion"`
	VersionInfo    *VersionInfo `json:"versionInfo,omitempty"`
	Error          string       `json:"error,omitempty"`
}

type UpdateStatus struct {
	State           string    `json:"state"`
	CurrentVersion  string    `json:"currentVersion"`
	TargetVersion   string    `json:"targetVersion,omitempty"`
	Progress        int       `json:"progress"`
	Message         string    `json:"message"`
	Error           string    `json:"error,omitempty"`
	StartedAt       time.Time `json:"startedAt,omitempty"`
	CompletedAt     time.Time `json:"completedAt,omitempty"`
	RetryCount      int       `json:"retryCount"`
	BytesDownloaded int64     `json:"bytesDownloaded"`
	TotalBytes      int64     `json:"totalBytes"`
}

type Updater struct {
	serverURL      string
	currentVersion string
	deviceID       string
	httpClient     *http.Client
	checkInterval  time.Duration
	maxRetries     int
	retryDelay     time.Duration
	maxRetryDelay  time.Duration
	updateMu       sync.Mutex
	isUpdating     bool
	status         UpdateStatus
	forceCheck     chan struct{}
}

func New(serverURL, currentVersion string) *Updater {
	// Create HTTP client with TLS config for CA verification
	httpClient := &http.Client{Timeout: 5 * time.Minute}

	tlsConfig, err := mtls.GetTLSConfig()
	if err == nil && tlsConfig != nil {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
		log.Println("[Updater] HTTP client configured with CA certificate")
	} else {
		log.Printf("[Updater] Warning: Using default TLS config: %v", err)
	}

	return &Updater{
		serverURL:      serverURL,
		currentVersion: currentVersion,
		httpClient:     httpClient,
		checkInterval:  1 * time.Hour,
		maxRetries:     3,
		retryDelay:     5 * time.Second,
		maxRetryDelay:  2 * time.Minute,
		forceCheck:     make(chan struct{}, 1),
		status:         UpdateStatus{State: StateIdle, CurrentVersion: currentVersion},
	}
}

func (u *Updater) SetDeviceID(deviceID string)             { u.deviceID = deviceID }
func (u *Updater) SetCheckInterval(interval time.Duration) { u.checkInterval = interval }

func (u *Updater) TriggerCheck() {
	select {
	case u.forceCheck <- struct{}{}:
		log.Println("Update check triggered")
	default:
		log.Println("Update check already pending")
	}
}

func (u *Updater) GetStatus() UpdateStatus {
	u.updateMu.Lock()
	defer u.updateMu.Unlock()
	return u.status
}

func (u *Updater) CheckForUpdate(ctx context.Context) (*UpdateResult, error) {
	url := fmt.Sprintf("%s/api/agent/version?platform=%s&arch=%s&current=%s",
		u.serverURL, runtime.GOOS, runtime.GOARCH, u.currentVersion)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("version check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("version check returned status %d: %s", resp.StatusCode, string(body))
	}

	var result UpdateResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	result.CurrentVersion = u.currentVersion
	return &result, nil
}

func (u *Updater) DownloadUpdate(ctx context.Context, info *VersionInfo) (string, error) {
	log.Printf("Downloading update v%s from %s", info.Version, info.DownloadURL)
	u.updateStatus(StateDownloading, "Downloading update...", 0)

	// Ensure staging directory exists
	if err := ipc.EnsureDirectories(); err != nil {
		return "", fmt.Errorf("failed to create staging directory: %w", err)
	}

	// Download to staging directory (not temp) for reliability
	stagingFile := ipc.StagingPath(info.Version, info.Platform, info.Arch)
	tempFile := stagingFile + ".tmp"

	var lastErr error
	for attempt := 0; attempt <= u.maxRetries; attempt++ {
		if attempt > 0 {
			delay := u.retryDelay * time.Duration(1<<uint(attempt))
			if delay > u.maxRetryDelay {
				delay = u.maxRetryDelay
			}
			log.Printf("Retry %d/%d after %v", attempt, u.maxRetries, delay)
			u.status.RetryCount = attempt
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}

		path, err := u.downloadOnce(ctx, info, tempFile)
		if err == nil {
			return path, nil
		}
		lastErr = err
		log.Printf("Download attempt %d failed: %v", attempt+1, err)
	}

	return "", fmt.Errorf("download failed after %d attempts: %w", u.maxRetries+1, lastErr)
}

func (u *Updater) downloadOnce(ctx context.Context, info *VersionInfo, tempFile string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", info.DownloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create download request: %w", err)
	}

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	out, err := os.Create(tempFile)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer out.Close()

	totalSize := resp.ContentLength
	if totalSize <= 0 && info.Size > 0 {
		totalSize = info.Size
	}
	u.status.TotalBytes = totalSize

	hasher := sha256.New()
	writer := io.MultiWriter(out, hasher)

	var written int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			writer.Write(buf[:n])
			written += int64(n)
			u.status.BytesDownloaded = written
			if totalSize > 0 {
				progress := int(float64(written) / float64(totalSize) * 100)
				u.updateStatus(StateDownloading, fmt.Sprintf("Downloading... %d%%", progress), progress)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			os.Remove(tempFile)
			return "", fmt.Errorf("download failed during transfer: %w", readErr)
		}
	}

	u.updateStatus(StateVerifying, "Verifying checksum...", 100)

	// Close the file before renaming (required on Windows)
	out.Close()
	checksum := hex.EncodeToString(hasher.Sum(nil))
	if info.Checksum != "" && checksum != info.Checksum {
		os.Remove(tempFile)
		return "", fmt.Errorf("checksum mismatch: expected %s, got %s", info.Checksum, checksum)
	}

	// Rename from .tmp to final staging path
	finalPath := ipc.StagingPath(info.Version, info.Platform, info.Arch)
	if tempFile != finalPath {
		os.Remove(finalPath) // Remove any existing file
		if err := os.Rename(tempFile, finalPath); err != nil {
			os.Remove(tempFile)
			return "", fmt.Errorf("failed to rename to staging path: %w", err)
		}
	}

	log.Printf("Download complete, checksum verified: %s", checksum)
	return finalPath, nil
}

func (u *Updater) ApplyUpdate(ctx context.Context, downloadPath string, info *VersionInfo) error {
	u.updateStatus(StateStaging, "Preparing update...", 0)

	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get current executable path: %w", err)
	}
	// Resolve symlinks if possible, but keep original path if EvalSymlinks fails
	if resolved, err := filepath.EvalSymlinks(currentExe); err == nil {
		currentExe = resolved
	}

	log.Printf("Applying update from %s to %s", downloadPath, currentExe)
	u.status.TargetVersion = info.Version
	u.reportStatus(ctx)

	if runtime.GOOS == "windows" {
		return u.applyUpdateWindows(currentExe, downloadPath, info.Version)
	}
	return u.applyUpdateUnix(currentExe, downloadPath)
}

func (u *Updater) applyUpdateWindows(currentExe, downloadPath, newVersion string) error {
	u.updateStatus(StateRestarting, "Signaling watchdog for update...", 50)

	// Check if watchdog pipe is available (new watchdog with update orchestration)
	if ipc.IsPipeAvailable() {
		log.Println("Watchdog pipe available, using watchdog-orchestrated update")
		return u.applyUpdateViaWatchdog(currentExe, downloadPath, newVersion)
	}

	// Fallback: old watchdog without pipe support - use legacy batch approach
	log.Println("Watchdog pipe not available, using legacy update method")
	return u.applyUpdateLegacyWindows(currentExe, downloadPath, newVersion)
}

// applyUpdateViaWatchdog uses the new watchdog-orchestrated update mechanism
func (u *Updater) applyUpdateViaWatchdog(currentExe, downloadPath, newVersion string) error {
	log.Printf("DEBUG applyUpdateViaWatchdog: currentExe=%q downloadPath=%q newVersion=%q", currentExe, downloadPath, newVersion)
	
	// Create update request for the watchdog
	request := &ipc.UpdateRequest{
		Version:     newVersion,
		StagedPath:  downloadPath,
		Checksum:    "", // Already verified during download
		RequestedAt: time.Now(),
		RequestedBy: u.deviceID,
		TargetPath:  currentExe,
	}

	// Write the update request file (persists across reboots)
	if err := ipc.WriteUpdateRequest(request); err != nil {
		return fmt.Errorf("failed to write update request: %w", err)
	}
	log.Printf("Update request written for version %s", newVersion)

	// Signal the watchdog via named pipe for immediate handling
	if err := ipc.SignalUpdateReady(request); err != nil {
		// Not fatal - watchdog will poll the JSON file
		log.Printf("Could not signal watchdog via pipe (will poll): %v", err)
	} else {
		log.Println("Watchdog signaled via pipe")
	}

	log.Printf("Update orchestration handed to watchdog, agent will be restarted")

	// Give the watchdog a moment to receive the signal before we potentially exit
	time.Sleep(1 * time.Second)

	return nil
}

// applyUpdateLegacyWindows uses the old batch script method for backward compatibility
func (u *Updater) applyUpdateLegacyWindows(currentExe, downloadPath, newVersion string) error {
	// Disable file protections before update
	installPath := filepath.Dir(currentExe)
	protMgr := protection.NewManager(installPath, "SentinelAgent")
	if err := protMgr.DisableProtections(); err != nil {
		log.Printf("Warning: failed to disable protections: %v", err)
	} else {
		log.Println("File protections disabled for update")
	}

	batchPath := filepath.Join(os.TempDir(), "sentinel-update.bat")
	backupPath := currentExe + ".old"
	logPath := filepath.Join(os.TempDir(), "sentinel-update.log")

	batchContent := fmt.Sprintf(`@echo off
setlocal enabledelayedexpansion
set LOG_FILE=%s
echo [%%date%% %%time%%] Starting update to v%s > "%%LOG_FILE%%"
echo [%%date%% %%time%%] Current exe: %s >> "%%LOG_FILE%%"
echo [%%date%% %%time%%] Download path: %s >> "%%LOG_FILE%%"
timeout /t 3 /nobreak > nul
sc query SentinelAgent | find "STOPPED" > nul
if %%errorlevel%% neq 0 (
    echo [%%date%% %%time%%] Stopping service... >> "%%LOG_FILE%%"
    net stop SentinelAgent /y
    timeout /t 2 /nobreak > nul
)
echo [%%date%% %%time%%] Deleting old backup if exists >> "%%LOG_FILE%%"
if exist "%s" del /f "%s" 2>nul
echo [%%date%% %%time%%] Moving current to backup >> "%%LOG_FILE%%"
move /y "%s" "%s"
if %%errorlevel%% neq 0 (
    echo [%%date%% %%time%%] Failed to backup current exe >> "%%LOG_FILE%%"
    goto :restart_old
)
echo [%%date%% %%time%%] Moving new exe into place >> "%%LOG_FILE%%"
move /y "%s" "%s"
if %%errorlevel%% neq 0 (
    echo [%%date%% %%time%%] Failed to install new exe >> "%%LOG_FILE%%"
    goto :rollback
)
echo [%%date%% %%time%%] Starting service... >> "%%LOG_FILE%%"
net start SentinelAgent
timeout /t 3 /nobreak > nul
sc query SentinelAgent | find "RUNNING" > nul
if %%errorlevel%% neq 0 (
    echo [%%date%% %%time%%] Service failed to start, rolling back >> "%%LOG_FILE%%"
    goto :rollback
)
echo [%%date%% %%time%%] Update successful! >> "%%LOG_FILE%%"
del /f "%s" 2>nul
goto :cleanup
:rollback
echo [%%date%% %%time%%] Rolling back... >> "%%LOG_FILE%%"
net stop SentinelAgent /y 2>nul
del /f "%s" 2>nul
move /y "%s" "%s"
:restart_old
echo [%%date%% %%time%%] Restarting old version >> "%%LOG_FILE%%"
net start SentinelAgent
:cleanup
echo [%%date%% %%time%%] Cleanup complete >> "%%LOG_FILE%%"
`, logPath, newVersion, currentExe, downloadPath,
		backupPath, backupPath,
		currentExe, backupPath,
		downloadPath, currentExe,
		backupPath,
		currentExe, backupPath, currentExe)

	if err := os.WriteFile(batchPath, []byte(batchContent), 0755); err != nil {
		return fmt.Errorf("failed to create update script: %w", err)
	}

	log.Printf("Created update script at %s", batchPath)
	log.Printf("Update will replace %s with %s", currentExe, downloadPath)

	// Use cmd.exe start to create a detached process
	cmd := exec.Command("cmd.exe", "/C", "start", "/min", "cmd.exe", "/C", batchPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start update process: %w", err)
	}

	log.Printf("Update initiated, agent will restart shortly")
	return nil
}

func (u *Updater) applyUpdateUnix(currentExe, downloadPath string) error {
	u.updateStatus(StateRestarting, "Installing update...", 50)
	backupPath := currentExe + ".old"

	// Set executable permissions on downloaded file
	if err := os.Chmod(downloadPath, 0755); err != nil {
		return fmt.Errorf("failed to set executable permissions: %w", err)
	}

	// Remove old backup if exists
	os.Remove(backupPath)

	// Backup current binary
	if err := os.Rename(currentExe, backupPath); err != nil {
		return fmt.Errorf("failed to backup current binary: %w", err)
	}

	// Install new binary
	if err := os.Rename(downloadPath, currentExe); err != nil {
		os.Rename(backupPath, currentExe)
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	// Re-apply executable permissions after move (some filesystems may not preserve)
	if err := os.Chmod(currentExe, 0755); err != nil {
		log.Printf("Warning: failed to re-apply permissions after move: %v", err)
	}

	// Restart the service and wait for completion
	log.Printf("Restarting service via systemctl...")
	cmd := exec.Command("systemctl", "restart", "sentinel-agent")

	// Use Run() instead of Start() to wait for completion and get error
	if err := cmd.Run(); err != nil {
		log.Printf("systemctl restart failed: %v, attempting rollback", err)
		// Rollback: restore backup
		if rbErr := os.Rename(backupPath, currentExe); rbErr != nil {
			log.Printf("CRITICAL: Rollback also failed: %v", rbErr)
		} else {
			log.Printf("Rollback successful, restored previous binary")
		}
		return fmt.Errorf("failed to restart service: %w", err)
	}

	// Verify service is running after restart
	time.Sleep(2 * time.Second) // Give service time to start
	verifyCmd := exec.Command("systemctl", "is-active", "--quiet", "sentinel-agent")
	if err := verifyCmd.Run(); err != nil {
		log.Printf("Service not running after restart, attempting rollback")
		// Rollback
		if rbErr := os.Rename(backupPath, currentExe); rbErr != nil {
			log.Printf("CRITICAL: Rollback failed: %v", rbErr)
		}
		// Try to start with old binary
		exec.Command("systemctl", "restart", "sentinel-agent").Run()
		return fmt.Errorf("service failed to start after update")
	}

	log.Printf("Update applied successfully, service is running")
	return nil
}

func (u *Updater) RunUpdateLoop(ctx context.Context) {
	// Initial check on startup after a brief delay
	// This catches updates that happened while agent was offline
	// Subsequent updates are handled via heartbeat ack notifications
	initialDelay := 30*time.Second + time.Duration(os.Getpid()%30)*time.Second
	log.Printf("Update checker: initial check in %v (subsequent updates via heartbeat)", initialDelay)

	select {
	case <-ctx.Done():
		return
	case <-time.After(initialDelay):
	}

	u.checkAndUpdate(ctx)

	// Only handle force checks now - periodic checks removed
	// Updates are now notified via heartbeat ack from server
	for {
		select {
		case <-ctx.Done():
			return
		case <-u.forceCheck:
			log.Println("Forced update check triggered")
			u.checkAndUpdate(ctx)
		}
	}
}

func (u *Updater) checkAndUpdate(ctx context.Context) {
	u.updateMu.Lock()
	if u.isUpdating {
		u.updateMu.Unlock()
		log.Println("Update already in progress, skipping check")
		return
	}
	u.isUpdating = true
	u.updateMu.Unlock()

	defer func() {
		u.updateMu.Lock()
		u.isUpdating = false
		u.updateMu.Unlock()
	}()

	// Check if watchdog is already handling an update (update-request.json exists)
	existingRequest, err := ipc.ReadUpdateRequest()
	if err == nil && existingRequest != nil {
		log.Printf("Update request already exists for v%s, watchdog will handle it", existingRequest.Version)
		return
	}

	// Also check if there's a pending/applying status from watchdog
	existingStatus, err := ipc.ReadUpdateStatus()
	if err == nil && existingStatus != nil {
		if existingStatus.State == ipc.StatePending || existingStatus.State == ipc.StateApplying {
			log.Printf("Update already in state %s by watchdog, skipping", existingStatus.State)
			return
		}
	}

	log.Println("Checking for updates...")
	u.updateStatus(StatePending, "Checking for updates...", 0)

	// CRITICAL: Check if watchdog needs updating FIRST
	// This ensures the new watchdog (with lenient crash detection) is running
	// before we update the agent, preventing false rollbacks
	if runtime.GOOS == "windows" {
		watchdogResult, err := u.CheckForWatchdogUpdate(ctx)
		if err != nil {
			log.Printf("Warning: Watchdog update check failed: %v", err)
			// Continue with agent update - watchdog check is best-effort
		} else if watchdogResult != nil && watchdogResult.Available {
			log.Printf("Watchdog update available: v%s -> v%s", watchdogResult.CurrentVersion, watchdogResult.LatestVersion)
			log.Println("Triggering watchdog update FIRST to prevent rollback issues")

			if err := u.TriggerWatchdogUpdate(ctx); err != nil {
				log.Printf("Warning: Failed to trigger watchdog update: %v", err)
				// IMPORTANT: Still defer agent update even on failure
				// Old watchdog may not properly orchestrate new agent update
				log.Println("Deferring agent update - watchdog must be updated first (will retry)")
				u.updateStatus(StateIdle, "Waiting for watchdog update (retry pending)", 0)
				return
			}
			// Successfully triggered watchdog update
			// STOP HERE and let watchdog update itself first
			// Next update cycle will proceed with agent update
			log.Println("Watchdog update triggered - deferring agent update until watchdog is updated")
			u.updateStatus(StateIdle, "Waiting for watchdog update", 0)
			return
		} else {
			log.Printf("Watchdog is up to date (v%s)", watchdogResult.CurrentVersion)
		}
	}

	result, err := u.CheckForUpdate(ctx)
	if err != nil {
		log.Printf("Update check failed: %v", err)
		u.updateStatus(StateIdle, "", 0)
		return
	}

	if !result.Available {
		log.Printf("No update available (current: v%s)", u.currentVersion)
		u.updateStatus(StateIdle, "Up to date", 0)
		return
	}

	log.Printf("Update available: v%s -> v%s", u.currentVersion, result.LatestVersion)

	if result.VersionInfo == nil {
		log.Printf("No version info in response")
		u.updateStatus(StateIdle, "No version info", 0)
		return
	}

	u.status.TargetVersion = result.LatestVersion
	u.status.StartedAt = time.Now()

	downloadPath, err := u.DownloadUpdate(ctx, result.VersionInfo)
	if err != nil {
		log.Printf("Failed to download update: %v", err)
		u.updateStatus(StateFailed, fmt.Sprintf("Download failed: %v", err), 0)
		u.reportStatus(ctx)
		return
	}

	if err := u.ApplyUpdate(ctx, downloadPath, result.VersionInfo); err != nil {
		log.Printf("Failed to apply update: %v", err)
		u.updateStatus(StateFailed, fmt.Sprintf("Apply failed: %v", err), 0)
		u.reportStatus(ctx)
		os.Remove(downloadPath)
		return
	}
}

func (u *Updater) updateStatus(state, message string, progress int) {
	u.updateMu.Lock()
	defer u.updateMu.Unlock()
	u.status.State = state
	u.status.Message = message
	u.status.Progress = progress
	if state == StateFailed {
		u.status.Error = message
	} else {
		u.status.Error = ""
	}
	if state == StateCompleted || state == StateFailed || state == StateRolledBack {
		u.status.CompletedAt = time.Now()
	}
}

func (u *Updater) reportStatus(ctx context.Context) {
	if u.deviceID == "" {
		return
	}
	statusData := map[string]interface{}{
		"agentId": u.deviceID, "fromVersion": u.status.CurrentVersion,
		"toVersion": u.status.TargetVersion, "status": u.status.State,
		"error": u.status.Error,
	}
	jsonData, err := json.Marshal(statusData)
	if err != nil {
		return
	}
	url := fmt.Sprintf("%s/api/agent/update/status", u.serverURL)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")
	resp, err := u.httpClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func (u *Updater) Rollback() error {
	currentExe, _ := os.Executable()
	currentExe, _ = filepath.EvalSymlinks(currentExe)
	backupPath := currentExe + ".old"

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("no backup available for rollback")
	}

	log.Printf("Rolling back from %s to %s", currentExe, backupPath)
	u.updateStatus(StateRolledBack, "Rolling back...", 0)

	if runtime.GOOS == "windows" {
		batchPath := filepath.Join(os.TempDir(), "sentinel-rollback.bat")
		batchContent := fmt.Sprintf(`@echo off
timeout /t 2 /nobreak > nul
net stop SentinelAgent /y
timeout /t 2 /nobreak > nul
del /f "%s"
move /y "%s" "%s"
net start SentinelAgent
del /f "%s"
`, currentExe, backupPath, currentExe, batchPath)
		os.WriteFile(batchPath, []byte(batchContent), 0755)
		cmd := exec.Command("cmd.exe", "/C", "net stop SentinelAgent && start /min cmd.exe /C "+batchPath)
		return cmd.Start()
	}

	os.Rename(currentExe, currentExe+".failed")
	if err := os.Rename(backupPath, currentExe); err != nil {
		os.Rename(currentExe+".failed", currentExe)
		return fmt.Errorf("failed to restore backup: %w", err)
	}
	os.Remove(currentExe + ".failed")
	cmd := exec.Command("systemctl", "restart", "sentinel-agent")
	return cmd.Start()
}

func CompareVersions(v1, v2 string) int {
	var v1Parts, v2Parts [3]int
	fmt.Sscanf(v1, "%d.%d.%d", &v1Parts[0], &v1Parts[1], &v1Parts[2])
	fmt.Sscanf(v2, "%d.%d.%d", &v2Parts[0], &v2Parts[1], &v2Parts[2])
	for i := 0; i < 3; i++ {
		if v1Parts[i] < v2Parts[i] {
			return -1
		}
		if v1Parts[i] > v2Parts[i] {
			return 1
		}
	}
	return 0
}

// ============================================================================
// Watchdog Update Functions
// ============================================================================

// CheckForWatchdogUpdate checks if a watchdog update is available
func (u *Updater) CheckForWatchdogUpdate(ctx context.Context) (*WatchdogUpdateResult, error) {
	// First get current watchdog version via IPC
	watchdogVersion, err := ipc.QueryWatchdogVersion()
	if err != nil {
		// Watchdog may be old version without pipe support
		log.Printf("Could not query watchdog version: %v", err)
		return nil, fmt.Errorf("watchdog version unavailable: %w", err)
	}

	url := fmt.Sprintf("%s/api/agent/watchdog/version?platform=%s&arch=%s&current=%s",
		u.serverURL, runtime.GOOS, runtime.GOARCH, watchdogVersion)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("watchdog version check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Endpoint not implemented on server, watchdog updates not supported
		return &WatchdogUpdateResult{
			Available:      false,
			CurrentVersion: watchdogVersion,
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("watchdog version check returned status %d: %s", resp.StatusCode, string(body))
	}

	var result WatchdogUpdateResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	result.CurrentVersion = watchdogVersion
	return &result, nil
}

// DownloadWatchdogUpdate downloads the watchdog update and returns the staging path
func (u *Updater) DownloadWatchdogUpdate(ctx context.Context, info *WatchdogVersionInfo) (string, error) {
	log.Printf("[Updater] Downloading watchdog update v%s from %s", info.Version, info.DownloadURL)

	// Ensure staging directory exists
	if err := ipc.EnsureDirectories(); err != nil {
		return "", fmt.Errorf("failed to create staging directory: %w", err)
	}

	stagingFile := ipc.WatchdogStagingPath(info.Version, info.Platform, info.Arch)
	tempFile := stagingFile + ".tmp"

	// Download the file
	req, err := http.NewRequestWithContext(ctx, "GET", info.DownloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create download request: %w", err)
	}

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	out, err := os.Create(tempFile)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer out.Close()

	hasher := sha256.New()
	writer := io.MultiWriter(out, hasher)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		os.Remove(tempFile)
		return "", fmt.Errorf("download failed during transfer: %w", err)
	}

	// Close the file before renaming (required on Windows)
	out.Close()

	// Verify checksum if provided
	checksum := hex.EncodeToString(hasher.Sum(nil))
	if info.Checksum != "" && checksum != info.Checksum {
		os.Remove(tempFile)
		return "", fmt.Errorf("checksum mismatch: expected %s, got %s", info.Checksum, checksum)
	}

	// Rename from .tmp to final staging path
	os.Remove(stagingFile) // Remove any existing file
	if err := os.Rename(tempFile, stagingFile); err != nil {
		os.Remove(tempFile)
		return "", fmt.Errorf("failed to rename to staging path: %w", err)
	}

	log.Printf("[Updater] Watchdog download complete, checksum verified: %s", checksum)
	return stagingFile, nil
}

// TriggerWatchdogUpdate initiates a watchdog update
func (u *Updater) TriggerWatchdogUpdate(ctx context.Context) error {
	log.Println("[Updater] Checking for watchdog updates...")

	result, err := u.CheckForWatchdogUpdate(ctx)
	if err != nil {
		return fmt.Errorf("watchdog update check failed: %w", err)
	}

	if !result.Available {
		log.Printf("[Updater] Watchdog is up to date (v%s)", result.CurrentVersion)
		return nil
	}

	if result.VersionInfo == nil {
		return fmt.Errorf("no version info in watchdog update response")
	}

	log.Printf("[Updater] Watchdog update available: v%s -> v%s",
		result.CurrentVersion, result.LatestVersion)

	// Download the update
	stagingPath, err := u.DownloadWatchdogUpdate(ctx, result.VersionInfo)
	if err != nil {
		return fmt.Errorf("failed to download watchdog update: %w", err)
	}

	// Get watchdog executable path (assumes it's in the same directory as agent)
	agentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get agent executable path: %w", err)
	}
	watchdogPath := filepath.Join(filepath.Dir(agentExe), "sentinel-watchdog.exe")

	// Create update request for the watchdog
	request := &ipc.WatchdogUpdateRequest{
		Version:     result.VersionInfo.Version,
		StagedPath:  stagingPath,
		Checksum:    result.VersionInfo.Checksum,
		RequestedAt: time.Now(),
		RequestedBy: u.deviceID,
		TargetPath:  watchdogPath,
	}

	// Write the update request file
	if err := ipc.WriteWatchdogUpdateRequest(request); err != nil {
		os.Remove(stagingPath)
		return fmt.Errorf("failed to write watchdog update request: %w", err)
	}

	log.Printf("[Updater] Watchdog update request written for v%s", result.VersionInfo.Version)

	// Signal the watchdog via named pipe (15s timeout for slow/busy systems)
	client, err := ipc.ConnectPipeWithTimeout(15 * time.Second)
	if err != nil {
		log.Printf("[Updater] Could not signal watchdog via pipe (will poll): %v", err)
	} else {
		msg := ipc.PipeMessage{Type: ipc.MsgWatchdogUpdateReady}
		client.Send(msg, false)
		client.Close()
		log.Println("[Updater] Watchdog signaled via pipe for self-update")
	}

	return nil
}

// CheckAndReportUpdateResult checks for completed update status and reports to server.
// This should be called on agent startup to report the outcome of any previous update.
func (u *Updater) CheckAndReportUpdateResult(ctx context.Context) {
	status, err := ipc.ReadUpdateStatus()
	if err != nil {
		log.Printf("Error reading update status: %v", err)
		return
	}

	if status == nil {
		return // No update status to report
	}

	// Only report terminal states
	switch status.State {
	case ipc.StateComplete, ipc.StateFailed, ipc.StateRolledBack:
		// Report to server
		log.Printf("Reporting update result: state=%s version=%s", status.State, status.Version)
		u.reportUpdateResult(ctx, status)

		// Clean up status file after reporting
		if err := ipc.DeleteUpdateStatus(); err != nil {
			log.Printf("Warning: failed to delete update status: %v", err)
		}
	default:
		// Update still in progress or pending - don't clear
		log.Printf("Update status: %s (not reporting yet)", status.State)
	}
}

// reportUpdateResult sends the update result to the server with retry logic
func (u *Updater) reportUpdateResult(ctx context.Context, status *ipc.UpdateStatus) {
	if u.deviceID == "" || u.serverURL == "" {
		return
	}

	resultData := map[string]interface{}{
		"deviceId":        u.deviceID,
		"state":           string(status.State),
		"version":         status.Version,
		"previousVersion": status.PreviousVer,
		"rolledBack":      status.RolledBack,
		"error":           status.Error,
		"startedAt":       status.StartedAt,
		"completedAt":     status.CompletedAt,
	}

	jsonData, err := json.Marshal(resultData)
	if err != nil {
		log.Printf("Failed to marshal update result: %v", err)
		return
	}

	url := fmt.Sprintf("%s/api/agent/update/result", u.serverURL)

	// Retry with exponential backoff (3 attempts: 0s, 5s, 15s)
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*5) * time.Second
			log.Printf("Retrying update result report in %v (attempt %d/%d)", backoff, attempt+1, maxRetries)
			select {
			case <-ctx.Done():
				log.Printf("Context cancelled, stopping update result report")
				return
			case <-time.After(backoff):
			}
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
		if err != nil {
			log.Printf("Failed to create update result request: %v", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := u.httpClient.Do(req)
		if err != nil {
			log.Printf("Failed to report update result (attempt %d/%d): %v", attempt+1, maxRetries, err)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			resp.Body.Close()
			log.Printf("Update result reported successfully")
			return
		}

		resp.Body.Close()
		log.Printf("Server returned status %d for update result (attempt %d/%d)", resp.StatusCode, attempt+1, maxRetries)

		// Don't retry on client errors (4xx)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			log.Printf("Client error, not retrying")
			return
		}
	}

	log.Printf("Failed to report update result after %d attempts", maxRetries)
}
