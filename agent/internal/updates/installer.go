package updates

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// InstallResult represents the result of an update installation
type InstallResult struct {
	Success          bool      `json:"success"`
	InstalledCount   int       `json:"installedCount"`
	FailedCount      int       `json:"failedCount"`
	RebootRequired   bool      `json:"rebootRequired"`
	InstalledUpdates []string  `json:"installedUpdates,omitempty"`
	FailedUpdates    []string  `json:"failedUpdates,omitempty"`
	Error            string    `json:"error,omitempty"`
	StartedAt        time.Time `json:"startedAt"`
	CompletedAt      time.Time `json:"completedAt"`
}

// InstallOptions configures the installation behavior
type InstallOptions struct {
	SecurityOnly     bool     `json:"securityOnly"`     // Only install security updates
	SpecificKBs      []string `json:"specificKBs"`      // Install only specific KB articles
	AcceptEULA       bool     `json:"acceptEULA"`       // Automatically accept EULAs
	AllowReboot      bool     `json:"allowReboot"`      // Allow automatic reboot if required
	RebootDelaySecs  int      `json:"rebootDelaySecs"`  // Delay before reboot (if allowed)
}

// Installer handles Windows Update installation
type Installer struct {
	mu            sync.Mutex
	isInstalling  bool
	currentResult *InstallResult
	progress      chan InstallProgress
}

// InstallProgress reports installation progress
type InstallProgress struct {
	Phase           string `json:"phase"`           // "downloading", "installing", "complete"
	CurrentUpdate   string `json:"currentUpdate"`
	CurrentIndex    int    `json:"currentIndex"`
	TotalUpdates    int    `json:"totalUpdates"`
	PercentComplete int    `json:"percentComplete"`
}

// NewInstaller creates a new update installer
func NewInstaller() *Installer {
	return &Installer{
		progress: make(chan InstallProgress, 100),
	}
}

// IsInstalling returns true if an installation is in progress
func (i *Installer) IsInstalling() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.isInstalling
}

// GetProgress returns the progress channel
func (i *Installer) GetProgress() <-chan InstallProgress {
	return i.progress
}

// InstallUpdates downloads and installs pending Windows updates
func (i *Installer) InstallUpdates(ctx context.Context, opts InstallOptions) (*InstallResult, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("update installation only supported on Windows")
	}

	i.mu.Lock()
	if i.isInstalling {
		i.mu.Unlock()
		return nil, fmt.Errorf("installation already in progress")
	}
	i.isInstalling = true
	i.mu.Unlock()

	defer func() {
		i.mu.Lock()
		i.isInstalling = false
		i.mu.Unlock()
	}()

	result := &InstallResult{
		StartedAt: time.Now(),
	}

	// Build the PowerShell script for installation
	psScript := i.buildInstallScript(opts)

	// Create timeout context (30 minutes for updates)
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", psScript)
	output, err := cmd.Output()

	result.CompletedAt = time.Now()

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.Error = "installation timed out"
		} else {
			result.Error = fmt.Sprintf("installation failed: %v", err)
		}
		return result, nil
	}

	// Parse the output
	i.parseInstallOutput(string(output), result)

	return result, nil
}

func (i *Installer) buildInstallScript(opts InstallOptions) string {
	var filterCriteria string
	if opts.SecurityOnly {
		filterCriteria = `$Update.Categories | Where-Object { $_.Name -match 'Security' }`
	} else {
		filterCriteria = "$true"
	}

	// Build KB filter if specific KBs requested
	kbFilter := ""
	if len(opts.SpecificKBs) > 0 {
		kbs := make([]string, len(opts.SpecificKBs))
		for idx, kb := range opts.SpecificKBs {
			// Normalize KB format (remove "KB" prefix if present)
			kb = strings.TrimPrefix(strings.ToUpper(kb), "KB")
			kbs[idx] = fmt.Sprintf("'%s'", kb)
		}
		kbFilter = fmt.Sprintf(`
            $kbList = @(%s)
            $hasTargetKB = $false
            foreach ($kbID in $Update.KBArticleIDs) {
                if ($kbList -contains $kbID) {
                    $hasTargetKB = $true
                    break
                }
            }
            if (-not $hasTargetKB) { continue }
        `, strings.Join(kbs, ","))
	}

	acceptEULA := "True"
	if !opts.AcceptEULA {
		acceptEULA = "False"
	}

	script := fmt.Sprintf(`
$ErrorActionPreference = 'Continue'

try {
    $UpdateSession = New-Object -ComObject Microsoft.Update.Session
    $UpdateSearcher = $UpdateSession.CreateUpdateSearcher()

    Write-Output "PHASE:searching"
    $SearchResult = $UpdateSearcher.Search("IsInstalled=0 and Type='Software'")

    if ($SearchResult.Updates.Count -eq 0) {
        Write-Output "RESULT:no_updates"
        exit 0
    }

    # Filter and collect updates to install
    $UpdatesToInstall = New-Object -ComObject Microsoft.Update.UpdateColl
    $installedNames = @()
    $failedNames = @()

    foreach ($Update in $SearchResult.Updates) {
        # Apply security filter
        $matchesCriteria = %s
        if (-not $matchesCriteria) { continue }

        # Apply KB filter
        %s

        # Accept EULA if needed
        if ($Update.EulaAccepted -eq $false -and %s) {
            $Update.AcceptEula()
        }

        $UpdatesToInstall.Add($Update) | Out-Null
    }

    if ($UpdatesToInstall.Count -eq 0) {
        Write-Output "RESULT:no_matching_updates"
        exit 0
    }

    Write-Output "TOTAL:$($UpdatesToInstall.Count)"

    # Download updates
    Write-Output "PHASE:downloading"
    $Downloader = $UpdateSession.CreateUpdateDownloader()
    $Downloader.Updates = $UpdatesToInstall

    $idx = 0
    foreach ($Update in $UpdatesToInstall) {
        $idx++
        Write-Output "DOWNLOAD:$idx|$($Update.Title)"
    }

    $DownloadResult = $Downloader.Download()

    if ($DownloadResult.ResultCode -ne 2) {
        Write-Output "ERROR:Download failed with code $($DownloadResult.ResultCode)"
    }

    # Install updates
    Write-Output "PHASE:installing"
    $Installer = $UpdateSession.CreateUpdateInstaller()
    $Installer.Updates = $UpdatesToInstall

    $idx = 0
    foreach ($Update in $UpdatesToInstall) {
        $idx++
        Write-Output "INSTALL:$idx|$($Update.Title)"
    }

    $InstallResult = $Installer.Install()

    # Collect results
    $installed = 0
    $failed = 0
    for ($i = 0; $i -lt $UpdatesToInstall.Count; $i++) {
        $updateResult = $InstallResult.GetUpdateResult($i)
        $updateTitle = $UpdatesToInstall.Item($i).Title

        if ($updateResult.ResultCode -eq 2) {
            $installed++
            $installedNames += $updateTitle
            Write-Output "SUCCESS:$updateTitle"
        } else {
            $failed++
            $failedNames += $updateTitle
            Write-Output "FAILED:$updateTitle|Code:$($updateResult.ResultCode)"
        }
    }

    # Check if reboot required
    $SystemInfo = New-Object -ComObject Microsoft.Update.SystemInfo
    $rebootRequired = $SystemInfo.RebootRequired

    Write-Output "INSTALLED:$installed"
    Write-Output "FAILEDCOUNT:$failed"
    Write-Output "REBOOT:$rebootRequired"
    Write-Output "PHASE:complete"

} catch {
    Write-Output "ERROR:$($_.Exception.Message)"
}
`, filterCriteria, kbFilter, acceptEULA)

	return script
}

func (i *Installer) parseInstallOutput(output string, result *InstallResult) {
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "RESULT:") {
			val := strings.TrimPrefix(line, "RESULT:")
			if val == "no_updates" || val == "no_matching_updates" {
				result.Success = true
			}
		} else if strings.HasPrefix(line, "INSTALLED:") {
			count, _ := strconv.Atoi(strings.TrimPrefix(line, "INSTALLED:"))
			result.InstalledCount = count
		} else if strings.HasPrefix(line, "FAILEDCOUNT:") {
			count, _ := strconv.Atoi(strings.TrimPrefix(line, "FAILEDCOUNT:"))
			result.FailedCount = count
		} else if strings.HasPrefix(line, "REBOOT:") {
			result.RebootRequired = strings.TrimPrefix(line, "REBOOT:") == "True"
		} else if strings.HasPrefix(line, "SUCCESS:") {
			result.InstalledUpdates = append(result.InstalledUpdates, strings.TrimPrefix(line, "SUCCESS:"))
		} else if strings.HasPrefix(line, "FAILED:") {
			result.FailedUpdates = append(result.FailedUpdates, strings.TrimPrefix(line, "FAILED:"))
		} else if strings.HasPrefix(line, "ERROR:") {
			result.Error = strings.TrimPrefix(line, "ERROR:")
		} else if strings.HasPrefix(line, "PHASE:") {
			phase := strings.TrimPrefix(line, "PHASE:")
			if phase == "complete" {
				result.Success = result.Error == ""
			}
			// Send progress update
			select {
			case i.progress <- InstallProgress{Phase: phase}:
			default:
			}
		} else if strings.HasPrefix(line, "TOTAL:") {
			total, _ := strconv.Atoi(strings.TrimPrefix(line, "TOTAL:"))
			select {
			case i.progress <- InstallProgress{Phase: "found", TotalUpdates: total}:
			default:
			}
		} else if strings.HasPrefix(line, "DOWNLOAD:") || strings.HasPrefix(line, "INSTALL:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				subParts := strings.SplitN(parts[1], "|", 2)
				if len(subParts) == 2 {
					idx, _ := strconv.Atoi(subParts[0])
					phase := "downloading"
					if strings.HasPrefix(line, "INSTALL:") {
						phase = "installing"
					}
					select {
					case i.progress <- InstallProgress{
						Phase:         phase,
						CurrentIndex:  idx,
						CurrentUpdate: subParts[1],
					}:
					default:
					}
				}
			}
		}
	}

	// If no explicit success/failure, determine from counts
	if !result.Success && result.Error == "" && result.InstalledCount > 0 {
		result.Success = true
	}
}

// ScheduleReboot schedules a system reboot
func ScheduleReboot(ctx context.Context, delaySecs int, message string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("reboot scheduling only supported on Windows")
	}

	if delaySecs < 0 {
		delaySecs = 60 // Default 60 second delay
	}

	if message == "" {
		message = "System will restart to complete Windows Update installation."
	}

	cmd := exec.CommandContext(ctx, "shutdown", "/r", "/t", strconv.Itoa(delaySecs), "/c", message)
	return cmd.Run()
}

// CancelReboot cancels a scheduled reboot
func CancelReboot(ctx context.Context) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("reboot cancellation only supported on Windows")
	}

	cmd := exec.CommandContext(ctx, "shutdown", "/a")
	return cmd.Run()
}
