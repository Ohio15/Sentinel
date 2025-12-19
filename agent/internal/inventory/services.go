package inventory

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Service represents a system service
type Service struct {
	Name             string    `json:"name"`
	DisplayName      string    `json:"displayName,omitempty"`
	Description      string    `json:"description,omitempty"`
	ServiceType      string    `json:"serviceType"` // windows_service, systemd, launchd, init
	StartType        string    `json:"startType"`   // automatic, manual, disabled, automatic_delayed
	CurrentState     string    `json:"currentState"` // running, stopped, paused, starting, stopping
	PathToExecutable string    `json:"pathToExecutable,omitempty"`
	Account          string    `json:"account,omitempty"`
	PID              int       `json:"pid,omitempty"`
	Dependencies     []string  `json:"dependencies,omitempty"`
	CollectedAt      time.Time `json:"collectedAt"`
}

// ServiceCollector collects system services information
type ServiceCollector struct {
	timeout time.Duration
}

// NewServiceCollector creates a new service collector
func NewServiceCollector() *ServiceCollector {
	return &ServiceCollector{
		timeout: 60 * time.Second,
	}
}

// Collect gathers all system services
func (c *ServiceCollector) Collect(ctx context.Context) ([]Service, error) {
	switch runtime.GOOS {
	case "windows":
		return c.collectWindows(ctx)
	case "darwin":
		return c.collectMacOS(ctx)
	case "linux":
		return c.collectLinux(ctx)
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

// collectWindows collects Windows services
func (c *ServiceCollector) collectWindows(ctx context.Context) ([]Service, error) {
	psScript := `
Get-Service | ForEach-Object {
    $wmi = Get-WmiObject Win32_Service -Filter "Name='$($_.Name)'" -ErrorAction SilentlyContinue
    [PSCustomObject]@{
        Name = $_.Name
        DisplayName = $_.DisplayName
        Status = $_.Status.ToString()
        StartType = $_.StartType.ToString()
        Description = $wmi.Description
        PathName = $wmi.PathName
        StartName = $wmi.StartName
        ProcessId = $wmi.ProcessId
        ServiceType = 'windows_service'
    }
} | ConvertTo-Json -Depth 2
`

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute PowerShell: %w", err)
	}

	return c.parseWindowsServices(string(output)), nil
}

// parseWindowsServices parses Windows service output
func (c *ServiceCollector) parseWindowsServices(output string) []Service {
	var services []Service
	now := time.Now()

	entries := parseJSONArray(strings.TrimSpace(output))
	for _, entry := range entries {
		svc := Service{
			Name:             entry["Name"],
			DisplayName:      entry["DisplayName"],
			Description:      entry["Description"],
			ServiceType:      "windows_service",
			PathToExecutable: entry["PathName"],
			Account:          entry["StartName"],
			CollectedAt:      now,
		}

		// Map status
		switch strings.ToLower(entry["Status"]) {
		case "running":
			svc.CurrentState = "running"
		case "stopped":
			svc.CurrentState = "stopped"
		case "paused":
			svc.CurrentState = "paused"
		case "startpending":
			svc.CurrentState = "starting"
		case "stoppending":
			svc.CurrentState = "stopping"
		default:
			svc.CurrentState = entry["Status"]
		}

		// Map start type
		switch strings.ToLower(entry["StartType"]) {
		case "automatic":
			svc.StartType = "automatic"
		case "manual":
			svc.StartType = "manual"
		case "disabled":
			svc.StartType = "disabled"
		case "boot", "system":
			svc.StartType = "automatic"
		default:
			svc.StartType = entry["StartType"]
		}

		// Parse PID
		if pidStr := entry["ProcessId"]; pidStr != "" {
			if pid, err := strconv.Atoi(pidStr); err == nil {
				svc.PID = pid
			}
		}

		if svc.Name != "" {
			services = append(services, svc)
		}
	}

	return services
}

// collectMacOS collects launchd services
func (c *ServiceCollector) collectMacOS(ctx context.Context) ([]Service, error) {
	var services []Service
	now := time.Now()

	// Get launchctl list
	cmd := exec.CommandContext(ctx, "launchctl", "list")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run launchctl: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		if i == 0 { // Skip header
			continue
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 3 {
			svc := Service{
				Name:        parts[2],
				ServiceType: "launchd",
				CollectedAt: now,
			}

			// Parse PID
			if parts[0] != "-" {
				if pid, err := strconv.Atoi(parts[0]); err == nil {
					svc.PID = pid
					svc.CurrentState = "running"
				}
			} else {
				svc.CurrentState = "stopped"
			}

			// Get additional info from plist
			info := c.getLaunchdServiceInfo(ctx, svc.Name)
			if info != nil {
				if info.Label != "" {
					svc.DisplayName = info.Label
				}
				if info.Program != "" {
					svc.PathToExecutable = info.Program
				} else if len(info.ProgramArguments) > 0 {
					svc.PathToExecutable = info.ProgramArguments[0]
				}
				if info.Disabled {
					svc.StartType = "disabled"
				} else if info.RunAtLoad {
					svc.StartType = "automatic"
				} else {
					svc.StartType = "manual"
				}
			}

			services = append(services, svc)
		}
	}

	return services, nil
}

// LaunchdInfo holds launchd plist info
type LaunchdInfo struct {
	Label            string
	Program          string
	ProgramArguments []string
	RunAtLoad        bool
	Disabled         bool
}

// getLaunchdServiceInfo gets additional info for a launchd service
func (c *ServiceCollector) getLaunchdServiceInfo(ctx context.Context, label string) *LaunchdInfo {
	// Try to find and read the plist
	paths := []string{
		"/System/Library/LaunchDaemons/" + label + ".plist",
		"/Library/LaunchDaemons/" + label + ".plist",
		"/System/Library/LaunchAgents/" + label + ".plist",
		"/Library/LaunchAgents/" + label + ".plist",
	}

	for _, path := range paths {
		cmd := exec.CommandContext(ctx, "defaults", "read", path)
		output, err := cmd.Output()
		if err == nil {
			info := &LaunchdInfo{Label: label}
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "Program = ") {
					info.Program = strings.Trim(strings.TrimPrefix(line, "Program = "), `";`)
				}
				if strings.Contains(line, "RunAtLoad = 1") || strings.Contains(line, "RunAtLoad = true") {
					info.RunAtLoad = true
				}
				if strings.Contains(line, "Disabled = 1") || strings.Contains(line, "Disabled = true") {
					info.Disabled = true
				}
			}
			return info
		}
	}

	return nil
}

// collectLinux collects systemd services
func (c *ServiceCollector) collectLinux(ctx context.Context) ([]Service, error) {
	var services []Service
	now := time.Now()

	// Check if systemd is available
	if _, err := exec.LookPath("systemctl"); err != nil {
		// Fall back to init.d
		return c.collectInitD(ctx)
	}

	// Get all units
	cmd := exec.CommandContext(ctx, "systemctl", "list-units", "--type=service", "--all", "--no-pager", "--no-legend")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run systemctl: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 4 {
			name := strings.TrimSuffix(parts[0], ".service")

			svc := Service{
				Name:        name,
				ServiceType: "systemd",
				CollectedAt: now,
			}

			// Parse status
			switch parts[3] {
			case "running":
				svc.CurrentState = "running"
			case "exited", "dead":
				svc.CurrentState = "stopped"
			case "failed":
				svc.CurrentState = "failed"
			default:
				svc.CurrentState = parts[3]
			}

			// Get detailed info
			info := c.getSystemdServiceInfo(ctx, parts[0])
			if info != nil {
				svc.DisplayName = info.Description
				svc.Description = info.Description
				svc.PathToExecutable = info.ExecStart
				svc.StartType = info.UnitFileState
				svc.PID = info.MainPID
				svc.Account = info.User
			}

			services = append(services, svc)
		}
	}

	return services, nil
}

// SystemdInfo holds systemd service info
type SystemdInfo struct {
	Description   string
	ExecStart     string
	UnitFileState string
	MainPID       int
	User          string
}

// getSystemdServiceInfo gets additional info for a systemd service
func (c *ServiceCollector) getSystemdServiceInfo(ctx context.Context, unit string) *SystemdInfo {
	cmd := exec.CommandContext(ctx, "systemctl", "show", unit,
		"--property=Description,ExecStart,UnitFileState,MainPID,User")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	info := &SystemdInfo{}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "Description":
			info.Description = value
		case "ExecStart":
			// Extract path from ExecStart
			if idx := strings.Index(value, "path="); idx != -1 {
				end := strings.Index(value[idx:], ";")
				if end != -1 {
					info.ExecStart = value[idx+5 : idx+end]
				}
			} else {
				info.ExecStart = value
			}
		case "UnitFileState":
			switch value {
			case "enabled":
				info.UnitFileState = "automatic"
			case "disabled":
				info.UnitFileState = "disabled"
			case "static":
				info.UnitFileState = "manual"
			case "masked":
				info.UnitFileState = "disabled"
			default:
				info.UnitFileState = value
			}
		case "MainPID":
			if pid, err := strconv.Atoi(value); err == nil {
				info.MainPID = pid
			}
		case "User":
			info.User = value
		}
	}

	return info
}

// collectInitD collects init.d services (fallback for non-systemd systems)
func (c *ServiceCollector) collectInitD(ctx context.Context) ([]Service, error) {
	var services []Service
	now := time.Now()

	// Check /etc/init.d
	cmd := exec.CommandContext(ctx, "ls", "/etc/init.d")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list init.d: %w", err)
	}

	serviceNames := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, name := range serviceNames {
		name = strings.TrimSpace(name)
		if name == "" || name == "README" || name == "functions" {
			continue
		}

		svc := Service{
			Name:        name,
			ServiceType: "init",
			CollectedAt: now,
		}

		// Check status
		statusCmd := exec.CommandContext(ctx, "/etc/init.d/"+name, "status")
		if err := statusCmd.Run(); err == nil {
			svc.CurrentState = "running"
		} else {
			svc.CurrentState = "stopped"
		}

		// Check if enabled (look in rc.d directories)
		rcDirs := []string{"/etc/rc2.d", "/etc/rc3.d", "/etc/rc5.d"}
		for _, rcDir := range rcDirs {
			checkCmd := exec.CommandContext(ctx, "ls", rcDir)
			rcOutput, _ := checkCmd.Output()
			if strings.Contains(string(rcOutput), "S") && strings.Contains(string(rcOutput), name) {
				svc.StartType = "automatic"
				break
			}
		}
		if svc.StartType == "" {
			svc.StartType = "manual"
		}

		services = append(services, svc)
	}

	return services, nil
}

// Helper function for JSON parsing (reuse from software.go logic)
func parseJSONArray(jsonStr string) []map[string]string {
	var result []map[string]string

	jsonStr = strings.TrimSpace(jsonStr)
	if jsonStr == "" || jsonStr == "null" {
		return result
	}

	isArray := strings.HasPrefix(jsonStr, "[")

	var objects []string
	if isArray {
		depth := 0
		start := 0
		for i, ch := range jsonStr {
			if ch == '{' {
				if depth == 0 {
					start = i
				}
				depth++
			} else if ch == '}' {
				depth--
				if depth == 0 {
					objects = append(objects, jsonStr[start:i+1])
				}
			}
		}
	} else if strings.HasPrefix(jsonStr, "{") {
		objects = []string{jsonStr}
	}

	for _, obj := range objects {
		entry := make(map[string]string)
		obj = strings.Trim(obj, "{}")

		pairs := strings.Split(obj, ",")
		for _, pair := range pairs {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}

			parts := strings.SplitN(pair, ":", 2)
			if len(parts) != 2 {
				continue
			}

			key := strings.Trim(strings.TrimSpace(parts[0]), `"`)
			value := strings.Trim(strings.TrimSpace(parts[1]), `"`)
			entry[key] = value
		}

		if len(entry) > 0 {
			result = append(result, entry)
		}
	}

	return result
}
