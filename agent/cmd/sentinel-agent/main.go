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
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/sentinel/agent/internal/client"
	"github.com/sentinel/agent/internal/collector"
	"github.com/sentinel/agent/internal/config"
	"github.com/sentinel/agent/internal/desktop"
	"github.com/sentinel/agent/internal/diagnostics"
	"github.com/sentinel/agent/internal/executor"
	"github.com/sentinel/agent/internal/filetransfer"
	"github.com/sentinel/agent/internal/ipc"
	agentgrpc "github.com/sentinel/agent/internal/grpc"
	"github.com/sentinel/agent/internal/protection"
	"github.com/sentinel/agent/internal/remote"
	svc "github.com/sentinel/agent/internal/service"
	"github.com/sentinel/agent/internal/terminal"
	"github.com/sentinel/agent/internal/updater"
	"github.com/sentinel/agent/internal/webrtc"
	"github.com/sentinel/agent/internal/admin"
)

var Version = "1.51.0"

const ServiceName = "SentinelAgent"

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
	cfg               *config.Config
	client            *client.Client
	dataPlane         *agentgrpc.DataPlaneClient // gRPC Data Plane connection
	collector         *collector.Collector
	executor          *executor.Executor
	terminalManager   *terminal.Manager
	fileTransfer      *filetransfer.FileTransfer
	remoteManager     *remote.Manager
	webrtcManager     *webrtc.Manager // WebRTC for remote desktop (legacy)
	desktopManager    *desktop.Manager // Desktop helper manager for WebRTC
	updater           *updater.Updater
	protectionManager    *protection.Manager
	diagnosticsCollector *diagnostics.Collector
	adminManager         *admin.Manager
	tamperChan           chan string
	metricsIntervalChan  chan time.Duration // Channel for dynamic metrics interval changes
	ctx                  context.Context
	cancel               context.CancelFunc
}

func main() {
	flag.Parse()

	// Set up logging to file for debugging
	logFile, err := os.OpenFile("C:\\ProgramData\\Sentinel\\agent.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}
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

	// Create protection manager
	exePath, _ := os.Executable()
	installPath := filepath.Dir(exePath)
	protMgr := protection.NewManager(installPath, ServiceName)

	// Create Data Plane client (gRPC for metrics streaming)
	grpcAddr := cfg.GetGrpcAddress()
	var dataPlane *agentgrpc.DataPlaneClient
	if grpcAddr != "" {
		dataPlane = agentgrpc.NewDataPlaneClient(cfg.AgentID, grpcAddr)
	}

	return &Agent{
		cfg:                  cfg,
		client:               client.New(cfg, Version),
		dataPlane:            dataPlane,
		collector:            collector.New(),
		executor:             executor.New(),
		terminalManager:      terminal.NewManager(),
		fileTransfer:         ft,
		remoteManager:        remote.NewManager(),
		webrtcManager:        webrtc.NewManager(),
		desktopManager:       desktop.NewManager(""),
		updater:              agentUpdater,
		protectionManager:    protMgr,
		diagnosticsCollector: diagnostics.New(),
		adminManager:         admin.NewManager(Version),
		tamperChan:           make(chan string, 10),
		metricsIntervalChan:  make(chan time.Duration, 1),
		ctx:                  ctx,
		cancel:               cancel,
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

	// Write agent info for watchdog to verify updates
	a.writeAgentInfo()

	// Check for and report any completed update result
	a.updater.SetDeviceID(a.cfg.DeviceID)
	a.updater.CheckAndReportUpdateResult(a.ctx)

	// Enable protection mechanisms when running as service
	if protection.IsRunningAsService() {
		log.Println("Enabling protection mechanisms...")
		if err := a.protectionManager.EnableAllProtections(); err != nil {
			log.Printf("Warning: Some protections could not be enabled: %v", err)
		}

		// Start tamper monitoring
		go a.protectionManager.MonitorTamperAttempts(a.tamperChan)
		go a.handleTamperReports()
	}

	// Register message handlers
	a.registerHandlers()

	// Set up desktop manager callbacks for WebRTC signaling
	a.desktopManager.SetCallbacks(
		nil, // onSessionAnswer not needed, we use synchronous return
		func(sessionID uint32, connectionID, candidate, sdpMid string, sdpMLineIndex *int) {
			// Forward ICE candidate to server
			a.client.SendWebRTCSignal(connectionID, "candidate", "", candidate)
		},
		func(sessionID uint32, state desktop.HelperState, message, connectionID string) {
			log.Printf("[Desktop] Status update: sessionID=%d, state=%s, message=%s", sessionID, state, message)
		},
	)

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

	// Start Data Plane (gRPC) connection in parallel
	// This is optional - metrics will fallback to WebSocket if gRPC is unavailable
	if a.dataPlane != nil {
		grpcAddr := a.cfg.GetGrpcAddress()
		log.Printf("Starting gRPC Data Plane connection to %s", grpcAddr)
		go a.dataPlane.RunWithReconnect(a.ctx)
	}

	// Start heartbeat loop
	go a.heartbeatLoop()

	// Start metrics loop
	go a.metricsLoop()

	// Start update check loop
	go a.updater.RunUpdateLoop(a.ctx)

	return nil
}

// writeAgentInfo writes the agent's version info for watchdog verification
func (a *Agent) writeAgentInfo() {
	info := &ipc.AgentInfo{
		Version:   Version,
		StartedAt: time.Now(),
		PID:       os.Getpid(),
		AgentID:   a.cfg.AgentID,
	}

	if err := ipc.WriteAgentInfo(info); err != nil {
		log.Printf("Warning: failed to write agent info: %v", err)
	} else {
		log.Printf("Agent info written: version=%s pid=%d", Version, info.PID)
	}
}

// Stop gracefully shuts down the agent
func (a *Agent) Stop() error {
	log.Println("Stopping agent...")

	a.cancel()

	// Shutdown desktop manager (kills helper processes)
	if a.desktopManager != nil {
		a.desktopManager.Shutdown()
	}
	a.terminalManager.CloseAll()

	// Stop Data Plane (gRPC) connection
	if a.dataPlane != nil {
		a.dataPlane.Stop()
	}

	// Stop Control Plane (WebSocket) connection
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
	a.client.RegisterHandler(client.MsgTypeListDrives, a.handleListDrives)
	a.client.RegisterHandler(client.MsgTypeListFiles, a.handleListFiles)
	a.client.RegisterHandler(client.MsgTypeScanDirectory, a.handleScanDirectory)
	a.client.RegisterHandler(client.MsgTypeDownloadFile, a.handleDownloadFile)
	a.client.RegisterHandler(client.MsgTypeUploadFile, a.handleUploadFile)
	a.client.RegisterHandler(client.MsgTypeStartRemote, a.handleStartRemote)
	a.client.RegisterHandler(client.MsgTypeStopRemote, a.handleStopRemote)
	a.client.RegisterHandler(client.MsgTypeRemoteInput, a.handleRemoteInput)
	a.client.RegisterHandler(client.MsgTypeCollectDiagnostics, a.handleCollectDiagnostics)
	a.client.RegisterHandler(client.MsgTypeUninstallAgent, a.handleUninstallAgent)
	a.client.RegisterHandler(client.MsgTypePing, a.handlePing)
	// Admin management handlers
	a.client.RegisterHandler(client.MsgTypeAdminDiscover, a.handleAdminDiscover)
	a.client.RegisterHandler(client.MsgTypeAdminDemote, a.handleAdminDemote)
	// WebRTC handlers
	a.client.RegisterHandler(client.MsgTypeWebRTCStart, a.handleWebRTCStart)
	a.client.RegisterHandler(client.MsgTypeWebRTCSignal, a.handleWebRTCSignal)
	a.client.RegisterHandler(client.MsgTypeWebRTCStop, a.handleWebRTCStop)
	// Configuration handlers
	a.client.RegisterHandler(client.MsgTypeSetMetricsInterval, a.handleSetMetricsInterval)
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
		"ipAddress":      sysInfo.IPAddress,
		"macAddress":     sysInfo.MACAddress,
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

	log.Printf("Starting metrics loop with interval: %v", interval)

	for {
		select {
		case <-a.ctx.Done():
			return
		case newInterval := <-a.metricsIntervalChan:
			// Dynamic interval change requested
			if newInterval > 0 && newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
				log.Printf("Metrics interval changed to: %v", interval)
			}
		case <-ticker.C:
			if !a.client.IsConnected() || !a.client.IsAuthenticated() {
				continue
			}

			metrics, err := a.collector.Collect(a.ctx)
			if err != nil {
				log.Printf("Failed to collect metrics: %v", err)
				continue
			}

			// Try gRPC Data Plane first (preferred for metrics streaming)
			if a.dataPlane != nil && a.dataPlane.IsConnected() {
				grpcMetrics := &agentgrpc.Metrics{
					AgentID:         a.cfg.AgentID,
					Timestamp:       time.Now().UnixMilli(),
					CPUPercent:      metrics.CPUPercent,
					MemoryPercent:   metrics.MemoryPercent,
					MemoryUsed:      metrics.MemoryUsed,
					MemoryAvailable: metrics.MemoryAvailable,
					DiskPercent:     metrics.DiskPercent,
					DiskUsed:        metrics.DiskUsed,
					DiskTotal:       metrics.DiskTotal,
					NetworkRxBytes:  metrics.NetworkRxBytes,
					NetworkTxBytes:  metrics.NetworkTxBytes,
					ProcessCount:    int32(metrics.ProcessCount),
					Uptime:          metrics.Uptime,
				}
				if err := a.dataPlane.SendMetrics(a.ctx, grpcMetrics); err != nil {
					log.Printf("gRPC metrics failed, falling back to WebSocket: %v", err)
					// Fallback to WebSocket
					if err := a.client.SendMetrics(metrics); err != nil {
						log.Printf("Failed to send metrics via WebSocket: %v", err)
					}
				}
			} else {
				// gRPC not available, use WebSocket
				if err := a.client.SendMetrics(metrics); err != nil {
					log.Printf("Failed to send metrics: %v", err)
				}
			}
		}
	}
}

// Message handlers

func (a *Agent) handlePing(msg *client.Message) error {
	// Respond to ping with pong
	return a.client.SendJSON(map[string]interface{}{
		"type": client.MsgTypePong,
		"requestId": msg.RequestID,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

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

func (a *Agent) handleListDrives(msg *client.Message) error {
	drives, err := a.fileTransfer.ListDrives()
	if err != nil {
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	return a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
		"drives": drives,
	}, "")
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

func (a *Agent) handleScanDirectory(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	path, _ := data["path"].(string)
	maxDepth := 10 // Default depth
	if depth, ok := data["maxDepth"].(float64); ok {
		maxDepth = int(depth)
	}

	// Send progress updates via scan_progress messages
	onProgress := func(progress filetransfer.ScanProgress) {
		a.client.SendJSON(map[string]interface{}{
			"type":      client.MsgTypeScanProgress,
			"requestId": msg.RequestID,
			"progress":  progress,
		})
	}

	result, err := a.fileTransfer.ScanDirectoryRecursive(a.ctx, path, maxDepth, onProgress)
	if err != nil {
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	return a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
		"result": result,
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

// Remote Desktop handlers

func (a *Agent) handleStartRemote(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	sessionID, _ := data["sessionId"].(string)
	quality, _ := data["quality"].(string)
	if quality == "" {
		quality = "medium"
	}

	onFrame := func(frameData string, width, height int) {
		a.client.SendRemoteFrame(sessionID, frameData, width, height)
	}

	_, err := a.remoteManager.StartSession(sessionID, quality, onFrame)
	if err != nil {
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	return a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
		"sessionId": sessionID,
	}, "")
}

func (a *Agent) handleStopRemote(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	sessionID, _ := data["sessionId"].(string)
	a.remoteManager.StopSession(sessionID)

	return a.client.SendResponse(msg.RequestID, true, nil, "")
}

func (a *Agent) handleRemoteInput(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid message data")
	}

	sessionID, _ := data["sessionId"].(string)
	inputType, _ := data["type"].(string)

	session, ok := a.remoteManager.GetSession(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	session.HandleInput(inputType, data)
	return nil
}

// handleTamperReports processes tamper detection alerts
func (a *Agent) handleTamperReports() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case report := <-a.tamperChan:
			log.Printf("SECURITY ALERT: %s", report)
			// Send tamper report to server
			if a.client.IsConnected() && a.client.IsAuthenticated() {
				a.client.SendJSON(map[string]interface{}{
					"type": "tamper_alert",
					"data": map[string]interface{}{
						"message":   report,
						"timestamp": time.Now().UTC().Format(time.RFC3339),
						"agentId":   a.cfg.AgentID,
					},
				})
			}
		}
	}

}

// handleCollectDiagnostics handles diagnostic data collection requests
func (a *Agent) handleCollectDiagnostics(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	hoursBack := 8 // Default to 8 hours
	if h, ok := data["hoursBack"].(float64); ok {
		hoursBack = int(h)
	}

	log.Printf("Collecting diagnostics for the past %d hours...", hoursBack)

	result, err := a.diagnosticsCollector.CollectAll(a.ctx, hoursBack)
	if err != nil {
		log.Printf("Diagnostics collection error: %v", err)
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	log.Printf("Diagnostics collected: %d system errors, %d app logs, %d processes",
		len(result.SystemErrors), len(result.ApplicationLogs), len(result.ActivePrograms))

	return a.client.SendResponse(msg.RequestID, true, result, "")
}

// handleUninstallAgent handles remote uninstall requests
func (a *Agent) handleUninstallAgent(msg *client.Message) error {
	// Server sends data in Payload field
	data, ok := msg.Payload.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	// Extract the uninstall token from the message
	uninstallToken, _ := data["uninstallToken"].(string)
	deviceID, _ := data["deviceId"].(string)

	log.Printf("Received remote uninstall request for device %s", deviceID)

	// Send acknowledgment before starting uninstall
	if err := a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
		"status": "uninstalling",
	}, ""); err != nil {
		log.Printf("Failed to send uninstall acknowledgment: %v", err)
	}

	// Perform uninstall in a goroutine so we can respond first
	go func() {
		// Small delay to ensure response is sent
		time.Sleep(2 * time.Second)

		log.Println("Starting agent uninstall process...")

		// Call the service uninstall with token
		err := svc.UninstallWithToken(a.cfg.ServerURL, deviceID, uninstallToken)
		if err != nil {
			log.Printf("Uninstall error: %v", err)
			// Try to send error notification if still connected
			a.client.SendJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]interface{}{
					"message": fmt.Sprintf("Uninstall failed: %v", err),
				},
			})
		} else {
			log.Println("Agent uninstalled successfully, exiting...")
		}

		// Stop the agent
		a.cancel()

	// Shutdown desktop manager (kills helper processes)
	if a.desktopManager != nil {
		a.desktopManager.Shutdown()
	}

		// Give time for cleanup
		time.Sleep(1 * time.Second)

		// Exit the process
		os.Exit(0)
	}()

	return nil
}

// WebRTC handlers

func (a *Agent) handleWebRTCStart(msg *client.Message) error {
	log.Printf("[WebRTC] handleWebRTCStart called, RequestID=%s", msg.RequestID)

	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		log.Printf("[WebRTC] ERROR: Invalid message data type")
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	connectionID, _ := data["sessionId"].(string)
	offerSdp, _ := data["offerSdp"].(string)

	log.Printf("[WebRTC] Parsed: connectionID=%s, offerSdp length=%d", connectionID, len(offerSdp))

	if offerSdp == "" {
		return a.client.SendResponse(msg.RequestID, false, nil, "No SDP offer provided")
	}

	// Get the active Windows session ID (the user's desktop session)
	winSessionID := desktop.GetActiveConsoleSessionID()
	log.Printf("[WebRTC] Using Windows session ID: %d", winSessionID)

	// Use the desktop manager to spawn helper and start WebRTC session
	// The helper runs in the user's session where it has display access
	answerType, answerSdp, err := a.desktopManager.StartSession(a.ctx, winSessionID, connectionID, "offer", offerSdp)
	if err != nil {
		log.Printf("[WebRTC] Failed to start session via desktop helper: %v", err)
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	log.Printf("[WebRTC] Got answer from helper: type=%s, sdp length=%d", answerType, len(answerSdp))

	return a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
		"sessionId": connectionID,
		"answerSdp": answerSdp,
	}, "")
}

func (a *Agent) handleWebRTCSignal(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid message data")
	}

	signalData, ok := data["signal"].(map[string]interface{})
	if !ok {
		signalData = data
	}

	signalType, _ := signalData["type"].(string)
	connectionID, _ := signalData["sessionId"].(string)

	// Handle ICE candidates - forward to desktop helper
	if signalType == "candidate" {
		candidate, _ := signalData["candidate"].(string)
		sdpMid, _ := signalData["sdpMid"].(string)
		var sdpMLineIndex *int
		if idx, ok := signalData["sdpMLineIndex"].(float64); ok {
			i := int(idx)
			sdpMLineIndex = &i
		}

		winSessionID := desktop.GetActiveConsoleSessionID()
		return a.desktopManager.AddICECandidate(winSessionID, connectionID, candidate, sdpMid, sdpMLineIndex)
	}

	log.Printf("[WebRTC] Ignoring signal type: %s", signalType)
	return nil
}

func (a *Agent) handleWebRTCStop(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	connectionID, _ := data["sessionId"].(string)
	winSessionID := desktop.GetActiveConsoleSessionID()
	
	if err := a.desktopManager.StopSession(winSessionID, connectionID, "user requested stop"); err != nil {
		log.Printf("[WebRTC] Stop session error: %v", err)
	}

	return a.client.SendResponse(msg.RequestID, true, nil, "")
}

// Admin management handlers

func (a *Agent) handleAdminDiscover(msg *client.Message) error {
	log.Println("[Admin] Discovering local administrators...")

	// Discover all local admins
	admins, err := a.adminManager.DiscoverAdmins()
	if err != nil {
		log.Printf("[Admin] Discovery error: %v", err)
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	log.Printf("[Admin] Found %d administrator accounts", len(admins))

	// Perform safety validation
	safetyCheck, err := a.adminManager.ValidateSafety(admins)
	if err != nil {
		log.Printf("[Admin] Safety validation error: %v", err)
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	log.Printf("[Admin] Safety check: safe=%v, canProceed=%v, currentUser=%s",
		safetyCheck.Safe, safetyCheck.CanProceed, safetyCheck.CurrentUser)

	// Send discovery results back
	return a.client.SendAdminDiscovery(msg.RequestID, admins, safetyCheck)
}

func (a *Agent) handleAdminDemote(msg *client.Message) error {
	log.Println("[Admin] Processing demotion request...")

	// Parse demotion request
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	// Extract accounts to demote
	var accountsToDemote []string
	if accounts, ok := data["accountsToDemote"].([]interface{}); ok {
		for _, acc := range accounts {
			if sid, ok := acc.(string); ok {
				accountsToDemote = append(accountsToDemote, sid)
			}
		}
	}

	confirmed, _ := data["confirmed"].(bool)

	if !confirmed {
		return a.client.SendResponse(msg.RequestID, false, nil, "Demotion must be confirmed")
	}

	if len(accountsToDemote) == 0 {
		return a.client.SendResponse(msg.RequestID, false, nil, "No accounts specified for demotion")
	}

	log.Printf("[Admin] Demoting %d accounts: %v", len(accountsToDemote), accountsToDemote)

	// Get current admin list for validation
	admins, err := a.adminManager.DiscoverAdmins()
	if err != nil {
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	// Execute demotion
	request := &admin.DemotionRequest{
		AccountsToDemote: accountsToDemote,
		Confirmed:        confirmed,
	}

	result, err := a.adminManager.Demote(request, admins)
	if err != nil {
		log.Printf("[Admin] Demotion error: %v", err)
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	log.Printf("[Admin] Demotion result: success=%v, demoted=%v, remaining=%v",
		result.Success, result.DemotedAccounts, result.RemainingAdmins)

	// Get hostname for telemetry
	hostname, _ := os.Hostname()

	// Create and send telemetry event
	event := a.adminManager.CreateDemotionEvent(result, hostname)
	if eventErr := a.client.SendAdminEvent(event); eventErr != nil {
		log.Printf("[Admin] Failed to send telemetry event: %v", eventErr)
	}

	// Send result
	return a.client.SendAdminDemotionResult(msg.RequestID, result)
}

// handleSetMetricsInterval handles dynamic metrics interval changes
func (a *Agent) handleSetMetricsInterval(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	// Interval is in milliseconds
	intervalMs, ok := data["intervalMs"].(float64)
	if !ok || intervalMs < 100 {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid interval (minimum 100ms)")
	}

	// Convert milliseconds to duration
	newInterval := time.Duration(intervalMs) * time.Millisecond

	log.Printf("Received request to change metrics interval to %v", newInterval)

	// Send to the metrics loop (non-blocking)
	select {
	case a.metricsIntervalChan <- newInterval:
		return a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
			"intervalMs": intervalMs,
		}, "")
	default:
		return a.client.SendResponse(msg.RequestID, false, nil, "Metrics interval change already pending")
	}
}
