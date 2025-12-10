package diagnostics

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"
)

// DiagnosticResult contains all collected diagnostic data
type DiagnosticResult struct {
	SystemErrors    []LogEntry       `json:"systemErrors"`
	ApplicationLogs []LogEntry       `json:"applicationLogs"`
	SecurityEvents  []LogEntry       `json:"securityEvents"`
	ActivePrograms  []ProcessInfo    `json:"activePrograms"`
	RecentCrashes   []LogEntry       `json:"recentCrashes"`
	HardwareEvents  []LogEntry       `json:"hardwareEvents"`
	NetworkEvents   []LogEntry       `json:"networkEvents"`
	CollectedAt     string           `json:"collectedAt"`
	HoursBack       int              `json:"hoursBack"`
}

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"`
	Level     string `json:"level"`
	EventID   string `json:"eventId,omitempty"`
	Message   string `json:"message"`
}

// ProcessInfo represents running process information
type ProcessInfo struct {
	Name            string  `json:"name"`
	PID             int     `json:"pid"`
	Path            string  `json:"path,omitempty"`
	Version         string  `json:"version,omitempty"`
	Company         string  `json:"company,omitempty"`
	MemoryMB        float64 `json:"memoryMB"`
	CPUPercent      float64 `json:"cpuPercent"`
	StartTime       string  `json:"startTime,omitempty"`
	SessionDuration string  `json:"sessionDuration,omitempty"`
}

// Collector handles diagnostic data collection
type Collector struct{}

// New creates a new diagnostics collector
func New() *Collector {
	return &Collector{}
}

// CollectAll gathers all diagnostic data for the specified time period
func (c *Collector) CollectAll(ctx context.Context, hoursBack int) (*DiagnosticResult, error) {
	result := &DiagnosticResult{
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
		HoursBack:   hoursBack,
	}

	if runtime.GOOS != "windows" {
		return result, fmt.Errorf("diagnostics collection only supported on Windows")
	}

	// Collect in parallel using channels
	type collectionResult struct {
		name string
		data interface{}
		err  error
	}

	results := make(chan collectionResult, 7)

	// System Errors (Event Viewer - System log, Error/Critical)
	go func() {
		data, err := c.collectWindowsEventLog(ctx, "System", []string{"Error", "Critical"}, hoursBack)
		results <- collectionResult{"systemErrors", data, err}
	}()

	// Application Logs (Event Viewer - Application log, Error/Warning)
	go func() {
		data, err := c.collectWindowsEventLog(ctx, "Application", []string{"Error", "Warning"}, hoursBack)
		results <- collectionResult{"applicationLogs", data, err}
	}()

	// Security Events (Event Viewer - Security log, audit failures)
	go func() {
		data, err := c.collectSecurityEvents(ctx, hoursBack)
		results <- collectionResult{"securityEvents", data, err}
	}()

	// Recent Crashes (Windows Error Reporting)
	go func() {
		data, err := c.collectRecentCrashes(ctx, hoursBack)
		results <- collectionResult{"recentCrashes", data, err}
	}()

	// Hardware Events
	go func() {
		data, err := c.collectHardwareEvents(ctx, hoursBack)
		results <- collectionResult{"hardwareEvents", data, err}
	}()

	// Network Events
	go func() {
		data, err := c.collectNetworkEvents(ctx, hoursBack)
		results <- collectionResult{"networkEvents", data, err}
	}()

	// Active Programs
	go func() {
		data, err := c.collectActivePrograms(ctx, hoursBack)
		results <- collectionResult{"activePrograms", data, err}
	}()

	// Collect all results
	for i := 0; i < 7; i++ {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case r := <-results:
			switch r.name {
			case "systemErrors":
				if r.err == nil {
					result.SystemErrors = r.data.([]LogEntry)
				}
			case "applicationLogs":
				if r.err == nil {
					result.ApplicationLogs = r.data.([]LogEntry)
				}
			case "securityEvents":
				if r.err == nil {
					result.SecurityEvents = r.data.([]LogEntry)
				}
			case "recentCrashes":
				if r.err == nil {
					result.RecentCrashes = r.data.([]LogEntry)
				}
			case "hardwareEvents":
				if r.err == nil {
					result.HardwareEvents = r.data.([]LogEntry)
				}
			case "networkEvents":
				if r.err == nil {
					result.NetworkEvents = r.data.([]LogEntry)
				}
			case "activePrograms":
				if r.err == nil {
					result.ActivePrograms = r.data.([]ProcessInfo)
				}
			}
		}
	}

	return result, nil
}

// collectWindowsEventLog retrieves events from Windows Event Viewer
func (c *Collector) collectWindowsEventLog(ctx context.Context, logName string, levels []string, hoursBack int) ([]LogEntry, error) {
	// Build PowerShell filter
	startTime := time.Now().Add(-time.Duration(hoursBack) * time.Hour)
	startTimeStr := startTime.Format("2006-01-02T15:04:05")

	levelFilter := ""
	for i, level := range levels {
		levelNum := c.getLevelNumber(level)
		if i > 0 {
			levelFilter += " -or "
		}
		levelFilter += fmt.Sprintf("Level -eq %d", levelNum)
	}

	// PowerShell command to get event logs
	psCmd := fmt.Sprintf(`
		Get-WinEvent -FilterHashtable @{
			LogName='%s'
			StartTime='%s'
		} -ErrorAction SilentlyContinue |
		Where-Object { %s } |
		Select-Object -First 100 TimeCreated, ProviderName, LevelDisplayName, Id, Message |
		ForEach-Object {
			$msg = if ($_.Message) { $_.Message.Substring(0, [Math]::Min(500, $_.Message.Length)) } else { "No message" }
			[PSCustomObject]@{
				TimeCreated = $_.TimeCreated.ToString("yyyy-MM-ddTHH:mm:ss")
				Provider = $_.ProviderName
				Level = $_.LevelDisplayName
				EventId = $_.Id
				Message = $msg -replace '\r?\n', ' '
			}
		} | ConvertTo-Json -Compress
	`, logName, startTimeStr, levelFilter)

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", psCmd)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return c.parseEventLogOutput(string(output))
}

func (c *Collector) getLevelNumber(level string) int {
	switch level {
	case "Critical":
		return 1
	case "Error":
		return 2
	case "Warning":
		return 3
	case "Information":
		return 4
	default:
		return 2
	}
}

func (c *Collector) parseEventLogOutput(output string) ([]LogEntry, error) {
	output = strings.TrimSpace(output)
	if output == "" || output == "null" {
		return []LogEntry{}, nil
	}

	var entries []LogEntry

	// Handle both single object and array from PowerShell JSON
	if strings.HasPrefix(output, "[") {
		// Array of events
		lines := strings.Split(output, "},{")
		for _, line := range lines {
			line = strings.Trim(line, "[]{}")
			entry := c.parseEventLine(line)
			if entry.Timestamp != "" {
				entries = append(entries, entry)
			}
		}
	} else if strings.HasPrefix(output, "{") {
		// Single event
		entry := c.parseEventLine(strings.Trim(output, "{}"))
		if entry.Timestamp != "" {
			entries = append(entries, entry)
		}
	}

	return entries, nil
}

func (c *Collector) parseEventLine(line string) LogEntry {
	entry := LogEntry{}

	// Simple JSON field extraction
	fields := map[string]*string{
		"TimeCreated": &entry.Timestamp,
		"Provider":    &entry.Source,
		"Level":       &entry.Level,
		"EventId":     &entry.EventID,
		"Message":     &entry.Message,
	}

	for key, ptr := range fields {
		start := strings.Index(line, `"`+key+`":`)
		if start == -1 {
			continue
		}
		start += len(key) + 3

		// Check if value is string or number
		if start < len(line) && line[start] == '"' {
			start++
			end := strings.Index(line[start:], `"`)
			if end != -1 {
				*ptr = line[start : start+end]
			}
		} else {
			// Number value
			end := strings.IndexAny(line[start:], ",}")
			if end != -1 {
				*ptr = strings.TrimSpace(line[start : start+end])
			}
		}
	}

	return entry
}

// collectSecurityEvents retrieves security audit failures
func (c *Collector) collectSecurityEvents(ctx context.Context, hoursBack int) ([]LogEntry, error) {
	startTime := time.Now().Add(-time.Duration(hoursBack) * time.Hour)
	startTimeStr := startTime.Format("2006-01-02T15:04:05")

	// Security events for failed logins, permission changes, etc.
	psCmd := fmt.Sprintf(`
		Get-WinEvent -FilterHashtable @{
			LogName='Security'
			StartTime='%s'
		} -ErrorAction SilentlyContinue |
		Where-Object { $_.Keywords -band 0x10000000000000 } |
		Select-Object -First 50 TimeCreated, ProviderName, LevelDisplayName, Id, Message |
		ForEach-Object {
			$msg = if ($_.Message) { $_.Message.Substring(0, [Math]::Min(500, $_.Message.Length)) } else { "No message" }
			[PSCustomObject]@{
				TimeCreated = $_.TimeCreated.ToString("yyyy-MM-ddTHH:mm:ss")
				Provider = $_.ProviderName
				Level = "Audit Failure"
				EventId = $_.Id
				Message = $msg -replace '\r?\n', ' '
			}
		} | ConvertTo-Json -Compress
	`, startTimeStr)

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", psCmd)
	output, err := cmd.Output()
	if err != nil {
		return []LogEntry{}, nil // Security logs may require elevation
	}

	return c.parseEventLogOutput(string(output))
}

// collectRecentCrashes retrieves application crash reports
func (c *Collector) collectRecentCrashes(ctx context.Context, hoursBack int) ([]LogEntry, error) {
	startTime := time.Now().Add(-time.Duration(hoursBack) * time.Hour)
	startTimeStr := startTime.Format("2006-01-02T15:04:05")

	// Windows Error Reporting events
	psCmd := fmt.Sprintf(`
		Get-WinEvent -FilterHashtable @{
			LogName='Application'
			ProviderName='Windows Error Reporting'
			StartTime='%s'
		} -ErrorAction SilentlyContinue |
		Select-Object -First 50 TimeCreated, ProviderName, LevelDisplayName, Id, Message |
		ForEach-Object {
			$msg = if ($_.Message) { $_.Message.Substring(0, [Math]::Min(500, $_.Message.Length)) } else { "No message" }
			[PSCustomObject]@{
				TimeCreated = $_.TimeCreated.ToString("yyyy-MM-ddTHH:mm:ss")
				Provider = $_.ProviderName
				Level = $_.LevelDisplayName
				EventId = $_.Id
				Message = $msg -replace '\r?\n', ' '
			}
		} | ConvertTo-Json -Compress
	`, startTimeStr)

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", psCmd)
	output, err := cmd.Output()
	if err != nil {
		return []LogEntry{}, nil
	}

	return c.parseEventLogOutput(string(output))
}

// collectHardwareEvents retrieves hardware-related events
func (c *Collector) collectHardwareEvents(ctx context.Context, hoursBack int) ([]LogEntry, error) {
	startTime := time.Now().Add(-time.Duration(hoursBack) * time.Hour)
	startTimeStr := startTime.Format("2006-01-02T15:04:05")

	// Hardware events from System log (disk, memory, driver issues)
	psCmd := fmt.Sprintf(`
		Get-WinEvent -FilterHashtable @{
			LogName='System'
			StartTime='%s'
		} -ErrorAction SilentlyContinue |
		Where-Object {
			$_.ProviderName -match 'disk|storage|memory|driver|hardware|ntfs|volsnap|volmgr|iastor' -and
			($_.Level -eq 1 -or $_.Level -eq 2 -or $_.Level -eq 3)
		} |
		Select-Object -First 50 TimeCreated, ProviderName, LevelDisplayName, Id, Message |
		ForEach-Object {
			$msg = if ($_.Message) { $_.Message.Substring(0, [Math]::Min(500, $_.Message.Length)) } else { "No message" }
			[PSCustomObject]@{
				TimeCreated = $_.TimeCreated.ToString("yyyy-MM-ddTHH:mm:ss")
				Provider = $_.ProviderName
				Level = $_.LevelDisplayName
				EventId = $_.Id
				Message = $msg -replace '\r?\n', ' '
			}
		} | ConvertTo-Json -Compress
	`, startTimeStr)

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", psCmd)
	output, err := cmd.Output()
	if err != nil {
		return []LogEntry{}, nil
	}

	return c.parseEventLogOutput(string(output))
}

// collectNetworkEvents retrieves network-related events
func (c *Collector) collectNetworkEvents(ctx context.Context, hoursBack int) ([]LogEntry, error) {
	startTime := time.Now().Add(-time.Duration(hoursBack) * time.Hour)
	startTimeStr := startTime.Format("2006-01-02T15:04:05")

	// Network events
	psCmd := fmt.Sprintf(`
		Get-WinEvent -FilterHashtable @{
			LogName='System'
			StartTime='%s'
		} -ErrorAction SilentlyContinue |
		Where-Object {
			$_.ProviderName -match 'tcpip|dhcp|dns|netbt|network|wlan|wifi|ethernet' -and
			($_.Level -eq 1 -or $_.Level -eq 2 -or $_.Level -eq 3)
		} |
		Select-Object -First 50 TimeCreated, ProviderName, LevelDisplayName, Id, Message |
		ForEach-Object {
			$msg = if ($_.Message) { $_.Message.Substring(0, [Math]::Min(500, $_.Message.Length)) } else { "No message" }
			[PSCustomObject]@{
				TimeCreated = $_.TimeCreated.ToString("yyyy-MM-ddTHH:mm:ss")
				Provider = $_.ProviderName
				Level = $_.LevelDisplayName
				EventId = $_.Id
				Message = $msg -replace '\r?\n', ' '
			}
		} | ConvertTo-Json -Compress
	`, startTimeStr)

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", psCmd)
	output, err := cmd.Output()
	if err != nil {
		return []LogEntry{}, nil
	}

	return c.parseEventLogOutput(string(output))
}

// collectActivePrograms retrieves information about running processes
func (c *Collector) collectActivePrograms(ctx context.Context, hoursBack int) ([]ProcessInfo, error) {
	cutoffTime := time.Now().Add(-time.Duration(hoursBack) * time.Hour)

	// PowerShell command to get process info with version details
	psCmd := `
		Get-Process |
		Where-Object { $_.MainWindowTitle -ne '' -or $_.Path } |
		Select-Object -Unique Name, Id, Path, @{N='MemoryMB';E={[math]::Round($_.WorkingSet64/1MB,2)}},
			@{N='CPU';E={$_.CPU}}, StartTime |
		ForEach-Object {
			$ver = ""
			$company = ""
			if ($_.Path) {
				try {
					$fi = [System.Diagnostics.FileVersionInfo]::GetVersionInfo($_.Path)
					$ver = $fi.FileVersion
					$company = $fi.CompanyName
				} catch {}
			}
			[PSCustomObject]@{
				Name = $_.Name
				PID = $_.Id
				Path = $_.Path
				Version = $ver
				Company = $company
				MemoryMB = $_.MemoryMB
				CPU = $_.CPU
				StartTime = if ($_.StartTime) { $_.StartTime.ToString("yyyy-MM-ddTHH:mm:ss") } else { "" }
			}
		} | ConvertTo-Json -Compress
	`

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", psCmd)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	processes := c.parseProcessOutput(string(output), cutoffTime)

	// Sort alphabetically by name
	sort.Slice(processes, func(i, j int) bool {
		return strings.ToLower(processes[i].Name) < strings.ToLower(processes[j].Name)
	})

	return processes, nil
}

func (c *Collector) parseProcessOutput(output string, cutoffTime time.Time) []ProcessInfo {
	output = strings.TrimSpace(output)
	if output == "" || output == "null" {
		return []ProcessInfo{}
	}

	var processes []ProcessInfo
	seen := make(map[string]bool)

	// Parse JSON-like output
	lines := strings.Split(output, "},{")
	for _, line := range lines {
		line = strings.Trim(line, "[]{}")
		proc := c.parseProcessLine(line)

		// Only include processes started within the time window
		if proc.StartTime != "" {
			startTime, err := time.Parse("2006-01-02T15:04:05", proc.StartTime)
			if err == nil && startTime.After(cutoffTime) {
				// Calculate session duration
				duration := time.Since(startTime)
				proc.SessionDuration = c.formatDuration(duration)
			}
		}

		// Deduplicate by name
		key := strings.ToLower(proc.Name)
		if !seen[key] && proc.Name != "" {
			seen[key] = true
			processes = append(processes, proc)
		}
	}

	return processes
}

func (c *Collector) parseProcessLine(line string) ProcessInfo {
	proc := ProcessInfo{}

	// Extract fields using simple parsing
	proc.Name = c.extractStringField(line, "Name")
	proc.Path = c.extractStringField(line, "Path")
	proc.Version = c.extractStringField(line, "Version")
	proc.Company = c.extractStringField(line, "Company")
	proc.StartTime = c.extractStringField(line, "StartTime")

	// Extract numeric fields
	if pidStr := c.extractStringField(line, "PID"); pidStr != "" {
		fmt.Sscanf(pidStr, "%d", &proc.PID)
	}
	if memStr := c.extractStringField(line, "MemoryMB"); memStr != "" {
		fmt.Sscanf(memStr, "%f", &proc.MemoryMB)
	}
	if cpuStr := c.extractStringField(line, "CPU"); cpuStr != "" {
		fmt.Sscanf(cpuStr, "%f", &proc.CPUPercent)
	}

	return proc
}

func (c *Collector) extractStringField(line, field string) string {
	start := strings.Index(line, `"`+field+`":`)
	if start == -1 {
		return ""
	}
	start += len(field) + 3

	if start >= len(line) {
		return ""
	}

	// Check for null
	if strings.HasPrefix(line[start:], "null") {
		return ""
	}

	// String value
	if line[start] == '"' {
		start++
		end := strings.Index(line[start:], `"`)
		if end != -1 {
			return line[start : start+end]
		}
	} else {
		// Number value
		end := strings.IndexAny(line[start:], ",}")
		if end != -1 {
			return strings.TrimSpace(line[start : start+end])
		}
	}

	return ""
}

func (c *Collector) formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
