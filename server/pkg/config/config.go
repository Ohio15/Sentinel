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
	PublicURL   string
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
	APIKey          string
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

	// PKI / mTLS
	CACertPath          string // Path to CA certificate PEM file
	CAKeyPath           string // Path to CA private key PEM file
	EnableMTLS          bool   // Enable mTLS certificate issuance
	CertValidityYears   int    // Certificate validity period in years

	// EnforceAgentCertBinding controls the token→cert binding check on the agent
	// WebSocket auth path. When false (default), a cert-holding agent that
	// authenticates with its enrollment token over the tunnel is allowed but
	// logged + counted (WARN/OBSERVE mode) so we can enumerate which agents would
	// break under enforcement. When true, such an agent is REJECTED and must use
	// direct mTLS (port 8443).
	//
	// FLIP PRECONDITION: only enable after confirming ALL cert-holding agents can
	// reach direct mTLS (8443), or after proof-of-possession-over-tunnel (phase 2)
	// ships. Enabling this prematurely will disconnect any agent that depends on
	// the Cloudflare tunnel, where agent-side mTLS is impossible (CF terminates TLS).
	EnforceAgentCertBinding bool

	// Agent mTLS HTTP listener
	AgentMTLSPort int // Port for mTLS HTTP listener (default 8443, 0 disables)

	// gRPC Data Plane
	GRPCPort          int    // gRPC server port (0 to disable)
	GRPCPlaintextPort int    // Plaintext gRPC port for CF tunnel connections (0 to disable)
	TLSCertPath       string // Path to server TLS certificate
	TLSKeyPath        string // Path to server TLS private key

	// WebAuthn / Passkey Authentication
	WebAuthnRPID             string   // Relying Party ID (domain name)
	WebAuthnRPName           string   // Relying Party display name
	WebAuthnRPOrigins        []string // Allowed origins for WebAuthn
	WebAuthnTimeout          int      // Ceremony timeout in milliseconds
	WebAuthnUserVerification string   // "preferred", "required", or "discouraged"

	// Email / SMTP Configuration
	SMTPHost     string // SMTP server hostname
	SMTPPort     int    // SMTP server port (25, 465, 587)
	SMTPUsername string // SMTP username
	SMTPPassword string // SMTP password
	SMTPFrom     string // From email address
	SMTPFromName string // From display name
	SMTPUseTLS   bool   // Use TLS for SMTP connection

	// Alerting Configuration
	AlertEvaluationInterval int  // Alert evaluation interval in seconds (default 60)
	AlertingEnabled         bool // Enable/disable alert evaluation engine
}

func Load() (*Config, error) {
	cfg := &Config{
		// Server
		Environment: getEnv("SERVER_ENV", "development"),
		Port:        getEnvInt("PORT", 8080),
		ServerURL:   getEnv("SERVER_URL", "http://localhost:8080"),
		PublicURL:   getEnv("PUBLIC_URL", ""),
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
		APIKey:          getEnv("API_KEY", ""),
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

		// PKI / mTLS
		CACertPath:        getEnv("CA_CERT_PATH", "/certs/ca-cert.pem"),
		CAKeyPath:         getEnv("CA_KEY_PATH", "/certs/ca-key.pem"),
		EnableMTLS:        getEnvBool("ENABLE_MTLS", true),
		CertValidityYears: getEnvInt("CERT_VALIDITY_YEARS", 2),

		// Token→cert binding enforcement — default OFF (WARN/OBSERVE only).
		// See EnforceAgentCertBinding field doc for the flip precondition.
		EnforceAgentCertBinding: getEnvBool("ENFORCE_AGENT_CERT_BINDING", false),

		// Agent mTLS HTTP listener
		AgentMTLSPort: getEnvInt("AGENT_MTLS_PORT", 8443),

		// gRPC Data Plane
		GRPCPort:          getEnvInt("GRPC_PORT", 4444),
		GRPCPlaintextPort: getEnvInt("GRPC_PLAINTEXT_PORT", 0),
		TLSCertPath:       getEnv("TLS_CERT_PATH", "/certs/server-cert.pem"),
		TLSKeyPath:        getEnv("TLS_KEY_PATH", "/certs/server-key.pem"),

		// WebAuthn / Passkey Authentication
		WebAuthnRPID:             getEnv("WEBAUTHN_RP_ID", "localhost"),
		WebAuthnRPName:           getEnv("WEBAUTHN_RP_NAME", "Sentinel RMM"),
		WebAuthnRPOrigins:        getEnvSlice("WEBAUTHN_RP_ORIGINS", []string{"http://localhost:5173", "http://localhost:8080"}),
		WebAuthnTimeout:          getEnvInt("WEBAUTHN_TIMEOUT", 60000),
		WebAuthnUserVerification: getEnv("WEBAUTHN_USER_VERIFICATION", "preferred"),

		// Email / SMTP Configuration
		SMTPHost:     getEnv("SMTP_HOST", ""),
		SMTPPort:     getEnvInt("SMTP_PORT", 587),
		SMTPUsername: getEnv("SMTP_USERNAME", ""),
		SMTPPassword: getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:     getEnv("SMTP_FROM", ""),
		SMTPFromName: getEnv("SMTP_FROM_NAME", "Sentinel RMM"),
		SMTPUseTLS:   getEnvBool("SMTP_USE_TLS", true),

		// Alerting Configuration
		AlertEvaluationInterval: getEnvInt("ALERT_EVALUATION_INTERVAL", 60),
		AlertingEnabled:         getEnvBool("ALERTING_ENABLED", true),
	}

	// In production, require explicit allowed origins
	if cfg.Environment == "production" && len(cfg.AllowedOrigins) == 0 {
		return nil, fmt.Errorf("ALLOWED_ORIGINS is required in production")
	}

	return cfg, nil
}

// Validate checks all required configuration fields and returns a combined error
// listing ALL missing or invalid configs rather than failing on the first one.
func (c *Config) Validate() error {
	var errors []string

	if c.DatabaseURL == "" {
		errors = append(errors, "DATABASE_URL is required")
	}
	if c.JWTSecret == "" {
		errors = append(errors, "JWT_SECRET must be at least 32 characters")
	} else if len(c.JWTSecret) < 32 {
		errors = append(errors, "JWT_SECRET must be at least 32 characters")
	}
	if c.EnrollmentToken == "" {
		errors = append(errors, "ENROLLMENT_TOKEN is required")
	}
	if c.RedisURL == "" {
		errors = append(errors, "REDIS_URL is required")
	}

	if len(errors) > 0 {
		return fmt.Errorf("%s", strings.Join(errors, "\n"))
	}
	return nil
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
