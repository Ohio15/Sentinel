package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/google/uuid"
)

// Config holds the agent configuration
type Config struct {
	AgentID           string `json:"agent_id"`
	ServerURL         string `json:"server_url"`
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
