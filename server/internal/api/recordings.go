package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/constants"
)

// Recording represents a performance metrics recording session
type Recording struct {
	ID              uuid.UUID  `json:"id"`
	DeviceID        uuid.UUID  `json:"deviceId"`
	OrganizationID  int        `json:"organizationId"`
	Name            *string    `json:"name"`
	Description     *string    `json:"description"`
	Status          string     `json:"status"`
	StartedAt       time.Time  `json:"startedAt"`
	EndedAt         *time.Time `json:"endedAt"`
	DurationSeconds *int       `json:"durationSeconds"`
	MetricsCount    int        `json:"metricsCount"`
	InitiatedBy     *uuid.UUID `json:"initiatedBy"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	// Joined fields
	DeviceHostname    string `json:"deviceHostname,omitempty"`
	DeviceDisplayName string `json:"deviceDisplayName,omitempty"`
	InitiatedByEmail  string `json:"initiatedByEmail,omitempty"`
}

// RecordingMetric represents a single metric data point in a recording
type RecordingMetric struct {
	ID               int64      `json:"id"`
	RecordingID      uuid.UUID  `json:"recordingId"`
	Timestamp        time.Time  `json:"timestamp"`
	CPUPercent       *float32   `json:"cpuPercent"`
	MemoryPercent    *float32   `json:"memoryPercent"`
	MemoryUsedBytes  *int64     `json:"memoryUsedBytes"`
	MemoryTotalBytes *int64     `json:"memoryTotalBytes"`
	DiskPercent      *float32   `json:"diskPercent"`
	DiskUsedBytes    *int64     `json:"diskUsedBytes"`
	DiskTotalBytes   *int64     `json:"diskTotalBytes"`
	NetworkRxBytes   *int64     `json:"networkRxBytes"`
	NetworkTxBytes   *int64     `json:"networkTxBytes"`
	NetworkRxRate    *int64     `json:"networkRxRate"`
	NetworkTxRate    *int64     `json:"networkTxRate"`
	ProcessCount     *int       `json:"processCount"`
}

// RecordingSummary includes aggregated statistics
type RecordingSummary struct {
	Recording
	AvgCPUPercent    *float32 `json:"avgCpuPercent"`
	MaxCPUPercent    *float32 `json:"maxCpuPercent"`
	AvgMemoryPercent *float32 `json:"avgMemoryPercent"`
	MaxMemoryPercent *float32 `json:"maxMemoryPercent"`
	TotalNetworkRx   *int64   `json:"totalNetworkRx"`
	TotalNetworkTx   *int64   `json:"totalNetworkTx"`
}

// StartRecordingRequest is the request body for starting a recording
type StartRecordingRequest struct {
	DeviceID    uuid.UUID `json:"deviceId" binding:"required"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}

// UpdateRecordingRequest is the request body for updating a recording
type UpdateRecordingRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// listRecordingsHandler returns all recordings with optional device filter
func listRecordingsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		// Parse pagination
		page := 1
		pageSize := 50
		const maxPageSize = 200

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

		offset := (page - 1) * pageSize

		// Optional device filter
		var deviceID *uuid.UUID
		if did := c.Query("deviceId"); did != "" {
			if parsed, err := uuid.Parse(did); err == nil {
				deviceID = &parsed
			}
		}

		// Optional status filter
		status := c.Query("status")

		// Build query
		baseQuery := `
			FROM recordings r
			LEFT JOIN devices d ON r.device_id = d.id
			LEFT JOIN users u ON r.initiated_by = u.id
			WHERE r.organization_id = $1
		`
		args := []interface{}{constants.CurrentOrganizationID}
		argNum := 2

		if deviceID != nil {
			baseQuery += fmt.Sprintf(" AND r.device_id = $%d", argNum)
			args = append(args, *deviceID)
			argNum++
		}

		if status != "" {
			baseQuery += fmt.Sprintf(" AND r.status = $%d", argNum)
			args = append(args, status)
			argNum++
		}

		// Get total count
		var total int
		countQuery := "SELECT COUNT(*) " + baseQuery
		err := services.DB.Pool().QueryRow(ctx, countQuery, args...).Scan(&total)
		if err != nil {
			log.Printf("[Recordings] Error counting recordings: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count recordings"})
			return
		}

		totalPages := (total + pageSize - 1) / pageSize
		if totalPages < 1 {
			totalPages = 1
		}

		// Query recordings
		selectQuery := `
			SELECT r.id, r.device_id, r.organization_id, r.name, r.description, r.status,
			       r.started_at, r.ended_at, r.duration_seconds, r.metrics_count,
			       r.initiated_by, r.created_at, r.updated_at,
			       COALESCE(d.hostname, '') as device_hostname,
			       COALESCE(d.display_name, '') as device_display_name,
			       COALESCE(u.email, '') as initiated_by_email
		` + baseQuery + fmt.Sprintf(" ORDER BY r.started_at DESC LIMIT $%d OFFSET $%d", argNum, argNum+1)
		args = append(args, pageSize, offset)

		rows, err := services.DB.Pool().Query(ctx, selectQuery, args...)
		if err != nil {
			log.Printf("[Recordings] Error listing recordings: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list recordings"})
			return
		}
		defer rows.Close()

		recordings := make([]Recording, 0)
		for rows.Next() {
			var r Recording
			err := rows.Scan(
				&r.ID, &r.DeviceID, &r.OrganizationID, &r.Name, &r.Description, &r.Status,
				&r.StartedAt, &r.EndedAt, &r.DurationSeconds, &r.MetricsCount,
				&r.InitiatedBy, &r.CreatedAt, &r.UpdatedAt,
				&r.DeviceHostname, &r.DeviceDisplayName, &r.InitiatedByEmail,
			)
			if err != nil {
				log.Printf("[Recordings] Error scanning recording row: %v", err)
				continue
			}
			recordings = append(recordings, r)
		}

		c.JSON(http.StatusOK, gin.H{
			"recordings": recordings,
			"total":      total,
			"page":       page,
			"pageSize":   pageSize,
			"totalPages": totalPages,
		})
	}
}

// getRecordingHandler returns a single recording with its metrics
func getRecordingHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recording ID"})
			return
		}

		ctx := context.Background()

		// Get recording with summary stats
		var r RecordingSummary
		err = services.DB.Pool().QueryRow(ctx, `
			SELECT r.id, r.device_id, r.organization_id, r.name, r.description, r.status,
			       r.started_at, r.ended_at, r.duration_seconds, r.metrics_count,
			       r.initiated_by, r.created_at, r.updated_at,
			       COALESCE(d.hostname, '') as device_hostname,
			       COALESCE(d.display_name, '') as device_display_name,
			       COALESCE(u.email, '') as initiated_by_email,
			       ROUND(AVG(rm.cpu_percent)::numeric, 2)::REAL as avg_cpu,
			       MAX(rm.cpu_percent) as max_cpu,
			       ROUND(AVG(rm.memory_percent)::numeric, 2)::REAL as avg_memory,
			       MAX(rm.memory_percent) as max_memory,
			       MAX(rm.network_rx_bytes) - MIN(rm.network_rx_bytes) as total_rx,
			       MAX(rm.network_tx_bytes) - MIN(rm.network_tx_bytes) as total_tx
			FROM recordings r
			LEFT JOIN devices d ON r.device_id = d.id
			LEFT JOIN users u ON r.initiated_by = u.id
			LEFT JOIN recording_metrics rm ON r.id = rm.recording_id
			WHERE r.id = $1 AND r.organization_id = $2
			GROUP BY r.id, r.device_id, r.organization_id, r.name, r.description, r.status,
			         r.started_at, r.ended_at, r.duration_seconds, r.metrics_count,
			         r.initiated_by, r.created_at, r.updated_at,
			         d.hostname, d.display_name, u.email
		`, id, constants.CurrentOrganizationID).Scan(
			&r.ID, &r.DeviceID, &r.OrganizationID, &r.Name, &r.Description, &r.Status,
			&r.StartedAt, &r.EndedAt, &r.DurationSeconds, &r.MetricsCount,
			&r.InitiatedBy, &r.CreatedAt, &r.UpdatedAt,
			&r.DeviceHostname, &r.DeviceDisplayName, &r.InitiatedByEmail,
			&r.AvgCPUPercent, &r.MaxCPUPercent, &r.AvgMemoryPercent, &r.MaxMemoryPercent,
			&r.TotalNetworkRx, &r.TotalNetworkTx,
		)

		if err != nil {
			log.Printf("[Recordings] Error getting recording %s: %v", id, err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Recording not found"})
			return
		}

		c.JSON(http.StatusOK, r)
	}
}

// getRecordingMetricsHandler returns the metrics for a recording
func getRecordingMetricsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recording ID"})
			return
		}

		ctx := context.Background()

		// Verify recording exists and belongs to org
		var exists bool
		err = services.DB.Pool().QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM recordings WHERE id = $1 AND organization_id = $2)",
			id, constants.CurrentOrganizationID,
		).Scan(&exists)
		if err != nil || !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Recording not found"})
			return
		}

		// Get metrics
		rows, err := services.DB.Pool().Query(ctx, `
			SELECT id, recording_id, timestamp, cpu_percent, memory_percent,
			       memory_used_bytes, memory_total_bytes, disk_percent,
			       disk_used_bytes, disk_total_bytes, network_rx_bytes,
			       network_tx_bytes, network_rx_rate, network_tx_rate, process_count
			FROM recording_metrics
			WHERE recording_id = $1
			ORDER BY timestamp ASC
		`, id)
		if err != nil {
			log.Printf("[Recordings] Error getting metrics for recording %s: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get recording metrics"})
			return
		}
		defer rows.Close()

		metrics := make([]RecordingMetric, 0)
		for rows.Next() {
			var m RecordingMetric
			err := rows.Scan(
				&m.ID, &m.RecordingID, &m.Timestamp, &m.CPUPercent, &m.MemoryPercent,
				&m.MemoryUsedBytes, &m.MemoryTotalBytes, &m.DiskPercent,
				&m.DiskUsedBytes, &m.DiskTotalBytes, &m.NetworkRxBytes,
				&m.NetworkTxBytes, &m.NetworkRxRate, &m.NetworkTxRate, &m.ProcessCount,
			)
			if err != nil {
				log.Printf("[Recordings] Error scanning metric row: %v", err)
				continue
			}
			metrics = append(metrics, m)
		}

		c.JSON(http.StatusOK, gin.H{"metrics": metrics})
	}
}

// startRecordingHandler starts a new recording session
func startRecordingHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req StartRecordingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		ctx := context.Background()

		// Get user ID from context
		userID, _ := c.Get("userID")
		var initiatedBy *uuid.UUID
		if uid, ok := userID.(uuid.UUID); ok {
			initiatedBy = &uid
		}

		// Verify device exists and get agent_id
		var agentID string
		err := services.DB.Pool().QueryRow(ctx,
			"SELECT agent_id FROM devices WHERE id = $1 AND organization_id = $2",
			req.DeviceID, constants.CurrentOrganizationID,
		).Scan(&agentID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
			return
		}

		// Check if there's already an active recording for this device
		var existingID uuid.UUID
		err = services.DB.Pool().QueryRow(ctx,
			"SELECT id FROM recordings WHERE device_id = $1 AND status = 'recording'",
			req.DeviceID,
		).Scan(&existingID)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{
				"error":       "Device already has an active recording",
				"recordingId": existingID,
			})
			return
		}

		// Create recording record
		var recordingID uuid.UUID
		var name, description *string
		if req.Name != "" {
			name = &req.Name
		}
		if req.Description != "" {
			description = &req.Description
		}

		err = services.DB.Pool().QueryRow(ctx, `
			INSERT INTO recordings (device_id, organization_id, name, description, status, started_at, initiated_by)
			VALUES ($1, $2, $3, $4, 'recording', NOW(), $5)
			RETURNING id
		`, req.DeviceID, constants.CurrentOrganizationID, name, description, initiatedBy).Scan(&recordingID)

		if err != nil {
			log.Printf("[Recordings] Error creating recording: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create recording"})
			return
		}

		// Start metrics recording in gRPC server with the recording ID
		if services.MetricsRecorder != nil {
			services.MetricsRecorder.SetRecordingID(agentID, recordingID, req.DeviceID)
		}

		// Store recording ID in cache for the agent (backup for reconnection scenarios)
		cacheKey := fmt.Sprintf("recording:active:%s", agentID)
		if services.Redis != nil {
			services.Redis.Set(ctx, cacheKey, recordingID.String(), 24*time.Hour)
		}

		log.Printf("[Recordings] Started recording %s for device %s (agent: %s)", recordingID, req.DeviceID, agentID)

		c.JSON(http.StatusCreated, gin.H{
			"id":        recordingID,
			"deviceId":  req.DeviceID,
			"status":    "recording",
			"startedAt": time.Now(),
		})
	}
}

// stopRecordingHandler stops an active recording
func stopRecordingHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recording ID"})
			return
		}

		ctx := context.Background()

		// Get recording and verify it's active
		var deviceID uuid.UUID
		var agentID string
		var status string
		err = services.DB.Pool().QueryRow(ctx, `
			SELECT r.device_id, r.status, d.agent_id
			FROM recordings r
			JOIN devices d ON r.device_id = d.id
			WHERE r.id = $1 AND r.organization_id = $2
		`, id, constants.CurrentOrganizationID).Scan(&deviceID, &status, &agentID)

		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Recording not found"})
			return
		}

		if status != "recording" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Recording is not active"})
			return
		}

		// Stop metrics recording in gRPC server
		if services.MetricsRecorder != nil {
			services.MetricsRecorder.StopRecording(agentID)
		}

		// Clear recording ID from cache
		cacheKey := fmt.Sprintf("recording:active:%s", agentID)
		if services.Redis != nil {
			services.Redis.Del(ctx, cacheKey)
		}

		// Update recording status
		_, err = services.DB.Pool().Exec(ctx, `
			UPDATE recordings
			SET status = 'completed', ended_at = NOW(), updated_at = NOW()
			WHERE id = $1
		`, id)

		if err != nil {
			log.Printf("[Recordings] Error stopping recording %s: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to stop recording"})
			return
		}

		// Get updated recording
		var r Recording
		err = services.DB.Pool().QueryRow(ctx, `
			SELECT id, device_id, organization_id, name, description, status,
			       started_at, ended_at, duration_seconds, metrics_count,
			       initiated_by, created_at, updated_at
			FROM recordings WHERE id = $1
		`, id).Scan(
			&r.ID, &r.DeviceID, &r.OrganizationID, &r.Name, &r.Description, &r.Status,
			&r.StartedAt, &r.EndedAt, &r.DurationSeconds, &r.MetricsCount,
			&r.InitiatedBy, &r.CreatedAt, &r.UpdatedAt,
		)

		if err != nil {
			log.Printf("[Recordings] Error fetching stopped recording: %v", err)
		}

		log.Printf("[Recordings] Stopped recording %s (device: %s, metrics: %d)", id, deviceID, r.MetricsCount)

		c.JSON(http.StatusOK, r)
	}
}

// updateRecordingHandler updates recording metadata (name, description)
func updateRecordingHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recording ID"})
			return
		}

		var req UpdateRecordingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		ctx := context.Background()

		// Verify recording exists
		var exists bool
		err = services.DB.Pool().QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM recordings WHERE id = $1 AND organization_id = $2)",
			id, constants.CurrentOrganizationID,
		).Scan(&exists)
		if err != nil || !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Recording not found"})
			return
		}

		// Build update query
		updates := []string{}
		args := []interface{}{}
		argNum := 1

		if req.Name != nil {
			updates = append(updates, fmt.Sprintf("name = $%d", argNum))
			args = append(args, *req.Name)
			argNum++
		}
		if req.Description != nil {
			updates = append(updates, fmt.Sprintf("description = $%d", argNum))
			args = append(args, *req.Description)
			argNum++
		}

		if len(updates) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No fields to update"})
			return
		}

		query := fmt.Sprintf("UPDATE recordings SET %s, updated_at = NOW() WHERE id = $%d",
			joinStrings(updates, ", "), argNum)
		args = append(args, id)

		_, err = services.DB.Pool().Exec(ctx, query, args...)
		if err != nil {
			log.Printf("[Recordings] Error updating recording %s: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update recording"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Recording updated"})
	}
}

// deleteRecordingHandler deletes a recording and its metrics
func deleteRecordingHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recording ID"})
			return
		}

		ctx := context.Background()

		// Verify recording exists and is not active
		var status string
		err = services.DB.Pool().QueryRow(ctx,
			"SELECT status FROM recordings WHERE id = $1 AND organization_id = $2",
			id, constants.CurrentOrganizationID,
		).Scan(&status)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Recording not found"})
			return
		}

		if status == "recording" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot delete an active recording. Stop it first."})
			return
		}

		// Delete recording (metrics are deleted via CASCADE)
		_, err = services.DB.Pool().Exec(ctx,
			"DELETE FROM recordings WHERE id = $1",
			id,
		)
		if err != nil {
			log.Printf("[Recordings] Error deleting recording %s: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete recording"})
			return
		}

		log.Printf("[Recordings] Deleted recording %s", id)
		c.JSON(http.StatusOK, gin.H{"message": "Recording deleted"})
	}
}

// exportRecordingCSVHandler exports recording metrics as CSV
func exportRecordingCSVHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recording ID"})
			return
		}

		ctx := context.Background()

		// Get recording info
		var name *string
		var deviceHostname string
		err = services.DB.Pool().QueryRow(ctx, `
			SELECT r.name, COALESCE(d.hostname, 'unknown')
			FROM recordings r
			LEFT JOIN devices d ON r.device_id = d.id
			WHERE r.id = $1 AND r.organization_id = $2
		`, id, constants.CurrentOrganizationID).Scan(&name, &deviceHostname)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Recording not found"})
			return
		}

		// Get metrics
		rows, err := services.DB.Pool().Query(ctx, `
			SELECT timestamp, cpu_percent, memory_percent, memory_used_bytes,
			       disk_percent, disk_used_bytes, network_rx_bytes, network_tx_bytes,
			       process_count
			FROM recording_metrics
			WHERE recording_id = $1
			ORDER BY timestamp ASC
		`, id)
		if err != nil {
			log.Printf("[Recordings] Error exporting recording %s: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to export recording"})
			return
		}
		defer rows.Close()

		// Generate filename
		filename := "recording"
		if name != nil && *name != "" {
			filename = *name
		} else {
			filename = fmt.Sprintf("%s-%s", deviceHostname, id.String()[:8])
		}

		// Set headers for CSV download
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.csv\"", filename))

		// Write CSV
		writer := csv.NewWriter(c.Writer)
		defer writer.Flush()

		// Header row
		writer.Write([]string{
			"Timestamp", "CPU %", "Memory %", "Memory Used (bytes)",
			"Disk %", "Disk Used (bytes)", "Network RX (bytes)", "Network TX (bytes)",
			"Process Count",
		})

		// Data rows
		for rows.Next() {
			var ts time.Time
			var cpu, memory, disk *float32
			var memUsed, diskUsed, netRx, netTx *int64
			var procCount *int

			if err := rows.Scan(&ts, &cpu, &memory, &memUsed, &disk, &diskUsed, &netRx, &netTx, &procCount); err != nil {
				continue
			}

			row := []string{
				ts.Format(time.RFC3339),
				formatFloat32(cpu),
				formatFloat32(memory),
				formatInt64(memUsed),
				formatFloat32(disk),
				formatInt64(diskUsed),
				formatInt64(netRx),
				formatInt64(netTx),
				formatInt(procCount),
			}
			writer.Write(row)
		}
	}
}

// exportRecordingJSONHandler exports recording metrics as JSON
func exportRecordingJSONHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recording ID"})
			return
		}

		ctx := context.Background()

		// Get recording with metrics
		var r Recording
		err = services.DB.Pool().QueryRow(ctx, `
			SELECT r.id, r.device_id, r.organization_id, r.name, r.description, r.status,
			       r.started_at, r.ended_at, r.duration_seconds, r.metrics_count,
			       r.initiated_by, r.created_at, r.updated_at,
			       COALESCE(d.hostname, '') as device_hostname,
			       COALESCE(d.display_name, '') as device_display_name
			FROM recordings r
			LEFT JOIN devices d ON r.device_id = d.id
			WHERE r.id = $1 AND r.organization_id = $2
		`, id, constants.CurrentOrganizationID).Scan(
			&r.ID, &r.DeviceID, &r.OrganizationID, &r.Name, &r.Description, &r.Status,
			&r.StartedAt, &r.EndedAt, &r.DurationSeconds, &r.MetricsCount,
			&r.InitiatedBy, &r.CreatedAt, &r.UpdatedAt,
			&r.DeviceHostname, &r.DeviceDisplayName,
		)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Recording not found"})
			return
		}

		// Get metrics
		rows, err := services.DB.Pool().Query(ctx, `
			SELECT timestamp, cpu_percent, memory_percent, memory_used_bytes, memory_total_bytes,
			       disk_percent, disk_used_bytes, disk_total_bytes,
			       network_rx_bytes, network_tx_bytes, network_rx_rate, network_tx_rate,
			       process_count
			FROM recording_metrics
			WHERE recording_id = $1
			ORDER BY timestamp ASC
		`, id)
		if err != nil {
			log.Printf("[Recordings] Error exporting recording %s: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to export recording"})
			return
		}
		defer rows.Close()

		metrics := make([]map[string]interface{}, 0)
		for rows.Next() {
			var ts time.Time
			var cpu, memory, disk *float32
			var memUsed, memTotal, diskUsed, diskTotal, netRx, netTx, rxRate, txRate *int64
			var procCount *int

			if err := rows.Scan(&ts, &cpu, &memory, &memUsed, &memTotal,
				&disk, &diskUsed, &diskTotal, &netRx, &netTx, &rxRate, &txRate, &procCount); err != nil {
				continue
			}

			m := map[string]interface{}{
				"timestamp":        ts,
				"cpuPercent":       cpu,
				"memoryPercent":    memory,
				"memoryUsedBytes":  memUsed,
				"memoryTotalBytes": memTotal,
				"diskPercent":      disk,
				"diskUsedBytes":    diskUsed,
				"diskTotalBytes":   diskTotal,
				"networkRxBytes":   netRx,
				"networkTxBytes":   netTx,
				"networkRxRate":    rxRate,
				"networkTxRate":    txRate,
				"processCount":     procCount,
			}
			metrics = append(metrics, m)
		}

		// Generate filename
		filename := "recording"
		if r.Name != nil && *r.Name != "" {
			filename = *r.Name
		} else {
			filename = fmt.Sprintf("%s-%s", r.DeviceHostname, id.String()[:8])
		}

		// Set headers for JSON download
		c.Header("Content-Type", "application/json")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.json\"", filename))

		// Write JSON
		export := map[string]interface{}{
			"recording": r,
			"metrics":   metrics,
			"exportedAt": time.Now(),
		}

		encoder := json.NewEncoder(c.Writer)
		encoder.SetIndent("", "  ")
		encoder.Encode(export)
	}
}

// getActiveRecordingHandler returns the active recording for a device (if any)
func getActiveRecordingHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		deviceID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
			return
		}

		ctx := context.Background()

		var r Recording
		err = services.DB.Pool().QueryRow(ctx, `
			SELECT id, device_id, organization_id, name, description, status,
			       started_at, ended_at, duration_seconds, metrics_count,
			       initiated_by, created_at, updated_at
			FROM recordings
			WHERE device_id = $1 AND organization_id = $2 AND status = 'recording'
		`, deviceID, constants.CurrentOrganizationID).Scan(
			&r.ID, &r.DeviceID, &r.OrganizationID, &r.Name, &r.Description, &r.Status,
			&r.StartedAt, &r.EndedAt, &r.DurationSeconds, &r.MetricsCount,
			&r.InitiatedBy, &r.CreatedAt, &r.UpdatedAt,
		)

		if err != nil {
			c.JSON(http.StatusOK, gin.H{"recording": nil})
			return
		}

		c.JSON(http.StatusOK, gin.H{"recording": r})
	}
}

// Helper functions
func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

func formatFloat32(v *float32) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", *v)
}

func formatInt64(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

func formatInt(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}
