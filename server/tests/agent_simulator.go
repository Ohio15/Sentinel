// Package tests provides integration test utilities for the Sentinel server.
// AgentSimulator connects to the Sentinel server as a fake agent over WebSocket,
// enabling end-to-end testing of the agent lifecycle (connect, heartbeat, disconnect, replacement).
package tests

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// AgentSimulator — simulates a real Sentinel agent connecting via WebSocket
// ---------------------------------------------------------------------------

// AgentSimulator is a lightweight client that mimics a real Sentinel agent for
// integration testing. It connects to /ws/agent, authenticates with an enrollment
// token, and can send heartbeats and receive server messages.
type AgentSimulator struct {
	ServerURL string // e.g. "http://localhost:8091"
	AgentID   string // Hardware-fingerprint-style UUID
	Token     string // Enrollment token (hex string)
	Hostname  string // Simulated hostname

	conn          *websocket.Conn
	connected     bool
	stopHeartbeat chan struct{}
	messages      chan []byte // received messages buffer
	mu            sync.Mutex
	readDone      chan struct{} // closed when readPump exits
}

// authMessage is the first message the agent sends after WebSocket upgrade.
type authMessage struct {
	Type    string      `json:"type"`
	Payload authPayload `json:"payload"`
}

type authPayload struct {
	AgentID    string     `json:"agentId"`
	Token      string     `json:"token"`
	DeviceInfo deviceInfo `json:"deviceInfo"`
}

type deviceInfo struct {
	Hostname     string `json:"hostname"`
	Platform     string `json:"platform"`
	OSType       string `json:"osType"`
	OSVersion    string `json:"osVersion"`
	Architecture string `json:"architecture"`
	CPUModel     string `json:"cpuModel"`
	CPUCores     int    `json:"cpuCores"`
	TotalMemory  uint64 `json:"totalMemory"`
	IPAddress    string `json:"ipAddress"`
	MACAddress   string `json:"macAddress"`
}

type wsMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// NewAgentSimulator creates a new agent simulator. It does NOT connect until
// Connect() is called.
func NewAgentSimulator(serverURL, agentID, token, hostname string) *AgentSimulator {
	return &AgentSimulator{
		ServerURL: serverURL,
		AgentID:   agentID,
		Token:     token,
		Hostname:  hostname,
		messages:  make(chan []byte, 256),
	}
}

// Connect establishes a WebSocket connection, sends the auth message, and
// waits for the auth_response. Returns an error if the server rejects the
// connection or if the auth exchange fails.
func (a *AgentSimulator) Connect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	// Build the WebSocket URL from the HTTP server URL.
	wsURL, err := httpToWS(a.ServerURL, "/ws/agent")
	if err != nil {
		return fmt.Errorf("bad server URL: %w", err)
	}

	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}
	a.conn = conn

	// Send the auth message
	auth := authMessage{
		Type: "auth",
		Payload: authPayload{
			AgentID: a.AgentID,
			Token:   a.Token,
			DeviceInfo: deviceInfo{
				Hostname:     a.Hostname,
				Platform:     "windows",
				OSType:       "Windows",
				OSVersion:    "10.0.22631",
				Architecture: "x64",
				CPUModel:     "Test CPU (simulated)",
				CPUCores:     4,
				TotalMemory:  8589934592,
				IPAddress:    "192.168.1.200",
				MACAddress:   fmt.Sprintf("AA:BB:CC:%02X:%02X:%02X", time.Now().UnixNano()&0xFF, (time.Now().UnixNano()>>8)&0xFF, (time.Now().UnixNano()>>16)&0xFF),
			},
		},
	}

	if err := conn.WriteJSON(auth); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send auth: %w", err)
	}

	// Read auth response (with deadline)
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to read auth_response: %w", err)
	}
	conn.SetReadDeadline(time.Time{}) // clear deadline

	var resp wsMessage
	if err := json.Unmarshal(raw, &resp); err != nil {
		conn.Close()
		return fmt.Errorf("invalid auth_response JSON: %w", err)
	}
	if resp.Type != "auth_response" {
		conn.Close()
		return fmt.Errorf("expected auth_response, got %q", resp.Type)
	}

	var payload struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		conn.Close()
		return fmt.Errorf("invalid auth_response payload: %w", err)
	}
	if !payload.Success {
		conn.Close()
		return fmt.Errorf("auth rejected: %s", payload.Error)
	}

	a.connected = true
	a.readDone = make(chan struct{})

	// Background goroutine to drain incoming messages
	go a.readPump()

	return nil
}

// readPump reads messages from the server and pushes them into the messages channel.
func (a *AgentSimulator) readPump() {
	defer close(a.readDone)
	for {
		_, msg, err := a.conn.ReadMessage()
		if err != nil {
			return
		}
		select {
		case a.messages <- msg:
		default:
			// Drop if buffer full — test can still function
		}
	}
}

// Disconnect cleanly closes the WebSocket connection.
func (a *AgentSimulator) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	a.StopHeartbeatsLocked()

	// Send close frame, then close
	_ = a.conn.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
	)
	err := a.conn.Close()
	a.connected = false

	// Wait for readPump to exit
	if a.readDone != nil {
		select {
		case <-a.readDone:
		case <-time.After(3 * time.Second):
		}
	}

	return err
}

// Reconnect disconnects and reconnects with the same agent ID and token.
func (a *AgentSimulator) Reconnect() error {
	if err := a.Disconnect(); err != nil {
		return fmt.Errorf("disconnect before reconnect: %w", err)
	}
	time.Sleep(200 * time.Millisecond) // brief pause to let server process disconnect
	return a.Connect()
}

// ReconnectWithNewID disconnects and reconnects with a different agent ID,
// simulating a fresh agent install on the same machine.
func (a *AgentSimulator) ReconnectWithNewID(newAgentID string) error {
	if err := a.Disconnect(); err != nil {
		return fmt.Errorf("disconnect before reconnect: %w", err)
	}
	a.mu.Lock()
	a.AgentID = newAgentID
	a.mu.Unlock()
	time.Sleep(200 * time.Millisecond)
	return a.Connect()
}

// SendHeartbeat sends a single heartbeat message.
func (a *AgentSimulator) SendHeartbeat() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected || a.conn == nil {
		return fmt.Errorf("not connected")
	}

	hb := map[string]interface{}{
		"type":      "heartbeat",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	return a.conn.WriteJSON(hb)
}

// StartHeartbeats begins sending heartbeats at the given interval. The
// goroutine runs until StopHeartbeats() or Disconnect() is called.
func (a *AgentSimulator) StartHeartbeats(interval time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.startHeartbeatsLocked(interval)
}

func (a *AgentSimulator) startHeartbeatsLocked(interval time.Duration) {
	if a.stopHeartbeat != nil {
		return // already running
	}
	a.stopHeartbeat = make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-a.stopHeartbeat:
				return
			case <-ticker.C:
				_ = a.SendHeartbeat()
			}
		}
	}()
}

// StopHeartbeats stops the heartbeat goroutine if running.
func (a *AgentSimulator) StopHeartbeats() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.StopHeartbeatsLocked()
}

// StopHeartbeatsLocked stops heartbeats — caller must hold a.mu.
func (a *AgentSimulator) StopHeartbeatsLocked() {
	if a.stopHeartbeat != nil {
		close(a.stopHeartbeat)
		a.stopHeartbeat = nil
	}
}

// WaitForMessage waits for a message of the given type within timeout.
// Returns the raw JSON bytes or an error if the timeout expires.
func (a *AgentSimulator) WaitForMessage(msgType string, timeout time.Duration) ([]byte, error) {
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			return nil, fmt.Errorf("timeout waiting for message type %q", msgType)
		case msg := <-a.messages:
			var m wsMessage
			if err := json.Unmarshal(msg, &m); err != nil {
				continue
			}
			if m.Type == msgType {
				return msg, nil
			}
			// Not the type we want — keep draining
		}
	}
}

// IsConnected returns true if the agent is currently connected.
func (a *AgentSimulator) IsConnected() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.connected
}

// Destroy tears down the simulator: stops heartbeats, closes the connection,
// and drains the message channel.
func (a *AgentSimulator) Destroy() {
	_ = a.Disconnect()
	// Drain remaining messages
	for {
		select {
		case <-a.messages:
		default:
			return
		}
	}
}

// ---------------------------------------------------------------------------
// TestAPIClient — HTTP helper for calling the Sentinel REST API in tests
// ---------------------------------------------------------------------------

// TestAPIClient is a test helper that authenticates against the Sentinel REST
// API and provides convenience methods for enrollment tokens, devices, and
// kill tokens.
type TestAPIClient struct {
	BaseURL   string
	Token     string // JWT access token
	CSRFToken string // CSRF token from login
	Client    *http.Client
	cookies   []*http.Cookie // Cookies from login response (includes csrf_token cookie)
}

// NewTestAPIClient creates a new unauthenticated API client.
func NewTestAPIClient(baseURL string) *TestAPIClient {
	jar, _ := cookiejar.New(nil)
	return &TestAPIClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Client: &http.Client{
			Timeout: 15 * time.Second,
			Jar:     jar,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

// Login authenticates with email/password and stores the JWT and CSRF tokens.
func (c *TestAPIClient) Login(email, password string) error {
	body := map[string]string{
		"identifier": email,
		"password":   password,
	}
	jsonBody, _ := json.Marshal(body)

	resp, err := c.Client.Post(c.BaseURL+"/api/auth/login", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		CSRFToken    string `json:"csrfToken"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse login response: %w", err)
	}
	if result.AccessToken == "" {
		return fmt.Errorf("login returned empty access token")
	}

	c.Token = result.AccessToken
	c.CSRFToken = result.CSRFToken
	c.cookies = resp.Cookies()

	// Store cookies in jar for the base URL
	u, _ := url.Parse(c.BaseURL)
	c.Client.Jar.SetCookies(u, resp.Cookies())

	return nil
}

// doRequest is an internal helper that attaches auth + CSRF headers.
func (c *TestAPIClient) doRequest(method, path string, body interface{}) (*http.Response, []byte, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBytes)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.CSRFToken != "" {
		req.Header.Set("X-CSRF-Token", c.CSRFToken)
	}

	// Attach csrf_token cookie
	for _, cookie := range c.cookies {
		req.AddCookie(cookie)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp, respBody, nil
}

// CreateEnrollmentToken creates a new enrollment token via the admin API.
// Returns the token database ID and the plaintext token value.
func (c *TestAPIClient) CreateEnrollmentToken(name string) (tokenID string, token string, err error) {
	resp, body, err := c.doRequest("POST", "/api/enrollment-tokens", map[string]interface{}{
		"name":        name,
		"description": "Integration test token",
	})
	if err != nil {
		return "", "", fmt.Errorf("create enrollment token request: %w", err)
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("create enrollment token failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("parse create enrollment token response: %w", err)
	}
	return result.ID, result.Token, nil
}

// ListDevices returns the list of all devices. The response is the paginated
// "data" array from GET /api/devices.
func (c *TestAPIClient) ListDevices() ([]map[string]interface{}, error) {
	resp, body, err := c.doRequest("GET", "/api/devices?pageSize=500", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list devices failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse list devices: %w", err)
	}
	return result.Data, nil
}

// GetDevice returns a single device by its UUID.
func (c *TestAPIClient) GetDevice(id string) (map[string]interface{}, error) {
	resp, body, err := c.doRequest("GET", "/api/devices/"+id, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get device failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse get device: %w", err)
	}
	return result, nil
}

// FindDeviceByHostname searches the device list for a device with the given hostname.
// Returns nil if not found.
func (c *TestAPIClient) FindDeviceByHostname(hostname string) (map[string]interface{}, error) {
	devices, err := c.ListDevices()
	if err != nil {
		return nil, err
	}
	for _, d := range devices {
		if h, ok := d["hostname"].(string); ok && h == hostname {
			return d, nil
		}
	}
	return nil, nil
}

// GenerateKillToken generates a kill token for the given device ID.
// Returns the plaintext kill token.
func (c *TestAPIClient) GenerateKillToken(deviceID string) (string, error) {
	resp, body, err := c.doRequest("POST", "/api/devices/"+deviceID+"/generate-kill-token", nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("generate kill token failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		KillToken string `json:"killToken"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse kill token response: %w", err)
	}
	return result.KillToken, nil
}

// GetEmergencyScript downloads the emergency uninstall PowerShell script for a device.
func (c *TestAPIClient) GetEmergencyScript(deviceID string) (string, error) {
	resp, body, err := c.doRequest("GET", "/api/devices/"+deviceID+"/emergency-uninstall-script", nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get emergency script failed (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return string(body), nil
}

// DeleteDevice deletes a device by ID.
func (c *TestAPIClient) DeleteDevice(id string) error {
	resp, body, err := c.doRequest("DELETE", "/api/devices/"+id, nil)
	if err != nil {
		return err
	}
	// 200 = deleted, 403 = device is online (expected in some tests), 404 = already gone
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("delete device failed (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// DeleteEnrollmentToken deletes an enrollment token by ID.
func (c *TestAPIClient) DeleteEnrollmentToken(id string) error {
	resp, body, err := c.doRequest("DELETE", "/api/enrollment-tokens/"+id, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("delete enrollment token failed (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// WaitForDeviceStatus polls the device list until the device with the given
// hostname reaches the expected status, or the timeout expires. Returns the
// device map on success.
func (c *TestAPIClient) WaitForDeviceStatus(hostname, expectedStatus string, timeout time.Duration) (map[string]interface{}, error) {
	deadline := time.Now().Add(timeout)
	var lastStatus string
	for time.Now().Before(deadline) {
		dev, err := c.FindDeviceByHostname(hostname)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		if dev != nil {
			status, _ := dev["status"].(string)
			lastStatus = status
			if status == expectedStatus {
				return dev, nil
			}
		}
		time.Sleep(1 * time.Second)
	}
	return nil, fmt.Errorf("timeout waiting for device %q to reach status %q (last seen: %q)", hostname, expectedStatus, lastStatus)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// httpToWS converts an HTTP(S) URL to a WS(S) URL with the given path appended.
func httpToWS(httpURL, path string) (string, error) {
	u, err := url.Parse(httpURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	u.Path = path
	return u.String(), nil
}

// IsServerReachable returns true if the server health endpoint responds.
func IsServerReachable(baseURL string) bool {
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
