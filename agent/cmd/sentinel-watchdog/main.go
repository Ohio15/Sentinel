package main

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/sentinel/agent/internal/ipc"
	"github.com/sentinel/agent/internal/protection"
	"github.com/sentinel/agent/internal/selfupdate"
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
	updateVerifyTimeout    = 120 * time.Second // How long to wait for agent to report version
	updateVerifyInterval   = 2 * time.Second  // How often to check agent version during verification

	// Health check constants for auto-rollback
	// NOTE: These are intentionally very lenient to avoid false rollbacks from:
	// - Network disconnects causing brief service restarts
	// - Windows SCM auto-recovery restarts counting as "crashes"
	// - Normal reconnection behavior during network instability
	// CRITICAL: Old watchdog monitors with old settings during update, so new settings
	// won't take effect until AFTER the update succeeds. Set very high tolerance.
	healthCheckInterval   = 10 * time.Second  // How often to check agent health after update
	healthCheckDuration   = 120 * time.Second // How long to monitor health after update (2 min)
	maxAgentMemoryMB      = 500               // Maximum memory usage before triggering rollback
	maxCrashesPerMinute   = 20                // Maximum crashes before triggering rollback (very lenient)
	healthStatusTimeout   = 60 * time.Second  // Time to wait for agent to write health status

	// Independent update polling constants (Layer 1 - resilient updates)
	// The watchdog polls the server directly, independent of the agent's WebSocket connection
	independentPollInterval    = 15 * time.Minute // How often to check for updates
	independentPollMaxBackoff  = 2 * time.Hour    // Maximum backoff on repeated failures
	independentDownloadTimeout = 10 * time.Minute // Timeout for downloading update binary
)

var (
	Version = "1.77.5"
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
	config              *WatchdogConfig
	restartCount        int
	consecutiveFailCycles int // Track how many times we hit maxRestarts and cooled down
	lastRestart         time.Time
	mu                  sync.Mutex
	stopChan            chan struct{}
	installPath         string
	updateInProgress    bool
	pipeServer          *ipc.PipeServer
	selfUpdater         *selfupdate.SelfUpdater
	selfUpdateInProgress bool
	protMgr             *protection.Manager // Shared protection manager for tamper monitoring + updates
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

	// Create self-updater
	selfUpdater, err := selfupdate.New(serviceName, Version, installPath)
	if err != nil {
		elog.Warning(1, fmt.Sprintf("Failed to create self-updater: %v", err))
	}

	cfg := loadConfig(installPath)
	ws := &watchdogService{
		config:      cfg,
		stopChan:    make(chan struct{}),
		installPath: installPath,
		selfUpdater: selfUpdater,
		protMgr:     protection.NewManager(installPath, cfg.AgentService),
	}

	// Write watchdog info on startup
	if selfUpdater != nil {
		if err := selfUpdater.WriteWatchdogInfo(); err != nil {
			elog.Warning(1, fmt.Sprintf("Failed to write watchdog info: %v", err))
		}

		// Check result of any previous self-update
		if status, err := selfUpdater.CheckUpdateResult(); err == nil && status != nil {
			if status.State == ipc.StateComplete {
				elog.Info(1, fmt.Sprintf("Previous self-update to v%s successful", status.Version))
				selfUpdater.CleanupAfterUpdate()
			} else if status.State == ipc.StateRolledBack {
				elog.Warning(1, fmt.Sprintf("Previous self-update to v%s was rolled back: %s", status.Version, status.Error))
			}
		}
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

	// Create self-updater
	selfUpdater, err := selfupdate.New(serviceName, Version, installPath)
	if err != nil {
		log.Printf("Warning: Failed to create self-updater: %v", err)
	}

	debugCfg := loadConfig(installPath)
	ws := &watchdogService{
		config:      debugCfg,
		stopChan:    make(chan struct{}),
		installPath: installPath,
		selfUpdater: selfUpdater,
		protMgr:     protection.NewManager(installPath, debugCfg.AgentService),
	}

	// Write watchdog info on startup
	if selfUpdater != nil {
		if err := selfUpdater.WriteWatchdogInfo(); err != nil {
			log.Printf("Warning: Failed to write watchdog info: %v", err)
		}

		// Check result of any previous self-update
		if status, err := selfUpdater.CheckUpdateResult(); err == nil && status != nil {
			if status.State == ipc.StateComplete {
				log.Printf("Previous self-update to v%s successful", status.Version)
				selfUpdater.CleanupAfterUpdate()
			} else if status.State == ipc.StateRolledBack {
				log.Printf("Previous self-update to v%s was rolled back: %s", status.Version, status.Error)
			}
		}
	}

	// Start pipe server for update coordination
	go ws.startPipeServer()

	// Check for any pending updates from before restart
	go ws.checkForPendingUpdate()

	// Start watchdog self-update checker
	go ws.watchdogUpdateChecker()

	// Start independent HTTP polling (Layer 1 - resilient updates)
	go ws.independentUpdatePoller()

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

	// Start update checker goroutine (for agent updates via IPC)
	go ws.updateChecker()

	// Start watchdog self-update checker goroutine
	go ws.watchdogUpdateChecker()

	// Start independent HTTP polling (Layer 1 - resilient updates)
	// This runs completely independent of the agent and doesn't require IPC
	go ws.independentUpdatePoller()

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
		// Completed a full fail cycle (maxRestarts reached + cooldown elapsed)
		ws.consecutiveFailCycles++
		logMessage(fmt.Sprintf("Restart cooldown elapsed — consecutive fail cycles: %d", ws.consecutiveFailCycles))

		// If we've gone through 2+ full fail cycles, the binary is likely bad
		if ws.consecutiveFailCycles >= 2 {
			logMessage("CRITICAL: Agent binary appears corrupted — 2 consecutive fail cycles detected")

			// Write alert for the agent/server
			ipc.WriteAlert(&ipc.AlertRelayPayload{
				Severity: "critical",
				Title:    "Agent Binary Corrupted — Bootstrap Recovery Triggered",
				Message:  fmt.Sprintf("Agent failed %d restart cycles (%d restarts each). Binary declared bad. Attempting bootstrap recovery.",
					ws.consecutiveFailCycles, ws.config.MaxRestarts),
			})

			// Attempt bootstrap recovery
			bootstrapPath := filepath.Join(ws.installPath, "sentinel-bootstrap.exe")
			if _, statErr := os.Stat(bootstrapPath); statErr == nil {
				logMessage(fmt.Sprintf("Launching bootstrap recovery: %s --repair --silent", bootstrapPath))
				cmd := exec.Command(bootstrapPath, "--repair", "--silent")
				cmd.Dir = ws.installPath
				if startErr := cmd.Start(); startErr != nil {
					logMessage(fmt.Sprintf("Failed to launch bootstrap recovery: %v", startErr))
				} else {
					logMessage("Bootstrap recovery process launched successfully")
				}
			} else {
				logMessage(fmt.Sprintf("Bootstrap binary not found at %s — cannot auto-recover", bootstrapPath))
			}

			// Reset cycles so we don't spam recovery attempts every cooldown
			ws.consecutiveFailCycles = 0
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
		ws.consecutiveFailCycles = 0 // Successful restart resets fail cycle tracking
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
			logMessage(fmt.Sprintf("[Pipe] Received MsgUpdateReady — payload length=%d", len(msg.Payload)))
			if msg.Payload != "" {
				logMessage(fmt.Sprintf("[Pipe] Payload: %s", msg.Payload))
			}
			// Verify the update-request.json file exists (agent should have written it before signaling)
			reqPath := ipc.UpdateRequestPath()
			if data, err := os.ReadFile(reqPath); err != nil {
				logMessage(fmt.Sprintf("[Pipe] WARNING: update-request.json NOT FOUND at %s after pipe signal: %v", reqPath, err))
			} else {
				logMessage(fmt.Sprintf("[Pipe] Confirmed update-request.json exists (%d bytes) — updateChecker will process on next tick", len(data)))
			}
			// The update checker will pick up the request file
			return nil

		case ipc.MsgWatchdogUpdateReady:
			logMessage(fmt.Sprintf("[Pipe] Received MsgWatchdogUpdateReady — payload length=%d", len(msg.Payload)))
			// The watchdog update checker will pick up the request file
			return nil

		case ipc.MsgVersionQuery:
			return &ipc.PipeMessage{
				Type:    ipc.MsgVersionResp,
				Payload: Version,
			}

		case ipc.MsgWatchdogVersionQuery:
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

	requestPath := ipc.UpdateRequestPath()
	logMessage(fmt.Sprintf("[StartupCheck] Checking for pending update at %s", requestPath))

	request, err := ipc.ReadUpdateRequest()
	if err != nil {
		logMessage(fmt.Sprintf("[StartupCheck] Error reading update request: %v", err))
		return
	}

	if request == nil {
		logMessage("[StartupCheck] No pending update request file found")
		return
	}

	logMessage(fmt.Sprintf("[StartupCheck] Found pending update request: version=%s staged=%s target=%s requestedAt=%s",
		request.Version, request.StagedPath, request.TargetPath, request.RequestedAt.Format(time.RFC3339)))

	// Reject stale requests older than 1 hour — prevents old/tampered requests from being applied
	if !request.RequestedAt.IsZero() && time.Since(request.RequestedAt) > 1*time.Hour {
		logMessage(fmt.Sprintf("[StartupCheck] REJECTED: Update request is stale (age: %v) — deleting for security", time.Since(request.RequestedAt).Round(time.Second)))
		ipc.DeleteUpdateRequest()
		return
	}

	// Check if this update was already applied (agent is running new version)
	infoPath := ipc.AgentInfoPath()
	info, infoErr := ipc.ReadAgentInfo()
	if infoErr != nil {
		logMessage(fmt.Sprintf("[StartupCheck] Error reading agent info at %s: %v", infoPath, infoErr))
	}
	if info != nil {
		logMessage(fmt.Sprintf("[StartupCheck] Agent info: version=%s pid=%d startedAt=%s", info.Version, info.PID, info.StartedAt.Format(time.RFC3339)))
		if info.Version == request.Version {
			logMessage(fmt.Sprintf("[StartupCheck] Update already applied: agent running version %s — cleaning up", info.Version))
			ipc.DeleteUpdateRequest()
			status, _ := ipc.ReadUpdateStatus()
			if status == nil || status.State != ipc.StateComplete {
				ipc.WriteUpdateStatus(&ipc.UpdateStatus{
					State:       ipc.StateComplete,
					Version:     request.Version,
					CompletedAt: time.Now(),
				})
				logMessage("[StartupCheck] Wrote completion status")
			}
			return
		}
		logMessage(fmt.Sprintf("[StartupCheck] Version mismatch: agent=%s, request=%s — update not yet applied", info.Version, request.Version))
	} else {
		logMessage("[StartupCheck] No agent info file — cannot verify if update was applied")
	}

	// Verify staged file still exists
	if stagedInfo, statErr := os.Stat(request.StagedPath); statErr != nil {
		logMessage(fmt.Sprintf("[StartupCheck] WARNING: Staged file not found at %s: %v", request.StagedPath, statErr))
	} else {
		logMessage(fmt.Sprintf("[StartupCheck] Staged file exists: size=%d bytes", stagedInfo.Size()))
	}

	// Check for watchdog self-update that would block this
	wdRequestPath := ipc.WatchdogUpdateRequestPath()
	if wdReq, wdErr := ipc.ReadWatchdogUpdateRequest(); wdErr == nil && wdReq != nil {
		logMessage(fmt.Sprintf("[StartupCheck] WARNING: watchdog-update-request.json exists at %s (version=%s) — this will BLOCK agent updates in updateChecker!", wdRequestPath, wdReq.Version))
	}

	// There's a pending update that wasn't completed - attempt to apply it
	ws.mu.Lock()
	if ws.updateInProgress {
		logMessage("[StartupCheck] Another update already in progress — skipping")
		ws.mu.Unlock()
		return
	}
	ws.updateInProgress = true
	ws.mu.Unlock()

	logMessage("[StartupCheck] Launching applyUpdate goroutine for pending update")
	go ws.applyUpdate(request)
}

// updateChecker periodically checks for pending update requests
func (ws *watchdogService) updateChecker() {
	ticker := time.NewTicker(updateCheckInterval)
	defer ticker.Stop()

	requestPath := ipc.UpdateRequestPath()
	wdRequestPath := ipc.WatchdogUpdateRequestPath()

	logMessage(fmt.Sprintf("[UpdateChecker] Started — polling every %v", updateCheckInterval))
	logMessage(fmt.Sprintf("[UpdateChecker]   agent request file:    %s", requestPath))
	logMessage(fmt.Sprintf("[UpdateChecker]   watchdog request file: %s", wdRequestPath))
	logMessage(fmt.Sprintf("[UpdateChecker]   selfUpdater initialized: %v", ws.selfUpdater != nil))

	pollCount := 0
	consecutiveDefers := 0
	for {
		select {
		case <-ws.stopChan:
			return
		case <-ticker.C:
			pollCount++

			// Verbose logging: first 20 ticks, then every 60
			verbose := pollCount <= 20 || pollCount%60 == 0

			ws.mu.Lock()
			inProgress := ws.updateInProgress
			selfUpdateInProgress := ws.selfUpdateInProgress
			ws.mu.Unlock()

			if inProgress {
				if verbose {
					logMessage(fmt.Sprintf("[UpdateChecker] poll #%d: BLOCKED — agent update in progress", pollCount))
				}
				continue
			}
			if selfUpdateInProgress {
				if verbose {
					logMessage(fmt.Sprintf("[UpdateChecker] poll #%d: BLOCKED — watchdog self-update in progress", pollCount))
				}
				continue
			}

			// CRITICAL: Check for pending watchdog self-update FIRST
			// If there's a watchdog update pending, defer agent update until watchdog is updated
			// This ensures the new watchdog (with lenient settings) handles the agent update
			// SAFETY: Deferral is capped at 360 ticks (30 min) and stale files (>30 min) are auto-deleted
			if ws.selfUpdater != nil {
				watchdogRequest, wdErr := ws.selfUpdater.CheckForPendingUpdate()
				if wdErr != nil {
					logMessage(fmt.Sprintf("[UpdateChecker] poll #%d: Error reading watchdog self-update request at %s: %v", pollCount, wdRequestPath, wdErr))
				}
				if watchdogRequest != nil {
					if watchdogRequest.Version != Version {
						// Check file staleness — if the request has been sitting for >30 min, it's dead
						stale := false
						if wdFileInfo, wdStatErr := os.Stat(wdRequestPath); wdStatErr == nil {
							fileAge := time.Since(wdFileInfo.ModTime())
							if fileAge > 30*time.Minute {
								logMessage(fmt.Sprintf("[UpdateChecker] poll #%d: WARNING — watchdog-update-request.json is stale (age: %v, mod: %s). Deleting to unblock agent updates.",
									pollCount, fileAge.Round(time.Second), wdFileInfo.ModTime().Format(time.RFC3339)))
								if delErr := ipc.DeleteWatchdogUpdateRequest(); delErr != nil {
									logMessage(fmt.Sprintf("[UpdateChecker] Failed to delete stale watchdog request: %v", delErr))
								}
								stale = true
								consecutiveDefers = 0
							}
						}

						if !stale {
							consecutiveDefers++

							// Cap: 360 consecutive defers = 30 minutes at 5s intervals
							if consecutiveDefers >= 360 {
								logMessage(fmt.Sprintf("[UpdateChecker] poll #%d: WARNING — deferral cap reached (%d consecutive). Force-clearing watchdog gate to unblock agent updates.",
									pollCount, consecutiveDefers))
								if delErr := ipc.DeleteWatchdogUpdateRequest(); delErr != nil {
									logMessage(fmt.Sprintf("[UpdateChecker] Failed to delete watchdog request at cap: %v", delErr))
								}
								consecutiveDefers = 0
							} else {
								// Log every deferral for first 10, then every 60
								if consecutiveDefers <= 10 || consecutiveDefers%60 == 0 {
									logMessage(fmt.Sprintf("[UpdateChecker] poll #%d: DEFERRED — watchdog self-update pending to v%s (current: %s) [deferred %d/360 times, file: %s]",
										pollCount, watchdogRequest.Version, Version, consecutiveDefers, wdRequestPath))
								}
								continue
							}
						}
					} else {
						// Watchdog request exists but version matches — stale file, log it
						if verbose {
							logMessage(fmt.Sprintf("[UpdateChecker] poll #%d: watchdog-update-request.json exists but version %s matches current — will be cleaned by watchdogUpdateChecker", pollCount, watchdogRequest.Version))
						}
					}
				} else {
					if consecutiveDefers > 0 {
						logMessage(fmt.Sprintf("[UpdateChecker] poll #%d: watchdog self-update deferral cleared after %d defers", pollCount, consecutiveDefers))
						consecutiveDefers = 0
					}
				}
			}

			// Check for agent update request file
			request, err := ipc.ReadUpdateRequest()
			if err != nil {
				logMessage(fmt.Sprintf("[UpdateChecker] poll #%d: Error reading update request at %s: %v", pollCount, requestPath, err))
				continue
			}

			if request == nil {
				if verbose {
					// Also check raw file existence for path debugging
					_, statErr := os.Stat(requestPath)
					logMessage(fmt.Sprintf("[UpdateChecker] poll #%d: no update request (file exists: %v, stat err: %v)",
						pollCount, statErr == nil, statErr))
				}
				continue
			}

			logMessage(fmt.Sprintf("[UpdateChecker] poll #%d: === UPDATE REQUEST FOUND ===", pollCount))
			logMessage(fmt.Sprintf("[UpdateChecker]   version:     %s", request.Version))
			logMessage(fmt.Sprintf("[UpdateChecker]   stagedPath:  %s", request.StagedPath))
			logMessage(fmt.Sprintf("[UpdateChecker]   targetPath:  %s", request.TargetPath))
			logMessage(fmt.Sprintf("[UpdateChecker]   requestedAt: %s", request.RequestedAt.Format(time.RFC3339)))
			logMessage(fmt.Sprintf("[UpdateChecker]   requestedBy: %s", request.RequestedBy))

			// Reject stale requests older than 1 hour — prevents old/tampered requests from being applied
			if !request.RequestedAt.IsZero() && time.Since(request.RequestedAt) > 1*time.Hour {
				logMessage(fmt.Sprintf("[UpdateChecker] poll #%d: REJECTED — update request is stale (age: %v) — deleting for security",
					pollCount, time.Since(request.RequestedAt).Round(time.Second)))
				ipc.DeleteUpdateRequest()
				continue
			}

			// Verify staged file exists before starting update
			stagedInfo, statErr := os.Stat(request.StagedPath)
			if statErr != nil {
				logMessage(fmt.Sprintf("[UpdateChecker] ERROR: Staged file not accessible at %s: %v — deleting stale request", request.StagedPath, statErr))
				ipc.DeleteUpdateRequest()
				continue
			}
			logMessage(fmt.Sprintf("[UpdateChecker] Staged file verified: size=%d bytes, mode=%s", stagedInfo.Size(), stagedInfo.Mode()))

			// Verify target path exists (the current agent binary)
			targetInfo, targetErr := os.Stat(request.TargetPath)
			if targetErr != nil {
				logMessage(fmt.Sprintf("[UpdateChecker] WARNING: Target path not accessible at %s: %v", request.TargetPath, targetErr))
			} else {
				logMessage(fmt.Sprintf("[UpdateChecker] Target binary: size=%d bytes, mode=%s", targetInfo.Size(), targetInfo.Mode()))
			}

			ws.mu.Lock()
			ws.updateInProgress = true
			ws.mu.Unlock()

			logMessage("[UpdateChecker] updateInProgress=true — launching applyUpdate goroutine")
			go ws.applyUpdate(request)
		}
	}
}

// watchdogUpdateChecker periodically checks for pending watchdog self-update requests
func (ws *watchdogService) watchdogUpdateChecker() {
	// Short delay to allow system to stabilize
	// NOTE: Reduced from 10s to 2s to ensure watchdog updates before agent updates
	time.Sleep(2 * time.Second)

	ticker := time.NewTicker(updateCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ws.stopChan:
			return
		case <-ticker.C:
			ws.mu.Lock()
			inProgress := ws.selfUpdateInProgress
			ws.mu.Unlock()

			if inProgress {
				continue
			}

			// Check for watchdog update request
			if ws.selfUpdater == nil {
				continue
			}

			request, err := ws.selfUpdater.CheckForPendingUpdate()
			if err != nil {
				logMessage(fmt.Sprintf("Error checking for watchdog updates: %v", err))
				continue
			}

			if request == nil {
				continue
			}

			// Don't update if target version matches current version
			if request.Version == Version {
				logMessage(fmt.Sprintf("Watchdog already at version %s, cleaning up request", Version))
				ipc.DeleteWatchdogUpdateRequest()
				continue
			}

			logMessage(fmt.Sprintf("Watchdog update request found for version %s (current: %s)", request.Version, Version))

			ws.mu.Lock()
			ws.selfUpdateInProgress = true
			ws.mu.Unlock()

			go ws.applySelfUpdate(request)
		}
	}
}

// applySelfUpdate performs the watchdog self-update via Task Scheduler
func (ws *watchdogService) applySelfUpdate(request *ipc.WatchdogUpdateRequest) {
	defer func() {
		ws.mu.Lock()
		ws.selfUpdateInProgress = false
		ws.mu.Unlock()
	}()

	if ws.selfUpdater == nil {
		logMessage("[SelfUpdate] Self-updater not initialized")
		// Clean up the request file so we don't block agent updates forever
		ipc.DeleteWatchdogUpdateRequest()
		return
	}

	logMessage(fmt.Sprintf("[SelfUpdate] Starting watchdog self-update to version %s", request.Version))

	if err := ws.selfUpdater.ApplySelfUpdate(request); err != nil {
		logMessage(fmt.Sprintf("[SelfUpdate] Self-update FAILED: %v — deleting request file to unblock agent updates", err))
		// Always clean up on failure so the self-update gate doesn't block agent updates.
		// The agent will re-stage a watchdog update on its next cycle if needed.
		if delErr := ipc.DeleteWatchdogUpdateRequest(); delErr != nil {
			logMessage(fmt.Sprintf("[SelfUpdate] Warning: failed to delete watchdog request after failure: %v", delErr))
		}
		return
	}

	// Success path: clean up the request file before the scheduled task restarts us.
	// If the restart happens before we get here, the new watchdog will see version match
	// and clean it up via the watchdogUpdateChecker's version==current check.
	if delErr := ipc.DeleteWatchdogUpdateRequest(); delErr != nil {
		logMessage(fmt.Sprintf("[SelfUpdate] Warning: failed to delete watchdog request after success: %v", delErr))
	}

	// The scheduled task will stop this service, so we just wait
	logMessage("[SelfUpdate] Self-update initiated, watchdog will be restarted by scheduled task")
}

// applyUpdate performs the actual update operation
func (ws *watchdogService) applyUpdate(request *ipc.UpdateRequest) {
	updateStart := time.Now()
	defer func() {
		if r := recover(); r != nil {
			logMessage(fmt.Sprintf("[ApplyUpdate] PANIC recovered: %v", r))
		}
		ws.mu.Lock()
		ws.updateInProgress = false
		ws.mu.Unlock()
		logMessage(fmt.Sprintf("[ApplyUpdate] Completed in %v — updateInProgress reset to false", time.Since(updateStart).Round(time.Millisecond)))
	}()

	logMessage(fmt.Sprintf("[ApplyUpdate] === BEGIN === version=%s", request.Version))
	logMessage(fmt.Sprintf("[ApplyUpdate]   stagedPath: %s", request.StagedPath))
	logMessage(fmt.Sprintf("[ApplyUpdate]   targetPath: %s", request.TargetPath))
	logMessage(fmt.Sprintf("[ApplyUpdate]   installPath: %s", ws.installPath))
	logMessage(fmt.Sprintf("[ApplyUpdate]   agentService: %s", ws.config.AgentService))

	// Write status: applying
	statusPath := ipc.UpdateStatusPath()
	status := &ipc.UpdateStatus{
		State:     ipc.StateApplying,
		Version:   request.Version,
		StartedAt: time.Now(),
	}
	if err := ipc.WriteUpdateStatus(status); err != nil {
		logMessage(fmt.Sprintf("[ApplyUpdate] WARNING: Failed to write 'applying' status to %s: %v", statusPath, err))
	} else {
		logMessage(fmt.Sprintf("[ApplyUpdate] Step 0/10: Wrote 'applying' status to %s", statusPath))
	}

	// Step 1: Verify staged file exists and checksum matches
	stepStart := time.Now()
	if err := ws.verifyStagedFile(request); err != nil {
		logMessage(fmt.Sprintf("[ApplyUpdate] Step 1 FAILED in %v", time.Since(stepStart)))
		ws.failUpdate(status, fmt.Sprintf("staged file verification failed: %v", err))
		return
	}
	logMessage(fmt.Sprintf("[ApplyUpdate] Step 1/10: Staged file verified in %v", time.Since(stepStart).Round(time.Millisecond)))

	// Step 2: Disable protection on target file AND directory FIRST (before stopping service)
	stepStart = time.Now()
	protMgr := protection.NewManager(ws.installPath, ws.config.AgentService)
	if err := protMgr.DisableProtectionForDir(ws.installPath); err != nil {
		logMessage(fmt.Sprintf("[ApplyUpdate] Step 2: Warning — failed to disable directory protection on %s: %v", ws.installPath, err))
	}
	if err := protMgr.DisableProtectionForFile(request.TargetPath); err != nil {
		logMessage(fmt.Sprintf("[ApplyUpdate] Step 2: Warning — failed to disable file protection on %s: %v", request.TargetPath, err))
	}
	logMessage(fmt.Sprintf("[ApplyUpdate] Step 2/10: Protection disabled in %v", time.Since(stepStart).Round(time.Millisecond)))

	// Step 3: Stop the agent service (releases file lock)
	stepStart = time.Now()
	logMessage(fmt.Sprintf("[ApplyUpdate] Step 3/10: Stopping agent service %q ...", ws.config.AgentService))
	if err := ws.stopAgentService(); err != nil {
		logMessage(fmt.Sprintf("[ApplyUpdate] Step 3 FAILED in %v", time.Since(stepStart)))
		ws.failUpdate(status, fmt.Sprintf("failed to stop agent: %v", err))
		return
	}
	logMessage(fmt.Sprintf("[ApplyUpdate] Step 3/10: Agent service stopped in %v", time.Since(stepStart).Round(time.Millisecond)))

	// Step 4: Create backup of current binary (after service stopped, file unlocked)
	stepStart = time.Now()
	backupPath, err := ws.createBackup(request.TargetPath)
	if err != nil {
		logMessage(fmt.Sprintf("[ApplyUpdate] Step 4 FAILED in %v", time.Since(stepStart)))
		ws.failUpdate(status, fmt.Sprintf("failed to create backup: %v", err))
		return
	}
	status.BackupPath = backupPath
	logMessage(fmt.Sprintf("[ApplyUpdate] Step 4/10: Backup created at %s in %v", backupPath, time.Since(stepStart).Round(time.Millisecond)))

	// Step 5: Replace the binary using atomic move
	stepStart = time.Now()
	logMessage(fmt.Sprintf("[ApplyUpdate] Step 5/10: Replacing %s with %s ...", request.TargetPath, request.StagedPath))
	if err := ws.atomicReplace(request.StagedPath, request.TargetPath); err != nil {
		logMessage(fmt.Sprintf("[ApplyUpdate] Step 5 FAILED in %v: %v — attempting rollback", time.Since(stepStart), err))
		ws.rollbackUpdate(backupPath, request.TargetPath, status)
		return
	}
	// Verify the new binary is in place
	if newInfo, statErr := os.Stat(request.TargetPath); statErr != nil {
		logMessage(fmt.Sprintf("[ApplyUpdate] Step 5 WARNING: new binary not stat-able after replace: %v", statErr))
	} else {
		logMessage(fmt.Sprintf("[ApplyUpdate] Step 5/10: Binary replaced in %v — new size=%d bytes", time.Since(stepStart).Round(time.Millisecond), newInfo.Size()))
	}

	// Step 6: Pause tamper monitoring during startup window
	ws.protMgr.PauseTamperMonitoring()
	logMessage("[ApplyUpdate] Step 6/10: Tamper monitoring paused")

	// Step 7: Start the agent service
	stepStart = time.Now()
	logMessage(fmt.Sprintf("[ApplyUpdate] Step 7/10: Starting agent service %q ...", ws.config.AgentService))
	if err := ws.startAgentService(); err != nil {
		ws.protMgr.ResumeTamperMonitoring()
		logMessage(fmt.Sprintf("[ApplyUpdate] Step 7 FAILED in %v: %v — attempting rollback", time.Since(stepStart), err))
		ws.rollbackUpdate(backupPath, request.TargetPath, status)
		return
	}
	logMessage(fmt.Sprintf("[ApplyUpdate] Step 7/10: Agent service started in %v", time.Since(stepStart).Round(time.Millisecond)))

	// Step 8: Verify the update succeeded (120s timeout for agent to write agent-info.json)
	stepStart = time.Now()
	logMessage(fmt.Sprintf("[ApplyUpdate] Step 8/10: Verifying agent reports version %s (timeout: %v) ...", request.Version, updateVerifyTimeout))
	if err := ws.verifyUpdate(request.Version); err != nil {
		ws.protMgr.ResumeTamperMonitoring()
		logMessage(fmt.Sprintf("[ApplyUpdate] Step 8 FAILED in %v: %v — attempting rollback", time.Since(stepStart), err))
		ws.rollbackUpdate(backupPath, request.TargetPath, status)
		return
	}
	logMessage(fmt.Sprintf("[ApplyUpdate] Step 8/10: Version verified in %v", time.Since(stepStart).Round(time.Millisecond)))

	// Step 9: Re-enable protection on the new file and directory (after verified running)
	stepStart = time.Now()
	if err := protMgr.EnableProtectionForFile(request.TargetPath); err != nil {
		logMessage(fmt.Sprintf("[ApplyUpdate] Step 9: Warning — failed to re-enable file protection: %v", err))
	}
	if err := protMgr.EnableProtectionForDir(ws.installPath); err != nil {
		logMessage(fmt.Sprintf("[ApplyUpdate] Step 9: Warning — failed to re-enable directory protection: %v", err))
	}
	logMessage(fmt.Sprintf("[ApplyUpdate] Step 9/10: Protection re-enabled in %v", time.Since(stepStart).Round(time.Millisecond)))

	// Step 10: Resume tamper monitoring
	ws.protMgr.ResumeTamperMonitoring()
	logMessage("[ApplyUpdate] Step 10/10: Tamper monitoring resumed")

	// Success!
	status.State = ipc.StateComplete
	status.CompletedAt = time.Now()
	statusPath = ipc.UpdateStatusPath()
	if err := ipc.WriteUpdateStatus(status); err != nil {
		logMessage(fmt.Sprintf("[ApplyUpdate] WARNING: Failed to write 'complete' status to %s: %v", statusPath, err))
	} else {
		logMessage(fmt.Sprintf("[ApplyUpdate] Wrote 'complete' status to %s", statusPath))
	}

	// Clean up
	if err := ipc.DeleteUpdateRequest(); err != nil {
		logMessage(fmt.Sprintf("[ApplyUpdate] Warning: failed to delete update request: %v", err))
	}
	os.Remove(backupPath)
	ipc.CleanupStagingDir()

	totalDuration := time.Since(updateStart).Round(time.Millisecond)
	logMessage(fmt.Sprintf("[ApplyUpdate] === SUCCESS === Update to v%s completed in %v", request.Version, totalDuration))
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

	// Reject requests with empty checksum — all updates must have integrity verification
	if request.Checksum == "" {
		return fmt.Errorf("update request missing checksum — rejecting for security")
	}

	// Verify checksum matches staged file
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

	return nil
}

// createBackup creates a backup of the current binary with integrity verification.
// After copying, it flushes to disk and verifies SHA256 checksums match to prevent
// corrupted backups (e.g., from power loss between copy and flush).
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

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(backupPath)
		return "", fmt.Errorf("failed to copy to backup: %w", err)
	}

	// Force flush to disk before closing to prevent corruption on power loss
	if err := dst.Sync(); err != nil {
		dst.Close()
		os.Remove(backupPath)
		return "", fmt.Errorf("failed to sync backup to disk: %w", err)
	}
	dst.Close()
	src.Close()

	// Verify integrity: compute SHA256 of both source and backup, compare
	srcChecksum, err := fileSHA256(targetPath)
	if err != nil {
		os.Remove(backupPath)
		return "", fmt.Errorf("failed to hash source file: %w", err)
	}

	dstChecksum, err := fileSHA256(backupPath)
	if err != nil {
		os.Remove(backupPath)
		return "", fmt.Errorf("failed to hash backup file: %w", err)
	}

	if srcChecksum != dstChecksum {
		os.Remove(backupPath)
		return "", fmt.Errorf("backup integrity check failed: source=%s backup=%s", srcChecksum, dstChecksum)
	}

	logMessage(fmt.Sprintf("[Backup] Integrity verified: %s (SHA256: %s)", backupPath, srcChecksum))
	return backupPath, nil
}

// fileSHA256 computes the SHA256 hex digest of a file
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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

	// Wait for service to stop (60s to allow for graceful shutdown)
	deadline := time.Now().Add(60 * time.Second)
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

	// Wait for service to start (60s to allow for slow startup/initialization)
	deadline := time.Now().Add(60 * time.Second)
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

	// Write alert file for agent to relay to server
	if err := ipc.WriteAlert(&ipc.AlertRelayPayload{
		Severity: "critical",
		Title:    "Agent Update Rolled Back",
		Message:  fmt.Sprintf("Update to v%s was rolled back: %s", status.Version, status.Error),
	}); err != nil {
		logMessage(fmt.Sprintf("Warning: failed to write rollback alert file: %v", err))
	}

	// Clean up request file to prevent retry loops
	ipc.DeleteUpdateRequest()

	logMessage("Rollback completed")
}

// failUpdate marks the update as failed and cleans up
func (ws *watchdogService) failUpdate(status *ipc.UpdateStatus, reason string) {
	logMessage(fmt.Sprintf("Update failed: %s", reason))
	status.State = ipc.StateFailed
	status.Error = reason
	status.CompletedAt = time.Now()
	ipc.WriteUpdateStatus(status)

	// Write alert file for agent to relay to server
	if err := ipc.WriteAlert(&ipc.AlertRelayPayload{
		Severity: "critical",
		Title:    "Agent Update Failed",
		Message:  fmt.Sprintf("Update to v%s failed: %s", status.Version, reason),
	}); err != nil {
		logMessage(fmt.Sprintf("Warning: failed to write alert file: %v", err))
	}

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

// ============================================================================
// Independent HTTP Polling (Layer 1 - Resilient Updates)
// ============================================================================
// This polling mechanism operates completely independent of the agent.
// Even if the agent is completely broken or unable to communicate,
// the watchdog can still discover and apply updates via direct HTTP.

// ServerVersionResponse matches the server's AgentUpdateResponse
type ServerVersionResponse struct {
	Available      bool               `json:"available"`
	CurrentVersion string             `json:"currentVersion"`
	LatestVersion  string             `json:"latestVersion"`
	VersionInfo    *ServerVersionInfo `json:"versionInfo,omitempty"`
}

// ServerVersionInfo matches the server's AgentVersionInfo
type ServerVersionInfo struct {
	Version     string `json:"version"`
	Platform    string `json:"platform"`
	Arch        string `json:"arch"`
	DownloadURL string `json:"downloadUrl"`
	Checksum    string `json:"checksum"`
	Size        int64  `json:"size"`
	ReleaseDate string `json:"releaseDate"`
	Changelog   string `json:"changelog"`
	Required    bool   `json:"required"`
}

// independentUpdatePoller runs a background loop that polls the server for updates
// independent of the agent's WebSocket connection
func (ws *watchdogService) independentUpdatePoller() {
	// Initial delay to let everything stabilize
	time.Sleep(30 * time.Second)

	// Get server URL from config or use default
	serverURL := ws.getServerURL()
	if serverURL == "" {
		logMessage("[IndependentPoll] No server URL configured, independent polling disabled")
		return
	}

	logMessage(fmt.Sprintf("[IndependentPoll] Starting independent update poller (interval: %v, server: %s)", independentPollInterval, serverURL))

	currentBackoff := independentPollInterval
	consecutiveFailures := 0

	for {
		select {
		case <-ws.stopChan:
			logMessage("[IndependentPoll] Stopping independent update poller")
			return
		case <-time.After(currentBackoff):
			// Check if there's already an update in progress
			ws.mu.Lock()
			inProgress := ws.updateInProgress || ws.selfUpdateInProgress
			ws.mu.Unlock()

			if inProgress {
				logMessage("[IndependentPoll] Skipping poll - update already in progress")
				continue
			}

			// Also skip if there's already a pending update request from the agent
			existingRequest, _ := ipc.ReadUpdateRequest()
			if existingRequest != nil {
				logMessage("[IndependentPoll] Skipping poll - pending update request exists")
				continue
			}

			// Poll the server
			err := ws.pollServerForUpdates(serverURL)
			if err != nil {
				consecutiveFailures++
				// Exponential backoff on failures, capped at max
				currentBackoff = time.Duration(float64(independentPollInterval) * float64(consecutiveFailures))
				if currentBackoff > independentPollMaxBackoff {
					currentBackoff = independentPollMaxBackoff
				}
				logMessage(fmt.Sprintf("[IndependentPoll] Poll failed: %v (next retry in %v)", err, currentBackoff))
			} else {
				consecutiveFailures = 0
				currentBackoff = independentPollInterval
			}
		}
	}
}

// getServerURL returns the server URL from config or tries to read from agent config
func (ws *watchdogService) getServerURL() string {
	// First check watchdog config
	if ws.config.ServerURL != "" {
		return ws.config.ServerURL
	}

	// Try to read from agent's config file
	agentConfigPath := filepath.Join(ws.installPath, "agent-config.json")
	data, err := os.ReadFile(agentConfigPath)
	if err == nil {
		var agentConfig struct {
			Server string `json:"server"`
		}
		if json.Unmarshal(data, &agentConfig) == nil && agentConfig.Server != "" {
			return agentConfig.Server
		}
	}

	// Try reading from agent state file
	statePath := filepath.Join(ipc.BaseDir, "agent-state.json")
	data, err = os.ReadFile(statePath)
	if err == nil {
		var state struct {
			ServerURL string `json:"serverUrl"`
		}
		if json.Unmarshal(data, &state) == nil && state.ServerURL != "" {
			return state.ServerURL
		}
	}

	// Fallback to default (the production server - standard HTTPS through Cloudflare)
	return "https://sentinelrmm.us"
}

// getEnrollmentToken reads the enrollment token from the agent's config file.
// Used to authenticate update download and status report requests (C-02 Phase 2).
func (ws *watchdogService) getEnrollmentToken() string {
	agentConfigPath := filepath.Join(ws.installPath, "agent-config.json")
	data, err := os.ReadFile(agentConfigPath)
	if err != nil {
		return ""
	}
	var agentConfig struct {
		EnrollmentToken string `json:"enrollment_token"`
	}
	if json.Unmarshal(data, &agentConfig) == nil {
		return agentConfig.EnrollmentToken
	}
	return ""
}

// buildSecureTLSConfig creates a TLS configuration with proper certificate verification.
// It attempts to load a CA certificate from standard locations. If no custom CA cert
// is found, it falls back to the system root CA pool (which handles Let's Encrypt
// and other public CAs). InsecureSkipVerify is NEVER set to true.
func buildSecureTLSConfig() *tls.Config {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Determine config directory from executable location
	exePath, err := os.Executable()
	if err != nil {
		// Use system roots (default when RootCAs is nil)
		return tlsConfig
	}
	configDir := filepath.Dir(exePath)

	// Try to load CA cert from standard locations
	caCertPaths := []string{
		filepath.Join(configDir, "certs", "ca.crt"),
		filepath.Join(configDir, "certs", "ca-cert.pem"),
		`C:\ProgramData\Sentinel\certs\ca.crt`,
		`C:\ProgramData\Sentinel\certs\ca-cert.pem`,
	}

	for _, path := range caCertPaths {
		if caCert, err := os.ReadFile(path); err == nil {
			caCertPool := x509.NewCertPool()
			if caCertPool.AppendCertsFromPEM(caCert) {
				tlsConfig.RootCAs = caCertPool
				logMessage(fmt.Sprintf("[TLS] Loaded CA certificate from %s", path))
				break
			}
		}
	}

	// If no CA cert found, use system roots (DO NOT skip verification)
	// System root CAs handle Let's Encrypt and other public CAs
	if tlsConfig.RootCAs == nil {
		logMessage("[TLS] No custom CA certificate found, using system root CAs")
	}

	return tlsConfig
}

// pollServerForUpdates checks the server for available updates
func (ws *watchdogService) pollServerForUpdates(serverURL string) error {
	// Get current agent version from agent-info.json
	currentVersion := ""
	agentInfo, _ := ipc.ReadAgentInfo()
	if agentInfo != nil {
		currentVersion = agentInfo.Version
	}

	// If no agent info, try reading from the binary version
	if currentVersion == "" {
		// Use watchdog's version as a fallback - they should be in sync
		currentVersion = Version
	}

	// Build version check URL
	versionURL := fmt.Sprintf("%s/api/agent/version?platform=windows&arch=amd64&current=%s", serverURL, currentVersion)

	logMessage(fmt.Sprintf("[IndependentPoll] Checking for updates (current: %s)", currentVersion))

	// Create HTTP client with proper TLS verification
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: buildSecureTLSConfig(),
		},
	}

	req, err := http.NewRequest("GET", versionURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create version check request: %w", err)
	}
	if token := ws.getEnrollmentToken(); token != "" {
		req.Header.Set("X-Enrollment-Token", token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var versionResp ServerVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&versionResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !versionResp.Available {
		logMessage(fmt.Sprintf("[IndependentPoll] No update available (current: %s, latest: %s)", currentVersion, versionResp.LatestVersion))
		return nil
	}

	// Update is available!
	logMessage(fmt.Sprintf("[IndependentPoll] Update available: %s -> %s", currentVersion, versionResp.LatestVersion))

	// Download and stage the update
	if versionResp.VersionInfo == nil {
		return fmt.Errorf("version info missing from response")
	}

	return ws.downloadAndStageUpdate(versionResp.VersionInfo, serverURL)
}

// downloadAndStageUpdate downloads the update binary and creates an update request
func (ws *watchdogService) downloadAndStageUpdate(info *ServerVersionInfo, serverURL string) error {
	// Ensure staging directory exists
	if err := ipc.EnsureDirectories(); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Determine download URL
	downloadURL := info.DownloadURL
	if downloadURL == "" {
		downloadURL = fmt.Sprintf("%s/api/agent/update/download?platform=windows&arch=amd64", serverURL)
	}

	logMessage(fmt.Sprintf("[IndependentPoll] Downloading update from %s", downloadURL))

	// Create HTTP client for download with proper TLS verification
	client := &http.Client{
		Timeout: independentDownloadTimeout,
		Transport: &http.Transport{
			TLSClientConfig: buildSecureTLSConfig(),
		},
	}

	dlReq, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}
	if token := ws.getEnrollmentToken(); token != "" {
		dlReq.Header.Set("X-Enrollment-Token", token)
	}

	resp, err := client.Do(dlReq)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Stage path
	stagedPath := ipc.StagingPath(info.Version, "windows", "amd64")

	// Create staging file
	f, err := os.Create(stagedPath)
	if err != nil {
		return fmt.Errorf("failed to create staging file: %w", err)
	}

	// Download with checksum verification
	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)

	written, err := io.Copy(writer, resp.Body)
	f.Close()

	if err != nil {
		os.Remove(stagedPath)
		return fmt.Errorf("download failed: %w", err)
	}

	// Verify size
	if info.Size > 0 && written != info.Size {
		os.Remove(stagedPath)
		return fmt.Errorf("size mismatch: expected %d, got %d", info.Size, written)
	}

	// Verify checksum if provided
	actualChecksum := hex.EncodeToString(hasher.Sum(nil))
	if info.Checksum != "" && actualChecksum != info.Checksum {
		os.Remove(stagedPath)
		return fmt.Errorf("checksum mismatch: expected %s, got %s", info.Checksum, actualChecksum)
	}

	logMessage(fmt.Sprintf("[IndependentPoll] Downloaded %d bytes, checksum: %s", written, actualChecksum))

	// Create update request for the existing update machinery
	targetPath := filepath.Join(ws.installPath, "sentinel-agent.exe")
	request := &ipc.UpdateRequest{
		Version:     info.Version,
		StagedPath:  stagedPath,
		Checksum:    actualChecksum,
		RequestedAt: time.Now(),
		RequestedBy: "watchdog-independent-poll",
		TargetPath:  targetPath,
	}

	if err := ipc.WriteUpdateRequest(request); err != nil {
		os.Remove(stagedPath)
		return fmt.Errorf("failed to write update request: %w", err)
	}

	logMessage(fmt.Sprintf("[IndependentPoll] Update staged and request written for version %s", info.Version))

	return nil
}
