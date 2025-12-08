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

	// Database
	DatabaseURL string

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
}

func Load() (*Config, error) {
	cfg := &Config{
		Environment:          getEnv("SERVER_ENV", "development"),
		Port:                 getEnvInt("PORT", 8080),
		DatabaseURL:          getEnv("DATABASE_URL", ""),
		RedisURL:             getEnv("REDIS_URL", "redis://localhost:6379"),
		JWTSecret:            getEnv("JWT_SECRET", ""),
		EnrollmentToken:      getEnv("ENROLLMENT_TOKEN", ""),
		AllowedOrigins:       getEnvSlice("ALLOWED_ORIGINS", []string{}),
		RateLimitRequests:    getEnvInt("RATE_LIMIT_REQUESTS", 100),
		RateLimitWindow:      getEnvInt("RATE_LIMIT_WINDOW", 60),
		MetricsRetentionDays: getEnvInt("METRICS_RETENTION_DAYS", 30),
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
