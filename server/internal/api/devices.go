package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sentinel/server/internal/audit"
	"github.com/sentinel/server/internal/constants"
	"github.com/sentinel/server/internal/models"
	"github.com/sentinel/server/internal/websocket"
)

func (r *Router) listDevices(c *gin.Context) {
	ctx := context.Background()

	// Parse pagination parameters
	page := 1
	pageSize := 100 // Default page size
	const maxPageSize = 500

	if p := c.Query("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	if ps := c.Query("pageSize"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 {
			if parsed > maxPageSize {
				pageSize = maxPageSize
			} else {
				pageSize = parsed
			}
		}
	}

	// Calculate offset for pagination
	offset := (page - 1) * pageSize

	// Hidden devices are excluded by default. ?include_hidden=true opts back in.
	// The predicate must be identical in the COUNT and the SELECT or the
	// pagination metadata would describe a different result set than the page.
	hiddenFilter := " AND hidden_at IS NULL"
	if c.Query("include_hidden") == "true" {
		hiddenFilter = ""
	}

	// Get total count of devices for pagination metadata
	var total int
	err := r.db.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE organization_id = $1`+hiddenFilter, constants.CurrentOrganizationID).Scan(&total)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count devices"})
		return
	}

	// Calculate total pages
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	// Query devices with pagination using LIMIT and OFFSET
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, agent_id, COALESCE(hostname, ''), COALESCE(display_name, ''), COALESCE(device_type, 'desktop'),
			   COALESCE(os_type, ''), COALESCE(os_version, ''), COALESCE(os_build, ''),
			   COALESCE(platform, ''), COALESCE(platform_family, ''), COALESCE(architecture, ''), COALESCE(cpu_model, ''), COALESCE(cpu_cores, 0), COALESCE(cpu_threads, 0),
			   COALESCE(cpu_speed, 0), COALESCE(total_memory, 0), COALESCE(EXTRACT(EPOCH FROM boot_time)::bigint, 0), COALESCE(gpu::jsonb, '[]'::jsonb), COALESCE(storage, '[]'::jsonb), COALESCE(serial_number, ''),
			   COALESCE(manufacturer, ''), COALESCE(model, ''), COALESCE(domain, ''), COALESCE(agent_version, ''), last_seen, COALESCE(status, 'offline'),
			   COALESCE(host(ip_address), '' ) as ip_address, COALESCE(host(public_ip), '' ) as public_ip, COALESCE(mac_address, ''), COALESCE(tags, ARRAY[]::text[]), COALESCE(metadata, '{}'::jsonb), client_id, hidden_at, created_at, updated_at
		FROM devices
		WHERE organization_id = $1`+hiddenFilter+`
		ORDER BY hostname
		LIMIT $2 OFFSET $3
	`, constants.CurrentOrganizationID, pageSize, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch devices"})
		return
	}
	defer rows.Close()

	devices := make([]models.Device, 0)
	for rows.Next() {
		var d models.Device
		var tags []string
		var metadata map[string]string
		var gpuJSON, storageJSON []byte

		err := rows.Scan(&d.ID, &d.AgentID, &d.Hostname, &d.DisplayName, &d.DeviceType,
			&d.OSType, &d.OSVersion, &d.OSBuild, &d.Platform, &d.PlatformFamily, &d.Architecture,
			&d.CPUModel, &d.CPUCores, &d.CPUThreads, &d.CPUSpeed, &d.TotalMemory,
			&d.BootTime, &gpuJSON, &storageJSON, &d.SerialNumber, &d.Manufacturer,
			&d.Model, &d.Domain, &d.AgentVersion, &d.LastSeen, &d.Status,
			&d.IPAddress, &d.PublicIP, &d.MACAddress, &tags, &metadata,
			&d.ClientID, &d.HiddenAt, &d.CreatedAt, &d.UpdatedAt)
		if err != nil {
			log.Printf("Error scanning device row: %v", err)
			continue
		}
		d.Tags = tags
		d.Metadata = metadata
		if err := json.Unmarshal(gpuJSON, &d.GPU); err != nil && len(gpuJSON) > 0 {
			log.Printf("Error unmarshaling GPU data for device %s: %v", d.ID, err)
		}
		if err := json.Unmarshal(storageJSON, &d.Storage); err != nil && len(storageJSON) > 0 {
			log.Printf("Error unmarshaling storage data for device %s: %v", d.ID, err)
		}

		// Check if agent is currently connected
		if r.hub.IsAgentOnline(d.AgentID) {
			d.Status = "online"
		}

		devices = append(devices, d)
	}

	// Return paginated response with metadata
	c.JSON(http.StatusOK, gin.H{
		"data":       devices,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	})
}


func (r *Router) getDevice(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()

	var d models.Device
	var tags []string
	var metadata map[string]string
	var gpuJSON, storageJSON, powerMgmtJSON []byte

	err = r.db.Pool().QueryRow(ctx, `
		SELECT id, agent_id, COALESCE(hostname, ''), COALESCE(display_name, ''), COALESCE(device_type, 'desktop'),
			   COALESCE(os_type, ''), COALESCE(os_version, ''), COALESCE(os_build, ''),
			   COALESCE(platform, ''), COALESCE(platform_family, ''), COALESCE(architecture, ''), COALESCE(cpu_model, ''), COALESCE(cpu_cores, 0), COALESCE(cpu_threads, 0),
			   COALESCE(cpu_speed, 0), COALESCE(total_memory, 0), COALESCE(EXTRACT(EPOCH FROM boot_time)::bigint, 0), COALESCE(gpu::jsonb, '[]'::jsonb), COALESCE(storage, '[]'::jsonb), COALESCE(serial_number, ''),
			   COALESCE(manufacturer, ''), COALESCE(model, ''), COALESCE(domain, ''), COALESCE(agent_version, ''), last_seen, COALESCE(status, 'offline'),
			   COALESCE(host(ip_address), '' ) as ip_address, COALESCE(host(public_ip), '' ) as public_ip, COALESCE(mac_address, ''), COALESCE(tags, ARRAY[]::text[]), COALESCE(metadata, '{}'::jsonb), client_id,
			   COALESCE(power_management, '{}'::jsonb), hidden_at, created_at, updated_at
		FROM devices WHERE id = $1 AND organization_id = $2
	`, id, constants.CurrentOrganizationID).Scan(&d.ID, &d.AgentID, &d.Hostname, &d.DisplayName, &d.DeviceType,
		&d.OSType, &d.OSVersion, &d.OSBuild, &d.Platform, &d.PlatformFamily, &d.Architecture,
		&d.CPUModel, &d.CPUCores, &d.CPUThreads, &d.CPUSpeed, &d.TotalMemory,
		&d.BootTime, &gpuJSON, &storageJSON, &d.SerialNumber, &d.Manufacturer,
		&d.Model, &d.Domain, &d.AgentVersion, &d.LastSeen, &d.Status,
		&d.IPAddress, &d.PublicIP, &d.MACAddress, &tags, &metadata,
		&d.ClientID, &powerMgmtJSON, &d.HiddenAt, &d.CreatedAt, &d.UpdatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	d.Tags = tags
	d.Metadata = metadata
	if err := json.Unmarshal(gpuJSON, &d.GPU); err != nil && len(gpuJSON) > 0 {
		log.Printf("Error unmarshaling GPU data for device %s: %v", d.ID, err)
	}
	if err := json.Unmarshal(storageJSON, &d.Storage); err != nil && len(storageJSON) > 0 {
		log.Printf("Error unmarshaling storage data for device %s: %v", d.ID, err)
	}
	if len(powerMgmtJSON) > 2 { // More than just "{}"
		var pm models.PowerManagement
		if err := json.Unmarshal(powerMgmtJSON, &pm); err == nil {
			d.PowerManagement = &pm
		}
	}

	if r.hub.IsAgentOnline(d.AgentID) {
		d.Status = "online"
	}

	c.JSON(http.StatusOK, d)
}

// UpdateDeviceRequest defines the fields that can be updated
type UpdateDeviceRequest struct {
	DisplayName   *string    `json:"displayName"`
	DeviceType    *string    `json:"deviceType"` // desktop, laptop, server, tablet, virtual
	Tags          []string   `json:"tags"`
	ClientID      *uuid.UUID `json:"clientId"`
	UpdateGroupID *uuid.UUID `json:"updateGroupId"` // pointer-to-pointer-zero-value semantics: omitted = no change; explicit null in JSON = unassign
}

// updateDevice updates device properties like display name, tags, and client assignment
func (r *Router) updateDevice(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	var req UpdateDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	ctx := context.Background()

	// Build dynamic update query
	updates := []string{}
	args := []interface{}{}
	argNum := 1

	if req.DisplayName != nil {
		updates = append(updates, "display_name = $"+strconv.Itoa(argNum))
		args = append(args, *req.DisplayName)
		argNum++
	}

	if req.DeviceType != nil {
		// Validate device type
		validTypes := map[string]bool{"desktop": true, "laptop": true, "server": true, "tablet": true, "virtual": true}
		if !validTypes[*req.DeviceType] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device type. Must be: desktop, laptop, server, tablet, or virtual"})
			return
		}
		updates = append(updates, "device_type = $"+strconv.Itoa(argNum))
		args = append(args, *req.DeviceType)
		argNum++
	}

	if req.Tags != nil {
		updates = append(updates, "tags = $"+strconv.Itoa(argNum))
		args = append(args, req.Tags)
		argNum++
	}

	// Handle ClientID - allow setting to null to unassign
	if req.ClientID != nil {
		updates = append(updates, "client_id = $"+strconv.Itoa(argNum))
		args = append(args, *req.ClientID)
		argNum++
	}

	if req.UpdateGroupID != nil {
		var exists bool
		if err := r.db.Pool().QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM update_groups WHERE id = $1)", *req.UpdateGroupID).Scan(&exists); err != nil || !exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Update group not found"})
			return
		}
		updates = append(updates, "update_group_id = $"+strconv.Itoa(argNum))
		args = append(args, *req.UpdateGroupID)
		argNum++
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No fields to update"})
		return
	}

	// Add updated_at
	updates = append(updates, "updated_at = NOW()")

	// Add WHERE clause arguments
	args = append(args, id, constants.CurrentOrganizationID)

	query := "UPDATE devices SET " + strings.Join(updates, ", ") + " WHERE id = $" + strconv.Itoa(argNum) + " AND organization_id = $" + strconv.Itoa(argNum+1)

	result, err := r.db.Pool().Exec(ctx, query, args...)
	if err != nil {
		log.Printf("Error updating device %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update device"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Device updated successfully"})
}

// deleteDevice removes a device record - allowed for devices in 'uninstalling' or 'offline' status
// This ensures active devices cannot be accidentally deleted.
//
// Audit: writes a 'device_delete' entry (severity=warning) to audit_log capturing
// hostname, agent_id, last_seen, and client_cert_serial BEFORE the DELETE so the
// trail survives the row removal. The audit write is best-effort — if it fails the
// delete still proceeds, but the failure is logged.
func (r *Router) deleteDevice(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()

	// Capture identifying metadata for the audit trail before the row vanishes.
	var (
		status            string
		hostname          string
		agentID           string
		lastSeen          *time.Time
		clientCertSerial  *string
	)
	err = r.db.Pool().QueryRow(ctx, `
		SELECT status, hostname, agent_id, last_seen, client_cert_serial
		FROM devices
		WHERE id = $1 AND organization_id = $2
	`, id, constants.CurrentOrganizationID).Scan(&status, &hostname, &agentID, &lastSeen, &clientCertSerial)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	if status != "uninstalling" && status != "offline" {
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "Cannot delete active device",
			"message": "Only offline or uninstalling devices can be deleted. Use 'Uninstall Agent' for online devices.",
		})
		return
	}

	// Write audit entry BEFORE the DELETE so the forensic trail survives even if
	// the DELETE later races with another request. The logger writes asynchronously
	// in a goroutine with a 5s timeout (see audit.Logger.LogFromContextWithSeverity).
	if r.audit != nil {
		details := map[string]interface{}{
			"hostname":           hostname,
			"agent_id":           agentID,
			"prior_status":       status,
			"client_cert_serial": clientCertSerial,
		}
		if lastSeen != nil {
			details["last_seen"] = lastSeen.UTC().Format(time.RFC3339)
		}
		r.audit.LogFromContextWithSeverity(c, audit.ActionDeviceDelete, audit.ResourceTypeDevice, &id, audit.SeverityWarning, details)
	}

	// Device is uninstalling - safe to delete
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM devices WHERE id = $1 AND organization_id = $2", id, constants.CurrentOrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete device"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Device deleted"})
}

// disableDevice disables a device, preventing the agent from connecting
func (r *Router) disableDevice(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()
	userID := c.MustGet("userId").(uuid.UUID)

	// Get agent ID to disconnect if online
	var agentID string
	err = r.db.Pool().QueryRow(ctx, "SELECT agent_id FROM devices WHERE id = $1 AND organization_id = $2", id, constants.CurrentOrganizationID).Scan(&agentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	// Update device status to disabled
	result, err := r.db.Pool().Exec(ctx, `
		UPDATE devices SET
			is_disabled = TRUE,
			disabled_at = NOW(),
			disabled_by = $2,
			status = 'disabled',
			updated_at = NOW()
		WHERE id = $1
	`, id, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable device"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	// Disconnect the agent if online (it will be rejected on reconnect)
	if r.hub.IsAgentOnline(agentID) {
		// Send a disconnect message - agent will try to reconnect but will be rejected
		msg := websocket.Message{
			Type:    "disconnect",
			Payload: json.RawMessage(`{"reason":"Device has been disabled by administrator"}`),
		}
		msgBytes, _ := json.Marshal(msg)
		r.hub.SendToAgent(agentID, msgBytes)
	}

	// Broadcast status change to dashboards
	statusMsg, _ := json.Marshal(map[string]interface{}{
		"type":     "device_status",
		"deviceId": id.String(),
		"status":   "disabled",
	})
	r.hub.BroadcastToDashboards(statusMsg)

	c.JSON(http.StatusOK, gin.H{
		"message": "Device disabled successfully",
		"status":  "disabled",
	})
}

// enableDevice re-enables a previously disabled device
func (r *Router) enableDevice(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()

	// Re-enable the device
	result, err := r.db.Pool().Exec(ctx, `
		UPDATE devices SET
			is_disabled = FALSE,
			disabled_at = NULL,
			disabled_by = NULL,
			status = 'offline',
			updated_at = NOW()
		WHERE id = $1
	`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enable device"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	// Broadcast status change to dashboards
	statusMsg, _ := json.Marshal(map[string]interface{}{
		"type":     "device_status",
		"deviceId": id.String(),
		"status":   "offline",
	})
	r.hub.BroadcastToDashboards(statusMsg)

	c.JSON(http.StatusOK, gin.H{
		"message": "Device enabled successfully. Agent will reconnect automatically.",
		"status":  "offline",
	})
}

// hideDevice hides a device from the default device list.
//
// Hiding is DISPLAY-ONLY and intentionally different from disabling:
//   - the agent is NOT disconnected and is NOT rejected on reconnect
//   - no status restriction — online devices can be hidden just like offline ones
//   - the row keeps collecting metrics, alerts and inventory
//
// The hide is automatically reverted the next time the agent establishes an
// authenticated connection or re-enrolls, so a machine that comes back to life
// cannot stay invisible. That revert lives in autoUnhideOnReconnect
// (device_unhide.go): a conditional `hidden_at IS NOT NULL` UPDATE whose
// rows-affected count identifies a genuine restore, which is then surfaced
// out-of-band as an informational alert plus a device.auto_unhide audit entry
// so the change is never silent. Disabled devices are excluded — the WS and
// mTLS paths reject them before unhiding, and the enroll path skips it.
//
// Ongoing traffic on an existing session (heartbeats, gRPC metrics, inventory)
// deliberately does not clear it.
func (r *Router) hideDevice(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()

	// hidden_by is `UUID REFERENCES users(id)`. The static-API-key auth path sets
	// userId to uuid.Nil, which is not a real user row — bind NULL for it rather
	// than triggering a foreign-key violation.
	var hiddenBy *uuid.UUID
	if userID, ok := c.MustGet("userId").(uuid.UUID); ok && userID != uuid.Nil {
		hiddenBy = &userID
	}

	var hostname, agentID string
	err = r.db.Pool().QueryRow(ctx, `
		UPDATE devices SET
			hidden_at = NOW(),
			hidden_by = $2,
			updated_at = NOW()
		WHERE id = $1 AND organization_id = $3
		RETURNING COALESCE(hostname, ''), agent_id
	`, id, hiddenBy, constants.CurrentOrganizationID).Scan(&hostname, &agentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
			return
		}
		log.Printf("Error hiding device %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hide device"})
		return
	}

	if r.audit != nil {
		r.audit.LogFromContextWithSeverity(c, audit.ActionDeviceHide, audit.ResourceTypeDevice, &id, audit.SeverityInfo, map[string]interface{}{
			"hostname": hostname,
			"agent_id": agentID,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Device hidden successfully",
		"hidden":  true,
	})
}

// unhideDevice restores a hidden device to the default device list.
func (r *Router) unhideDevice(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()

	var hostname, agentID string
	err = r.db.Pool().QueryRow(ctx, `
		UPDATE devices SET
			hidden_at = NULL,
			hidden_by = NULL,
			updated_at = NOW()
		WHERE id = $1 AND organization_id = $2
		RETURNING COALESCE(hostname, ''), agent_id
	`, id, constants.CurrentOrganizationID).Scan(&hostname, &agentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
			return
		}
		log.Printf("Error unhiding device %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to unhide device"})
		return
	}

	if r.audit != nil {
		r.audit.LogFromContextWithSeverity(c, audit.ActionDeviceUnhide, audit.ResourceTypeDevice, &id, audit.SeverityInfo, map[string]interface{}{
			"hostname": hostname,
			"agent_id": agentID,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Device unhidden successfully",
		"hidden":  false,
	})
}

func (r *Router) getDeviceMetrics(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	hours := 24
	if h := c.Query("hours"); h != "" {
		if parsed, err := strconv.Atoi(h); err == nil && parsed > 0 && parsed <= 168 {
			hours = parsed
		}
	}

	ctx := context.Background()

	rows, err := r.db.Pool().Query(ctx, `
		SELECT timestamp, cpu_percent, memory_percent, memory_used_bytes, memory_total_bytes,
			   disk_percent, disk_used_bytes, disk_total_bytes, network_rx_bytes,
			   network_tx_bytes, process_count
		FROM device_metrics
		WHERE device_id = $1 AND timestamp > NOW() - INTERVAL '1 hour' * $2
		ORDER BY timestamp DESC
	`, id, hours)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch metrics"})
		return
	}
	defer rows.Close()

	metrics := make([]models.DeviceMetrics, 0)
	for rows.Next() {
		var m models.DeviceMetrics
		m.DeviceID = id
		err := rows.Scan(&m.Timestamp, &m.CPUPercent, &m.MemoryPercent, &m.MemoryUsedBytes,
			&m.MemoryTotalBytes, &m.DiskPercent, &m.DiskUsedBytes, &m.DiskTotalBytes,
			&m.NetworkRxBytes, &m.NetworkTxBytes, &m.ProcessCount)
		if err != nil {
			log.Printf("Error scanning metrics row for device %s: %v", id, err)
			continue
		}
		metrics = append(metrics, m)
	}

	c.JSON(http.StatusOK, metrics)
}

type ExecuteCommandRequest struct {
	Command     string `json:"command" binding:"required"`
	CommandType string `json:"commandType"` // shell, powershell, bash
}

func (r *Router) executeCommand(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	var req ExecuteCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	if req.CommandType == "" || req.CommandType == "shell" {
		// Agent validator only accepts: "", "powershell", "cmd", "bash", "sh"
		// Auto-detect based on device OS type
		var deviceOS string
		_ = r.db.Pool().QueryRow(c.Request.Context(), "SELECT COALESCE(os_type, '') FROM devices WHERE id = $1", id).Scan(&deviceOS)
		if deviceOS == "windows" {
			req.CommandType = "powershell"
		} else {
			req.CommandType = "bash"
		}
	}

	ctx := context.Background()
	userID := c.MustGet("userId").(uuid.UUID)

	// Get device agent ID
	var agentID string
	err = r.db.Pool().QueryRow(ctx, "SELECT agent_id FROM devices WHERE id = $1 AND organization_id = $2", id, constants.CurrentOrganizationID).Scan(&agentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	// Check if agent is online
	if !r.hub.IsAgentOnline(agentID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Device is offline"})
		return
	}

	// Create command record
	commandID := uuid.New()
	requestID := uuid.New().String()

	// Handle API key auth (uuid.Nil) - set created_by to NULL
	var createdBy interface{}
	if userID == uuid.Nil {
		createdBy = nil
	} else {
		createdBy = userID
	}

	_, err = r.db.Pool().Exec(ctx, `
		INSERT INTO commands (id, device_id, command_type, command, status, created_by)
		VALUES ($1, $2, $3, $4, 'pending', $5)
	`, commandID, id, req.CommandType, req.Command, createdBy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create command"})
		return
	}

	// Send command to agent.
	// Include command info in BOTH "payload" and "data" fields for compatibility:
	// - Agents >= 1.76.0 read from Payload (json.RawMessage)
	// - Agents < 1.76.0 read from Data (interface{})
	cmdData := map[string]interface{}{
		"commandId":   commandID.String(),
		"command":     req.Command,
		"commandType": req.CommandType,
	}
	msgBytes, _ := json.Marshal(map[string]interface{}{
		"type":      websocket.MsgTypeCommand,
		"requestId": requestID,
		"payload":   cmdData,
		"data":      cmdData,
	})
	if err := r.hub.SendToAgent(agentID, msgBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send command to agent"})
		return
	}

	// Update command status to running
	if _, err := r.db.Pool().Exec(ctx, `
		UPDATE commands SET status = 'running', started_at = NOW() WHERE id = $1
	`, commandID); err != nil {
		log.Printf("Error updating command %s status to running: %v", commandID, err)
	}

	c.JSON(http.StatusOK, gin.H{
		"commandId": commandID,
		"requestId": requestID,
		"status":    "running",
	})
}

func (r *Router) enrollAgent(c *gin.Context) {
	var enrollment models.AgentEnrollment
	if err := c.ShouldBindJSON(&enrollment); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid enrollment data"})
		return
	}

	ctx := context.Background()

	// Convert GPU and Storage to JSON
	gpuJSON, _ := json.Marshal(enrollment.GPU)
	storageJSON, _ := json.Marshal(enrollment.Storage)

	// Convert empty strings to nil for inet type columns (PostgreSQL rejects empty strings for inet)
	var ipAddr interface{}
	if enrollment.IPAddress != "" {
		ipAddr = enrollment.IPAddress
	}

	// Check if agent already exists
	var existingID uuid.UUID
	var existingDisabled bool
	err := r.db.Pool().QueryRow(ctx,
		"SELECT id, COALESCE(is_disabled, false) FROM devices WHERE agent_id = $1 AND organization_id = $2",
		enrollment.AgentID, constants.CurrentOrganizationID,
	).Scan(&existingID, &existingDisabled)

	if err == nil {
		// Update existing device
		_, err = r.db.Pool().Exec(ctx, `
			UPDATE devices SET
				hostname = $2, os_type = $3, os_version = $4, os_build = $5,
				platform = $6, platform_family = $7, architecture = $8,
				cpu_model = $9, cpu_cores = $10, cpu_threads = $11, cpu_speed = $12,
				total_memory = $13, boot_time = to_timestamp($14), gpu = $15, storage = $16,
				serial_number = $17, manufacturer = $18, model = $19, domain = $20,
				agent_version = $21, ip_address = $22, mac_address = $23,
				last_seen = NOW(), status = 'online', updated_at = NOW()
			WHERE agent_id = $1
		`, enrollment.AgentID, enrollment.Hostname, enrollment.OSType, enrollment.OSVersion,
			enrollment.OSBuild, enrollment.Platform, enrollment.PlatformFamily, enrollment.Architecture,
			enrollment.CPUModel, enrollment.CPUCores, enrollment.CPUThreads, enrollment.CPUSpeed,
			enrollment.TotalMemory, enrollment.BootTime, string(gpuJSON), storageJSON,
			enrollment.SerialNumber, enrollment.Manufacturer, enrollment.Model, enrollment.Domain,
			enrollment.AgentVersion, ipAddr, enrollment.MACAddress)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update device"})
			return
		}

		// A hidden device that re-enrolls is restored to the device list, with
		// the restore surfaced as an alert + audit entry. No-op for devices that
		// were not hidden. Only the existing-device branch reaches this — newly
		// auto-enrolled devices are never hidden to begin with.
		//
		// Disabled devices are excluded: the WebSocket and mTLS paths reject a
		// disabled device outright before they can unhide it, so unhiding here
		// would let a disabled agent undo an administrator's hide simply by
		// re-enrolling. Enrollment itself is deliberately left unchanged.
		if !existingDisabled {
			autoUnhideOnReconnect(ctx, r.db.Pool(), r.hub, existingID, unhideTriggerEnroll)
		}

		// Generate a kill token for re-enrollment (rotates on every enroll)
		killTokenPlain, killTokenHash, killErr := generateKillToken()
		if killErr != nil {
			log.Printf("[Enrollment] Warning: failed to generate kill token for re-enrolling device %s: %v", existingID, killErr)
		} else {
			if _, dbErr := r.db.Pool().Exec(ctx,
				"UPDATE devices SET kill_token_hash = $1 WHERE id = $2",
				killTokenHash, existingID,
			); dbErr != nil {
				log.Printf("[Enrollment] Warning: failed to store kill token hash for device %s: %v", existingID, dbErr)
				killTokenPlain = "" // Don't return a token we couldn't persist
			}
		}

		response := gin.H{
			"success":  true,
			"deviceId": existingID,
			"config": map[string]int{
				"heartbeatInterval": 30,
				"metricsInterval":   2, // 2 seconds default
			},
		}
		if killTokenPlain != "" {
			response["killToken"] = killTokenPlain
		}
		c.JSON(http.StatusOK, response)
		return
	}

	// Create new device
	deviceID := uuid.New()
	displayName := enrollment.Hostname
	if displayName == "" {
		displayName = enrollment.AgentID
	}

	// Default device_type to 'desktop' if not provided
	deviceType := enrollment.DeviceType
	if deviceType == "" {
		deviceType = "desktop"
	}

	// Generate kill token for the new device
	killTokenPlain, killTokenHash, killErr := generateKillToken()
	if killErr != nil {
		log.Printf("[Enrollment] Warning: failed to generate kill token for new device %s: %v", deviceID, killErr)
	}

	var killTokenHashParam interface{}
	if killErr == nil {
		killTokenHashParam = killTokenHash
	}

	_, err = r.db.Pool().Exec(ctx, `
		INSERT INTO devices (id, agent_id, hostname, display_name, device_type, os_type, os_version,
			os_build, platform, platform_family, architecture, cpu_model, cpu_cores,
			cpu_threads, cpu_speed, total_memory, boot_time, gpu, storage, serial_number,
			manufacturer, model, domain, agent_version, ip_address, mac_address,
			last_seen, status, kill_token_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, to_timestamp($17),
			$18, $19, $20, $21, $22, $23, $24, $25, $26, NOW(), 'online', $27)
	`, deviceID, enrollment.AgentID, enrollment.Hostname, displayName, deviceType, enrollment.OSType,
		enrollment.OSVersion, enrollment.OSBuild, enrollment.Platform, enrollment.PlatformFamily,
		enrollment.Architecture, enrollment.CPUModel, enrollment.CPUCores, enrollment.CPUThreads,
		enrollment.CPUSpeed, enrollment.TotalMemory, enrollment.BootTime, string(gpuJSON), storageJSON,
		enrollment.SerialNumber, enrollment.Manufacturer, enrollment.Model, enrollment.Domain,
		enrollment.AgentVersion, ipAddr, enrollment.MACAddress, killTokenHashParam)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create device"})
		return
	}

	response := gin.H{
		"success":  true,
		"deviceId": deviceID,
		"config": map[string]int{
			"heartbeatInterval": 30,
			"metricsInterval":   2, // 2 seconds default
		},
	}
	if killTokenPlain != "" {
		response["killToken"] = killTokenPlain
	}
	c.JSON(http.StatusCreated, response)
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

// listDeviceCommands returns commands for a specific device
func (r *Router) listDeviceCommands(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()

	pageSize := 10
	if ps := c.Query("pageSize"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 && parsed <= 100 {
			pageSize = parsed
		}
	}

	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, device_id, command_type, command, status, output, error_message,
			   exit_code, created_by, created_at, started_at, completed_at
		FROM commands WHERE device_id = $1 ORDER BY created_at DESC LIMIT $2
	`, id, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch commands"})
		return
	}
	defer rows.Close()

	commands := make([]models.Command, 0)
	for rows.Next() {
		var cmd models.Command
		err := rows.Scan(&cmd.ID, &cmd.DeviceID, &cmd.CommandType, &cmd.Command,
			&cmd.Status, &cmd.Output, &cmd.ErrorMessage, &cmd.ExitCode,
			&cmd.CreatedBy, &cmd.CreatedAt, &cmd.StartedAt, &cmd.CompletedAt)
		if err != nil {
			log.Printf("Error scanning command row for device %s: %v", id, err)
			continue
		}
		commands = append(commands, cmd)
	}

	c.JSON(http.StatusOK, gin.H{
		"commands": commands,
		"total":    len(commands),
	})
}

// listCommands returns all commands with optional filtering
func (r *Router) listCommands(c *gin.Context) {
	ctx := context.Background()
	deviceID := c.Query("deviceId")

	var query string
	var args []interface{}

	if deviceID != "" {
		id, _ := uuid.Parse(deviceID)
		query = `
			SELECT id, device_id, command_type, command, status, output, error_message,
				   exit_code, created_by, created_at, started_at, completed_at
			FROM commands WHERE device_id = $1 ORDER BY created_at DESC LIMIT 100`
		args = []interface{}{id}
	} else {
		query = `
			SELECT id, device_id, command_type, command, status, output, error_message,
				   exit_code, created_by, created_at, started_at, completed_at
			FROM commands ORDER BY created_at DESC LIMIT 100`
		args = []interface{}{}
	}

	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch commands"})
		return
	}
	defer rows.Close()

	commands := make([]models.Command, 0)
	for rows.Next() {
		var cmd models.Command
		err := rows.Scan(&cmd.ID, &cmd.DeviceID, &cmd.CommandType, &cmd.Command,
			&cmd.Status, &cmd.Output, &cmd.ErrorMessage, &cmd.ExitCode,
			&cmd.CreatedBy, &cmd.CreatedAt, &cmd.StartedAt, &cmd.CompletedAt)
		if err != nil {
			log.Printf("Error scanning command row: %v", err)
			continue
		}
		commands = append(commands, cmd)
	}

	c.JSON(http.StatusOK, gin.H{
		"commands": commands,
		"total":    len(commands),
	})
}

func (r *Router) getCommand(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid command ID"})
		return
	}

	ctx := context.Background()

	var cmd models.Command
	err = r.db.Pool().QueryRow(ctx, `
		SELECT id, device_id, command_type, command, status, output, error_message,
			   exit_code, created_by, created_at, started_at, completed_at
		FROM commands WHERE id = $1
	`, id).Scan(&cmd.ID, &cmd.DeviceID, &cmd.CommandType, &cmd.Command,
		&cmd.Status, &cmd.Output, &cmd.ErrorMessage, &cmd.ExitCode,
		&cmd.CreatedBy, &cmd.CreatedAt, &cmd.StartedAt, &cmd.CompletedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Command not found"})
		return
	}

	c.JSON(http.StatusOK, cmd)
}



// uninstallAgent sends an uninstall command to the agent
func (r *Router) uninstallAgent(c *gin.Context) {
	log.Printf("[Uninstall] Received uninstall request for device %s", c.Param("id"))

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		log.Printf("[Uninstall] Invalid device ID: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()

	// Get device agent ID
	var agentID string
	err = r.db.Pool().QueryRow(ctx, "SELECT agent_id FROM devices WHERE id = $1 AND organization_id = $2", id, constants.CurrentOrganizationID).Scan(&agentID)
	if err != nil {
		log.Printf("[Uninstall] Device not found: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}
	log.Printf("[Uninstall] Found device with agent_id: %s", agentID)

	// Check if agent is online
	if !r.hub.IsAgentOnline(agentID) {
		log.Printf("[Uninstall] Agent %s is offline", agentID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Device is offline. Cannot uninstall an offline agent."})
		return
	}
	log.Printf("[Uninstall] Agent %s is online, proceeding with uninstall", agentID)

	// Generate an uninstall token for security
	uninstallToken := uuid.New().String()
	requestID := uuid.New().String()

	// Send uninstall command to agent
	msg := websocket.Message{
		Type:      "uninstall_agent",
		RequestID: requestID,
		Payload: json.RawMessage(mustMarshal(map[string]interface{}{
			"deviceId":       id.String(),
			"uninstallToken": uninstallToken,
		})),
	}

	msgBytes, _ := json.Marshal(msg)
	if err := r.hub.SendToAgent(agentID, msgBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send uninstall command to agent"})
		return
	}

	// Mark device as pending uninstall in database
	if _, err := r.db.Pool().Exec(ctx, `
		UPDATE devices SET status = 'uninstalling', updated_at = NOW() WHERE id = $1
	`, id); err != nil {
		log.Printf("Error updating device %s status to uninstalling: %v", id, err)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Uninstall command sent to agent",
		"requestId": requestID,
		"status":    "uninstalling",
	})
}

// pingAgent sends a ping to the agent to check if it's responsive
// This helps verify connectivity when the app shows the device as offline
// but the agent service is actually running
func (r *Router) pingAgent(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()

	// Get device agent ID
	var agentID string
	err = r.db.Pool().QueryRow(ctx, "SELECT agent_id FROM devices WHERE id = $1 AND organization_id = $2", id, constants.CurrentOrganizationID).Scan(&agentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	// Check if agent is in the WebSocket hub (connected)
	isOnline := r.hub.IsAgentOnline(agentID)

	if !isOnline {
		// Agent is not connected to WebSocket - update status to offline
		if _, err := r.db.Pool().Exec(ctx, `
			UPDATE devices SET status = 'offline', updated_at = NOW() WHERE id = $1
		`, id); err != nil {
			log.Printf("Error updating device %s status to offline: %v", id, err)
		}

		// Broadcast offline status to dashboards
		statusMsg, _ := json.Marshal(map[string]interface{}{
			"type":     "device_status",
			"deviceId": id.String(),
			"status":   "offline",
		})
		r.hub.BroadcastToDashboards(statusMsg)

		c.JSON(http.StatusOK, gin.H{
			"online":  false,
			"status":  "offline",
			"message": "Agent is not connected. The agent service may need to be restarted.",
		})
		return
	}

	// Agent is connected - send a ping message
	requestID := uuid.New().String()
	msg := websocket.Message{
		Type:      websocket.MsgTypePing,
		RequestID: requestID,
	}

	msgBytes, _ := json.Marshal(msg)
	if err := r.hub.SendToAgent(agentID, msgBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"online":  false,
			"status":  "error",
			"message": "Failed to send ping to agent",
		})
		return
	}

	// Update device status to online and last_seen
	if _, err := r.db.Pool().Exec(ctx, `
		UPDATE devices SET status = 'online', last_seen = NOW(), updated_at = NOW() WHERE id = $1
	`, id); err != nil {
		log.Printf("Error updating device %s status to online: %v", id, err)
	}

	// Broadcast online status to dashboards
	statusMsg, _ := json.Marshal(map[string]interface{}{
		"type":     "device_status",
		"deviceId": id.String(),
		"status":   "online",
		"lastSeen": time.Now().UTC().Format(time.RFC3339),
	})
	r.hub.BroadcastToDashboards(statusMsg)

	c.JSON(http.StatusOK, gin.H{
		"online":    true,
		"status":    "online",
		"message":   "Agent is connected and responsive",
		"requestId": requestID,
	})
}

// forceUpdate sends a command to the agent to trigger an immediate update check
func (r *Router) forceUpdate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()

	// Get device agent ID and current version
	var agentID, currentVersion string
	err = r.db.Pool().QueryRow(ctx, "SELECT agent_id, COALESCE(agent_version, '') FROM devices WHERE id = $1 AND organization_id = $2", id, constants.CurrentOrganizationID).Scan(&agentID, &currentVersion)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	// Check if agent is online
	if !r.hub.IsAgentOnline(agentID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Device is offline. Cannot send update command to offline agent."})
		return
	}

	log.Printf("[ForceUpdate] Sending force update command to agent %s (current version: %s)", agentID, currentVersion)

	// Send force update command to agent
	requestID := uuid.New().String()
	msg := websocket.Message{
		Type:      "force_update",
		RequestID: requestID,
	}

	msgBytes, _ := json.Marshal(msg)
	if err := r.hub.SendToAgent(agentID, msgBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send update command to agent"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":        "Update check triggered",
		"requestId":      requestID,
		"currentVersion": currentVersion,
	})
}

// powerAction sends a power action command to the agent (shutdown, restart, wake)
func (r *Router) powerAction(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	var req struct {
		Action string `json:"action"` // shutdown, restart, wake
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Validate action
	validActions := map[string]bool{"shutdown": true, "restart": true, "wake": true}
	if !validActions[req.Action] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid action. Must be 'shutdown', 'restart', or 'wake'"})
		return
	}

	ctx := context.Background()

	// Get device info including agent ID and power management capabilities
	var agentID, macAddress string
	var powerMgmtJSON []byte
	err = r.db.Pool().QueryRow(ctx, `
		SELECT agent_id, COALESCE(mac_address, ''), COALESCE(power_management, '{}'::jsonb)
		FROM devices WHERE id = $1 AND organization_id = $2
	`, id, constants.CurrentOrganizationID).Scan(&agentID, &macAddress, &powerMgmtJSON)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	// For wake action, we need WoL support and the device should be offline
	if req.Action == "wake" {
		// Parse power management JSON
		var powerMgmt struct {
			WoLSupported bool   `json:"wol_supported"`
			WoLEnabled   bool   `json:"wol_enabled"`
			MACAddress   string `json:"mac_address"`
		}
		if err := json.Unmarshal(powerMgmtJSON, &powerMgmt); err != nil {
			log.Printf("Error parsing power management for device %s: %v", id, err)
		}

		if !powerMgmt.WoLSupported {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Wake-on-LAN is not supported on this device"})
			return
		}

		// Use MAC from power management if available, otherwise use device MAC
		wolMAC := powerMgmt.MACAddress
		if wolMAC == "" {
			wolMAC = macAddress
		}
		if wolMAC == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No MAC address available for Wake-on-LAN"})
			return
		}

		// Send WoL packet via any online agent on the same network
		// For now, we'll try to use the agent on the same subnet
		// In a more advanced implementation, we'd pick an agent on the same broadcast domain
		requestID := uuid.New().String()
		payloadBytes, _ := json.Marshal(map[string]interface{}{
			"action":     "wake",
			"macAddress": wolMAC,
		})
		msg := websocket.Message{
			Type:      websocket.MsgTypePowerAction,
			RequestID: requestID,
			Payload:   payloadBytes,
		}

		msgBytes, _ := json.Marshal(msg)

		// Try to send to the target agent first (it might be coming online)
		// If that fails, broadcast to all agents on the same network
		if err := r.hub.SendToAgent(agentID, msgBytes); err != nil {
			// Agent is offline, try to broadcast WoL via any online agent
			log.Printf("[PowerAction] Target agent %s is offline, attempting WoL broadcast via other agents", agentID)
			// For now, return an error - in future we could implement subnet-aware WoL relay
			c.JSON(http.StatusOK, gin.H{
				"message":   "Wake-on-LAN packet will be sent when an agent on the same network is available",
				"requestId": requestID,
				"action":    req.Action,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":   "Wake-on-LAN command sent",
			"requestId": requestID,
			"action":    req.Action,
		})
		return
	}

	// For shutdown/restart, the device must be online
	if !r.hub.IsAgentOnline(agentID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Device is offline. Cannot send power command to offline agent."})
		return
	}

	log.Printf("[PowerAction] Sending %s command to agent %s", req.Action, agentID)

	// Send power action command to agent
	requestID := uuid.New().String()
	payloadBytes, _ := json.Marshal(map[string]interface{}{
		"action": req.Action,
	})
	msg := websocket.Message{
		Type:      websocket.MsgTypePowerAction,
		RequestID: requestID,
		Payload:   payloadBytes,
	}

	msgBytes, _ := json.Marshal(msg)
	if err := r.hub.SendToAgent(agentID, msgBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send power command to agent"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   strings.Title(req.Action) + " command sent",
		"requestId": requestID,
		"action":    req.Action,
	})
}

// InstallUpdatesRequest represents a request to install Windows updates
type InstallUpdatesRequest struct {
	SecurityOnly    bool     `json:"securityOnly"`    // Only install security updates
	SpecificKBs     []string `json:"specificKBs"`     // Install only specific KB articles
	AcceptEULA      bool     `json:"acceptEULA"`      // Automatically accept EULAs
	AllowReboot     bool     `json:"allowReboot"`     // Allow automatic reboot if required
	RebootDelaySecs int      `json:"rebootDelaySecs"` // Delay before reboot (if allowed)
}

func (r *Router) installUpdates(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	var req InstallUpdatesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	ctx := context.Background()

	// Get device agent ID and OS type
	var agentID, osType string
	err = r.db.Pool().QueryRow(ctx, `
		SELECT agent_id, COALESCE(os_type, 'unknown')
		FROM devices WHERE id = $1 AND organization_id = $2
	`, id, constants.CurrentOrganizationID).Scan(&agentID, &osType)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	// Validate OS type - only Windows supports this feature
	if osType != "windows" && osType != "Windows" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Windows Update installation is only supported on Windows devices"})
		return
	}

	// Check if agent is online
	if !r.hub.IsAgentOnline(agentID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Device is offline. Cannot install updates on offline agent."})
		return
	}

	log.Printf("[InstallUpdates] Initiating update installation on device %s (agent: %s)", id, agentID)

	// Send install updates command to agent
	requestID := uuid.New().String()
	payloadBytes, _ := json.Marshal(map[string]interface{}{
		"securityOnly":    req.SecurityOnly,
		"specificKBs":     req.SpecificKBs,
		"acceptEULA":      req.AcceptEULA,
		"allowReboot":     req.AllowReboot,
		"rebootDelaySecs": req.RebootDelaySecs,
	})
	msg := websocket.Message{
		Type:      websocket.MsgTypeInstallUpdates,
		RequestID: requestID,
		Payload:   payloadBytes,
	}

	msgBytes, _ := json.Marshal(msg)
	if err := r.hub.SendToAgent(agentID, msgBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send install command to agent"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Windows Update installation initiated",
		"requestId": requestID,
		"deviceId":  id.String(),
		"options": map[string]interface{}{
			"securityOnly":    req.SecurityOnly,
			"specificKBs":     req.SpecificKBs,
			"acceptEULA":      req.AcceptEULA,
			"allowReboot":     req.AllowReboot,
			"rebootDelaySecs": req.RebootDelaySecs,
		},
	})
}
