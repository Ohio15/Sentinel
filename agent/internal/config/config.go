package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/google/uuid"
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

// Config holds the agent configuration
type Config struct {
	AgentID           string `json:"agent_id"`
	ServerURL         string `json:"server_url"`
	GrpcAddress       string `json:"grpc_address"`       // gRPC Data Plane address (HTTP port + 1)
	EnrollmentToken   string `json:"enrollment_token"`
	HeartbeatInterval int    `json:"heartbeat_interval"` // seconds
	MetricsInterval   int    `json:"metrics_interval"`   // seconds
	Enrolled          bool   `json:"enrolled"`
	DeviceID          string `json:"device_id"`
}

var (
	instance *Config
	once     sync.Once
	mu       sync.RWMutex
)

// DefaultConfig returns a config with default values
func DefaultConfig() *Config {
	return &Config{
		AgentID:           uuid.New().String(),
		HeartbeatInterval: 30,
		MetricsInterval:   60,
		Enrolled:          false,
	}
}

// GetConfigPath returns the platform-specific config path
func GetConfigPath() string {
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

	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	instance = cfg
	return cfg, nil
}

// Save writes the configuration to disk
func (c *Config) Save() error {
	mu.Lock()
	defer mu.Unlock()

	configPath := GetConfigPath()

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
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
