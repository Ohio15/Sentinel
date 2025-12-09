package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/models"
	"github.com/sentinel/server/internal/websocket"
)

func (r *Router) listDevices(c *gin.Context) {
	ctx := context.Background()

	rows, err := r.db.Pool.Query(ctx, `
		SELECT id, agent_id, hostname, display_name, os_type, os_version, os_build,
			   platform, platform_family, architecture, cpu_model, cpu_cores, cpu_threads,
			   cpu_speed, total_memory, boot_time, gpu, storage, serial_number,
			   manufacturer, model, domain, agent_version, last_seen, status,
			   ip_address, public_ip, mac_address, tags, metadata, created_at, updated_at
		FROM devices
		ORDER BY hostname
	`)
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

		err := rows.Scan(&d.ID, &d.AgentID, &d.Hostname, &d.DisplayName, &d.OSType,
			&d.OSVersion, &d.OSBuild, &d.Platform, &d.PlatformFamily, &d.Architecture,
			&d.CPUModel, &d.CPUCores, &d.CPUThreads, &d.CPUSpeed, &d.TotalMemory,
			&d.BootTime, &gpuJSON, &storageJSON, &d.SerialNumber, &d.Manufacturer,
			&d.Model, &d.Domain, &d.AgentVersion, &d.LastSeen, &d.Status,
			&d.IPAddress, &d.PublicIP, &d.MACAddress, &tags, &metadata,
			&d.CreatedAt, &d.UpdatedAt)
		if err != nil {
			continue
		}
		d.Tags = tags
		d.Metadata = metadata
		json.Unmarshal(gpuJSON, &d.GPU)
		json.Unmarshal(storageJSON, &d.Storage)

		// Check if agent is currently connected
		if r.hub.IsAgentOnline(d.AgentID) {
			d.Status = "online"
		}

		devices = append(devices, d)
	}

	c.JSON(http.StatusOK, devices)
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
	var gpuJSON, storageJSON []byte

	err = r.db.Pool.QueryRow(ctx, `
		SELECT id, agent_id, hostname, display_name, os_type, os_version, os_build,
			   platform, platform_family, architecture, cpu_model, cpu_cores, cpu_threads,
			   cpu_speed, total_memory, boot_time, gpu, storage, serial_number,
			   manufacturer, model, domain, agent_version, last_seen, status,
			   ip_address, public_ip, mac_address, tags, metadata, created_at, updated_at
		FROM devices WHERE id = $1
	`, id).Scan(&d.ID, &d.AgentID, &d.Hostname, &d.DisplayName, &d.OSType,
		&d.OSVersion, &d.OSBuild, &d.Platform, &d.PlatformFamily, &d.Architecture,
		&d.CPUModel, &d.CPUCores, &d.CPUThreads, &d.CPUSpeed, &d.TotalMemory,
		&d.BootTime, &gpuJSON, &storageJSON, &d.SerialNumber, &d.Manufacturer,
		&d.Model, &d.Domain, &d.AgentVersion, &d.LastSeen, &d.Status,
		&d.IPAddress, &d.PublicIP, &d.MACAddress, &tags, &metadata,
		&d.CreatedAt, &d.UpdatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	d.Tags = tags
	d.Metadata = metadata
	json.Unmarshal(gpuJSON, &d.GPU)
	json.Unmarshal(storageJSON, &d.Storage)

	if r.hub.IsAgentOnline(d.AgentID) {
		d.Status = "online"
	}

	c.JSON(http.StatusOK, d)
}

func (r *Router) deleteDevice(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()

	result, err := r.db.Pool.Exec(ctx, "DELETE FROM devices WHERE id = $1", id)
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

	rows, err := r.db.Pool.Query(ctx, `
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

	if req.CommandType == "" {
		req.CommandType = "shell"
	}

	ctx := context.Background()
	userID := c.MustGet("userId").(uuid.UUID)

	// Get device agent ID
	var agentID string
	err = r.db.Pool.QueryRow(ctx, "SELECT agent_id FROM devices WHERE id = $1", id).Scan(&agentID)
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

	_, err = r.db.Pool.Exec(ctx, `
		INSERT INTO commands (id, device_id, command_type, command, status, created_by)
		VALUES ($1, $2, $3, $4, 'pending', $5)
	`, commandID, id, req.CommandType, req.Command, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create command"})
		return
	}

	// Send command to agent
	msg := websocket.Message{
		Type:      websocket.MsgTypeCommand,
		RequestID: requestID,
		Payload: json.RawMessage(mustMarshal(map[string]interface{}{
			"commandId":   commandID.String(),
			"command":     req.Command,
			"commandType": req.CommandType,
		})),
	}

	msgBytes, _ := json.Marshal(msg)
	if err := r.hub.SendToAgent(agentID, msgBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send command to agent"})
		return
	}

	// Update command status to running
	r.db.Pool.Exec(ctx, `
		UPDATE commands SET status = 'running', started_at = NOW() WHERE id = $1
	`, commandID)

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
	err := r.db.Pool.QueryRow(ctx, "SELECT id FROM devices WHERE agent_id = $1", enrollment.AgentID).Scan(&existingID)

	if err == nil {
		// Update existing device
		_, err = r.db.Pool.Exec(ctx, `
			UPDATE devices SET
				hostname = $2, os_type = $3, os_version = $4, os_build = $5,
				platform = $6, platform_family = $7, architecture = $8,
				cpu_model = $9, cpu_cores = $10, cpu_threads = $11, cpu_speed = $12,
				total_memory = $13, boot_time = $14, gpu = $15, storage = $16,
				serial_number = $17, manufacturer = $18, model = $19, domain = $20,
				agent_version = $21, ip_address = $22, mac_address = $23,
				last_seen = NOW(), status = 'online', updated_at = NOW()
			WHERE agent_id = $1
		`, enrollment.AgentID, enrollment.Hostname, enrollment.OSType, enrollment.OSVersion,
			enrollment.OSBuild, enrollment.Platform, enrollment.PlatformFamily, enrollment.Architecture,
			enrollment.CPUModel, enrollment.CPUCores, enrollment.CPUThreads, enrollment.CPUSpeed,
			enrollment.TotalMemory, enrollment.BootTime, gpuJSON, storageJSON,
			enrollment.SerialNumber, enrollment.Manufacturer, enrollment.Model, enrollment.Domain,
			enrollment.AgentVersion, ipAddr, enrollment.MACAddress)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update device"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"deviceId": existingID,
			"config": map[string]int{
				"heartbeatInterval": 30,
				"metricsInterval":   60,
			},
		})
		return
	}

	// Create new device
	deviceID := uuid.New()
	displayName := enrollment.Hostname
	if displayName == "" {
		displayName = enrollment.AgentID
	}

	_, err = r.db.Pool.Exec(ctx, `
		INSERT INTO devices (id, agent_id, hostname, display_name, os_type, os_version,
			os_build, platform, platform_family, architecture, cpu_model, cpu_cores,
			cpu_threads, cpu_speed, total_memory, boot_time, gpu, storage, serial_number,
			manufacturer, model, domain, agent_version, ip_address, mac_address,
			last_seen, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
			$17, $18, $19, $20, $21, $22, $23, $24, $25, NOW(), 'online')
	`, deviceID, enrollment.AgentID, enrollment.Hostname, displayName, enrollment.OSType,
		enrollment.OSVersion, enrollment.OSBuild, enrollment.Platform, enrollment.PlatformFamily,
		enrollment.Architecture, enrollment.CPUModel, enrollment.CPUCores, enrollment.CPUThreads,
		enrollment.CPUSpeed, enrollment.TotalMemory, enrollment.BootTime, gpuJSON, storageJSON,
		enrollment.SerialNumber, enrollment.Manufacturer, enrollment.Model, enrollment.Domain,
		enrollment.AgentVersion, ipAddr, enrollment.MACAddress)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create device"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success":  true,
		"deviceId": deviceID,
		"config": map[string]int{
			"heartbeatInterval": 30,
			"metricsInterval":   60,
		},
	})
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

	rows, err := r.db.Pool.Query(ctx, `
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

	rows, err := r.db.Pool.Query(ctx, query, args...)
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
	err = r.db.Pool.QueryRow(ctx, `
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


