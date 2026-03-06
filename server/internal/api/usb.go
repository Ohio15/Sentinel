package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// USBDevice represents a USB device from an agent
type USBDevice struct {
	DeviceID       string    `json:"deviceId"`
	InstancePath   string    `json:"instancePath"`
	VendorID       string    `json:"vendorId"`
	ProductID      string    `json:"productId"`
	SerialNumber   string    `json:"serialNumber"`
	Manufacturer   string    `json:"manufacturer"`
	ProductName    string    `json:"productName"`
	DeviceClass    string    `json:"deviceClass"`
	ClassCode      int       `json:"classCode"`
	SubclassCode   int       `json:"subclassCode"`
	ProtocolCode   int       `json:"protocolCode"`
	BusNumber      int       `json:"busNumber"`
	PortNumber     int       `json:"portNumber"`
	DeviceSpeed    string    `json:"deviceSpeed"`
	ParentDevice   string    `json:"parentDevice"`
	DriveLetter    string    `json:"driveLetter,omitempty"`
	MountPoint     string    `json:"mountPoint,omitempty"`
	VolumeLabel    string    `json:"volumeLabel,omitempty"`
	FileSystem     string    `json:"fileSystem,omitempty"`
	TotalSize      int64     `json:"totalSize,omitempty"`
	FreeSpace      int64     `json:"freeSpace,omitempty"`
	IsConnected    bool      `json:"isConnected"`
	ConnectionTime time.Time `json:"connectionTime"`
	IsRemovable    bool      `json:"isRemovable"`
	IsBootable     bool      `json:"isBootable"`
	IsEncrypted    bool      `json:"isEncrypted"`
}

// USBDeviceEvent represents a USB device event from an agent
type USBDeviceEvent struct {
	EventType string     `json:"eventType"` // connected, disconnected, changed
	Device    *USBDevice `json:"device"`
	Timestamp time.Time  `json:"timestamp"`
}

// USBPolicy represents a USB device policy
type USBPolicy struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	OrganizationID uuid.UUID `json:"organizationId,omitempty"`
	PolicyType     string    `json:"policyType"` // allow, block, alert
	Priority       int       `json:"priority"`
	VendorIDs      []string  `json:"vendorIds"`
	ProductIDs     []string  `json:"productIds"`
	SerialNumbers  []string  `json:"serialNumbers"`
	DeviceClasses  []string  `json:"deviceClasses"`
	GenerateAlert  bool      `json:"generateAlert"`
	AlertSeverity  string    `json:"alertSeverity"`
	BlockDevice    bool      `json:"blockDevice"`
	IsActive       bool      `json:"isActive"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// USBApprovedDevice represents an approved USB device
type USBApprovedDevice struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organizationId,omitempty"`
	DeviceID       uuid.UUID  `json:"deviceId,omitempty"`
	VendorID       string     `json:"vendorId,omitempty"`
	ProductID      string     `json:"productId,omitempty"`
	SerialNumber   string     `json:"serialNumber,omitempty"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	ApprovedBy     string     `json:"approvedBy"`
	CreatedAt      time.Time  `json:"createdAt"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
}

// RegisterUSBRoutes registers USB device management routes
func RegisterUSBRoutes(rg *gin.RouterGroup, db *pgxpool.Pool) {
	usb := rg.Group("/usb")
	{
		// Device listing
		usb.GET("/devices", listUSBDevicesHandler(db))
		usb.GET("/devices/:deviceId", getUSBDevicesForDevice(db))
		usb.GET("/events", listUSBEventsHandler(db))
		usb.GET("/events/:deviceId", getUSBEventsForDevice(db))

		// File transfers
		usb.GET("/transfers/:alertId", getFileTransfersForAlert(db))
		usb.GET("/transfers/session/:sessionId", getFileTransfersForSession(db))

		// Policy management
		usb.GET("/policies", listUSBPoliciesHandler(db))
		usb.POST("/policies", createUSBPolicyHandler(db))
		usb.PUT("/policies/:id", updateUSBPolicyHandler(db))
		usb.DELETE("/policies/:id", deleteUSBPolicyHandler(db))

		// Approved devices
		usb.GET("/approved", listApprovedDevicesHandler(db))
		usb.POST("/approved", addApprovedDeviceHandler(db))
		usb.DELETE("/approved/:id", removeApprovedDeviceHandler(db))

		// Actions
		usb.POST("/scan/:deviceId", requestUSBScanHandler(db))
	}
}

// listUSBDevicesHandler lists all connected USB devices across all machines
func listUSBDevicesHandler(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Optional filters
		deviceClass := c.Query("class")
		connectedOnly := c.Query("connected") != "false"

		query := `
			SELECT
				ud.id, ud.device_id, ud.usb_device_id, ud.instance_path,
				ud.vendor_id, ud.product_id, ud.serial_number,
				ud.manufacturer, ud.product_name, ud.device_class,
				ud.class_code, ud.subclass_code, ud.protocol_code,
				ud.bus_number, ud.port_number, ud.device_speed, ud.parent_device,
				ud.drive_letter, ud.mount_point, ud.volume_label, ud.file_system,
				ud.total_size, ud.free_space,
				ud.is_connected, ud.connection_time, ud.disconnection_time,
				ud.is_removable, ud.is_bootable, ud.is_encrypted, ud.is_approved,
				d.hostname
			FROM usb_devices ud
			JOIN devices d ON ud.device_id = d.id
			WHERE 1=1
		`
		args := []interface{}{}
		argNum := 1

		if connectedOnly {
			query += " AND ud.is_connected = true"
		}
		if deviceClass != "" {
			query += " AND ud.device_class = $" + string(rune('0'+argNum))
			args = append(args, deviceClass)
			argNum++
		}

		query += " ORDER BY ud.connection_time DESC LIMIT 500"

		rows, err := db.Query(c.Request.Context(), query, args...)
		if err != nil {
			log.Printf("Failed to query USB devices: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query devices"})
			return
		}
		defer rows.Close()

		devices := []map[string]interface{}{}
		for rows.Next() {
			var (
				id, deviceID                                            uuid.UUID
				usbDeviceID, instancePath, vendorID, productID          string
				serialNumber, manufacturer, productName, deviceClassStr sql.NullString
				classCode, subclassCode, protocolCode                   int
				busNumber, portNumber                                   sql.NullInt32
				deviceSpeed, parentDevice                               sql.NullString
				driveLetter, mountPoint, volumeLabel, fileSystem        sql.NullString
				totalSize, freeSpace                                    sql.NullInt64
				isConnected, isRemovable, isBootable, isEncrypted       bool
				isApproved                                              bool
				connectionTime                                          time.Time
				disconnectionTime                                       sql.NullTime
				hostname                                                string
			)

			if err := rows.Scan(
				&id, &deviceID, &usbDeviceID, &instancePath,
				&vendorID, &productID, &serialNumber,
				&manufacturer, &productName, &deviceClassStr,
				&classCode, &subclassCode, &protocolCode,
				&busNumber, &portNumber, &deviceSpeed, &parentDevice,
				&driveLetter, &mountPoint, &volumeLabel, &fileSystem,
				&totalSize, &freeSpace,
				&isConnected, &connectionTime, &disconnectionTime,
				&isRemovable, &isBootable, &isEncrypted, &isApproved,
				&hostname,
			); err != nil {
				log.Printf("Failed to scan USB device row: %v", err)
				continue
			}

			device := map[string]interface{}{
				"id":             id,
				"deviceId":       deviceID,
				"hostname":       hostname,
				"usbDeviceId":    usbDeviceID,
				"instancePath":   instancePath,
				"vendorId":       vendorID,
				"productId":      productID,
				"serialNumber":   nullString(serialNumber),
				"manufacturer":   nullString(manufacturer),
				"productName":    nullString(productName),
				"deviceClass":    nullString(deviceClassStr),
				"classCode":      classCode,
				"subclassCode":   subclassCode,
				"protocolCode":   protocolCode,
				"busNumber":      nullInt32(busNumber),
				"portNumber":     nullInt32(portNumber),
				"deviceSpeed":    nullString(deviceSpeed),
				"parentDevice":   nullString(parentDevice),
				"driveLetter":    nullString(driveLetter),
				"mountPoint":     nullString(mountPoint),
				"volumeLabel":    nullString(volumeLabel),
				"fileSystem":     nullString(fileSystem),
				"totalSize":      nullInt64(totalSize),
				"freeSpace":      nullInt64(freeSpace),
				"isConnected":    isConnected,
				"connectionTime": connectionTime,
				"isRemovable":    isRemovable,
				"isBootable":     isBootable,
				"isEncrypted":    isEncrypted,
				"isApproved":     isApproved,
			}

			if disconnectionTime.Valid {
				device["disconnectionTime"] = disconnectionTime.Time
			}

			devices = append(devices, device)
		}

		c.JSON(http.StatusOK, gin.H{
			"devices": devices,
			"count":   len(devices),
		})
	}
}

// getUSBDevicesForDevice lists USB devices for a specific machine
func getUSBDevicesForDevice(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		deviceID, err := uuid.Parse(c.Param("deviceId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
			return
		}

		query := `
			SELECT
				id, usb_device_id, instance_path,
				vendor_id, product_id, serial_number,
				manufacturer, product_name, device_class,
				class_code, subclass_code, protocol_code,
				bus_number, port_number, device_speed, parent_device,
				drive_letter, mount_point, volume_label, file_system,
				total_size, free_space,
				is_connected, connection_time, disconnection_time,
				is_removable, is_bootable, is_encrypted, is_approved
			FROM usb_devices
			WHERE device_id = $1
			ORDER BY is_connected DESC, connection_time DESC
		`

		rows, err := db.Query(c.Request.Context(), query, deviceID)
		if err != nil {
			log.Printf("Failed to query USB devices for device %s: %v", deviceID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query devices"})
			return
		}
		defer rows.Close()

		devices := []map[string]interface{}{}
		for rows.Next() {
			var (
				id                                                      uuid.UUID
				usbDeviceID, instancePath, vendorID, productID          string
				serialNumber, manufacturer, productName, deviceClassStr sql.NullString
				classCode, subclassCode, protocolCode                   int
				busNumber, portNumber                                   sql.NullInt32
				deviceSpeed, parentDevice                               sql.NullString
				driveLetter, mountPoint, volumeLabel, fileSystem        sql.NullString
				totalSize, freeSpace                                    sql.NullInt64
				isConnected, isRemovable, isBootable, isEncrypted       bool
				isApproved                                              bool
				connectionTime                                          time.Time
				disconnectionTime                                       sql.NullTime
			)

			if err := rows.Scan(
				&id, &usbDeviceID, &instancePath,
				&vendorID, &productID, &serialNumber,
				&manufacturer, &productName, &deviceClassStr,
				&classCode, &subclassCode, &protocolCode,
				&busNumber, &portNumber, &deviceSpeed, &parentDevice,
				&driveLetter, &mountPoint, &volumeLabel, &fileSystem,
				&totalSize, &freeSpace,
				&isConnected, &connectionTime, &disconnectionTime,
				&isRemovable, &isBootable, &isEncrypted, &isApproved,
			); err != nil {
				log.Printf("Failed to scan USB device row: %v", err)
				continue
			}

			device := map[string]interface{}{
				"id":             id,
				"usbDeviceId":    usbDeviceID,
				"instancePath":   instancePath,
				"vendorId":       vendorID,
				"productId":      productID,
				"serialNumber":   nullString(serialNumber),
				"manufacturer":   nullString(manufacturer),
				"productName":    nullString(productName),
				"deviceClass":    nullString(deviceClassStr),
				"classCode":      classCode,
				"subclassCode":   subclassCode,
				"protocolCode":   protocolCode,
				"busNumber":      nullInt32(busNumber),
				"portNumber":     nullInt32(portNumber),
				"deviceSpeed":    nullString(deviceSpeed),
				"parentDevice":   nullString(parentDevice),
				"driveLetter":    nullString(driveLetter),
				"mountPoint":     nullString(mountPoint),
				"volumeLabel":    nullString(volumeLabel),
				"fileSystem":     nullString(fileSystem),
				"totalSize":      nullInt64(totalSize),
				"freeSpace":      nullInt64(freeSpace),
				"isConnected":    isConnected,
				"connectionTime": connectionTime,
				"isRemovable":    isRemovable,
				"isBootable":     isBootable,
				"isEncrypted":    isEncrypted,
				"isApproved":     isApproved,
			}

			if disconnectionTime.Valid {
				device["disconnectionTime"] = disconnectionTime.Time
			}

			devices = append(devices, device)
		}

		c.JSON(http.StatusOK, gin.H{
			"deviceId": deviceID,
			"devices":  devices,
			"count":    len(devices),
		})
	}
}

// listUSBEventsHandler lists recent USB device events
func listUSBEventsHandler(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := 100

		query := `
			SELECT
				ue.id, ue.device_id, ue.event_type,
				ue.vendor_id, ue.product_id, ue.serial_number,
				ue.manufacturer, ue.product_name, ue.device_class,
				ue.drive_letter, ue.mount_point, ue.volume_label, ue.total_size,
				ue.is_approved, ue.policy_matched, ue.was_blocked,
				ue.alert_generated, ue.created_at,
				d.hostname
			FROM usb_device_events ue
			JOIN devices d ON ue.device_id = d.id
			ORDER BY ue.created_at DESC
			LIMIT $1
		`

		rows, err := db.Query(c.Request.Context(), query, limit)
		if err != nil {
			log.Printf("Failed to query USB events: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query events"})
			return
		}
		defer rows.Close()

		events := []map[string]interface{}{}
		for rows.Next() {
			var (
				id, deviceID                            uuid.UUID
				eventType, vendorID, productID          string
				serialNumber, manufacturer, productName sql.NullString
				deviceClass                             sql.NullString
				driveLetter, mountPoint, volumeLabel    sql.NullString
				totalSize                               sql.NullInt64
				isApproved, wasBlocked, alertGenerated  bool
				policyMatched                           sql.NullString
				createdAt                               time.Time
				hostname                                string
			)

			if err := rows.Scan(
				&id, &deviceID, &eventType,
				&vendorID, &productID, &serialNumber,
				&manufacturer, &productName, &deviceClass,
				&driveLetter, &mountPoint, &volumeLabel, &totalSize,
				&isApproved, &policyMatched, &wasBlocked,
				&alertGenerated, &createdAt,
				&hostname,
			); err != nil {
				log.Printf("Failed to scan USB event row: %v", err)
				continue
			}

			events = append(events, map[string]interface{}{
				"id":             id,
				"deviceId":       deviceID,
				"hostname":       hostname,
				"eventType":      eventType,
				"vendorId":       vendorID,
				"productId":      productID,
				"serialNumber":   nullString(serialNumber),
				"manufacturer":   nullString(manufacturer),
				"productName":    nullString(productName),
				"deviceClass":    nullString(deviceClass),
				"driveLetter":    nullString(driveLetter),
				"mountPoint":     nullString(mountPoint),
				"volumeLabel":    nullString(volumeLabel),
				"totalSize":      nullInt64(totalSize),
				"isApproved":     isApproved,
				"policyMatched":  nullString(policyMatched),
				"wasBlocked":     wasBlocked,
				"alertGenerated": alertGenerated,
				"createdAt":      createdAt,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"events": events,
			"count":  len(events),
		})
	}
}

// getUSBEventsForDevice lists USB events for a specific machine
func getUSBEventsForDevice(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		deviceID, err := uuid.Parse(c.Param("deviceId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
			return
		}

		query := `
			SELECT
				id, event_type,
				vendor_id, product_id, serial_number,
				manufacturer, product_name, device_class,
				drive_letter, mount_point, volume_label, total_size,
				is_approved, policy_matched, was_blocked,
				alert_generated, created_at
			FROM usb_device_events
			WHERE device_id = $1
			ORDER BY created_at DESC
			LIMIT 100
		`

		rows, err := db.Query(c.Request.Context(), query, deviceID)
		if err != nil {
			log.Printf("Failed to query USB events for device %s: %v", deviceID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query events"})
			return
		}
		defer rows.Close()

		events := []map[string]interface{}{}
		for rows.Next() {
			var (
				id                                      uuid.UUID
				eventType, vendorID, productID          string
				serialNumber, manufacturer, productName sql.NullString
				deviceClass                             sql.NullString
				driveLetter, mountPoint, volumeLabel    sql.NullString
				totalSize                               sql.NullInt64
				isApproved, wasBlocked, alertGenerated  bool
				policyMatched                           sql.NullString
				createdAt                               time.Time
			)

			if err := rows.Scan(
				&id, &eventType,
				&vendorID, &productID, &serialNumber,
				&manufacturer, &productName, &deviceClass,
				&driveLetter, &mountPoint, &volumeLabel, &totalSize,
				&isApproved, &policyMatched, &wasBlocked,
				&alertGenerated, &createdAt,
			); err != nil {
				log.Printf("Failed to scan USB event row: %v", err)
				continue
			}

			events = append(events, map[string]interface{}{
				"id":             id,
				"eventType":      eventType,
				"vendorId":       vendorID,
				"productId":      productID,
				"serialNumber":   nullString(serialNumber),
				"manufacturer":   nullString(manufacturer),
				"productName":    nullString(productName),
				"deviceClass":    nullString(deviceClass),
				"driveLetter":    nullString(driveLetter),
				"mountPoint":     nullString(mountPoint),
				"volumeLabel":    nullString(volumeLabel),
				"totalSize":      nullInt64(totalSize),
				"isApproved":     isApproved,
				"policyMatched":  nullString(policyMatched),
				"wasBlocked":     wasBlocked,
				"alertGenerated": alertGenerated,
				"createdAt":      createdAt,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"deviceId": deviceID,
			"events":   events,
			"count":    len(events),
		})
	}
}

// listUSBPoliciesHandler lists USB device policies
func listUSBPoliciesHandler(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := `
			SELECT
				id, name, description, organization_id,
				policy_type, priority,
				vendor_ids, product_ids, serial_numbers, device_classes,
				generate_alert, alert_severity, block_device,
				is_active, created_at, updated_at
			FROM usb_device_policies
			ORDER BY priority ASC, created_at DESC
		`

		rows, err := db.Query(c.Request.Context(), query)
		if err != nil {
			log.Printf("Failed to query USB policies: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query policies"})
			return
		}
		defer rows.Close()

		policies := []USBPolicy{}
		for rows.Next() {
			var (
				policy         USBPolicy
				orgID          uuid.NullUUID
				description    sql.NullString
				vendorIDs      json.RawMessage
				productIDs     json.RawMessage
				serialNumbers  json.RawMessage
				deviceClasses  json.RawMessage
			)

			if err := rows.Scan(
				&policy.ID, &policy.Name, &description, &orgID,
				&policy.PolicyType, &policy.Priority,
				&vendorIDs, &productIDs, &serialNumbers, &deviceClasses,
				&policy.GenerateAlert, &policy.AlertSeverity, &policy.BlockDevice,
				&policy.IsActive, &policy.CreatedAt, &policy.UpdatedAt,
			); err != nil {
				log.Printf("Failed to scan USB policy row: %v", err)
				continue
			}

			if description.Valid {
				policy.Description = description.String
			}
			if orgID.Valid {
				policy.OrganizationID = orgID.UUID
			}

			json.Unmarshal(vendorIDs, &policy.VendorIDs)
			json.Unmarshal(productIDs, &policy.ProductIDs)
			json.Unmarshal(serialNumbers, &policy.SerialNumbers)
			json.Unmarshal(deviceClasses, &policy.DeviceClasses)

			policies = append(policies, policy)
		}

		c.JSON(http.StatusOK, gin.H{
			"policies": policies,
			"count":    len(policies),
		})
	}
}

// createUSBPolicyHandler creates a new USB device policy
func createUSBPolicyHandler(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Name          string   `json:"name" binding:"required"`
			Description   string   `json:"description"`
			PolicyType    string   `json:"policyType" binding:"required"`
			Priority      int      `json:"priority"`
			VendorIDs     []string `json:"vendorIds"`
			ProductIDs    []string `json:"productIds"`
			SerialNumbers []string `json:"serialNumbers"`
			DeviceClasses []string `json:"deviceClasses"`
			GenerateAlert bool     `json:"generateAlert"`
			AlertSeverity string   `json:"alertSeverity"`
			BlockDevice   bool     `json:"blockDevice"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
			return
		}

		// Default values
		if req.Priority == 0 {
			req.Priority = 100
		}
		if req.AlertSeverity == "" {
			req.AlertSeverity = "warning"
		}

		vendorIDs, _ := json.Marshal(req.VendorIDs)
		productIDs, _ := json.Marshal(req.ProductIDs)
		serialNumbers, _ := json.Marshal(req.SerialNumbers)
		deviceClasses, _ := json.Marshal(req.DeviceClasses)

		var policyID uuid.UUID
		err := db.QueryRow(c.Request.Context(), `
			INSERT INTO usb_device_policies (
				name, description, policy_type, priority,
				vendor_ids, product_ids, serial_numbers, device_classes,
				generate_alert, alert_severity, block_device
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id
		`,
			req.Name, req.Description, req.PolicyType, req.Priority,
			vendorIDs, productIDs, serialNumbers, deviceClasses,
			req.GenerateAlert, req.AlertSeverity, req.BlockDevice,
		).Scan(&policyID)

		if err != nil {
			log.Printf("Failed to create USB policy: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create policy"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"id":      policyID,
			"message": "Policy created successfully",
		})
	}
}

// updateUSBPolicyHandler updates a USB device policy
func updateUSBPolicyHandler(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		policyID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid policy ID"})
			return
		}

		var req struct {
			Name          string   `json:"name"`
			Description   string   `json:"description"`
			PolicyType    string   `json:"policyType"`
			Priority      int      `json:"priority"`
			VendorIDs     []string `json:"vendorIds"`
			ProductIDs    []string `json:"productIds"`
			SerialNumbers []string `json:"serialNumbers"`
			DeviceClasses []string `json:"deviceClasses"`
			GenerateAlert *bool    `json:"generateAlert"`
			AlertSeverity string   `json:"alertSeverity"`
			BlockDevice   *bool    `json:"blockDevice"`
			IsActive      *bool    `json:"isActive"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
			return
		}

		vendorIDs, _ := json.Marshal(req.VendorIDs)
		productIDs, _ := json.Marshal(req.ProductIDs)
		serialNumbers, _ := json.Marshal(req.SerialNumbers)
		deviceClasses, _ := json.Marshal(req.DeviceClasses)

		_, err = db.Exec(c.Request.Context(), `
			UPDATE usb_device_policies SET
				name = COALESCE(NULLIF($2, ''), name),
				description = COALESCE(NULLIF($3, ''), description),
				policy_type = COALESCE(NULLIF($4, ''), policy_type),
				priority = CASE WHEN $5 > 0 THEN $5 ELSE priority END,
				vendor_ids = $6,
				product_ids = $7,
				serial_numbers = $8,
				device_classes = $9,
				generate_alert = COALESCE($10, generate_alert),
				alert_severity = COALESCE(NULLIF($11, ''), alert_severity),
				block_device = COALESCE($12, block_device),
				is_active = COALESCE($13, is_active),
				updated_at = NOW()
			WHERE id = $1
		`,
			policyID,
			req.Name, req.Description, req.PolicyType, req.Priority,
			vendorIDs, productIDs, serialNumbers, deviceClasses,
			req.GenerateAlert, req.AlertSeverity, req.BlockDevice, req.IsActive,
		)

		if err != nil {
			log.Printf("Failed to update USB policy: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update policy"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Policy updated successfully",
		})
	}
}

// deleteUSBPolicyHandler deletes a USB device policy
func deleteUSBPolicyHandler(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		policyID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid policy ID"})
			return
		}

		_, err = db.Exec(c.Request.Context(), `DELETE FROM usb_device_policies WHERE id = $1`, policyID)
		if err != nil {
			log.Printf("Failed to delete USB policy: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete policy"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Policy deleted successfully",
		})
	}
}

// listApprovedDevicesHandler lists approved USB devices
func listApprovedDevicesHandler(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := `
			SELECT
				id, organization_id, device_id,
				vendor_id, product_id, serial_number,
				name, description, approved_by,
				created_at, expires_at
			FROM usb_approved_devices
			WHERE expires_at IS NULL OR expires_at > NOW()
			ORDER BY created_at DESC
		`

		rows, err := db.Query(c.Request.Context(), query)
		if err != nil {
			log.Printf("Failed to query approved devices: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query approved devices"})
			return
		}
		defer rows.Close()

		devices := []USBApprovedDevice{}
		for rows.Next() {
			var (
				device                                                  USBApprovedDevice
				orgID, devID                                            uuid.NullUUID
				vendorID, productID, serialNumber, description, approvedBy sql.NullString
				expiresAt                                               sql.NullTime
			)

			if err := rows.Scan(
				&device.ID, &orgID, &devID,
				&vendorID, &productID, &serialNumber,
				&device.Name, &description, &approvedBy,
				&device.CreatedAt, &expiresAt,
			); err != nil {
				log.Printf("Failed to scan approved device row: %v", err)
				continue
			}

			if orgID.Valid {
				device.OrganizationID = orgID.UUID
			}
			if devID.Valid {
				device.DeviceID = devID.UUID
			}
			device.VendorID = nullString(vendorID)
			device.ProductID = nullString(productID)
			device.SerialNumber = nullString(serialNumber)
			device.Description = nullString(description)
			device.ApprovedBy = nullString(approvedBy)
			if expiresAt.Valid {
				device.ExpiresAt = &expiresAt.Time
			}

			devices = append(devices, device)
		}

		c.JSON(http.StatusOK, gin.H{
			"devices": devices,
			"count":   len(devices),
		})
	}
}

// addApprovedDeviceHandler adds a device to the approved list
func addApprovedDeviceHandler(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			VendorID     string     `json:"vendorId"`
			ProductID    string     `json:"productId"`
			SerialNumber string     `json:"serialNumber"`
			Name         string     `json:"name" binding:"required"`
			Description  string     `json:"description"`
			ApprovedBy   string     `json:"approvedBy"`
			ExpiresAt    *time.Time `json:"expiresAt"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
			return
		}

		// Must have at least one identifier
		if req.VendorID == "" && req.ProductID == "" && req.SerialNumber == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Must provide vendorId, productId, or serialNumber"})
			return
		}

		var deviceID uuid.UUID
		err := db.QueryRow(c.Request.Context(), `
			INSERT INTO usb_approved_devices (
				vendor_id, product_id, serial_number,
				name, description, approved_by, expires_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id
		`,
			nullIfEmptyUSB(req.VendorID), nullIfEmptyUSB(req.ProductID), nullIfEmptyUSB(req.SerialNumber),
			req.Name, nullIfEmptyUSB(req.Description), nullIfEmptyUSB(req.ApprovedBy), req.ExpiresAt,
		).Scan(&deviceID)

		if err != nil {
			log.Printf("Failed to add approved device: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add approved device"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"id":      deviceID,
			"message": "Device added to approved list",
		})
	}
}

// removeApprovedDeviceHandler removes a device from the approved list
func removeApprovedDeviceHandler(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		deviceID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
			return
		}

		_, err = db.Exec(c.Request.Context(), `DELETE FROM usb_approved_devices WHERE id = $1`, deviceID)
		if err != nil {
			log.Printf("Failed to remove approved device: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove approved device"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Device removed from approved list",
		})
	}
}

// requestUSBScanHandler requests a USB device scan from an agent
func requestUSBScanHandler(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// This would need access to the WebSocket hub to send the request to the agent
		// For now, just return a placeholder response
		c.JSON(http.StatusOK, gin.H{
			"message": "USB scan requested - feature requires WebSocket integration",
		})
	}
}

// getFileTransfersForAlert retrieves file transfers for a specific alert
func getFileTransfersForAlert(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		alertID, err := uuid.Parse(c.Param("alertId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alert ID"})
			return
		}

		// First, get the session ID from the alert metadata
		var metadata json.RawMessage
		err = db.QueryRow(c.Request.Context(), `
			SELECT COALESCE(metadata, '{}'::jsonb) FROM alerts WHERE id = $1
		`, alertID).Scan(&metadata)

		if err != nil {
			log.Printf("Failed to get alert metadata: %v", err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Alert not found"})
			return
		}

		// Parse metadata to get session ID
		var meta struct {
			SessionID   string `json:"sessionId"`
			FileCount   int    `json:"fileCount"`
			USBDeviceID string `json:"usbDeviceId"`
		}
		if err := json.Unmarshal(metadata, &meta); err != nil || meta.SessionID == "" {
			c.JSON(http.StatusOK, gin.H{
				"alertId":   alertID,
				"transfers": []map[string]interface{}{},
				"count":     0,
				"message":   "No file transfers recorded for this alert",
			})
			return
		}

		sessionUUID, err := uuid.Parse(meta.SessionID)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"alertId":   alertID,
				"transfers": []map[string]interface{}{},
				"count":     0,
				"message":   "Invalid session ID in alert metadata",
			})
			return
		}

		// Query file transfers for this session
		transfers := queryFileTransfers(c, db, sessionUUID)

		c.JSON(http.StatusOK, gin.H{
			"alertId":     alertID,
			"sessionId":   meta.SessionID,
			"usbDeviceId": meta.USBDeviceID,
			"transfers":   transfers,
			"count":       len(transfers),
		})
	}
}

// getFileTransfersForSession retrieves file transfers for a specific session
func getFileTransfersForSession(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID, err := uuid.Parse(c.Param("sessionId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid session ID"})
			return
		}

		transfers := queryFileTransfers(c, db, sessionID)

		c.JSON(http.StatusOK, gin.H{
			"sessionId": sessionID,
			"transfers": transfers,
			"count":     len(transfers),
		})
	}
}

// queryFileTransfers queries file transfers for a given session ID
func queryFileTransfers(c *gin.Context, db *pgxpool.Pool, sessionID uuid.UUID) []map[string]interface{} {
	query := `
		SELECT
			id, device_id, usb_device_id, session_id,
			file_name, file_path, file_size, transfer_time, operation,
			created_at
		FROM usb_file_transfers
		WHERE session_id = $1
		ORDER BY transfer_time ASC
		LIMIT 1000
	`

	rows, err := db.Query(c.Request.Context(), query, sessionID)
	if err != nil {
		log.Printf("Failed to query file transfers: %v", err)
		return []map[string]interface{}{}
	}
	defer rows.Close()

	transfers := []map[string]interface{}{}
	for rows.Next() {
		var (
			id, deviceID, sessID uuid.UUID
			usbDeviceID          string
			fileName             string
			filePath             sql.NullString
			fileSize             int64
			transferTime         time.Time
			operation            string
			createdAt            time.Time
		)

		if err := rows.Scan(
			&id, &deviceID, &usbDeviceID, &sessID,
			&fileName, &filePath, &fileSize, &transferTime, &operation,
			&createdAt,
		); err != nil {
			log.Printf("Failed to scan file transfer row: %v", err)
			continue
		}

		transfers = append(transfers, map[string]interface{}{
			"id":           id,
			"deviceId":     deviceID,
			"usbDeviceId":  usbDeviceID,
			"sessionId":    sessID,
			"fileName":     fileName,
			"filePath":     nullString(filePath),
			"fileSize":     fileSize,
			"transferTime": transferTime,
			"operation":    operation,
			"createdAt":    createdAt,
		})
	}

	return transfers
}

// Helper functions
func nullString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func nullInt32(ni sql.NullInt32) int32 {
	if ni.Valid {
		return ni.Int32
	}
	return 0
}

func nullInt64(ni sql.NullInt64) int64 {
	if ni.Valid {
		return ni.Int64
	}
	return 0
}

func nullIfEmptyUSB(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
