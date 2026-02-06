package api

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler wrappers that adapt existing handlers to use Services container
// These bridge the old Router-based handlers to the new service-based architecture

// Auth handlers
func loginHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.login
}

func refreshTokenHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.refreshToken
}

func logoutHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.logout
}

func meHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.me
}

// Invitation handlers
func validateInvitationHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.validateInvitation
}

func registerHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.register
}

func listInvitationsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.listInvitations
}

func createInvitationHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.createInvitation
}

func deleteInvitationHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.deleteInvitation
}

// Agent handlers
func enrollAgentHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.enrollAgent
}

func getAgentVersionHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.getAgentVersion
}

func downloadAgentUpdateHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.downloadAgentUpdate
}

func reportUpdateStatusHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.reportUpdateStatus
}

func downloadAgentInstallerHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.downloadAgentInstaller
}

func getAgentInstallScriptHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.getAgentInstallScript
}

// Device handlers
func listDevicesHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.listDevices
}

func getDeviceHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.getDevice
}
func getDeviceCertStatusesHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.getDeviceCertStatuses
}


func deleteDeviceHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.deleteDevice
}

func updateDeviceHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.updateDevice
}

func getDeviceMetricsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.getDeviceMetrics
}

func executeCommandHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.executeCommand
}

func uninstallAgentHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.uninstallAgent
}

func disableDeviceHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.disableDevice
}

func enableDeviceHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.enableDevice
}

func pingAgentHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.pingAgent
}

func forceUpdateHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.forceUpdate
}

func listDeviceCommandsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.listDeviceCommands
}

// Command handlers
func listCommandsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.listCommands
}

func getCommandHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.getCommand
}

// Script handlers
func listScriptsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] listScriptsHandler recovered: %v", r)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			}
		}()
		log.Printf("[DEBUG] listScriptsHandler entering")
		router.listScripts(c)
		log.Printf("[DEBUG] listScriptsHandler completed")
	}
}

func createScriptHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.createScript
}

func getScriptHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.getScript
}

func updateScriptHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.updateScript
}

func deleteScriptHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.deleteScript
}

func executeScriptHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.executeScript
}

// Alert handlers
func listAlertsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.listAlerts
}

func getAlertHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.getAlert
}

func acknowledgeAlertHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.acknowledgeAlert
}

func resolveAlertHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.resolveAlert
}

// Alert rule handlers
func listAlertRulesHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.listAlertRules
}

func createAlertRuleHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.createAlertRule
}

func getAlertRuleHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.getAlertRule
}

func updateAlertRuleHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.updateAlertRule
}

func deleteAlertRuleHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.deleteAlertRule
}

// Dashboard handlers
func getDashboardStatsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.getDashboardStats
}

// Settings handlers
func getSettingsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.getSettings
}

func updateSettingsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.updateSettings
}

// User handlers
func listUsersHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] listUsersHandler recovered: %v", r)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			}
		}()
		log.Printf("[DEBUG] listUsersHandler entering")
		router.listUsers(c)
		log.Printf("[DEBUG] listUsersHandler completed")
	}
}

func createUserHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.createUser
}

func updateUserHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.updateUser
}

func deleteUserHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.deleteUser
}

// Enrollment token handlers
func listEnrollmentTokensHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.listEnrollmentTokens
}

func createEnrollmentTokenHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.createEnrollmentToken
}

func getEnrollmentTokenHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.getEnrollmentToken
}

func updateEnrollmentTokenHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.updateEnrollmentToken
}

func deleteEnrollmentTokenHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.deleteEnrollmentToken
}

func regenerateEnrollmentTokenHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.regenerateEnrollmentToken
}

// Agent installer handlers
func listAgentInstallersHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.listAgentInstallers
}

// Agent version handlers
func listAgentVersionsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.listAgentVersions
}

func getDeviceVersionHistoryHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub}
	return router.getDeviceVersionHistory
}

// WebSocket handlers
func handleAgentWebSocketWithServices(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub, pki: services.PKI}
	return router.handleAgentWebSocket
}

func handleDashboardWebSocketWithServices(services *Services) gin.HandlerFunc {
	router := &Router{
		config:          services.Config,
		db:              services.DB.AsDB(),
		cache:           services.Redis,
		hub:             services.Hub,
		metricsRecorder: services.MetricsRecorder,
	}
	return router.handleDashboardWebSocket
}

// Mobile device handlers
func listMobileDevicesHandler(services *Services) gin.HandlerFunc {
	return NewMobileHandlers(services).ListDevices
}

func getMobileDeviceHandler(services *Services) gin.HandlerFunc {
	return NewMobileHandlers(services).GetDevice
}

func locateMobileDeviceHandler(services *Services) gin.HandlerFunc {
	return NewMobileHandlers(services).LocateDevice
}

func lockMobileDeviceHandler(services *Services) gin.HandlerFunc {
	return NewMobileHandlers(services).LockDevice
}

func wipeMobileDeviceHandler(services *Services) gin.HandlerFunc {
	return NewMobileHandlers(services).WipeDevice
}

func enrollMobileDeviceHandler(services *Services) gin.HandlerFunc {
	return NewMobileHandlers(services).EnrollDevice
}

func registerPushTokenHandler(services *Services) gin.HandlerFunc {
	return NewMobileHandlers(services).RegisterPushToken
}
