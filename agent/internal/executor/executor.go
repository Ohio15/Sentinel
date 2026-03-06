package executor

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const maxOutputSize = 1024 * 1024 // 1MB per stream

// limitedBuffer captures output up to a maximum size, discarding excess data.
type limitedBuffer struct {
	buf       []byte
	max       int
	truncated bool
}

func (lb *limitedBuffer) Write(p []byte) (n int, err error) {
	remaining := lb.max - len(lb.buf)
	if remaining <= 0 {
		lb.truncated = true
		return len(p), nil // Accept but discard
	}
	if len(p) > remaining {
		lb.buf = append(lb.buf, p[:remaining]...)
		lb.truncated = true
		return len(p), nil
	}
	lb.buf = append(lb.buf, p...)
	return len(p), nil
}

func (lb *limitedBuffer) String() string {
	s := string(lb.buf)
	if lb.truncated {
		s += "\n... [output truncated at 1MB]"
	}
	return s
}

// CommandResult contains the result of a command execution
type CommandResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Duration int64  `json:"duration_ms"`
}

// Executor handles command and script execution
type Executor struct {
	maxTimeout     time.Duration
	concurrencySem chan struct{} // CW-002: Limits concurrent command execution
}


// New creates a new command executor
func New() *Executor {
	return &Executor{
		maxTimeout:     30 * time.Minute,
		concurrencySem: make(chan struct{}, 10), // Max 10 concurrent commands
	}
}

// Execute runs a shell command and returns the result
func (e *Executor) Execute(ctx context.Context, command string, cmdType string) (*CommandResult, error) {
	// CW-002: Rate limit check
	if err := CheckRateLimit(); err != nil {
		log.Printf("[SECURITY] Rate limit exceeded for command execution")
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	// CW-002: Acquire concurrency semaphore
	select {
	case e.concurrencySem <- struct{}{}:
		defer func() { <-e.concurrencySem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, fmt.Errorf("too many concurrent commands executing")
	}

	// Validate command before execution
	if err := ValidateCommand(command, cmdType); err != nil {
		log.Printf("[SECURITY] Command validation failed: %v | Command: %s | Type: %s", err, command, cmdType)
		return nil, fmt.Errorf("command validation failed: %w", err)
	}

	// CW-002: Apply execution timeout
	execTimeout := e.maxTimeout
	if execTimeout == 0 {
		execTimeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	log.Printf("[EXECUTOR] Executing command: %s | Type: %s", command, cmdType)
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

	stdout := &limitedBuffer{max: maxOutputSize}
	stderr := &limitedBuffer{max: maxOutputSize}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

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
		if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = -2
			result.Stderr = fmt.Sprintf("%s\ncommand execution timed out after %v", result.Stderr, execTimeout)
		} else if exitErr, ok := err.(*exec.ExitError); ok {
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
	// CW-002: Rate limit check
	if err := CheckRateLimit(); err != nil {
		log.Printf("[SECURITY] Rate limit exceeded for script execution")
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	// CW-002: Acquire concurrency semaphore
	select {
	case e.concurrencySem <- struct{}{}:
		defer func() { <-e.concurrencySem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, fmt.Errorf("too many concurrent commands executing")
	}
	// Validate script before execution
	if err := ValidateScript(script, language); err != nil {
		log.Printf("[SECURITY] Script validation failed: %v | Language: %s | Script length: %d", err, language, len(script))
		return nil, fmt.Errorf("script validation failed: %w", err)
	}

	// CW-002: Apply execution timeout
	execTimeout := e.maxTimeout
	if execTimeout == 0 {
		execTimeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	log.Printf("[EXECUTOR] Executing script | Language: %s | Script length: %d bytes", language, len(script))
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

	stdout := &limitedBuffer{max: maxOutputSize}
	stderr := &limitedBuffer{max: maxOutputSize}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = os.Environ()

	err := cmd.Run()
	duration := time.Since(start).Milliseconds()

	result := &CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = -2
			result.Stderr = fmt.Sprintf("%s\nscript execution timed out after %v", result.Stderr, execTimeout)
		} else if exitErr, ok := err.(*exec.ExitError); ok {
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