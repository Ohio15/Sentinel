package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/sentinel/agent/internal/client"
	"github.com/sentinel/agent/internal/collector"
	"github.com/sentinel/agent/internal/config"
	"github.com/sentinel/agent/internal/executor"
	"github.com/sentinel/agent/internal/filetransfer"
	svc "github.com/sentinel/agent/internal/service"
	"github.com/sentinel/agent/internal/terminal"
	"github.com/sentinel/agent/internal/updater"
)

const (
	Version = "1.0.0"
)

var (
	serverURL   = flag.String("server", "", "Sentinel server URL (e.g., http://192.168.1.100:8080)")
	token       = flag.String("token", "", "Enrollment token")
	installFlag = flag.Bool("install", false, "Install as system service")
	uninstall   = flag.Bool("uninstall", false, "Uninstall the system service")
	runService  = flag.Bool("service", false, "Run as a service (internal)")
	showVersion = flag.Bool("version", false, "Show version information")
	showStatus  = flag.Bool("status", false, "Show service status")
)

// Agent represents the main agent application
type Agent struct {
	cfg             *config.Config
	client          *client.Client
	collector       *collector.Collector
	executor        *executor.Executor
	terminalManager *terminal.Manager
	fileTransfer    *filetransfer.FileTransfer
	updater         *updater.Updater
	ctx             context.Context
	cancel          context.CancelFunc
}

func main() {
	flag.Parse()

	// Set up logging
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if *showVersion {
		fmt.Printf("Sentinel Agent v%s\n", Version)
		fmt.Printf("OS: %s\n", runtime.GOOS)
		fmt.Printf("Arch: %s\n", runtime.GOARCH)
		os.Exit(0)
	}

	if *showStatus {
		status, err := svc.Status()
		if err != nil {
			fmt.Printf("Service status: unknown (%v)\n", err)
		} else {
			fmt.Printf("Service status: %s\n", status)
		}
		os.Exit(0)
	}

	// Check for embedded configuration (set at download time)
	embeddedServer, embeddedToken, hasEmbedded := config.GetEmbeddedConfig()

	// If we have embedded config and no flags provided, auto-install
	if hasEmbedded && !*installFlag && *serverURL == "" && *token == "" {
		fmt.Println("============================================")
		fmt.Println("  Sentinel Agent - Auto-Installing...")
		fmt.Println("============================================")
		fmt.Println()
		fmt.Printf("Server: %s\n", embeddedServer)
		fmt.Println()

		if !svc.IsElevated() {
			fmt.Println("ERROR: Administrator privileges required!")
			fmt.Println()
			fmt.Println("Please right-click the agent and select")
			fmt.Println("'Run as administrator' to install.")
			fmt.Println()
			fmt.Println("Press Enter to exit...")
			fmt.Scanln()
			os.Exit(1)
		}

		// Save configuration
		cfg := config.DefaultConfig()
		cfg.ServerURL = embeddedServer
		cfg.EnrollmentToken = embeddedToken
		if err := cfg.Save(); err != nil {
			fmt.Printf("Error saving configuration: %v\n", err)
			fmt.Println()
			fmt.Println("Press Enter to exit...")
			fmt.Scanln()
			os.Exit(1)
		}

		if err := svc.Install(embeddedServer, embeddedToken); err != nil {
			fmt.Printf("Error installing service: %v\n", err)
			fmt.Println()
			fmt.Println("Press Enter to exit...")
			fmt.Scanln()
			os.Exit(1)
		}
		fmt.Println("Sentinel Agent installed successfully!")
		fmt.Println()
		fmt.Println("The agent is now running as a service and will")
		fmt.Println("automatically start when Windows boots.")
		fmt.Println()
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		os.Exit(0)
	}

	if *installFlag {
		// Use embedded config if no command line args provided
		if *serverURL == "" && hasEmbedded {
			*serverURL = embeddedServer
		}
		if *token == "" && hasEmbedded {
			*token = embeddedToken
		}

		if *serverURL == "" || *token == "" {
			fmt.Println("Error: --server and --token are required for installation")
			fmt.Println("Usage: sentinel-agent --install --server=http://server:8080 --token=<enrollment-token>")
			os.Exit(1)
		}

		if !svc.IsElevated() {
			fmt.Println("Error: Administrator/root privileges required for installation")
			os.Exit(1)
		}

		// Save configuration
		cfg := config.DefaultConfig()
		cfg.ServerURL = *serverURL
		cfg.EnrollmentToken = *token
		if err := cfg.Save(); err != nil {
			fmt.Printf("Error saving configuration: %v\n", err)
			os.Exit(1)
		}

		if err := svc.Install(*serverURL, *token); err != nil {
			fmt.Printf("Error installing service: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Sentinel Agent installed and started successfully")
		os.Exit(0)
	}

	if *uninstall {
		if !svc.IsElevated() {
			fmt.Println("Error: Administrator/root privileges required for uninstallation")
			os.Exit(1)
		}

		if err := svc.Uninstall(); err != nil {
			fmt.Printf("Error uninstalling service: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Sentinel Agent uninstalled successfully")
		os.Exit(0)
	}

	// Load or create configuration
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Warning: Could not load config, using defaults: %v", err)
		cfg = config.DefaultConfig()
	}

	// Override with command line arguments
	if *serverURL != "" {
		cfg.ServerURL = *serverURL
	}
	if *token != "" {
		cfg.EnrollmentToken = *token
	}

	// Validate configuration
	if cfg.ServerURL == "" {
		fmt.Println("============================================")
		fmt.Println("  Sentinel Agent - Installation Required")
		fmt.Println("============================================")
		fmt.Println()
		fmt.Println("This agent must be installed with server details.")
		fmt.Println()
		fmt.Println("Run from an elevated command prompt:")
		fmt.Println()
		fmt.Println("  sentinel-agent.exe --install --server=http://SERVER:8080 --token=TOKEN")
		fmt.Println()
		fmt.Println("Get the server URL and token from the Sentinel dashboard.")
		fmt.Println()
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		os.Exit(1)
	}

	// Save updated configuration
	if err := cfg.Save(); err != nil {
		log.Printf("Warning: Could not save config: %v", err)
	}

	// Create agent
	agent := NewAgent(cfg)

	if *runService {
		// Running as a service
		s, err := svc.New(agent.Start, agent.Stop)
		if err != nil {
			log.Fatalf("Failed to create service: %v", err)
		}
		if err := s.Run(); err != nil {
			log.Fatalf("Service error: %v", err)
		}
	} else {
		// Running interactively
		if err := agent.Run(); err != nil {
			log.Fatalf("Agent error: %v", err)
		}
	}
}

// NewAgent creates a new agent instance
func NewAgent(cfg *config.Config) *Agent {
	ctx, cancel := context.WithCancel(context.Background())

	ft := filetransfer.New(nil)

	// Create updater for autonomous updates
	agentUpdater := updater.New(cfg.ServerURL, Version)
	agentUpdater.SetCheckInterval(1 * time.Hour) // Check for updates hourly

	return &Agent{
		cfg:             cfg,
		client:          client.New(cfg),
		collector:       collector.New(),
		executor:        executor.New(),
		terminalManager: terminal.NewManager(),
		fileTransfer:    ft,
		updater:         agentUpdater,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Run starts the agent in interactive mode
func (a *Agent) Run() error {
	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start the agent
	if err := a.Start(); err != nil {
		return err
	}

	// Wait for shutdown signal
	sig := <-sigChan
	log.Printf("Received signal %v, shutting down...", sig)

	return a.Stop()
}

// Start initializes and starts the agent
func (a *Agent) Start() error {
	log.Printf("Starting Sentinel Agent v%s", Version)
	log.Printf("Agent ID: %s", a.cfg.AgentID)
	log.Printf("Server: %s", a.cfg.ServerURL)

	// Register message handlers
	a.registerHandlers()

	// Set up connection callbacks
	a.client.OnConnect(a.onConnect)
	a.client.OnDisconnect(a.onDisconnect)

	// Enroll if not already enrolled
	if !a.cfg.Enrolled {
		if err := a.enroll(); err != nil {
			log.Printf("Enrollment failed: %v", err)
			// Continue anyway - will retry on connection
		}
	}

	// Start connection with automatic reconnection
	go a.client.RunWithReconnect(a.ctx)

	// Start heartbeat loop
	go a.heartbeatLoop()

	// Start metrics loop
	go a.metricsLoop()

	// Start update check loop
	go a.updater.RunUpdateLoop(a.ctx)

	return nil
}

// Stop gracefully shuts down the agent
func (a *Agent) Stop() error {
	log.Println("Stopping agent...")

	a.cancel()
	a.terminalManager.CloseAll()
	a.client.Close()

	log.Println("Agent stopped")
	return nil
}

func (a *Agent) registerHandlers() {
	a.client.RegisterHandler(client.MsgTypeHeartbeatAck, a.handleHeartbeatAck)
	a.client.RegisterHandler(client.MsgTypeExecuteCmd, a.handleExecuteCommand)
	a.client.RegisterHandler(client.MsgTypeExecuteScript, a.handleExecuteScript)
	a.client.RegisterHandler(client.MsgTypeStartTerminal, a.handleStartTerminal)
	a.client.RegisterHandler(client.MsgTypeTerminalInput, a.handleTerminalInput)
	a.client.RegisterHandler(client.MsgTypeTerminalResize, a.handleTerminalResize)
	a.client.RegisterHandler(client.MsgTypeCloseTerminal, a.handleCloseTerminal)
	a.client.RegisterHandler(client.MsgTypeListFiles, a.handleListFiles)
	a.client.RegisterHandler(client.MsgTypeDownloadFile, a.handleDownloadFile)
	a.client.RegisterHandler(client.MsgTypeUploadFile, a.handleUploadFile)
}

func (a *Agent) onConnect() {
	log.Println("Connected to server")
}

func (a *Agent) onDisconnect() {
	log.Println("Disconnected from server")
}

func (a *Agent) enroll() error {
	log.Println("Enrolling with server...")

	// Collect system info
	sysInfo, err := a.collector.GetSystemInfo()
	if err != nil {
		return fmt.Errorf("failed to collect system info: %w", err)
	}

	// Build enrollment payload with extended system info
	payload := map[string]interface{}{
		"agentId":        a.cfg.AgentID,
		"hostname":       sysInfo.Hostname,
		"osType":         sysInfo.OS,
		"osVersion":      sysInfo.OSVersion,
		"osBuild":        sysInfo.OSBuild,
		"platform":       sysInfo.Platform,
		"platformFamily": sysInfo.PlatformFamily,
		"architecture":   sysInfo.Architecture,
		"cpuModel":       sysInfo.CPUModel,
		"cpuCores":       sysInfo.CPUCores,
		"cpuThreads":     sysInfo.CPUThreads,
		"cpuSpeed":       sysInfo.CPUSpeed,
		"totalMemory":    sysInfo.TotalMemory,
		"bootTime":       sysInfo.BootTime,
		"gpu":            sysInfo.GPU,
		"storage":        sysInfo.Storage,
		"serialNumber":   sysInfo.SerialNumber,
		"manufacturer":   sysInfo.Manufacturer,
		"model":          sysInfo.Model,
		"domain":         sysInfo.Domain,
		"agentVersion":   Version,
	}

	// Send enrollment request
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	enrollURL := a.cfg.ServerURL + "/api/agent/enroll"

	httpReq, err := http.NewRequestWithContext(
		a.ctx,
		"POST",
		enrollURL,
		bytes.NewReader(jsonPayload),
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Enrollment-Token", a.cfg.EnrollmentToken)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("enrollment request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("enrollment failed with status: %d", resp.StatusCode)
	}

	var result struct {
		Success  bool   `json:"success"`
		DeviceID string `json:"deviceId"`
		Config   struct {
			HeartbeatInterval int `json:"heartbeatInterval"`
			MetricsInterval   int `json:"metricsInterval"`
		} `json:"config"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("enrollment failed")
	}

	// Update configuration
	a.cfg.SetEnrolled(result.DeviceID)
	if result.Config.HeartbeatInterval > 0 {
		a.cfg.HeartbeatInterval = result.Config.HeartbeatInterval
	}
	if result.Config.MetricsInterval > 0 {
		a.cfg.MetricsInterval = result.Config.MetricsInterval
	}
	a.cfg.Save()

	log.Printf("Enrolled successfully. Device ID: %s", result.DeviceID)
	return nil
}

func (a *Agent) heartbeatLoop() {
	interval := time.Duration(a.cfg.HeartbeatInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if a.client.IsConnected() && a.client.IsAuthenticated() {
				if err := a.client.SendHeartbeat(); err != nil {
					log.Printf("Failed to send heartbeat: %v", err)
				}
			}
		}
	}
}

func (a *Agent) metricsLoop() {
	interval := time.Duration(a.cfg.MetricsInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if a.client.IsConnected() && a.client.IsAuthenticated() {
				metrics, err := a.collector.Collect(a.ctx)
				if err != nil {
					log.Printf("Failed to collect metrics: %v", err)
					continue
				}
				if err := a.client.SendMetrics(metrics); err != nil {
					log.Printf("Failed to send metrics: %v", err)
				}
			}
		}
	}
}

// Message handlers

func (a *Agent) handleHeartbeatAck(msg *client.Message) error {
	// Heartbeat acknowledged
	return nil
}

func (a *Agent) handleExecuteCommand(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	command, _ := data["command"].(string)
	cmdType, _ := data["commandType"].(string)

	result, err := a.executor.Execute(a.ctx, command, cmdType)
	if err != nil {
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	return a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
		"output":   result.Stdout + result.Stderr,
		"exitCode": result.ExitCode,
		"duration": result.Duration,
	}, "")
}

func (a *Agent) handleExecuteScript(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	script, _ := data["script"].(string)
	language, _ := data["language"].(string)

	result, err := a.executor.ExecuteScript(a.ctx, script, language)
	if err != nil {
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	return a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
		"output":   result.Stdout + result.Stderr,
		"exitCode": result.ExitCode,
		"duration": result.Duration,
	}, "")
}

func (a *Agent) handleStartTerminal(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	sessionID, _ := data["sessionId"].(string)
	if sessionID == "" {
		return a.client.SendResponse(msg.RequestID, false, nil, "Session ID required")
	}

	onOutput := func(output string) {
		a.client.SendTerminalOutput(sessionID, output)
	}

	onClose := func() {
		log.Printf("Terminal session %s closed", sessionID)
	}

	_, err := a.terminalManager.CreateSession(a.ctx, sessionID, onOutput, onClose)
	if err != nil {
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	return a.client.SendResponse(msg.RequestID, true, nil, "")
}

func (a *Agent) handleTerminalInput(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid message data")
	}

	sessionID, _ := data["sessionId"].(string)
	input, _ := data["data"].(string)

	session, ok := a.terminalManager.GetSession(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	return session.Write(input)
}

func (a *Agent) handleTerminalResize(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid message data")
	}

	sessionID, _ := data["sessionId"].(string)
	cols, _ := data["cols"].(float64)
	rows, _ := data["rows"].(float64)

	session, ok := a.terminalManager.GetSession(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	return session.Resize(int(cols), int(rows))
}

func (a *Agent) handleCloseTerminal(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid message data")
	}

	sessionID, _ := data["sessionId"].(string)
	return a.terminalManager.CloseSession(sessionID)
}

func (a *Agent) handleListFiles(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	path, _ := data["path"].(string)

	files, err := a.fileTransfer.ListDirectory(path)
	if err != nil {
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	return a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
		"files": files,
	}, "")
}

func (a *Agent) handleDownloadFile(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	remotePath, _ := data["remotePath"].(string)

	// Stream file chunks to server
	err := a.fileTransfer.ReadFile(a.ctx, remotePath, func(chunk string, offset int64, total int64) error {
		return a.client.SendJSON(map[string]interface{}{
			"type":      client.MsgTypeFileData,
			"requestId": msg.RequestID,
			"chunk":     chunk,
			"offset":    offset,
			"total":     total,
		})
	})

	if err != nil {
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	return a.client.SendResponse(msg.RequestID, true, nil, "")
}

func (a *Agent) handleUploadFile(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	remotePath, _ := data["remotePath"].(string)
	fileData, _ := data["data"].(string)
	appendMode, _ := data["append"].(bool)

	err := a.fileTransfer.WriteFile(a.ctx, remotePath, fileData, appendMode)
	if err != nil {
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	return a.client.SendResponse(msg.RequestID, true, nil, "")
}
