package api

import (
	"net/http"
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
