package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sentinel/agent/internal/config"
	"github.com/sentinel/agent/internal/offline"
	"github.com/sentinel/agent/internal/mtls"
	"github.com/sentinel/agent/internal/paths"
)

// schtaskExists returns true if a Windows scheduled task with the given name
// is currently registered. Used by collectLayerState() to populate Layer-4
// posture in heartbeats. Non-Windows: always false.
func schtaskExists(name string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	err := exec.Command("schtasks", "/query", "/tn", name).Run()
	return err == nil
}

// Message types
const (
	MsgTypeAuth               = "auth"
	MsgTypeAuthResponse       = "auth_response"
	MsgTypeHandshake          = "handshake"
	MsgTypeHeartbeat          = "heartbeat"
	MsgTypeHeartbeatAck       = "heartbeat_ack"
	MsgTypePing               = "ping"
	MsgTypePong               = "pong"
	MsgTypeMetrics            = "metrics"
	MsgTypeResponse           = "response"
	MsgTypeExecuteCmd         = "execute_command"
	MsgTypeExecuteScript      = "execute_script"
	MsgTypeStartTerminal      = "start_terminal"
	MsgTypeTerminalInput      = "terminal_input"
	MsgTypeTerminalOutput     = "terminal_output"
	MsgTypeTerminalResize     = "terminal_resize"
	MsgTypeCloseTerminal      = "close_terminal"
	MsgTypeListDrives         = "list_drives"
	MsgTypeListFiles          = "list_files"
	MsgTypeScanDirectory      = "scan_directory"
	MsgTypeDownloadFile       = "download_file"
	MsgTypeUploadFile         = "upload_file"
	MsgTypeFileData           = "file_data"
	MsgTypeScanProgress = "scan_progress"
	MsgTypeEvent        = "event"
	MsgTypeError              = "error"
	MsgTypeCollectDiagnostics = "collect_diagnostics"
	MsgTypeUninstallAgent     = "uninstall_agent"
	// WebRTC signaling messages
	MsgTypeWebRTCStart  = "webrtc_start"
	MsgTypeWebRTCSignal = "webrtc_signal"
	MsgTypeWebRTCStop   = "webrtc_stop"
	// Admin management messages
	MsgTypeAdminDiscover = "admin_discover"
	MsgTypeAdminDemote   = "admin_demote"
	MsgTypeAdminEvent    = "admin_event"
	// Configuration messages
	MsgTypeSetMetricsInterval = "set_metrics_interval"
	// Certificate management messages
	MsgTypeUpdateCertificate = "update_certificate"
	MsgTypeCertUpdateAck     = "cert_update_ack"
	// System update status
	MsgTypeUpdateStatus     = "update_status"
	MsgTypeForceUpdate      = "force_update"
	MsgTypeInstallUpdates   = "install_updates"
	MsgTypeInstallProgress  = "install_progress"
	// Sync protocol messages
	MsgTypeSyncRequest    = "sync_request"
	MsgTypeSyncResponse   = "sync_response"
	MsgTypeBulkMetrics    = "bulk_metrics"
	MsgTypeBulkMetricsAck = "bulk_metrics_ack"
	MsgTypeCommandResult  = "command_result"
	MsgTypeHealthReport   = "health_report"
	// Power management messages
	MsgTypePowerAction = "power_action"

	// USB/Peripheral device messages
	MsgTypeUSBDeviceEvent      = "usb_device_event"      // Report USB device events
	MsgTypeUSBDeviceList       = "usb_device_list"       // Report full USB device list
	MsgTypeUSBDeviceRequest    = "usb_device_request"    // Server requests device scan
	MsgTypeUSBPolicyUpdate     = "usb_policy_update"     // Receive policy updates
	MsgTypeUSBSessionComplete  = "usb_session_complete"  // USB session ended with file transfers
)

// Message represents a WebSocket message
type Message struct {
	Type      string      `json:"type"`
	RequestID string      `json:"requestId,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Payload   interface{} `json:"payload,omitempty"`
	Success   bool        `json:"success,omitempty"`
	Error     string      `json:"error,omitempty"`
	Timestamp string      `json:"timestamp,omitempty"`
}

// MessageHandler is a function that handles incoming messages
type MessageHandler func(msg *Message) error

// StorageInfo contains disk/partition information for auto-enrollment
type StorageInfo struct {
	Device     string  `json:"device"`
	Mountpoint string  `json:"mountpoint"`
	FSType     string  `json:"fstype"`
	Total      uint64  `json:"total"`
	Used       uint64  `json:"used"`
	Free       uint64  `json:"free"`
	Percent    float64 `json:"percent"`
}

// GPUInfo contains graphics card information for auto-enrollment
type GPUInfo struct {
	Name          string `json:"name"`
	Vendor        string `json:"vendor"`
	Memory        uint64 `json:"memory"`
	DriverVersion string `json:"driver_version"`
}

// DeviceInfo contains device information for auto-enrollment
type DeviceInfo struct {
	Hostname     string        `json:"hostname"`
	Platform     string        `json:"platform"`
	OSType       string        `json:"osType"`
	OSVersion    string        `json:"osVersion"`
	Architecture string        `json:"architecture"`
	CPUModel     string        `json:"cpuModel"`
	CPUCores     int           `json:"cpuCores"`
	TotalMemory  uint64        `json:"totalMemory"`
	SerialNumber string        `json:"serialNumber"`
	Manufacturer string        `json:"manufacturer"`
	Model        string        `json:"model"`
	IPAddress    string        `json:"ipAddress"`
	MACAddress   string        `json:"macAddress"`
	Storage      []StorageInfo `json:"storage,omitempty"`
	GPU          []GPUInfo     `json:"gpu,omitempty"`
}

// Client manages the WebSocket connection to the server
type Client struct {
	config            *config.Config
	conn              *websocket.Conn
	handlers          map[string]MessageHandler
	authenticated     bool
	connected         bool
	connectedSince    time.Time
	reconnectDelay    time.Duration
	maxReconnect      time.Duration
	mu                sync.RWMutex
	done              chan struct{}
	doneClosed        atomic.Bool
	sendQueue         chan []byte
	onConnect         func()
	onDisconnect      func()
	onNeedsEnrollment func()
	version           string
	lastPong          time.Time
	pingInterval      time.Duration
	pongTimeout       time.Duration
	healthPollRate    time.Duration
	httpClient        *http.Client
	offlineStore      *offline.Store
	lastDisconnect    time.Time
	wasOffline        bool
	deviceInfo         *DeviceInfo
	reconnectAttempt   int
	enrollmentFailures int       // consecutive NeedsEnrollment responses
	orphanBackoffUntil time.Time // when to next attempt after orphan detection
}

// New creates a new WebSocket client
func New(cfg *config.Config, version string) *Client {
	return &Client{
		config:         cfg,
		handlers:       make(map[string]MessageHandler),
		reconnectDelay: 1 * time.Second,
		maxReconnect:   60 * time.Second,
		done:           make(chan struct{}),
		sendQueue:      make(chan []byte, 1024),
		version:        version,
		pingInterval:   5 * time.Second,
		pongTimeout:    5 * time.Second,
		healthPollRate: 250 * time.Millisecond,
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}


// initHTTPClient initializes the HTTP client with mTLS configuration if available
func (c *Client) initHTTPClient() {
	// Get mTLS configuration
	tlsConfig, err := mtls.GetTLSConfig()
	if err != nil {
		log.Printf("[mTLS] Warning: Failed to load TLS config, using default: %v", err)
		if c.httpClient == nil {
			c.httpClient = &http.Client{
				Timeout: 2 * time.Second,
			}
		}
		return
	}

	// Create/update HTTP client with mTLS config
	c.httpClient = &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
	log.Println("[mTLS] HTTP client initialized with TLS configuration")
}

// SetDeviceInfo sets the device information for auto-enrollment
func (c *Client) SetDeviceInfo(info *DeviceInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deviceInfo = info
}

// OnConnect sets the callback for successful connection
func (c *Client) OnConnect(fn func()) {
	c.onConnect = fn
}

// OnDisconnect sets the callback for disconnection
func (c *Client) OnDisconnect(fn func()) {
	c.onDisconnect = fn
}

// OnNeedsEnrollment sets the callback for when server indicates device not found
func (c *Client) OnNeedsEnrollment(fn func()) {
	c.onNeedsEnrollment = fn
}

// RegisterHandler registers a message handler for a specific message type
func (c *Client) RegisterHandler(msgType string, handler MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[msgType] = handler
}

// getHealthURL returns the HTTP health check URL from the server URL
func (c *Client) getHealthURL() string {
	serverURL := c.config.ServerURL
	if serverURL == "" {
		return ""
	}

	// Ensure http:// or https:// prefix
	if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		if strings.HasPrefix(serverURL, "ws://") {
			serverURL = "http://" + serverURL[5:]
		} else if strings.HasPrefix(serverURL, "wss://") {
			serverURL = "https://" + serverURL[6:]
		} else {
			serverURL = "http://" + serverURL
		}
	}

	// Remove any path suffix and add /api/agent/version
	// Note: Using /api/agent/version instead of /health because port 8443
	// (mTLS agent port) only routes /api/agent and /ws/agent paths
	if idx := strings.Index(serverURL[8:], "/"); idx > 0 {
		serverURL = serverURL[:8+idx]
	}

	return serverURL + "/api/agent/version"
}

// checkServerHealth performs an HTTP health check to see if server is available
func (c *Client) checkServerHealth() bool {
	healthURL := c.getHealthURL()
	if healthURL == "" {
		return false
	}

	resp, err := c.httpClient.Get(healthURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// waitForServer polls the server until it becomes available
func (c *Client) waitForServer(ctx context.Context) bool {
	log.Println("Waiting for server to become available...")

	// Initialize HTTP client with TLS config before making health check requests
	c.initHTTPClient()

	// Try immediately first
	if c.checkServerHealth() {
		log.Println("Server is available!")
		return true
	}

	// Add jitter to health polling to prevent all agents polling in lockstep
	jitter := time.Duration(rand.Int63n(int64(500 * time.Millisecond)))
	ticker := time.NewTicker(c.healthPollRate + jitter)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if c.checkServerHealth() {
				log.Println("Server is available!")
				return true
			}
		}
	}
}

// backoffWithJitter returns a randomized exponential backoff duration.
// Full jitter: sleep = random(0, min(cap, base * 2^attempt))
// This prevents thundering herd when many agents reconnect simultaneously.
func (c *Client) backoffWithJitter() time.Duration {
	maxMs := float64(c.maxReconnect / time.Millisecond)
	baseMs := float64(c.reconnectDelay / time.Millisecond)
	expMs := math.Min(maxMs, baseMs*math.Pow(2, float64(c.reconnectAttempt)))
	jitteredMs := rand.Float64() * expMs
	if jitteredMs < float64(c.reconnectDelay/time.Millisecond) {
		jitteredMs = float64(c.reconnectDelay / time.Millisecond)
	}
	c.reconnectAttempt++
	return time.Duration(jitteredMs) * time.Millisecond
}

// Connect establishes a WebSocket connection to the server
func (c *Client) Connect(ctx context.Context) error {
	if c.config.ServerURL == "" {
		return fmt.Errorf("server URL not configured")
	}

	connMode := c.config.GetConnectionMode()

	// In auto or tunnel mode, try the Cloudflare tunnel path first (port 443, token auth)
	if connMode == config.ConnModeAuto || connMode == config.ConnModeTunnel {
		err := c.connectViaTunnel(ctx)
		if err == nil {
			return nil
		}
		if connMode == config.ConnModeTunnel {
			return fmt.Errorf("tunnel connection failed: %w", err)
		}
		log.Printf("[Tunnel] Connection failed, falling back to direct: %v", err)
	}

	// Direct mode: use existing mTLS / token auth flow
	wsURL := c.config.ServerURL

	// Check if we have mTLS certificates for certificate-based auth
	useMTLS := mtls.HasMTLS()

	// Convert http:// to ws:// if needed
	if len(wsURL) > 7 && wsURL[:7] == "http://" {
		wsURL = "ws://" + wsURL[7:]
	} else if len(wsURL) > 8 && wsURL[:8] == "https://" {
		wsURL = "wss://" + wsURL[8:]
	}

	// Determine endpoint based on mTLS availability
	if useMTLS {
		// Use mTLS endpoint - convert URL to use port 8443 and /ws/agent/mtls path
		wsURL = mtls.GetMTLSServerURL(wsURL)
		log.Printf("[mTLS] Using certificate-based authentication")
	} else {
		// Use standard endpoint for token auth
		if !strings.HasSuffix(wsURL, "/ws/agent") {
			if strings.HasSuffix(wsURL, "/ws") {
				wsURL = wsURL[:len(wsURL)-3]
			}
			wsURL = wsURL + "/ws/agent"
		}
	}

	log.Printf("Connecting to %s", wsURL)

	// Configure dialer with TLS if we have certificates
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	if useMTLS {
		tlsConfig, err := mtls.GetTLSConfig()
		if err != nil {
			// Fail closed (AG silent-downgrade): a client certificate is present,
			// so we must not silently revert to long-lived enrollment-token auth.
			return fmt.Errorf("mTLS is configured but the TLS config could not be loaded; refusing token fallback: %w", err)
		}
		dialer.TLSClientConfig = tlsConfig
	}

	headers := http.Header{}
	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		// Fail closed (AG silent-downgrade): when a client certificate exists we
		// refuse to fall back to token auth. Otherwise an on-path attacker could
		// block the mTLS port (8443) to force the weaker, long-lived
		// enrollment-token authentication.
		if useMTLS {
			return fmt.Errorf("mTLS connection failed and token fallback is refused because a client certificate is present: %w", err)
		}
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.connectedSince = time.Now()
	c.lastPong = time.Now()
	c.mu.Unlock()

	// Set up WebSocket-level pong handler to detect dead connections
	conn.SetPongHandler(func(appData string) error {
		c.mu.Lock()
		c.lastPong = time.Now()
		c.mu.Unlock()
		log.Println("Received WebSocket pong")
		return nil
	})

	log.Println("WebSocket connected")

	// For mTLS connections, server sends auth_response immediately without us sending auth
	// For token connections, we need to send auth message
	if !useMTLS {
		// Build auth payload with device info for auto-enrollment
		c.mu.RLock()
		deviceInfo := c.deviceInfo
		c.mu.RUnlock()

		authPayload := map[string]interface{}{
			"agentId":       c.config.AgentID,
			"token":         c.config.EnrollmentToken,
			"caCertHash":    paths.GetCACertHash(),
			"hasClientCert": mtls.HasMTLS(),
		}

		// Include device info if available (for auto-enrollment of orphaned agents)
		if deviceInfo != nil {
			authPayload["deviceInfo"] = deviceInfo
		}

		// Send auth message immediately (server expects auth first)
		authMsg := map[string]interface{}{
			"type":    MsgTypeAuth,
			"payload": authPayload,
		}
		authData, err := json.Marshal(authMsg)
		if err != nil {
			conn.Close()
			return fmt.Errorf("failed to marshal auth message: %w", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, authData); err != nil {
			conn.Close()
			return fmt.Errorf("failed to send auth message: %w", err)
		}
		log.Println("Auth message sent, waiting for response...")
	} else {
		log.Println("[mTLS] Certificate authenticated, waiting for server confirmation...")
	}

	// Start message handlers
	go c.readLoop(ctx)
	go c.writeLoop(ctx)
	go c.pingLoop(ctx)

	if c.onConnect != nil {
		c.onConnect()
	}

	return nil
}

// NOTE: The former connectWithToken() mTLS→token downgrade fallback was removed
// (AG silent-downgrade). Token authentication for the no-certificate enrollment
// case is handled inline by Connect() when useMTLS is false; once a client
// certificate exists, direct-mode connection failures fail closed rather than
// silently reverting to long-lived token auth.

// connectViaTunnel connects through the Cloudflare tunnel on port 443 using token auth.
// This uses standard HTTPS with system root CAs (CF provides a publicly-signed cert).
func (c *Client) connectViaTunnel(ctx context.Context) error {
	// Build tunnel URL: wss://hostname:443/ws/agent
	parsed, err := url.Parse(c.config.ServerURL)
	if err != nil {
		return fmt.Errorf("failed to parse server URL: %w", err)
	}

	tunnelURL := fmt.Sprintf("wss://%s/ws/agent", parsed.Hostname())
	log.Printf("[Tunnel] Connecting via Cloudflare tunnel: %s", tunnelURL)

	// Use system root CAs — Cloudflare presents a publicly-signed certificate
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// RootCAs: nil means use system root CA pool
		},
	}

	conn, _, err := dialer.DialContext(ctx, tunnelURL, nil)
	if err != nil {
		return fmt.Errorf("tunnel dial failed: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.connectedSince = time.Now()
	c.lastPong = time.Now()
	c.mu.Unlock()

	conn.SetPongHandler(func(appData string) error {
		c.mu.Lock()
		c.lastPong = time.Now()
		c.mu.Unlock()
		return nil
	})

	// Token auth — same as connectWithToken
	c.mu.RLock()
	deviceInfo := c.deviceInfo
	c.mu.RUnlock()

	authPayload := map[string]interface{}{
		"agentId":       c.config.AgentID,
		"token":         c.config.EnrollmentToken,
		"caCertHash":    paths.GetCACertHash(),
		"hasClientCert": mtls.HasMTLS(),
	}
	if deviceInfo != nil {
		authPayload["deviceInfo"] = deviceInfo
	}

	authMsg := map[string]interface{}{
		"type":    MsgTypeAuth,
		"payload": authPayload,
	}
	authData, _ := json.Marshal(authMsg)
	if err := conn.WriteMessage(websocket.TextMessage, authData); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send auth: %w", err)
	}

	log.Printf("[Tunnel] Connected via Cloudflare tunnel, auth sent")

	go c.readLoop(ctx)
	go c.writeLoop(ctx)
	go c.pingLoop(ctx)

	if c.onConnect != nil {
		c.onConnect()
	}

	return nil
}

// pingLoop sends WebSocket-level ping frames to detect dead connections
func (c *Client) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(c.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.RLock()
			conn := c.conn
			connected := c.connected
			lastPong := c.lastPong
			c.mu.RUnlock()

			if !connected || conn == nil {
				return
			}

			// Check if we've received a pong recently
			if time.Since(lastPong) > c.pingInterval+c.pongTimeout {
				log.Printf("No pong received for %v, connection appears dead", time.Since(lastPong))
				c.forceClose()
				return
			}

			// Send WebSocket-level ping
			if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second)); err != nil {
				log.Printf("Failed to send ping: %v", err)
				c.forceClose()
				return
			}
			log.Println("Sent WebSocket ping")
		}
	}
}

// forceClose forcefully closes the connection to trigger reconnection
func (c *Client) forceClose() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connected = false
	c.authenticated = false

	// Signal done to trigger reconnection (safe close)
	c.signalDone()
}

// signalDone safely closes the done channel to trigger reconnection
// Uses atomic flag to prevent double-close panic
func (c *Client) signalDone() {
	if c.doneClosed.CompareAndSwap(false, true) {
		close(c.done)
	}
}

// Authenticate sends authentication message to the server
func (c *Client) Authenticate() error {
	authPayload := map[string]interface{}{
		"agentId":    c.config.AgentID,
		"token":      c.config.EnrollmentToken,
		"caCertHash": paths.GetCACertHash(),
	}

	msg := map[string]interface{}{
		"type":    MsgTypeAuth,
		"payload": authPayload,
	}

	return c.SendJSON(msg)
}

// SendJSON sends a JSON message through the WebSocket
func (c *Client) SendJSON(v interface{}) error {
	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()

	if !connected {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	select {
	case c.sendQueue <- data:
		return nil
	default:
		return fmt.Errorf("send queue full")
	}
}

// SendResponse sends a response to a request
func (c *Client) SendResponse(requestID string, success bool, data interface{}, errMsg string) error {
	msg := map[string]interface{}{
		"type":      MsgTypeResponse,
		"requestId": requestID,
		"success":   success,
		"data":      data,
		"error":     errMsg,
	}
	return c.SendJSON(msg)
}

// SendMetrics sends system metrics to the server
func (c *Client) SendMetrics(metrics interface{}) error {
	msg := map[string]interface{}{
		"type": MsgTypeMetrics,
		"data": metrics,
	}
	return c.SendJSON(msg)
}

// SendHeartbeat sends a heartbeat message with device info for orphaned agent recovery
func (c *Client) SendHeartbeat() error {
	msg := map[string]interface{}{
		"type":         MsgTypeHeartbeat,
		"timestamp":    time.Now().Format(time.RFC3339),
		"agentVersion": c.version,
	}
	// Include device info if available (helps with orphaned agent re-enrollment)
	c.mu.RLock()
	if c.deviceInfo != nil {
		msg["hostname"] = c.deviceInfo.Hostname
		msg["osType"] = c.deviceInfo.OSType
		msg["osVersion"] = c.deviceInfo.OSVersion
		msg["platform"] = c.deviceInfo.Platform
		msg["architecture"] = c.deviceInfo.Architecture
	}
	c.mu.RUnlock()
	// Recovery-posture self-report (PR #18). Server populates agent_health from
	// this so the silent-agent detector can pick the right heal action.
	msg["layer_state"] = c.collectLayerState()
	return c.SendJSON(msg)
}

// collectLayerState gathers what the agent can cheaply observe about its own
// recovery layers. Each field is optional — server's upsertAgentHealth uses
// COALESCE so omitted fields preserve prior values.
func (c *Client) collectLayerState() map[string]interface{} {
	state := map[string]interface{}{
		"mtls_cert_present": mtls.HasMTLS(),
	}
	// Layer 1 (WS uptime) — derive from authenticatedAt if we tracked one.
	c.mu.RLock()
	if !c.connectedSince.IsZero() {
		state["layer1_ws_uptime_secs"] = int64(time.Since(c.connectedSince).Seconds())
	}
	c.mu.RUnlock()
	// Layer 4 (scheduled task present) — Windows only; cheap registry-style
	// check via schtasks. Skip on non-Windows where Layer 4 is a cron job.
	if runtime.GOOS == "windows" {
		state["layer4_schtask_present"] = schtaskExists("SentinelBootstrapRecovery")
	}
	return state
}

// SendTerminalOutput sends terminal output to the server
func (c *Client) SendTerminalOutput(sessionID string, data string) error {
	msg := map[string]interface{}{
		"type":      MsgTypeTerminalOutput,
		"sessionId": sessionID,
		"data":      data,
	}
	return c.SendJSON(msg)
}

// SendEvent sends an event notification to the server
func (c *Client) SendEvent(severity, title, message string) error {
	msg := map[string]interface{}{
		"type": MsgTypeEvent,
		"event": map[string]interface{}{
			"severity": severity,
			"title":    title,
			"message":  message,
		},
	}
	return c.SendJSON(msg)
}

// readLoop handles incoming messages
func (c *Client) readLoop(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		c.connected = false
		c.authenticated = false
		c.lastDisconnect = time.Now()
		c.wasOffline = true
		// Signal done channel to trigger reconnection in RunWithReconnect
		c.signalDone()
		c.mu.Unlock()

		if c.onDisconnect != nil {
			c.onDisconnect()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		default:
		}

		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		if conn == nil {
			return
		}

		// Set read deadline to detect dead connections
		conn.SetReadDeadline(time.Now().Add(130 * time.Second))

		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			} else {
				log.Printf("WebSocket read error (timeout or closed): %v", err)
			}
			return
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("Failed to parse message: %v", err)
			continue
		}

		// Normalize message: server sends data in Payload, handlers expect Data
		// Copy Payload to Data if Data is nil to ensure handlers work correctly
		if msg.Data == nil && msg.Payload != nil {
			msg.Data = msg.Payload
		}

		// Handle authentication response
		if msg.Type == MsgTypeAuthResponse {
			// Server sends success/error inside Payload field
			var authResp struct {
				Success         bool   `json:"success"`
				Error           string `json:"error"`
				NeedsEnrollment bool   `json:"needsEnrollment"`
				// Certificate fields (sent during enrollment when PKI is enabled)
				ClientCert    string `json:"clientCert"`
				ClientKey     string `json:"clientKey"`
				CACert        string `json:"caCert"`
				CertExpiresAt string `json:"certExpiresAt"`
				CertSerial    string `json:"certSerial"`
				MTLSAuth      bool   `json:"mtlsAuth"` // True if authenticated via mTLS
			}
			// Try to parse Payload if present
			if msg.Payload != nil {
				if payloadBytes, err := json.Marshal(msg.Payload); err == nil {
					json.Unmarshal(payloadBytes, &authResp)
				}
			}
			// Fall back to top-level Success/Error for backwards compatibility
			if !authResp.Success && msg.Success {
				authResp.Success = true
			}
			if authResp.Error == "" && msg.Error != "" {
				authResp.Error = msg.Error
			}

			if authResp.Success {
				c.mu.Lock()
				c.authenticated = true
				c.enrollmentFailures = 0
				c.orphanBackoffUntil = time.Time{} // reset orphan backoff
				c.mu.Unlock()

				if authResp.MTLSAuth {
					log.Println("[mTLS] Authentication successful via client certificate")
				} else {
					log.Println("Authentication successful")
				}

				// Install certificates if server provided them. Verbose logging
				// here is intentional — silent failure of this branch on
				// PS-BSIKORA-LT (2026-05-22) burned days because we couldn't tell
				// whether the cert was issued, sent, received, or persisted.
				if authResp.ClientCert != "" && authResp.ClientKey != "" {
					log.Printf("[mTLS] Received cert from server (serial=%s, expires=%s, certLen=%d, keyLen=%d, caLen=%d)",
						authResp.CertSerial, authResp.CertExpiresAt,
						len(authResp.ClientCert), len(authResp.ClientKey), len(authResp.CACert))
					err := mtls.InstallCertificates(
						[]byte(authResp.ClientCert),
						[]byte(authResp.ClientKey),
						[]byte(authResp.CACert),
					)
					if err != nil {
						log.Printf("[mTLS] ERROR installing cert: %v (serial=%s) — will retry on next auth", err, authResp.CertSerial)
					} else {
						log.Printf("[mTLS] Client certificate installed (serial=%s); next connection will use mTLS auth", authResp.CertSerial)
					}
				} else if authResp.Success && !authResp.MTLSAuth && !mtls.HasMTLS() {
					// We authenticated with token, server should have issued a
					// cert (we don't have one and didn't authenticate via mTLS),
					// but the response carried no cert bytes. Either the server
					// considers us already-cert'd (stale HasClientCert advertisement
					// from us), or PKI issuance failed server-side. Surface this
					// loudly so it isn't a silent "we'll just stay on token auth
					// forever" state.
					log.Printf("[mTLS] WARNING: auth succeeded via token but no cert in response (mtlsAuth=%v, hasMTLS=%v) — agent will operate without mTLS until next reconnect",
						authResp.MTLSAuth, mtls.HasMTLS())
				}

				// Check if server says we need to re-enroll
				if authResp.NeedsEnrollment {
					c.mu.Lock()
					c.enrollmentFailures++
					failures := c.enrollmentFailures
					c.mu.Unlock()

					if failures <= 3 {
						// First 3 attempts: try re-enrollment normally (may be transient)
						log.Printf("Server indicates device not found (attempt %d/3) - triggering re-enrollment", failures)
						c.mu.RLock()
						cb := c.onNeedsEnrollment
						c.mu.RUnlock()
						if cb != nil {
							go cb()
						}
					} else {
						// Orphan state: escalating backoff to reduce server load
						backoffMin := failures - 3 // starts at 1, grows linearly
						if backoffMin > 12 {
							backoffMin = 12 // cap at 60 minutes
						}
						backoff := time.Duration(backoffMin) * 5 * time.Minute
						log.Printf("[Orphan] Device not recognized after %d attempts. Backing off %v before retry.", failures, backoff)

						c.mu.Lock()
						c.orphanBackoffUntil = time.Now().Add(backoff)
						c.mu.Unlock()

						// Still try re-enrollment callback (admin may have re-added the device)
						c.mu.RLock()
						cb := c.onNeedsEnrollment
						c.mu.RUnlock()
						if cb != nil {
							go cb()
						}
					}
				}
			} else {
				log.Printf("Authentication failed: %s", authResp.Error)
			}
			continue
		}

		// Handle handshake
		if msg.Type == MsgTypeHandshake {
			log.Println("Received handshake, authenticating...")
			if err := c.Authenticate(); err != nil {
				log.Printf("Failed to authenticate: %v", err)
			}
			continue
		}

		// Dispatch to registered handler
		c.mu.RLock()
		handler, ok := c.handlers[msg.Type]
		c.mu.RUnlock()

		if ok {
			go func(m Message) {
				if err := handler(&m); err != nil {
					log.Printf("Handler error for %s: %v", m.Type, err)
				}
			}(msg)
		} else {
			log.Printf("No handler for message type: %s", msg.Type)
		}
	}
}

// writeLoop handles outgoing messages
func (c *Client) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case data := <-c.sendQueue:
			c.mu.RLock()
			conn := c.conn
			c.mu.RUnlock()

			if conn == nil {
				continue
			}

			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("Write error: %v", err)
				return
			}
		}
	}
}

// Close closes the WebSocket connection
func (c *Client) Close() error {
	c.signalDone()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		err := c.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		if err != nil {
			log.Printf("Error sending close message: %v", err)
		}
		c.conn.Close()
		c.conn = nil
	}

	c.connected = false
	c.authenticated = false
	return nil
}

// IsConnected returns the connection status
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// IsAuthenticated returns the authentication status
func (c *Client) IsAuthenticated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.authenticated
}

// RunWithReconnect maintains a persistent connection with automatic reconnection
// Uses HTTP health polling to detect server availability for immediate connection
func (c *Client) RunWithReconnect(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Reset done channel before connecting
		c.mu.Lock()
		select {
		case <-c.done:
			c.done = make(chan struct{})
			c.doneClosed.Store(false)
		default:
		}
		c.mu.Unlock()

		// Phase 1: Wait for server to be available via HTTP health check
		// This ensures we connect IMMEDIATELY when server starts
		if !c.waitForServer(ctx) {
			return // Context cancelled
		}

		// Check orphan backoff — if we're in orphan state, wait before reconnecting
		c.mu.RLock()
		backoffUntil := c.orphanBackoffUntil
		c.mu.RUnlock()
		if !backoffUntil.IsZero() && time.Now().Before(backoffUntil) {
			wait := time.Until(backoffUntil)
			log.Printf("[Orphan] Waiting %v before next connection attempt...", wait.Round(time.Second))
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}

		// Phase 2: Connect WebSocket immediately after server is detected
		log.Println("Attempting WebSocket connection...")
		err := c.Connect(ctx)
		if err != nil {
			log.Printf("WebSocket connection failed: %v, retrying...", err)
			// Exponential backoff with jitter before retry
			select {
			case <-ctx.Done():
				return
			case <-time.After(c.backoffWithJitter()):
			}
			continue
		}

		log.Println("Connection established successfully")
		c.reconnectAttempt = 0

		// Phase 3: Wait for disconnection
		<-c.done

		log.Println("Disconnected, checking server availability...")
	}
}

// SendWebRTCSignal sends a WebRTC signaling message (SDP offer/answer or ICE candidate)
func (c *Client) SendWebRTCSignal(sessionID, signalType, sdp, candidate string) error {
	msg := map[string]interface{}{
		"type":      MsgTypeWebRTCSignal,
		"sessionId": sessionID,
		"signal": map[string]interface{}{
			"type":      signalType,
			"sessionId": sessionID,
			"sdp":       sdp,
			"candidate": candidate,
		},
	}
	return c.SendJSON(msg)
}

// SendAdminDiscovery sends the admin discovery results to the server
func (c *Client) SendAdminDiscovery(requestID string, admins interface{}, safetyCheck interface{}) error {
	msg := map[string]interface{}{
		"type":      MsgTypeAdminDiscover,
		"requestId": requestID,
		"data": map[string]interface{}{
			"admins":      admins,
			"safetyCheck": safetyCheck,
		},
	}
	return c.SendJSON(msg)
}

// SendAdminDemotionResult sends the result of an admin demotion operation
func (c *Client) SendAdminDemotionResult(requestID string, result interface{}) error {
	msg := map[string]interface{}{
		"type":      MsgTypeResponse,
		"requestId": requestID,
		"success":   true,
		"data":      result,
	}
	return c.SendJSON(msg)
}

// SendAdminEvent sends an admin management event (for telemetry)
func (c *Client) SendAdminEvent(event interface{}) error {
	msg := map[string]interface{}{
		"type":  MsgTypeAdminEvent,
		"event": event,
	}
	return c.SendJSON(msg)
}

// SendCertUpdateAck sends a certificate update acknowledgment to the server
func (c *Client) SendCertUpdateAck(certHash string, success bool, errMsg string) error {
	msg := map[string]interface{}{
		"type": MsgTypeCertUpdateAck,
		"data": map[string]interface{}{
			"certHash": certHash,
			"success":  success,
			"error":    errMsg,
		},
	}
	return c.SendJSON(msg)
}

// SendUpdateStatus sends system update status to the server
func (c *Client) SendUpdateStatus(status interface{}) error {
	msg := map[string]interface{}{
		"type": MsgTypeUpdateStatus,
		"data": status,
	}
	return c.SendJSON(msg)
}

// SetOfflineStore sets the offline store for caching data during disconnections
func (c *Client) SetOfflineStore(store *offline.Store) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offlineStore = store
}

// GetOfflineStore returns the offline store
func (c *Client) GetOfflineStore() *offline.Store {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.offlineStore
}

// MarkDisconnected records the disconnection time for offline duration tracking
func (c *Client) MarkDisconnected() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastDisconnect = time.Now()
	c.wasOffline = true
}

// SendSyncRequest sends a sync request after reconnection
func (c *Client) SendSyncRequest() error {
	c.mu.RLock()
	store := c.offlineStore
	lastDisconnect := c.lastDisconnect
	wasOffline := c.wasOffline
	c.mu.RUnlock()

	if !wasOffline {
		return nil // Not reconnecting from offline state
	}

	var offlineDuration string
	if !lastDisconnect.IsZero() {
		offlineDuration = time.Since(lastDisconnect).String()
	}

	var cachedMetricsCount, cachedEventsCount, cachedCommandsCount int
	if store != nil {
		cachedMetricsCount = store.GetMetricsCount()
		cachedEventsCount = store.GetEventsCount()
		cachedCommandsCount = store.GetPendingCommandsCount()
	}

	log.Printf("[Sync] Sending sync request after %s offline (metrics: %d, events: %d, commands: %d)",
		offlineDuration, cachedMetricsCount, cachedEventsCount, cachedCommandsCount)

	msg := map[string]interface{}{
		"type": MsgTypeSyncRequest,
		"data": map[string]interface{}{
			"offlineDuration":     offlineDuration,
			"cachedMetricsCount":  cachedMetricsCount,
			"cachedEventsCount":   cachedEventsCount,
			"cachedCommandsCount": cachedCommandsCount,
		},
	}

	err := c.SendJSON(msg)
	if err == nil {
		c.mu.Lock()
		c.wasOffline = false
		c.mu.Unlock()
	}
	return err
}

// SendBulkMetrics sends cached metrics to the server in batches
func (c *Client) SendBulkMetrics(batchID string, metrics []map[string]interface{}) error {
	msg := map[string]interface{}{
		"type": MsgTypeBulkMetrics,
		"data": map[string]interface{}{
			"batchId": batchID,
			"metrics": metrics,
		},
	}
	return c.SendJSON(msg)
}

// SendCommandResult sends the result of a command execution
func (c *Client) SendCommandResult(commandID string, success bool, result interface{}, errMsg string) error {
	msg := map[string]interface{}{
		"type": MsgTypeCommandResult,
		"data": map[string]interface{}{
			"commandId": commandID,
			"success":   success,
			"result":    result,
			"error":     errMsg,
		},
	}
	return c.SendJSON(msg)
}

// SendHealthReport sends the agent's health status to the server
func (c *Client) SendHealthReport(score int, status string, factors, components map[string]interface{}) error {
	msg := map[string]interface{}{
		"type": MsgTypeHealthReport,
		"data": map[string]interface{}{
			"score":      score,
			"status":     status,
			"factors":    factors,
			"components": components,
			"timestamp":  time.Now().Format(time.RFC3339),
		},
	}
	return c.SendJSON(msg)
}

// QueueMetricsIfOffline queues metrics for later sync if not connected
func (c *Client) QueueMetricsIfOffline(metrics interface{}) (bool, error) {
	c.mu.RLock()
	connected := c.connected
	store := c.offlineStore
	c.mu.RUnlock()

	if connected || store == nil {
		return false, nil // Not offline or no store
	}

	// Marshal metrics to JSON
	data, err := json.Marshal(metrics)
	if err != nil {
		return false, fmt.Errorf("failed to marshal metrics: %w", err)
	}

	// Queue the metrics
	if err := store.QueueMetrics(data, offline.PriorityNormal); err != nil {
		return false, fmt.Errorf("failed to queue metrics: %w", err)
	}

	return true, nil
}

// QueueEventIfOffline queues an event for later sync if not connected
func (c *Client) QueueEventIfOffline(eventType, severity string, payload interface{}) (bool, error) {
	c.mu.RLock()
	connected := c.connected
	store := c.offlineStore
	c.mu.RUnlock()

	if connected || store == nil {
		return false, nil // Not offline or no store
	}

	// Marshal payload to JSON
	data, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("failed to marshal event payload: %w", err)
	}

	// Queue the event
	if err := store.QueueEvent(eventType, severity, data); err != nil {
		return false, fmt.Errorf("failed to queue event: %w", err)
	}

	return true, nil
}

// UploadCachedMetrics uploads all cached metrics to the server
func (c *Client) UploadCachedMetrics(ctx context.Context, batchSize int) error {
	c.mu.RLock()
	store := c.offlineStore
	c.mu.RUnlock()

	if store == nil {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Get a batch of cached metrics
		entries, err := store.GetPendingMetrics(batchSize)
		if err != nil {
			return fmt.Errorf("failed to get cached metrics: %w", err)
		}

		if len(entries) == 0 {
			return nil // All done
		}

		// Convert to the format expected by the server
		metrics := make([]map[string]interface{}, 0, len(entries))
		ids := make([]int64, 0, len(entries))
		for _, entry := range entries {
			var data map[string]interface{}
			if err := json.Unmarshal(entry.Payload, &data); err != nil {
				log.Printf("[Sync] Failed to unmarshal cached metric: %v", err)
				continue
			}
			metrics = append(metrics, map[string]interface{}{
				"timestamp": entry.Timestamp.Format(time.RFC3339),
				"data":      data,
			})
			ids = append(ids, entry.ID)
		}

		if len(metrics) == 0 {
			continue
		}

		// Send the batch
		batchID := fmt.Sprintf("batch-%d", time.Now().UnixNano())
		if err := c.SendBulkMetrics(batchID, metrics); err != nil {
			return fmt.Errorf("failed to send bulk metrics: %w", err)
		}

		// Mark as synced
		for _, id := range ids {
			if err := store.MarkMetricsSynced(id); err != nil {
				log.Printf("[Sync] Failed to mark metric %d as synced: %v", id, err)
			}
		}

		log.Printf("[Sync] Uploaded %d cached metrics", len(metrics))
	}
}
