package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sentinel/server/internal/alerting"
	"github.com/sentinel/server/internal/api"
	"github.com/sentinel/server/internal/constants"
	"github.com/sentinel/server/internal/credentials"
	grpcserver "github.com/sentinel/server/internal/grpc"
	pb "github.com/sentinel/server/internal/grpc/dataplane"
	"github.com/sentinel/server/internal/metrics"
	"github.com/sentinel/server/internal/notifier"
	"github.com/sentinel/server/internal/pki"
	"github.com/sentinel/server/internal/push"
	"github.com/sentinel/server/internal/queue"
	"github.com/sentinel/server/internal/websocket"
	"github.com/sentinel/server/pkg/cache"
	"github.com/sentinel/server/pkg/config"
	"github.com/sentinel/server/pkg/database"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Starting Sentinel server (ID: %s, Environment: %s)", cfg.ServerID, cfg.Environment)

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

	// Initialize credential management services
	var jwtManager *credentials.JWTManager
	var apiKeyManager *credentials.APIKeyManager

	// Derive master encryption key from JWT secret (or use dedicated MASTER_KEY env var)
	masterKeySource := os.Getenv("CREDENTIAL_MASTER_KEY")
	if masterKeySource == "" {
		masterKeySource = cfg.JWTSecret // Fallback to JWT secret for key derivation
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
	}

	// Initialize API router with all services
	router := api.NewRouterWithServices(services)

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start command queue consumer
	cmdQueue.StartConsumer(func(cmd queue.CommandMessage) error {
		return handleCommand(distHub, localHub, cmd)
	})

	// Start HTTP server in goroutine
	go func() {
		log.Printf("Sentinel HTTP server listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

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
				if err := srv.Serve(listener); err != nil {
					log.Printf("gRPC server error: %v", err)
				}
			}()
		}
	}

	// Start agent log cleanup goroutine (delete logs older than 7 days, runs daily)
	go func() {
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
