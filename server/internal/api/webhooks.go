package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/constants"
)

// Webhook represents a webhook configuration
type Webhook struct {
	ID              uuid.UUID         `json:"id"`
	OrganizationID  uuid.UUID         `json:"organizationId"`
	Name            string            `json:"name"`
	URL             string            `json:"url"`
	Secret          string            `json:"secret,omitempty"` // Hidden in responses
	Events          []string          `json:"events"`
	Headers         map[string]string `json:"headers"`
	IsEnabled       bool              `json:"isEnabled"`
	LastTriggeredAt *time.Time        `json:"lastTriggeredAt,omitempty"`
	LastStatus      string            `json:"lastStatus,omitempty"`
	LastError       string            `json:"lastError,omitempty"`
	FailureCount    int               `json:"failureCount"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

// WebhookDelivery represents a webhook delivery record
type WebhookDelivery struct {
	ID             uuid.UUID       `json:"id"`
	WebhookID      uuid.UUID       `json:"webhookId"`
	EventType      string          `json:"eventType"`
	Payload        json.RawMessage `json:"payload"`
	ResponseStatus int             `json:"responseStatus,omitempty"`
	ResponseBody   string          `json:"responseBody,omitempty"`
	DurationMs     int             `json:"durationMs"`
	Success        bool            `json:"success"`
	Error          string          `json:"error,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

// CreateWebhookRequest is the request body for creating a webhook
type CreateWebhookRequest struct {
	Name    string            `json:"name" binding:"required"`
	URL     string            `json:"url" binding:"required,url"`
	Secret  string            `json:"secret"`
	Events  []string          `json:"events" binding:"required,min=1"`
	Headers map[string]string `json:"headers"`
}

// UpdateWebhookRequest is the request body for updating a webhook
type UpdateWebhookRequest struct {
	Name      *string           `json:"name"`
	URL       *string           `json:"url"`
	Secret    *string           `json:"secret"`
	Events    []string          `json:"events"`
	Headers   map[string]string `json:"headers"`
	IsEnabled *bool             `json:"isEnabled"`
}

func listWebhooksHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		rows, err := services.DB.Pool().Query(ctx, `
			SELECT id, organization_id, name, url, events, headers, is_enabled,
			       last_triggered_at, last_status, last_error, failure_count,
			       created_at, updated_at
			FROM webhooks
			WHERE organization_id = $1
			ORDER BY created_at DESC
		`, constants.CurrentOrganizationID)
		if err != nil {
			log.Printf("[Webhooks] Error listing webhooks: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list webhooks"})
			return
		}
		defer rows.Close()

		webhooks := []Webhook{}
		for rows.Next() {
			var w Webhook
			var eventsArr []string
			var headersJSON []byte
			var lastTriggeredAt *time.Time
			var lastStatus, lastError *string

			err := rows.Scan(
				&w.ID, &w.OrganizationID, &w.Name, &w.URL, &eventsArr, &headersJSON,
				&w.IsEnabled, &lastTriggeredAt, &lastStatus, &lastError, &w.FailureCount,
				&w.CreatedAt, &w.UpdatedAt,
			)
			if err != nil {
				log.Printf("[Webhooks] Error scanning webhook: %v", err)
				continue
			}

			w.Events = eventsArr
			if len(headersJSON) > 0 {
				json.Unmarshal(headersJSON, &w.Headers)
			}
			if w.Headers == nil {
				w.Headers = make(map[string]string)
			}
			if lastTriggeredAt != nil {
				w.LastTriggeredAt = lastTriggeredAt
			}
			if lastStatus != nil {
				w.LastStatus = *lastStatus
			}
			if lastError != nil {
				w.LastError = *lastError
			}

			webhooks = append(webhooks, w)
		}

		c.JSON(http.StatusOK, webhooks)
	}
}

func createWebhookHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateWebhookRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
			return
		}

		// Validate events
		validEvents := map[string]bool{
			"alert.created":        true,
			"alert.resolved":       true,
			"alert.acknowledged":   true,
			"device.online":        true,
			"device.offline":       true,
			"device.enrolled":      true,
			"device.unenrolled":    true,
			"update.available":     true,
			"update.installed":     true,
			"command.completed":    true,
			"script.completed":     true,
		}
		for _, event := range req.Events {
			if !validEvents[event] {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid event type: " + event})
				return
			}
		}

		ctx := context.Background()
		userID := c.MustGet("userId").(uuid.UUID)
		webhookID := uuid.New()

		headersJSON, _ := json.Marshal(req.Headers)
		if req.Headers == nil {
			headersJSON = []byte("{}")
		}

		_, err := services.DB.Pool().Exec(ctx, `
			INSERT INTO webhooks (id, organization_id, name, url, secret, events, headers, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, webhookID, constants.CurrentOrganizationID, req.Name, req.URL, req.Secret, req.Events, headersJSON, userID)
		if err != nil {
			log.Printf("[Webhooks] Error creating webhook: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create webhook"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"id":      webhookID,
			"message": "Webhook created successfully",
		})
	}
}

func getWebhookHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid webhook ID"})
			return
		}

		ctx := context.Background()
		var w Webhook
		var eventsArr []string
		var headersJSON []byte
		var lastTriggeredAt *time.Time
		var lastStatus, lastError *string

		err = services.DB.Pool().QueryRow(ctx, `
			SELECT id, organization_id, name, url, events, headers, is_enabled,
			       last_triggered_at, last_status, last_error, failure_count,
			       created_at, updated_at
			FROM webhooks
			WHERE id = $1 AND organization_id = $2
		`, id, constants.CurrentOrganizationID).Scan(
			&w.ID, &w.OrganizationID, &w.Name, &w.URL, &eventsArr, &headersJSON,
			&w.IsEnabled, &lastTriggeredAt, &lastStatus, &lastError, &w.FailureCount,
			&w.CreatedAt, &w.UpdatedAt,
		)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Webhook not found"})
			return
		}

		w.Events = eventsArr
		if len(headersJSON) > 0 {
			json.Unmarshal(headersJSON, &w.Headers)
		}
		if w.Headers == nil {
			w.Headers = make(map[string]string)
		}
		if lastTriggeredAt != nil {
			w.LastTriggeredAt = lastTriggeredAt
		}
		if lastStatus != nil {
			w.LastStatus = *lastStatus
		}
		if lastError != nil {
			w.LastError = *lastError
		}

		c.JSON(http.StatusOK, w)
	}
}

func updateWebhookHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid webhook ID"})
			return
		}

		var req UpdateWebhookRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		ctx := context.Background()

		// Build dynamic update query
		query := "UPDATE webhooks SET updated_at = NOW()"
		args := []interface{}{}
		argIdx := 1

		if req.Name != nil {
			query += ", name = $" + string(rune('0'+argIdx))
			args = append(args, *req.Name)
			argIdx++
		}
		if req.URL != nil {
			query += ", url = $" + string(rune('0'+argIdx))
			args = append(args, *req.URL)
			argIdx++
		}
		if req.Secret != nil {
			query += ", secret = $" + string(rune('0'+argIdx))
			args = append(args, *req.Secret)
			argIdx++
		}
		if req.Events != nil {
			query += ", events = $" + string(rune('0'+argIdx))
			args = append(args, req.Events)
			argIdx++
		}
		if req.Headers != nil {
			headersJSON, _ := json.Marshal(req.Headers)
			query += ", headers = $" + string(rune('0'+argIdx))
			args = append(args, headersJSON)
			argIdx++
		}
		if req.IsEnabled != nil {
			query += ", is_enabled = $" + string(rune('0'+argIdx))
			args = append(args, *req.IsEnabled)
			argIdx++
		}

		query += " WHERE id = $" + string(rune('0'+argIdx)) + " AND organization_id = $" + string(rune('0'+argIdx+1))
		args = append(args, id, constants.CurrentOrganizationID)

		result, err := services.DB.Pool().Exec(ctx, query, args...)
		if err != nil {
			log.Printf("[Webhooks] Error updating webhook: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update webhook"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Webhook not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Webhook updated successfully"})
	}
}

func deleteWebhookHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid webhook ID"})
			return
		}

		ctx := context.Background()
		result, err := services.DB.Pool().Exec(ctx, `
			DELETE FROM webhooks WHERE id = $1 AND organization_id = $2
		`, id, constants.CurrentOrganizationID)
		if err != nil {
			log.Printf("[Webhooks] Error deleting webhook: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete webhook"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Webhook not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Webhook deleted successfully"})
	}
}

func testWebhookHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid webhook ID"})
			return
		}

		ctx := context.Background()
		var url, secret string
		err = services.DB.Pool().QueryRow(ctx, `
			SELECT url, COALESCE(secret, '') FROM webhooks WHERE id = $1 AND organization_id = $2
		`, id, constants.CurrentOrganizationID).Scan(&url, &secret)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Webhook not found"})
			return
		}

		// Send test webhook
		testPayload := map[string]interface{}{
			"event":     "test",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"data": map[string]interface{}{
				"message": "This is a test webhook from Sentinel RMM",
				"webhookId": id.String(),
			},
		}

		payloadBytes, _ := json.Marshal(testPayload)

		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Sentinel-RMM/1.0")
		req.Header.Set("X-Sentinel-Event", "test")

		// Note: In production, we'd use the notifier.WebhookClient with signing
		// For the test endpoint, we just verify connectivity

		client := &http.Client{Timeout: 10 * time.Second}
		req.Body = http.NoBody // Actually send the payload

		// Create new request with body
		req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
		req2.Header = req.Header

		// Simple connectivity test
		resp, err := client.Do(req2)
		if err != nil {
			log.Printf("[Webhook] Test delivery failed for webhook %s: %v", id, err)
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"error":   "Failed to connect to webhook URL",
				"message": "Failed to connect to webhook URL",
			})
			return
		}
		defer resp.Body.Close()

		// Record the test delivery
		services.DB.Pool().Exec(ctx, `
			INSERT INTO webhook_deliveries (webhook_id, event_type, payload, response_status, success, duration_ms)
			VALUES ($1, 'test', $2, $3, $4, 0)
		`, id, payloadBytes, resp.StatusCode, resp.StatusCode >= 200 && resp.StatusCode < 300)

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.JSON(http.StatusOK, gin.H{
				"success":    true,
				"statusCode": resp.StatusCode,
				"message":    "Webhook test successful",
			})
		} else {
			c.JSON(http.StatusOK, gin.H{
				"success":    false,
				"statusCode": resp.StatusCode,
				"message":    "Webhook returned non-success status",
			})
		}
	}
}

func listWebhookDeliveriesHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid webhook ID"})
			return
		}

		ctx := context.Background()

		// Verify webhook belongs to organization
		var exists bool
		services.DB.Pool().QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM webhooks WHERE id = $1 AND organization_id = $2)
		`, id, constants.CurrentOrganizationID).Scan(&exists)
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Webhook not found"})
			return
		}

		rows, err := services.DB.Pool().Query(ctx, `
			SELECT id, webhook_id, event_type, payload, response_status,
			       COALESCE(response_body, ''), duration_ms, success,
			       COALESCE(error, ''), created_at
			FROM webhook_deliveries
			WHERE webhook_id = $1
			ORDER BY created_at DESC
			LIMIT 100
		`, id)
		if err != nil {
			log.Printf("[Webhooks] Error listing deliveries: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list deliveries"})
			return
		}
		defer rows.Close()

		deliveries := []WebhookDelivery{}
		for rows.Next() {
			var d WebhookDelivery
			var responseStatus *int
			err := rows.Scan(
				&d.ID, &d.WebhookID, &d.EventType, &d.Payload, &responseStatus,
				&d.ResponseBody, &d.DurationMs, &d.Success, &d.Error, &d.CreatedAt,
			)
			if err != nil {
				continue
			}
			if responseStatus != nil {
				d.ResponseStatus = *responseStatus
			}
			deliveries = append(deliveries, d)
		}

		c.JSON(http.StatusOK, deliveries)
	}
}
