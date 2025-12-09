package updater

import (
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
	"time"
)

// VersionInfo contains information about an available version
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

// UpdateResult represents the result of an update check or operation
type UpdateResult struct {
	Available      bool        `json:"available"`
	CurrentVersion string      `json:"currentVersion"`
	LatestVersion  string      `json:"latestVersion"`
	VersionInfo    *VersionInfo `json:"versionInfo,omitempty"`
	Error          string      `json:"error,omitempty"`
}

// Updater handles agent self-updates
type Updater struct {
	serverURL      string
	currentVersion string
	httpClient     *http.Client
	checkInterval  time.Duration
}

// New creates a new Updater instance
func New(serverURL, currentVersion string) *Updater {
	return &Updater{
		serverURL:      serverURL,
		currentVersion: currentVersion,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		checkInterval: 1 * time.Hour,
	}
}

// SetCheckInterval sets the interval between update checks
func (u *Updater) SetCheckInterval(interval time.Duration) {
	u.checkInterval = interval
}

// CheckForUpdate checks if a newer version is available
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
		return nil, fmt.Errorf("version check returned status %d", resp.StatusCode)
	}

	var result UpdateResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	result.CurrentVersion = u.currentVersion
	return &result, nil
}

// DownloadUpdate downloads the new agent binary
func (u *Updater) DownloadUpdate(ctx context.Context, info *VersionInfo) (string, error) {
	log.Printf("Downloading update v%s from %s", info.Version, info.DownloadURL)

	// Create temp directory for download
	tempDir := os.TempDir()
	tempFile := filepath.Join(tempDir, fmt.Sprintf("sentinel-agent-%s-%s-%s.tmp",
		info.Version, info.Platform, info.Arch))

	// Download file
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

	// Download and compute checksum simultaneously
	hasher := sha256.New()
	writer := io.MultiWriter(out, hasher)

	_, err = io.Copy(writer, resp.Body)
	if err != nil {
		os.Remove(tempFile)
		return "", fmt.Errorf("download failed during transfer: %w", err)
	}

	// Verify checksum
	checksum := hex.EncodeToString(hasher.Sum(nil))
	if info.Checksum != "" && checksum != info.Checksum {
		os.Remove(tempFile)
		return "", fmt.Errorf("checksum mismatch: expected %s, got %s", info.Checksum, checksum)
	}

	log.Printf("Download complete, checksum verified: %s", checksum)
	return tempFile, nil
}

// ApplyUpdate applies the downloaded update
func (u *Updater) ApplyUpdate(ctx context.Context, downloadPath string) error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get current executable path: %w", err)
	}

	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	log.Printf("Applying update from %s to %s", downloadPath, currentExe)

	// Platform-specific update logic
	if runtime.GOOS == "windows" {
		return u.applyUpdateWindows(currentExe, downloadPath)
	}
	return u.applyUpdateUnix(currentExe, downloadPath)
}

// applyUpdateWindows handles update on Windows
func (u *Updater) applyUpdateWindows(currentExe, downloadPath string) error {
	// On Windows, we can't replace a running executable directly
	// Create a batch script to perform the update after the agent stops

	batchPath := filepath.Join(os.TempDir(), "sentinel-update.bat")
	backupPath := currentExe + ".old"

	// Create batch script
	batchContent := fmt.Sprintf(`@echo off
echo Waiting for agent to stop...
timeout /t 2 /nobreak > nul

echo Backing up current agent...
move /y "%s" "%s"
if %%errorlevel%% neq 0 (
    echo Failed to backup current agent
    exit /b 1
)

echo Installing new agent...
move /y "%s" "%s"
if %%errorlevel%% neq 0 (
    echo Failed to install new agent, restoring backup...
    move /y "%s" "%s"
    exit /b 1
)

echo Cleaning up backup...
del /f "%s" 2>nul

echo Starting updated agent...
net start SentinelAgent

echo Update complete!
del /f "%s"
`, currentExe, backupPath, downloadPath, currentExe, backupPath, currentExe, backupPath, batchPath)

	if err := os.WriteFile(batchPath, []byte(batchContent), 0755); err != nil {
		return fmt.Errorf("failed to create update script: %w", err)
	}

	// Stop service, run batch, which will restart service
	cmd := exec.Command("cmd.exe", "/C", "net stop SentinelAgent && start /min cmd.exe /C "+batchPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start update process: %w", err)
	}

	log.Printf("Update initiated, agent will restart shortly")
	return nil
}

// applyUpdateUnix handles update on Unix systems
func (u *Updater) applyUpdateUnix(currentExe, downloadPath string) error {
	backupPath := currentExe + ".old"

	// Make new binary executable
	if err := os.Chmod(downloadPath, 0755); err != nil {
		return fmt.Errorf("failed to set executable permissions: %w", err)
	}

	// Backup current binary
	if err := os.Rename(currentExe, backupPath); err != nil {
		return fmt.Errorf("failed to backup current binary: %w", err)
	}

	// Move new binary into place
	if err := os.Rename(downloadPath, currentExe); err != nil {
		// Try to restore backup
		os.Rename(backupPath, currentExe)
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	// Remove backup
	os.Remove(backupPath)

	// Restart service
	cmd := exec.Command("systemctl", "restart", "sentinel-agent")
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to restart via systemctl, manual restart may be required: %v", err)
	}

	log.Printf("Update applied, agent will restart shortly")
	return nil
}

// RunUpdateLoop periodically checks for and applies updates
func (u *Updater) RunUpdateLoop(ctx context.Context) {
	// Initial delay before first check
	time.Sleep(30 * time.Second)

	ticker := time.NewTicker(u.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			u.checkAndUpdate(ctx)
		}
	}
}

func (u *Updater) checkAndUpdate(ctx context.Context) {
	log.Println("Checking for updates...")

	result, err := u.CheckForUpdate(ctx)
	if err != nil {
		log.Printf("Update check failed: %v", err)
		return
	}

	if !result.Available {
		log.Printf("No update available (current: v%s)", u.currentVersion)
		return
	}

	log.Printf("Update available: v%s -> v%s", u.currentVersion, result.LatestVersion)

	if result.VersionInfo == nil {
		log.Printf("No version info in response")
		return
	}

	// Download update
	downloadPath, err := u.DownloadUpdate(ctx, result.VersionInfo)
	if err != nil {
		log.Printf("Failed to download update: %v", err)
		return
	}

	// Apply update
	if err := u.ApplyUpdate(ctx, downloadPath); err != nil {
		log.Printf("Failed to apply update: %v", err)
		os.Remove(downloadPath)
		return
	}
}

// CompareVersions compares two version strings
// Returns: -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func CompareVersions(v1, v2 string) int {
	// Simple version comparison - assumes semver format
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
