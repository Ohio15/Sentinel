package api

import (
	"context"
	"net/http"
	"time"

	"github.com/sentinel/server/internal/credentials"
	"github.com/sentinel/server/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RegisterCredentialRoutes registers the credential management API endpoints
func RegisterCredentialRoutes(protected *gin.RouterGroup, services *Services) {
	creds := protected.Group("/credentials")
	creds.Use(middleware.RequireRole("admin")) // Admin only
	{
		// Status and overview
		creds.GET("/status", getCredentialStatusHandler(services))
		creds.GET("/rotation-history", getRotationHistoryHandler(services))

		// JWT Secret rotation
		creds.POST("/jwt/rotate", rotateJWTSecretHandler(services))
		creds.GET("/jwt/status", getJWTStatusHandler(services))

		// API Key management
		creds.GET("/api-keys", listAPIKeysHandler(services))
		creds.POST("/api-keys", createAPIKeyHandler(services))
		creds.DELETE("/api-keys/:id", revokeAPIKeyHandler(services))
		creds.GET("/api-keys/status", getAPIKeyStatusHandler(services))

		// Rotation schedule configuration
		creds.GET("/schedules", getRotationSchedulesHandler(services))
		creds.PUT("/schedules/:type", updateRotationScheduleHandler(services))
	}
}

// getCredentialStatusHandler returns status of all managed credentials
func getCredentialStatusHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		statuses := make(map[string]interface{})

		// JWT status
		if services.JWTManager != nil {
			jwtStatus, err := services.JWTManager.GetStatus(ctx)
			if err != nil {
				statuses["jwt_secret"] = map[string]interface{}{
					"error": err.Error(),
				}
			} else {
				statuses["jwt_secret"] = jwtStatus
			}
		}

		// API Key status
		if services.APIKeyManager != nil {
			apiKeyStatus, err := services.APIKeyManager.GetStatus(ctx)
			if err != nil {
				statuses["api_key"] = map[string]interface{}{
					"error": err.Error(),
				}
			} else {
				statuses["api_key"] = apiKeyStatus
			}
		}

		// Get rotation schedules
		schedules, _ := getRotationSchedules(ctx, services.DB.Pool())
		statuses["schedules"] = schedules

		c.JSON(http.StatusOK, statuses)
	}
}

// rotateJWTSecretHandler triggers JWT secret rotation
func rotateJWTSecretHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		if services.JWTManager == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "JWT manager not initialized",
			})
			return
		}

		userIDValue, exists := c.Get("userId")
		if !exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User ID not found in context"})
			return
		}
		userID, ok := userIDValue.(uuid.UUID)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID type"})
			return
		}

		result, err := services.JWTManager.Rotate(c.Request.Context(), userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":    err.Error(),
				"rollback": true,
			})
			return
		}

		c.JSON(http.StatusOK, result)
	}
}

// getJWTStatusHandler returns current JWT key status
func getJWTStatusHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		if services.JWTManager == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "JWT manager not initialized",
			})
			return
		}

		status, err := services.JWTManager.GetStatus(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, status)
	}
}

// listAPIKeysHandler returns all API keys (without secrets)
func listAPIKeysHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		if services.APIKeyManager == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "API key manager not initialized",
			})
			return
		}

		keys, err := services.APIKeyManager.ListKeys(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, keys)
	}
}

// createAPIKeyHandler creates a new API key
func createAPIKeyHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		if services.APIKeyManager == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "API key manager not initialized",
			})
			return
		}

		var req credentials.CreateAPIKeyRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		userIDValue, exists := c.Get("userId")
		if !exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User ID not found in context"})
			return
		}
		userID, ok := userIDValue.(uuid.UUID)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID type"})
			return
		}
		req.CreatedBy = userID

		// Validate permissions
		if len(req.Permissions) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "At least one permission is required"})
			return
		}

		key, err := services.APIKeyManager.CreateKey(c.Request.Context(), req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Return with full key (only time it's shown)
		c.JSON(http.StatusCreated, key)
	}
}

// revokeAPIKeyHandler revokes an API key
func revokeAPIKeyHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		if services.APIKeyManager == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "API key manager not initialized",
			})
			return
		}

		keyID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid key ID"})
			return
		}

		userIDValue, exists := c.Get("userId")
		if !exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User ID not found in context"})
			return
		}
		userID, ok := userIDValue.(uuid.UUID)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID type"})
			return
		}

		var req struct {
			Reason string `json:"reason"`
		}
		c.ShouldBindJSON(&req)
		if req.Reason == "" {
			req.Reason = "Manual revocation"
		}

		err = services.APIKeyManager.RevokeKey(c.Request.Context(), keyID, userID, req.Reason)
		if err != nil {
			if err == credentials.ErrKeyNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "API key revoked"})
	}
}

// getAPIKeyStatusHandler returns API key statistics
func getAPIKeyStatusHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		if services.APIKeyManager == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "API key manager not initialized",
			})
			return
		}

		status, err := services.APIKeyManager.GetStatus(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, status)
	}
}

// getRotationHistoryHandler returns the credential rotation audit log
func getRotationHistoryHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := c.DefaultQuery("limit", "50")
		offset := c.DefaultQuery("offset", "0")
		credType := c.Query("type") // Optional filter by credential type

		query := `
			SELECT crl.id, crl.credential_type, crl.action, crl.old_version, crl.new_version,
			       crl.status, crl.initiated_at, crl.completed_at, crl.failure_reason,
			       crl.affected_sessions, crl.affected_agents, crl.grace_period_hours,
			       u.email as initiated_by_email
			FROM credential_rotation_log crl
			LEFT JOIN users u ON crl.initiated_by = u.id
		`
		args := make([]interface{}, 0)
		argNum := 1

		if credType != "" {
			query += " WHERE credential_type = $1"
			args = append(args, credType)
			argNum++
		}

		query += " ORDER BY initiated_at DESC LIMIT $" + string(rune('0'+argNum)) + " OFFSET $" + string(rune('0'+argNum+1))
		args = append(args, limit, offset)

		rows, err := services.DB.Pool().Query(c.Request.Context(), query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var logs []map[string]interface{}
		for rows.Next() {
			var log struct {
				ID               string
				CredentialType   string
				Action           string
				OldVersion       *int
				NewVersion       int
				Status           string
				InitiatedAt      interface{}
				CompletedAt      interface{}
				FailureReason    *string
				AffectedSessions int
				AffectedAgents   int
				GracePeriodHours *int
				InitiatedByEmail *string
			}
			err := rows.Scan(
				&log.ID, &log.CredentialType, &log.Action, &log.OldVersion,
				&log.NewVersion, &log.Status, &log.InitiatedAt, &log.CompletedAt,
				&log.FailureReason, &log.AffectedSessions, &log.AffectedAgents,
				&log.GracePeriodHours, &log.InitiatedByEmail,
			)
			if err != nil {
				continue
			}
			logs = append(logs, map[string]interface{}{
				"id":               log.ID,
				"credentialType":   log.CredentialType,
				"action":           log.Action,
				"oldVersion":       log.OldVersion,
				"newVersion":       log.NewVersion,
				"status":           log.Status,
				"initiatedAt":      log.InitiatedAt,
				"completedAt":      log.CompletedAt,
				"failureReason":    log.FailureReason,
				"affectedSessions": log.AffectedSessions,
				"affectedAgents":   log.AffectedAgents,
				"gracePeriodHours": log.GracePeriodHours,
				"initiatedBy":      log.InitiatedByEmail,
			})
		}

		c.JSON(http.StatusOK, logs)
	}
}

// getRotationSchedulesHandler returns rotation schedule configuration
func getRotationSchedulesHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		schedules, err := getRotationSchedules(c.Request.Context(), services.DB.Pool())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, schedules)
	}
}

// updateRotationScheduleHandler updates a rotation schedule
func updateRotationScheduleHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		credType := c.Param("type")

		var req struct {
			RotationIntervalDays  *int  `json:"rotationIntervalDays"`
			GracePeriodHours      *int  `json:"gracePeriodHours"`
			WarningThresholdDays  *int  `json:"warningThresholdDays"`
			AutoRotate            *bool `json:"autoRotate"`
			NotifyOnRotation      *bool `json:"notifyOnRotation"`
			NotifyOnWarning       *bool `json:"notifyOnWarning"`
			Enabled               *bool `json:"enabled"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		userIDValue, _ := c.Get("userId")
		userID, _ := userIDValue.(uuid.UUID)

		// Build update query dynamically
		query := "UPDATE credential_rotation_schedule SET updated_at = NOW(), updated_by = $1"
		args := []interface{}{userID}
		argNum := 2

		if req.RotationIntervalDays != nil {
			query += ", rotation_interval_days = $" + string(rune('0'+argNum))
			args = append(args, *req.RotationIntervalDays)
			argNum++
		}
		if req.GracePeriodHours != nil {
			query += ", grace_period_hours = $" + string(rune('0'+argNum))
			args = append(args, *req.GracePeriodHours)
			argNum++
		}
		if req.WarningThresholdDays != nil {
			query += ", warning_threshold_days = $" + string(rune('0'+argNum))
			args = append(args, *req.WarningThresholdDays)
			argNum++
		}
		if req.AutoRotate != nil {
			query += ", auto_rotate = $" + string(rune('0'+argNum))
			args = append(args, *req.AutoRotate)
			argNum++
		}
		if req.NotifyOnRotation != nil {
			query += ", notify_on_rotation = $" + string(rune('0'+argNum))
			args = append(args, *req.NotifyOnRotation)
			argNum++
		}
		if req.NotifyOnWarning != nil {
			query += ", notify_on_warning = $" + string(rune('0'+argNum))
			args = append(args, *req.NotifyOnWarning)
			argNum++
		}
		if req.Enabled != nil {
			query += ", enabled = $" + string(rune('0'+argNum))
			args = append(args, *req.Enabled)
			argNum++
		}

		query += " WHERE credential_type = $" + string(rune('0'+argNum))
		args = append(args, credType)

		_, err := services.DB.Pool().Exec(c.Request.Context(), query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Schedule updated"})
	}
}

// Helper function to get rotation schedules
func getRotationSchedules(ctx context.Context, db *pgxpool.Pool) ([]map[string]interface{}, error) {
	rows, err := db.Query(ctx, `
		SELECT credential_type, rotation_interval_days, grace_period_hours,
		       warning_threshold_days, last_rotation_at, next_scheduled_rotation,
		       auto_rotate, notify_on_rotation, notify_on_warning, enabled
		FROM credential_rotation_schedule
		ORDER BY credential_type
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []map[string]interface{}
	for rows.Next() {
		var s struct {
			CredentialType        string
			RotationIntervalDays  int
			GracePeriodHours      int
			WarningThresholdDays  int
			LastRotationAt        *time.Time
			NextScheduledRotation *time.Time
			AutoRotate            bool
			NotifyOnRotation      bool
			NotifyOnWarning       bool
			Enabled               bool
		}
		err := rows.Scan(
			&s.CredentialType, &s.RotationIntervalDays, &s.GracePeriodHours,
			&s.WarningThresholdDays, &s.LastRotationAt, &s.NextScheduledRotation,
			&s.AutoRotate, &s.NotifyOnRotation, &s.NotifyOnWarning, &s.Enabled,
		)
		if err != nil {
			continue
		}
		schedules = append(schedules, map[string]interface{}{
			"credentialType":        s.CredentialType,
			"rotationIntervalDays":  s.RotationIntervalDays,
			"gracePeriodHours":      s.GracePeriodHours,
			"warningThresholdDays":  s.WarningThresholdDays,
			"lastRotationAt":        s.LastRotationAt,
			"nextScheduledRotation": s.NextScheduledRotation,
			"autoRotate":            s.AutoRotate,
			"notifyOnRotation":      s.NotifyOnRotation,
			"notifyOnWarning":       s.NotifyOnWarning,
			"enabled":               s.Enabled,
		})
	}

	return schedules, rows.Err()
}
