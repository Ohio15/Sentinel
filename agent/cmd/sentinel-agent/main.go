package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
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
	svc "github.com/sentinel/agent/internal/service"
	"github.com/sentinel/agent/internal/terminal"
	"github.com/sentinel/agent/internal/logrotate"
	"github.com/sentinel/agent/internal/updater"
	"github.com/sentinel/agent/internal/updates"
	"github.com/sentinel/agent/internal/admin"
	"github.com/sentinel/agent/internal/logforward"
	"github.com/sentinel/agent/internal/mtls"
	"github.com/sentinel/agent/internal/peripheral"
)

var Version = "1.77.7"

const ServiceName = "SentinelAgent"

var (
	serverURL   = flag.String("server", "", "Sentinel server URL (e.g., http://192.168.1.100:8080)")
	token       = flag.String("token", "", "Enrollment token")
	grpcAddress = flag.String("grpc-address", "", "gRPC Data Plane address (e.g., 192.168.1.100:8444)")
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
	terminalManager *terminal.Manager
	fileTransfer    *filetransfer.FileTransfer
	desktopManager  *desktop.Manager // Desktop helper manager for WebRTC
	updater           *updater.Updater
	protectionManager    *protection.Manager
	diagnosticsCollector *diagnostics.Collector
	adminManager         *admin.Manager
	updateChecker        *updates.Checker
	updateInstaller      *updates.Installer
	peripheralManager    *peripheral.Manager
	logForwarder         *logforward.Forwarder
	terminalAudit        *terminal.AuditLogger
	logRotator           *logrotate.RotatingWriter
	tamperChan           chan string
	metricsIntervalChan  chan time.Duration // Channel for dynamic metrics interval changes
	ctx                  context.Context
	cancel               context.CancelFunc
}

func main() {
	flag.Parse()

	// Set up logging - use platform-specific path
	var logPath string
	if runtime.GOOS == "windows" {
		logPath = "C:\\ProgramData\\Sentinel\\agent.log"
	} else {
		// On Linux/Unix, write to /var/log if possible, otherwise just use stderr
		logPath = "/var/log/sentinel/agent.log"
	}

	// Use rotating log writer: 10MB max, keep 5 rotated files
	logRotator, err := logrotate.New(logPath, 0, 0) // 0,0 = use defaults (10MB, 5 files)
	if err == nil {
		log.SetOutput(logRotator)
		defer logRotator.Close()
	} else if runtime.GOOS != "windows" {
		// On Linux, if we can't write to log file, write to stderr for systemd
		log.SetOutput(os.Stderr)
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
	if *grpcAddress != "" {
		cfg.GrpcAddress = *grpcAddress
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

	// Create updater for autonomous updates with resilient architecture
	// Layer 1: WebSocket push (existing)
	// Layer 2: Agent independent HTTP polling (30 min default, adaptive)
	// Layer 3: Watchdog independent polling (Windows, 15 min)
	// Layer 4: Bootstrap recovery task (6 hours, last resort)
	agentUpdater := updater.New(cfg.ServerURL, Version)
	// Note: Updater now uses adaptive polling with sensible defaults

	// Create protection manager
	exePath, _ := os.Executable()
	installPath := filepath.Dir(exePath)
	protMgr := protection.NewManager(installPath, ServiceName)

	// Create Data Plane client (gRPC for metrics streaming)
	// gRPC is used for direct/LAN connections only. Tunnel/remote agents use WebSocket fallback
	// for metrics, which is simpler and works reliably through Cloudflare.
	grpcAddr := cfg.GetGrpcAddress()
	var dataPlane *agentgrpc.DataPlaneClient
	connMode := cfg.GetConnectionMode()
	if grpcAddr != "" && connMode != config.ConnModeTunnel {
		dataPlane = agentgrpc.NewDataPlaneClient(cfg.AgentID, grpcAddr)
		log.Printf("[gRPC] Data Plane client created (direct, address=%s)", grpcAddr)
	} else if connMode == config.ConnModeTunnel {
		log.Printf("[gRPC] Skipping gRPC in tunnel mode — metrics via WebSocket")
	}

	wsClient := client.New(cfg, Version)
	agent := &Agent{
		cfg:                  cfg,
		client:               wsClient,
		dataPlane:            dataPlane,
		collector:            collector.New(),
		executor:        executor.New(),
		terminalManager: terminal.NewManager(),
		fileTransfer:    ft,
		desktopManager:  desktop.NewManager(""),
		updater:              agentUpdater,
		protectionManager:    protMgr,
		diagnosticsCollector: diagnostics.New(),
		adminManager:         admin.NewManager(Version),
		updateChecker:        updates.NewChecker(),
		updateInstaller:      updates.NewInstaller(),
		logForwarder:         logforward.New(wsClient),
		terminalAudit:        initTerminalAudit(cfg),
		tamperChan:           make(chan string, 10),
		metricsIntervalChan:  make(chan time.Duration, 1),
		ctx:                  ctx,
		cancel:               cancel,
	}

	// Initialize peripheral manager with callback
	agent.peripheralManager = peripheral.NewManager(agent.handleUSBDeviceEvent)

	return agent
}

// initTerminalAudit creates an audit logger for terminal sessions.
// Uses the configured AuditLogDir if set, otherwise falls back to the platform default.
// Returns nil if the audit log directory cannot be created (non-fatal).
func initTerminalAudit(cfg *config.Config) *terminal.AuditLogger {
	logDir := cfg.GetAuditLogDir()
	audit, err := terminal.NewAuditLogger(logDir)
	if err != nil {
		log.Printf("[WARN] Terminal audit logging disabled: %v", err)
		return nil
	}
	log.Printf("[INFO] Terminal audit logging to: %s", logDir)
	return audit
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
	a.updater.SetAgentID(a.cfg.AgentID)
	a.updater.SetEnrollmentToken(a.cfg.EnrollmentToken)
	a.updater.CheckAndReportUpdateResult(a.ctx)

	// Install bootstrap recovery task (Layer 4 - last resort recovery)
	// This runs independently every 6 hours and can recover the agent
	// even if both agent and watchdog are completely dead
	if err := a.updater.InstallBootstrapRecoveryTask(a.cfg.ServerURL); err != nil {
		log.Printf("Warning: Could not install bootstrap recovery task: %v", err)
	}

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

	// Collect and set device info for auto-enrollment of orphaned agents
	if sysInfo, err := a.collector.GetSystemInfo(); err == nil {
		// Convert collector.GPUInfo to client.GPUInfo (static hardware specs)
		var gpuInfo []client.GPUInfo
		for _, g := range sysInfo.GPU {
			gpuInfo = append(gpuInfo, client.GPUInfo{
				Name:          g.Name,
				Vendor:        g.Vendor,
				Memory:        g.Memory,
				DriverVersion: g.DriverVersion,
			})
		}

		// Convert collector.StorageInfo to client.StorageInfo (static: device/mount/total capacity)
		var storageInfo []client.StorageInfo
		for _, s := range sysInfo.Storage {
			storageInfo = append(storageInfo, client.StorageInfo{
				Device:     s.Device,
				Mountpoint: s.Mountpoint,
				FSType:     s.FSType,
				Total:      s.Total,
				// Note: Used/Free/Percent are dynamic and come via metrics, but included as initial snapshot
				Used:    s.Used,
				Free:    s.Free,
				Percent: s.Percent,
			})
		}

		a.client.SetDeviceInfo(&client.DeviceInfo{
			Hostname:     sysInfo.Hostname,
			Platform:     sysInfo.Platform,
			OSType:       sysInfo.OS,
			OSVersion:    sysInfo.OSVersion,
			Architecture: sysInfo.Architecture,
			CPUModel:     sysInfo.CPUModel,
			CPUCores:     sysInfo.CPUCores,
			TotalMemory:  sysInfo.TotalMemory,
			SerialNumber: sysInfo.SerialNumber,
			Manufacturer: sysInfo.Manufacturer,
			Model:        sysInfo.Model,
			IPAddress:    sysInfo.IPAddress,
			MACAddress:   sysInfo.MACAddress,
			GPU:          gpuInfo,
			Storage:      storageInfo,
		})
	}

	// Set up connection callbacks
	a.client.OnConnect(a.onConnect)
	a.client.OnDisconnect(a.onDisconnect)
	a.client.OnNeedsEnrollment(a.onNeedsEnrollment)

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

	// Check for pending alerts from watchdog on startup (e.g., update failures during restart)
	go func() {
		// Wait for connection to be established
		time.Sleep(10 * time.Second)
		if a.client.IsConnected() && a.client.IsAuthenticated() {
			a.relayPendingAlerts()
		}
	}()

	// Start heartbeat loop
	go a.heartbeatLoop()

	// Start metrics loop
	go a.metricsLoop()

	// Start update check loop
	go a.updater.RunUpdateLoop(a.ctx)

	// Start Windows Update status monitoring loop
	go a.updateStatusLoop()

	// Start certificate renewal check loop (daily)
	go a.certRenewalLoop()

	// Start centralized log forwarding pipeline
	if a.logForwarder != nil {
		go a.logForwarder.Start(a.ctx)
	}

	// Start USB/peripheral device monitoring
	if a.peripheralManager != nil {
		if err := a.peripheralManager.Start(); err != nil {
			log.Printf("Warning: Failed to start peripheral monitoring: %v", err)
		}
	}

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

	// Stop peripheral monitoring
	if a.peripheralManager != nil {
		a.peripheralManager.Stop()
	}

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
	// Certificate management handlers
	a.client.RegisterHandler(client.MsgTypeUpdateCertificate, a.handleUpdateCertificate)
	// Update handlers
	a.client.RegisterHandler(client.MsgTypeForceUpdate, a.handleForceUpdate)
	// Power management handlers
	a.client.RegisterHandler(client.MsgTypePowerAction, a.handlePowerAction)
	// Windows Update installation handlers
	a.client.RegisterHandler(client.MsgTypeInstallUpdates, a.handleInstallUpdates)
	// USB/Peripheral device handlers
	a.client.RegisterHandler(client.MsgTypeUSBDeviceRequest, a.handleUSBDeviceRequest)
}

func (a *Agent) onConnect() {
	log.Println("Connected to server")
}

func (a *Agent) onDisconnect() {
	log.Println("Disconnected from server")
}

func (a *Agent) onNeedsEnrollment() {
	log.Println("Re-enrolling with server...")
	if err := a.enroll(); err != nil {
		log.Printf("Re-enrollment failed: %v", err)
	}
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
		// Provide actionable error messages based on error type
		errStr := err.Error()
		var message, suggestion string
		switch {
		case strings.Contains(errStr, "no such host"):
			message = "Cannot resolve server hostname"
			suggestion = "Check that the server URL is correct. Try using an IP address instead."
		case strings.Contains(errStr, "connection refused"):
			message = "Server is not accepting connections"
			suggestion = "Verify the server is running and the port is correct. Check firewall rules."
		case strings.Contains(errStr, "i/o timeout") || strings.Contains(errStr, "context deadline exceeded"):
			message = "Connection timed out"
			suggestion = "Server may be down or unreachable. Check network connectivity and server status."
		case strings.Contains(errStr, "certificate") || strings.Contains(errStr, "x509"):
			message = "TLS/SSL certificate error"
			suggestion = "Server certificate may be invalid or expired. Check server TLS configuration."
		default:
			message = fmt.Sprintf("Connection failed: %v", err)
			suggestion = "Check server URL and network connectivity."
		}
		log.Printf("[Enrollment] %s: %v", message, err)
		return fmt.Errorf("enrollment failed: %s\nSuggestion: %s", message, suggestion)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		// Read error body for additional context
		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyStr := string(bodyBytes)

		// Provide specific guidance based on status code
		var suggestion string
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			suggestion = "Enrollment token is invalid or expired. Generate a new token from the Sentinel dashboard."
		case http.StatusForbidden:
			suggestion = "Agent is not allowed to enroll. Check server enrollment settings."
		case http.StatusConflict:
			suggestion = "Agent ID already enrolled. The agent may need to be removed from the dashboard first."
		case http.StatusServiceUnavailable:
			suggestion = "Server is temporarily unavailable. It may be starting up or under maintenance."
		default:
			suggestion = "Check server logs for more details."
		}

		log.Printf("[Enrollment] Server returned status %d: %s", resp.StatusCode, bodyStr)
		return fmt.Errorf("enrollment failed (HTTP %d): %s\nSuggestion: %s", resp.StatusCode, bodyStr, suggestion)
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
		// Also update the running metrics loop
		newInterval := time.Duration(result.Config.MetricsInterval) * time.Second
		select {
		case a.metricsIntervalChan <- newInterval:
			log.Printf("Updated metrics interval to %v from server config", newInterval)
		default:
			// Channel full, will be picked up next iteration
		}
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
				// Check for pending alerts from the watchdog (update failures, rollbacks)
				a.relayPendingAlerts()
			}
		}
	}
}

// relayPendingAlerts checks for alert files written by the watchdog and sends them to the server
func (a *Agent) relayPendingAlerts() {
	alert, err := ipc.ReadAndDeleteAlert()
	if err != nil {
		log.Printf("[Alert] Error reading pending alert: %v", err)
		return
	}
	if alert == nil {
		return
	}

	log.Printf("[Alert] Relaying watchdog alert: [%s] %s - %s", alert.Severity, alert.Title, alert.Message)
	if err := a.client.SendEvent(alert.Severity, alert.Title, alert.Message); err != nil {
		log.Printf("[Alert] Failed to relay alert to server: %v", err)
		// Re-write the alert so it can be retried next heartbeat
		if writeErr := ipc.WriteAlert(alert); writeErr != nil {
			log.Printf("[Alert] Failed to re-write alert for retry: %v", writeErr)
		}
	}
}

func (a *Agent) metricsLoop() {
	interval := time.Duration(a.cfg.MetricsInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Starting metrics loop with interval: %v", interval)

	// Helper to check for interval changes (non-blocking)
	checkIntervalChange := func() {
		select {
		case newInterval := <-a.metricsIntervalChan:
			if newInterval > 0 && newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
				log.Printf("Metrics interval changed to: %v", interval)
			}
		default:
			// No pending interval change
		}
	}

	for {
		// Always check for interval changes first (non-blocking)
		checkIntervalChange()

		select {
		case <-a.ctx.Done():
			return
		case newInterval := <-a.metricsIntervalChan:
			// Dynamic interval change requested (blocking case for when ticker isn't ready)
			if newInterval > 0 && newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
				log.Printf("Metrics interval changed to: %v", interval)
			}
		case <-ticker.C:
			// Check again before collecting (in case change came during wait)
			checkIntervalChange()

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

// updateStatusLoop periodically checks for pending Windows updates and reports to server
func (a *Agent) updateStatusLoop() {
	// Check immediately on startup after a short delay
	select {
	case <-a.ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}

	// Initial check
	a.checkAndSendUpdateStatus()

	// Then check every 4 hours
	ticker := time.NewTicker(4 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.checkAndSendUpdateStatus()
		}
	}
}

// checkAndSendUpdateStatus checks for updates and sends status to the server
func (a *Agent) checkAndSendUpdateStatus() {
	if !a.client.IsConnected() || !a.client.IsAuthenticated() {
		return
	}

	log.Println("Checking for Windows updates...")
	status, err := a.updateChecker.GetStatus(a.ctx, false)
	if err != nil {
		log.Printf("Failed to check update status: %v", err)
		return
	}

	log.Printf("Update status: %d pending, %d security updates, reboot required: %v",
		status.PendingCount, status.SecurityUpdateCount, status.RebootRequired)

	if err := a.client.SendUpdateStatus(status); err != nil {
		log.Printf("Failed to send update status: %v", err)
	}
}

// certRenewalLoop checks daily if the client certificate needs renewal
func (a *Agent) certRenewalLoop() {
	// Initial delay before first check (5 minutes after startup)
	select {
	case <-a.ctx.Done():
		return
	case <-time.After(5 * time.Minute):
	}

	// Initial check
	a.checkCertRenewal()

	// Then check every 24 hours
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.checkCertRenewal()
		}
	}
}

// checkCertRenewal checks if the client certificate needs renewal and requests it
func (a *Agent) checkCertRenewal() {
	// Skip if we don't have mTLS certificates
	if !mtls.HasMTLS() {
		return
	}

	// Skip if not connected
	if !a.client.IsConnected() || !a.client.IsAuthenticated() {
		return
	}

	// Check if renewal is needed (expires within 30 days)
	if !mtls.NeedsRenewal() {
		expiry, err := mtls.GetCertificateExpiry()
		if err == nil {
			log.Printf("[mTLS] Certificate valid until %s", expiry.Format(time.RFC3339))
		}
		return
	}

	log.Println("[mTLS] Certificate expires soon, requesting renewal...")

	// Get current certificate serial for revocation
	oldSerial, _ := mtls.GetCertificateSerial()

	// Request renewal via HTTP API (uses mTLS for authentication)
	if err := a.requestCertRenewal(oldSerial); err != nil {
		log.Printf("[mTLS] Certificate renewal failed: %v", err)
		return
	}

	log.Println("[mTLS] Certificate renewed successfully")
}

// requestCertRenewal sends a certificate renewal request to the server
func (a *Agent) requestCertRenewal(oldSerial string) error {
	// Build the renewal endpoint URL
	serverURL := a.cfg.ServerURL
	if serverURL == "" {
		return fmt.Errorf("server URL not configured")
	}

	// Use mTLS port for the request
	renewURL := mtls.GetMTLSServerURL(serverURL)
	if strings.Contains(renewURL, "wss://") {
		renewURL = strings.Replace(renewURL, "wss://", "https://", 1)
	} else if strings.Contains(renewURL, "ws://") {
		renewURL = strings.Replace(renewURL, "ws://", "http://", 1)
	}
	// Remove WebSocket path, add API path
	if idx := strings.Index(renewURL, "/ws"); idx != -1 {
		renewURL = renewURL[:idx]
	}
	renewURL = renewURL + "/api/agent/certs/renew"

	// Get TLS config with client certificate
	tlsConfig, err := mtls.GetTLSConfig()
	if err != nil {
		return fmt.Errorf("failed to get TLS config: %w", err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	req, err := http.NewRequest("POST", renewURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var renewResp struct {
		ClientCert   string `json:"clientCert"`
		ClientKey    string `json:"clientKey"`
		CACert       string `json:"caCert"`
		CertExpiresAt string `json:"certExpiresAt"`
		CertSerial   string `json:"certSerial"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&renewResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Install the new certificates
	if err := mtls.InstallCertificates(
		[]byte(renewResp.ClientCert),
		[]byte(renewResp.ClientKey),
		[]byte(renewResp.CACert),
	); err != nil {
		return fmt.Errorf("failed to install certificates: %w", err)
	}

	log.Printf("[mTLS] New certificate installed, serial=%s, expires=%s",
		renewResp.CertSerial, renewResp.CertExpiresAt)

	return nil
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
	// Notify updater of successful server communication
	// This enables adaptive polling - if WebSocket is healthy, polling can be less aggressive
	a.updater.NotifyServerContact()

	// Check if server indicated an update is available
	// Server sends update info in 'payload' field, not 'data'
	payloadData := msg.Payload
	if payloadData == nil {
		payloadData = msg.Data // Fallback for backwards compatibility
	}
	if payloadData != nil {
		if payload, ok := payloadData.(map[string]interface{}); ok {
			if updateAvailable, ok := payload["updateAvailable"].(bool); ok && updateAvailable {
				latestVersion := ""
				if v, ok := payload["latestVersion"].(string); ok {
					latestVersion = v
				}
				log.Printf("[Update] Server indicated update available: v%s -> v%s", Version, latestVersion)
				// Use TriggerCheck() to deduplicate via buffered channel (capacity 1)
				// This prevents the old bug where every heartbeat ack spawned a new
				// goroutine that independently downloaded + applied, causing 1000+
				// concurrent downloads and race conditions in apply
				a.updater.TriggerCheck()
			}
		}
	}
	return nil
}

func (a *Agent) handleExecuteCommand(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	command, _ := data["command"].(string)
	cmdType, _ := data["commandType"].(string)
	commandID, _ := data["commandId"].(string)

	result, err := a.executor.Execute(a.ctx, command, cmdType)
	if err != nil {
		return a.client.SendResponse(msg.RequestID, false, map[string]interface{}{
			"commandId": commandID,
		}, err.Error())
	}

	return a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
		"commandId": commandID,
		"output":    result.Stdout + result.Stderr,
		"exitCode":  result.ExitCode,
		"duration":  result.Duration,
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

	requestedBy, _ := data["requestedBy"].(string)
	if requestedBy == "" {
		requestedBy = "unknown"
	}

	onOutput := func(output string) {
		a.client.SendTerminalOutput(sessionID, output)
	}

	sessionStart := time.Now()
	cmdCount := 0

	onClose := func() {
		log.Printf("Terminal session %s closed", sessionID)
		if a.terminalAudit != nil {
			a.terminalAudit.LogSessionEnd(sessionID, time.Since(sessionStart), cmdCount)
		}
	}

	if a.terminalAudit != nil {
		a.terminalAudit.LogSessionStart(sessionID, requestedBy)
	}

	_, err := a.terminalManager.CreateSession(a.ctx, sessionID, onOutput, onClose)
	if err != nil {
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	return a.client.SendResponse(msg.RequestID, true, nil, "")
}

func (a *Agent) handleTerminalInput(msg *client.Message) error {
	const maxInputLength = 4096 // 4KB max per command

	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid message data")
	}

	sessionID, _ := data["sessionId"].(string)
	input, _ := data["data"].(string)

	// Validate input length
	if len(input) > maxInputLength {
		log.Printf("[SECURITY] Oversized terminal input rejected: session=%s length=%d max=%d", sessionID, len(input), maxInputLength)
		if a.terminalAudit != nil {
			a.terminalAudit.LogInput(sessionID, fmt.Sprintf("[REJECTED: oversized input %d bytes]", len(input)))
		}
		return fmt.Errorf("terminal input too long (%d bytes, max %d)", len(input), maxInputLength)
	}

	// Reject null bytes (potential injection)
	for i := 0; i < len(input); i++ {
		if input[i] == 0x00 {
			log.Printf("[SECURITY] Null byte in terminal input rejected: session=%s", sessionID)
			if a.terminalAudit != nil {
				a.terminalAudit.LogInput(sessionID, "[REJECTED: null byte detected]")
			}
			return fmt.Errorf("terminal input contains null bytes")
		}
	}

	session, ok := a.terminalManager.GetSession(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if a.terminalAudit != nil {
		a.terminalAudit.LogInput(sessionID, input)
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

// handleTamperReports processes tamper detection alerts
func (a *Agent) handleTamperReports() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case report := <-a.tamperChan:
			log.Printf("SECURITY ALERT: %s", report)
			// Forward to log pipeline
			if a.logForwarder != nil {
				a.logForwarder.Log("error", "protection", report, nil)
			}
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
		// Include sessionId in error response so frontend can match it
		return a.client.SendResponse(msg.RequestID, false, map[string]interface{}{
			"sessionId": connectionID,
		}, err.Error())
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
// handleUpdateCertificate handles CA certificate updates from the server
func (a *Agent) handleUpdateCertificate(msg *client.Message) error {
	log.Println("[Certs] Received certificate update request")

	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		errMsg := "Invalid message data"
		a.client.SendCertUpdateAck("", false, errMsg)
		return fmt.Errorf(errMsg)
	}

	certType, _ := data["certType"].(string)
	certContent, _ := data["certContent"].(string)
	certHash, _ := data["certHash"].(string)

	if certContent == "" {
		errMsg := "No certificate content provided"
		a.client.SendCertUpdateAck(certHash, false, errMsg)
		return fmt.Errorf(errMsg)
	}

	hashPrefix := certHash
	if len(hashPrefix) > 8 {
		hashPrefix = hashPrefix[:8]
	}
	log.Printf("[Certs] Updating %s certificate (hash: %s...)", certType, hashPrefix)

	// Determine cert storage path
	var certPath string
	if runtime.GOOS == "windows" {
		certPath = filepath.Join("C:\\ProgramData\\Sentinel\\certs", "ca-cert.pem")
	} else {
		certPath = filepath.Join("/etc/sentinel/certs", "ca-cert.pem")
	}

	// Ensure directory exists
	certDir := filepath.Dir(certPath)
	if err := os.MkdirAll(certDir, 0755); err != nil {
		errMsg := fmt.Sprintf("Failed to create cert directory: %v", err)
		log.Printf("[Certs] %s", errMsg)
		a.client.SendCertUpdateAck(certHash, false, errMsg)
		return fmt.Errorf(errMsg)
	}

	// Write the certificate file
	if err := os.WriteFile(certPath, []byte(certContent), 0644); err != nil {
		errMsg := fmt.Sprintf("Failed to write certificate: %v", err)
		log.Printf("[Certs] %s", errMsg)
		a.client.SendCertUpdateAck(certHash, false, errMsg)
		return fmt.Errorf(errMsg)
	}

	log.Printf("[Certs] Certificate updated successfully at %s", certPath)

	// Send acknowledgment back to server
	if err := a.client.SendCertUpdateAck(certHash, true, ""); err != nil {
		log.Printf("[Certs] Failed to send acknowledgment: %v", err)
		return err
	}

	log.Println("[Certs] Certificate update acknowledged")
	return nil
}

// handleForceUpdate triggers an immediate update check
func (a *Agent) handleForceUpdate(msg *client.Message) error {
	log.Println("[Update] Received force update request from server")

	// Send acknowledgment immediately
	if err := a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
		"status": "checking",
	}, ""); err != nil {
		log.Printf("[Update] Failed to send acknowledgment: %v", err)
	}

	// Trigger update check in background
	go func() {
		log.Println("[Update] Starting forced update check...")
		result, err := a.updater.CheckForUpdate(a.ctx)
		if err != nil {
			log.Printf("[Update] Force update check failed: %v", err)
			return
		}

		if !result.Available {
			log.Printf("[Update] No update available (current: v%s)", Version)
			return
		}

		log.Printf("[Update] Update available: v%s -> v%s, downloading...", Version, result.LatestVersion)

		// Download and apply the update
		downloadPath, err := a.updater.DownloadUpdate(a.ctx, result.VersionInfo)
		if err != nil {
			log.Printf("[Update] Failed to download update: %v", err)
			return
		}

		if err := a.updater.ApplyUpdate(a.ctx, downloadPath, result.VersionInfo); err != nil {
			log.Printf("[Update] Failed to apply update: %v", err)
			os.Remove(downloadPath)
			return
		}

		log.Println("[Update] Update applied successfully, agent will restart")
	}()

	return nil
}

func (a *Agent) handlePowerAction(msg *client.Message) error {
	// Check both Data and Payload fields (server may send in either)
	var data map[string]interface{}
	if d, ok := msg.Data.(map[string]interface{}); ok {
		data = d
	} else if p, ok := msg.Payload.(map[string]interface{}); ok {
		data = p
	}
	if data == nil {
		return fmt.Errorf("invalid power action message format: no data")
	}

	action, _ := data["action"].(string)
	if action == "" {
		return fmt.Errorf("invalid power action message format: no action specified")
	}

	log.Printf("[Power] Received power action: %s", action)

	switch action {
	case "shutdown":
		// Send acknowledgment before shutting down
		if err := a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
			"status": "executing",
			"action": "shutdown",
		}, ""); err != nil {
			log.Printf("[Power] Failed to send acknowledgment: %v", err)
		}

		// Execute shutdown in background after small delay
		go func() {
			time.Sleep(2 * time.Second) // Give time for acknowledgment to be sent
			if err := a.collector.ExecutePowerAction("shutdown"); err != nil {
				log.Printf("[Power] Shutdown failed: %v", err)
			}
		}()

	case "restart":
		// Send acknowledgment before restarting
		if err := a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
			"status": "executing",
			"action": "restart",
		}, ""); err != nil {
			log.Printf("[Power] Failed to send acknowledgment: %v", err)
		}

		// Execute restart in background after small delay
		go func() {
			time.Sleep(2 * time.Second) // Give time for acknowledgment to be sent
			if err := a.collector.ExecutePowerAction("restart"); err != nil {
				log.Printf("[Power] Restart failed: %v", err)
			}
		}()

	case "wake":
		// Wake-on-LAN - send magic packet to the target MAC
		macAddress, _ := data["macAddress"].(string)
		if macAddress == "" {
			return a.client.SendResponse(msg.RequestID, false, nil, "Missing MAC address for Wake-on-LAN")
		}

		if err := a.collector.SendWakeOnLAN(macAddress); err != nil {
			log.Printf("[Power] Wake-on-LAN failed: %v", err)
			return a.client.SendResponse(msg.RequestID, false, nil, fmt.Sprintf("Wake-on-LAN failed: %v", err))
		}

		return a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
			"status":     "sent",
			"action":     "wake",
			"macAddress": macAddress,
		}, "")

	default:
		return a.client.SendResponse(msg.RequestID, false, nil, fmt.Sprintf("Unknown power action: %s", action))
	}

	return nil
}

// handleInstallUpdates handles Windows Update installation requests
func (a *Agent) handleInstallUpdates(msg *client.Message) error {
	if runtime.GOOS != "windows" {
		return a.client.SendResponse(msg.RequestID, false, nil, "Windows Update installation only supported on Windows")
	}

	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		data = make(map[string]interface{})
	}

	// Parse options
	opts := updates.InstallOptions{
		AcceptEULA: true,
	}

	if securityOnly, ok := data["securityOnly"].(bool); ok {
		opts.SecurityOnly = securityOnly
	}
	if allowReboot, ok := data["allowReboot"].(bool); ok {
		opts.AllowReboot = allowReboot
	}
	if rebootDelay, ok := data["rebootDelaySecs"].(float64); ok {
		opts.RebootDelaySecs = int(rebootDelay)
	}
	if kbs, ok := data["specificKBs"].([]interface{}); ok {
		for _, kb := range kbs {
			if kbStr, ok := kb.(string); ok {
				opts.SpecificKBs = append(opts.SpecificKBs, kbStr)
			}
		}
	}

	log.Printf("[Updates] Received install updates request (securityOnly=%v, allowReboot=%v)",
		opts.SecurityOnly, opts.AllowReboot)

	// Check if installation is already in progress
	if a.updateInstaller.IsInstalling() {
		return a.client.SendResponse(msg.RequestID, false, nil, "Update installation already in progress")
	}

	// Send acknowledgment
	if err := a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
		"status":  "started",
		"message": "Windows Update installation started",
	}, ""); err != nil {
		log.Printf("[Updates] Failed to send acknowledgment: %v", err)
	}

	// Run installation in background
	go func() {
		result, err := a.updateInstaller.InstallUpdates(a.ctx, opts)
		if err != nil {
			log.Printf("[Updates] Installation failed: %v", err)
			// Send failure notification
			a.client.SendJSON(map[string]interface{}{
				"type":      client.MsgTypeInstallProgress,
				"requestId": msg.RequestID,
				"phase":     "error",
				"error":     err.Error(),
				"success":   false,
			})
			return
		}

		log.Printf("[Updates] Installation completed: installed=%d, failed=%d, reboot=%v",
			result.InstalledCount, result.FailedCount, result.RebootRequired)

		// Send completion notification
		a.client.SendJSON(map[string]interface{}{
			"type":             client.MsgTypeInstallProgress,
			"requestId":        msg.RequestID,
			"phase":            "complete",
			"success":          result.Success,
			"installedCount":   result.InstalledCount,
			"failedCount":      result.FailedCount,
			"rebootRequired":   result.RebootRequired,
			"installedUpdates": result.InstalledUpdates,
			"failedUpdates":    result.FailedUpdates,
			"error":            result.Error,
			"startedAt":        result.StartedAt.Format(time.RFC3339),
			"completedAt":      result.CompletedAt.Format(time.RFC3339),
		})

		// Refresh update status after installation
		if _, err := a.updateChecker.GetStatus(a.ctx, true); err != nil {
			log.Printf("[Updates] Failed to refresh status: %v", err)
		}

		// Handle reboot if required and allowed
		if result.RebootRequired && opts.AllowReboot {
			delay := opts.RebootDelaySecs
			if delay <= 0 {
				delay = 120 // Default 2 minute delay
			}

			log.Printf("[Updates] Scheduling system reboot in %d seconds", delay)

			// Notify server about pending reboot
			a.client.SendJSON(map[string]interface{}{
				"type":         client.MsgTypeInstallProgress,
				"phase":        "reboot_scheduled",
				"rebootDelay":  delay,
				"rebootReason": "Windows Update installation completed - reboot required",
			})

			if err := updates.ScheduleReboot(a.ctx, delay, "Windows Update installation completed. System will restart."); err != nil {
				log.Printf("[Updates] Failed to schedule reboot: %v", err)
			}
		}
	}()

	return nil
}

// handleUSBDeviceEvent is called when a USB device is connected/disconnected
func (a *Agent) handleUSBDeviceEvent(event *peripheral.DeviceEvent) {
	if event == nil || event.Device == nil {
		return
	}

	if !a.client.IsConnected() || !a.client.IsAuthenticated() {
		log.Printf("[USB] Event %s but not connected to server, queuing", event.EventType)
		return
	}

	log.Printf("[USB] Sending %s event for device: %s (%s %s)",
		event.EventType, event.Device.DeviceID, event.Device.Manufacturer, event.Device.ProductName)

	// Build device data
	deviceData := map[string]interface{}{
		"deviceId":       event.Device.DeviceID,
		"instancePath":   event.Device.InstancePath,
		"vendorId":       event.Device.VendorID,
		"productId":      event.Device.ProductID,
		"serialNumber":   event.Device.SerialNumber,
		"manufacturer":   event.Device.Manufacturer,
		"productName":    event.Device.ProductName,
		"deviceClass":    string(event.Device.DeviceClass),
		"classCode":      event.Device.ClassCode,
		"subclassCode":   event.Device.SubclassCode,
		"protocolCode":   event.Device.ProtocolCode,
		"busNumber":      event.Device.BusNumber,
		"portNumber":     event.Device.PortNumber,
		"deviceSpeed":    event.Device.DeviceSpeed,
		"parentDevice":   event.Device.ParentDevice,
		"driveLetter":    event.Device.DriveLetter,
		"mountPoint":     event.Device.MountPoint,
		"volumeLabel":    event.Device.VolumeLabel,
		"fileSystem":     event.Device.FileSystem,
		"totalSize":      event.Device.TotalSize,
		"freeSpace":      event.Device.FreeSpace,
		"isConnected":    event.Device.IsConnected,
		"connectionTime": event.Device.ConnectionTime.Format(time.RFC3339),
		"isRemovable":    event.Device.IsRemovable,
		"isBootable":     event.Device.IsBootable,
		"isEncrypted":    event.Device.IsEncrypted,
	}

	// Build event data
	eventData := map[string]interface{}{
		"eventType":    event.EventType,
		"device":       deviceData,
		"timestamp":    event.Timestamp.Format(time.RFC3339),
		"securityRisk": peripheral.ClassifySecurityRisk(event.Device),
	}

	// Add session ID if present
	if event.SessionID != "" {
		eventData["sessionId"] = event.SessionID
	}

	// Send event to server
	a.client.SendJSON(map[string]interface{}{
		"type": client.MsgTypeUSBDeviceEvent,
		"data": eventData,
	})

	// If this is a disconnect event with file transfers, send a session complete message
	if event.EventType == "disconnected" && len(event.FileTransfers) > 0 {
		log.Printf("[USB] Session %s completed with %d file transfers", event.SessionID, len(event.FileTransfers))

		// Convert file transfers to serializable format
		transfers := make([]map[string]interface{}, len(event.FileTransfers))
		for i, t := range event.FileTransfers {
			transfers[i] = map[string]interface{}{
				"fileName":     t.FileName,
				"filePath":     t.FilePath,
				"fileSize":     t.FileSize,
				"transferTime": t.TransferTime.Format(time.RFC3339),
				"operation":    t.Operation,
			}
		}

		a.client.SendJSON(map[string]interface{}{
			"type": client.MsgTypeUSBSessionComplete,
			"data": map[string]interface{}{
				"sessionId":        event.SessionID,
				"usbDeviceId":      event.Device.DeviceID,
				"disconnectTime":   event.Timestamp.Format(time.RFC3339),
				"fileTransfers":    transfers,
				"fileCount":        len(transfers),
			},
		})
	}
}

// handleUSBDeviceRequest handles requests from the server to scan USB devices
func (a *Agent) handleUSBDeviceRequest(msg *client.Message) error {
	log.Println("[USB] Received USB device scan request")

	if a.peripheralManager == nil {
		return a.client.SendResponse(msg.RequestID, false, nil, "Peripheral manager not available")
	}

	// Get current connected devices
	devices := a.peripheralManager.GetConnectedDevices()

	// Convert to serializable format
	deviceList := make([]map[string]interface{}, len(devices))
	for i, device := range devices {
		deviceList[i] = map[string]interface{}{
			"deviceId":       device.DeviceID,
			"instancePath":   device.InstancePath,
			"vendorId":       device.VendorID,
			"productId":      device.ProductID,
			"serialNumber":   device.SerialNumber,
			"manufacturer":   device.Manufacturer,
			"productName":    device.ProductName,
			"deviceClass":    string(device.DeviceClass),
			"classCode":      device.ClassCode,
			"subclassCode":   device.SubclassCode,
			"protocolCode":   device.ProtocolCode,
			"busNumber":      device.BusNumber,
			"portNumber":     device.PortNumber,
			"deviceSpeed":    device.DeviceSpeed,
			"parentDevice":   device.ParentDevice,
			"driveLetter":    device.DriveLetter,
			"mountPoint":     device.MountPoint,
			"volumeLabel":    device.VolumeLabel,
			"fileSystem":     device.FileSystem,
			"totalSize":      device.TotalSize,
			"freeSpace":      device.FreeSpace,
			"isConnected":    device.IsConnected,
			"connectionTime": device.ConnectionTime.Format(time.RFC3339),
			"isRemovable":    device.IsRemovable,
			"isBootable":     device.IsBootable,
			"isEncrypted":    device.IsEncrypted,
			"securityRisk":   peripheral.ClassifySecurityRisk(device),
		}
	}

	log.Printf("[USB] Returning %d connected devices", len(deviceList))

	return a.client.SendJSON(map[string]interface{}{
		"type":      client.MsgTypeUSBDeviceList,
		"requestId": msg.RequestID,
		"data": map[string]interface{}{
			"devices":   deviceList,
			"count":     len(deviceList),
			"timestamp": time.Now().Format(time.RFC3339),
		},
	})
}
