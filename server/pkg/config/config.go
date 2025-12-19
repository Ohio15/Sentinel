package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// Server
	Environment string
	Port        int
	ServerURL   string
	ServerID    string // Unique identifier for this server instance

	// Database
	DatabaseURL         string
	DatabaseReplicaURLs []string // Read replica URLs for scaling
	DBMaxConns          int      // Maximum database connections
	DBMinConns          int      // Minimum database connections

	// Redis
	RedisURL string

	// Security
	JWTSecret       string
	EnrollmentToken string
	AllowedOrigins  []string

	// Rate Limiting
	RateLimitRequests int
	RateLimitWindow   int // seconds

	// Features
	MetricsRetentionDays int

	// Scaling Options
	EnableDistributedHub bool // Enable Redis-backed distributed WebSocket hub
	MetricsBatchSize     int  // Batch size for bulk metrics insertion
	MetricsFlushInterval int  // Flush interval in seconds

	// Push Notifications
	APNsKeyPath   string // Path to APNs .p8 key file
	APNsKeyID     string // APNs Key ID
	APNsTeamID    string // Apple Team ID
	APNsBundleID  string // iOS App Bundle ID
	APNsSandbox   bool   // Use APNs sandbox environment
	FCMCredsPath  string // Path to Firebase credentials JSON
	FCMProjectID  string // Firebase project ID
}

func Load() (*Config, error) {
	cfg := &Config{
		// Server
		Environment: getEnv("SERVER_ENV", "development"),
		Port:        getEnvInt("PORT", 8080),
		ServerURL:   getEnv("SERVER_URL", "http://localhost:8080"),
		ServerID:    getEnv("SERVER_ID", generateServerID()),

		// Database
		DatabaseURL:         getEnv("DATABASE_URL", ""),
		DatabaseReplicaURLs: getEnvSlice("DATABASE_REPLICA_URLS", []string{}),
		DBMaxConns:          getEnvInt("DB_MAX_CONNS", 50),
		DBMinConns:          getEnvInt("DB_MIN_CONNS", 10),

		// Redis
		RedisURL: getEnv("REDIS_URL", "redis://localhost:6379"),

		// Security
		JWTSecret:       getEnv("JWT_SECRET", ""),
		EnrollmentToken: getEnv("ENROLLMENT_TOKEN", ""),
		AllowedOrigins:  getEnvSlice("ALLOWED_ORIGINS", []string{}),

		// Rate Limiting
		RateLimitRequests: getEnvInt("RATE_LIMIT_REQUESTS", 100),
		RateLimitWindow:   getEnvInt("RATE_LIMIT_WINDOW", 60),

		// Features
		MetricsRetentionDays: getEnvInt("METRICS_RETENTION_DAYS", 30),

		// Scaling
		EnableDistributedHub: getEnvBool("ENABLE_DISTRIBUTED_HUB", false),
		MetricsBatchSize:     getEnvInt("METRICS_BATCH_SIZE", 100),
		MetricsFlushInterval: getEnvInt("METRICS_FLUSH_INTERVAL", 5),

		// Push Notifications
		APNsKeyPath:  getEnv("APNS_KEY_PATH", ""),
		APNsKeyID:    getEnv("APNS_KEY_ID", ""),
		APNsTeamID:   getEnv("APNS_TEAM_ID", ""),
		APNsBundleID: getEnv("APNS_BUNDLE_ID", ""),
		APNsSandbox:  getEnvBool("APNS_SANDBOX", false),
		FCMCredsPath: getEnv("FCM_CREDENTIALS_PATH", ""),
		FCMProjectID: getEnv("FCM_PROJECT_ID", ""),
	}

	// Validate required fields
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}
	if cfg.EnrollmentToken == "" {
		return nil, fmt.Errorf("ENROLLMENT_TOKEN is required")
	}

	// In production, require explicit allowed origins
	if cfg.Environment == "production" && len(cfg.AllowedOrigins) == 0 {
		return nil, fmt.Errorf("ALLOWED_ORIGINS is required in production")
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		parts := strings.Split(value, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		lower := strings.ToLower(value)
		return lower == "true" || lower == "1" || lower == "yes"
	}
	return defaultValue
}

func generateServerID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "server"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}
