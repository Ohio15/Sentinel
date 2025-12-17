// Package paths provides centralized path management for the Sentinel agent.
// All file paths used by the agent should be defined here to ensure consistency
// and prevent hardcoded path errors.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// File names (constants - never change these inline elsewhere)
const (
	ConfigFileName      = "config.json"
	AgentLogFileName    = "agent.log"
	AgentInfoFileName   = "agent-info.json"
	ProtectionDataFile  = "protection.dat"
	WatchdogConfigFile  = "watchdog-config.json"
	AgentExecutable     = "sentinel-agent.exe"
	WatchdogExecutable  = "sentinel-watchdog.exe"
)

// Directory names
const (
	SentinelDirName = "Sentinel"
	CertsDirName    = "certs"
	UpdateDirName   = "update"
)

// DataDir returns the platform-specific data directory for Sentinel.
// Windows: C:\ProgramData\Sentinel
// macOS: /Library/Application Support/Sentinel
// Linux: /etc/sentinel
var DataDir = func() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramData"), SentinelDirName)
	case "darwin":
		return filepath.Join("/Library/Application Support", SentinelDirName)
	default: // linux
		return filepath.Join("/etc", "sentinel")
	}
}

// InstallDir returns the platform-specific installation directory.
// Windows: C:\Program Files\Sentinel Agent
// macOS: /usr/local/bin (or /Applications/Sentinel.app)
// Linux: /usr/local/bin
var InstallDir = func() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramFiles"), "Sentinel Agent")
	case "darwin":
		return "/usr/local/bin"
	default: // linux
		return "/usr/local/bin"
	}
}

// ConfigPath returns the full path to the config file.
var ConfigPath = func() string {
	return filepath.Join(DataDir(), ConfigFileName)
}

// LogPath returns the full path to the agent log file.
var LogPath = func() string {
	return filepath.Join(DataDir(), AgentLogFileName)
}

// AgentInfoPath returns the full path to the agent info file.
var AgentInfoPath = func() string {
	return filepath.Join(DataDir(), AgentInfoFileName)
}

// ProtectionDataPath returns the full path to protection data.
var ProtectionDataPath = func() string {
	return filepath.Join(InstallDir(), ProtectionDataFile)
}

// CertsDir returns the path to the certificates directory.
var CertsDir = func() string {
	return filepath.Join(DataDir(), CertsDirName)
}

// UpdateDir returns the path to the update staging directory.
var UpdateDir = func() string {
	return filepath.Join(DataDir(), UpdateDirName)
}

// AgentPath returns the full path to the agent executable.
var AgentPath = func() string {
	return filepath.Join(InstallDir(), AgentExecutable)
}

// WatchdogPath returns the full path to the watchdog executable.
var WatchdogPath = func() string {
	return filepath.Join(InstallDir(), WatchdogExecutable)
}

// EnsureDataDir creates the data directory if it doesn't exist.
func EnsureDataDir() error {
	return os.MkdirAll(DataDir(), 0755)
}

// EnsureCertsDir creates the certificates directory if it doesn't exist.
func EnsureCertsDir() error {
	return os.MkdirAll(CertsDir(), 0755)
}

// EnsureUpdateDir creates the update directory if it doesn't exist.
func EnsureUpdateDir() error {
	return os.MkdirAll(UpdateDir(), 0755)
}

// ExecutableFilesInInstallDir returns the list of executable files
// that should exist in the installation directory.
func ExecutableFilesInInstallDir() []string {
	return []string{
		AgentPath(),
		WatchdogPath(),
	}
}

// Join is a convenience wrapper around filepath.Join for constructing paths.
func Join(elem ...string) string {
	return filepath.Join(elem...)
}
