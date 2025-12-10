package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

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
)

var (
	Version = "1.12.0"
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

	err = svc.Run(serviceName, &watchdogService{
		config:   loadConfig(installPath),
		stopChan: make(chan struct{}),
	})
	if err != nil {
		elog.Error(1, fmt.Sprintf("Service failed: %v", err))
	}
}

func runDebug(installPath string) {
	elog = debug.New(serviceName)
	defer elog.Close()

	log.Printf("Starting %s v%s in debug mode", serviceName, Version)

	ws := &watchdogService{
		config:   loadConfig(installPath),
		stopChan: make(chan struct{}),
	}

	// Run in foreground
	go ws.monitorAgent()

	// Wait for interrupt
	fmt.Println("Press Ctrl+C to stop...")
	select {}
}

// Execute implements svc.Handler
func (ws *watchdogService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}

	// Start the monitoring goroutine
	go ws.monitorAgent()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				elog.Info(1, "Received stop signal")
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

	// Run agent install command
	cmd := exec.Command(ws.config.AgentPath, "install")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("install failed: %v - %s", err, string(output))
	}

	// Start the service
	cmd = exec.Command(ws.config.AgentPath, "start")
	return cmd.Run()
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
		Dependencies:     []string{agentServiceName}, // Start after agent
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
