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
	return &Updater{
		serverURL:      serverURL,
		currentVersion: currentVersion,
		httpClient:     &http.Client{Timeout: 5 * time.Minute},
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

	tempFile := filepath.Join(os.TempDir(), fmt.Sprintf("sentinel-agent-%s-%s-%s.tmp",
		info.Version, info.Platform, info.Arch))

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
	checksum := hex.EncodeToString(hasher.Sum(nil))
	if info.Checksum != "" && checksum != info.Checksum {
		os.Remove(tempFile)
		return "", fmt.Errorf("checksum mismatch: expected %s, got %s", info.Checksum, checksum)
	}

	log.Printf("Download complete, checksum verified: %s", checksum)
	return tempFile, nil
}

func (u *Updater) ApplyUpdate(ctx context.Context, downloadPath string, info *VersionInfo) error {
	u.updateStatus(StateStaging, "Preparing update...", 0)

	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get current executable path: %w", err)
	}
	currentExe, _ = filepath.EvalSymlinks(currentExe)

	log.Printf("Applying update from %s to %s", downloadPath, currentExe)
	u.status.TargetVersion = info.Version
	u.reportStatus(ctx)

	if runtime.GOOS == "windows" {
		return u.applyUpdateWindows(currentExe, downloadPath, info.Version)
	}
	return u.applyUpdateUnix(currentExe, downloadPath)
}

func (u *Updater) applyUpdateWindows(currentExe, downloadPath, newVersion string) error {
	u.updateStatus(StateRestarting, "Installing update...", 50)

	batchPath := filepath.Join(os.TempDir(), "sentinel-update.bat")
	backupPath := currentExe + ".old"
	logPath := filepath.Join(os.TempDir(), "sentinel-update.log")

	batchContent := fmt.Sprintf(`@echo off
setlocal enabledelayedexpansion
set LOG_FILE=%s
echo [%%%%date%%%% %%%%time%%%%] Starting update to v%s > "%%%%LOG_FILE%%%%"
timeout /t 3 /nobreak > nul
sc query SentinelAgent | find "STOPPED" > nul
if %%%%errorlevel%%%% neq 0 (
    net stop SentinelAgent /y
    timeout /t 2 /nobreak > nul
)
if exist "%s" del /f "%s" 2>nul
move /y "%s" "%s"
if %%%%errorlevel%%%% neq 0 goto :restart_old
move /y "%s" "%s"
if %%%%errorlevel%%%% neq 0 goto :rollback
net start SentinelAgent
timeout /t 3 /nobreak > nul
sc query SentinelAgent | find "RUNNING" > nul
if %%%%errorlevel%%%% neq 0 goto :rollback
del /f "%s" 2>nul
goto :cleanup
:rollback
net stop SentinelAgent /y 2>nul
del /f "%s" 2>nul
move /y "%s" "%s"
:restart_old
net start SentinelAgent
:cleanup
del /f "%s" 2>nul
`, logPath, newVersion, backupPath, backupPath, currentExe, backupPath, downloadPath, currentExe, backupPath, currentExe, backupPath, currentExe, batchPath)

	if err := os.WriteFile(batchPath, []byte(batchContent), 0755); err != nil {
		return fmt.Errorf("failed to create update script: %w", err)
	}

	cmd := exec.Command("cmd.exe", "/C", "net stop SentinelAgent && start /min cmd.exe /C "+batchPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start update process: %w", err)
	}

	log.Printf("Update initiated, agent will restart shortly")
	return nil
}

func (u *Updater) applyUpdateUnix(currentExe, downloadPath string) error {
	u.updateStatus(StateRestarting, "Installing update...", 50)
	backupPath := currentExe + ".old"

	if err := os.Chmod(downloadPath, 0755); err != nil {
		return fmt.Errorf("failed to set executable permissions: %w", err)
	}

	os.Remove(backupPath)
	if err := os.Rename(currentExe, backupPath); err != nil {
		return fmt.Errorf("failed to backup current binary: %w", err)
	}
	if err := os.Rename(downloadPath, currentExe); err != nil {
		os.Rename(backupPath, currentExe)
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	cmd := exec.Command("systemctl", "restart", "sentinel-agent")
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to restart via systemctl: %v", err)
	}

	log.Printf("Update applied, agent will restart shortly")
	return nil
}

func (u *Updater) RunUpdateLoop(ctx context.Context) {
	initialDelay := 30*time.Second + time.Duration(os.Getpid()%30)*time.Second
	log.Printf("Starting update loop, first check in %v", initialDelay)

	select {
	case <-ctx.Done():
		return
	case <-time.After(initialDelay):
	}

	u.checkAndUpdate(ctx)

	ticker := time.NewTicker(u.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			u.checkAndUpdate(ctx)
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

	log.Println("Checking for updates...")
	u.updateStatus(StatePending, "Checking for updates...", 0)

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
		"deviceId": u.deviceID, "state": u.status.State, "currentVersion": u.status.CurrentVersion,
		"targetVersion": u.status.TargetVersion, "progress": u.status.Progress,
		"message": u.status.Message, "error": u.status.Error,
	}
	jsonData, err := json.Marshal(statusData)
	if err != nil {
		return
	}
	url := fmt.Sprintf("%s/api/agent/update/status", u.serverURL)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
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
