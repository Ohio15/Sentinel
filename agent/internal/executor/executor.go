package executor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// CommandResult contains the result of a command execution
type CommandResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Duration int64  `json:"duration_ms"`
}

// Executor handles command and script execution
type Executor struct {
	maxTimeout time.Duration
}

// New creates a new command executor
func New() *Executor {
	return &Executor{
		maxTimeout: 30 * time.Minute,
	}
}

// Execute runs a shell command and returns the result
func (e *Executor) Execute(ctx context.Context, command string, cmdType string) (*CommandResult, error) {
	start := time.Now()

	var cmd *exec.Cmd

	switch cmdType {
	case "powershell":
		if runtime.GOOS != "windows" {
			return nil, fmt.Errorf("PowerShell is only available on Windows")
		}
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	case "cmd":
		if runtime.GOOS != "windows" {
			return nil, fmt.Errorf("CMD is only available on Windows")
		}
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	case "bash":
		shell := "/bin/bash"
		if _, err := os.Stat(shell); os.IsNotExist(err) {
			shell = "/bin/sh"
		}
		cmd = exec.CommandContext(ctx, shell, "-c", command)
	case "sh":
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command)
	default:
		// Auto-detect based on OS
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd", "/C", command)
		} else {
			cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command)
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set environment
	cmd.Env = os.Environ()

	err := cmd.Run()
	duration := time.Since(start).Milliseconds()

	result := &CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			result.Stderr = fmt.Sprintf("%s\n%s", result.Stderr, err.Error())
		}
	}

	return result, nil
}

// ExecuteScript runs a script with the specified language
func (e *Executor) ExecuteScript(ctx context.Context, script string, language string) (*CommandResult, error) {
	start := time.Now()

	// Create temp file for script
	tmpDir := os.TempDir()
	var filename string
	var cmd *exec.Cmd

	switch strings.ToLower(language) {
	case "powershell", "ps1":
		if runtime.GOOS != "windows" {
			return nil, fmt.Errorf("PowerShell is only available on Windows")
		}
		filename = filepath.Join(tmpDir, fmt.Sprintf("sentinel_script_%d.ps1", time.Now().UnixNano()))
		if err := os.WriteFile(filename, []byte(script), 0600); err != nil {
			return nil, fmt.Errorf("failed to write script file: %w", err)
		}
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filename)

	case "batch", "bat", "cmd":
		if runtime.GOOS != "windows" {
			return nil, fmt.Errorf("Batch scripts are only available on Windows")
		}
		filename = filepath.Join(tmpDir, fmt.Sprintf("sentinel_script_%d.bat", time.Now().UnixNano()))
		if err := os.WriteFile(filename, []byte(script), 0600); err != nil {
			return nil, fmt.Errorf("failed to write script file: %w", err)
		}
		cmd = exec.CommandContext(ctx, "cmd", "/C", filename)

	case "bash":
		filename = filepath.Join(tmpDir, fmt.Sprintf("sentinel_script_%d.sh", time.Now().UnixNano()))
		if err := os.WriteFile(filename, []byte(script), 0700); err != nil {
			return nil, fmt.Errorf("failed to write script file: %w", err)
		}
		shell := "/bin/bash"
		if _, err := os.Stat(shell); os.IsNotExist(err) {
			shell = "/bin/sh"
		}
		cmd = exec.CommandContext(ctx, shell, filename)

	case "python", "python3", "py":
		filename = filepath.Join(tmpDir, fmt.Sprintf("sentinel_script_%d.py", time.Now().UnixNano()))
		if err := os.WriteFile(filename, []byte(script), 0600); err != nil {
			return nil, fmt.Errorf("failed to write script file: %w", err)
		}
		pythonCmd := "python3"
		if runtime.GOOS == "windows" {
			pythonCmd = "python"
		}
		cmd = exec.CommandContext(ctx, pythonCmd, filename)

	default:
		return nil, fmt.Errorf("unsupported script language: %s", language)
	}

	// Clean up temp file after execution
	defer os.Remove(filename)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = os.Environ()

	err := cmd.Run()
	duration := time.Since(start).Milliseconds()

	result := &CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			result.Stderr = fmt.Sprintf("%s\n%s", result.Stderr, err.Error())
		}
	}

	return result, nil
}

// KillProcess terminates a process by PID
func (e *Executor) KillProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process not found: %w", err)
	}

	if runtime.GOOS == "windows" {
		// On Windows, use taskkill for more reliable termination
		cmd := exec.Command("taskkill", "/F", "/PID", fmt.Sprintf("%d", pid))
		return cmd.Run()
	}

	return proc.Kill()
}

// GetSystemShell returns the default system shell
func GetSystemShell() string {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("powershell"); err == nil {
			return "powershell"
		}
		return "cmd"
	}

	shell := os.Getenv("SHELL")
	if shell != "" {
		return shell
	}

	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}
