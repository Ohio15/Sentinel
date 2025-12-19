package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sentinel/server/internal/middleware"
	"github.com/sentinel/server/internal/websocket"
	"github.com/sentinel/server/pkg/cache"
	"github.com/sentinel/server/pkg/config"
	"github.com/sentinel/server/pkg/database"
)

type Router struct {
	config *config.Config
	db     *database.DB
	cache  *cache.Cache
	hub    *websocket.Hub
}

func NewRouter(cfg *config.Config, db *database.DB, cache *cache.Cache, hub *websocket.Hub) *gin.Engine {
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(securityHeadersMiddleware())
	r.Use(corsMiddleware(cfg))

	router := &Router{
		config: cfg,
		db:     db,
		cache:  cache,
		hub:    hub,
	}

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().UTC(),
		})
	})

	// API routes
	api := r.Group("/api")
	{
		// Public routes with rate limiting
		auth := api.Group("/auth")
		auth.Use(rateLimitMiddleware(cache, cfg.RateLimitRequests, cfg.RateLimitWindow))
		{
			auth.POST("/login", router.login)
			auth.POST("/refresh", router.refreshToken)
		}

		// Agent routes (uses enrollment token)
		agent := api.Group("/agent")
		agent.Use(middleware.AgentAuthMiddleware(cfg.EnrollmentToken))
		{
			agent.POST("/enroll", router.enrollAgent)
		}

		// Agent update routes (public - agents call these for updates)
		agentUpdate := api.Group("/agent")
		{
			agentUpdate.GET("/version", router.getAgentVersion)
			agentUpdate.GET("/update/download", router.downloadAgentUpdate)
			agentUpdate.POST("/update/status", router.reportUpdateStatus)
		}

		// Agent download routes (public with token validation)
		agents := api.Group("/agents")
		{
			agents.GET("/download/:platform/:arch", router.downloadAgentInstaller)
			agents.GET("/script/:platform", router.getAgentInstallScript)
		}

		// Protected routes (require JWT)
		protected := api.Group("")
		protected.Use(middleware.AuthMiddleware(cfg.JWTSecret))
		{
			// Auth
			protected.POST("/auth/logout", router.logout)
			protected.GET("/auth/me", router.me)

			// Devices
			protected.GET("/devices", router.listDevices)
			protected.GET("/devices/:id", router.getDevice)
			protected.DELETE("/devices/:id", middleware.RequireRole("admin", "operator"), router.deleteDevice)
			protected.GET("/devices/:id/metrics", router.getDeviceMetrics)
			protected.POST("/devices/:id/commands", middleware.RequireRole("admin", "operator"), router.executeCommand)
			protected.POST("/devices/:id/uninstall", middleware.RequireRole("admin"), router.uninstallAgent)
			protected.POST("/devices/:id/ping", router.pingAgent)
			protected.GET("/devices/:id/commands", router.listDeviceCommands)

			// Commands
			protected.GET("/commands", router.listCommands)
			protected.GET("/commands/:id", router.getCommand)

			// Scripts
			protected.GET("/scripts", router.listScripts)
			protected.POST("/scripts", middleware.RequireRole("admin", "operator"), router.createScript)
			protected.GET("/scripts/:id", router.getScript)
			protected.PUT("/scripts/:id", middleware.RequireRole("admin", "operator"), router.updateScript)
			protected.DELETE("/scripts/:id", middleware.RequireRole("admin"), router.deleteScript)
			protected.POST("/scripts/:id/execute", middleware.RequireRole("admin", "operator"), router.executeScript)

			// Alerts
			protected.GET("/alerts", router.listAlerts)
			protected.GET("/alerts/:id", router.getAlert)
			protected.POST("/alerts/:id/acknowledge", middleware.RequireRole("admin", "operator"), router.acknowledgeAlert)
			protected.POST("/alerts/:id/resolve", middleware.RequireRole("admin", "operator"), router.resolveAlert)

			// Alert Rules
			protected.GET("/alert-rules", router.listAlertRules)
			protected.POST("/alert-rules", middleware.RequireRole("admin"), router.createAlertRule)
			protected.GET("/alert-rules/:id", router.getAlertRule)
			protected.PUT("/alert-rules/:id", middleware.RequireRole("admin"), router.updateAlertRule)
			protected.DELETE("/alert-rules/:id", middleware.RequireRole("admin"), router.deleteAlertRule)

			// Dashboard
			protected.GET("/dashboard/stats", router.getDashboardStats)

			// Settings
			protected.GET("/settings", router.getSettings)
			protected.PUT("/settings", middleware.RequireRole("admin"), router.updateSettings)

			// Users (admin only)
			protected.GET("/users", middleware.RequireRole("admin"), router.listUsers)
			protected.POST("/users", middleware.RequireRole("admin"), router.createUser)
			protected.PUT("/users/:id", middleware.RequireRole("admin"), router.updateUser)
			protected.DELETE("/users/:id", middleware.RequireRole("admin"), router.deleteUser)

			// Enrollment Tokens (admin only)
			protected.GET("/enrollment-tokens", middleware.RequireRole("admin"), router.listEnrollmentTokens)
			protected.POST("/enrollment-tokens", middleware.RequireRole("admin"), router.createEnrollmentToken)
			protected.GET("/enrollment-tokens/:id", middleware.RequireRole("admin"), router.getEnrollmentToken)
			protected.PUT("/enrollment-tokens/:id", middleware.RequireRole("admin"), router.updateEnrollmentToken)
			protected.DELETE("/enrollment-tokens/:id", middleware.RequireRole("admin"), router.deleteEnrollmentToken)
			protected.POST("/enrollment-tokens/:id/regenerate", middleware.RequireRole("admin"), router.regenerateEnrollmentToken)

			// Agent Installers (authenticated users can view)
			protected.GET("/agents/installers", router.listAgentInstallers)

			// Agent Version Management
			protected.GET("/agents/versions", router.listAgentVersions)
			protected.GET("/devices/:id/version-history", router.getDeviceVersionHistory)
		}
	}

	// WebSocket routes
	ws := r.Group("/ws")
	{
		ws.GET("/agent", router.handleAgentWebSocket)
		ws.GET("/dashboard", middleware.AuthMiddleware(cfg.JWTSecret), router.handleDashboardWebSocket)
	}

	// Backwards-compatible WebSocket route for older agents connecting to /ws directly
	r.GET("/ws", router.handleAgentWebSocket)

	return r
}

// NewRouterWithServices creates a router with full service dependency injection
func NewRouterWithServices(services *Services) *gin.Engine {
	if services.Config.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(securityHeadersMiddleware())
	r.Use(corsMiddleware(services.Config))

	// Create inventory handlers
	inventoryHandlers := NewInventoryHandlers(services)

	// Health check endpoints for load balancer
	r.GET("/health", healthCheck(services))
	r.GET("/health/live", livenessCheck())
	r.GET("/health/ready", readinessCheck(services))

	// API routes
	api := r.Group("/api")
	{
		// Public routes with rate limiting
		auth := api.Group("/auth")
		auth.Use(rateLimitMiddleware(services.Redis, services.Config.RateLimitRequests, services.Config.RateLimitWindow))
		{
			auth.POST("/login", loginHandler(services))
			auth.POST("/refresh", refreshTokenHandler(services))
		}

		// Agent routes (uses enrollment token)
		agent := api.Group("/agent")
		agent.Use(middleware.AgentAuthMiddleware(services.Config.EnrollmentToken))
		{
			agent.POST("/enroll", enrollAgentHandler(services))
		}

		// Agent update routes (public - agents call these for updates)
		agentUpdate := api.Group("/agent")
		{
			agentUpdate.GET("/version", getAgentVersionHandler(services))
			agentUpdate.GET("/update/download", downloadAgentUpdateHandler(services))
			agentUpdate.POST("/update/status", reportUpdateStatusHandler(services))
		}

		// Agent download routes (public with token validation)
		agents := api.Group("/agents")
		{
			agents.GET("/download/:platform/:arch", downloadAgentInstallerHandler(services))
			agents.GET("/script/:platform", getAgentInstallScriptHandler(services))
		}

		// Protected routes (require JWT)
		protected := api.Group("")
		protected.Use(middleware.AuthMiddleware(services.Config.JWTSecret))
		{
			// Auth
			protected.POST("/auth/logout", logoutHandler(services))
			protected.GET("/auth/me", meHandler(services))

			// Devices
			protected.GET("/devices", listDevicesHandler(services))
			protected.GET("/devices/:id", getDeviceHandler(services))
			protected.DELETE("/devices/:id", middleware.RequireRole("admin", "operator"), deleteDeviceHandler(services))
			protected.GET("/devices/:id/metrics", getDeviceMetricsHandler(services))
			protected.POST("/devices/:id/commands", middleware.RequireRole("admin", "operator"), executeCommandHandler(services))
			protected.POST("/devices/:id/uninstall", middleware.RequireRole("admin"), uninstallAgentHandler(services))
			protected.POST("/devices/:id/ping", pingAgentHandler(services))
			protected.GET("/devices/:id/commands", listDeviceCommandsHandler(services))

			// Inventory endpoints (new)
			protected.GET("/devices/:id/inventory", inventoryHandlers.GetDeviceInventory)
			protected.GET("/devices/:id/inventory/software", inventoryHandlers.GetDeviceSoftware)
			protected.GET("/devices/:id/inventory/services", inventoryHandlers.GetDeviceServices)
			protected.GET("/devices/:id/security", inventoryHandlers.GetDeviceSecurity)
			protected.GET("/devices/:id/users", inventoryHandlers.GetDeviceUsers)
			protected.GET("/devices/:id/hardware", inventoryHandlers.GetDeviceHardware)
			protected.POST("/devices/:id/inventory/collect", middleware.RequireRole("admin", "operator"), inventoryHandlers.TriggerInventoryCollection)

			// Fleet-wide inventory endpoints
			protected.GET("/inventory/software", inventoryHandlers.GetFleetSoftware)
			protected.GET("/inventory/vulnerabilities", inventoryHandlers.GetFleetVulnerabilities)
			protected.GET("/reports/security-posture", inventoryHandlers.GetSecurityPostureReport)

			// Commands
			protected.GET("/commands", listCommandsHandler(services))
			protected.GET("/commands/:id", getCommandHandler(services))

			// Scripts
			protected.GET("/scripts", listScriptsHandler(services))
			protected.POST("/scripts", middleware.RequireRole("admin", "operator"), createScriptHandler(services))
			protected.GET("/scripts/:id", getScriptHandler(services))
			protected.PUT("/scripts/:id", middleware.RequireRole("admin", "operator"), updateScriptHandler(services))
			protected.DELETE("/scripts/:id", middleware.RequireRole("admin"), deleteScriptHandler(services))
			protected.POST("/scripts/:id/execute", middleware.RequireRole("admin", "operator"), executeScriptHandler(services))

			// Alerts
			protected.GET("/alerts", listAlertsHandler(services))
			protected.GET("/alerts/:id", getAlertHandler(services))
			protected.POST("/alerts/:id/acknowledge", middleware.RequireRole("admin", "operator"), acknowledgeAlertHandler(services))
			protected.POST("/alerts/:id/resolve", middleware.RequireRole("admin", "operator"), resolveAlertHandler(services))

			// Alert Rules
			protected.GET("/alert-rules", listAlertRulesHandler(services))
			protected.POST("/alert-rules", middleware.RequireRole("admin"), createAlertRuleHandler(services))
			protected.GET("/alert-rules/:id", getAlertRuleHandler(services))
			protected.PUT("/alert-rules/:id", middleware.RequireRole("admin"), updateAlertRuleHandler(services))
			protected.DELETE("/alert-rules/:id", middleware.RequireRole("admin"), deleteAlertRuleHandler(services))

			// Dashboard
			protected.GET("/dashboard/stats", getDashboardStatsHandler(services))

			// Settings
			protected.GET("/settings", getSettingsHandler(services))
			protected.PUT("/settings", middleware.RequireRole("admin"), updateSettingsHandler(services))

			// Users (admin only)
			protected.GET("/users", middleware.RequireRole("admin"), listUsersHandler(services))
			protected.POST("/users", middleware.RequireRole("admin"), createUserHandler(services))
			protected.PUT("/users/:id", middleware.RequireRole("admin"), updateUserHandler(services))
			protected.DELETE("/users/:id", middleware.RequireRole("admin"), deleteUserHandler(services))

			// Enrollment Tokens (admin only)
			protected.GET("/enrollment-tokens", middleware.RequireRole("admin"), listEnrollmentTokensHandler(services))
			protected.POST("/enrollment-tokens", middleware.RequireRole("admin"), createEnrollmentTokenHandler(services))
			protected.GET("/enrollment-tokens/:id", middleware.RequireRole("admin"), getEnrollmentTokenHandler(services))
			protected.PUT("/enrollment-tokens/:id", middleware.RequireRole("admin"), updateEnrollmentTokenHandler(services))
			protected.DELETE("/enrollment-tokens/:id", middleware.RequireRole("admin"), deleteEnrollmentTokenHandler(services))
			protected.POST("/enrollment-tokens/:id/regenerate", middleware.RequireRole("admin"), regenerateEnrollmentTokenHandler(services))

			// Agent Installers (authenticated users can view)
			protected.GET("/agents/installers", listAgentInstallersHandler(services))

			// Agent Version Management
			protected.GET("/agents/versions", listAgentVersionsHandler(services))
			protected.GET("/devices/:id/version-history", getDeviceVersionHistoryHandler(services))

			// Mobile device endpoints
			protected.GET("/mobile/devices", listMobileDevicesHandler(services))
			protected.GET("/mobile/devices/:id", getMobileDeviceHandler(services))
			protected.POST("/mobile/devices/:id/locate", middleware.RequireRole("admin", "operator"), locateMobileDeviceHandler(services))
			protected.POST("/mobile/devices/:id/lock", middleware.RequireRole("admin", "operator"), lockMobileDeviceHandler(services))
			protected.POST("/mobile/devices/:id/wipe", middleware.RequireRole("admin"), wipeMobileDeviceHandler(services))
		}

		// Mobile enrollment routes (public with token)
		mobile := api.Group("/mobile")
		{
			mobile.POST("/enroll", enrollMobileDeviceHandler(services))
			mobile.POST("/push/register", registerPushTokenHandler(services))
		}
	}

	// WebSocket routes
	ws := r.Group("/ws")
	{
		ws.GET("/agent", handleAgentWebSocketWithServices(services))
		ws.GET("/dashboard", middleware.AuthMiddleware(services.Config.JWTSecret), handleDashboardWebSocketWithServices(services))
	}

	// Backwards-compatible WebSocket route
	r.GET("/ws", handleAgentWebSocketWithServices(services))

	return r
}

// Health check handlers for load balancer

func healthCheck(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().UTC(),
			"serverId":  services.Config.ServerID,
		})
	}
}

func livenessCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "alive",
		})
	}
}

func readinessCheck(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check database connectivity
		dbHealthy := true
		if err := services.DB.Pool().Ping(c.Request.Context()); err != nil {
			dbHealthy = false
		}

		// Check Redis connectivity
		redisHealthy := true
		if err := services.Redis.Ping(c.Request.Context()); err != nil {
			redisHealthy = false
		}

		if !dbHealthy || !redisHealthy {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":   "not_ready",
				"database": dbHealthy,
				"redis":    redisHealthy,
			})
			return
		}

		// Get memory stats for monitoring
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		c.JSON(http.StatusOK, gin.H{
			"status":   "ready",
			"database": true,
			"redis":    true,
			"serverId": services.Config.ServerID,
			"memory": gin.H{
				"allocMB":      mem.Alloc / 1024 / 1024,
				"sysMemMB":     mem.Sys / 1024 / 1024,
				"numGoroutine": runtime.NumGoroutine(),
			},
		})
	}
}

// securityHeadersMiddleware adds security headers to all responses
func securityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		// Only add HSTS in production
		if gin.Mode() == gin.ReleaseMode {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		c.Next()
	}
}

// corsMiddleware handles CORS with configurable allowed origins
func corsMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		// Check if origin is allowed
		allowed := false
		if cfg.Environment != "production" {
			// In development, allow all origins
			allowed = true
		} else {
			for _, allowedOrigin := range cfg.AllowedOrigins {
				if origin == allowedOrigin {
					allowed = true
					break
				}
			}
		}

		if allowed && origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Enrollment-Token, X-Agent-Token")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// rateLimitMiddleware implements rate limiting using Redis
func rateLimitMiddleware(cache *cache.Cache, maxRequests int, windowSeconds int) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cache == nil {
			c.Next()
			return
		}

		key := "ratelimit:" + c.ClientIP()

		count, err := cache.Incr(c.Request.Context(), key)
		if err != nil {
			// If Redis fails, allow the request
			c.Next()
			return
		}

		// Set expiry on first request
		if count == 1 {
			cache.Expire(c.Request.Context(), key, windowSeconds)
		}

		if int(count) > maxRequests {
			c.Header("Retry-After", string(rune(windowSeconds)))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded. Please try again later.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
