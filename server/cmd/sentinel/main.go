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
	"github.com/sentinel/server/internal/metrics"
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
		MaxConns: cfg.DBMaxConns,
		MinConns: cfg.DBMinConns,
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

	// Create services container for dependency injection
	services := &api.Services{
		Config:       cfg,
		DB:           db,
		Redis:        redisClient,
		Hub:          wsHub,
		BulkInserter: bulkInserter,
		CommandQueue: cmdQueue,
		PushService:  pushService,
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

	// Start server in goroutine
	go func() {
		log.Printf("Sentinel server listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Flush any pending metrics
	bulkInserter.Flush()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped")
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
