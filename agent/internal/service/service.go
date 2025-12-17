package service

import (
	"os/exec"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kardianos/service"
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
		svc.Stop()
		svc.Uninstall()
	}

	// Install the service
	if err := svc.Install(); err != nil {
		return fmt.Errorf("failed to install service: %w", err)
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

	// Stop and uninstall if already exists
	status, _ := svc.Status()
	if status == service.StatusRunning {
		svc.Stop()
	}
	svc.Uninstall()

	if err := svc.Install(); err != nil {
		log.Printf("Warning: could not install watchdog service: %v", err)
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
		protMgr := protection.NewManager(installPath, ServiceName)
		if err := protMgr.DisableProtections(); err != nil {
			log.Printf("Warning: could not disable protections: %v", err)
		}
	}

	svc, err := New(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	// Stop the service first
	status, err := svc.Status()
	if err == nil && status == service.StatusRunning {
		if err := svc.Stop(); err != nil {
			log.Printf("Warning: failed to stop service: %v", err)
		}
	}

	// Also stop the watchdog if running
	stopWatchdog()

	// Uninstall the service
	if err := svc.Uninstall(); err != nil {
		return fmt.Errorf("failed to uninstall service: %w", err)
	}

	log.Println("Service uninstalled successfully")
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
