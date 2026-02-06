//go:build windows
// +build windows

package rdp

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// DefaultRDPAddr is the default local RDP service address
	DefaultRDPAddr = "127.0.0.1:3389"

	// WriteBufferSize is the size of the write buffer
	WriteBufferSize = 32 * 1024 // 32KB

	// ReadBufferSize is the size of the read buffer
	ReadBufferSize = 32 * 1024 // 32KB

	// PingInterval is how often we send pings to keep the connection alive
	PingInterval = 30 * time.Second

	// ReadDeadline is the read timeout for the RDP connection
	ReadDeadline = 100 * time.Millisecond
)

// TunnelConfig holds configuration for the RDP tunnel
type TunnelConfig struct {
	ServerURL    string
	AgentID      string
	LocalRDPAddr string
}

// TunnelStats holds statistics about the tunnel
type TunnelStats struct {
	BytesSent     uint64
	BytesReceived uint64
	StartTime     time.Time
	LastActivity  time.Time
}

// Tunnel forwards RDP traffic between the server and local RDP service
type Tunnel struct {
	config TunnelConfig

	conn    *websocket.Conn
	rdpConn net.Conn

	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	active bool
	stats  TunnelStats

	// Callbacks
	onStateChange func(connected bool, err error)
}

// NewTunnel creates a new RDP tunnel
func NewTunnel(config TunnelConfig) *Tunnel {
	if config.LocalRDPAddr == "" {
		config.LocalRDPAddr = DefaultRDPAddr
	}

	return &Tunnel{
		config: config,
	}
}

// SetOnStateChange sets the callback for tunnel state changes
func (t *Tunnel) SetOnStateChange(callback func(connected bool, err error)) {
	t.onStateChange = callback
}

// Start initiates the RDP tunnel
func (t *Tunnel) Start(ctx context.Context, sessionID string) error {
	t.mu.Lock()
	if t.active {
		t.mu.Unlock()
		return fmt.Errorf("tunnel already active")
	}
	t.active = true
	t.stats.StartTime = time.Now()
	t.stats.LastActivity = time.Now()
	t.mu.Unlock()

	t.ctx, t.cancel = context.WithCancel(ctx)

	// Connect to server's RDP tunnel endpoint
	tunnelURL := fmt.Sprintf("%s/api/agents/%s/rdp-tunnel?session=%s",
		t.config.ServerURL, t.config.AgentID, sessionID)

	log.Printf("[RDP Tunnel] Connecting to server: %s", tunnelURL)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		ReadBufferSize:   ReadBufferSize,
		WriteBufferSize:  WriteBufferSize,
	}

	conn, _, err := dialer.DialContext(t.ctx, tunnelURL, nil)
	if err != nil {
		t.setInactive()
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	t.conn = conn

	// Connect to local RDP service
	log.Printf("[RDP Tunnel] Connecting to local RDP: %s", t.config.LocalRDPAddr)

	rdpConn, err := net.DialTimeout("tcp", t.config.LocalRDPAddr, 5*time.Second)
	if err != nil {
		t.conn.Close()
		t.setInactive()
		return fmt.Errorf("failed to connect to local RDP: %w", err)
	}
	t.rdpConn = rdpConn

	log.Printf("[RDP Tunnel] Tunnel established")

	if t.onStateChange != nil {
		t.onStateChange(true, nil)
	}

	// Start bidirectional forwarding
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		t.forwardToRDP()
	}()

	go func() {
		defer wg.Done()
		t.forwardFromRDP()
	}()

	// Wait for both goroutines to complete
	go func() {
		wg.Wait()
		t.Stop()
	}()

	return nil
}

// forwardToRDP forwards data from WebSocket to local RDP
func (t *Tunnel) forwardToRDP() {
	defer func() {
		log.Printf("[RDP Tunnel] forwardToRDP stopped")
	}()

	for {
		select {
		case <-t.ctx.Done():
			return
		default:
			_, data, err := t.conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Printf("[RDP Tunnel] WebSocket closed normally")
				} else {
					log.Printf("[RDP Tunnel] WebSocket read error: %v", err)
				}
				return
			}

			if _, err := t.rdpConn.Write(data); err != nil {
				log.Printf("[RDP Tunnel] RDP write error: %v", err)
				return
			}

			atomic.AddUint64(&t.stats.BytesSent, uint64(len(data)))
			t.updateLastActivity()
		}
	}
}

// forwardFromRDP forwards data from local RDP to WebSocket
func (t *Tunnel) forwardFromRDP() {
	defer func() {
		log.Printf("[RDP Tunnel] forwardFromRDP stopped")
	}()

	buf := make([]byte, ReadBufferSize)

	for {
		select {
		case <-t.ctx.Done():
			return
		default:
			// Set a short read deadline to allow checking context
			t.rdpConn.SetReadDeadline(time.Now().Add(ReadDeadline))
			n, err := t.rdpConn.Read(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// Timeout is expected, continue
					continue
				}
				if err != io.EOF {
					log.Printf("[RDP Tunnel] RDP read error: %v", err)
				}
				return
			}

			if n > 0 {
				if err := t.conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					log.Printf("[RDP Tunnel] WebSocket write error: %v", err)
					return
				}

				atomic.AddUint64(&t.stats.BytesReceived, uint64(n))
				t.updateLastActivity()
			}
		}
	}
}

// Stop terminates the tunnel
func (t *Tunnel) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active {
		return
	}

	log.Printf("[RDP Tunnel] Stopping tunnel")

	t.active = false
	if t.cancel != nil {
		t.cancel()
	}

	if t.conn != nil {
		t.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		t.conn.Close()
		t.conn = nil
	}

	if t.rdpConn != nil {
		t.rdpConn.Close()
		t.rdpConn = nil
	}

	if t.onStateChange != nil {
		t.onStateChange(false, nil)
	}

	log.Printf("[RDP Tunnel] Tunnel stopped (sent: %d bytes, received: %d bytes)",
		t.stats.BytesSent, t.stats.BytesReceived)
}

// IsActive returns whether the tunnel is currently active
func (t *Tunnel) IsActive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.active
}

// GetStats returns the current tunnel statistics
func (t *Tunnel) GetStats() TunnelStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return TunnelStats{
		BytesSent:     atomic.LoadUint64(&t.stats.BytesSent),
		BytesReceived: atomic.LoadUint64(&t.stats.BytesReceived),
		StartTime:     t.stats.StartTime,
		LastActivity:  t.stats.LastActivity,
	}
}

func (t *Tunnel) setInactive() {
	t.mu.Lock()
	t.active = false
	t.mu.Unlock()
}

func (t *Tunnel) updateLastActivity() {
	t.mu.Lock()
	t.stats.LastActivity = time.Now()
	t.mu.Unlock()
}

// TunnelManager manages multiple RDP tunnels
type TunnelManager struct {
	mu      sync.Mutex
	tunnels map[string]*Tunnel
}

// NewTunnelManager creates a new tunnel manager
func NewTunnelManager() *TunnelManager {
	return &TunnelManager{
		tunnels: make(map[string]*Tunnel),
	}
}

// StartTunnel starts a new RDP tunnel for the given session
func (m *TunnelManager) StartTunnel(ctx context.Context, sessionID string, config TunnelConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if tunnel already exists
	if existing, ok := m.tunnels[sessionID]; ok {
		if existing.IsActive() {
			return fmt.Errorf("tunnel for session %s already active", sessionID)
		}
		// Clean up inactive tunnel
		delete(m.tunnels, sessionID)
	}

	tunnel := NewTunnel(config)
	tunnel.SetOnStateChange(func(connected bool, err error) {
		if !connected {
			m.mu.Lock()
			delete(m.tunnels, sessionID)
			m.mu.Unlock()
			log.Printf("[TunnelManager] Tunnel %s removed", sessionID)
		}
	})

	if err := tunnel.Start(ctx, sessionID); err != nil {
		return err
	}

	m.tunnels[sessionID] = tunnel
	log.Printf("[TunnelManager] Started tunnel for session %s", sessionID)
	return nil
}

// StopTunnel stops the tunnel for the given session
func (m *TunnelManager) StopTunnel(sessionID string) {
	m.mu.Lock()
	tunnel, ok := m.tunnels[sessionID]
	if ok {
		delete(m.tunnels, sessionID)
	}
	m.mu.Unlock()

	if ok && tunnel != nil {
		tunnel.Stop()
		log.Printf("[TunnelManager] Stopped tunnel for session %s", sessionID)
	}
}

// StopAll stops all active tunnels
func (m *TunnelManager) StopAll() {
	m.mu.Lock()
	tunnels := make([]*Tunnel, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		tunnels = append(tunnels, t)
	}
	m.tunnels = make(map[string]*Tunnel)
	m.mu.Unlock()

	for _, t := range tunnels {
		t.Stop()
	}

	log.Printf("[TunnelManager] Stopped all tunnels")
}

// GetActiveTunnels returns the number of active tunnels
func (m *TunnelManager) GetActiveTunnels() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tunnels)
}
