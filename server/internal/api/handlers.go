package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
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
		AgentID string `json:"agentId"`
		Token   string `json:"token"`
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
		_, insertErr := r.db.Pool().Exec(ctx, `
			INSERT INTO devices (id, agent_id, hostname, status, created_at, last_seen)
			VALUES ($1, $2, $3, 'online', NOW(), NOW())
		`, deviceID, authPayload.AgentID, "Auto-enrolled-"+authPayload.AgentID[:8])
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
	r.db.Pool().Exec(ctx, "UPDATE devices SET status = 'online', last_seen = NOW() WHERE id = $1", deviceID)

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
	r.db.Pool().Exec(context.Background(), "UPDATE devices SET status = 'offline' WHERE id = $1", deviceID)

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
			r.db.Pool().Exec(ctx, "UPDATE devices SET last_seen = NOW(), agent_version = $1 WHERE id = $2", heartbeat.AgentVersion, deviceID)
		} else {
			r.db.Pool().Exec(ctx, "UPDATE devices SET last_seen = NOW() WHERE id = $1", deviceID)
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
		r.db.Pool().Exec(ctx, `
			INSERT INTO device_metrics (device_id, cpu_percent, memory_percent, memory_used_bytes,
				memory_total_bytes, disk_percent, disk_used_bytes, disk_total_bytes,
				network_rx_bytes, network_tx_bytes, process_count)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, deviceID, metrics.CPUPercent, metrics.MemoryPercent, metrics.MemoryUsedBytes,
			metrics.MemoryTotalBytes, metrics.DiskPercent, metrics.DiskUsedBytes,
			metrics.DiskTotalBytes, metrics.NetworkRxBytes, metrics.NetworkTxBytes,
			metrics.ProcessCount)

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
		var response struct {
			RequestID string          `json:"requestId"`
			Success   bool            `json:"success"`
			Data      json.RawMessage `json:"data"`
			Error     string          `json:"error"`
		}
		if err := json.Unmarshal(msg.Payload, &response); err != nil {
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

			r.db.Pool().Exec(ctx, `
				UPDATE commands SET status = $1, output = $2, error_message = $3, completed_at = NOW()
				WHERE id = $4
			`, status, data.Output, response.Error, data.CommandID)
		}

		// Broadcast to dashboards
		r.hub.BroadcastToDashboards(message)

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
			r.db.Pool().QueryRow(ctx, `
				SELECT COUNT(*) FROM alerts
				WHERE device_id = $1 AND rule_id = $2 AND status != 'resolved'
				AND created_at > NOW() - INTERVAL '15 minutes'
			`, deviceID, rule.ID).Scan(&count)

			if count == 0 {
				r.db.Pool().Exec(ctx, `
					INSERT INTO alerts (device_id, rule_id, severity, title, message)
					VALUES ($1, $2, $3, $4, $5)
				`, deviceID, rule.ID, rule.Severity, rule.Name,
					rule.Metric+" is "+rule.Operator+" "+string(rune(int(rule.Threshold))))
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
	c.JSON(http.StatusCreated, gin.H{})
}

func (r *Router) getScript(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

func (r *Router) updateScript(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

func (r *Router) deleteScript(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

func (r *Router) executeScript(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
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
	c.JSON(http.StatusOK, gin.H{})
}

func (r *Router) acknowledgeAlert(c *gin.Context) {
	id, _ := uuid.Parse(c.Param("id"))
	userID := c.MustGet("userId").(uuid.UUID)
	ctx := context.Background()

	r.db.Pool().Exec(ctx, `
		UPDATE alerts SET status = 'acknowledged', acknowledged_by = $1, acknowledged_at = NOW()
		WHERE id = $2
	`, userID, id)

	c.JSON(http.StatusOK, gin.H{"message": "Alert acknowledged"})
}

func (r *Router) resolveAlert(c *gin.Context) {
	id, _ := uuid.Parse(c.Param("id"))
	ctx := context.Background()

	r.db.Pool().Exec(ctx, "UPDATE alerts SET status = 'resolved', resolved_at = NOW() WHERE id = $1", id)

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
	c.JSON(http.StatusCreated, gin.H{})
}

func (r *Router) getAlertRule(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

func (r *Router) updateAlertRule(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

func (r *Router) deleteAlertRule(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
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
			continue
		}
		settings[key] = value
	}

	c.JSON(http.StatusOK, settings)
}

func (r *Router) updateSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
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
		updates = append(updates, "email = $"+string(rune('0'+argNum)))
		args = append(args, req.Email)
		argNum++
	}
	if req.FirstName != "" {
		updates = append(updates, "first_name = $"+string(rune('0'+argNum)))
		args = append(args, req.FirstName)
		argNum++
	}
	if req.LastName != "" {
		updates = append(updates, "last_name = $"+string(rune('0'+argNum)))
		args = append(args, req.LastName)
		argNum++
	}
	if req.Role != "" {
		updates = append(updates, "role = $"+string(rune('0'+argNum)))
		args = append(args, req.Role)
		argNum++
	}
	if req.Password != "" {
		hashedPassword, err := hashPassword(req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}
		updates = append(updates, "password_hash = $"+string(rune('0'+argNum)))
		args = append(args, hashedPassword)
		argNum++
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No fields to update"})
		return
	}

	args = append(args, id)
	query := "UPDATE users SET " + strings.Join(updates, ", ") + ", updated_at = NOW() WHERE id = $" + string(rune('0'+argNum))

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
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices").Scan(&totalDevices)
	stats["totalDevices"] = totalDevices

	// Online devices
	var onlineDevices int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices WHERE status = 'online'").Scan(&onlineDevices)
	stats["onlineDevices"] = onlineDevices

	// Offline devices
	var offlineDevices int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices WHERE status = 'offline'").Scan(&offlineDevices)
	stats["offlineDevices"] = offlineDevices

	// Critical alerts
	var criticalAlerts int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE severity = 'critical' AND status = 'open'").Scan(&criticalAlerts)
	stats["criticalAlerts"] = criticalAlerts

	// Warning alerts
	var warningAlerts int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE severity = 'warning' AND status = 'open'").Scan(&warningAlerts)
	stats["warningAlerts"] = warningAlerts

	// Total alerts
	var totalAlerts int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE status = 'open'").Scan(&totalAlerts)
	stats["totalAlerts"] = totalAlerts

	// Total scripts
	var totalScripts int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM scripts").Scan(&totalScripts)
	stats["totalScripts"] = totalScripts

	// Total users
	var totalUsers int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE is_active = true").Scan(&totalUsers)
	stats["totalUsers"] = totalUsers

	c.JSON(http.StatusOK, stats)
}
