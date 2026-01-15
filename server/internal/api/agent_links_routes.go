package api

import (
	"github.com/gin-gonic/gin"
	"github.com/sentinel/server/internal/middleware"
)

// RegisterAgentLinkRoutes registers all routes for the agent installation link system
func RegisterAgentLinkRoutes(api *gin.RouterGroup, protected *gin.RouterGroup, services *Services) {
	// Public installation portal routes (no auth - accessed via download token)
	publicInstall := api.Group("/public/install")
	{
		publicInstall.GET("/:downloadToken", validatePublicLinkHandler(services))
		publicInstall.GET("/:downloadToken/download", downloadInstallerHandler(services))
		publicInstall.GET("/:downloadToken/status", checkInstallationStatusHandler(services))
	}

	// Public installation code validation (no auth - used by installer)
	api.GET("/public/install/validate-code", validateInstallationCodeHandler(services))

	// Public generic installer download (no auth)
	api.GET("/download/agent", serveGenericInstallerHandler(services))
	api.GET("/download/agent/windows", serveGenericInstallerHandler(services))
	api.GET("/download/agent/test", serveTestInstallerHandler(services))

	// Admin routes for managing installation links (requires JWT + admin/operator role)
	agentLinks := protected.Group("/admin/agent-links")
	{
		agentLinks.GET("", listAgentLinksHandler(services))
		agentLinks.POST("", middleware.RequireRole("admin", "operator"), createAgentLinkHandler(services))
		agentLinks.GET("/stats", getAgentLinkStatsHandler(services))
		agentLinks.GET("/:linkId", getAgentLinkHandler(services))
		agentLinks.POST("/:linkId/resend", middleware.RequireRole("admin", "operator"), resendAgentLinkEmailHandler(services))
		agentLinks.POST("/:linkId/revoke", middleware.RequireRole("admin", "operator"), revokeAgentLinkHandler(services))
		agentLinks.DELETE("/:linkId", middleware.RequireRole("admin"), deleteAgentLinkHandler(services))
	}

	// Admin routes for installation codes (new code-based flow)
	installCodes := protected.Group("/admin/installation-codes")
	{
		installCodes.GET("", listInstallationCodesHandler(services))
		installCodes.POST("", middleware.RequireRole("admin", "operator"), createInstallationCodeHandler(services))
	}
}
