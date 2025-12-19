package inventory

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Software represents an installed application
type Software struct {
	Name            string    `json:"name"`
	Version         string    `json:"version,omitempty"`
	Publisher       string    `json:"publisher,omitempty"`
	InstallDate     string    `json:"installDate,omitempty"`
	InstallLocation string    `json:"installLocation,omitempty"`
	InstallSource   string    `json:"installSource"` // registry, msi, dpkg, rpm, brew, app_store
	SizeBytes       int64     `json:"sizeBytes,omitempty"`
	Architecture    string    `json:"architecture,omitempty"` // x86, x64, arm64
	UninstallString string    `json:"uninstallString,omitempty"`
	IsSystem        bool      `json:"isSystem"`
	IsHidden        bool      `json:"isHidden"`
	CollectedAt     time.Time `json:"collectedAt"`
}

// SoftwareCollector collects installed software information
type SoftwareCollector struct {
	timeout time.Duration
}

// NewSoftwareCollector creates a new software collector
func NewSoftwareCollector() *SoftwareCollector {
	return &SoftwareCollector{
		timeout: 60 * time.Second,
	}
}

// Collect gathers all installed software
func (c *SoftwareCollector) Collect(ctx context.Context) ([]Software, error) {
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

// collectWindows collects software from Windows registry
func (c *SoftwareCollector) collectWindows(ctx context.Context) ([]Software, error) {
	var allSoftware []Software

	// PowerShell script to get installed software
	psScript := `
$software = @()

# 64-bit applications
$regPaths = @(
    'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*',
    'HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*',
    'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*'
)

foreach ($path in $regPaths) {
    try {
        Get-ItemProperty $path -ErrorAction SilentlyContinue | Where-Object { $_.DisplayName } | ForEach-Object {
            $arch = if ($path -like '*WOW6432Node*') { 'x86' } else { 'x64' }
            $isSystem = $_.SystemComponent -eq 1

            $software += [PSCustomObject]@{
                Name = $_.DisplayName
                Version = $_.DisplayVersion
                Publisher = $_.Publisher
                InstallDate = $_.InstallDate
                InstallLocation = $_.InstallLocation
                Size = $_.EstimatedSize
                Architecture = $arch
                UninstallString = $_.UninstallString
                IsSystem = $isSystem
                Source = 'registry'
            }
        }
    } catch {}
}

$software | ConvertTo-Json -Depth 3
`

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute PowerShell: %w", err)
	}

	allSoftware = c.parseWindowsOutput(string(output))

	// Also get Windows Store apps
	storeApps, err := c.collectWindowsStoreApps(ctx)
	if err == nil {
		allSoftware = append(allSoftware, storeApps...)
	}

	return allSoftware, nil
}

// collectWindowsStoreApps collects Microsoft Store applications
func (c *SoftwareCollector) collectWindowsStoreApps(ctx context.Context) ([]Software, error) {
	psScript := `
Get-AppxPackage | Where-Object { $_.IsFramework -eq $false } | ForEach-Object {
    [PSCustomObject]@{
        Name = $_.Name
        Version = $_.Version
        Publisher = $_.Publisher
        InstallLocation = $_.InstallLocation
        Architecture = $_.Architecture
        IsSystem = $_.SignatureKind -eq 'System'
        Source = 'app_store'
    }
} | ConvertTo-Json -Depth 2
`

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return c.parseWindowsStoreOutput(string(output)), nil
}

// parseWindowsOutput parses the PowerShell JSON output
func (c *SoftwareCollector) parseWindowsOutput(output string) []Software {
	var software []Software
	now := time.Now()

	// Parse JSON output from PowerShell
	lines := strings.Split(output, "\n")
	var jsonStr strings.Builder
	for _, line := range lines {
		jsonStr.WriteString(line)
	}

	// Simple parsing for demonstration - in production use encoding/json
	// This handles the case where PowerShell returns a single object vs array
	trimmed := strings.TrimSpace(jsonStr.String())
	if trimmed == "" || trimmed == "null" {
		return software
	}

	// Parse individual software entries
	entries := c.parseJSONArray(trimmed)
	for _, entry := range entries {
		sw := Software{
			Name:            entry["Name"],
			Version:         entry["Version"],
			Publisher:       entry["Publisher"],
			InstallDate:     entry["InstallDate"],
			InstallLocation: entry["InstallLocation"],
			UninstallString: entry["UninstallString"],
			Architecture:    entry["Architecture"],
			InstallSource:   "registry",
			CollectedAt:     now,
		}

		if entry["IsSystem"] == "true" || entry["IsSystem"] == "True" {
			sw.IsSystem = true
		}

		// Convert size from KB to bytes
		if sizeStr := entry["Size"]; sizeStr != "" {
			var size int64
			fmt.Sscanf(sizeStr, "%d", &size)
			sw.SizeBytes = size * 1024
		}

		if sw.Name != "" {
			software = append(software, sw)
		}
	}

	return software
}

// parseWindowsStoreOutput parses Windows Store apps output
func (c *SoftwareCollector) parseWindowsStoreOutput(output string) []Software {
	var software []Software
	now := time.Now()

	entries := c.parseJSONArray(strings.TrimSpace(output))
	for _, entry := range entries {
		sw := Software{
			Name:            entry["Name"],
			Version:         entry["Version"],
			Publisher:       entry["Publisher"],
			InstallLocation: entry["InstallLocation"],
			Architecture:    entry["Architecture"],
			InstallSource:   "app_store",
			CollectedAt:     now,
		}

		if entry["IsSystem"] == "true" || entry["IsSystem"] == "True" {
			sw.IsSystem = true
		}

		if sw.Name != "" {
			software = append(software, sw)
		}
	}

	return software
}

// collectMacOS collects software from macOS
func (c *SoftwareCollector) collectMacOS(ctx context.Context) ([]Software, error) {
	var allSoftware []Software
	now := time.Now()

	// Get applications from /Applications
	cmd := exec.CommandContext(ctx, "system_profiler", "SPApplicationsDataType", "-json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run system_profiler: %w", err)
	}

	// Parse system_profiler output
	apps := c.parseMacOSSystemProfiler(string(output), now)
	allSoftware = append(allSoftware, apps...)

	// Also check Homebrew packages
	brewApps, err := c.collectHomebrewPackages(ctx)
	if err == nil {
		allSoftware = append(allSoftware, brewApps...)
	}

	return allSoftware, nil
}

// parseMacOSSystemProfiler parses macOS system_profiler output
func (c *SoftwareCollector) parseMacOSSystemProfiler(output string, timestamp time.Time) []Software {
	var software []Software

	// Simple line-by-line parsing for the JSON structure
	// In production, use encoding/json with proper struct
	lines := strings.Split(output, "\n")
	var currentApp Software

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.Contains(line, `"_name"`) {
			if currentApp.Name != "" {
				currentApp.CollectedAt = timestamp
				currentApp.InstallSource = "app_store"
				software = append(software, currentApp)
			}
			currentApp = Software{}
			currentApp.Name = extractJSONValue(line, "_name")
		} else if strings.Contains(line, `"version"`) {
			currentApp.Version = extractJSONValue(line, "version")
		} else if strings.Contains(line, `"obtained_from"`) {
			source := extractJSONValue(line, "obtained_from")
			if source == "apple" {
				currentApp.InstallSource = "app_store"
			} else if source == "identified_developer" {
				currentApp.InstallSource = "developer"
			} else {
				currentApp.InstallSource = source
			}
		} else if strings.Contains(line, `"path"`) {
			currentApp.InstallLocation = extractJSONValue(line, "path")
		} else if strings.Contains(line, `"signed_by"`) {
			currentApp.Publisher = extractJSONValue(line, "signed_by")
		}
	}

	// Don't forget the last app
	if currentApp.Name != "" {
		currentApp.CollectedAt = timestamp
		software = append(software, currentApp)
	}

	return software
}

// collectHomebrewPackages collects Homebrew packages on macOS
func (c *SoftwareCollector) collectHomebrewPackages(ctx context.Context) ([]Software, error) {
	var software []Software
	now := time.Now()

	// List installed formulae
	cmd := exec.CommandContext(ctx, "brew", "list", "--versions")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			sw := Software{
				Name:          parts[0],
				Version:       parts[len(parts)-1],
				InstallSource: "brew",
				Publisher:     "Homebrew",
				CollectedAt:   now,
			}
			software = append(software, sw)
		}
	}

	// List installed casks
	cmd = exec.CommandContext(ctx, "brew", "list", "--cask", "--versions")
	output, err = cmd.Output()
	if err == nil {
		lines = strings.Split(string(output), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			parts := strings.Fields(line)
			if len(parts) >= 2 {
				sw := Software{
					Name:          parts[0],
					Version:       parts[len(parts)-1],
					InstallSource: "brew_cask",
					Publisher:     "Homebrew Cask",
					CollectedAt:   now,
				}
				software = append(software, sw)
			}
		}
	}

	return software, nil
}

// collectLinux collects software from Linux package managers
func (c *SoftwareCollector) collectLinux(ctx context.Context) ([]Software, error) {
	var allSoftware []Software

	// Try dpkg (Debian/Ubuntu)
	if dpkgApps, err := c.collectDpkg(ctx); err == nil {
		allSoftware = append(allSoftware, dpkgApps...)
	}

	// Try rpm (RHEL/CentOS/Fedora)
	if rpmApps, err := c.collectRpm(ctx); err == nil {
		allSoftware = append(allSoftware, rpmApps...)
	}

	// Try pacman (Arch Linux)
	if pacmanApps, err := c.collectPacman(ctx); err == nil {
		allSoftware = append(allSoftware, pacmanApps...)
	}

	// Try snap
	if snapApps, err := c.collectSnap(ctx); err == nil {
		allSoftware = append(allSoftware, snapApps...)
	}

	// Try flatpak
	if flatpakApps, err := c.collectFlatpak(ctx); err == nil {
		allSoftware = append(allSoftware, flatpakApps...)
	}

	return allSoftware, nil
}

// collectDpkg collects packages from dpkg
func (c *SoftwareCollector) collectDpkg(ctx context.Context) ([]Software, error) {
	var software []Software
	now := time.Now()

	cmd := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${Package}\t${Version}\t${Installed-Size}\t${Status}\n")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) >= 4 && strings.Contains(parts[3], "installed") {
			sw := Software{
				Name:          parts[0],
				Version:       parts[1],
				InstallSource: "dpkg",
				CollectedAt:   now,
			}

			// Size is in KB
			var size int64
			fmt.Sscanf(parts[2], "%d", &size)
			sw.SizeBytes = size * 1024

			software = append(software, sw)
		}
	}

	return software, nil
}

// collectRpm collects packages from rpm
func (c *SoftwareCollector) collectRpm(ctx context.Context) ([]Software, error) {
	var software []Software
	now := time.Now()

	cmd := exec.CommandContext(ctx, "rpm", "-qa", "--queryformat", "%{NAME}\t%{VERSION}\t%{SIZE}\t%{VENDOR}\n")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) >= 4 {
			sw := Software{
				Name:          parts[0],
				Version:       parts[1],
				Publisher:     parts[3],
				InstallSource: "rpm",
				CollectedAt:   now,
			}

			var size int64
			fmt.Sscanf(parts[2], "%d", &size)
			sw.SizeBytes = size

			software = append(software, sw)
		}
	}

	return software, nil
}

// collectPacman collects packages from pacman (Arch Linux)
func (c *SoftwareCollector) collectPacman(ctx context.Context) ([]Software, error) {
	var software []Software
	now := time.Now()

	cmd := exec.CommandContext(ctx, "pacman", "-Q")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			sw := Software{
				Name:          parts[0],
				Version:       parts[1],
				InstallSource: "pacman",
				CollectedAt:   now,
			}
			software = append(software, sw)
		}
	}

	return software, nil
}

// collectSnap collects snap packages
func (c *SoftwareCollector) collectSnap(ctx context.Context) ([]Software, error) {
	var software []Software
	now := time.Now()

	cmd := exec.CommandContext(ctx, "snap", "list")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
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
		if len(parts) >= 2 {
			sw := Software{
				Name:          parts[0],
				Version:       parts[1],
				InstallSource: "snap",
				CollectedAt:   now,
			}

			if len(parts) >= 4 {
				sw.Publisher = parts[3]
			}

			software = append(software, sw)
		}
	}

	return software, nil
}

// collectFlatpak collects flatpak applications
func (c *SoftwareCollector) collectFlatpak(ctx context.Context) ([]Software, error) {
	var software []Software
	now := time.Now()

	cmd := exec.CommandContext(ctx, "flatpak", "list", "--columns=application,version,origin")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 1 {
			sw := Software{
				Name:          parts[0],
				InstallSource: "flatpak",
				CollectedAt:   now,
			}

			if len(parts) >= 2 {
				sw.Version = parts[1]
			}
			if len(parts) >= 3 {
				sw.Publisher = parts[2]
			}

			software = append(software, sw)
		}
	}

	return software, nil
}

// Helper functions

func extractJSONValue(line, key string) string {
	// Simple extraction for key: "value" pattern
	keyPattern := fmt.Sprintf(`"%s"`, key)
	if !strings.Contains(line, keyPattern) {
		return ""
	}

	parts := strings.SplitN(line, ":", 2)
	if len(parts) < 2 {
		return ""
	}

	value := strings.TrimSpace(parts[1])
	value = strings.Trim(value, `",`)
	return value
}

func (c *SoftwareCollector) parseJSONArray(jsonStr string) []map[string]string {
	var result []map[string]string

	// Handle single object vs array
	jsonStr = strings.TrimSpace(jsonStr)
	if jsonStr == "" || jsonStr == "null" {
		return result
	}

	// Simple parser for flat JSON objects
	// In production, use encoding/json properly
	isArray := strings.HasPrefix(jsonStr, "[")

	var objects []string
	if isArray {
		// Find individual objects in array
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

		// Remove braces
		obj = strings.Trim(obj, "{}")

		// Split by comma (simple approach)
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
