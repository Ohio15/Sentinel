package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// MobileHandlers contains handlers for mobile device management
type MobileHandlers struct {
	services *Services
}

// NewMobileHandlers creates new mobile handlers
func NewMobileHandlers(services *Services) *MobileHandlers {
	return &MobileHandlers{services: services}
}

// ListDevices returns all mobile devices
func (h *MobileHandlers) ListDevices(c *gin.Context) {
	ctx := c.Request.Context()

	query := `
		SELECT
			md.id, md.device_id, d.hostname, d.platform,
			md.device_type, md.manufacturer, md.model,
			md.os_version, md.serial_number, md.is_managed,
			md.last_check_in, md.battery_level, md.storage_available_bytes
		FROM mobile_devices md
		JOIN devices d ON md.device_id = d.id
		ORDER BY md.last_check_in DESC
		LIMIT 500
	`

	rows, err := h.services.DB.Pool().Query(ctx, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query mobile devices"})
		return
	}
	defer rows.Close()

	var devices []map[string]interface{}
	for rows.Next() {
		var (
			id, deviceID                           uuid.UUID
			hostname, platform                     string
			deviceType, manufacturer, model        string
			osVersion, serialNumber                string
			isManaged                              bool
			lastCheckIn                            *time.Time
			batteryLevel                           *int
			storageAvailable                       *int64
		)

		if err := rows.Scan(
			&id, &deviceID, &hostname, &platform,
			&deviceType, &manufacturer, &model,
			&osVersion, &serialNumber, &isManaged,
			&lastCheckIn, &batteryLevel, &storageAvailable,
		); err != nil {
			continue
		}

		devices = append(devices, map[string]interface{}{
			"id":               id,
			"deviceId":         deviceID,
			"hostname":         hostname,
			"platform":         platform,
			"deviceType":       deviceType,
			"manufacturer":     manufacturer,
			"model":            model,
			"osVersion":        osVersion,
			"serialNumber":     serialNumber,
			"isManaged":        isManaged,
			"lastCheckIn":      lastCheckIn,
			"batteryLevel":     batteryLevel,
			"storageAvailable": storageAvailable,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"devices": devices,
		"count":   len(devices),
	})
}

// GetDevice returns a single mobile device
func (h *MobileHandlers) GetDevice(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device ID"})
		return
	}

	ctx := c.Request.Context()

	query := `
		SELECT
			md.id, md.device_id, d.hostname, d.platform,
			md.device_type, md.manufacturer, md.model,
			md.os_version, md.serial_number, md.imei, md.meid,
			md.phone_number, md.carrier, md.is_rooted, md.is_managed,
			md.mdm_enrolled, md.supervision_enabled,
			md.last_check_in, md.battery_level, md.battery_charging,
			md.storage_total_bytes, md.storage_available_bytes,
			md.wifi_mac, md.bluetooth_mac, md.last_known_latitude, md.last_known_longitude,
			md.created_at
		FROM mobile_devices md
		JOIN devices d ON md.device_id = d.id
		WHERE md.device_id = $1 OR md.id = $1
	`

	var device struct {
		ID                 uuid.UUID
		DeviceID           uuid.UUID
		Hostname           string
		Platform           string
		DeviceType         string
		Manufacturer       string
		Model              string
		OSVersion          string
		SerialNumber       string
		IMEI               *string
		MEID               *string
		PhoneNumber        *string
		Carrier            *string
		IsRooted           bool
		IsManaged          bool
		MDMEnrolled        bool
		SupervisionEnabled bool
		LastCheckIn        *time.Time
		BatteryLevel       *int
		BatteryCharging    *bool
		StorageTotal       *int64
		StorageAvailable   *int64
		WifiMAC            *string
		BluetoothMAC       *string
		Latitude           *float64
		Longitude          *float64
		CreatedAt          time.Time
	}

	err = h.services.DB.Pool().QueryRow(ctx, query, deviceID).Scan(
		&device.ID, &device.DeviceID, &device.Hostname, &device.Platform,
		&device.DeviceType, &device.Manufacturer, &device.Model,
		&device.OSVersion, &device.SerialNumber, &device.IMEI, &device.MEID,
		&device.PhoneNumber, &device.Carrier, &device.IsRooted, &device.IsManaged,
		&device.MDMEnrolled, &device.SupervisionEnabled,
		&device.LastCheckIn, &device.BatteryLevel, &device.BatteryCharging,
		&device.StorageTotal, &device.StorageAvailable,
		&device.WifiMAC, &device.BluetoothMAC, &device.Latitude, &device.Longitude,
		&device.CreatedAt,
	)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}

	c.JSON(http.StatusOK, device)
}

// LocateDevice triggers a locate request for a mobile device
func (h *MobileHandlers) LocateDevice(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device ID"})
		return
	}

	// Get push token for device
	ctx := c.Request.Context()
	var pushToken, platform string
	err = h.services.DB.Pool().QueryRow(ctx, `
		SELECT pt.token, pt.platform
		FROM push_tokens pt
		JOIN mobile_devices md ON pt.device_id = md.device_id
		WHERE md.device_id = $1 OR md.id = $1
		AND pt.is_active = true
		ORDER BY pt.updated_at DESC
		LIMIT 1
	`, deviceID).Scan(&pushToken, &platform)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no active push token for device"})
		return
	}

	// Send locate command via push notification
	if h.services.PushService != nil {
		err = h.services.PushService.SendCommand(ctx, pushToken, platform, "locate", nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send locate command"})
			return
		}
	}

	// Record the command
	_, err = h.services.DB.Pool().Exec(ctx, `
		INSERT INTO mobile_commands (id, device_id, command_type, status, created_at)
		VALUES ($1, $2, 'locate', 'pending', NOW())
	`, uuid.New(), deviceID)
	if err != nil {
		// Log but don't fail
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "locate command sent",
		"deviceId": deviceID,
	})
}

// LockDevice sends a lock command to a mobile device
func (h *MobileHandlers) LockDevice(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device ID"})
		return
	}

	var req struct {
		PIN     string `json:"pin"`
		Message string `json:"message"`
	}
	c.ShouldBindJSON(&req)

	ctx := c.Request.Context()

	// Get push token
	var pushToken, platform string
	err = h.services.DB.Pool().QueryRow(ctx, `
		SELECT pt.token, pt.platform
		FROM push_tokens pt
		JOIN mobile_devices md ON pt.device_id = md.device_id
		WHERE md.device_id = $1 OR md.id = $1
		AND pt.is_active = true
		ORDER BY pt.updated_at DESC
		LIMIT 1
	`, deviceID).Scan(&pushToken, &platform)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no active push token for device"})
		return
	}

	// Send lock command
	if h.services.PushService != nil {
		payload := map[string]interface{}{
			"pin":     req.PIN,
			"message": req.Message,
		}
		err = h.services.PushService.SendCommand(ctx, pushToken, platform, "lock", payload)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send lock command"})
			return
		}
	}

	// Record command
	_, _ = h.services.DB.Pool().Exec(ctx, `
		INSERT INTO mobile_commands (id, device_id, command_type, payload, status, created_at)
		VALUES ($1, $2, 'lock', $3, 'pending', NOW())
	`, uuid.New(), deviceID, map[string]interface{}{"message": req.Message})

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "lock command sent",
		"deviceId": deviceID,
	})
}

// WipeDevice sends a wipe command to a mobile device
func (h *MobileHandlers) WipeDevice(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device ID"})
		return
	}

	var req struct {
		Confirm bool `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !req.Confirm {
		c.JSON(http.StatusBadRequest, gin.H{"error": "must confirm wipe operation"})
		return
	}

	ctx := c.Request.Context()

	// Get push token
	var pushToken, platform string
	err = h.services.DB.Pool().QueryRow(ctx, `
		SELECT pt.token, pt.platform
		FROM push_tokens pt
		JOIN mobile_devices md ON pt.device_id = md.device_id
		WHERE md.device_id = $1 OR md.id = $1
		AND pt.is_active = true
		ORDER BY pt.updated_at DESC
		LIMIT 1
	`, deviceID).Scan(&pushToken, &platform)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no active push token for device"})
		return
	}

	// Send wipe command
	if h.services.PushService != nil {
		err = h.services.PushService.SendCommand(ctx, pushToken, platform, "wipe", nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send wipe command"})
			return
		}
	}

	// Record command
	_, _ = h.services.DB.Pool().Exec(ctx, `
		INSERT INTO mobile_commands (id, device_id, command_type, status, created_at)
		VALUES ($1, $2, 'wipe', 'pending', NOW())
	`, uuid.New(), deviceID)

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "wipe command sent - device data will be erased",
		"deviceId": deviceID,
	})
}

// EnrollDevice enrolls a new mobile device
func (h *MobileHandlers) EnrollDevice(c *gin.Context) {
	var req struct {
		EnrollmentToken string `json:"enrollmentToken" binding:"required"`
		DeviceInfo      struct {
			Manufacturer string `json:"manufacturer"`
			Model        string `json:"model"`
			OSVersion    string `json:"osVersion"`
			Platform     string `json:"platform"`
			SerialNumber string `json:"serialNumber"`
			IMEI         string `json:"imei,omitempty"`
			DeviceType   string `json:"deviceType"` // phone, tablet
		} `json:"deviceInfo" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	ctx := c.Request.Context()

	// Validate enrollment token
	var tokenID uuid.UUID
	err := h.services.DB.Pool().QueryRow(ctx, `
		SELECT id FROM enrollment_tokens
		WHERE token = $1
		AND is_active = true
		AND (expires_at IS NULL OR expires_at > NOW())
		AND (max_uses IS NULL OR used_count < max_uses)
	`, req.EnrollmentToken).Scan(&tokenID)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired enrollment token"})
		return
	}

	// Create device
	deviceID := uuid.New()
	agentID := uuid.New().String()
	hostname := req.DeviceInfo.Manufacturer + " " + req.DeviceInfo.Model

	_, err = h.services.DB.Pool().Exec(ctx, `
		INSERT INTO devices (id, agent_id, hostname, platform, os_version, status, created_at, last_seen)
		VALUES ($1, $2, $3, $4, $5, 'online', NOW(), NOW())
	`, deviceID, agentID, hostname, req.DeviceInfo.Platform, req.DeviceInfo.OSVersion)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create device"})
		return
	}

	// Create mobile device record
	mobileID := uuid.New()
	_, err = h.services.DB.Pool().Exec(ctx, `
		INSERT INTO mobile_devices (
			id, device_id, device_type, manufacturer, model,
			os_version, serial_number, imei, is_managed, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true, NOW())
	`, mobileID, deviceID, req.DeviceInfo.DeviceType, req.DeviceInfo.Manufacturer,
		req.DeviceInfo.Model, req.DeviceInfo.OSVersion, req.DeviceInfo.SerialNumber,
		req.DeviceInfo.IMEI)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create mobile device"})
		return
	}

	// Increment token usage
	_, _ = h.services.DB.Pool().Exec(ctx, `
		UPDATE enrollment_tokens SET used_count = used_count + 1 WHERE id = $1
	`, tokenID)

	c.JSON(http.StatusCreated, gin.H{
		"deviceId":       deviceID,
		"mobileDeviceId": mobileID,
		"agentId":        agentID,
		"enrolled":       true,
	})
}

// RegisterPushToken registers a push notification token for a device
func (h *MobileHandlers) RegisterPushToken(c *gin.Context) {
	var req struct {
		DeviceID string `json:"deviceId" binding:"required"`
		Token    string `json:"token" binding:"required"`
		Platform string `json:"platform" binding:"required"` // ios, android
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device ID"})
		return
	}

	ctx := c.Request.Context()

	// Deactivate old tokens for this device
	_, _ = h.services.DB.Pool().Exec(ctx, `
		UPDATE push_tokens SET is_active = false
		WHERE device_id = $1 AND platform = $2
	`, deviceID, req.Platform)

	// Insert new token
	tokenID := uuid.New()
	_, err = h.services.DB.Pool().Exec(ctx, `
		INSERT INTO push_tokens (id, device_id, token, platform, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, true, NOW(), NOW())
		ON CONFLICT (token) DO UPDATE SET
			is_active = true,
			updated_at = NOW()
	`, tokenID, deviceID, req.Token, req.Platform)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"registered": true,
		"tokenId":    tokenID,
	})
}
