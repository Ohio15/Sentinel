package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/sentinel/agent/internal/ipc"
	"github.com/sentinel/agent/internal/protection"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	serviceName        = "SentinelWatchdog"
	serviceDisplayName = "Sentinel Watchdog Service"
	serviceDescription = "Monitors and maintains Sentinel Agent availability"
	agentServiceName   = "SentinelAgent"
	checkInterval      = 10 * time.Second
	maxRestartAttempts = 5
	restartCooldown    = 60 * time.Second

	// Update orchestration constants
	updateCheckInterval    = 5 * time.Second  // How often to check for pending updates
	updateVerifyTimeout    = 30 * time.Second // How long to wait for agent to report version
	updateVerifyInterval   = 2 * time.Second  // How often to check agent version during verification

	// Health check constants for auto-rollback
	healthCheckInterval   = 10 * time.Second // How often to check agent health after update
	healthCheckDuration   = 60 * time.Second // How long to monitor health after update
	maxAgentMemoryMB      = 500              // Maximum memory usage before triggering rollback
	maxCrashesPerMinute   = 2                // Maximum crashes before triggering rollback
	healthStatusTimeout   = 15 * time.Second // Time to wait for agent to write health status
)

var (
	Version = "1.66.1"
	elog    debug.Log
	isDebug = false
)

// WatchdogConfig holds watchdog configuration
type WatchdogConfig struct {
	AgentPath       string `json:"agentPath"`
	AgentService    string `json:"agentService"`
	CheckInterval   int    `json:"checkIntervalSeconds"`
	MaxRestarts     int    `json:"maxRestarts"`
	ServerURL       string `json:"serverUrl"`
	ReportEndpoint  string `json:"reportEndpoint"`
}

// watchdogService implements svc.Handler
type watchdogService struct {
	config         *WatchdogConfig
	restartCount   int
	lastRestart    time.Time
	mu             sync.Mutex
	stopChan       chan struct{}
	installPath    string
	updateInProgress bool
	pipeServer     *ipc.PipeServer
}

func main() {
	// Determine install path
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}
	installPath := filepath.Dir(exePath)

	// Check command line arguments
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			installService(installPath)
			return
		case "uninstall":
			uninstallService()
			return
		case "start":
			startService()
			return
		case "stop":
			stopService()
			return
		case "debug":
			isDebug = true
			runDebug(installPath)
			return
		case "version":
			fmt.Printf("Sentinel Watchdog v%s\n", Version)
			return
		case "help":
			printUsage()
			return
		}
	}

	// Check if running as Windows service
	isService, err := svc.IsWindowsService()
	if err != nil {
		log.Fatalf("Failed to determine if running as service: %v", err)
	}

	if isService {
		runService(installPath)
	} else {
		// Running interactively
		fmt.Println("Sentinel Watchdog")
		fmt.Println("Use 'sentinel-watchdog install' to install as a service")
		fmt.Println("Use 'sentinel-watchdog debug' to run in debug mode")
	}
}

func printUsage() {
	fmt.Println("Sentinel Watchdog Service")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  sentinel-watchdog install   - Install as Windows service")
	fmt.Println("  sentinel-watchdog uninstall - Remove Windows service")
	fmt.Println("  sentinel-watchdog start     - Start the service")
	fmt.Println("  sentinel-watchdog stop      - Stop the service")
	fmt.Println("  sentinel-watchdog debug     - Run in debug mode (console)")
	fmt.Println("  sentinel-watchdog version   - Show version")
}

func loadConfig(installPath string) *WatchdogConfig {
	configPath := filepath.Join(installPath, "watchdog-config.json")

	config := &WatchdogConfig{
		AgentPath:     filepath.Join(installPath, "sentinel-agent.exe"),
		AgentService:  agentServiceName,
		CheckInterval: 10,
		MaxRestarts:   5,
	}

	data, err := os.ReadFile(configPath)
	if err == nil {
		json.Unmarshal(data, config)
	}

	return config
}

func runService(installPath string) {
	var err error
	elog, err = eventlog.Open(serviceName)
	if err != nil {
		log.Fatalf("Failed to open event log: %v", err)
	}
	defer elog.Close()

	elog.Info(1, fmt.Sprintf("Starting %s v%s", serviceName, Version))

	ws := &watchdogService{
		config:      loadConfig(installPath),
		stopChan:    make(chan struct{}),
		installPath: installPath,
	}

	err = svc.Run(serviceName, ws)
	if err != nil {
		elog.Error(1, fmt.Sprintf("Service failed: %v", err))
	}
}

func runDebug(installPath string) {
	elog = debug.New(serviceName)
	defer elog.Close()

	log.Printf("Starting %s v%s in debug mode", serviceName, Version)

	ws := &watchdogService{
		config:      loadConfig(installPath),
		stopChan:    make(chan struct{}),
		installPath: installPath,
	}

	// Start pipe server for update coordination
	go ws.startPipeServer()

	// Check for any pending updates from before restart
	go ws.checkForPendingUpdate()

	// Run agent monitor in foreground
	go ws.monitorAgent()

	// Wait for interrupt
	fmt.Println("Press Ctrl+C to stop...")
	select {}
}

// Execute implements svc.Handler
func (ws *watchdogService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}

	// Start pipe server for update coordination
	go ws.startPipeServer()

	// Check for any pending updates from before restart
	go ws.checkForPendingUpdate()

	// Start the monitoring goroutine
	go ws.monitorAgent()

	// Start update checker goroutine
	go ws.updateChecker()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				elog.Info(1, "Received stop signal")
				if ws.pipeServer != nil {
					ws.pipeServer.Close()
				}
				close(ws.stopChan)
				changes <- svc.Status{State: svc.StopPending}
				return
			default:
				elog.Warning(1, fmt.Sprintf("Unexpected control request: %d", c.Cmd))
			}
		}
	}
}

// monitorAgent continuously monitors the agent service
func (ws *watchdogService) monitorAgent() {
	// Immediately check and start agent on watchdog startup
	ws.checkAndRestartAgent()

	ticker := time.NewTicker(time.Duration(ws.config.CheckInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ws.stopChan:
			return
		case <-ticker.C:
			ws.checkAndRestartAgent()
		}
	}
}

// checkAndRestartAgent checks if the agent is running and restarts it if needed
func (ws *watchdogService) checkAndRestartAgent() {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	// Check cooldown
	if ws.restartCount >= ws.config.MaxRestarts {
		if time.Since(ws.lastRestart) < restartCooldown {
			// Too many restarts, wait for cooldown
			return
		}
		// Reset counter after cooldown
		ws.restartCount = 0
	}

	// Check if agent service is running
	running, err := isServiceRunning(ws.config.AgentService)
	if err != nil {
		logMessage(fmt.Sprintf("Error checking agent service: %v", err))
		return
	}

	if running {
		// Also verify the process is actually responding
		if ws.isAgentResponding() {
			return // All good
		}
		logMessage("Agent service running but not responding, restarting...")
	} else {
		logMessage("Agent service not running, attempting restart...")
	}

	// Attempt to restart
	if err := ws.restartAgent(); err != nil {
		logMessage(fmt.Sprintf("Failed to restart agent: %v", err))
		ws.restartCount++
		ws.lastRestart = time.Now()
	} else {
		logMessage("Agent service restarted successfully")
		ws.restartCount = 0
	}
}

// isServiceRunning checks if a Windows service is running
func isServiceRunning(serviceName string) (bool, error) {
	m, err := mgr.Connect()
	if err != nil {
		return false, err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return false, nil // Service doesn't exist
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return false, err
	}

	return status.State == svc.Running, nil
}

// restartAgent attempts to restart the agent service
func (ws *watchdogService) restartAgent() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(ws.config.AgentService)
	if err != nil {
		// Service doesn't exist, try to reinstall it
		return ws.reinstallAgent()
	}
	defer s.Close()

	// Stop if running
	status, _ := s.Query()
	if status.State != svc.Stopped {
		s.Control(svc.Stop)
		// Wait for stop
		for i := 0; i < 10; i++ {
			time.Sleep(time.Second)
			status, _ = s.Query()
			if status.State == svc.Stopped {
				break
			}
		}
	}

	// Start the service
	return s.Start()
}

// reinstallAgent reinstalls the agent service if it was removed
func (ws *watchdogService) reinstallAgent() error {
	logMessage("Attempting to reinstall agent service...")

	// Check if agent executable exists
	if _, err := os.Stat(ws.config.AgentPath); os.IsNotExist(err) {
		return fmt.Errorf("agent executable not found: %s", ws.config.AgentPath)
	}

	// Run agent install command (uses --install flag)
	cmd := exec.Command(ws.config.AgentPath, "--install")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("install failed: %v - %s", err, string(output))
	}

	// Start the service via SCM (the --install command also starts it, but ensure it's running)
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ws.config.AgentService)
	if err != nil {
		return fmt.Errorf("failed to open service after install: %v", err)
	}
	defer s.Close()

	return s.Start()
}

// isAgentResponding checks if the agent process is actually working
func (ws *watchdogService) isAgentResponding() bool {
	// Check if the agent's PID file exists and process is alive
	// For now, just check service state - can be enhanced later
	return true
}

func logMessage(msg string) {
	if elog != nil {
		elog.Info(1, msg)
	}
	if isDebug {
		log.Println(msg)
	}
}

// Service installation functions

func installService(installPath string) {
	exePath := filepath.Join(installPath, "sentinel-watchdog.exe")

	m, err := mgr.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to service manager: %v", err)
	}
	defer m.Disconnect()

	// Check if service already exists
	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		log.Println("Service already installed")
		return
	}

	// Create the service
	config := mgr.Config{
		DisplayName:      serviceDisplayName,
		Description:      serviceDescription,
		StartType:        mgr.StartAutomatic,
		ServiceStartName: "LocalSystem",
		// No dependencies - watchdog starts independently to monitor agent
	}

	s, err = m.CreateService(serviceName, exePath, config)
	if err != nil {
		log.Fatalf("Failed to create service: %v", err)
	}
	defer s.Close()

	// Set recovery actions
	recoveryActions := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}
	s.SetRecoveryActions(recoveryActions, 86400)

	// Create default config
	config_data := WatchdogConfig{
		AgentPath:     filepath.Join(installPath, "sentinel-agent.exe"),
		AgentService:  agentServiceName,
		CheckInterval: 10,
		MaxRestarts:   5,
	}
	configBytes, _ := json.MarshalIndent(config_data, "", "  ")
	os.WriteFile(filepath.Join(installPath, "watchdog-config.json"), configBytes, 0644)

	log.Println("Service installed successfully")
	log.Println("Use 'sentinel-watchdog start' to start the service")
}

func uninstallService() {
	m, err := mgr.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to service manager: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		log.Println("Service not installed")
		return
	}
	defer s.Close()

	// Stop the service first
	s.Control(svc.Stop)
	time.Sleep(2 * time.Second)

	// Delete the service
	err = s.Delete()
	if err != nil {
		log.Fatalf("Failed to delete service: %v", err)
	}

	log.Println("Service uninstalled successfully")
}

func startService() {
	m, err := mgr.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to service manager: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		log.Fatalf("Service not installed: %v", err)
	}
	defer s.Close()

	err = s.Start()
	if err != nil {
		log.Fatalf("Failed to start service: %v", err)
	}

	log.Println("Service started")
}

func stopService() {
	m, err := mgr.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to service manager: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		log.Fatalf("Service not installed: %v", err)
	}
	defer s.Close()

	_, err = s.Control(svc.Stop)
	if err != nil {
		log.Fatalf("Failed to stop service: %v", err)
	}

	log.Println("Service stopped")
}

func init() {
	// Ensure we're on Windows
	if runtime.GOOS != "windows" {
		log.Fatal("Watchdog service is only supported on Windows")
	}
}

// ============================================================================
// Update Orchestration Functions
// ============================================================================

// startPipeServer creates and runs the named pipe server for update coordination
func (ws *watchdogService) startPipeServer() {
	handler := func(msg ipc.PipeMessage) *ipc.PipeMessage {
		switch msg.Type {
		case ipc.MsgUpdateReady:
			logMessage("Received update ready signal via pipe")
			// The update checker will pick up the request file
			return nil

		case ipc.MsgVersionQuery:
			return &ipc.PipeMessage{
				Type:    ipc.MsgVersionResp,
				Payload: Version,
			}

		case ipc.MsgShutdown:
			logMessage("Received shutdown signal via pipe")
			return nil

		default:
			logMessage(fmt.Sprintf("Unknown pipe message type: %s", msg.Type))
			return nil
		}
	}

	var err error
	ws.pipeServer, err = ipc.NewPipeServer(handler)
	if err != nil {
		logMessage(fmt.Sprintf("Failed to create pipe server: %v", err))
		return
	}

	logMessage("Pipe server started")

	// Accept connections in a loop
	for {
		select {
		case <-ws.stopChan:
			return
		default:
			if err := ws.pipeServer.Accept(); err != nil {
				// Check if we're shutting down
				select {
				case <-ws.stopChan:
					return
				default:
					logMessage(fmt.Sprintf("Pipe accept error: %v", err))
				}
			}
		}
	}
}

// checkForPendingUpdate checks for any pending updates from before a restart
func (ws *watchdogService) checkForPendingUpdate() {
	// Give the system a moment to stabilize after startup
	time.Sleep(5 * time.Second)

	request, err := ipc.ReadUpdateRequest()
	if err != nil {
		logMessage(fmt.Sprintf("Error reading update request: %v", err))
		return
	}

	if request == nil {
		return // No pending update
	}

	logMessage(fmt.Sprintf("Found pending update request for version %s", request.Version))

	// Check if this update was already applied (agent is running new version)
	info, _ := ipc.ReadAgentInfo()
	if info != nil && info.Version == request.Version {
		logMessage(fmt.Sprintf("Update already applied: agent running version %s", info.Version))
		// Clean up the request file
		ipc.DeleteUpdateRequest()
		// Write success status if not already written
		status, _ := ipc.ReadUpdateStatus()
		if status == nil || status.State != ipc.StateComplete {
			ipc.WriteUpdateStatus(&ipc.UpdateStatus{
				State:       ipc.StateComplete,
				Version:     request.Version,
				CompletedAt: time.Now(),
			})
		}
		return
	}

	// There's a pending update that wasn't completed - attempt to apply it
	ws.mu.Lock()
	if ws.updateInProgress {
		ws.mu.Unlock()
		return
	}
	ws.updateInProgress = true
	ws.mu.Unlock()

	go ws.applyUpdate(request)
}

// updateChecker periodically checks for pending update requests
func (ws *watchdogService) updateChecker() {
	ticker := time.NewTicker(updateCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ws.stopChan:
			return
		case <-ticker.C:
			ws.mu.Lock()
			inProgress := ws.updateInProgress
			ws.mu.Unlock()

			if inProgress {
				continue
			}

			request, err := ipc.ReadUpdateRequest()
			if err != nil {
				logMessage(fmt.Sprintf("Error checking for updates: %v", err))
				continue
			}

			if request == nil {
				continue
			}

			logMessage(fmt.Sprintf("Update request found for version %s", request.Version))

			ws.mu.Lock()
			ws.updateInProgress = true
			ws.mu.Unlock()

			go ws.applyUpdate(request)
		}
	}
}

// applyUpdate performs the actual update operation
func (ws *watchdogService) applyUpdate(request *ipc.UpdateRequest) {
	defer func() {
		ws.mu.Lock()
		ws.updateInProgress = false
		ws.mu.Unlock()
	}()

	logMessage(fmt.Sprintf("Starting update to version %s", request.Version))

	// Write status: applying
	status := &ipc.UpdateStatus{
		State:     ipc.StateApplying,
		Version:   request.Version,
		StartedAt: time.Now(),
	}
	if err := ipc.WriteUpdateStatus(status); err != nil {
		logMessage(fmt.Sprintf("Failed to write update status: %v", err))
	}

	// Step 1: Verify staged file exists and checksum matches
	if err := ws.verifyStagedFile(request); err != nil {
		ws.failUpdate(status, fmt.Sprintf("staged file verification failed: %v", err))
		return
	}
	logMessage("Staged file verified")

	// Step 2: Disable protection on target file AND directory FIRST (before stopping service)
	// The directory has a DENY ACL on Everyone that prevents creating new files (like .backup)
	protMgr := protection.NewManager(ws.installPath, ws.config.AgentService)
	if err := protMgr.DisableProtectionForDir(ws.installPath); err != nil {
		logMessage(fmt.Sprintf("Warning: failed to disable directory protection: %v", err))
		// Continue anyway - might not have been protected
	}
	if err := protMgr.DisableProtectionForFile(request.TargetPath); err != nil {
		logMessage(fmt.Sprintf("Warning: failed to disable file protection: %v", err))
		// Continue anyway - might not have been protected
	}

	// Step 3: Stop the agent service (releases file lock)
	if err := ws.stopAgentService(); err != nil {
		ws.failUpdate(status, fmt.Sprintf("failed to stop agent: %v", err))
		return
	}
	logMessage("Agent service stopped")

	// Step 4: Create backup of current binary (after service stopped, file unlocked)
	backupPath, err := ws.createBackup(request.TargetPath)
	if err != nil {
		ws.failUpdate(status, fmt.Sprintf("failed to create backup: %v", err))
		return
	}
	status.BackupPath = backupPath
	logMessage(fmt.Sprintf("Backup created at %s", backupPath))

	// Step 5: Replace the binary using atomic move
	if err := ws.atomicReplace(request.StagedPath, request.TargetPath); err != nil {
		logMessage(fmt.Sprintf("Failed to replace binary: %v, attempting rollback", err))
		ws.rollbackUpdate(backupPath, request.TargetPath, status)
		return
	}
	logMessage("Binary replaced successfully")

	// Step 6: Re-enable protection on the new file and directory
	if err := protMgr.EnableProtectionForFile(request.TargetPath); err != nil {
		logMessage(fmt.Sprintf("Warning: failed to re-enable file protection: %v", err))
	}
	if err := protMgr.EnableProtectionForDir(ws.installPath); err != nil {
		logMessage(fmt.Sprintf("Warning: failed to re-enable directory protection: %v", err))
	}

	// Step 7: Start the agent service
	if err := ws.startAgentService(); err != nil {
		logMessage(fmt.Sprintf("Failed to start agent: %v, attempting rollback", err))
		ws.rollbackUpdate(backupPath, request.TargetPath, status)
		return
	}
	logMessage("Agent service started")

	// Step 8: Verify the update succeeded
	if err := ws.verifyUpdate(request.Version); err != nil {
		logMessage(fmt.Sprintf("Update verification failed: %v, attempting rollback", err))
		ws.rollbackUpdate(backupPath, request.TargetPath, status)
		return
	}

	// Success!
	status.State = ipc.StateComplete
	status.CompletedAt = time.Now()
	if err := ipc.WriteUpdateStatus(status); err != nil {
		logMessage(fmt.Sprintf("Failed to write success status: %v", err))
	}

	// Clean up
	ipc.DeleteUpdateRequest()
	os.Remove(backupPath)
	ipc.CleanupStagingDir()

	logMessage(fmt.Sprintf("Update to version %s completed successfully", request.Version))
}

// verifyStagedFile verifies the staged update file exists and checksum matches
func (ws *watchdogService) verifyStagedFile(request *ipc.UpdateRequest) error {
	// Check file exists
	info, err := os.Stat(request.StagedPath)
	if err != nil {
		return fmt.Errorf("staged file not found: %w", err)
	}

	if info.Size() == 0 {
		return fmt.Errorf("staged file is empty")
	}

	// Verify checksum if provided
	if request.Checksum != "" {
		file, err := os.Open(request.StagedPath)
		if err != nil {
			return fmt.Errorf("failed to open staged file: %w", err)
		}
		defer file.Close()

		hasher := sha256.New()
		if _, err := io.Copy(hasher, file); err != nil {
			return fmt.Errorf("failed to hash staged file: %w", err)
		}

		actualChecksum := hex.EncodeToString(hasher.Sum(nil))
		if actualChecksum != request.Checksum {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", request.Checksum, actualChecksum)
		}
	}

	return nil
}

// createBackup creates a backup of the current binary
func (ws *watchdogService) createBackup(targetPath string) (string, error) {
	backupPath := targetPath + ".backup"

	// Remove old backup if exists
	os.Remove(backupPath)

	// Copy current file to backup
	src, err := os.Open(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to open source: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("failed to copy to backup: %w", err)
	}

	return backupPath, nil
}

// stopAgentService stops the agent Windows service
func (ws *watchdogService) stopAgentService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ws.config.AgentService)
	if err != nil {
		return fmt.Errorf("failed to open agent service: %w", err)
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("failed to query service: %w", err)
	}

	if status.State == svc.Stopped {
		return nil // Already stopped
	}

	_, err = s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("failed to send stop control: %w", err)
	}

	// Wait for service to stop
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		status, err = s.Query()
		if err != nil {
			return fmt.Errorf("failed to query service status: %w", err)
		}
		if status.State == svc.Stopped {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for service to stop")
}

// startAgentService starts the agent Windows service
func (ws *watchdogService) startAgentService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ws.config.AgentService)
	if err != nil {
		return fmt.Errorf("failed to open agent service: %w", err)
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	// Wait for service to start
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		status, err := s.Query()
		if err != nil {
			return fmt.Errorf("failed to query service status: %w", err)
		}
		if status.State == svc.Running {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for service to start")
}

// atomicReplace replaces the target file with the source using Windows MoveFileEx
func (ws *watchdogService) atomicReplace(src, dst string) error {
	srcPtr, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}

	dstPtr, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}

	// MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH
	const flags = 0x1 | 0x8

	err = windows.MoveFileEx(srcPtr, dstPtr, flags)
	if err != nil {
		return fmt.Errorf("MoveFileEx failed: %w", err)
	}

	return nil
}

// verifyUpdate waits for the agent to start and runs comprehensive health checks
// This replaces the simple version check with full post-update health monitoring
func (ws *watchdogService) verifyUpdate(expectedVersion string) error {
	// First do a quick initial check for the version (up to updateVerifyTimeout)
	logMessage(fmt.Sprintf("Waiting for agent to report version %s...", expectedVersion))

	deadline := time.Now().Add(updateVerifyTimeout)
	versionConfirmed := false

	for time.Now().Before(deadline) {
		info, err := ipc.ReadAgentInfo()
		if err == nil && info != nil {
			if info.Version == expectedVersion {
				logMessage(fmt.Sprintf("Version confirmed: agent running %s", expectedVersion))
				versionConfirmed = true
				break
			}
			logMessage(fmt.Sprintf("Version mismatch: expected %s, got %s", expectedVersion, info.Version))
		}
		time.Sleep(updateVerifyInterval)
	}

	if !versionConfirmed {
		return fmt.Errorf("timeout waiting for agent to report version %s", expectedVersion)
	}

	// Now run comprehensive health checks for the full monitoring duration
	// This monitors for crashes, memory issues, and stability
	return ws.runPostUpdateHealthChecks(expectedVersion)
}

// rollbackUpdate restores the backup and restarts the agent
func (ws *watchdogService) rollbackUpdate(backupPath, targetPath string, status *ipc.UpdateStatus) {
	logMessage("Starting rollback...")

	// Stop agent if running
	ws.stopAgentService()

	// Disable protection on directory and file
	protMgr := protection.NewManager(ws.installPath, ws.config.AgentService)
	protMgr.DisableProtectionForDir(ws.installPath)
	protMgr.DisableProtectionForFile(targetPath)

	// Restore from backup
	if err := ws.atomicReplace(backupPath, targetPath); err != nil {
		logMessage(fmt.Sprintf("CRITICAL: Failed to restore backup: %v", err))
		status.State = ipc.StateFailed
		status.Error = fmt.Sprintf("rollback failed: %v", err)
		ipc.WriteUpdateStatus(status)
		return
	}

	// Re-enable protection on file and directory
	protMgr.EnableProtectionForFile(targetPath)
	protMgr.EnableProtectionForDir(ws.installPath)

	// Start agent
	if err := ws.startAgentService(); err != nil {
		logMessage(fmt.Sprintf("Warning: failed to start agent after rollback: %v", err))
	}

	status.State = ipc.StateRolledBack
	status.RolledBack = true
	status.CompletedAt = time.Now()
	ipc.WriteUpdateStatus(status)

	logMessage("Rollback completed")
}

// failUpdate marks the update as failed and cleans up
func (ws *watchdogService) failUpdate(status *ipc.UpdateStatus, reason string) {
	logMessage(fmt.Sprintf("Update failed: %s", reason))
	status.State = ipc.StateFailed
	status.Error = reason
	status.CompletedAt = time.Now()
	ipc.WriteUpdateStatus(status)

	// Clean up request file so we don't keep retrying
	ipc.DeleteUpdateRequest()
}

// ============================================================================
// Comprehensive Health Checks for Auto-Rollback
// ============================================================================

// HealthCheckResult contains the result of a single health check
type HealthCheckResult struct {
	Name    string
	Passed  bool
	Message string
	Value   interface{}
}

// PostUpdateHealth tracks the overall health status during post-update monitoring
type PostUpdateHealth struct {
	VersionConfirmed  bool
	ServiceRunning    bool
	MemoryOK          bool
	CrashCount        int
	HealthFileWritten bool
	AllChecksPassed   bool
	FailureReason     string
}

// runPostUpdateHealthChecks runs comprehensive health checks for the specified duration
// Returns nil if all health checks pass, or an error describing what failed
func (ws *watchdogService) runPostUpdateHealthChecks(expectedVersion string) error {
	logMessage(fmt.Sprintf("Starting %v post-update health monitoring for version %s", healthCheckDuration, expectedVersion))

	health := &PostUpdateHealth{}
	startTime := time.Now()
	checkTicker := time.NewTicker(healthCheckInterval)
	defer checkTicker.Stop()

	// Track crash count by monitoring service state transitions
	lastServiceState := svc.Stopped
	crashTimes := []time.Time{}

	for {
		elapsed := time.Since(startTime)
		if elapsed >= healthCheckDuration {
			// Monitoring period complete - verify all checks passed
			return ws.evaluateFinalHealth(health, expectedVersion)
		}

		select {
		case <-ws.stopChan:
			return fmt.Errorf("watchdog stopped during health monitoring")
		case <-checkTicker.C:
			results := ws.runHealthChecks(expectedVersion, &lastServiceState, &crashTimes)

			// Update health status
			for _, result := range results {
				switch result.Name {
				case "version":
					if result.Passed {
						health.VersionConfirmed = true
					}
				case "service_running":
					health.ServiceRunning = result.Passed
				case "memory":
					health.MemoryOK = result.Passed
				case "crash_count":
					if count, ok := result.Value.(int); ok {
						health.CrashCount = count
					}
				case "health_file":
					if result.Passed {
						health.HealthFileWritten = true
					}
				}

				// Log failed checks
				if !result.Passed {
					logMessage(fmt.Sprintf("[Health] FAILED: %s - %s", result.Name, result.Message))
				}
			}

			// Check for immediate rollback triggers
			if reason := ws.checkRollbackTriggers(health, elapsed); reason != "" {
				health.FailureReason = reason
				return fmt.Errorf("health check failed: %s", reason)
			}

			// Log progress
			logMessage(fmt.Sprintf("[Health] Monitoring: %v/%v - Version:%v Service:%v Memory:%v Crashes:%d",
				elapsed.Round(time.Second), healthCheckDuration,
				health.VersionConfirmed, health.ServiceRunning, health.MemoryOK, health.CrashCount))
		}
	}
}

// runHealthChecks performs all individual health checks
func (ws *watchdogService) runHealthChecks(expectedVersion string, lastState *svc.State, crashTimes *[]time.Time) []HealthCheckResult {
	var results []HealthCheckResult

	// Check 1: Version verification
	results = append(results, ws.checkAgentVersion(expectedVersion))

	// Check 2: Service running state
	serviceResult, currentState := ws.checkServiceRunning()
	results = append(results, serviceResult)

	// Detect crashes (service went from Running to Stopped unexpectedly)
	if *lastState == svc.Running && currentState == svc.Stopped {
		*crashTimes = append(*crashTimes, time.Now())
		logMessage("[Health] Detected agent crash/stop")
	}
	*lastState = currentState

	// Clean up old crash times (only count crashes in the last minute)
	cutoff := time.Now().Add(-1 * time.Minute)
	var recentCrashes []time.Time
	for _, t := range *crashTimes {
		if t.After(cutoff) {
			recentCrashes = append(recentCrashes, t)
		}
	}
	*crashTimes = recentCrashes

	// Check 3: Crash count
	results = append(results, HealthCheckResult{
		Name:    "crash_count",
		Passed:  len(recentCrashes) < maxCrashesPerMinute,
		Message: fmt.Sprintf("%d crashes in last minute (max: %d)", len(recentCrashes), maxCrashesPerMinute),
		Value:   len(recentCrashes),
	})

	// Check 4: Memory usage
	results = append(results, ws.checkAgentMemory())

	// Check 5: Health file written (agent writes status to indicate it's healthy)
	results = append(results, ws.checkHealthFile())

	return results
}

// checkAgentVersion verifies the agent is reporting the expected version
func (ws *watchdogService) checkAgentVersion(expectedVersion string) HealthCheckResult {
	info, err := ipc.ReadAgentInfo()
	if err != nil {
		return HealthCheckResult{
			Name:    "version",
			Passed:  false,
			Message: fmt.Sprintf("cannot read agent info: %v", err),
		}
	}

	if info == nil {
		return HealthCheckResult{
			Name:    "version",
			Passed:  false,
			Message: "agent info file not found",
		}
	}

	if info.Version != expectedVersion {
		return HealthCheckResult{
			Name:    "version",
			Passed:  false,
			Message: fmt.Sprintf("version mismatch: expected %s, got %s", expectedVersion, info.Version),
			Value:   info.Version,
		}
	}

	return HealthCheckResult{
		Name:    "version",
		Passed:  true,
		Message: fmt.Sprintf("version confirmed: %s", expectedVersion),
		Value:   info.Version,
	}
}

// checkServiceRunning verifies the agent service is in running state
func (ws *watchdogService) checkServiceRunning() (HealthCheckResult, svc.State) {
	m, err := mgr.Connect()
	if err != nil {
		return HealthCheckResult{
			Name:    "service_running",
			Passed:  false,
			Message: fmt.Sprintf("cannot connect to SCM: %v", err),
		}, svc.Stopped
	}
	defer m.Disconnect()

	s, err := m.OpenService(ws.config.AgentService)
	if err != nil {
		return HealthCheckResult{
			Name:    "service_running",
			Passed:  false,
			Message: fmt.Sprintf("cannot open service: %v", err),
		}, svc.Stopped
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return HealthCheckResult{
			Name:    "service_running",
			Passed:  false,
			Message: fmt.Sprintf("cannot query service: %v", err),
		}, svc.Stopped
	}

	if status.State != svc.Running {
		return HealthCheckResult{
			Name:    "service_running",
			Passed:  false,
			Message: fmt.Sprintf("service not running: state=%d", status.State),
			Value:   status.State,
		}, status.State
	}

	return HealthCheckResult{
		Name:    "service_running",
		Passed:  true,
		Message: "service is running",
		Value:   status.State,
	}, status.State
}

// PROCESS_MEMORY_COUNTERS for GetProcessMemoryInfo
type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

var (
	modpsapi                = windows.NewLazySystemDLL("psapi.dll")
	procGetProcessMemoryInfo = modpsapi.NewProc("GetProcessMemoryInfo")
)

// getProcessMemoryInfo calls the Windows API to get process memory info
func getProcessMemoryInfo(handle windows.Handle, memCounters *processMemoryCounters, cb uint32) error {
	ret, _, err := procGetProcessMemoryInfo.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(memCounters)),
		uintptr(cb),
	)
	if ret == 0 {
		return err
	}
	return nil
}

// checkAgentMemory checks if the agent process memory usage is within limits
func (ws *watchdogService) checkAgentMemory() HealthCheckResult {
	// Get agent process by querying the service
	m, err := mgr.Connect()
	if err != nil {
		return HealthCheckResult{
			Name:    "memory",
			Passed:  true, // Give benefit of doubt if we can't check
			Message: "cannot check memory: SCM connection failed",
		}
	}
	defer m.Disconnect()

	s, err := m.OpenService(ws.config.AgentService)
	if err != nil {
		return HealthCheckResult{
			Name:    "memory",
			Passed:  true,
			Message: "cannot check memory: service not found",
		}
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil || status.ProcessId == 0 {
		return HealthCheckResult{
			Name:    "memory",
			Passed:  true,
			Message: "cannot check memory: no PID",
		}
	}

	// Open the process to get memory info
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, status.ProcessId)
	if err != nil {
		return HealthCheckResult{
			Name:    "memory",
			Passed:  true,
			Message: "cannot check memory: process access denied",
		}
	}
	defer windows.CloseHandle(handle)

	var memInfo processMemoryCounters
	memInfo.CB = uint32(unsafe.Sizeof(memInfo))
	err = getProcessMemoryInfo(handle, &memInfo, memInfo.CB)
	if err != nil {
		return HealthCheckResult{
			Name:    "memory",
			Passed:  true,
			Message: "cannot check memory: GetProcessMemoryInfo failed",
		}
	}

	memoryMB := uint64(memInfo.WorkingSetSize) / (1024 * 1024)
	if memoryMB > uint64(maxAgentMemoryMB) {
		return HealthCheckResult{
			Name:    "memory",
			Passed:  false,
			Message: fmt.Sprintf("memory usage %dMB exceeds limit %dMB", memoryMB, maxAgentMemoryMB),
			Value:   memoryMB,
		}
	}

	return HealthCheckResult{
		Name:    "memory",
		Passed:  true,
		Message: fmt.Sprintf("memory usage OK: %dMB", memoryMB),
		Value:   memoryMB,
	}
}

// checkHealthFile checks if the agent has written its health status file recently
func (ws *watchdogService) checkHealthFile() HealthCheckResult {
	// Check if agent info file exists and was modified recently
	infoPath := ipc.AgentInfoPath()
	fileInfo, err := os.Stat(infoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return HealthCheckResult{
				Name:    "health_file",
				Passed:  false,
				Message: "agent info file not found",
			}
		}
		return HealthCheckResult{
			Name:    "health_file",
			Passed:  false,
			Message: fmt.Sprintf("cannot stat agent info file: %v", err),
		}
	}

	// Check if the file was modified recently (within healthStatusTimeout)
	modAge := time.Since(fileInfo.ModTime())
	if modAge > healthStatusTimeout {
		return HealthCheckResult{
			Name:    "health_file",
			Passed:  false,
			Message: fmt.Sprintf("agent info is stale: last modified %v ago", modAge.Round(time.Second)),
		}
	}

	// Also verify the file is valid JSON with version info
	info, err := ipc.ReadAgentInfo()
	if err != nil || info == nil {
		return HealthCheckResult{
			Name:    "health_file",
			Passed:  false,
			Message: "agent info file exists but cannot be read",
		}
	}

	return HealthCheckResult{
		Name:    "health_file",
		Passed:  true,
		Message: fmt.Sprintf("agent info fresh: modified %v ago", modAge.Round(time.Second)),
	}
}

// checkRollbackTriggers checks if any immediate rollback conditions are met
func (ws *watchdogService) checkRollbackTriggers(health *PostUpdateHealth, elapsed time.Duration) string {
	// Trigger 1: Service stopped and stays stopped for 10+ seconds after starting
	if elapsed > 10*time.Second && !health.ServiceRunning {
		return "agent service stopped and did not recover"
	}

	// Trigger 2: Too many crashes
	if health.CrashCount >= maxCrashesPerMinute {
		return fmt.Sprintf("agent crashed %d times in the last minute", health.CrashCount)
	}

	// Trigger 3: Memory exceeded (checked in individual check)
	// This is handled by MemoryOK being set to false

	// Trigger 4: Version not confirmed after 30 seconds
	if elapsed > updateVerifyTimeout && !health.VersionConfirmed {
		return "agent did not report expected version within timeout"
	}

	return ""
}

// evaluateFinalHealth performs final evaluation after the monitoring period
func (ws *watchdogService) evaluateFinalHealth(health *PostUpdateHealth, expectedVersion string) error {
	// Must have confirmed version
	if !health.VersionConfirmed {
		return fmt.Errorf("agent never reported expected version %s", expectedVersion)
	}

	// Must be running at end of monitoring period
	if !health.ServiceRunning {
		return fmt.Errorf("agent service not running at end of health monitoring")
	}

	// Must have written health file
	if !health.HealthFileWritten {
		return fmt.Errorf("agent did not write health status within %v", healthStatusTimeout)
	}

	// Memory must be OK
	if !health.MemoryOK {
		return fmt.Errorf("agent memory usage exceeded %dMB", maxAgentMemoryMB)
	}

	// Should not have crashed too many times (relaxed check - any crashes during full period)
	if health.CrashCount > 0 {
		logMessage(fmt.Sprintf("[Health] Warning: agent crashed %d times during monitoring but recovered", health.CrashCount))
	}

	health.AllChecksPassed = true
	logMessage("[Health] All post-update health checks passed")
	return nil
}
