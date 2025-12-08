package service

import (
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/kardianos/service"
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

	// Start the service
	if err := svc.Start(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	log.Println("Service started successfully")
	return nil
}

// Uninstall removes the service
func Uninstall() error {
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

	// Uninstall the service
	if err := svc.Uninstall(); err != nil {
		return fmt.Errorf("failed to uninstall service: %w", err)
	}

	log.Println("Service uninstalled successfully")
	return nil
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
