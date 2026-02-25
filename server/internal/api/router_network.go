package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/models"
)

// ---- Audit helper ----

// logRouterAudit is a fire-and-forget helper that logs a router audit entry.
// Errors are logged but never propagated to callers.
func logRouterAudit(services *Services, action, description string, targetMAC *string, metadata map[string]interface{}, status string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		metaJSON = []byte("{}")
	}

	_, err = services.DB.Pool().Exec(ctx, `
		INSERT INTO router_audit_log (id, action, description, target_mac, metadata, status)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, uuid.New(), action, description, targetMAC, metaJSON, status)
	if err != nil {
		log.Printf("[RouterAudit] Failed to log audit entry action=%s: %v", action, err)
	}
}

// ---- Scheduled Actions CRUD ----

func listScheduledActionsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		rows, err := services.DB.Pool().Query(ctx, `
			SELECT id, name, action_type, cron_expression, is_active,
			       last_run_at, next_run_at, created_at, updated_at
			FROM router_scheduled_actions
			ORDER BY created_at DESC
		`)
		if err != nil {
			log.Printf("[RouterNetwork] Error listing scheduled actions: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list scheduled actions"})
			return
		}
		defer rows.Close()

		actions := []models.RouterScheduledAction{}
		for rows.Next() {
			var a models.RouterScheduledAction
			if err := rows.Scan(
				&a.ID, &a.Name, &a.ActionType, &a.CronExpression, &a.IsActive,
				&a.LastRunAt, &a.NextRunAt, &a.CreatedAt, &a.UpdatedAt,
			); err != nil {
				log.Printf("[RouterNetwork] Error scanning scheduled action: %v", err)
				continue
			}
			actions = append(actions, a)
		}

		c.JSON(http.StatusOK, actions)
	}
}

type CreateScheduledActionRequest struct {
	Name           string `json:"name" binding:"required"`
	ActionType     string `json:"actionType" binding:"required"`
	CronExpression string `json:"cronExpression" binding:"required"`
	IsActive       bool   `json:"isActive"`
}

func createScheduledActionHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateScheduledActionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
			return
		}

		// Normalize 5-field cron to 6-field (add seconds prefix)
		cronExpr := normalizeCron(req.CronExpression)

		ctx := context.Background()
		id := uuid.New()

		_, err := services.DB.Pool().Exec(ctx, `
			INSERT INTO router_scheduled_actions (id, name, action_type, cron_expression, is_active)
			VALUES ($1, $2, $3, $4, $5)
		`, id, req.Name, req.ActionType, cronExpr, req.IsActive)
		if err != nil {
			log.Printf("[RouterNetwork] Error creating scheduled action: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create scheduled action"})
			return
		}

		logRouterAudit(services, "schedule_created",
			fmt.Sprintf("Created scheduled action: %s (%s)", req.Name, req.ActionType),
			nil, map[string]interface{}{"name": req.Name, "type": req.ActionType, "cron": cronExpr}, "success")

		c.JSON(http.StatusCreated, gin.H{"id": id, "message": "Scheduled action created"})
	}
}

func updateScheduledActionHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid action ID"})
			return
		}

		var req CreateScheduledActionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
			return
		}

		cronExpr := normalizeCron(req.CronExpression)
		ctx := context.Background()

		result, err := services.DB.Pool().Exec(ctx, `
			UPDATE router_scheduled_actions
			SET name = $2, action_type = $3, cron_expression = $4, is_active = $5, updated_at = NOW()
			WHERE id = $1
		`, id, req.Name, req.ActionType, cronExpr, req.IsActive)
		if err != nil {
			log.Printf("[RouterNetwork] Error updating scheduled action: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update scheduled action"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Scheduled action not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Scheduled action updated"})
	}
}

func deleteScheduledActionHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid action ID"})
			return
		}

		ctx := context.Background()

		// Get name for audit log before deleting
		var name string
		services.DB.Pool().QueryRow(ctx, `SELECT name FROM router_scheduled_actions WHERE id = $1`, id).Scan(&name)

		result, err := services.DB.Pool().Exec(ctx, `DELETE FROM router_scheduled_actions WHERE id = $1`, id)
		if err != nil {
			log.Printf("[RouterNetwork] Error deleting scheduled action: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete scheduled action"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Scheduled action not found"})
			return
		}

		logRouterAudit(services, "schedule_deleted",
			fmt.Sprintf("Deleted scheduled action: %s", name),
			nil, map[string]interface{}{"name": name}, "success")

		c.JSON(http.StatusOK, gin.H{"message": "Scheduled action deleted"})
	}
}

func toggleScheduledActionHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid action ID"})
			return
		}

		var req struct {
			IsActive bool `json:"isActive"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		ctx := context.Background()
		result, err := services.DB.Pool().Exec(ctx, `
			UPDATE router_scheduled_actions SET is_active = $2, updated_at = NOW() WHERE id = $1
		`, id, req.IsActive)
		if err != nil {
			log.Printf("[RouterNetwork] Error toggling scheduled action: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to toggle scheduled action"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Scheduled action not found"})
			return
		}

		status := "disabled"
		if req.IsActive {
			status = "enabled"
		}
		c.JSON(http.StatusOK, gin.H{"message": "Scheduled action " + status})
	}
}

// ---- Router write handlers with audit logging ----

func handleBlockDevice(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			MAC    string `json:"mac" binding:"required"`
			Reason string `json:"reason"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
			return
		}

		logRouterAudit(services, "device_blocked",
			fmt.Sprintf("Blocked device %s: %s", req.MAC, req.Reason),
			&req.MAC, map[string]interface{}{"reason": req.Reason}, "success")

		c.JSON(http.StatusOK, gin.H{"message": "Device blocked", "mac": req.MAC})
	}
}

func handleAllowDevice(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			MAC string `json:"mac" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
			return
		}

		logRouterAudit(services, "device_allowed",
			fmt.Sprintf("Allowed device %s", req.MAC),
			&req.MAC, nil, "success")

		c.JSON(http.StatusOK, gin.H{"message": "Device allowed", "mac": req.MAC})
	}
}

func handleMarkDeviceKnown(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			MAC  string `json:"mac" binding:"required"`
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
			return
		}

		logRouterAudit(services, "device_marked_known",
			fmt.Sprintf("Marked device %s as known (%s)", req.MAC, req.Name),
			&req.MAC, map[string]interface{}{"name": req.Name}, "success")

		c.JSON(http.StatusOK, gin.H{"message": "Device marked as known", "mac": req.MAC})
	}
}

func handleSendWOL(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			MAC string `json:"mac" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
			return
		}

		logRouterAudit(services, "wol_sent",
			fmt.Sprintf("Sent Wake-on-LAN to %s", req.MAC),
			&req.MAC, nil, "success")

		c.JSON(http.StatusOK, gin.H{"message": "WOL packet sent", "mac": req.MAC})
	}
}

func handleRunSpeedTest(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		logRouterAudit(services, "speed_test_run",
			"Manual speed test triggered",
			nil, nil, "success")

		c.JSON(http.StatusOK, gin.H{"message": "Speed test started"})
	}
}

func handleDismissAnomaly(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			AnomalyID string `json:"anomalyId" binding:"required"`
			Reason    string `json:"reason"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
			return
		}

		logRouterAudit(services, "anomaly_dismissed",
			fmt.Sprintf("Dismissed anomaly %s: %s", req.AnomalyID, req.Reason),
			nil, map[string]interface{}{"anomalyId": req.AnomalyID, "reason": req.Reason}, "success")

		c.JSON(http.StatusOK, gin.H{"message": "Anomaly dismissed"})
	}
}

// ---- Audit log reader ----

func getAuditLogsHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "25"))
		action := c.Query("action")
		search := c.Query("search")

		if page < 1 {
			page = 1
		}
		if limit < 1 || limit > 100 {
			limit = 25
		}
		offset := (page - 1) * limit

		// Build query dynamically
		where := "WHERE 1=1"
		args := []interface{}{}
		argIdx := 1

		if action != "" {
			where += fmt.Sprintf(" AND action = $%d", argIdx)
			args = append(args, action)
			argIdx++
		}
		if search != "" {
			where += fmt.Sprintf(" AND (description ILIKE $%d OR COALESCE(target_mac, '') ILIKE $%d)", argIdx, argIdx)
			args = append(args, "%"+search+"%")
			argIdx++
		}

		// Get total count
		var total int
		countQuery := "SELECT COUNT(*) FROM router_audit_log " + where
		err := services.DB.Pool().QueryRow(ctx, countQuery, args...).Scan(&total)
		if err != nil {
			log.Printf("[RouterAudit] Error counting audit logs: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count audit logs"})
			return
		}

		// Fetch page
		dataQuery := fmt.Sprintf(`
			SELECT id, action, description, target_mac, COALESCE(metadata::text, '{}'), status, created_at
			FROM router_audit_log %s
			ORDER BY created_at DESC
			LIMIT $%d OFFSET $%d
		`, where, argIdx, argIdx+1)
		args = append(args, limit, offset)

		rows, err := services.DB.Pool().Query(ctx, dataQuery, args...)
		if err != nil {
			log.Printf("[RouterAudit] Error listing audit logs: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list audit logs"})
			return
		}
		defer rows.Close()

		entries := []models.RouterAuditLog{}
		for rows.Next() {
			var e models.RouterAuditLog
			var metaStr string
			if err := rows.Scan(&e.ID, &e.Action, &e.Description, &e.TargetMAC, &metaStr, &e.Status, &e.CreatedAt); err != nil {
				log.Printf("[RouterAudit] Error scanning audit log: %v", err)
				continue
			}
			if err := json.Unmarshal([]byte(metaStr), &e.Metadata); err != nil {
				e.Metadata = map[string]interface{}{}
			}
			entries = append(entries, e)
		}

		totalPages := (total + limit - 1) / limit
		c.JSON(http.StatusOK, gin.H{
			"data":       entries,
			"page":       page,
			"limit":      limit,
			"total":      total,
			"totalPages": totalPages,
		})
	}
}

// ---- Seed default scheduled actions ----

// SeedDefaultScheduledActions inserts default actions if the table is empty.
// Called during server startup.
func SeedDefaultScheduledActions(services *Services) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var count int
	err := services.DB.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM router_scheduled_actions`).Scan(&count)
	if err != nil {
		log.Printf("[RouterNetwork] Error checking scheduled actions count (table may not exist yet): %v", err)
		return
	}

	if count > 0 {
		log.Printf("[RouterNetwork] %d scheduled actions already exist, skipping seed", count)
		return
	}

	defaults := []struct {
		Name     string
		Type     string
		Cron     string
		IsActive bool
	}{
		{"Daily Speed Test (3 AM)", "speed_test", "0 0 3 * * *", true},
		{"Weekend Guest WiFi On (8 AM)", "guest_wifi_on", "0 0 8 * * 0,6", false},
		{"Weekend Guest WiFi Off (10 PM)", "guest_wifi_off", "0 0 22 * * 0,6", false},
	}

	for _, d := range defaults {
		_, err := services.DB.Pool().Exec(ctx, `
			INSERT INTO router_scheduled_actions (id, name, action_type, cron_expression, is_active)
			VALUES ($1, $2, $3, $4, $5)
		`, uuid.New(), d.Name, d.Type, d.Cron, d.IsActive)
		if err != nil {
			log.Printf("[RouterNetwork] Error seeding default action %s: %v", d.Name, err)
		}
	}

	log.Printf("[RouterNetwork] Seeded %d default scheduled actions", len(defaults))
}

// ---- Helpers ----

// normalizeCron converts 5-field cron expressions to 6-field by prepending "0 " (seconds).
// If the expression already has 6 fields, it is returned as-is.
func normalizeCron(expr string) string {
	parts := strings.Fields(expr)
	if len(parts) == 5 {
		return "0 " + expr
	}
	return expr
}
