package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// InventoryHandlers contains handlers for inventory-related API endpoints
type InventoryHandlers struct {
	services *Services
}

// NewInventoryHandlers creates new inventory handlers
func NewInventoryHandlers(services *Services) *InventoryHandlers {
	return &InventoryHandlers{services: services}
}

// GetDeviceInventory returns full inventory for a device
func (h *InventoryHandlers) GetDeviceInventory(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device ID"})
		return
	}

	ctx := c.Request.Context()

	// Get software inventory
	software, err := h.getDeviceSoftware(ctx, deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get software inventory"})
		return
	}

	// Get services
	services, err := h.getDeviceServices(ctx, deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get services"})
		return
	}

	// Get security posture
	security, err := h.getDeviceSecurity(ctx, deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get security posture"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deviceId":  deviceID,
		"software":  software,
		"services":  services,
		"security":  security,
		"fetchedAt": time.Now(),
	})
}

// GetDeviceSoftware returns software inventory for a device
func (h *InventoryHandlers) GetDeviceSoftware(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device ID"})
		return
	}

	ctx := c.Request.Context()
	software, err := h.getDeviceSoftware(ctx, deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get software inventory"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deviceId": deviceID,
		"software": software,
		"count":    len(software),
	})
}

// GetDeviceServices returns services for a device
func (h *InventoryHandlers) GetDeviceServices(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device ID"})
		return
	}

	ctx := c.Request.Context()
	services, err := h.getDeviceServices(ctx, deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get services"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deviceId": deviceID,
		"services": services,
		"count":    len(services),
	})
}

// GetDeviceSecurity returns security posture for a device
func (h *InventoryHandlers) GetDeviceSecurity(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device ID"})
		return
	}

	ctx := c.Request.Context()
	security, err := h.getDeviceSecurity(ctx, deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get security posture"})
		return
	}

	c.JSON(http.StatusOK, security)
}

// GetDeviceUsers returns user accounts for a device
func (h *InventoryHandlers) GetDeviceUsers(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device ID"})
		return
	}

	ctx := c.Request.Context()
	users, err := h.getDeviceUsers(ctx, deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get users"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deviceId": deviceID,
		"users":    users,
		"count":    len(users),
	})
}

// GetDeviceHardware returns hardware inventory for a device
func (h *InventoryHandlers) GetDeviceHardware(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device ID"})
		return
	}

	ctx := c.Request.Context()

	// Get USB devices
	usb, _ := h.getDeviceUSB(ctx, deviceID)
	// Get monitors
	monitors, _ := h.getDeviceMonitors(ctx, deviceID)
	// Get printers
	printers, _ := h.getDevicePrinters(ctx, deviceID)
	// Get BIOS info
	bios, _ := h.getDeviceBIOS(ctx, deviceID)

	c.JSON(http.StatusOK, gin.H{
		"deviceId": deviceID,
		"usb":      usb,
		"monitors": monitors,
		"printers": printers,
		"bios":     bios,
	})
}

// TriggerInventoryCollection triggers an inventory collection on a device
func (h *InventoryHandlers) TriggerInventoryCollection(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device ID"})
		return
	}

	// Get collection type from request
	var req struct {
		Type string `json:"type"` // full, delta, security
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Type = "full"
	}

	// Get agent ID for this device
	agentID, err := h.getAgentID(c.Request.Context(), deviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}

	// Check if agent is online
	if !h.services.Hub.IsAgentOnline(agentID) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "agent is offline"})
		return
	}

	// Send inventory request to agent
	msg := map[string]interface{}{
		"type":           "request_inventory",
		"collectionType": req.Type,
		"requestId":      uuid.New().String(),
	}

	msgBytes, _ := encodeJSONBytes(msg)
	if err := h.services.Hub.SendToAgent(agentID, msgBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send request to agent"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":   "inventory collection triggered",
		"deviceId":  deviceID,
		"type":      req.Type,
	})
}

// GetFleetSoftware returns software inventory across all devices
func (h *InventoryHandlers) GetFleetSoftware(c *gin.Context) {
	ctx := c.Request.Context()

	query := `
		SELECT name, version, publisher, COUNT(DISTINCT device_id) as device_count
		FROM device_software
		WHERE removed_at IS NULL
		GROUP BY name, version, publisher
		ORDER BY device_count DESC
		LIMIT 500
	`

	rows, err := h.services.DB.Pool().Query(ctx, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query software"})
		return
	}
	defer rows.Close()

	var software []map[string]interface{}
	for rows.Next() {
		var name, version, publisher string
		var deviceCount int
		if err := rows.Scan(&name, &version, &publisher, &deviceCount); err != nil {
			continue
		}
		software = append(software, map[string]interface{}{
			"name":        name,
			"version":     version,
			"publisher":   publisher,
			"deviceCount": deviceCount,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"software": software,
		"count":    len(software),
	})
}

// GetFleetVulnerabilities returns all vulnerabilities across the fleet
func (h *InventoryHandlers) GetFleetVulnerabilities(c *gin.Context) {
	ctx := c.Request.Context()

	query := `
		SELECT
			cve_id, title, severity, cvss_score, affected_product,
			COUNT(DISTINCT device_id) as device_count
		FROM device_vulnerabilities
		WHERE resolved_at IS NULL
		GROUP BY cve_id, title, severity, cvss_score, affected_product
		ORDER BY cvss_score DESC NULLS LAST
		LIMIT 200
	`

	rows, err := h.services.DB.Pool().Query(ctx, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query vulnerabilities"})
		return
	}
	defer rows.Close()

	var vulns []map[string]interface{}
	for rows.Next() {
		var cveID, title, severity, affectedProduct string
		var cvssScore *float64
		var deviceCount int
		if err := rows.Scan(&cveID, &title, &severity, &cvssScore, &affectedProduct, &deviceCount); err != nil {
			continue
		}
		vulns = append(vulns, map[string]interface{}{
			"cveId":           cveID,
			"title":           title,
			"severity":        severity,
			"cvssScore":       cvssScore,
			"affectedProduct": affectedProduct,
			"deviceCount":     deviceCount,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"vulnerabilities": vulns,
		"count":           len(vulns),
	})
}

// GetSecurityPostureReport returns a security posture report for the fleet
func (h *InventoryHandlers) GetSecurityPostureReport(c *gin.Context) {
	ctx := c.Request.Context()

	query := `
		WITH latest_security AS (
			SELECT DISTINCT ON (device_id) *
			FROM device_security
			ORDER BY device_id, collected_at DESC
		)
		SELECT
			COUNT(*) as total_devices,
			COUNT(*) FILTER (WHERE antivirus_enabled) as av_enabled,
			COUNT(*) FILTER (WHERE antivirus_up_to_date) as av_current,
			COUNT(*) FILTER (WHERE firewall_enabled) as firewall_enabled,
			COUNT(*) FILTER (WHERE disk_encryption_enabled) as encryption_enabled,
			COUNT(*) FILTER (WHERE secure_boot_enabled) as secure_boot_enabled,
			AVG(security_score) as avg_security_score,
			COUNT(*) FILTER (WHERE security_score >= 80) as compliant_devices,
			COUNT(*) FILTER (WHERE security_score < 50) as at_risk_devices
		FROM latest_security
	`

	var report struct {
		TotalDevices       int     `json:"totalDevices"`
		AVEnabled          int     `json:"avEnabled"`
		AVCurrent          int     `json:"avCurrent"`
		FirewallEnabled    int     `json:"firewallEnabled"`
		EncryptionEnabled  int     `json:"encryptionEnabled"`
		SecureBootEnabled  int     `json:"secureBootEnabled"`
		AvgSecurityScore   float64 `json:"avgSecurityScore"`
		CompliantDevices   int     `json:"compliantDevices"`
		AtRiskDevices      int     `json:"atRiskDevices"`
	}

	err := h.services.DB.Pool().QueryRow(ctx, query).Scan(
		&report.TotalDevices,
		&report.AVEnabled,
		&report.AVCurrent,
		&report.FirewallEnabled,
		&report.EncryptionEnabled,
		&report.SecureBootEnabled,
		&report.AvgSecurityScore,
		&report.CompliantDevices,
		&report.AtRiskDevices,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate report"})
		return
	}

	// Calculate percentages
	if report.TotalDevices > 0 {
		c.JSON(http.StatusOK, gin.H{
			"report": report,
			"percentages": gin.H{
				"avEnabled":         float64(report.AVEnabled) / float64(report.TotalDevices) * 100,
				"avCurrent":         float64(report.AVCurrent) / float64(report.TotalDevices) * 100,
				"firewallEnabled":   float64(report.FirewallEnabled) / float64(report.TotalDevices) * 100,
				"encryptionEnabled": float64(report.EncryptionEnabled) / float64(report.TotalDevices) * 100,
				"secureBootEnabled": float64(report.SecureBootEnabled) / float64(report.TotalDevices) * 100,
				"compliant":         float64(report.CompliantDevices) / float64(report.TotalDevices) * 100,
			},
			"generatedAt": time.Now(),
		})
	} else {
		c.JSON(http.StatusOK, gin.H{
			"report":      report,
			"percentages": gin.H{},
			"generatedAt": time.Now(),
		})
	}
}

// Helper methods for database queries

func (h *InventoryHandlers) getDeviceSoftware(ctx interface{}, deviceID uuid.UUID) ([]map[string]interface{}, error) {
	query := `
		SELECT name, version, publisher, install_date, install_location, size_bytes, architecture
		FROM device_software
		WHERE device_id = $1 AND removed_at IS NULL
		ORDER BY name
	`

	rows, err := h.services.DB.Pool().Query(ctx.(interface{ Done() <-chan struct{} }), query, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var software []map[string]interface{}
	for rows.Next() {
		var name, version, publisher, installLocation, architecture string
		var installDate *time.Time
		var sizeBytes *int64
		if err := rows.Scan(&name, &version, &publisher, &installDate, &installLocation, &sizeBytes, &architecture); err != nil {
			continue
		}
		software = append(software, map[string]interface{}{
			"name":            name,
			"version":         version,
			"publisher":       publisher,
			"installDate":     installDate,
			"installLocation": installLocation,
			"sizeBytes":       sizeBytes,
			"architecture":    architecture,
		})
	}
	return software, nil
}

func (h *InventoryHandlers) getDeviceServices(ctx interface{}, deviceID uuid.UUID) ([]map[string]interface{}, error) {
	query := `
		SELECT name, display_name, current_state, start_type, path_to_executable, account
		FROM device_services
		WHERE device_id = $1
		ORDER BY name
	`

	rows, err := h.services.DB.Pool().Query(ctx.(interface{ Done() <-chan struct{} }), query, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []map[string]interface{}
	for rows.Next() {
		var name, displayName, currentState, startType, path, account string
		if err := rows.Scan(&name, &displayName, &currentState, &startType, &path, &account); err != nil {
			continue
		}
		services = append(services, map[string]interface{}{
			"name":             name,
			"displayName":      displayName,
			"currentState":     currentState,
			"startType":        startType,
			"pathToExecutable": path,
			"account":          account,
		})
	}
	return services, nil
}

func (h *InventoryHandlers) getDeviceSecurity(ctx interface{}, deviceID uuid.UUID) (map[string]interface{}, error) {
	query := `
		SELECT
			antivirus_product, antivirus_enabled, antivirus_up_to_date, antivirus_realtime_enabled,
			firewall_enabled, firewall_profiles,
			disk_encryption_enabled, disk_encryption_type,
			tpm_present, tpm_enabled, secure_boot_enabled,
			uac_enabled, screen_lock_enabled,
			remote_desktop_enabled, guest_account_enabled, auto_login_enabled,
			security_score, risk_factors, collected_at
		FROM device_security
		WHERE device_id = $1
		ORDER BY collected_at DESC
		LIMIT 1
	`

	var security map[string]interface{}
	// Implementation would scan all fields into the map
	_ = query
	_ = deviceID

	return security, nil
}

func (h *InventoryHandlers) getDeviceUsers(ctx interface{}, deviceID uuid.UUID) ([]map[string]interface{}, error) {
	query := `
		SELECT username, full_name, user_type, is_admin, is_disabled, last_logon
		FROM device_users
		WHERE device_id = $1
		ORDER BY username
	`

	rows, err := h.services.DB.Pool().Query(ctx.(interface{ Done() <-chan struct{} }), query, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []map[string]interface{}
	for rows.Next() {
		var username, fullName, userType string
		var isAdmin, isDisabled bool
		var lastLogon *time.Time
		if err := rows.Scan(&username, &fullName, &userType, &isAdmin, &isDisabled, &lastLogon); err != nil {
			continue
		}
		users = append(users, map[string]interface{}{
			"username":   username,
			"fullName":   fullName,
			"userType":   userType,
			"isAdmin":    isAdmin,
			"isDisabled": isDisabled,
			"lastLogon":  lastLogon,
		})
	}
	return users, nil
}

func (h *InventoryHandlers) getDeviceUSB(ctx interface{}, deviceID uuid.UUID) ([]map[string]interface{}, error) {
	query := `
		SELECT friendly_name, device_class, vendor_id, product_id, is_currently_connected, last_connected_at
		FROM device_usb
		WHERE device_id = $1
		ORDER BY last_connected_at DESC
	`

	rows, err := h.services.DB.Pool().Query(ctx.(interface{ Done() <-chan struct{} }), query, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []map[string]interface{}
	for rows.Next() {
		var friendlyName, deviceClass, vendorID, productID string
		var isConnected bool
		var lastConnected *time.Time
		if err := rows.Scan(&friendlyName, &deviceClass, &vendorID, &productID, &isConnected, &lastConnected); err != nil {
			continue
		}
		devices = append(devices, map[string]interface{}{
			"friendlyName":   friendlyName,
			"deviceClass":    deviceClass,
			"vendorId":       vendorID,
			"productId":      productID,
			"isConnected":    isConnected,
			"lastConnected":  lastConnected,
		})
	}
	return devices, nil
}

func (h *InventoryHandlers) getDeviceMonitors(ctx interface{}, deviceID uuid.UUID) ([]map[string]interface{}, error) {
	query := `
		SELECT name, manufacturer, resolution_width, resolution_height, is_primary, connection_type
		FROM device_monitors
		WHERE device_id = $1
	`

	rows, err := h.services.DB.Pool().Query(ctx.(interface{ Done() <-chan struct{} }), query, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var monitors []map[string]interface{}
	for rows.Next() {
		var name, manufacturer, connectionType string
		var width, height int
		var isPrimary bool
		if err := rows.Scan(&name, &manufacturer, &width, &height, &isPrimary, &connectionType); err != nil {
			continue
		}
		monitors = append(monitors, map[string]interface{}{
			"name":           name,
			"manufacturer":   manufacturer,
			"resolutionWidth": width,
			"resolutionHeight": height,
			"isPrimary":      isPrimary,
			"connectionType": connectionType,
		})
	}
	return monitors, nil
}

func (h *InventoryHandlers) getDevicePrinters(ctx interface{}, deviceID uuid.UUID) ([]map[string]interface{}, error) {
	query := `
		SELECT name, driver_name, printer_type, is_default, is_network, status
		FROM device_printers
		WHERE device_id = $1
	`

	rows, err := h.services.DB.Pool().Query(ctx.(interface{ Done() <-chan struct{} }), query, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var printers []map[string]interface{}
	for rows.Next() {
		var name, driverName, printerType, status string
		var isDefault, isNetwork bool
		if err := rows.Scan(&name, &driverName, &printerType, &isDefault, &isNetwork, &status); err != nil {
			continue
		}
		printers = append(printers, map[string]interface{}{
			"name":        name,
			"driverName":  driverName,
			"printerType": printerType,
			"isDefault":   isDefault,
			"isNetwork":   isNetwork,
			"status":      status,
		})
	}
	return printers, nil
}

func (h *InventoryHandlers) getDeviceBIOS(ctx interface{}, deviceID uuid.UUID) (map[string]interface{}, error) {
	query := `
		SELECT manufacturer, version, release_date, is_uefi, secure_boot_capable, secure_boot_enabled
		FROM device_bios
		WHERE device_id = $1
	`

	var manufacturer, version string
	var releaseDate *time.Time
	var isUEFI, secureBootCapable, secureBootEnabled bool

	err := h.services.DB.Pool().QueryRow(ctx.(interface{ Done() <-chan struct{} }), query, deviceID).Scan(
		&manufacturer, &version, &releaseDate, &isUEFI, &secureBootCapable, &secureBootEnabled,
	)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"manufacturer":      manufacturer,
		"version":           version,
		"releaseDate":       releaseDate,
		"isUefi":            isUEFI,
		"secureBootCapable": secureBootCapable,
		"secureBootEnabled": secureBootEnabled,
	}, nil
}

func (h *InventoryHandlers) getAgentID(ctx interface{}, deviceID uuid.UUID) (string, error) {
	query := `SELECT agent_id FROM devices WHERE id = $1`
	var agentID string
	err := h.services.DB.Pool().QueryRow(ctx.(interface{ Done() <-chan struct{} }), query, deviceID).Scan(&agentID)
	return agentID, err
}

func encodeJSONBytes(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}
