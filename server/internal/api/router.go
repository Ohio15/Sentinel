package api

import (
	"log"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sentinel/server/internal/middleware"
	"github.com/sentinel/server/internal/pki"
	"github.com/sentinel/server/internal/websocket"
	"github.com/sentinel/server/pkg/cache"
	"github.com/sentinel/server/pkg/config"
	"github.com/sentinel/server/pkg/database"
)

type Router struct {
	config          *config.Config
	db              *database.DB
	cache           *cache.Cache
	hub             WebSocketHub
	pki             *pki.PKI
	metricsRecorder MetricsRecorder
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
		// DC-001: Apply authentication rate limiting middleware for brute force protection
		auth := api.Group("/auth")
		auth.Use(rateLimitMiddleware(cache, cfg.RateLimitRequests, cfg.RateLimitWindow))
		auth.Use(middleware.AuthRateLimitMiddleware())
		{
			auth.POST("/login", router.login)
			auth.POST("/refresh", router.refreshToken)
			auth.GET("/invitations/validate", router.validateInvitation)
			auth.POST("/register", router.register)
		}

		// Agent routes (uses enrollment token)
		agent := api.Group("/agent")
		agent.Use(middleware.NewAgentAuthMiddleware(db.Pool(), cfg.EnrollmentToken))
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
		csrfConfig := middleware.DefaultCSRFConfig()
		if cfg.Environment != "production" {
			csrfConfig.Secure = false
			csrfConfig.SameSite = 2 // http.SameSiteLaxMode
		}
		protected.Use(middleware.CSRFMiddleware(csrfConfig))
		{
			// Auth
			protected.POST("/auth/logout", router.logout)
			protected.GET("/auth/me", router.me)

			// Devices
			protected.GET("/devices", router.listDevices)
			protected.GET("/devices/cert-status", router.getDeviceCertStatuses)
			protected.GET("/certificates/info", router.getCertificateInfo)
			protected.GET("/devices/:id", router.getDevice)
			protected.PUT("/devices/:id", middleware.RequireRole("admin", "operator"), router.updateDevice)
			protected.DELETE("/devices/:id", middleware.RequireRole("admin", "operator"), router.deleteDevice)
			protected.GET("/devices/:id/metrics", router.getDeviceMetrics)
			protected.POST("/devices/:id/commands", middleware.RequireRole("admin", "operator"), router.executeCommand)
			protected.POST("/devices/:id/uninstall", middleware.RequireRole("admin"), router.uninstallAgent)
			protected.POST("/devices/:id/disable", middleware.RequireRole("admin", "operator"), router.disableDevice)
			protected.POST("/devices/:id/enable", middleware.RequireRole("admin", "operator"), router.enableDevice)
			protected.POST("/devices/:id/ping", router.pingAgent)
			protected.POST("/devices/:id/force-update", middleware.RequireRole("admin"), router.forceUpdate)
			protected.POST("/devices/:id/power", middleware.RequireRole("admin", "operator"), router.powerAction)
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

			// Invitations (admin only)
			protected.GET("/invitations", middleware.RequireRole("admin"), router.listInvitations)
			protected.POST("/invitations", middleware.RequireRole("admin"), router.createInvitation)
			protected.DELETE("/invitations/:id", middleware.RequireRole("admin"), router.deleteInvitation)

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
		ws.GET("/dashboard", router.handleDashboardWebSocket)
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
		// API Documentation (public)
		api.GET("", apiInfoHandler())
		api.GET("/docs", serveSwaggerUI())
		api.GET("/openapi.yaml", serveOpenAPISpec())

		// Public routes with rate limiting
		// DC-001: Apply authentication rate limiting middleware for brute force protection
		auth := api.Group("/auth")
		auth.Use(rateLimitMiddleware(services.Redis, services.Config.RateLimitRequests, services.Config.RateLimitWindow))
		auth.Use(middleware.AuthRateLimitMiddleware())
		{
			auth.POST("/login", loginHandler(services))
			auth.POST("/refresh", refreshTokenHandler(services))
			auth.GET("/invitations/validate", validateInvitationHandler(services))
			auth.POST("/register", registerHandler(services))
			auth.POST("/mfa/verify", verifyMFACodeHandler(services)) // MFA verification during login
		}

		// Agent routes (uses enrollment token)
		agent := api.Group("/agent")
		agent.Use(middleware.NewAgentAuthMiddleware(services.DB.Pool(), services.Config.EnrollmentToken))
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

		// Bootstrap routes (public - for bootstrapper-based installation)
		bootstrap := api.Group("/bootstrap")
		{
			bootstrap.GET("/agent-info", getBootstrapAgentInfoHandler(services))
			bootstrap.GET("/download", downloadBootstrapHandler(services))
			bootstrap.GET("/agent", downloadBootstrapAgentHandler(services))
			bootstrap.GET("/watchdog", downloadBootstrapWatchdogHandler(services))
			bootstrap.GET("/desktop-helper", downloadBootstrapDesktopHelperHandler(services))
			bootstrap.GET("/openh264", downloadBootstrapOpenH264Handler(services))
		}

		// Protected routes (require JWT)
		protected := api.Group("")
		protected.Use(middleware.AuthOrAPIKeyMiddleware(services.Config.JWTSecret, services.Config.APIKey))
		protected.Use(middleware.CSRFMiddleware(middleware.DefaultCSRFConfig()))
		{
			// Auth
			protected.POST("/auth/logout", logoutHandler(services))
			protected.GET("/auth/me", meHandler(services))

			// Devices
			protected.GET("/devices", listDevicesHandler(services))
			protected.GET("/devices/cert-status", getDeviceCertStatusesHandler(services))
			protected.GET("/certificates/info", getCertificateInfoHandler(services))
			protected.GET("/devices/:id", getDeviceHandler(services))
			protected.PUT("/devices/:id", middleware.RequireRole("admin", "operator"), updateDeviceHandler(services))
			protected.DELETE("/devices/:id", middleware.RequireRole("admin", "operator"), deleteDeviceHandler(services))
			protected.GET("/devices/:id/metrics", getDeviceMetricsHandler(services))
			protected.POST("/devices/:id/commands", middleware.RequireRole("admin", "operator"), executeCommandHandler(services))
			protected.POST("/devices/:id/uninstall", middleware.RequireRole("admin"), uninstallAgentHandler(services))
			protected.POST("/devices/:id/disable", middleware.RequireRole("admin", "operator"), disableDeviceHandler(services))
			protected.POST("/devices/:id/enable", middleware.RequireRole("admin", "operator"), enableDeviceHandler(services))
			protected.POST("/devices/:id/ping", pingAgentHandler(services))
			protected.POST("/devices/:id/force-update", middleware.RequireRole("admin"), forceUpdateHandler(services))
			protected.POST("/devices/:id/power", middleware.RequireRole("admin", "operator"), powerActionHandler(services))
			protected.POST("/devices/:id/install-updates", middleware.RequireRole("admin", "operator"), installUpdatesHandler(services))
			protected.GET("/devices/:id/commands", listDeviceCommandsHandler(services))

			// Agent installer (generates pre-configured script for direct download)
			protected.GET("/installer/:platform", generateConfiguredInstallerHandler(services))

			// Agent installer with platform/arch query params (generates SPK/EXE with embedded config)
			// GET /api/agents/installer?platform=synology&arch=amd64
			protected.GET("/agents/installer", generateInstallerDownloadHandler(services))

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

			// Webhooks
			protected.GET("/webhooks", listWebhooksHandler(services))
			protected.POST("/webhooks", middleware.RequireRole("admin"), createWebhookHandler(services))
			protected.GET("/webhooks/:id", getWebhookHandler(services))
			protected.PUT("/webhooks/:id", middleware.RequireRole("admin"), updateWebhookHandler(services))
			protected.DELETE("/webhooks/:id", middleware.RequireRole("admin"), deleteWebhookHandler(services))
			protected.POST("/webhooks/:id/test", middleware.RequireRole("admin"), testWebhookHandler(services))
			protected.GET("/webhooks/:id/deliveries", listWebhookDeliveriesHandler(services))

			// Dashboard
			protected.GET("/dashboard/stats", getDashboardStatsHandler(services))

			// Reports (PDF generation) - Note: /reports/security-posture is in inventory section
			protected.GET("/reports", listReportTypesHandler(services))
			protected.GET("/reports/alert-history", generateAlertHistoryReportHandler(services))
			protected.GET("/reports/executive", generateExecutiveReportHandler(services))
			protected.GET("/devices/:id/report", generateDeviceReportHandler(services))

			// Patch Management
			protected.GET("/patch-policies", listPatchPoliciesHandler(services))
			protected.POST("/patch-policies", middleware.RequireRole("admin"), createPatchPolicyHandler(services))
			protected.GET("/patch-policies/:id", getPatchPolicyHandler(services))
			protected.PUT("/patch-policies/:id", middleware.RequireRole("admin"), updatePatchPolicyHandler(services))
			protected.DELETE("/patch-policies/:id", middleware.RequireRole("admin"), deletePatchPolicyHandler(services))
			protected.GET("/patch-approvals", listPatchApprovalsHandler(services))
			protected.POST("/patch-approvals/:id/approve", middleware.RequireRole("admin", "operator"), approvePatchHandler(services))
			protected.POST("/patch-approvals/bulk", middleware.RequireRole("admin", "operator"), bulkApprovePatchesHandler(services))
			protected.GET("/devices/:id/pending-patches", getPendingPatchesForDeviceHandler(services))

			// Multi-Factor Authentication (MFA/TOTP)
			protected.GET("/auth/mfa/status", getMFAStatusHandler(services))
			protected.POST("/auth/mfa/setup", setupMFAHandler(services))
			protected.POST("/auth/mfa/verify-setup", verifyMFASetupHandler(services))
			protected.POST("/auth/mfa/disable", disableMFAHandler(services))
			protected.POST("/auth/mfa/regenerate-backup-codes", regenerateBackupCodesHandler(services))

			// Script Scheduling
			protected.GET("/schedules", listSchedulesHandler(services))
			protected.POST("/schedules", middleware.RequireRole("admin", "operator"), createScheduleHandler(services))
			protected.GET("/schedules/:id", getScheduleHandler(services))
			protected.PUT("/schedules/:id", middleware.RequireRole("admin", "operator"), updateScheduleHandler(services))
			protected.DELETE("/schedules/:id", middleware.RequireRole("admin"), deleteScheduleHandler(services))
			protected.POST("/schedules/:id/toggle", middleware.RequireRole("admin", "operator"), toggleScheduleHandler(services))
			protected.POST("/schedules/:id/run", middleware.RequireRole("admin", "operator"), runScheduleNowHandler(services))
			protected.GET("/executions", listExecutionsHandler(services))
			protected.GET("/executions/:id", getExecutionHandler(services))

			// Data Export (CSV/Excel)
			protected.GET("/export", listExportTypesHandler(services))
			protected.GET("/export/devices", exportDevicesHandler(services))
			protected.GET("/export/alerts", exportAlertsHandler(services))
			protected.GET("/export/updates", exportUpdatesHandler(services))
			protected.GET("/export/software", exportSoftwareHandler(services))
			protected.GET("/export/users", middleware.RequireRole("admin"), exportUsersHandler(services))
			protected.GET("/export/metrics", exportMetricsHandler(services))

			// Global Search (Cmd+K)
			protected.GET("/search", globalSearchHandler(services))
			protected.GET("/search/suggestions", searchSuggestionsHandler(services))
			protected.GET("/search/quick-actions", quickActionsHandler(services))

			// USB/Peripheral Device Management
			RegisterUSBRoutes(protected, services.DB.Pool())

			// Settings
			protected.GET("/settings", getSettingsHandler(services))
			protected.PUT("/settings", middleware.RequireRole("admin"), updateSettingsHandler(services))

			// Credential Management (admin only)
			RegisterCredentialRoutes(protected, services)

			// Users (admin only)
			protected.GET("/users", middleware.RequireRole("admin"), listUsersHandler(services))
			protected.POST("/users", middleware.RequireRole("admin"), createUserHandler(services))
			protected.PUT("/users/:id", middleware.RequireRole("admin"), updateUserHandler(services))
			protected.DELETE("/users/:id", middleware.RequireRole("admin"), deleteUserHandler(services))

			// Invitations (admin only)
			protected.GET("/invitations", middleware.RequireRole("admin"), listInvitationsHandler(services))
			protected.POST("/invitations", middleware.RequireRole("admin"), createInvitationHandler(services))
			protected.DELETE("/invitations/:id", middleware.RequireRole("admin"), deleteInvitationHandler(services))

			// Enrollment Tokens (admin only - SECURITY: moved from public routes)
			protected.GET("/enrollment-tokens", middleware.RequireRole("admin"), listEnrollmentTokensHandler(services))
			protected.POST("/enrollment-tokens", middleware.RequireRole("admin"), createEnrollmentTokenHandler(services))
			protected.GET("/enrollment-tokens/:id", middleware.RequireRole("admin"), getEnrollmentTokenHandler(services))
			protected.PUT("/enrollment-tokens/:id", middleware.RequireRole("admin"), updateEnrollmentTokenHandler(services))
			protected.DELETE("/enrollment-tokens/:id", middleware.RequireRole("admin"), deleteEnrollmentTokenHandler(services))
			protected.POST("/enrollment-tokens/:id/regenerate", middleware.RequireRole("admin"), regenerateEnrollmentTokenHandler(services))

			// Agent Installers (authenticated users can view)
			protected.GET("/agents/installers", listAgentInstallersHandler(services))

			// Agent Installer Generation (generates installer with embedded config)
			// GET /api/agent/installer?platform=windows&arch=amd64
			protected.GET("/agent/installer", generateInstallerDownloadHandler(services))

			// Agent Version Management
			protected.GET("/agents/versions", listAgentVersionsHandler(services))
			protected.GET("/devices/:id/version-history", getDeviceVersionHistoryHandler(services))

			// Mobile device endpoints
			protected.GET("/mobile/devices", listMobileDevicesHandler(services))
			protected.GET("/mobile/devices/:id", getMobileDeviceHandler(services))
			protected.POST("/mobile/devices/:id/locate", middleware.RequireRole("admin", "operator"), locateMobileDeviceHandler(services))
			protected.POST("/mobile/devices/:id/lock", middleware.RequireRole("admin", "operator"), lockMobileDeviceHandler(services))
			protected.POST("/mobile/devices/:id/wipe", middleware.RequireRole("admin"), wipeMobileDeviceHandler(services))

			// Clients
			protected.GET("/clients", listClientsHandler(services))
			protected.GET("/clients/:id", getClientHandler(services))
			protected.POST("/clients", middleware.RequireRole("admin"), createClientHandler(services))
			protected.PUT("/clients/:id", middleware.RequireRole("admin"), updateClientHandler(services))
			protected.DELETE("/clients/:id", middleware.RequireRole("admin"), deleteClientHandler(services))
			protected.POST("/devices/:id/assign-client", middleware.RequireRole("admin", "operator"), assignDeviceToClientHandler(services))
			protected.POST("/clients/bulk-assign-devices", middleware.RequireRole("admin", "operator"), bulkAssignDevicesToClientHandler(services))

			// Tickets
			protected.GET("/tickets", listTicketsHandler(services))
			protected.GET("/tickets/:id", getTicketHandler(services))
			protected.POST("/tickets", createTicketHandler(services))
			protected.PUT("/tickets/:id", updateTicketHandler(services))
			protected.DELETE("/tickets/:id", middleware.RequireRole("admin"), deleteTicketHandler(services))
			protected.GET("/tickets/:id/comments", getTicketCommentsHandler(services))
			protected.POST("/tickets/:id/comments", createTicketCommentHandler(services))
			protected.PUT("/tickets/comments/:commentId", updateTicketCommentHandler(services))
			protected.DELETE("/tickets/comments/:commentId", deleteTicketCommentHandler(services))
			protected.GET("/tickets/:id/activity", getTicketActivityHandler(services))
			protected.GET("/tickets/stats", getTicketStatsHandler(services))
			protected.GET("/ticket-templates", listTicketTemplatesHandler(services))
			protected.GET("/ticket-templates/:id", getTicketTemplateHandler(services))
			protected.POST("/ticket-templates", middleware.RequireRole("admin", "operator"), createTicketTemplateHandler(services))
			protected.PUT("/ticket-templates/:id", middleware.RequireRole("admin", "operator"), updateTicketTemplateHandler(services))
			protected.DELETE("/ticket-templates/:id", middleware.RequireRole("admin"), deleteTicketTemplateHandler(services))

			// SLA Policies
			protected.GET("/sla-policies", listSLAPoliciesHandler(services))
			protected.GET("/sla-policies/:id", getSLAPolicyHandler(services))
			protected.POST("/sla-policies", middleware.RequireRole("admin"), createSLAPolicyHandler(services))
			protected.PUT("/sla-policies/:id", middleware.RequireRole("admin"), updateSLAPolicyHandler(services))
			protected.DELETE("/sla-policies/:id", middleware.RequireRole("admin"), deleteSLAPolicyHandler(services))

			// Ticket Categories
			protected.GET("/ticket-categories", listTicketCategoriesHandler(services))
			protected.GET("/ticket-categories/:id", getTicketCategoryHandler(services))
			protected.POST("/ticket-categories", middleware.RequireRole("admin"), createTicketCategoryHandler(services))
			protected.PUT("/ticket-categories/:id", middleware.RequireRole("admin"), updateTicketCategoryHandler(services))
			protected.DELETE("/ticket-categories/:id", middleware.RequireRole("admin"), deleteTicketCategoryHandler(services))

			// Ticket Tags
			protected.GET("/ticket-tags", listTicketTagsHandler(services))
			protected.GET("/ticket-tags/:id", getTicketTagHandler(services))
			protected.POST("/ticket-tags", middleware.RequireRole("admin", "operator"), createTicketTagHandler(services))
			protected.PUT("/ticket-tags/:id", middleware.RequireRole("admin", "operator"), updateTicketTagHandler(services))
			protected.DELETE("/ticket-tags/:id", middleware.RequireRole("admin"), deleteTicketTagHandler(services))
			protected.GET("/tickets/:id/tags", getTicketTagsHandler(services))
			protected.POST("/tickets/:id/tags", assignTicketTagHandler(services))
			protected.DELETE("/tickets/:id/tags/:tagId", removeTicketTagHandler(services))

			// Ticket Links
			protected.GET("/tickets/:id/links", getTicketLinksHandler(services))
			protected.POST("/tickets/:id/links", createTicketLinkHandler(services))
			protected.DELETE("/ticket-links/:id", deleteTicketLinkHandler(services))

			// Custom Field Definitions
			protected.GET("/custom-fields", listCustomFieldDefinitionsHandler(services))
			protected.GET("/custom-fields/:id", getCustomFieldDefinitionHandler(services))
			protected.POST("/custom-fields", middleware.RequireRole("admin"), createCustomFieldDefinitionHandler(services))
			protected.PUT("/custom-fields/:id", middleware.RequireRole("admin"), updateCustomFieldDefinitionHandler(services))
			protected.DELETE("/custom-fields/:id", middleware.RequireRole("admin"), deleteCustomFieldDefinitionHandler(services))

			// Knowledge Base Categories
			protected.GET("/kb/categories", listKBCategoriesHandler(services))
			protected.GET("/kb/categories/:id", getKBCategoryHandler(services))
			protected.POST("/kb/categories", middleware.RequireRole("admin", "operator"), createKBCategoryHandler(services))
			protected.PUT("/kb/categories/:id", middleware.RequireRole("admin", "operator"), updateKBCategoryHandler(services))
			protected.DELETE("/kb/categories/:id", middleware.RequireRole("admin"), deleteKBCategoryHandler(services))

			// Knowledge Base Articles
			protected.GET("/kb/articles", listKBArticlesHandler(services))
			protected.GET("/kb/articles/:id", getKBArticleHandler(services))
			protected.POST("/kb/articles", middleware.RequireRole("admin", "operator"), createKBArticleHandler(services))
			protected.PUT("/kb/articles/:id", middleware.RequireRole("admin", "operator"), updateKBArticleHandler(services))
			protected.DELETE("/kb/articles/:id", middleware.RequireRole("admin"), deleteKBArticleHandler(services))
			protected.POST("/kb/articles/:id/feedback", submitKBArticleFeedbackHandler(services))
			protected.GET("/kb/articles/:id/feedback", getKBArticleFeedbackHandler(services))
			protected.POST("/kb/articles/:id/view", recordKBArticleViewHandler(services))

			// Performance Recordings
			protected.GET("/recordings", listRecordingsHandler(services))
			protected.POST("/recordings", middleware.RequireRole("admin", "operator"), startRecordingHandler(services))
			protected.GET("/recordings/:id", getRecordingHandler(services))
			protected.PUT("/recordings/:id", middleware.RequireRole("admin", "operator"), updateRecordingHandler(services))
			protected.DELETE("/recordings/:id", middleware.RequireRole("admin", "operator"), deleteRecordingHandler(services))
			protected.POST("/recordings/:id/stop", middleware.RequireRole("admin", "operator"), stopRecordingHandler(services))
			protected.GET("/recordings/:id/metrics", getRecordingMetricsHandler(services))
			protected.GET("/recordings/:id/export/csv", exportRecordingCSVHandler(services))
			protected.GET("/recordings/:id/export/json", exportRecordingJSONHandler(services))
			protected.GET("/devices/:id/recording/active", getActiveRecordingHandler(services))
		}

		// Mobile enrollment routes (public with token)
		mobile := api.Group("/mobile")
		{
			mobile.POST("/enroll", enrollMobileDeviceHandler(services))
			mobile.POST("/push/register", registerPushTokenHandler(services))
		}
		// Agent Installation Links routes (public portal + admin management)
		RegisterAgentLinkRoutes(api, protected, services)

		// RDP Remote Desktop routes
		registerRDPRoutes(api, protected, services)

		// WebAuthn / Passkey Authentication routes
		if err := RegisterWebAuthnRoutes(api, protected, services); err != nil {
			log.Printf("[WEBAUTHN] Failed to register routes: %v", err)
		}

		// Portal routes (client tenant mapping)
		RegisterPortalRoutes(api, protected, services)
	}

	// WebSocket routes
	ws := r.Group("/ws")
	{
		// Standard agent WebSocket (token auth)
		ws.GET("/agent", handleAgentWebSocketWithServices(services))
		// mTLS-authenticated agent WebSocket (certificate auth, no token needed)
		ws.GET("/agent/mtls", handleAgentWebSocketMTLS(services))
		// Dashboard WebSocket (JWT auth)
		ws.GET("/dashboard", middleware.AuthMiddleware(services.Config.JWTSecret), handleDashboardWebSocketWithServices(services))
	}

	// Agent certificate management routes (require mTLS)
	agentCerts := api.Group("/agent/certs")
	{
		agentCerts.POST("/renew", handleCertRenewal(services))
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
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Enrollment-Token, X-Agent-Token, X-CSRF-Token, X-API-Key")
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

		// Skip rate limiting for whitelisted IPs (localhost, private networks)
		clientIP := c.ClientIP()
		if middleware.IsWhitelisted(clientIP) {
			c.Next()
			return
		}

		key := "ratelimit:" + clientIP

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
			c.Header("Retry-After", strconv.Itoa(windowSeconds))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded. Please try again later.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
