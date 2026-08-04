package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/sentinel/server/internal/alerting"
	"github.com/sentinel/server/internal/api"
	"github.com/sentinel/server/internal/audit"
	"github.com/sentinel/server/internal/constants"
	"github.com/sentinel/server/internal/credentials"
	grpcserver "github.com/sentinel/server/internal/grpc"
	pb "github.com/sentinel/server/internal/grpc/dataplane"
	"github.com/sentinel/server/internal/metrics"
	"github.com/sentinel/server/internal/middleware"
	"github.com/sentinel/server/internal/notifier"
	"github.com/sentinel/server/internal/pki"
	"github.com/sentinel/server/internal/push"
	"github.com/sentinel/server/internal/queue"
	"github.com/sentinel/server/internal/turn"
	"github.com/sentinel/server/internal/websocket"
	"github.com/sentinel/server/pkg/cache"
	"github.com/sentinel/server/pkg/config"
	"github.com/sentinel/server/pkg/database"
	"github.com/sentinel/server/pkg/logger"
	"github.com/sentinel/server/pkg/tlsconfig"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Validate all required configuration
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Configuration validation failed:\n%s", err)
	}

	// Initialize structured logger
	logger.Init(cfg.Environment)
	logger.Info("Sentinel server starting", "version", "1.78.0", "env", cfg.Environment, "server_id", cfg.ServerID)

	// Initialize database with connection pool settings
	dbConfig := &database.Config{
		URL:      cfg.DatabaseURL,
		MaxConns: int32(cfg.DBMaxConns),
		MinConns: int32(cfg.DBMinConns),
	}
	db, err := database.NewWithConfig(dbConfig)
	if err != nil {
		// Fallback to basic connection
		db, err = database.New(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("Failed to connect to database: %v", err)
		}
	}
	defer db.Close()

	// Run migrations
	if err := db.Migrate(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// First-run admin creation (only when users table is empty)
	if err := ensureFirstRunAdmin(db); err != nil {
		log.Fatalf("Failed to create first-run admin: %v", err)
	}

	// Initialize Redis cache
	redisClient, err := cache.New(cfg.RedisURL)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer redisClient.Close()

	// Initialize WebSocket hub (distributed or local)
	var wsHub api.WebSocketHub
	var distHub *websocket.DistributedHub
	var localHub *websocket.Hub

	if cfg.EnableDistributedHub {
		log.Println("Initializing distributed WebSocket hub...")
		distHub = websocket.NewDistributedHub(redisClient.Client(), cfg.ServerID)
		go distHub.Run()
		wsHub = distHub
		defer distHub.Close()
	} else {
		log.Println("Initializing local WebSocket hub...")
		localHub = websocket.NewHub(redisClient)
		go localHub.Run()
		wsHub = localHub
	}

	// Initialize bulk metrics inserter
	bulkInserter := metrics.NewBulkInserter(db.Pool(), &metrics.BulkInserterConfig{
		BatchSize:     cfg.MetricsBatchSize,
		FlushInterval: time.Duration(cfg.MetricsFlushInterval) * time.Second,
	})
	defer bulkInserter.Close()

	// Initialize command queue
	cmdQueue := queue.NewCommandQueue(redisClient.Client(), cfg.ServerID)
	defer cmdQueue.Close()

	// Initialize push notification service (if configured)
	var pushService *push.Service
	if cfg.APNsKeyPath != "" || cfg.FCMCredsPath != "" {
		log.Println("Initializing push notification service...")
		pushConfig := push.Config{
			APNsKeyPath:        cfg.APNsKeyPath,
			APNsKeyID:          cfg.APNsKeyID,
			APNsTeamID:         cfg.APNsTeamID,
			APNsBundleID:       cfg.APNsBundleID,
			APNsSandbox:        cfg.APNsSandbox,
			FCMCredentialsPath: cfg.FCMCredsPath,
			FCMProjectID:       cfg.FCMProjectID,
		}
		pushService, err = push.NewService(db.Pool(), pushConfig)
		if err != nil {
			log.Printf("Warning: Failed to initialize push service: %v", err)
		}
	}

	// Initialize PKI service for mTLS certificate issuance
	var pkiService *pki.PKI
	if cfg.EnableMTLS {
		log.Println("Initializing PKI service for mTLS...")
		pkiConfig := pki.Config{
			CACertPath: cfg.CACertPath,
			CAKeyPath:  cfg.CAKeyPath,
		}
		pkiService, err = pki.New(pkiConfig, db.Pool())
		if err != nil {
			log.Printf("Warning: Failed to initialize PKI service: %v", err)
			log.Println("mTLS certificate issuance will be disabled")
		} else {
			log.Println("PKI service initialized - mTLS certificate issuance enabled")
		}
	}

	// Initialize notification service (for email and webhooks)
	log.Println("Initializing notification service...")
	notifierService := notifier.NewService(notifier.Config{
		SMTPHost:     cfg.SMTPHost,
		SMTPPort:     cfg.SMTPPort,
		SMTPUsername: cfg.SMTPUsername,
		SMTPPassword: cfg.SMTPPassword,
		SMTPFrom:     cfg.SMTPFrom,
		SMTPFromName: cfg.SMTPFromName,
		SMTPUseTLS:   cfg.SMTPUseTLS,
	})
	notifierService.SetDatabase(db.Pool())
	if cfg.SMTPHost != "" {
		log.Println("Email notifications enabled")
	}
	log.Println("Notification service initialized (webhooks enabled)")

	// Initialize alert evaluation engine
	var alertEngine *alerting.Engine
	if cfg.AlertingEnabled {
		log.Println("Initializing alert evaluation engine...")
		alertEngine = alerting.NewEngine(db.Pool(), wsHub, notifierService, alerting.EngineConfig{
			EvaluationInterval: time.Duration(cfg.AlertEvaluationInterval) * time.Second,
		})
		alertEngine.Start()
		defer alertEngine.Stop()
		log.Printf("Alert engine started (evaluation interval: %ds)", cfg.AlertEvaluationInterval)
	}

	// Create gRPC Data Plane server (create early so we can pass to Services)
	var grpcServer *grpcserver.DataPlaneServer
	if cfg.GRPCPort > 0 {
		grpcServer = grpcserver.NewDataPlaneServer(db, bulkInserter)
	}

	// Initialize TURN server if enabled
	var turnServer *turn.Server
	if os.Getenv("TURN_ENABLED") == "true" || os.Getenv("TURN_ENABLED") == "1" {
		turnConfig := turn.Config{
			PublicIP:   os.Getenv("TURN_PUBLIC_IP"),
			ListenIP:   "0.0.0.0",
			Port:       3478,
			Realm:      "sentinel.local",
			AuthSecret: os.Getenv("TURN_AUTH_SECRET"),
		}
		if portStr := os.Getenv("TURN_PORT"); portStr != "" {
			if p, err := strconv.Atoi(portStr); err == nil {
				turnConfig.Port = p
			}
		}
		if minPort := os.Getenv("TURN_MIN_PORT"); minPort != "" {
			if p, err := strconv.Atoi(minPort); err == nil {
				turnConfig.MinPort = p
			}
		}
		if maxPort := os.Getenv("TURN_MAX_PORT"); maxPort != "" {
			if p, err := strconv.Atoi(maxPort); err == nil {
				turnConfig.MaxPort = p
			}
		}

		turnServer = turn.NewServer(turnConfig)
		if err := turnServer.Start(); err != nil {
			log.Printf("WARNING: TURN server failed to start: %v", err)
			turnServer = nil
		} else {
			log.Printf("TURN server started on port %d", turnConfig.Port)
		}
		defer func() {
			if turnServer != nil {
				turnServer.Stop()
			}
		}()
	}

	// Initialize credential management services
	var jwtManager *credentials.JWTManager
	var apiKeyManager *credentials.APIKeyManager

	// Derive master encryption key from dedicated env var (or derive from JWT secret with salt)
	masterKeySource := os.Getenv("CREDENTIAL_MASTER_KEY")
	if masterKeySource == "" {
		// Derive a DIFFERENT key - not the raw JWT secret
		masterKeySource = cfg.JWTSecret + ":credential-master-key-v1"
		logger.Warn("CREDENTIAL_MASTER_KEY not set, deriving from JWT_SECRET — set a separate key for production")
	}
	masterKey := sha256.Sum256([]byte(masterKeySource))

	// Initialize JWT Manager with dual-key rotation support
	jwtManager, err = credentials.NewJWTManager(db.Pool(), masterKey[:], cfg.JWTSecret)
	if err != nil {
		log.Printf("Warning: Failed to initialize JWT Manager: %v", err)
		log.Println("Falling back to static JWT secret (no rotation support)")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := jwtManager.Initialize(ctx); err != nil {
			log.Printf("Warning: Failed to initialize JWT keys: %v", err)
			jwtManager = nil // Disable managed JWT
		} else {
			log.Println("JWT Manager initialized with dual-key rotation support")
		}
		cancel()
	}

	// Initialize API Key Manager
	if jwtManager != nil {
		encryptor, _ := credentials.NewKeyEncryptor(masterKey[:])
		apiKeyManager = credentials.NewAPIKeyManager(db.Pool(), encryptor)
		log.Println("API Key Manager initialized")
	}

	// Audit logger for security-critical events (device deletes, cert reissue, etc.)
	auditLogger := audit.NewLogger(db.Pool())

	// Create services container for dependency injection
	services := &api.Services{
		Config:          cfg,
		DB:              db,
		Redis:           redisClient,
		Hub:             wsHub,
		BulkInserter:    bulkInserter,
		CommandQueue:    cmdQueue,
		PushService:     pushService,
		PKI:             pkiService,
		MetricsRecorder: grpcServer, // gRPC server implements MetricsRecorder interface
		JWTManager:      jwtManager,
		APIKeyManager:   apiKeyManager,
		TURNServer:      turnServer,
		Audit:           auditLogger,
	}

	// Seed default router scheduled actions (no-op if already populated)
	api.SeedDefaultScheduledActions(services)

	// Initialize API router with all services
	router := api.NewRouterWithServices(services)

	// Create HTTP server with hardened timeouts to prevent slowloris and resource exhaustion
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           router,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}

	// Start command queue consumer
	cmdQueue.StartConsumer(func(cmd queue.CommandMessage) error {
		return handleCommand(distHub, localHub, cmd)
	})

	// Start HTTP server in goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("HTTP server goroutine panicked", "panic", r, "stack", string(debug.Stack()))
			}
		}()
		logger.Info("Sentinel HTTP server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// Start agent mTLS HTTP server on a dedicated port (:8443 by default).
	// This listener terminates TLS directly (no Traefik proxy), exposes only
	// agent-facing routes, and enforces per-IP rate limiting.
	var mtlsServer *http.Server
	if cfg.EnableMTLS && cfg.AgentMTLSPort > 0 {
		mtlsTLSConfig, err := tlsconfig.LoadAgentMTLSConfig(tlsconfig.Config{
			CertPath:   cfg.TLSCertPath,
			KeyPath:    cfg.TLSKeyPath,
			CACertPath: cfg.CACertPath,
		})
		if err != nil {
			log.Fatalf("Failed to load agent mTLS config: %v", err)
		}

		agentLimiter := middleware.NewAgentRateLimiter()
		agentRouter := api.NewAgentMTLSRouter(services, agentLimiter)

		mtlsServer = &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.AgentMTLSPort),
			Handler:           agentRouter,
			TLSConfig:         mtlsTLSConfig,
			ReadTimeout:       0,                 // WebSocket: no read deadline
			WriteTimeout:      0,                 // WebSocket: no write deadline
			IdleTimeout:       300 * time.Second,  // matches former Traefik respondingTimeouts
			ReadHeaderTimeout: 10 * time.Second,
			MaxHeaderBytes:    1 << 20, // 1MB
		}

		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("Agent mTLS server goroutine panicked", "panic", r, "stack", string(debug.Stack()))
				}
			}()
			logger.Info("Agent mTLS server listening", "addr", mtlsServer.Addr, "port", cfg.AgentMTLSPort)
			// Empty cert/key paths: TLSConfig.GetCertificate handles cert loading
			if err := mtlsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Agent mTLS server failed: %v", err)
			}
		}()
	}

	// Start gRPC Data Plane server (grpcServer was created earlier for Services)
	var grpcSrv interface{ GracefulStop() }
	if cfg.GRPCPort > 0 && grpcServer != nil {
		log.Printf("Starting gRPC Data Plane server on port %d...", cfg.GRPCPort)

		// Set up metrics callback to broadcast to dashboards (no storage by default)
		grpcServer.SetMetricsCallback(func(agentID string, m *pb.Metrics) {
			// Look up device ID from agent ID
			var deviceID string
			ctx := context.Background()
			err := db.Pool().QueryRow(ctx,
				"SELECT id FROM devices WHERE agent_id = $1 AND organization_id = $2",
				agentID, constants.CurrentOrganizationID).Scan(&deviceID)
			if err != nil {
				return // Device not found, skip broadcast
			}

			// Broadcast to connected dashboards - metrics are streamed, not stored
			broadcastMsg, _ := json.Marshal(map[string]interface{}{
				"type":     "device_metrics",
				"deviceId": deviceID,
				"metrics": map[string]interface{}{
					"cpuPercent":       m.CpuPercent,
					"memoryPercent":    m.MemoryPercent,
					"memoryUsedBytes":  m.MemoryUsed,
					"diskPercent":      m.DiskPercent,
					"diskUsedBytes":    m.DiskUsed,
					"networkRxBytes":   m.NetworkRxBytes,
					"networkTxBytes":   m.NetworkTxBytes,
					"processCount":     m.ProcessCount,
				},
			})
			wsHub.BroadcastToDashboards(broadcastMsg)
		})

		grpcConfig := grpcserver.ServerConfig{
			Port:        cfg.GRPCPort,
			TLSCertFile: cfg.TLSCertPath,
			TLSKeyFile:  cfg.TLSKeyPath,
			CACertFile:  cfg.CACertPath,
			UseTLS:      cfg.EnableMTLS,
		}

		srv, listener, err := grpcserver.StartServer(grpcConfig, grpcServer)
		if err != nil {
			log.Printf("Warning: Failed to start gRPC server: %v", err)
		} else {
			grpcSrv = srv
			go func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Error("gRPC server goroutine panicked", "panic", r, "stack", string(debug.Stack()))
					}
				}()
				if err := srv.Serve(listener); err != nil {
					log.Printf("gRPC server error: %v", err)
				}
			}()
		}

		// Start plaintext gRPC server for Cloudflare tunnel connections
		if cfg.GRPCPlaintextPort > 0 {
			log.Printf("Starting plaintext gRPC server on port %d (CF tunnel)...", cfg.GRPCPlaintextPort)
			ptSrv, ptListener, ptErr := grpcserver.StartPlaintextServer(cfg.GRPCPlaintextPort, grpcServer, nil)
			if ptErr != nil {
				log.Printf("Warning: Failed to start plaintext gRPC server: %v", ptErr)
			} else {
				go func() {
					defer func() {
						if r := recover(); r != nil {
							logger.Error("Plaintext gRPC server goroutine panicked", "panic", r, "stack", string(debug.Stack()))
						}
					}()
					if err := ptSrv.Serve(ptListener); err != nil {
						log.Printf("Plaintext gRPC server error: %v", err)
					}
				}()
			}
		}
	}

	// Start agent log cleanup goroutine (delete logs older than 7 days, runs daily)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Log cleanup goroutine panicked", "panic", r, "stack", string(debug.Stack()))
			}
		}()
		// Initial delay to let server stabilize
		time.Sleep(5 * time.Minute)
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			ctx := context.Background()
			result, err := db.Pool().Exec(ctx, "DELETE FROM agent_logs WHERE logged_at < NOW() - INTERVAL '7 days'")
			if err != nil {
				log.Printf("[LogCleanup] Failed to clean up old agent logs: %v", err)
			} else if result.RowsAffected() > 0 {
				log.Printf("[LogCleanup] Cleaned up %d old agent log entries", result.RowsAffected())
			}
			<-ticker.C
		}
	}()

	// Metrics retention cleanup (daily)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Metrics cleanup goroutine panicked", "panic", r)
			}
		}()
		// Run once on startup after a delay
		time.Sleep(5 * time.Minute)
		retentionDays := 90
		var deleted int
		err := db.Pool().QueryRow(context.Background(), "SELECT cleanup_old_metrics($1)", retentionDays).Scan(&deleted)
		if err != nil {
			logger.Error("Initial metrics cleanup failed", "error", err)
		} else if deleted > 0 {
			logger.Info("Startup metrics cleanup complete", "deleted", deleted)
		}

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			var d int
			err := db.Pool().QueryRow(context.Background(), "SELECT cleanup_old_metrics($1)", retentionDays).Scan(&d)
			if err != nil {
				logger.Error("Daily metrics cleanup failed", "error", err)
			} else if d > 0 {
				logger.Info("Daily metrics cleanup complete", "deleted", d, "retention_days", retentionDays)
			}
		}
	}()

	// Silent-agent detector: scans every 5 min for devices whose last_seen is
	// past the silence cutoff and pushes a graduated heal command via the WS
	// hub (when the connection is still open) or records a manual-review row
	// (when it isn't). Closes the "agent went dark, requires onsite visit"
	// failure mode that was the platform's longest-standing persistent issue.
	silentDetector := api.NewSilentAgentDetector(db.Pool(), wsHub)
	silentDetector.Start()
	defer silentDetector.Stop()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down servers...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Flush any pending metrics
	bulkInserter.Flush()

	// Shutdown gRPC server first
	if grpcSrv != nil {
		log.Println("Stopping gRPC server...")
		grpcSrv.GracefulStop()
	}

	// Shutdown agent mTLS server
	if mtlsServer != nil {
		log.Println("Stopping agent mTLS server...")
		if err := mtlsServer.Shutdown(ctx); err != nil {
			log.Printf("Agent mTLS server forced to shutdown: %v", err)
		}
	}

	// Shutdown HTTP server
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("HTTP server forced to shutdown: %v", err)
	}

	log.Println("Servers stopped")
}

// handleCommand routes commands to connected agents
func handleCommand(distHub *websocket.DistributedHub, localHub *websocket.Hub, cmd queue.CommandMessage) error {
	// Build command message for agent
	msg := map[string]interface{}{
		"type":      cmd.CommandType,
		"command":   cmd.Command,
		"requestId": cmd.RequestID,
		"timeout":   cmd.Timeout,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to encode command: %w", err)
	}

	// Try to send to agent via distributed hub
	if distHub != nil {
		return distHub.SendToAgentDistributed(cmd.AgentID, msgBytes)
	}

	// Local hub fallback
	if localHub != nil {
		return localHub.SendToAgent(cmd.AgentID, msgBytes)
	}

	return fmt.Errorf("no hub available")
}

// ensureFirstRunAdmin creates an admin user with a random password if the users table is empty.
// This replaces the old hardcoded admin INSERT in the migration SQL.
func ensureFirstRunAdmin(db *database.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var count int
	err := db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check users table: %w", err)
	}

	if count > 0 {
		return nil // Users already exist, skip first-run setup
	}

	// Generate a cryptographically random 24-character password
	randomBytes := make([]byte, 18) // 18 bytes = 24 base64 chars
	if _, err := rand.Read(randomBytes); err != nil {
		return fmt.Errorf("failed to generate random password: %w", err)
	}
	password := base64.URLEncoding.EncodeToString(randomBytes)

	// Hash with bcrypt cost 12
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// username is NOT NULL UNIQUE (migration 000031) and must be supplied here.
	// Omitting it made every fresh install abort at startup with
	// `null value in column "username" ... violates not-null constraint`
	// (SQLSTATE 23502) — invisible on existing deployments because this whole
	// function short-circuits once the users table is non-empty. The value
	// follows the same convention 000031 used to backfill existing rows: the
	// lower-cased local part of the email. A collision is impossible here, as
	// this code only runs when the users table is empty.
	_, err = db.Pool().Exec(ctx,
		"INSERT INTO users (email, username, password_hash, first_name, last_name, role) VALUES ($1, $2, $3, $4, $5, $6)",
		"admin@sentinel.local", "admin", string(hash), "Admin", "User", "admin",
	)
	if err != nil {
		return fmt.Errorf("failed to insert admin user: %w", err)
	}

	logger.Info("First-run admin user created",
		"email", "admin@sentinel.local",
		"action", "CHANGE PASSWORD IMMEDIATELY")
	fmt.Printf("\n========================================\nFIRST RUN: Admin credentials\nEmail: admin@sentinel.local\nPassword: %s\nCHANGE THIS IMMEDIATELY\n========================================\n", password)

	return nil
}
