package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	ws "github.com/sentinel/server/internal/websocket"
)

// getUpgrader returns a WebSocket upgrader with proper origin validation
func (r *Router) getUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(req *http.Request) bool {
			// In non-production, allow all origins
			if r.config.Environment != "production" {
				return true
			}
			// Allow WebSocket connections without Origin header (native apps, etc.)
			origin := req.Header.Get("Origin")
			if origin == "" {
				return true
			}
			// Check against allowed origins
			for _, allowed := range r.config.AllowedOrigins {
				if origin == allowed {
					return true
				}
			}
			return false
		},
	}
}

// WebSocket Handlers

func (r *Router) handleAgentWebSocket(c *gin.Context) {
	upgrader := r.getUpgrader()
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	// Wait for auth message
	_, message, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}

	var authMsg ws.Message
	if err := json.Unmarshal(message, &authMsg); err != nil || authMsg.Type != ws.MsgTypeAuth {
		conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Invalid auth message"}`)})
		conn.Close()
		return
	}

	var authPayload struct {
		AgentID    string `json:"agentId"`
		Token      string `json:"token"`
		DeviceInfo *struct {
			Hostname     string `json:"hostname"`
			Platform     string `json:"platform"`
			OSType       string `json:"osType"`
			OSVersion    string `json:"osVersion"`
			Architecture string `json:"architecture"`
			CPUModel     string `json:"cpuModel"`
			CPUCores     int    `json:"cpuCores"`
			TotalMemory  uint64 `json:"totalMemory"`
			SerialNumber string `json:"serialNumber"`
			Manufacturer string `json:"manufacturer"`
			Model        string `json:"model"`
			IPAddress    string `json:"ipAddress"`
			MACAddress   string `json:"macAddress"`
		} `json:"deviceInfo,omitempty"`
	}
	if err := json.Unmarshal(authMsg.Payload, &authPayload); err != nil {
		conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Invalid auth payload"}`)})
		conn.Close()
		return
	}

	// Verify token
	if authPayload.Token != r.config.EnrollmentToken {
		conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Invalid token"}`)})
		conn.Close()
		return
	}

	// Get device ID - or auto-enroll if device was deleted
	ctx := context.Background()
	var deviceID uuid.UUID
	var isDisabled bool
	err = r.db.Pool().QueryRow(ctx, "SELECT id, COALESCE(is_disabled, false) FROM devices WHERE agent_id = $1", authPayload.AgentID).Scan(&deviceID, &isDisabled)
	if err != nil {
		// Device not found - auto-enroll as a new device
		log.Printf("Device not found for agent %s, auto-enrolling...", authPayload.AgentID)
		deviceID = uuid.New()
		var insertErr error
		if authPayload.DeviceInfo != nil {
			// Use device info from agent for proper auto-enrollment
			log.Printf("Auto-enrolling with device info: hostname=%s, platform=%s",
				authPayload.DeviceInfo.Hostname, authPayload.DeviceInfo.Platform)
			_, insertErr = r.db.Pool().Exec(ctx, `
				INSERT INTO devices (id, agent_id, hostname, platform, os_type, os_version, 
					architecture, cpu_model, cpu_cores, total_memory, serial_number, 
					manufacturer, model, ip_address, mac_address, status, created_at, last_seen)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, 'online', NOW(), NOW())
			`, deviceID, authPayload.AgentID, authPayload.DeviceInfo.Hostname,
				authPayload.DeviceInfo.Platform, authPayload.DeviceInfo.OSType, authPayload.DeviceInfo.OSVersion,
				authPayload.DeviceInfo.Architecture, authPayload.DeviceInfo.CPUModel, authPayload.DeviceInfo.CPUCores,
				authPayload.DeviceInfo.TotalMemory, authPayload.DeviceInfo.SerialNumber,
				authPayload.DeviceInfo.Manufacturer, authPayload.DeviceInfo.Model,
				authPayload.DeviceInfo.IPAddress, authPayload.DeviceInfo.MACAddress)
		} else {
			// Fallback to minimal enrollment
			_, insertErr = r.db.Pool().Exec(ctx, `
				INSERT INTO devices (id, agent_id, hostname, status, created_at, last_seen)
				VALUES ($1, $2, $3, 'online', NOW(), NOW())
			`, deviceID, authPayload.AgentID, "Auto-enrolled-"+authPayload.AgentID[:8])
		}
		if insertErr != nil {
			log.Printf("Failed to auto-enroll device: %v", insertErr)
			conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Failed to auto-enroll device"}`)})
			conn.Close()
			return
		}
		log.Printf("Auto-enrolled device %s with ID %s", authPayload.AgentID, deviceID)
	}

	// Check if device is disabled
	if isDisabled {
		conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":false,"error":"Device is disabled"}`)})
		conn.Close()
		return
	}

	// Send auth success
	conn.WriteJSON(ws.Message{Type: ws.MsgTypeAuthResponse, Payload: json.RawMessage(`{"success":true}`)})

	// Register client
	client := r.hub.RegisterAgent(conn, authPayload.AgentID, deviceID)

	// Update device status
	if _, err := r.db.Pool().Exec(ctx, "UPDATE devices SET status = 'online', last_seen = NOW() WHERE id = $1", deviceID); err != nil {
		log.Printf("Error updating device %s status to online: %v", deviceID, err)
	}

	// Broadcast online status to dashboards
	onlineMsg, _ := json.Marshal(map[string]interface{}{
		"type":     "device_status",
		"deviceId": deviceID,
		"status":   "online",
	})
	r.hub.BroadcastToDashboards(onlineMsg)

	// Start read/write pumps
	go client.WritePump(ctx)
	client.ReadPump(ctx, func(msg []byte) {
		r.handleAgentMessage(authPayload.AgentID, deviceID, msg)
	})

	// Update device status on disconnect
	if _, err := r.db.Pool().Exec(context.Background(), "UPDATE devices SET status = 'offline' WHERE id = $1", deviceID); err != nil {
		log.Printf("Error updating device %s status to offline: %v", deviceID, err)
	}

	// Broadcast offline status to dashboards
	offlineMsg, _ := json.Marshal(map[string]interface{}{
		"type":     "device_status",
		"deviceId": deviceID,
		"status":   "offline",
	})
	r.hub.BroadcastToDashboards(offlineMsg)
}

func (r *Router) handleAgentMessage(agentID string, deviceID uuid.UUID, message []byte) {
	var msg ws.Message
	if err := json.Unmarshal(message, &msg); err != nil {
		return
	}

	ctx := context.Background()

	switch msg.Type {
	case ws.MsgTypeHeartbeat:
		// Parse heartbeat to get agent version
		var heartbeat struct {
			AgentVersion string `json:"agentVersion"`
		}
		if err := json.Unmarshal(message, &heartbeat); err != nil {
			log.Printf("Failed to unmarshal heartbeat: %v, raw: %s", err, string(message))
		}
		log.Printf("Heartbeat from %s: version=%q", agentID, heartbeat.AgentVersion)

		// Update last seen (and agent version if provided)
		if heartbeat.AgentVersion != "" {
			if _, err := r.db.Pool().Exec(ctx, "UPDATE devices SET last_seen = NOW(), agent_version = $1 WHERE id = $2", heartbeat.AgentVersion, deviceID); err != nil {
				log.Printf("Error updating device %s last_seen with version: %v", deviceID, err)
			}
		} else {
			if _, err := r.db.Pool().Exec(ctx, "UPDATE devices SET last_seen = NOW() WHERE id = $1", deviceID); err != nil {
				log.Printf("Error updating device %s last_seen: %v", deviceID, err)
			}
		}

		// Send ack back to agent
		ackMsg, _ := json.Marshal(ws.Message{Type: ws.MsgTypeHeartbeatAck})
		r.hub.SendToAgent(agentID, ackMsg)

	case ws.MsgTypeMetrics:
		var metrics struct {
			CPUPercent       float64 `json:"cpuPercent"`
			MemoryPercent    float64 `json:"memoryPercent"`
			MemoryUsedBytes  int64   `json:"memoryUsedBytes"`
			MemoryTotalBytes int64   `json:"memoryTotalBytes"`
			DiskPercent      float64 `json:"diskPercent"`
			DiskUsedBytes    int64   `json:"diskUsedBytes"`
			DiskTotalBytes   int64   `json:"diskTotalBytes"`
			NetworkRxBytes   int64   `json:"networkRxBytes"`
			NetworkTxBytes   int64   `json:"networkTxBytes"`
			ProcessCount     int     `json:"processCount"`
		}
		if err := json.Unmarshal(msg.Payload, &metrics); err != nil {
			return
		}

		// Insert metrics
		if _, err := r.db.Pool().Exec(ctx, `
			INSERT INTO device_metrics (device_id, cpu_percent, memory_percent, memory_used_bytes,
				memory_total_bytes, disk_percent, disk_used_bytes, disk_total_bytes,
				network_rx_bytes, network_tx_bytes, process_count)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, deviceID, metrics.CPUPercent, metrics.MemoryPercent, metrics.MemoryUsedBytes,
			metrics.MemoryTotalBytes, metrics.DiskPercent, metrics.DiskUsedBytes,
			metrics.DiskTotalBytes, metrics.NetworkRxBytes, metrics.NetworkTxBytes,
			metrics.ProcessCount); err != nil {
			log.Printf("Error inserting metrics for device %s: %v", deviceID, err)
		}

		// Broadcast to dashboards
		broadcastMsg, _ := json.Marshal(map[string]interface{}{
			"type":     "device_metrics",
			"deviceId": deviceID,
			"metrics":  metrics,
		})
		r.hub.BroadcastToDashboards(broadcastMsg)

		// Check alert rules
		r.checkAlertRules(deviceID, metrics.CPUPercent, metrics.MemoryPercent, metrics.DiskPercent)

	case ws.MsgTypeResponse:
		// Agent sends response data at root level, not in payload field
		// Parse from raw message instead of msg.Payload
		var response struct {
			Type      string          `json:"type"`
			RequestID string          `json:"requestId"`
			Success   bool            `json:"success"`
			Data      json.RawMessage `json:"data"`
			Error     string          `json:"error"`
		}
		if err := json.Unmarshal(message, &response); err != nil {
			log.Printf("[Handler] Failed to parse response: %v", err)
			return
		}

		// Get command ID from request tracking (simplified - just extract from data)
		var data struct {
			CommandID string `json:"commandId"`
			Output    string `json:"output"`
		}
		json.Unmarshal(response.Data, &data)

		if data.CommandID != "" {
			status := "completed"
			if !response.Success {
				status = "failed"
			}

			if _, err := r.db.Pool().Exec(ctx, `
				UPDATE commands SET status = $1, output = $2, error_message = $3, completed_at = NOW()
				WHERE id = $4
			`, status, data.Output, response.Error, data.CommandID); err != nil {
				log.Printf("Error updating command %s status: %v", data.CommandID, err)
			}
		}

		// Forward response to dashboards with requestId preserved
		broadcastMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeResponse,
			"requestId": response.RequestID,
			"success":   response.Success,
			"data":      response.Data,
			"error":     response.Error,
			"deviceId":  deviceID.String(),
			"agentId":   agentID,
		})
		r.hub.BroadcastToDashboards(broadcastMsg)

	case ws.MsgTypeTerminalOutput:
		// Forward terminal output to dashboards
		broadcastMsg, _ := json.Marshal(map[string]interface{}{
			"type":     ws.MsgTypeTerminalOutput,
			"deviceId": deviceID,
			"agentId":  agentID,
			"payload":  msg.Payload,
		})
		r.hub.BroadcastToDashboards(broadcastMsg)

	case ws.MsgTypeFileContent:
		// Forward file content to dashboards
		broadcastMsg, _ := json.Marshal(map[string]interface{}{
			"type":     ws.MsgTypeFileContent,
			"deviceId": deviceID,
			"agentId":  agentID,
			"payload":  msg.Payload,
		})
		r.hub.BroadcastToDashboards(broadcastMsg)

	case ws.MsgTypeRemoteFrame:
		// Forward remote desktop frame to dashboards
		broadcastMsg, _ := json.Marshal(map[string]interface{}{
			"type":     ws.MsgTypeRemoteFrame,
			"deviceId": deviceID,
			"agentId":  agentID,
			"payload":  msg.Payload,
		})
		r.hub.BroadcastToDashboards(broadcastMsg)

	case ws.MsgTypeScanProgress:
		// Forward scan progress to dashboards
		broadcastMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeScanProgress,
			"deviceId":  deviceID,
			"agentId":   agentID,
			"requestId": msg.RequestID,
			"payload":   msg.Payload,
		})
		r.hub.BroadcastToDashboards(broadcastMsg)
	}
}

func (r *Router) checkAlertRules(deviceID uuid.UUID, cpu, memory, disk float64) {
	ctx := context.Background()

	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, name, metric, operator, threshold, severity FROM alert_rules WHERE enabled = true
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var rule struct {
			ID        uuid.UUID
			Name      string
			Metric    string
			Operator  string
			Threshold float64
			Severity  string
		}
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Metric, &rule.Operator, &rule.Threshold, &rule.Severity); err != nil {
			log.Printf("Error scanning alert rule row: %v", err)
			continue
		}

		var value float64
		switch rule.Metric {
		case "cpu_percent":
			value = cpu
		case "memory_percent":
			value = memory
		case "disk_percent":
			value = disk
		default:
			continue
		}

		triggered := false
		switch rule.Operator {
		case "gt":
			triggered = value > rule.Threshold
		case "gte":
			triggered = value >= rule.Threshold
		case "lt":
			triggered = value < rule.Threshold
		case "lte":
			triggered = value <= rule.Threshold
		}

		if triggered {
			// Check cooldown (don't create duplicate alerts)
			var count int
			if err := r.db.Pool().QueryRow(ctx, `
				SELECT COUNT(*) FROM alerts
				WHERE device_id = $1 AND rule_id = $2 AND status != 'resolved'
				AND created_at > NOW() - INTERVAL '15 minutes'
			`, deviceID, rule.ID).Scan(&count); err != nil {
				log.Printf("Error checking alert cooldown for device %s: %v", deviceID, err)
				continue
			}

			if count == 0 {
				if _, err := r.db.Pool().Exec(ctx, `
					INSERT INTO alerts (device_id, rule_id, severity, title, message)
					VALUES ($1, $2, $3, $4, $5)
				`, deviceID, rule.ID, rule.Severity, rule.Name,
					rule.Metric+" is "+rule.Operator+" "+fmt.Sprintf("%.2f", rule.Threshold)); err != nil {
					log.Printf("Error creating alert for device %s: %v", deviceID, err)
				}
			}
		}
	}
}

func (r *Router) handleDashboardWebSocket(c *gin.Context) {
	upgrader := r.getUpgrader()
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	userID := c.MustGet("userId").(uuid.UUID)
	ctx := context.Background()

	client := r.hub.RegisterDashboard(conn, userID)

	go client.WritePump(ctx)
	client.ReadPump(ctx, func(msg []byte) {
		r.handleDashboardMessage(userID, msg)
	})
}

// Scripts handlers
func (r *Router) listScripts(c *gin.Context) {
	ctx := context.Background()

	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, name, description, language, content, os_types, created_at, updated_at
		FROM scripts ORDER BY name
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch scripts"})
		return
	}
	defer rows.Close()

	scripts := make([]map[string]interface{}, 0)
	for rows.Next() {
		var s struct {
			ID          uuid.UUID
			Name        string
			Description *string
			Language    string
			Content     string
			OSTypes     []string
			CreatedAt   time.Time
			UpdatedAt   time.Time
		}
		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.Language, &s.Content, &s.OSTypes, &s.CreatedAt, &s.UpdatedAt); err != nil {
			log.Printf("Error scanning script row: %v", err)
			continue
		}
		scripts = append(scripts, map[string]interface{}{
			"id":          s.ID,
			"name":        s.Name,
			"description": s.Description,
			"language":    s.Language,
			"content":     s.Content,
			"osTypes":     s.OSTypes,
			"createdAt":   s.CreatedAt,
			"updatedAt":   s.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, scripts)
}

func (r *Router) createScript(c *gin.Context) {
	var req struct {
		Name        string   `json:"name" binding:"required"`
		Description string   `json:"description"`
		Language    string   `json:"language" binding:"required"`
		Content     string   `json:"content" binding:"required"`
		OSTypes     []string `json:"osTypes"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	validLanguages := map[string]bool{"powershell": true, "bash": true, "python": true, "batch": true}
	if !validLanguages[req.Language] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid language"})
		return
	}

	userID := c.MustGet("userId").(uuid.UUID)
	ctx := context.Background()

	var id uuid.UUID
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO scripts (name, description, language, content, os_types, created_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
	`, req.Name, req.Description, req.Language, req.Content, req.OSTypes, userID).Scan(&id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create script"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name, "language": req.Language})
}

func (r *Router) getScript(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid script ID"})
		return
	}

	ctx := context.Background()
	var s struct {
		ID          uuid.UUID
		Name        string
		Description *string
		Language    string
		Content     string
		OSTypes     []string
		CreatedAt   time.Time
		UpdatedAt   time.Time
	}

	err = r.db.Pool().QueryRow(ctx, `
		SELECT id, name, description, language, content, os_types, created_at, updated_at
		FROM scripts WHERE id = $1
	`, id).Scan(&s.ID, &s.Name, &s.Description, &s.Language, &s.Content, &s.OSTypes, &s.CreatedAt, &s.UpdatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Script not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id": s.ID, "name": s.Name, "description": s.Description,
		"language": s.Language, "content": s.Content, "osTypes": s.OSTypes,
		"createdAt": s.CreatedAt, "updatedAt": s.UpdatedAt,
	})
}

func (r *Router) updateScript(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid script ID"})
		return
	}

	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Language    string   `json:"language"`
		Content     string   `json:"content"`
		OSTypes     []string `json:"osTypes"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	_, err = r.db.Pool().Exec(ctx, `
		UPDATE scripts SET name = COALESCE(NULLIF($1, ''), name),
		description = COALESCE(NULLIF($2, ''), description),
		language = COALESCE(NULLIF($3, ''), language),
		content = COALESCE(NULLIF($4, ''), content),
		os_types = COALESCE($5, os_types), updated_at = NOW()
		WHERE id = $6
	`, req.Name, req.Description, req.Language, req.Content, req.OSTypes, id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update script"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Script updated successfully"})
}

func (r *Router) deleteScript(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid script ID"})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM scripts WHERE id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete script"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Script not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Script deleted successfully"})
}

func (r *Router) executeScript(c *gin.Context) {
	scriptID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid script ID"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceId" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	ctx := context.Background()
	var script struct {
		Language string
		Content  string
	}

	err = r.db.Pool().QueryRow(ctx, "SELECT language, content FROM scripts WHERE id = $1", scriptID).Scan(&script.Language, &script.Content)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Script not found"})
		return
	}

	userID := c.MustGet("userId").(uuid.UUID)
	var commandID uuid.UUID
	err = r.db.Pool().QueryRow(ctx, `
		INSERT INTO commands (device_id, user_id, command_type, command, status)
		VALUES ($1, $2, $3, $4, 'pending') RETURNING id
	`, deviceID, userID, script.Language, script.Content).Scan(&commandID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create command"})
		return
	}

	msg, _ := json.Marshal(map[string]interface{}{
		"type":      "execute",
		"requestId": commandID.String(),
		"payload": map[string]interface{}{
			"commandId": commandID.String(),
			"type":      script.Language,
			"command":   script.Content,
		},
	})

	var agentID string
	if err := r.db.Pool().QueryRow(ctx, "SELECT agent_id FROM devices WHERE id = $1", deviceID).Scan(&agentID); err != nil {
		log.Printf("Error looking up agent ID for device %s: %v", deviceID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find device agent"})
		return
	}
	if err := r.hub.SendToAgent(agentID, msg); err != nil {
		log.Printf("Error sending script to agent %s: %v", agentID, err)
	}

	c.JSON(http.StatusOK, gin.H{"commandId": commandID, "message": "Script execution started"})
}

// Alerts handlers
func (r *Router) listAlerts(c *gin.Context) {
	ctx := context.Background()
	status := c.Query("status")

	query := `
		SELECT a.id, a.device_id, d.hostname, a.rule_id, a.severity, a.title, a.message,
			   a.status, a.acknowledged_at, a.resolved_at, a.created_at
		FROM alerts a
		LEFT JOIN devices d ON a.device_id = d.id
	`
	args := make([]interface{}, 0)

	if status != "" {
		query += " WHERE a.status = $1"
		args = append(args, status)
	}
	query += " ORDER BY a.created_at DESC LIMIT 100"

	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch alerts"})
		return
	}
	defer rows.Close()

	alerts := make([]map[string]interface{}, 0)
	for rows.Next() {
		var a struct {
			ID             uuid.UUID
			DeviceID       uuid.UUID
			DeviceName     *string
			RuleID         *uuid.UUID
			Severity       string
			Title          string
			Message        *string
			Status         string
			AcknowledgedAt *time.Time
			ResolvedAt     *time.Time
			CreatedAt      time.Time
		}
		if err := rows.Scan(&a.ID, &a.DeviceID, &a.DeviceName, &a.RuleID, &a.Severity,
			&a.Title, &a.Message, &a.Status, &a.AcknowledgedAt, &a.ResolvedAt, &a.CreatedAt); err != nil {
			log.Printf("Error scanning alert row: %v", err)
			continue
		}

		alert := map[string]interface{}{
			"id":        a.ID,
			"deviceId":  a.DeviceID,
			"severity":  a.Severity,
			"title":     a.Title,
			"status":    a.Status,
			"createdAt": a.CreatedAt,
		}
		if a.DeviceName != nil {
			alert["deviceName"] = *a.DeviceName
		}
		if a.Message != nil {
			alert["message"] = *a.Message
		}
		alerts = append(alerts, alert)
	}

	c.JSON(http.StatusOK, alerts)
}

func (r *Router) getAlert(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alert ID"})
		return
	}

	ctx := context.Background()
	var a struct {
		ID             uuid.UUID
		DeviceID       uuid.UUID
		DeviceName     *string
		RuleID         *uuid.UUID
		Severity       string
		Title          string
		Message        *string
		Status         string
		AcknowledgedAt *time.Time
		ResolvedAt     *time.Time
		CreatedAt      time.Time
	}

	err = r.db.Pool().QueryRow(ctx, `
		SELECT a.id, a.device_id, d.hostname, a.rule_id, a.severity, a.title, a.message,
			   a.status, a.acknowledged_at, a.resolved_at, a.created_at
		FROM alerts a LEFT JOIN devices d ON a.device_id = d.id WHERE a.id = $1
	`, id).Scan(&a.ID, &a.DeviceID, &a.DeviceName, &a.RuleID, &a.Severity, &a.Title, &a.Message,
		&a.Status, &a.AcknowledgedAt, &a.ResolvedAt, &a.CreatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Alert not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id": a.ID, "deviceId": a.DeviceID, "deviceName": a.DeviceName,
		"severity": a.Severity, "title": a.Title, "message": a.Message,
		"status": a.Status, "acknowledgedAt": a.AcknowledgedAt,
		"resolvedAt": a.ResolvedAt, "createdAt": a.CreatedAt,
	})
}

func (r *Router) acknowledgeAlert(c *gin.Context) {
	id, _ := uuid.Parse(c.Param("id"))
	userID := c.MustGet("userId").(uuid.UUID)
	ctx := context.Background()

	if _, err := r.db.Pool().Exec(ctx, `
		UPDATE alerts SET status = 'acknowledged', acknowledged_by = $1, acknowledged_at = NOW()
		WHERE id = $2
	`, userID, id); err != nil {
		log.Printf("Error acknowledging alert %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to acknowledge alert"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert acknowledged"})
}

func (r *Router) resolveAlert(c *gin.Context) {
	id, _ := uuid.Parse(c.Param("id"))
	ctx := context.Background()

	if _, err := r.db.Pool().Exec(ctx, "UPDATE alerts SET status = 'resolved', resolved_at = NOW() WHERE id = $1", id); err != nil {
		log.Printf("Error resolving alert %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to resolve alert"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert resolved"})
}

// Alert rules handlers
func (r *Router) listAlertRules(c *gin.Context) {
	ctx := context.Background()

	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, name, description, enabled, metric, operator, threshold, severity,
			   cooldown_minutes, created_at
		FROM alert_rules ORDER BY name
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch alert rules"})
		return
	}
	defer rows.Close()

	rules := make([]map[string]interface{}, 0)
	for rows.Next() {
		var rule struct {
			ID              uuid.UUID
			Name            string
			Description     *string
			Enabled         bool
			Metric          string
			Operator        string
			Threshold       float64
			Severity        string
			CooldownMinutes int
			CreatedAt       time.Time
		}
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Description, &rule.Enabled, &rule.Metric,
			&rule.Operator, &rule.Threshold, &rule.Severity, &rule.CooldownMinutes, &rule.CreatedAt); err != nil {
			log.Printf("Error scanning alert rule row: %v", err)
			continue
		}
		rules = append(rules, map[string]interface{}{
			"id":              rule.ID,
			"name":            rule.Name,
			"description":     rule.Description,
			"enabled":         rule.Enabled,
			"metric":          rule.Metric,
			"operator":        rule.Operator,
			"threshold":       rule.Threshold,
			"severity":        rule.Severity,
			"cooldownMinutes": rule.CooldownMinutes,
			"createdAt":       rule.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, rules)
}

func (r *Router) createAlertRule(c *gin.Context) {
	var req struct {
		Name            string  `json:"name" binding:"required"`
		Description     string  `json:"description"`
		Metric          string  `json:"metric" binding:"required"`
		Operator        string  `json:"operator" binding:"required"`
		Threshold       float64 `json:"threshold" binding:"required"`
		Severity        string  `json:"severity" binding:"required"`
		CooldownMinutes int     `json:"cooldownMinutes"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	var id uuid.UUID
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO alert_rules (name, description, metric, operator, threshold, severity, cooldown_minutes)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id
	`, req.Name, req.Description, req.Metric, req.Operator, req.Threshold, req.Severity, req.CooldownMinutes).Scan(&id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create alert rule"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name})
}

func (r *Router) getAlertRule(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alert rule ID"})
		return
	}

	ctx := context.Background()
	var rule struct {
		ID              uuid.UUID
		Name            string
		Description     *string
		Enabled         bool
		Metric          string
		Operator        string
		Threshold       float64
		Severity        string
		CooldownMinutes int
		CreatedAt       time.Time
	}

	err = r.db.Pool().QueryRow(ctx, `
		SELECT id, name, description, enabled, metric, operator, threshold, severity, cooldown_minutes, created_at
		FROM alert_rules WHERE id = $1
	`, id).Scan(&rule.ID, &rule.Name, &rule.Description, &rule.Enabled, &rule.Metric,
		&rule.Operator, &rule.Threshold, &rule.Severity, &rule.CooldownMinutes, &rule.CreatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Alert rule not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id": rule.ID, "name": rule.Name, "description": rule.Description,
		"enabled": rule.Enabled, "metric": rule.Metric, "operator": rule.Operator,
		"threshold": rule.Threshold, "severity": rule.Severity,
		"cooldownMinutes": rule.CooldownMinutes, "createdAt": rule.CreatedAt,
	})
}

func (r *Router) updateAlertRule(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alert rule ID"})
		return
	}

	var req struct {
		Name            string  `json:"name"`
		Description     string  `json:"description"`
		Enabled         *bool   `json:"enabled"`
		Metric          string  `json:"metric"`
		Operator        string  `json:"operator"`
		Threshold       float64 `json:"threshold"`
		Severity        string  `json:"severity"`
		CooldownMinutes int     `json:"cooldownMinutes"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	_, err = r.db.Pool().Exec(ctx, `
		UPDATE alert_rules SET 
			name = COALESCE(NULLIF($1, ''), name),
			description = COALESCE(NULLIF($2, ''), description),
			metric = COALESCE(NULLIF($3, ''), metric),
			operator = COALESCE(NULLIF($4, ''), operator),
			severity = COALESCE(NULLIF($5, ''), severity),
			updated_at = NOW()
		WHERE id = $6
	`, req.Name, req.Description, req.Metric, req.Operator, req.Severity, id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update alert rule"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert rule updated successfully"})
}

func (r *Router) deleteAlertRule(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alert rule ID"})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM alert_rules WHERE id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete alert rule"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Alert rule not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert rule deleted successfully"})
}

// Settings handlers
func (r *Router) getSettings(c *gin.Context) {
	ctx := context.Background()

	rows, err := r.db.Pool().Query(ctx, "SELECT key, value FROM settings")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
		return
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			log.Printf("Error scanning settings row: %v", err)
			continue
		}
		settings[key] = value
	}

	c.JSON(http.StatusOK, settings)
}

func (r *Router) updateSettings(c *gin.Context) {
	var req map[string]string
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	for key, value := range req {
		_, err := r.db.Pool().Exec(ctx, `
			INSERT INTO settings (key, value) VALUES ($1, $2)
			ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()
		`, key, value)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update settings"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings updated successfully"})
}

// Users handlers
func (r *Router) listUsers(c *gin.Context) {
	ctx := context.Background()

	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, email, first_name, last_name, role, is_active, last_login, created_at
		FROM users ORDER BY email
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch users"})
		return
	}
	defer rows.Close()

	users := make([]map[string]interface{}, 0)
	for rows.Next() {
		var u struct {
			ID        uuid.UUID
			Email     string
			FirstName *string
			LastName  *string
			Role      string
			IsActive  bool
			LastLogin *time.Time
			CreatedAt time.Time
		}
		if err := rows.Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.Role,
			&u.IsActive, &u.LastLogin, &u.CreatedAt); err != nil {
			log.Printf("Error scanning user row: %v", err)
			continue
		}
		users = append(users, map[string]interface{}{
			"id":        u.ID,
			"email":     u.Email,
			"firstName": u.FirstName,
			"lastName":  u.LastName,
			"role":      u.Role,
			"isActive":  u.IsActive,
			"lastLogin": u.LastLogin,
			"createdAt": u.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, users)
}

func (r *Router) createUser(c *gin.Context) {
	var req struct {
		Email     string `json:"email"`
		Password  string `json:"password"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
		Role      string `json:"role"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Role == "" {
		req.Role = "user"
	}

	// Hash password
	hashedPassword, err := hashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	ctx := context.Background()
	var id uuid.UUID
	err = r.db.Pool().QueryRow(ctx, `INSERT INTO users (email, password_hash, first_name, last_name, role) VALUES ($1, $2, $3, $4, $5) RETURNING id`, req.Email, hashedPassword, req.FirstName, req.LastName, req.Role).Scan(&id)

	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":        id,
		"email":     req.Email,
		"firstName": req.FirstName,
		"lastName":  req.LastName,
		"role":      req.Role,
	})
}

func (r *Router) updateUser(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	var req struct {
		Email     string `json:"email"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
		Role      string `json:"role"`
		Password  string `json:"password"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	// Build dynamic update query
	updates := make([]string, 0)
	args := make([]interface{}, 0)
	argNum := 1

	if req.Email != "" {
		updates = append(updates, "email = $"+strconv.Itoa(argNum))
		args = append(args, req.Email)
		argNum++
	}
	if req.FirstName != "" {
		updates = append(updates, "first_name = $"+strconv.Itoa(argNum))
		args = append(args, req.FirstName)
		argNum++
	}
	if req.LastName != "" {
		updates = append(updates, "last_name = $"+strconv.Itoa(argNum))
		args = append(args, req.LastName)
		argNum++
	}
	if req.Role != "" {
		updates = append(updates, "role = $"+strconv.Itoa(argNum))
		args = append(args, req.Role)
		argNum++
	}
	if req.Password != "" {
		hashedPassword, err := hashPassword(req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}
		updates = append(updates, "password_hash = $"+strconv.Itoa(argNum))
		args = append(args, hashedPassword)
		argNum++
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No fields to update"})
		return
	}

	args = append(args, id)
	query := "UPDATE users SET " + strings.Join(updates, ", ") + ", updated_at = NOW() WHERE id = $" + strconv.Itoa(argNum)

	_, err = r.db.Pool().Exec(ctx, query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User updated successfully"})
}

func (r *Router) deleteUser(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	ctx := context.Background()

	// Soft delete by setting is_active to false
	_, err = r.db.Pool().Exec(ctx, "UPDATE users SET is_active = false, updated_at = NOW() WHERE id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User deleted successfully"})
}

// Dashboard stats handler
func (r *Router) getDashboardStats(c *gin.Context) {
	ctx := context.Background()

	stats := make(map[string]interface{})

	// Total devices
	var totalDevices int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices").Scan(&totalDevices); err != nil {
		log.Printf("Error getting total devices count: %v", err)
	}
	stats["totalDevices"] = totalDevices

	// Online devices
	var onlineDevices int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices WHERE status = 'online'").Scan(&onlineDevices); err != nil {
		log.Printf("Error getting online devices count: %v", err)
	}
	stats["onlineDevices"] = onlineDevices

	// Offline devices
	var offlineDevices int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices WHERE status = 'offline'").Scan(&offlineDevices); err != nil {
		log.Printf("Error getting offline devices count: %v", err)
	}
	stats["offlineDevices"] = offlineDevices

	// Critical alerts
	var criticalAlerts int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE severity = 'critical' AND status = 'open'").Scan(&criticalAlerts); err != nil {
		log.Printf("Error getting critical alerts count: %v", err)
	}
	stats["criticalAlerts"] = criticalAlerts

	// Warning alerts
	var warningAlerts int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE severity = 'warning' AND status = 'open'").Scan(&warningAlerts); err != nil {
		log.Printf("Error getting warning alerts count: %v", err)
	}
	stats["warningAlerts"] = warningAlerts

	// Total alerts
	var totalAlerts int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE status = 'open'").Scan(&totalAlerts); err != nil {
		log.Printf("Error getting total alerts count: %v", err)
	}
	stats["totalAlerts"] = totalAlerts

	// Total scripts
	var totalScripts int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM scripts").Scan(&totalScripts); err != nil {
		log.Printf("Error getting total scripts count: %v", err)
	}
	stats["totalScripts"] = totalScripts

	// Total users
	var totalUsers int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE is_active = true").Scan(&totalUsers); err != nil {
		log.Printf("Error getting total users count: %v", err)
	}
	stats["totalUsers"] = totalUsers

	c.JSON(http.StatusOK, stats)
}
