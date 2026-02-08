package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/constants"
)

// ScriptSchedule represents a scheduled script execution
type ScriptSchedule struct {
	ID              uuid.UUID   `json:"id"`
	OrganizationID  uuid.UUID   `json:"organizationId"`
	ScriptID        uuid.UUID   `json:"scriptId"`
	ScriptName      string      `json:"scriptName,omitempty"`
	Name            string      `json:"name"`
	Description     string      `json:"description"`
	ScheduleType    string      `json:"scheduleType"`
	CronExpression  string      `json:"cronExpression,omitempty"`
	IntervalMinutes *int        `json:"intervalMinutes,omitempty"`
	RunAt           *time.Time  `json:"runAt,omitempty"`
	Timezone        string      `json:"timezone"`
	TargetType      string      `json:"targetType"`
	TargetDeviceIDs []uuid.UUID `json:"targetDeviceIds,omitempty"`
	TargetGroupID   *uuid.UUID  `json:"targetGroupId,omitempty"`
	TimeoutSeconds  int         `json:"timeoutSeconds"`
	RunAsSystem     bool        `json:"runAsSystem"`
	StopOnError     bool        `json:"stopOnError"`
	IsEnabled       bool        `json:"isEnabled"`
	LastRunAt       *time.Time  `json:"lastRunAt,omitempty"`
	NextRunAt       *time.Time  `json:"nextRunAt,omitempty"`
	LastRunStatus   string      `json:"lastRunStatus,omitempty"`
	CreatedAt       time.Time   `json:"createdAt"`
	UpdatedAt       time.Time   `json:"updatedAt"`
}

// CreateScheduleRequest is the request body for creating a schedule
type CreateScheduleRequest struct {
	ScriptID        uuid.UUID   `json:"scriptId" binding:"required"`
	Name            string      `json:"name" binding:"required"`
	Description     string      `json:"description"`
	ScheduleType    string      `json:"scheduleType" binding:"required,oneof=cron once interval"`
	CronExpression  string      `json:"cronExpression"`
	IntervalMinutes *int        `json:"intervalMinutes"`
	RunAt           *time.Time  `json:"runAt"`
	Timezone        string      `json:"timezone"`
	TargetType      string      `json:"targetType" binding:"required,oneof=all group specific"`
	TargetDeviceIDs []uuid.UUID `json:"targetDeviceIds"`
	TargetGroupID   *uuid.UUID  `json:"targetGroupId"`
	TimeoutSeconds  int         `json:"timeoutSeconds"`
	RunAsSystem     bool        `json:"runAsSystem"`
	StopOnError     bool        `json:"stopOnError"`
	IsEnabled       bool        `json:"isEnabled"`
}

// ScriptExecution represents a script execution record
type ScriptExecution struct {
	ID          uuid.UUID  `json:"id"`
	ScheduleID  *uuid.UUID `json:"scheduleId,omitempty"`
	ScriptID    uuid.UUID  `json:"scriptId"`
	ScriptName  string     `json:"scriptName,omitempty"`
	DeviceID    *uuid.UUID `json:"deviceId,omitempty"`
	Hostname    string     `json:"hostname,omitempty"`
	Status      string     `json:"status"`
	ExitCode    *int       `json:"exitCode,omitempty"`
	Output      string     `json:"output,omitempty"`
	Error       string     `json:"error,omitempty"`
	DurationMs  *int       `json:"durationMs,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

func listSchedulesHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		rows, err := services.DB.Pool().Query(ctx, `
			SELECT ss.id, ss.organization_id, ss.script_id, s.name as script_name,
			       ss.name, COALESCE(ss.description, ''), ss.schedule_type,
			       COALESCE(ss.cron_expression, ''), ss.interval_minutes, ss.run_at,
			       ss.timezone, ss.target_type, COALESCE(ss.target_device_ids, '{}'),
			       ss.target_group_id, ss.timeout_seconds, ss.run_as_system,
			       ss.stop_on_error, ss.is_enabled, ss.last_run_at, ss.next_run_at,
			       COALESCE(ss.last_run_status, ''), ss.created_at, ss.updated_at
			FROM script_schedules ss
			JOIN scripts s ON ss.script_id = s.id
			WHERE ss.organization_id = $1
			ORDER BY ss.created_at DESC
		`, constants.CurrentOrganizationID)
		if err != nil {
			log.Printf("[Schedules] Error listing schedules: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list schedules"})
			return
		}
		defer rows.Close()

		schedules := []ScriptSchedule{}
		for rows.Next() {
			var s ScriptSchedule
			err := rows.Scan(
				&s.ID, &s.OrganizationID, &s.ScriptID, &s.ScriptName,
				&s.Name, &s.Description, &s.ScheduleType,
				&s.CronExpression, &s.IntervalMinutes, &s.RunAt,
				&s.Timezone, &s.TargetType, &s.TargetDeviceIDs,
				&s.TargetGroupID, &s.TimeoutSeconds, &s.RunAsSystem,
				&s.StopOnError, &s.IsEnabled, &s.LastRunAt, &s.NextRunAt,
				&s.LastRunStatus, &s.CreatedAt, &s.UpdatedAt,
			)
			if err != nil {
				log.Printf("[Schedules] Error scanning: %v", err)
				continue
			}
			schedules = append(schedules, s)
		}

		c.JSON(http.StatusOK, schedules)
	}
}

func createScheduleHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateScheduleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
			return
		}

		// Validate based on schedule type
		switch req.ScheduleType {
		case "cron":
			if req.CronExpression == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Cron expression required for cron schedules"})
				return
			}
		case "interval":
			if req.IntervalMinutes == nil || *req.IntervalMinutes < 1 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Interval minutes required for interval schedules"})
				return
			}
		case "once":
			if req.RunAt == nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Run time required for one-time schedules"})
				return
			}
		}

		ctx := context.Background()
		userID := c.MustGet("userId").(uuid.UUID)
		scheduleID := uuid.New()

		if req.Timezone == "" {
			req.Timezone = "UTC"
		}
		if req.TimeoutSeconds == 0 {
			req.TimeoutSeconds = 300
		}

		// Calculate next run time
		var nextRunAt *time.Time
		now := time.Now()
		switch req.ScheduleType {
		case "once":
			nextRunAt = req.RunAt
		case "interval":
			next := now.Add(time.Duration(*req.IntervalMinutes) * time.Minute)
			nextRunAt = &next
		case "cron":
			// Would need a cron parser here - for now just set to nil
			// In production, use github.com/robfig/cron for proper parsing
		}

		_, err := services.DB.Pool().Exec(ctx, `
			INSERT INTO script_schedules (
				id, organization_id, script_id, name, description,
				schedule_type, cron_expression, interval_minutes, run_at,
				timezone, target_type, target_device_ids, target_group_id,
				timeout_seconds, run_as_system, stop_on_error, is_enabled,
				next_run_at, created_by
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
				$14, $15, $16, $17, $18, $19
			)
		`, scheduleID, constants.CurrentOrganizationID, req.ScriptID, req.Name, req.Description,
			req.ScheduleType, nullIfEmpty(req.CronExpression), req.IntervalMinutes, req.RunAt,
			req.Timezone, req.TargetType, req.TargetDeviceIDs, req.TargetGroupID,
			req.TimeoutSeconds, req.RunAsSystem, req.StopOnError, req.IsEnabled,
			nextRunAt, userID)

		if err != nil {
			log.Printf("[Schedules] Error creating schedule: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create schedule"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"id":        scheduleID,
			"message":   "Schedule created successfully",
			"nextRunAt": nextRunAt,
		})
	}
}

func getScheduleHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid schedule ID"})
			return
		}

		ctx := context.Background()
		var s ScriptSchedule

		err = services.DB.Pool().QueryRow(ctx, `
			SELECT ss.id, ss.organization_id, ss.script_id, sc.name as script_name,
			       ss.name, COALESCE(ss.description, ''), ss.schedule_type,
			       COALESCE(ss.cron_expression, ''), ss.interval_minutes, ss.run_at,
			       ss.timezone, ss.target_type, COALESCE(ss.target_device_ids, '{}'),
			       ss.target_group_id, ss.timeout_seconds, ss.run_as_system,
			       ss.stop_on_error, ss.is_enabled, ss.last_run_at, ss.next_run_at,
			       COALESCE(ss.last_run_status, ''), ss.created_at, ss.updated_at
			FROM script_schedules ss
			JOIN scripts sc ON ss.script_id = sc.id
			WHERE ss.id = $1 AND ss.organization_id = $2
		`, id, constants.CurrentOrganizationID).Scan(
			&s.ID, &s.OrganizationID, &s.ScriptID, &s.ScriptName,
			&s.Name, &s.Description, &s.ScheduleType,
			&s.CronExpression, &s.IntervalMinutes, &s.RunAt,
			&s.Timezone, &s.TargetType, &s.TargetDeviceIDs,
			&s.TargetGroupID, &s.TimeoutSeconds, &s.RunAsSystem,
			&s.StopOnError, &s.IsEnabled, &s.LastRunAt, &s.NextRunAt,
			&s.LastRunStatus, &s.CreatedAt, &s.UpdatedAt,
		)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Schedule not found"})
			return
		}

		c.JSON(http.StatusOK, s)
	}
}

func updateScheduleHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid schedule ID"})
			return
		}

		var req CreateScheduleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		ctx := context.Background()

		result, err := services.DB.Pool().Exec(ctx, `
			UPDATE script_schedules SET
				script_id = $3, name = $4, description = $5,
				schedule_type = $6, cron_expression = $7, interval_minutes = $8,
				run_at = $9, timezone = $10, target_type = $11,
				target_device_ids = $12, target_group_id = $13,
				timeout_seconds = $14, run_as_system = $15, stop_on_error = $16,
				is_enabled = $17, updated_at = NOW()
			WHERE id = $1 AND organization_id = $2
		`, id, constants.CurrentOrganizationID, req.ScriptID, req.Name, req.Description,
			req.ScheduleType, nullIfEmpty(req.CronExpression), req.IntervalMinutes,
			req.RunAt, req.Timezone, req.TargetType,
			req.TargetDeviceIDs, req.TargetGroupID,
			req.TimeoutSeconds, req.RunAsSystem, req.StopOnError, req.IsEnabled)

		if err != nil {
			log.Printf("[Schedules] Error updating schedule: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update schedule"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Schedule not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Schedule updated successfully"})
	}
}

func deleteScheduleHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid schedule ID"})
			return
		}

		ctx := context.Background()
		result, err := services.DB.Pool().Exec(ctx, `
			DELETE FROM script_schedules WHERE id = $1 AND organization_id = $2
		`, id, constants.CurrentOrganizationID)
		if err != nil {
			log.Printf("[Schedules] Error deleting schedule: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete schedule"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Schedule not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Schedule deleted successfully"})
	}
}

func toggleScheduleHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid schedule ID"})
			return
		}

		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		ctx := context.Background()
		result, err := services.DB.Pool().Exec(ctx, `
			UPDATE script_schedules SET is_enabled = $3, updated_at = NOW()
			WHERE id = $1 AND organization_id = $2
		`, id, constants.CurrentOrganizationID, req.Enabled)
		if err != nil {
			log.Printf("[Schedules] Error toggling schedule: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to toggle schedule"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Schedule not found"})
			return
		}

		status := "disabled"
		if req.Enabled {
			status = "enabled"
		}
		c.JSON(http.StatusOK, gin.H{"message": "Schedule " + status + " successfully"})
	}
}

func runScheduleNowHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid schedule ID"})
			return
		}

		ctx := context.Background()

		// Get schedule details
		var scriptID uuid.UUID
		var targetType string
		var targetDeviceIDs []uuid.UUID
		var targetGroupID *uuid.UUID
		var timeout int

		err = services.DB.Pool().QueryRow(ctx, `
			SELECT script_id, target_type, COALESCE(target_device_ids, '{}'),
			       target_group_id, timeout_seconds
			FROM script_schedules
			WHERE id = $1 AND organization_id = $2
		`, id, constants.CurrentOrganizationID).Scan(
			&scriptID, &targetType, &targetDeviceIDs, &targetGroupID, &timeout,
		)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Schedule not found"})
			return
		}

		// Get target devices based on target type
		var deviceIDs []uuid.UUID
		switch targetType {
		case "all":
			rows, err := services.DB.Pool().Query(ctx, `
				SELECT id FROM devices
				WHERE organization_id = $1 AND is_disabled = false AND status = 'online'
			`, constants.CurrentOrganizationID)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var devID uuid.UUID
					if rows.Scan(&devID) == nil {
						deviceIDs = append(deviceIDs, devID)
					}
				}
			}
		case "specific":
			deviceIDs = targetDeviceIDs
		case "group":
			// Would need to query device groups here
			deviceIDs = targetDeviceIDs
		}

		// Create execution records and trigger script execution
		userID := c.MustGet("userId").(uuid.UUID)
		execIDs := []uuid.UUID{}

		for _, deviceID := range deviceIDs {
			execID := uuid.New()
			services.DB.Pool().Exec(ctx, `
				INSERT INTO script_executions (
					id, schedule_id, script_id, device_id, organization_id,
					status, created_by
				) VALUES ($1, $2, $3, $4, $5, 'pending', $6)
			`, execID, id, scriptID, deviceID, constants.CurrentOrganizationID, userID)
			execIDs = append(execIDs, execID)

			// Here we would trigger the actual script execution via WebSocket
			// For now, just create the records
		}

		// Update last run time
		services.DB.Pool().Exec(ctx, `
			UPDATE script_schedules SET last_run_at = NOW() WHERE id = $1
		`, id)

		c.JSON(http.StatusOK, gin.H{
			"message":      "Schedule triggered successfully",
			"executionIds": execIDs,
			"deviceCount":  len(deviceIDs),
		})
	}
}

func listExecutionsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		// Optional filters
		scheduleID := c.Query("scheduleId")
		scriptID := c.Query("scriptId")
		deviceID := c.Query("deviceId")
		status := c.Query("status")

		query := `
			SELECT se.id, se.schedule_id, se.script_id, s.name as script_name,
			       se.device_id, COALESCE(d.hostname, 'Unknown'),
			       se.status, se.exit_code, COALESCE(se.output, ''),
			       COALESCE(se.error, ''), se.duration_ms,
			       se.started_at, se.completed_at, se.created_at
			FROM script_executions se
			JOIN scripts s ON se.script_id = s.id
			LEFT JOIN devices d ON se.device_id = d.id
			WHERE se.organization_id = $1
		`
		args := []interface{}{constants.CurrentOrganizationID}
		argIdx := 2

		if scheduleID != "" {
			if id, err := uuid.Parse(scheduleID); err == nil {
				query += " AND se.schedule_id = $" + string(rune('0'+argIdx))
				args = append(args, id)
				argIdx++
			}
		}
		if scriptID != "" {
			if id, err := uuid.Parse(scriptID); err == nil {
				query += " AND se.script_id = $" + string(rune('0'+argIdx))
				args = append(args, id)
				argIdx++
			}
		}
		if deviceID != "" {
			if id, err := uuid.Parse(deviceID); err == nil {
				query += " AND se.device_id = $" + string(rune('0'+argIdx))
				args = append(args, id)
				argIdx++
			}
		}
		if status != "" {
			query += " AND se.status = $" + string(rune('0'+argIdx))
			args = append(args, status)
		}

		query += " ORDER BY se.created_at DESC LIMIT 100"

		rows, err := services.DB.Pool().Query(ctx, query, args...)
		if err != nil {
			log.Printf("[Executions] Error listing: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list executions"})
			return
		}
		defer rows.Close()

		executions := []ScriptExecution{}
		for rows.Next() {
			var e ScriptExecution
			err := rows.Scan(
				&e.ID, &e.ScheduleID, &e.ScriptID, &e.ScriptName,
				&e.DeviceID, &e.Hostname,
				&e.Status, &e.ExitCode, &e.Output, &e.Error, &e.DurationMs,
				&e.StartedAt, &e.CompletedAt, &e.CreatedAt,
			)
			if err != nil {
				continue
			}
			executions = append(executions, e)
		}

		c.JSON(http.StatusOK, executions)
	}
}

func getExecutionHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid execution ID"})
			return
		}

		ctx := context.Background()
		var e ScriptExecution

		err = services.DB.Pool().QueryRow(ctx, `
			SELECT se.id, se.schedule_id, se.script_id, s.name as script_name,
			       se.device_id, COALESCE(d.hostname, 'Unknown'),
			       se.status, se.exit_code, COALESCE(se.output, ''),
			       COALESCE(se.error, ''), se.duration_ms,
			       se.started_at, se.completed_at, se.created_at
			FROM script_executions se
			JOIN scripts s ON se.script_id = s.id
			LEFT JOIN devices d ON se.device_id = d.id
			WHERE se.id = $1 AND se.organization_id = $2
		`, id, constants.CurrentOrganizationID).Scan(
			&e.ID, &e.ScheduleID, &e.ScriptID, &e.ScriptName,
			&e.DeviceID, &e.Hostname,
			&e.Status, &e.ExitCode, &e.Output, &e.Error, &e.DurationMs,
			&e.StartedAt, &e.CompletedAt, &e.CreatedAt,
		)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Execution not found"})
			return
		}

		c.JSON(http.StatusOK, e)
	}
}
