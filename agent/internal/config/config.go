package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/sentinel/agent/internal/hardware"
	"github.com/sentinel/agent/internal/crypto"
)

// Embedded configuration placeholders - these get replaced in the binary at download time
// The strings are padded to fixed length to allow binary replacement without changing file size
// Format: SENTINEL_EMBEDDED_<KEY>:<64-char-value-padded-with-underscores>:END
var (
	EmbeddedServerURL = "SENTINEL_EMBEDDED_SERVER:________________________________________________________________:END"
	EmbeddedToken     = "SENTINEL_EMBEDDED_TOKEN:________________________________________________________________:END"
)

// GetEmbeddedConfig extracts embedded config from the placeholder variables
func GetEmbeddedConfig() (serverURL, token string, hasEmbedded bool) {
	// Extract server URL
	if strings.HasPrefix(EmbeddedServerURL, "SENTINEL_EMBEDDED_SERVER:") && strings.HasSuffix(EmbeddedServerURL, ":END") {
		value := EmbeddedServerURL[25 : len(EmbeddedServerURL)-4] // Remove prefix and suffix
		value = strings.TrimRight(value, "_")                     // Remove padding
		if value != "" && !strings.HasPrefix(value, "_") {
			serverURL = value
		}
	}

	// Extract token
	if strings.HasPrefix(EmbeddedToken, "SENTINEL_EMBEDDED_TOKEN:") && strings.HasSuffix(EmbeddedToken, ":END") {
		value := EmbeddedToken[24 : len(EmbeddedToken)-4] // Remove prefix and suffix
		value = strings.TrimRight(value, "_")             // Remove padding
		if value != "" && !strings.HasPrefix(value, "_") {
			token = value
		}
	}

	hasEmbedded = serverURL != "" && token != ""
	return
}

// Connection mode constants
const (
	ConnModeAuto   = "auto"   // Try tunnel (443) first, fall back to direct (8443)
	ConnModeTunnel = "tunnel" // Only use CF tunnel on port 443
	ConnModeDirect = "direct" // Only use direct mTLS on port 8443
)

// Config holds the agent configuration
type Config struct {
	AgentID           string `json:"agent_id"`
	ServerURL         string `json:"server_url"`
	GrpcAddress       string `json:"grpc_address"`        // gRPC Data Plane address (HTTP port + 1)
	GrpcEndpoint      string `json:"grpc_endpoint,omitempty"` // Legacy alias the server-side installer writes — folded into GrpcAddress in Load()
	EnrollmentToken   string `json:"enrollment_token"`
	HeartbeatInterval int    `json:"heartbeat_interval"` // seconds
	MetricsInterval   int    `json:"metrics_interval"`   // seconds
	Enrolled          bool   `json:"enrolled"`
	DeviceID          string `json:"device_id"`
	AuditLogDir       string `json:"audit_log_dir"`      // directory for terminal audit logs (default: platform log path)
	ConnectionMode    string `json:"connection_mode"`     // "auto", "tunnel", or "direct" (default: "auto")
}

// GetConnectionMode returns the effective connection mode, defaulting to "auto"
func (c *Config) GetConnectionMode() string {
	switch c.ConnectionMode {
	case ConnModeTunnel, ConnModeDirect:
		return c.ConnectionMode
	default:
		return ConnModeAuto
	}
}

// GetAuditLogDir returns the configured audit log directory, or the platform default
func (c *Config) GetAuditLogDir() string {
	if c.AuditLogDir != "" {
		return c.AuditLogDir
	}
	return GetLogPath()
}

var (
	instance *Config
	once     sync.Once
	mu       sync.RWMutex
)

// DefaultConfig returns a config with default values
func DefaultConfig() *Config {
	return &Config{
		AgentID:           hardware.FingerprintWithFallback(),
		HeartbeatInterval: 10,
		MetricsInterval:   1, // 1 second default - matches Windows Task Manager behavior
		Enrolled:          false,
	}
}

// GetConfigPath returns the platform-specific config path
// This is a variable to allow overriding in tests
var GetConfigPath = func() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramData"), "Sentinel", "config.json")
	case "darwin":
		return "/Library/Application Support/Sentinel/config.json"
	default: // linux
		return "/etc/sentinel/config.json"
	}
}

// GetLogPath returns the platform-specific log path
func GetLogPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramData"), "Sentinel", "logs")
	case "darwin":
		return "/Library/Logs/Sentinel"
	default:
		return "/var/log/sentinel"
	}
}

// Load reads the configuration from disk
func Load() (*Config, error) {
	mu.Lock()
	defer mu.Unlock()

	configPath := GetConfigPath()

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create default config
		instance = DefaultConfig()
		return instance, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Check if config is encrypted
	var jsonData []byte
	if crypto.IsEncrypted(data) {
		// Decrypt the config
		decrypted, err := crypto.DecryptConfig(data)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt config: %w", err)
		}
		jsonData = decrypted
	} else {
		// Unencrypted config - migrate to encrypted format
		log.Println("[CONFIG] Migrating unencrypted config to encrypted format")
		jsonData = data

		// Parse the config first to ensure it's valid
		cfg := &Config{}
		if err := json.Unmarshal(jsonData, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
		foldLegacyAliases(cfg)
		ensureAgentID(cfg)

		// Save encrypted version immediately (use unlocked since we hold the lock)
		instance = cfg
		if err := cfg.saveUnlocked(); err != nil {
			log.Printf("[CONFIG] Warning: Failed to save encrypted config during migration: %v", err)
		} else {
			log.Println("[CONFIG] Successfully migrated config to encrypted format")
		}
		return cfg, nil
	}

	cfg := &Config{}
	if err := json.Unmarshal(jsonData, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	foldLegacyAliases(cfg)
	if ensureAgentID(cfg) {
		// Persist the regenerated agent_id so the next Load() doesn't have to
		// redo the fingerprint work. Best-effort — if save fails, we still
		// proceed (the in-memory cfg has the regenerated ID).
		if err := cfg.saveUnlocked(); err != nil {
			log.Printf("[CONFIG] Warning: failed to persist regenerated agent_id: %v", err)
		}
	}

	instance = cfg
	return cfg, nil
}

// ensureAgentID regenerates a hardware-fingerprint agent_id when the loaded
// config has an empty one. Empty agent_id rows propagate to the server as
// devices.agent_id='' which breaks UNIQUE-keyed lookups and dashboard filters
// (observed 2026-05-22 on PS-BSIKORA-LT). Returns true if a regeneration
// happened, so the caller can decide whether to persist.
func ensureAgentID(cfg *Config) bool {
	if strings.TrimSpace(cfg.AgentID) != "" {
		return false
	}
	cfg.AgentID = hardware.FingerprintWithFallback()
	log.Printf("[CONFIG] Regenerated empty agent_id -> %s", cfg.AgentID)
	return true
}

// foldLegacyAliases normalizes legacy/alias JSON keys into their canonical
// fields. The server-side installer writes `grpc_endpoint` (handlers_installer.go
// InstallerConfig) but the agent's canonical field is `grpc_address` — without
// this fold the explicit endpoint is silently dropped and GetGrpcAddress falls
// back to its HTTP-port+1 offset (yielding 8082 for prod sentinelrmm.us). Bug
// observed 2026-05-22 on PS-BSIKORA-LT.
func foldLegacyAliases(cfg *Config) {
	if cfg.GrpcAddress == "" && cfg.GrpcEndpoint != "" {
		cfg.GrpcAddress = cfg.GrpcEndpoint
		cfg.GrpcEndpoint = ""
		log.Printf("[CONFIG] Folded legacy grpc_endpoint -> grpc_address=%s", cfg.GrpcAddress)
	}
}

// Save writes the configuration to disk (encrypted)
func (c *Config) Save() error {
	mu.Lock()
	defer mu.Unlock()
	return c.saveUnlocked()
}

// saveUnlocked is the internal save implementation (caller must hold lock)
func (c *Config) saveUnlocked() error {
	configPath := GetConfigPath()

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Serialize config to JSON
	jsonData, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	// Encrypt the JSON data
	encryptedData, err := crypto.EncryptConfig(jsonData)
	if err != nil {
		return fmt.Errorf("failed to encrypt config: %w", err)
	}

	// Write encrypted data to file with restrictive permissions
	if err := os.WriteFile(configPath, encryptedData, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	instance = c
	return nil
}

// Get returns the current configuration instance
func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	return instance
}

// Update modifies configuration values and saves
func (c *Config) Update(heartbeatInterval, metricsInterval int) error {
	c.HeartbeatInterval = heartbeatInterval
	c.MetricsInterval = metricsInterval
	return c.Save()
}

// SetEnrolled marks the agent as enrolled with the server
func (c *Config) SetEnrolled(deviceID string) error {
	c.Enrolled = true
	c.DeviceID = deviceID
	return c.Save()
}

// GetGrpcAddress returns the gRPC address, deriving it from ServerURL if not set
// gRPC port = HTTP port + 1 (port offset pattern)
func (c *Config) GetGrpcAddress() string {
	if c.GrpcAddress != "" {
		return c.GrpcAddress
	}

	// Derive from ServerURL using port offset pattern (HTTP port + 1)
	// ServerURL format: http://host:port or ws://host:port/ws/agent
	serverURL := c.ServerURL
	if serverURL == "" {
		return ""
	}

	// Remove protocol prefix
	host := serverURL
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "ws://")
	host = strings.TrimPrefix(host, "wss://")

	// Remove path
	if idx := strings.Index(host, "/"); idx != -1 {
		host = host[:idx]
	}

	// Extract host and port, then apply port offset (+1)
	if colonIdx := strings.LastIndex(host, ":"); colonIdx != -1 {
		hostname := host[:colonIdx]
		portStr := host[colonIdx+1:]
		port := 8081 // default
		if _, err := fmt.Sscanf(portStr, "%d", &port); err == nil {
			// gRPC port = HTTP port + 1
			return fmt.Sprintf("%s:%d", hostname, port+1)
		}
	}

	// No port specified, assume default 8081, so gRPC is 8082
	return host + ":8082"
}

// GetGrpcTunnelAddress returns the gRPC address for Cloudflare tunnel connections.
// Uses grpc.<hostname>:443 for tunnel mode.
func (c *Config) GetGrpcTunnelAddress() string {
	serverURL := c.ServerURL
	if serverURL == "" {
		return ""
	}

	// Extract hostname from server URL
	host := serverURL
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "ws://")
	host = strings.TrimPrefix(host, "wss://")

	if idx := strings.Index(host, "/"); idx != -1 {
		host = host[:idx]
	}
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	return fmt.Sprintf("grpc.%s:443", host)
}
