// Package discovery provides server discovery mechanisms for the Sentinel agent.
// It supports multiple discovery methods with automatic fallback:
// 1. mDNS/Bonjour - Local network service discovery
// 2. DNS SRV records - Standard service discovery via DNS
// 3. Configured URL - Fallback to explicitly configured server URL
package discovery

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// ServiceName is the mDNS service name for Sentinel servers
	ServiceName = "_sentinel._tcp"

	// ServiceDomain is the default domain for mDNS
	ServiceDomain = "local."

	// DiscoveryTimeout is the timeout for discovery operations
	DiscoveryTimeout = 10 * time.Second

	// HealthCheckTimeout is the timeout for health checks
	HealthCheckTimeout = 5 * time.Second
)

// ServerInfo contains discovered server information
type ServerInfo struct {
	URL           string
	Host          string
	Port          int
	Version       string
	EnrollmentURL string
	Source        string // "mdns", "dns", "configured", "fallback"
	Verified      bool
}

// ConnectionError provides detailed error information for connection failures
type ConnectionError struct {
	ServerURL    string
	ErrorType    string // "dns_resolution", "connection_refused", "timeout", "tls_error", "auth_error", "unknown"
	Message      string
	Suggestion   string
	UnderlyingError error
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.ErrorType, e.Message, e.Suggestion)
}

// DiagnoseConnectionError analyzes an error and returns actionable information
func DiagnoseConnectionError(serverURL string, err error) *ConnectionError {
	if err == nil {
		return nil
	}

	errStr := err.Error()
	connErr := &ConnectionError{
		ServerURL:       serverURL,
		UnderlyingError: err,
	}

	// Parse the URL to extract host/port for better diagnostics
	parsedURL, parseErr := url.Parse(serverURL)
	var host, port string
	if parseErr == nil {
		host = parsedURL.Hostname()
		port = parsedURL.Port()
		if port == "" {
			if parsedURL.Scheme == "https" || parsedURL.Scheme == "wss" {
				port = "443"
			} else {
				port = "80"
			}
		}
	}

	switch {
	case strings.Contains(errStr, "no such host"):
		connErr.ErrorType = "dns_resolution"
		connErr.Message = fmt.Sprintf("Cannot resolve hostname '%s'", host)
		connErr.Suggestion = "Check that the server hostname is correct and DNS is working. Try using an IP address instead."

	case strings.Contains(errStr, "connection refused"):
		connErr.ErrorType = "connection_refused"
		connErr.Message = fmt.Sprintf("Server at %s:%s is not accepting connections", host, port)
		connErr.Suggestion = fmt.Sprintf("Verify the server is running and listening on port %s. Check firewall rules.", port)

	case strings.Contains(errStr, "i/o timeout") || strings.Contains(errStr, "context deadline exceeded"):
		connErr.ErrorType = "timeout"
		connErr.Message = fmt.Sprintf("Connection to %s:%s timed out", host, port)
		connErr.Suggestion = "Server may be down, network may be blocked, or wrong port. Check server status and network connectivity."

	case strings.Contains(errStr, "certificate") || strings.Contains(errStr, "x509"):
		connErr.ErrorType = "tls_error"
		connErr.Message = "TLS/SSL certificate error"
		connErr.Suggestion = "Server certificate may be invalid, expired, or self-signed. Check server TLS configuration."

	case strings.Contains(errStr, "401") || strings.Contains(errStr, "403") || strings.Contains(errStr, "unauthorized"):
		connErr.ErrorType = "auth_error"
		connErr.Message = "Authentication failed"
		connErr.Suggestion = "Enrollment token may be invalid or expired. Check the enrollment token in configuration."

	case strings.Contains(errStr, "network is unreachable"):
		connErr.ErrorType = "network_unreachable"
		connErr.Message = "Network is unreachable"
		connErr.Suggestion = "Check network connection and routing. The server may be on a different network segment."

	default:
		connErr.ErrorType = "unknown"
		connErr.Message = fmt.Sprintf("Connection failed: %v", err)
		connErr.Suggestion = "Check server URL, network connectivity, and server logs for more details."
	}

	return connErr
}

// Discoverer handles server discovery
type Discoverer struct {
	configuredURL string
	httpClient    *http.Client
	mu            sync.RWMutex
	cachedServer  *ServerInfo
	cacheExpiry   time.Time
	cacheDuration time.Duration
}

// NewDiscoverer creates a new server discoverer
func NewDiscoverer(configuredURL string) *Discoverer {
	return &Discoverer{
		configuredURL: configuredURL,
		httpClient: &http.Client{
			Timeout: HealthCheckTimeout,
		},
		cacheDuration: 5 * time.Minute,
	}
}

// Discover attempts to find a Sentinel server using multiple methods
func (d *Discoverer) Discover(ctx context.Context) (*ServerInfo, error) {
	// Check cache first
	d.mu.RLock()
	if d.cachedServer != nil && time.Now().Before(d.cacheExpiry) {
		server := d.cachedServer
		d.mu.RUnlock()
		return server, nil
	}
	d.mu.RUnlock()

	var lastErr error

	// Method 1: Try mDNS discovery
	log.Println("[Discovery] Attempting mDNS discovery...")
	if server, err := d.discoverMDNS(ctx); err == nil && server != nil {
		if d.verifyServer(ctx, server) {
			d.cacheServer(server)
			log.Printf("[Discovery] Found server via mDNS: %s", server.URL)
			return server, nil
		}
	} else if err != nil {
		lastErr = err
		log.Printf("[Discovery] mDNS discovery failed: %v", err)
	}

	// Method 2: Try configured URL
	if d.configuredURL != "" {
		log.Printf("[Discovery] Trying configured URL: %s", d.configuredURL)
		server := &ServerInfo{
			URL:    d.configuredURL,
			Source: "configured",
		}
		if d.verifyServer(ctx, server) {
			d.cacheServer(server)
			log.Printf("[Discovery] Using configured server: %s", server.URL)
			return server, nil
		}
		lastErr = fmt.Errorf("configured server at %s is not responding", d.configuredURL)
	}

	// Method 3: Try common local ports as fallback
	log.Println("[Discovery] Trying common local ports...")
	commonPorts := []int{8090, 8080, 8081, 80, 443}
	for _, port := range commonPorts {
		server := &ServerInfo{
			URL:    fmt.Sprintf("http://localhost:%d", port),
			Host:   "localhost",
			Port:   port,
			Source: "fallback",
		}
		if d.verifyServer(ctx, server) {
			d.cacheServer(server)
			log.Printf("[Discovery] Found server on localhost:%d", port)
			return server, nil
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("server discovery failed: %w", lastErr)
	}
	return nil, fmt.Errorf("no Sentinel server found")
}

// discoverMDNS attempts to find a server via mDNS
func (d *Discoverer) discoverMDNS(ctx context.Context) (*ServerInfo, error) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, DiscoveryTimeout)
	defer cancel()

	// Listen for mDNS responses
	results := make(chan *ServerInfo, 10)
	errors := make(chan error, 1)

	go func() {
		server, err := d.browseMDNS(ctx)
		if err != nil {
			errors <- err
			return
		}
		if server != nil {
			results <- server
		}
	}()

	select {
	case server := <-results:
		return server, nil
	case err := <-errors:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// browseMDNS performs the actual mDNS browsing
func (d *Discoverer) browseMDNS(ctx context.Context) (*ServerInfo, error) {
	// Create UDP connection for mDNS multicast
	addr, err := net.ResolveUDPAddr("udp", "224.0.0.251:5353")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve mDNS address: %w", err)
	}

	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create UDP listener: %w", err)
	}
	defer conn.Close()

	// Send mDNS query
	query := d.buildMDNSQuery()
	_, err = conn.WriteToUDP(query, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to send mDNS query: %w", err)
	}

	// Listen for responses with timeout
	conn.SetReadDeadline(time.Now().Add(DiscoveryTimeout))
	buf := make([]byte, 4096)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return nil, nil // Timeout, no server found
			}
			return nil, err
		}

		// Parse response and extract server info
		server := d.parseMDNSResponse(buf[:n], remoteAddr)
		if server != nil {
			return server, nil
		}
	}
}

// buildMDNSQuery creates an mDNS query packet for Sentinel service
func (d *Discoverer) buildMDNSQuery() []byte {
	// Simplified mDNS query - in production, use a proper mDNS library
	// This is a basic DNS query packet for _sentinel._tcp.local
	query := []byte{
		0x00, 0x00, // Transaction ID
		0x00, 0x00, // Flags (standard query)
		0x00, 0x01, // Questions: 1
		0x00, 0x00, // Answer RRs
		0x00, 0x00, // Authority RRs
		0x00, 0x00, // Additional RRs
		// Query: _sentinel._tcp.local PTR
		0x09, '_', 's', 'e', 'n', 't', 'i', 'n', 'e', 'l',
		0x04, '_', 't', 'c', 'p',
		0x05, 'l', 'o', 'c', 'a', 'l',
		0x00,       // End of name
		0x00, 0x0c, // Type: PTR
		0x00, 0x01, // Class: IN
	}
	return query
}

// parseMDNSResponse parses an mDNS response packet
func (d *Discoverer) parseMDNSResponse(data []byte, remoteAddr *net.UDPAddr) *ServerInfo {
	// Simplified parsing - look for Sentinel service info in response
	// In production, use a proper DNS parsing library

	// For now, if we get a response from the mDNS address and it contains "sentinel",
	// assume it's our server
	if len(data) < 12 {
		return nil
	}

	// Check if this is a response (QR bit set)
	if data[2]&0x80 == 0 {
		return nil // This is a query, not a response
	}

	// Look for "sentinel" in the response
	responseStr := string(data)
	if !strings.Contains(strings.ToLower(responseStr), "sentinel") {
		return nil
	}

	// Extract port from TXT record or use default
	port := 8090 // Default port

	return &ServerInfo{
		URL:    fmt.Sprintf("http://%s:%d", remoteAddr.IP.String(), port),
		Host:   remoteAddr.IP.String(),
		Port:   port,
		Source: "mdns",
	}
}

// verifyServer checks if a server is actually a valid Sentinel server
func (d *Discoverer) verifyServer(ctx context.Context, server *ServerInfo) bool {
	// Try health endpoint
	healthURL := server.URL + "/health"

	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return false
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		connErr := DiagnoseConnectionError(server.URL, err)
		log.Printf("[Discovery] %s", connErr.Error())
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		server.Verified = true
		return true
	}

	log.Printf("[Discovery] Server %s returned status %d", server.URL, resp.StatusCode)
	return false
}

// cacheServer stores a discovered server in cache
func (d *Discoverer) cacheServer(server *ServerInfo) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cachedServer = server
	d.cacheExpiry = time.Now().Add(d.cacheDuration)
}

// ClearCache clears the server cache
func (d *Discoverer) ClearCache() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cachedServer = nil
}

// GetCachedServer returns the cached server if still valid
func (d *Discoverer) GetCachedServer() *ServerInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.cachedServer != nil && time.Now().Before(d.cacheExpiry) {
		return d.cachedServer
	}
	return nil
}
