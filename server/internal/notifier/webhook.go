package notifier

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sentinel/server/internal/alerting"
)

// WebhookClient handles sending webhook notifications
type WebhookClient struct {
	client *http.Client
	secret string
}

// WebhookPayload is the structure sent to webhook endpoints
type WebhookPayload struct {
	Event     string                 `json:"event"`
	Timestamp string                 `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// NewWebhookClient creates a new webhook client
func NewWebhookClient(secret string) *WebhookClient {
	return &WebhookClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		secret: secret,
	}
}

// SendAlert sends an alert webhook notification
func (c *WebhookClient) SendAlert(url string, alert *alerting.Alert, device *alerting.DeviceMetrics, rule *alerting.AlertRule) error {
	payload := WebhookPayload{
		Event:     "alert.created",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data: map[string]interface{}{
			"alert": map[string]interface{}{
				"id":        alert.ID,
				"severity":  alert.Severity,
				"title":     alert.Title,
				"message":   alert.Message,
				"status":    alert.Status,
				"createdAt": alert.CreatedAt.Format(time.RFC3339),
			},
			"device": map[string]interface{}{
				"id":            device.DeviceID,
				"hostname":      device.Hostname,
				"cpuPercent":    device.CPUPercent,
				"memoryPercent": device.MemoryPercent,
				"diskPercent":   device.DiskPercent,
				"status":        device.Status,
			},
			"rule": map[string]interface{}{
				"id":        rule.ID,
				"name":      rule.Name,
				"metric":    rule.Metric,
				"operator":  rule.Operator,
				"threshold": rule.Threshold,
			},
		},
	}

	return c.send(url, payload)
}

// SendDeviceStatusChange sends a device status change webhook
func (c *WebhookClient) SendDeviceStatusChange(url string, deviceID, hostname, oldStatus, newStatus string) error {
	payload := WebhookPayload{
		Event:     "device.status_changed",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data: map[string]interface{}{
			"deviceId":  deviceID,
			"hostname":  hostname,
			"oldStatus": oldStatus,
			"newStatus": newStatus,
		},
	}

	return c.send(url, payload)
}

func (c *WebhookClient) send(url string, payload WebhookPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Sentinel-RMM/1.0")
	req.Header.Set("X-Sentinel-Event", payload.Event)
	req.Header.Set("X-Sentinel-Timestamp", payload.Timestamp)

	// Add HMAC signature if secret is configured
	if c.secret != "" {
		signature := c.sign(body)
		req.Header.Set("X-Sentinel-Signature", signature)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	// Drain and close body
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// sign creates an HMAC-SHA256 signature of the payload
func (c *WebhookClient) sign(payload []byte) string {
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// SendWithRetry sends a webhook with exponential backoff retry
func (c *WebhookClient) SendWithRetry(url string, payload WebhookPayload, maxRetries int) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, 8s...
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			time.Sleep(backoff)
		}

		if err := c.send(url, payload); err != nil {
			lastErr = err
			continue
		}
		return nil
	}

	return fmt.Errorf("webhook failed after %d attempts: %w", maxRetries+1, lastErr)
}
