package turn

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/turn/v4"
)

// Config for TURN server
type Config struct {
	PublicIP   string // External IP for relay
	ListenIP   string // IP to listen on (0.0.0.0 for all)
	Port       int    // Default 3478
	Realm      string // e.g., "sentinel.local"
	AuthSecret string // Shared secret for time-limited credentials
	MinPort    int    // Relay port range start
	MaxPort    int    // Relay port range end
}

// Server manages the embedded TURN server
type Server struct {
	config     Config
	server     *turn.Server
	udpConn    net.PacketConn
	tcpLn      net.Listener
	mu         sync.RWMutex
	running    bool
	credentials map[string]time.Time // Track active credentials
}

// Credentials represents TURN credentials
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TTL      int    `json:"ttl"` // Seconds until expiry
	URLs     []string `json:"urls"`
}

// NewServer creates a new TURN server instance
func NewServer(config Config) *Server {
	// Set defaults
	if config.Port == 0 {
		config.Port = 3478
	}
	if config.Realm == "" {
		config.Realm = "sentinel.local"
	}
	if config.ListenIP == "" {
		config.ListenIP = "0.0.0.0"
	}
	if config.MinPort == 0 {
		config.MinPort = 49152
	}
	if config.MaxPort == 0 {
		config.MaxPort = 65535
	}
	if config.AuthSecret == "" {
		// Generate a random secret if not provided
		config.AuthSecret = generateRandomSecret()
	}

	return &Server{
		config:      config,
		credentials: make(map[string]time.Time),
	}
}

// Start starts the TURN server
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	listenAddr := fmt.Sprintf("%s:%d", s.config.ListenIP, s.config.Port)

	// Create UDP listener
	udpConn, err := net.ListenPacket("udp4", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to create UDP listener: %w", err)
	}
	s.udpConn = udpConn

	// Create TCP listener
	tcpLn, err := net.Listen("tcp4", listenAddr)
	if err != nil {
		udpConn.Close()
		return fmt.Errorf("failed to create TCP listener: %w", err)
	}
	s.tcpLn = tcpLn

	// Parse public IP
	publicIP := net.ParseIP(s.config.PublicIP)
	if publicIP == nil {
		// Try to detect public IP
		publicIP = s.detectPublicIP()
	}

	// Create TURN server
	server, err := turn.NewServer(turn.ServerConfig{
		Realm: s.config.Realm,
		AuthHandler: func(username, realm string, srcAddr net.Addr) ([]byte, bool) {
			return s.authHandler(username, realm, srcAddr)
		},
		PacketConnConfigs: []turn.PacketConnConfig{
			{
				PacketConn: udpConn,
				RelayAddressGenerator: &turn.RelayAddressGeneratorPortRange{
					RelayAddress: publicIP,
					Address:      s.config.ListenIP,
					MinPort:      uint16(s.config.MinPort),
					MaxPort:      uint16(s.config.MaxPort),
				},
			},
		},
		ListenerConfigs: []turn.ListenerConfig{
			{
				Listener: tcpLn,
				RelayAddressGenerator: &turn.RelayAddressGeneratorPortRange{
					RelayAddress: publicIP,
					Address:      s.config.ListenIP,
					MinPort:      uint16(s.config.MinPort),
					MaxPort:      uint16(s.config.MaxPort),
				},
			},
		},
	})
	if err != nil {
		udpConn.Close()
		tcpLn.Close()
		return fmt.Errorf("failed to create TURN server: %w", err)
	}

	s.server = server
	s.running = true

	// Start credential cleanup goroutine
	go s.cleanupCredentials()

	log.Printf("[TURN] Server started on %s (public IP: %s)", listenAddr, publicIP)
	return nil
}

// Stop stops the TURN server
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false

	if s.server != nil {
		s.server.Close()
	}
	if s.udpConn != nil {
		s.udpConn.Close()
	}
	if s.tcpLn != nil {
		s.tcpLn.Close()
	}

	log.Println("[TURN] Server stopped")
	return nil
}

// GenerateCredentials creates time-limited TURN credentials
func (s *Server) GenerateCredentials(userID string, ttl time.Duration) *Credentials {
	if ttl == 0 {
		ttl = 24 * time.Hour // Default 24 hours
	}

	// Time-limited credential format: timestamp:userId
	timestamp := time.Now().Add(ttl).Unix()
	username := fmt.Sprintf("%d:%s", timestamp, userID)

	// HMAC-SHA1 password
	mac := hmac.New(sha1.New, []byte(s.config.AuthSecret))
	mac.Write([]byte(username))
	password := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	// Track credential
	s.mu.Lock()
	s.credentials[username] = time.Now().Add(ttl)
	s.mu.Unlock()

	// Build URLs
	urls := []string{
		fmt.Sprintf("turn:%s:%d?transport=udp", s.config.PublicIP, s.config.Port),
		fmt.Sprintf("turn:%s:%d?transport=tcp", s.config.PublicIP, s.config.Port),
	}

	// Also add STUN URL
	urls = append([]string{
		fmt.Sprintf("stun:%s:%d", s.config.PublicIP, s.config.Port),
	}, urls...)

	return &Credentials{
		Username: username,
		Password: password,
		TTL:      int(ttl.Seconds()),
		URLs:     urls,
	}
}

// authHandler validates TURN credentials
func (s *Server) authHandler(username, realm string, srcAddr net.Addr) ([]byte, bool) {
	// Parse timestamp from username
	parts := strings.SplitN(username, ":", 2)
	if len(parts) != 2 {
		log.Printf("[TURN] Invalid username format: %s", username)
		return nil, false
	}

	timestamp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		log.Printf("[TURN] Invalid timestamp in username: %s", username)
		return nil, false
	}

	// Check if credential has expired
	if time.Now().Unix() > timestamp {
		log.Printf("[TURN] Credential expired for user: %s", username)
		return nil, false
	}

	// Generate expected password
	mac := hmac.New(sha1.New, []byte(s.config.AuthSecret))
	mac.Write([]byte(username))
	password := mac.Sum(nil)

	log.Printf("[TURN] Authenticated user: %s from %s", username, srcAddr)
	return password, true
}

// cleanupCredentials removes expired credentials periodically
func (s *Server) cleanupCredentials() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		s.mu.RLock()
		running := s.running
		s.mu.RUnlock()

		if !running {
			return
		}

		select {
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			for username, expiry := range s.credentials {
				if now.After(expiry) {
					delete(s.credentials, username)
				}
			}
			s.mu.Unlock()
		}
	}
}

// detectPublicIP attempts to detect the public IP address
func (s *Server) detectPublicIP() net.IP {
	// Try to find a non-loopback IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return net.ParseIP("127.0.0.1")
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP
			}
		}
	}

	return net.ParseIP("127.0.0.1")
}

// GetConfig returns the current configuration
func (s *Server) GetConfig() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// IsRunning returns whether the server is running
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// GetPublicIP returns the public IP being used
func (s *Server) GetPublicIP() string {
	return s.config.PublicIP
}

// GetPort returns the port being used
func (s *Server) GetPort() int {
	return s.config.Port
}

// generateRandomSecret generates a random auth secret
func generateRandomSecret() string {
	// Use current time + some entropy as a basic secret
	// In production, this should be a proper cryptographic random string
	return fmt.Sprintf("sentinel-%d", time.Now().UnixNano())
}
