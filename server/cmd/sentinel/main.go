package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sentinel/server/internal/api"
	grpcserver "github.com/sentinel/server/internal/grpc"
	pb "github.com/sentinel/server/internal/grpc/dataplane"
	"github.com/sentinel/server/internal/constants"
	"github.com/sentinel/server/internal/metrics"
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

	// Create gRPC Data Plane server (create early so we can pass to Services)
	var grpcServer *grpcserver.DataPlaneServer
	if cfg.GRPCPort > 0 {
		grpcServer = grpcserver.NewDataPlaneServer(db, bulkInserter)
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
	}

	// Initialize API router with all services
	router := api.NewRouterWithServices(services)

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
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
