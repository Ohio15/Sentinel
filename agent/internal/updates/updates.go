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

// UpdateInfo represents information about a pending update
type UpdateInfo struct {
	Title       string `json:"title"`
	KB          string `json:"kb,omitempty"`
	Severity    string `json:"severity,omitempty"`
	SizeMB      int    `json:"sizeMB,omitempty"`
	IsSecurityUpdate bool `json:"isSecurityUpdate"`
}

// UpdateStatus represents the current update status of a device
type UpdateStatus struct {
	PendingCount         int          `json:"pendingCount"`
	SecurityUpdateCount  int          `json:"securityUpdateCount"`
	LastChecked          time.Time    `json:"lastChecked"`
	LastUpdateInstalled  *time.Time   `json:"lastUpdateInstalled,omitempty"`
	PendingUpdates       []UpdateInfo `json:"pendingUpdates,omitempty"`
	RebootRequired       bool         `json:"rebootRequired"`
}

// Checker handles update status checking
type Checker struct {
	mu          sync.RWMutex
	lastStatus  *UpdateStatus
	lastCheck   time.Time
	checkInterval time.Duration
}

// NewChecker creates a new update checker
func NewChecker() *Checker {
	return &Checker{
		checkInterval: 1 * time.Hour, // Check every hour by default
	}
}

// GetStatus returns the current update status, using cached data if recent
func (c *Checker) GetStatus(ctx context.Context, forceRefresh bool) (*UpdateStatus, error) {
	c.mu.RLock()
	if !forceRefresh && c.lastStatus != nil && time.Since(c.lastCheck) < c.checkInterval {
		status := c.lastStatus
		c.mu.RUnlock()
		return status, nil
	}
	c.mu.RUnlock()

	// Need to refresh
	status, err := c.checkUpdates(ctx)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.lastStatus = status
	c.lastCheck = time.Now()
	c.mu.Unlock()

	return status, nil
}

// checkUpdates performs the actual update check
func (c *Checker) checkUpdates(ctx context.Context) (*UpdateStatus, error) {
	if runtime.GOOS != "windows" {
		// For non-Windows, return empty status for now
		return &UpdateStatus{
			LastChecked: time.Now(),
		}, nil
	}

	return c.checkWindowsUpdates(ctx)
}

// checkWindowsUpdates checks for pending Windows updates
func (c *Checker) checkWindowsUpdates(ctx context.Context) (*UpdateStatus, error) {
	status := &UpdateStatus{
		LastChecked:    time.Now(),
		PendingUpdates: []UpdateInfo{},
	}

	// PowerShell script to check for updates
	psScript := `
$UpdateSession = New-Object -ComObject Microsoft.Update.Session
$UpdateSearcher = $UpdateSession.CreateUpdateSearcher()

# Check for pending updates
try {
    $SearchResult = $UpdateSearcher.Search("IsInstalled=0 and Type='Software'")

    $results = @()
    foreach ($Update in $SearchResult.Updates) {
        $isSecurity = $false
        foreach ($category in $Update.Categories) {
            if ($category.Name -match 'Security') {
                $isSecurity = $true
                break
            }
        }

        $kb = ""
        if ($Update.KBArticleIDs.Count -gt 0) {
            $kb = "KB" + $Update.KBArticleIDs[0]
        }

        $results += [PSCustomObject]@{
            Title = $Update.Title
            KB = $kb
            Severity = $Update.MsrcSeverity
            SizeMB = [math]::Round($Update.MaxDownloadSize / 1MB, 0)
            IsSecurity = $isSecurity
        }
    }

    # Check reboot required
    $SystemInfo = New-Object -ComObject Microsoft.Update.SystemInfo
    $rebootRequired = $SystemInfo.RebootRequired

    # Get last update installed date
    $lastUpdate = Get-HotFix | Sort-Object InstalledOn -Descending | Select-Object -First 1
    $lastUpdateDate = if ($lastUpdate.InstalledOn) { $lastUpdate.InstalledOn.ToString("yyyy-MM-ddTHH:mm:ssZ") } else { "" }

    # Output as simple format for parsing
    Write-Output "COUNT:$($results.Count)"
    Write-Output "SECURITY:$($results | Where-Object { $_.IsSecurity } | Measure-Object | Select-Object -ExpandProperty Count)"
    Write-Output "REBOOT:$rebootRequired"
    Write-Output "LASTUPDATE:$lastUpdateDate"

    foreach ($r in $results | Select-Object -First 20) {
        Write-Output "UPDATE:$($r.Title)|$($r.KB)|$($r.Severity)|$($r.SizeMB)|$($r.IsSecurity)"
    }
} catch {
    Write-Output "ERROR:$($_.Exception.Message)"
}
`

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "powershell", "-NoProfile", "-Command", psScript)
	output, err := cmd.Output()
	if err != nil {
		// Don't fail completely, just return empty status
		return status, nil
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "COUNT:") {
			count, _ := strconv.Atoi(strings.TrimPrefix(line, "COUNT:"))
			status.PendingCount = count
		} else if strings.HasPrefix(line, "SECURITY:") {
			count, _ := strconv.Atoi(strings.TrimPrefix(line, "SECURITY:"))
			status.SecurityUpdateCount = count
		} else if strings.HasPrefix(line, "REBOOT:") {
			status.RebootRequired = strings.TrimPrefix(line, "REBOOT:") == "True"
		} else if strings.HasPrefix(line, "LASTUPDATE:") {
			dateStr := strings.TrimPrefix(line, "LASTUPDATE:")
			if dateStr != "" {
				if t, err := time.Parse("2006-01-02T15:04:05Z", dateStr); err == nil {
					status.LastUpdateInstalled = &t
				}
			}
		} else if strings.HasPrefix(line, "UPDATE:") {
			parts := strings.Split(strings.TrimPrefix(line, "UPDATE:"), "|")
			if len(parts) >= 5 {
				sizeMB, _ := strconv.Atoi(parts[3])
				status.PendingUpdates = append(status.PendingUpdates, UpdateInfo{
					Title:            parts[0],
					KB:               parts[1],
					Severity:         parts[2],
					SizeMB:           sizeMB,
					IsSecurityUpdate: parts[4] == "True",
				})
			}
		} else if strings.HasPrefix(line, "ERROR:") {
			return status, fmt.Errorf("update check failed: %s", strings.TrimPrefix(line, "ERROR:"))
		}
	}

	return status, nil
}

// SetCheckInterval sets how often to recheck for updates
func (c *Checker) SetCheckInterval(interval time.Duration) {
	c.mu.Lock()
	c.checkInterval = interval
	c.mu.Unlock()
}
