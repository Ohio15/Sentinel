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

// Resilient Update Architecture Constants
// Based on industry best practices from Datto RMM, Tactical RMM, NinjaRMM
const (
	// Layer 2: Agent Independent Polling
	// Runs even when WebSocket/primary communication is broken
	DefaultPollInterval     = 30 * time.Minute // Base polling interval
	MinPollInterval         = 5 * time.Minute  // Minimum interval (aggressive mode)
	MaxPollInterval         = 2 * time.Hour    // Maximum interval (backoff ceiling)
	PollJitterPercent       = 20               // Randomization to prevent thundering herd

	// Retry and backoff settings
	DefaultMaxRetries       = 5                // More retries for resilience
	DefaultRetryDelay       = 10 * time.Second
	DefaultMaxRetryDelay    = 5 * time.Minute

	// Health check thresholds
	ConsecutiveFailuresForAggressive = 3  // Switch to aggressive polling after N failures
	ConsecutiveSuccessesForNormal    = 2  // Return to normal polling after N successes
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
	fallbackURLs   []string // Fallback server URLs for resilience
	currentVersion string
	deviceID       string // Device UUID (from enrollment)
	agentID        string // Hardware fingerprint (agent_id in DB)
	httpClient     *http.Client
	checkInterval  time.Duration
	maxRetries     int
	retryDelay     time.Duration
	maxRetryDelay  time.Duration
	updateMu       sync.Mutex
	isUpdating     bool
	status         UpdateStatus
	forceCheck     chan struct{}

	// Adaptive polling state
	pollMu                sync.Mutex
	consecutiveFailures   int
	consecutiveSuccesses  int
	currentPollInterval   time.Duration
	lastSuccessfulCheck   time.Time
	lastServerContact     time.Time // Track any successful server communication
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
		serverURL:           serverURL,
		fallbackURLs:        []string{}, // Can be configured via SetFallbackURLs
		currentVersion:      currentVersion,
		httpClient:          httpClient,
		checkInterval:       DefaultPollInterval,
		currentPollInterval: DefaultPollInterval,
		maxRetries:          DefaultMaxRetries,
		retryDelay:          DefaultRetryDelay,
		maxRetryDelay:       DefaultMaxRetryDelay,
		forceCheck:          make(chan struct{}, 1),
		status:              UpdateStatus{State: StateIdle, CurrentVersion: currentVersion},
		lastSuccessfulCheck: time.Now(), // Assume healthy on startup
		lastServerContact:   time.Now(),
	}
}

// SetFallbackURLs configures backup server URLs for resilience
func (u *Updater) SetFallbackURLs(urls []string) {
	u.fallbackURLs = urls
	log.Printf("[Updater] Configured %d fallback URLs", len(urls))
}

// NotifyServerContact should be called whenever any successful server communication occurs
// This helps the updater know if the primary channel (WebSocket) is working
func (u *Updater) NotifyServerContact() {
	u.pollMu.Lock()
	u.lastServerContact = time.Now()
	u.pollMu.Unlock()
}

// getNextPollInterval returns the next polling interval with adaptive adjustment and jitter
func (u *Updater) getNextPollInterval() time.Duration {
	u.pollMu.Lock()
	defer u.pollMu.Unlock()

	// Add jitter to prevent thundering herd (±20%)
	jitter := time.Duration(float64(u.currentPollInterval) * float64(PollJitterPercent) / 100)
	randomJitter := time.Duration(os.Getpid()%int(jitter.Seconds()+1)) * time.Second
	if os.Getpid()%2 == 0 {
		randomJitter = -randomJitter
	}

	interval := u.currentPollInterval + randomJitter
	if interval < MinPollInterval {
		interval = MinPollInterval
	}

	return interval
}

// recordPollSuccess adjusts polling interval on successful check
func (u *Updater) recordPollSuccess() {
	u.pollMu.Lock()
	defer u.pollMu.Unlock()

	u.consecutiveFailures = 0
	u.consecutiveSuccesses++
	u.lastSuccessfulCheck = time.Now()

	// Return to normal polling after consecutive successes
	if u.consecutiveSuccesses >= ConsecutiveSuccessesForNormal && u.currentPollInterval < DefaultPollInterval {
		u.currentPollInterval = DefaultPollInterval
		log.Printf("[Updater] Returning to normal polling interval: %v", u.currentPollInterval)
	}
}

// recordPollFailure adjusts polling interval on failed check
func (u *Updater) recordPollFailure() {
	u.pollMu.Lock()
	defer u.pollMu.Unlock()

	u.consecutiveSuccesses = 0
	u.consecutiveFailures++

	// Switch to aggressive polling after consecutive failures
	if u.consecutiveFailures >= ConsecutiveFailuresForAggressive {
		u.currentPollInterval = MinPollInterval
		log.Printf("[Updater] Switching to aggressive polling due to %d consecutive failures: %v",
			u.consecutiveFailures, u.currentPollInterval)
	}
}

// shouldPollAggressively returns true if we haven't had server contact recently
func (u *Updater) shouldPollAggressively() bool {
	u.pollMu.Lock()
	defer u.pollMu.Unlock()

	// If no server contact (including WebSocket heartbeats) for 2x the poll interval,
	// something might be wrong with primary communication
	threshold := 2 * DefaultPollInterval
	return time.Since(u.lastServerContact) > threshold
}

func (u *Updater) SetDeviceID(deviceID string)             { u.deviceID = deviceID }
func (u *Updater) SetAgentID(agentID string)               { u.agentID = agentID }
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
	// Try primary URL first
	path, err := u.downloadFromURL(ctx, info.DownloadURL, tempFile, info)
	if err != nil && u.serverURL != "" {
		// Fallback: construct URL from agent's own server URL
		fallbackURL := fmt.Sprintf("%s/api/agent/update/download?platform=%s&arch=%s",
			u.serverURL, runtime.GOOS, runtime.GOARCH)
		if fallbackURL != info.DownloadURL {
			log.Printf("[Updater] Primary download failed, trying fallback URL: %s", fallbackURL)
			path, err = u.downloadFromURL(ctx, fallbackURL, tempFile, info)
		}
	}
	return path, err
}

func (u *Updater) downloadFromURL(ctx context.Context, downloadURL, tempFile string, info *VersionInfo) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
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
		// Write alert for checksum mismatch (potential supply chain issue)
		ipc.WriteAlert(&ipc.AlertRelayPayload{
			Severity: "critical",
			Title:    "Agent Update Checksum Mismatch",
			Message:  fmt.Sprintf("Update v%s checksum mismatch: expected %s, got %s", info.Version, info.Checksum, checksum),
		})
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

	// Install new binary - try rename first, fall back to copy for cross-filesystem
	if err := os.Rename(downloadPath, currentExe); err != nil {
		log.Printf("os.Rename failed (cross-filesystem?): %v, trying copy", err)
		if cpErr := copyFile(downloadPath, currentExe); cpErr != nil {
			// Rollback: restore backup
			os.Rename(backupPath, currentExe)
			return fmt.Errorf("failed to install new binary: %w", cpErr)
		}
		os.Remove(downloadPath)
	}

	// Re-apply executable permissions after move (some filesystems may not preserve)
	if err := os.Chmod(currentExe, 0755); err != nil {
		log.Printf("Warning: failed to re-apply permissions after move: %v", err)
	}

	// Restart the service using the best available method
	restartErr := u.restartUnixService(currentExe)
	if restartErr != nil {
		log.Printf("Service restart failed: %v, attempting rollback", restartErr)
		if rbErr := os.Rename(backupPath, currentExe); rbErr != nil {
			log.Printf("CRITICAL: Rollback also failed: %v", rbErr)
		} else {
			log.Printf("Rollback successful, restored previous binary")
			// Try to restart with old binary
			u.restartUnixService(currentExe)
		}
		return fmt.Errorf("failed to restart service: %w", restartErr)
	}

	log.Printf("Update applied successfully, service is running")
	return nil
}

// restartUnixService tries multiple restart strategies in order:
// 1. Synology SPK start-stop-status script
// 2. systemctl (systemd)
// 3. Direct process signal (SIGHUP/re-exec via current process)
func (u *Updater) restartUnixService(currentExe string) error {
	// Strategy 1: Synology SPK package script
	synologyScript := "/var/packages/sentinel-agent/scripts/start-stop-status"
	if _, err := os.Stat(synologyScript); err == nil {
		log.Printf("Detected Synology SPK, restarting via start-stop-status script")
		cmd := exec.Command(synologyScript, "restart")
		if err := cmd.Run(); err != nil {
			log.Printf("Synology restart script failed: %v", err)
		} else {
			time.Sleep(3 * time.Second)
			// Verify by checking PID file
			if pidData, err := os.ReadFile("/var/run/sentinel-agent.pid"); err == nil {
				log.Printf("Synology agent restarted with PID from pidfile: %s", string(pidData))
				return nil
			}
			log.Printf("Synology restart appeared to succeed but no PID file found")
			return nil
		}
	}

	// Strategy 2: systemctl (standard Linux with systemd)
	if _, err := exec.LookPath("systemctl"); err == nil {
		log.Printf("Restarting service via systemctl...")
		cmd := exec.Command("systemctl", "restart", "sentinel-agent")
		if err := cmd.Run(); err != nil {
			log.Printf("systemctl restart failed: %v", err)
		} else {
			time.Sleep(2 * time.Second)
			verifyCmd := exec.Command("systemctl", "is-active", "--quiet", "sentinel-agent")
			if err := verifyCmd.Run(); err == nil {
				log.Printf("Service restarted successfully via systemctl")
				return nil
			}
			log.Printf("systemctl restart succeeded but service not active")
		}
	}

	// Strategy 3: Generic init.d script
	initScript := "/etc/init.d/sentinel-agent"
	if _, err := os.Stat(initScript); err == nil {
		log.Printf("Restarting via init.d script...")
		cmd := exec.Command(initScript, "restart")
		if err := cmd.Run(); err != nil {
			log.Printf("init.d restart failed: %v", err)
		} else {
			time.Sleep(3 * time.Second)
			return nil
		}
	}

	// Strategy 4: Direct re-exec - use nohup to start detached process
	log.Printf("No service manager found, starting new process via nohup")
	logFile := "/var/log/sentinel-agent.log"
	cmd := exec.Command("nohup", currentExe, ">>", logFile, "2>&1", "&")
	cmd.Dir = "/"
	if err := cmd.Start(); err != nil {
		// nohup not available, try direct start
		cmd2 := exec.Command(currentExe)
		cmd2.Dir = "/"
		if err2 := cmd2.Start(); err2 != nil {
			return fmt.Errorf("failed to start new process: %w", err2)
		}
		log.Printf("Started new agent process (PID %d), current process will exit", cmd2.Process.Pid)
	} else {
		log.Printf("Started new agent process via nohup, current process will exit")
	}
	go func() {
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}()
	return nil
}

// copyFile copies a file from src to dst, used when os.Rename fails (cross-filesystem)
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		os.Remove(dst)
		return err
	}
	return dstFile.Sync()
}

func (u *Updater) RunUpdateLoop(ctx context.Context) {
	// Initial check on startup after a brief delay with jitter
	// This catches updates that happened while agent was offline
	initialDelay := 30*time.Second + time.Duration(os.Getpid()%30)*time.Second
	log.Printf("[Updater] Resilient update loop starting: initial check in %v, base interval %v",
		initialDelay, u.checkInterval)
	log.Printf("[Updater] Defense layers: WebSocket push + Independent HTTP polling + Adaptive intervals")

	select {
	case <-ctx.Done():
		return
	case <-time.After(initialDelay):
	}

	// Initial check
	if err := u.checkAndUpdateWithFallback(ctx); err != nil {
		log.Printf("[Updater] Initial check failed: %v", err)
		u.recordPollFailure()
	} else {
		u.recordPollSuccess()
	}

	// Adaptive polling loop
	// This provides application-level control independent of:
	// - WebSocket state
	// - OS service managers (systemd, launchd, Windows SCM)
	// - Network interruptions
	for {
		// Calculate next interval based on adaptive state
		nextInterval := u.getNextPollInterval()

		// Check if we should be more aggressive due to lost server contact
		if u.shouldPollAggressively() {
			nextInterval = MinPollInterval
			log.Printf("[Updater] No recent server contact, using aggressive polling: %v", nextInterval)
		}

		select {
		case <-ctx.Done():
			log.Println("[Updater] Update loop stopping")
			return

		case <-u.forceCheck:
			log.Println("[Updater] Forced update check triggered (server notification)")
			if err := u.checkAndUpdateWithFallback(ctx); err != nil {
				log.Printf("[Updater] Forced check failed: %v", err)
			} else {
				u.recordPollSuccess()
			}

		case <-time.After(nextInterval):
			log.Printf("[Updater] Independent polling check (interval was %v)", nextInterval)
			if err := u.checkAndUpdateWithFallback(ctx); err != nil {
				log.Printf("[Updater] Poll check failed: %v", err)
				u.recordPollFailure()
			} else {
				u.recordPollSuccess()
			}
		}
	}
}

// checkAndUpdateWithFallback tries the primary server, then fallback URLs
func (u *Updater) checkAndUpdateWithFallback(ctx context.Context) error {
	// Try primary server first
	err := u.checkAndUpdateFromURL(ctx, u.serverURL)
	if err == nil {
		return nil
	}

	log.Printf("[Updater] Primary server failed: %v, trying fallbacks...", err)

	// Try fallback URLs
	for i, fallbackURL := range u.fallbackURLs {
		log.Printf("[Updater] Trying fallback %d: %s", i+1, fallbackURL)
		if err := u.checkAndUpdateFromURL(ctx, fallbackURL); err == nil {
			log.Printf("[Updater] Fallback %d succeeded", i+1)
			return nil
		}
		log.Printf("[Updater] Fallback %d failed: %v", i+1, err)
	}

	return fmt.Errorf("all servers failed, primary error: %w", err)
}

// checkAndUpdateFromURL checks a specific server URL for updates
func (u *Updater) checkAndUpdateFromURL(ctx context.Context, serverURL string) error {
	u.updateMu.Lock()
	if u.isUpdating {
		u.updateMu.Unlock()
		return nil // Not an error, just skip
	}
	u.isUpdating = true
	u.updateMu.Unlock()

	defer func() {
		u.updateMu.Lock()
		u.isUpdating = false
		u.updateMu.Unlock()
	}()

	// Check if watchdog is already handling an update
	existingRequest, err := ipc.ReadUpdateRequest()
	if err == nil && existingRequest != nil {
		log.Printf("[Updater] Update request already exists for v%s, watchdog will handle it", existingRequest.Version)
		return nil
	}

	// Check if there's a pending/applying status from watchdog
	existingStatus, err := ipc.ReadUpdateStatus()
	if err == nil && existingStatus != nil {
		if existingStatus.State == ipc.StatePending || existingStatus.State == ipc.StateApplying {
			log.Printf("[Updater] Update already in state %s by watchdog, skipping", existingStatus.State)
			return nil
		}
	}

	u.updateStatus(StatePending, "Checking for updates...", 0)

	// On Windows, check watchdog updates first
	if runtime.GOOS == "windows" {
		if err := u.checkAndTriggerWatchdogUpdate(ctx, serverURL); err != nil {
			// Watchdog update check failed or in progress, defer agent update
			return nil
		}
	}

	// Check for agent update
	result, err := u.checkForUpdateFromURL(ctx, serverURL)
	if err != nil {
		u.updateStatus(StateIdle, "", 0)
		return fmt.Errorf("update check failed: %w", err)
	}

	if !result.Available {
		log.Printf("[Updater] No update available (current: v%s)", u.currentVersion)
		u.updateStatus(StateIdle, "Up to date", 0)
		return nil
	}

	log.Printf("[Updater] Update available: v%s -> v%s", u.currentVersion, result.LatestVersion)

	if result.VersionInfo == nil {
		log.Printf("[Updater] No version info in response")
		u.updateStatus(StateIdle, "No version info", 0)
		return nil
	}

	u.status.TargetVersion = result.LatestVersion
	u.status.StartedAt = time.Now()

	downloadPath, err := u.DownloadUpdate(ctx, result.VersionInfo)
	if err != nil {
		log.Printf("[Updater] Failed to download update: %v", err)
		u.updateStatus(StateFailed, fmt.Sprintf("Download failed: %v", err), 0)
		u.reportStatus(ctx)
		// Write alert file for relay to server via WebSocket
		ipc.WriteAlert(&ipc.AlertRelayPayload{
			Severity: "critical",
			Title:    "Agent Update Download Failed",
			Message:  fmt.Sprintf("Failed to download update to v%s: %v", result.LatestVersion, err),
		})
		return fmt.Errorf("download failed: %w", err)
	}

	if err := u.ApplyUpdate(ctx, downloadPath, result.VersionInfo); err != nil {
		log.Printf("[Updater] Failed to apply update: %v", err)
		u.updateStatus(StateFailed, fmt.Sprintf("Apply failed: %v", err), 0)
		u.reportStatus(ctx)
		os.Remove(downloadPath)
		// Write alert file for relay to server via WebSocket
		ipc.WriteAlert(&ipc.AlertRelayPayload{
			Severity: "critical",
			Title:    "Agent Update Staging Failed",
			Message:  fmt.Sprintf("Failed to apply update to v%s: %v", result.LatestVersion, err),
		})
		return fmt.Errorf("apply failed: %w", err)
	}

	return nil
}

// checkForUpdateFromURL checks a specific server for updates
func (u *Updater) checkForUpdateFromURL(ctx context.Context, serverURL string) (*UpdateResult, error) {
	url := fmt.Sprintf("%s/api/agent/version?platform=%s&arch=%s&current=%s",
		serverURL, runtime.GOOS, runtime.GOARCH, u.currentVersion)

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

// checkAndTriggerWatchdogUpdate checks for watchdog update and returns error if agent update should be deferred
func (u *Updater) checkAndTriggerWatchdogUpdate(ctx context.Context, serverURL string) error {
	watchdogResult, err := u.CheckForWatchdogUpdate(ctx)
	if err != nil {
		log.Printf("[Updater] Warning: Watchdog update check failed: %v", err)
		return nil // Continue with agent update
	}

	if watchdogResult != nil && watchdogResult.Available {
		log.Printf("[Updater] Watchdog update available: v%s -> v%s",
			watchdogResult.CurrentVersion, watchdogResult.LatestVersion)
		log.Println("[Updater] Triggering watchdog update FIRST to prevent rollback issues")

		if err := u.TriggerWatchdogUpdate(ctx); err != nil {
			log.Printf("[Updater] Warning: Failed to trigger watchdog update: %v", err)
			log.Println("[Updater] Deferring agent update - watchdog must be updated first")
			u.updateStatus(StateIdle, "Waiting for watchdog update", 0)
			return fmt.Errorf("watchdog update in progress")
		}

		log.Println("[Updater] Watchdog update triggered - deferring agent update")
		u.updateStatus(StateIdle, "Waiting for watchdog update", 0)
		return fmt.Errorf("watchdog update triggered")
	}

	return nil
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
	// Use hardware fingerprint (agentID) for server lookup; fall back to device UUID
	reportID := u.agentID
	if reportID == "" {
		reportID = u.deviceID
	}
	if reportID == "" {
		return
	}
	statusData := map[string]interface{}{
		"agentId": reportID, "fromVersion": u.status.CurrentVersion,
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

// ============================================================================
// Layer 4: Bootstrap Recovery Task
// ============================================================================
// This creates a scheduled task/cron job that runs independently of both
// the agent and watchdog. It's the last line of defense - if everything
// else fails, this can recover the agent from scratch.

const (
	BootstrapTaskName     = "SentinelBootstrapRecovery"
	BootstrapCheckInterval = 6 // hours
)

// InstallBootstrapRecoveryTask installs a scheduled task that can recover
// the agent even if both agent and watchdog are completely dead.
// This is Layer 4 of the resilient update architecture.
func (u *Updater) InstallBootstrapRecoveryTask(serverURL string) error {
	if runtime.GOOS == "windows" {
		return u.installBootstrapTaskWindows(serverURL)
	} else if runtime.GOOS == "linux" {
		return u.installBootstrapTaskLinux(serverURL)
	}
	// macOS: launchd plist would go here
	log.Printf("[Bootstrap] Bootstrap recovery task not implemented for %s", runtime.GOOS)
	return nil
}

// installBootstrapTaskWindows creates a Windows scheduled task for bootstrap recovery
func (u *Updater) installBootstrapTaskWindows(serverURL string) error {
	// PowerShell script that checks agent health and recovers if needed
	scriptContent := fmt.Sprintf(`# Sentinel Bootstrap Recovery Script
# This script runs every %d hours to ensure agent availability
# Layer 4: Last resort recovery - independent of agent and watchdog

$ErrorActionPreference = 'Continue'
$LogFile = "$env:ProgramData\Sentinel\bootstrap-recovery.log"

function Write-Log($msg) {
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    "$timestamp - $msg" | Out-File -Append -FilePath $LogFile
}

Write-Log "Bootstrap recovery check starting..."

# Check 1: Is the watchdog service running?
$watchdog = Get-Service -Name "SentinelWatchdog" -ErrorAction SilentlyContinue
$agent = Get-Service -Name "SentinelAgent" -ErrorAction SilentlyContinue

if ($watchdog -and $watchdog.Status -eq "Running") {
    Write-Log "Watchdog is running - system healthy"

    # Check if agent is also running
    if ($agent -and $agent.Status -eq "Running") {
        Write-Log "Agent is also running - all good"
        exit 0
    } else {
        Write-Log "Agent not running but watchdog is - watchdog will recover it"
        exit 0
    }
}

Write-Log "WARNING: Watchdog not running - initiating bootstrap recovery"

# Check 2: Try to start the watchdog first
if ($watchdog) {
    Write-Log "Attempting to start watchdog service..."
    try {
        Start-Service -Name "SentinelWatchdog" -ErrorAction Stop
        Start-Sleep -Seconds 5
        $watchdog = Get-Service -Name "SentinelWatchdog"
        if ($watchdog.Status -eq "Running") {
            Write-Log "Watchdog started successfully"
            exit 0
        }
    } catch {
        Write-Log "Failed to start watchdog: $_"
    }
}

# Check 3: Try to start agent directly
if ($agent) {
    Write-Log "Attempting to start agent service..."
    try {
        Start-Service -Name "SentinelAgent" -ErrorAction Stop
        Start-Sleep -Seconds 5
        $agent = Get-Service -Name "SentinelAgent"
        if ($agent.Status -eq "Running") {
            Write-Log "Agent started successfully"
            exit 0
        }
    } catch {
        Write-Log "Failed to start agent: $_"
    }
}

Write-Log "CRITICAL: Both services failed to start - attempting fresh download"

# Layer 4 Nuclear Option: Download fresh agent from server
$serverUrl = "%s"
$downloadUrl = "$serverUrl/api/agent/update/download?platform=windows&arch=amd64"
$tempPath = "$env:TEMP\sentinel-bootstrap-recovery.exe"
$installPath = "C:\Program Files\Sentinel"

Write-Log "Downloading fresh agent from $downloadUrl"

try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

    # Download with proper TLS validation
    $caCertPath = "$env:ProgramData\Sentinel\certs\ca-cert.pem"
    if (Test-Path $caCertPath) {
        # Use internal CA cert for validation (mTLS/internal CA-signed certs)
        Add-Type @"
using System.Net;
using System.Net.Security;
using System.Security.Cryptography.X509Certificates;
public class CertValidator {
    public static void SetCA(string path) {
        ServicePointManager.ServerCertificateValidationCallback = delegate(
            object sender, X509Certificate cert, X509Chain chain, SslPolicyErrors errors) {
            if (errors == SslPolicyErrors.None) return true;
            X509Certificate2 ca = new X509Certificate2(path);
            chain.ChainPolicy.ExtraStore.Add(ca);
            chain.ChainPolicy.VerificationFlags = X509VerificationFlags.AllowUnknownCertificateAuthority;
            return chain.Build(new X509Certificate2(cert));
        };
    }
}
"@
        [CertValidator]::SetCA($caCertPath)
    }
    # If no CA cert, system trust store handles validation (Let's Encrypt)
    $webClient = New-Object System.Net.WebClient
    $webClient.DownloadFile($downloadUrl, $tempPath)

    if (Test-Path $tempPath) {
        $fileSize = (Get-Item $tempPath).Length
        Write-Log "Downloaded $fileSize bytes"

        if ($fileSize -gt 1000000) {  # At least 1MB
            # Stop any running services
            Stop-Service -Name "SentinelAgent" -Force -ErrorAction SilentlyContinue
            Stop-Service -Name "SentinelWatchdog" -Force -ErrorAction SilentlyContinue
            Start-Sleep -Seconds 3

            # Replace agent binary
            $agentPath = "$installPath\sentinel-agent.exe"
            if (Test-Path $agentPath) {
                Copy-Item $agentPath "$agentPath.backup" -Force -ErrorAction SilentlyContinue
            }
            Copy-Item $tempPath $agentPath -Force

            # Start services
            Start-Service -Name "SentinelWatchdog" -ErrorAction SilentlyContinue
            Start-Sleep -Seconds 2
            Start-Service -Name "SentinelAgent" -ErrorAction SilentlyContinue

            Write-Log "Bootstrap recovery completed - services restarted"
        } else {
            Write-Log "Downloaded file too small, aborting"
        }

        Remove-Item $tempPath -Force -ErrorAction SilentlyContinue
    }
} catch {
    Write-Log "Bootstrap recovery failed: $_"
}
`, BootstrapCheckInterval, serverURL)

	// Save the script
	scriptDir := filepath.Join(os.Getenv("ProgramData"), "Sentinel")
	os.MkdirAll(scriptDir, 0755)
	scriptPath := filepath.Join(scriptDir, "bootstrap-recovery.ps1")

	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0644); err != nil {
		return fmt.Errorf("failed to write bootstrap script: %w", err)
	}

	// Create the scheduled task
	// Delete existing task first
	exec.Command("schtasks", "/delete", "/tn", BootstrapTaskName, "/f").Run()

	// Create new task that runs every N hours
	taskCmd := fmt.Sprintf(
		`schtasks /create /tn "%s" /tr "powershell.exe -ExecutionPolicy Bypass -WindowStyle Hidden -File \"%s\"" /sc HOURLY /mo %d /ru SYSTEM /f /rl HIGHEST`,
		BootstrapTaskName, scriptPath, BootstrapCheckInterval,
	)

	cmd := exec.Command("cmd", "/C", taskCmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create scheduled task: %w - %s", err, string(output))
	}

	log.Printf("[Bootstrap] Windows bootstrap recovery task installed (runs every %d hours)", BootstrapCheckInterval)
	return nil
}

// installBootstrapTaskLinux creates a cron job for bootstrap recovery on Linux
func (u *Updater) installBootstrapTaskLinux(serverURL string) error {
	// Bash script for Linux bootstrap recovery
	scriptContent := fmt.Sprintf(`#!/bin/bash
# Sentinel Bootstrap Recovery Script
# This script runs every %d hours to ensure agent availability
# Layer 4: Last resort recovery - independent of agent and watchdog

LOG_FILE="/var/log/sentinel-bootstrap-recovery.log"
SERVER_URL="%s"

log() {
    echo "$(date '+%%Y-%%m-%%d %%H:%%M:%%S') - $1" >> "$LOG_FILE"
}

log "Bootstrap recovery check starting..."

# Check if agent service is running
if systemctl is-active --quiet SentinelAgent 2>/dev/null; then
    log "Agent service is running - system healthy"
    exit 0
fi

# Alternative check: sentinel-agent service name
if systemctl is-active --quiet sentinel-agent 2>/dev/null; then
    log "Agent service (sentinel-agent) is running - system healthy"
    exit 0
fi

log "WARNING: Agent service not running - attempting recovery"

# Try to start the service
log "Attempting to start agent service..."
if systemctl start SentinelAgent 2>/dev/null || systemctl start sentinel-agent 2>/dev/null; then
    sleep 5
    if systemctl is-active --quiet SentinelAgent 2>/dev/null || systemctl is-active --quiet sentinel-agent 2>/dev/null; then
        log "Agent started successfully"
        exit 0
    fi
fi

log "CRITICAL: Service failed to start - attempting fresh download"

# Download fresh agent
DOWNLOAD_URL="${SERVER_URL}/api/agent/update/download?platform=linux&arch=amd64"
TEMP_PATH="/tmp/sentinel-bootstrap-recovery"
INSTALL_PATH="/usr/local/bin/sentinel-agent"

log "Downloading fresh agent from $DOWNLOAD_URL"

CA_CERT="/etc/sentinel/certs/ca-cert.pem"
if [ -f "$CA_CERT" ]; then
    CURL_OPTS="--cacert $CA_CERT"
else
    CURL_OPTS=""
fi
if curl -s $CURL_OPTS -o "$TEMP_PATH" "$DOWNLOAD_URL"; then
    FILE_SIZE=$(stat -c%%s "$TEMP_PATH" 2>/dev/null || stat -f%%z "$TEMP_PATH" 2>/dev/null)
    log "Downloaded $FILE_SIZE bytes"

    if [ "$FILE_SIZE" -gt 1000000 ]; then
        # Stop service
        systemctl stop SentinelAgent 2>/dev/null
        systemctl stop sentinel-agent 2>/dev/null
        sleep 2

        # Backup and replace
        [ -f "$INSTALL_PATH" ] && cp "$INSTALL_PATH" "${INSTALL_PATH}.backup"
        cp "$TEMP_PATH" "$INSTALL_PATH"
        chmod +x "$INSTALL_PATH"

        # Start service
        systemctl start SentinelAgent 2>/dev/null || systemctl start sentinel-agent 2>/dev/null

        log "Bootstrap recovery completed - service restarted"
    else
        log "Downloaded file too small, aborting"
    fi

    rm -f "$TEMP_PATH"
else
    log "Download failed"
fi
`, BootstrapCheckInterval, serverURL)

	// Save the script
	scriptPath := "/usr/local/bin/sentinel-bootstrap-recovery.sh"
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		// Try alternative location
		scriptPath = "/opt/sentinel/bootstrap-recovery.sh"
		os.MkdirAll("/opt/sentinel", 0755)
		if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
			return fmt.Errorf("failed to write bootstrap script: %w", err)
		}
	}

	// Create cron job
	cronEntry := fmt.Sprintf("0 */%d * * * root %s\n", BootstrapCheckInterval, scriptPath)
	cronPath := "/etc/cron.d/sentinel-bootstrap"

	if err := os.WriteFile(cronPath, []byte(cronEntry), 0644); err != nil {
		return fmt.Errorf("failed to write cron job: %w", err)
	}

	log.Printf("[Bootstrap] Linux bootstrap recovery cron job installed (runs every %d hours)", BootstrapCheckInterval)
	return nil
}

// RemoveBootstrapRecoveryTask removes the bootstrap recovery scheduled task
func (u *Updater) RemoveBootstrapRecoveryTask() error {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("schtasks", "/delete", "/tn", BootstrapTaskName, "/f")
		cmd.Run() // Ignore errors

		// Also remove the script
		scriptPath := filepath.Join(os.Getenv("ProgramData"), "Sentinel", "bootstrap-recovery.ps1")
		os.Remove(scriptPath)
	} else if runtime.GOOS == "linux" {
		os.Remove("/etc/cron.d/sentinel-bootstrap")
		os.Remove("/usr/local/bin/sentinel-bootstrap-recovery.sh")
		os.Remove("/opt/sentinel/bootstrap-recovery.sh")
	}

	log.Println("[Bootstrap] Bootstrap recovery task removed")
	return nil
}
