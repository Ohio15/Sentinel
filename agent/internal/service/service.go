package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kardianos/service"
	"github.com/sentinel/agent/internal/ipc"
	"github.com/sentinel/agent/internal/paths"
	"github.com/sentinel/agent/internal/protection"
)

const (
	ServiceName        = "SentinelAgent"
	ServiceDisplayName = "Sentinel RMM Agent"
	ServiceDescription = "Sentinel Remote Monitoring and Management Agent"
)

// Program implements the service.Interface
type Program struct {
	start func() error
	stop  func() error
}

// Start is called when the service starts
func (p *Program) Start(s service.Service) error {
	log.Println("Service starting...")
	go p.run()
	return nil
}

func (p *Program) run() {
	if p.start != nil {
		if err := p.start(); err != nil {
			log.Printf("Start error: %v", err)
		}
	}
}

// Stop is called when the service stops
func (p *Program) Stop(s service.Service) error {
	log.Println("Service stopping...")
	if p.stop != nil {
		return p.stop()
	}
	return nil
}

// Config returns the service configuration
func Config() *service.Config {
	return &service.Config{
		Name:        ServiceName,
		DisplayName: ServiceDisplayName,
		Description: ServiceDescription,
		Option:      getServiceOptions(),
	}
}

func getServiceOptions() service.KeyValue {
	options := make(service.KeyValue)

	switch runtime.GOOS {
	case "windows":
		options["StartType"] = "automatic"
		options["OnFailure"] = "restart"
		options["OnFailureDelayDuration"] = "5s"
		options["OnFailureResetPeriod"] = 10
	case "linux":
		// systemd options
		options["SystemdScript"] = systemdScript
		options["Restart"] = "always"
		options["RestartSec"] = "5"
	case "darwin":
		options["KeepAlive"] = true
		options["RunAtLoad"] = true
	}

	return options
}

// New creates a new service
func New(startFn, stopFn func() error) (service.Service, error) {
	prg := &Program{
		start: startFn,
		stop:  stopFn,
	}

	return service.New(prg, Config())
}

// Install installs the service
func Install(serverURL, token string) error {
	svc, err := New(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	// Get executable path
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	installPath := filepath.Dir(exe)

	// Update config with arguments
	cfg := Config()
	cfg.Executable = exe
	cfg.Arguments = []string{
		"--server=" + serverURL,
		"--token=" + token,
		"--service",
	}

	svc, err = service.New(&Program{}, cfg)
	if err != nil {
		return fmt.Errorf("failed to create service with config: %w", err)
	}

	// Check if already installed
	status, err := svc.Status()
	if err == nil && status != service.StatusUnknown {
		// Service exists, stop and uninstall first
		log.Println("Service already installed, updating...")

		// Reset tamper protection DACLs before attempting service operations;
		// without this, stop/uninstall can silently fail on protected installs.
		resetTamperProtection(installPath)

		svc.Stop()
		waitForServiceStop(ServiceName)
		svc.Uninstall()
		waitForServiceDeletion(ServiceName)
	}

	// Install the service (retry if SCM hasn't fully released the name)
	var installErr error
	for i := 0; i < 5; i++ {
		if installErr = svc.Install(); installErr == nil {
			break
		}
		log.Printf("Service install attempt %d failed: %v, retrying...", i+1, installErr)
		time.Sleep(2 * time.Second)
	}
	if installErr != nil {
		return fmt.Errorf("failed to install service after retries: %w", installErr)
	}

	log.Println("Service installed successfully")
	
	// Configure with native SC commands for reliable startup/recovery
	configureServiceWithSC(ServiceName)

	// Start the service
	if err := svc.Start(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	log.Println("Service started successfully")

	// Install and start the watchdog service (Windows only)
	if runtime.GOOS == "windows" {
		installWatchdog(installPath)
	}

	return nil
}

// installWatchdog installs the watchdog service
func installWatchdog(installPath string) {
	watchdogPath := filepath.Join(installPath, "sentinel-watchdog.exe")
	if _, err := os.Stat(watchdogPath); os.IsNotExist(err) {
		log.Println("Watchdog executable not found, skipping watchdog installation")
		return
	}

	cfg := &service.Config{
		Name:        "SentinelWatchdog",
		DisplayName: "Sentinel Watchdog Service",
		Description: "Monitors and maintains Sentinel Agent availability",
		Executable:  watchdogPath,
		Option: service.KeyValue{
			"StartType":               "automatic",
			"OnFailure":               "restart",
			"OnFailureDelayDuration":  "5s",
			"OnFailureResetPeriod":    10,
		},
	}

	prg := &Program{}
	svc, err := service.New(prg, cfg)
	if err != nil {
		log.Printf("Warning: could not create watchdog service: %v", err)
		return
	}

	// Reset tamper protection DACLs before attempting service operations
	resetTamperProtection(installPath)

	// Stop and uninstall if already exists
	status, _ := svc.Status()
	if status == service.StatusRunning {
		svc.Stop()
		waitForServiceStop("SentinelWatchdog")
	}
	svc.Uninstall()
	waitForServiceDeletion("SentinelWatchdog")

	// Install with retry
	var installErr error
	for i := 0; i < 5; i++ {
		if installErr = svc.Install(); installErr == nil {
			break
		}
		log.Printf("Watchdog install attempt %d failed: %v, retrying...", i+1, installErr)
		time.Sleep(2 * time.Second)
	}
	if installErr != nil {
		log.Printf("Warning: could not install watchdog service after retries: %v", installErr)
		return
	}

	if err := svc.Start(); err != nil {
		log.Printf("Warning: could not start watchdog service: %v", err)
		return
	}

	log.Println("Watchdog service installed and started")
		configureServiceWithSC("SentinelWatchdog")
}

// Uninstall removes the service - requires server authorization
func Uninstall() error {
	return UninstallWithToken("", "", "")
}

// UninstallWithToken removes the service with server authorization
func UninstallWithToken(serverURL, deviceID, uninstallToken string) error {
	// Get install path
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	installPath := filepath.Dir(exe)

	// If no token provided, try to get one from server
	if uninstallToken == "" {
		// Try to load config to get server URL and device ID
		configPath := filepath.Join(installPath, "config.json")
		if data, err := os.ReadFile(configPath); err == nil {
			var cfg struct {
				ServerURL string `json:"serverUrl"`
				AgentID   string `json:"agentId"`
				DeviceID  string `json:"deviceId"`
			}
			if json.Unmarshal(data, &cfg) == nil {
				if serverURL == "" {
					serverURL = cfg.ServerURL
				}
				if deviceID == "" {
					deviceID = cfg.DeviceID
					if deviceID == "" {
						deviceID = cfg.AgentID
					}
				}
			}
		}

		if serverURL != "" && deviceID != "" {
			token, err := requestUninstallToken(serverURL, deviceID)
			if err != nil {
				log.Printf("Warning: Could not get uninstall token from server: %v", err)
				log.Println("Proceeding with local uninstall (protections may prevent this)")
			} else {
				uninstallToken = token
			}
		}
	}

	// If a token is provided from the server, treat it as authorized
	// (the server has already authenticated the admin user)
	// Only disable protections for legitimate uninstall
	if uninstallToken != "" {
		log.Printf("Server-authorized uninstall with token: %s...", uninstallToken[:8])

		// Reset DACLs at the filesystem level first, then disable protection state.
		// DisableProtections only does icacls /reset; we also need Administrators:F
		// and protection.dat removal to fully clear tamper protection.
		resetTamperProtection(installPath)

		protMgr := protection.NewManager(installPath, ServiceName)
		if err := protMgr.DisableProtections(); err != nil {
			log.Printf("Warning: could not disable protections: %v", err)
		}
	}

	// CRITICAL: Stop the watchdog FIRST before stopping the main agent
	// Otherwise the watchdog will restart the agent immediately
	log.Println("Stopping watchdog service first...")
	stopWatchdog()

	// Poll for watchdog to fully stop (up to 10 seconds)
	if err := waitForWatchdogStopped(10*time.Second, 500*time.Millisecond); err != nil {
		log.Printf("Warning: watchdog may not be fully stopped: %v", err)
	}

	svc, err := New(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	// Uninstall the service directly - this will stop it as part of the uninstall process
	// Do NOT call svc.Stop() first, as that causes the current process to exit
	// before Uninstall() can complete when running from within the service
	log.Println("Uninstalling main agent service...")
	if err := svc.Uninstall(); err != nil {
		return fmt.Errorf("failed to uninstall service: %w", err)
	}

	log.Println("Service uninstalled successfully")

	// Best-effort cleanup of sensitive files
	cleanupSensitiveFiles()

	// Best-effort removal of Windows Defender exclusions (I-10)
	removeDefenderExclusions(installPath)

	return nil
}

// requestUninstallToken requests an uninstall token from the server
func requestUninstallToken(serverURL, deviceID string) (string, error) {
	payload := map[string]string{"deviceId": deviceID}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(
		serverURL+"/api/agent/request-uninstall-token",
		"application/json",
		bytes.NewReader(jsonData),
	)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.Token, nil
}

// stopWatchdog stops the watchdog service if running
func stopWatchdog() {
	cfg := &service.Config{
		Name:        "SentinelWatchdog",
		DisplayName: "Sentinel Watchdog Service",
		Description: "Monitors Sentinel Agent",
	}
	prg := &Program{}
	svc, err := service.New(prg, cfg)
	if err != nil {
		return
	}

	status, err := svc.Status()
	if err == nil && status == service.StatusRunning {
		svc.Stop()
	}
	svc.Uninstall()
}

// cleanupSensitiveFiles removes sensitive credential and state files after uninstall.
// The logs/ directory is intentionally preserved for post-uninstall inspection.
// This is best-effort: errors are logged but do not fail the uninstall.
func cleanupSensitiveFiles() {
	dataDir := paths.DataDir()
	log.Printf("Cleaning up sensitive files in %s...", dataDir)

	// Individual sensitive files to remove
	sensitiveFiles := []string{
		paths.ConfigPath(),                                   // config.json (encrypted enrollment config)
		filepath.Join(dataDir, "ipc-key.dat"),                // HMAC signing key
		filepath.Join(dataDir, ipc.UpdateRequestFile),        // update-request.json
		filepath.Join(dataDir, ipc.UpdateStatusFile),         // update-status.json
		filepath.Join(dataDir, ipc.AgentInfoFile),            // agent-info.json
		filepath.Join(dataDir, ipc.WatchdogUpdateRequestFile), // watchdog-update-request.json
		filepath.Join(dataDir, ipc.WatchdogUpdateStatusFile), // watchdog-update-status.json
		filepath.Join(dataDir, ipc.WatchdogInfoFile),         // watchdog-info.json
		filepath.Join(dataDir, "pending-alert.json"),         // AlertFile
		paths.AgentInfoPath(),                                // agent-info.json (paths version)
	}

	for _, f := range sensitiveFiles {
		if err := os.Remove(f); err != nil {
			if !os.IsNotExist(err) {
				log.Printf("Warning: failed to remove %s: %v", f, err)
			}
		} else {
			log.Printf("Removed: %s", f)
		}
	}

	// Remove .sig files (HMAC signatures)
	sigFiles, err := filepath.Glob(filepath.Join(dataDir, "*.sig"))
	if err == nil {
		for _, f := range sigFiles {
			if err := os.Remove(f); err != nil {
				if !os.IsNotExist(err) {
					log.Printf("Warning: failed to remove signature file %s: %v", f, err)
				}
			} else {
				log.Printf("Removed: %s", f)
			}
		}
	}

	// Remove certs/ directory recursively (mTLS certificates and private keys)
	certsDir := paths.CertsDir()
	if err := os.RemoveAll(certsDir); err != nil {
		log.Printf("Warning: failed to remove certs directory %s: %v", certsDir, err)
	} else {
		log.Printf("Removed directory: %s", certsDir)
	}

	// Remove update/ directory recursively (staging area)
	updateDir := paths.UpdateDir()
	if err := os.RemoveAll(updateDir); err != nil {
		log.Printf("Warning: failed to remove update directory %s: %v", updateDir, err)
	} else {
		log.Printf("Removed directory: %s", updateDir)
	}

	log.Println("Sensitive file cleanup complete (logs/ preserved)")
}

// Start starts the service
func Start() error {
	svc, err := New(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	return svc.Start()
}

// Stop stops the service
func Stop() error {
	svc, err := New(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	return svc.Stop()
}

// Status returns the service status
func Status() (string, error) {
	svc, err := New(nil, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create service: %w", err)
	}

	status, err := svc.Status()
	if err != nil {
		return "", err
	}

	switch status {
	case service.StatusRunning:
		return "running", nil
	case service.StatusStopped:
		return "stopped", nil
	default:
		return "unknown", nil
	}
}

// IsElevated checks if the process has administrator/root privileges
func IsElevated() bool {
	switch runtime.GOOS {
	case "windows":
		return isWindowsAdmin()
	default:
		return os.Geteuid() == 0
	}
}


// waitForServiceStop waits for a Windows service to fully stop
func waitForServiceStop(serviceName string) {
	if runtime.GOOS != "windows" {
		return
	}
	for i := 0; i < 15; i++ {
		out, err := exec.Command("sc", "query", serviceName).CombinedOutput()
		if err != nil {
			return // Service doesn't exist or can't be queried
		}
		if bytes.Contains(out, []byte("STOPPED")) {
			return
		}
		log.Printf("Waiting for %s to stop... (%d/15)", serviceName, i+1)
		time.Sleep(time.Second)
	}
	// Force kill if still not stopped
	log.Printf("Force stopping %s", serviceName)
	exec.Command("taskkill", "/F", "/FI", "SERVICES eq "+serviceName).Run()
	time.Sleep(2 * time.Second)
}

// waitForServiceDeletion waits for Windows SCM to fully release a deleted service
func waitForServiceDeletion(serviceName string) {
	if runtime.GOOS != "windows" {
		return
	}
	for i := 0; i < 15; i++ {
		out, err := exec.Command("sc", "query", serviceName).CombinedOutput()
		if err != nil || bytes.Contains(out, []byte("FAILED 1060")) || bytes.Contains(out, []byte("does not exist")) {
			return // Service fully deleted
		}
		log.Printf("Waiting for %s to be deleted from SCM... (%d/15)", serviceName, i+1)
		time.Sleep(time.Second)
	}
	log.Printf("Warning: %s may not have been fully deleted from SCM", serviceName)
}

// configureServiceWithSC uses native Windows SC commands to ensure proper service configuration
func configureServiceWithSC(serviceName string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	
	// Set start type to automatic
	cmd := exec.Command("sc", "config", serviceName, "start=", "auto")
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: sc config start=auto failed: %v", err)
	}
	
	// Configure failure recovery: restart after 5s, 10s, 30s
	cmd = exec.Command("sc", "failure", serviceName, 
		"reset=", "86400",
		"actions=", "restart/5000/restart/10000/restart/30000")
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: sc failure config failed: %v", err)
	}
	
	// Enable recovery on non-crash failures (exit code != 0)
	cmd = exec.Command("sc", "failureflag", serviceName, "1")
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: sc failureflag failed: %v", err)
	}
	
	log.Printf("Service %s configured with automatic start and failure recovery", serviceName)
	return nil
}

// Linux systemd unit file template
const systemdScript = `[Unit]
Description={{.Description}}
After=network.target
Wants=network.target

[Service]
Type=simple
ExecStart={{.Path}} {{.Arguments}}
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier={{.Name}}

[Install]
WantedBy=multi-user.target
`
