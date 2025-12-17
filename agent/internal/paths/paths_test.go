package paths

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDataDirNotEmpty(t *testing.T) {
	dir := DataDir()
	if dir == "" {
		t.Error("DataDir() returned empty string")
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("DataDir() should return absolute path, got: %s", dir)
	}
}

func TestInstallDirNotEmpty(t *testing.T) {
	dir := InstallDir()
	if dir == "" {
		t.Error("InstallDir() returned empty string")
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("InstallDir() should return absolute path, got: %s", dir)
	}
}

func TestConfigPathContainsConfigFileName(t *testing.T) {
	path := ConfigPath()
	if !strings.HasSuffix(path, ConfigFileName) {
		t.Errorf("ConfigPath() should end with %s, got: %s", ConfigFileName, path)
	}
}

func TestLogPathContainsLogFileName(t *testing.T) {
	path := LogPath()
	if !strings.HasSuffix(path, AgentLogFileName) {
		t.Errorf("LogPath() should end with %s, got: %s", AgentLogFileName, path)
	}
}

func TestPathsAreInCorrectDirectories(t *testing.T) {
	dataDir := DataDir()
	installDir := InstallDir()

	// Config should be in data dir, not install dir
	configPath := ConfigPath()
	if !strings.HasPrefix(configPath, dataDir) {
		t.Errorf("ConfigPath should be in DataDir. ConfigPath: %s, DataDir: %s", configPath, dataDir)
	}
	if strings.HasPrefix(configPath, installDir) && installDir != dataDir {
		t.Error("ConfigPath should NOT be in InstallDir")
	}

	// Agent executable should be in install dir
	agentPath := AgentPath()
	if !strings.HasPrefix(agentPath, installDir) {
		t.Errorf("AgentPath should be in InstallDir. AgentPath: %s, InstallDir: %s", agentPath, installDir)
	}
}

func TestWindowsPathsUseCorrectDrive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows-specific test")
	}

	dataDir := DataDir()
	if !strings.Contains(dataDir, "ProgramData") {
		t.Errorf("Windows DataDir should contain ProgramData, got: %s", dataDir)
	}

	installDir := InstallDir()
	if !strings.Contains(installDir, "Program Files") {
		t.Errorf("Windows InstallDir should contain Program Files, got: %s", installDir)
	}
}

func TestExecutableFilesListNotEmpty(t *testing.T) {
	files := ExecutableFilesInInstallDir()
	if len(files) == 0 {
		t.Error("ExecutableFilesInInstallDir() returned empty list")
	}
	for _, f := range files {
		if f == "" {
			t.Error("ExecutableFilesInInstallDir() contains empty path")
		}
	}
}

func TestConstantsNotEmpty(t *testing.T) {
	constants := map[string]string{
		"ConfigFileName":     ConfigFileName,
		"AgentLogFileName":   AgentLogFileName,
		"AgentInfoFileName":  AgentInfoFileName,
		"ProtectionDataFile": ProtectionDataFile,
		"AgentExecutable":    AgentExecutable,
		"WatchdogExecutable": WatchdogExecutable,
		"SentinelDirName":    SentinelDirName,
	}

	for name, value := range constants {
		if value == "" {
			t.Errorf("Constant %s is empty", name)
		}
	}
}
