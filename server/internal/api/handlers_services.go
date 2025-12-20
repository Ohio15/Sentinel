package api

import (
	"github.com/gin-gonic/gin"
	"github.com/sentinel/server/internal/websocket"
)

// Handler wrappers that adapt existing handlers to use Services container
// These bridge the old Router-based handlers to the new service-based architecture

// Auth handlers
func loginHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.login
}

func refreshTokenHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.refreshToken
}

func logoutHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.logout
}

func meHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.me
}

// Agent handlers
func enrollAgentHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.enrollAgent
}

func getAgentVersionHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getAgentVersion
}

func downloadAgentUpdateHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.downloadAgentUpdate
}

func reportUpdateStatusHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.reportUpdateStatus
}

func downloadAgentInstallerHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.downloadAgentInstaller
}

func getAgentInstallScriptHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getAgentInstallScript
}

// Device handlers
func listDevicesHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listDevices
}

func getDeviceHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getDevice
}

func deleteDeviceHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteDevice
}

func getDeviceMetricsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getDeviceMetrics
}

func executeCommandHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.executeCommand
}

func uninstallAgentHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub.(*websocket.Hub)}
	return router.uninstallAgent
}

func disableDeviceHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub.(*websocket.Hub)}
	return router.disableDevice
}

func enableDeviceHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub.(*websocket.Hub)}
	return router.enableDevice
}

func pingAgentHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: services.Hub.(*websocket.Hub)}
	return router.pingAgent
}

func listDeviceCommandsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listDeviceCommands
}

// Command handlers
func listCommandsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listCommands
}

func getCommandHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getCommand
}

// Script handlers
func listScriptsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listScripts
}

func createScriptHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.createScript
}

func getScriptHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getScript
}

func updateScriptHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateScript
}

func deleteScriptHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteScript
}

func executeScriptHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.executeScript
}

// Alert handlers
func listAlertsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listAlerts
}

func getAlertHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getAlert
}

func acknowledgeAlertHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.acknowledgeAlert
}

func resolveAlertHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.resolveAlert
}

// Alert rule handlers
func listAlertRulesHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listAlertRules
}

func createAlertRuleHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.createAlertRule
}

func getAlertRuleHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getAlertRule
}

func updateAlertRuleHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateAlertRule
}

func deleteAlertRuleHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteAlertRule
}

// Dashboard handlers
func getDashboardStatsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getDashboardStats
}

// Settings handlers
func getSettingsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getSettings
}

func updateSettingsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateSettings
}

// User handlers
func listUsersHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listUsers
}

func createUserHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.createUser
}

func updateUserHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateUser
}

func deleteUserHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteUser
}

// Enrollment token handlers
func listEnrollmentTokensHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listEnrollmentTokens
}

func createEnrollmentTokenHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.createEnrollmentToken
}

func getEnrollmentTokenHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getEnrollmentToken
}

func updateEnrollmentTokenHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateEnrollmentToken
}

func deleteEnrollmentTokenHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteEnrollmentToken
}

func regenerateEnrollmentTokenHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.regenerateEnrollmentToken
}

// Agent installer handlers
func listAgentInstallersHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listAgentInstallers
}

// Agent version handlers
func listAgentVersionsHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listAgentVersions
}

func getDeviceVersionHistoryHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getDeviceVersionHistory
}

// WebSocket handlers
func handleAgentWebSocketWithServices(services *Services) gin.HandlerFunc {
	// Type assert to get the concrete Hub type
	hub, _ := services.Hub.(*websocket.Hub)
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: hub}
	return router.handleAgentWebSocket
}

func handleDashboardWebSocketWithServices(services *Services) gin.HandlerFunc {
	// Type assert to get the concrete Hub type
	hub, _ := services.Hub.(*websocket.Hub)
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis, hub: hub}
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
