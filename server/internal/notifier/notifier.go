package notifier

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sentinel/server/internal/alerting"
	"github.com/sentinel/server/internal/constants"
)

// Config holds notifier configuration
type Config struct {
	// Email settings
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPFromName string
	SMTPUseTLS   bool

	// Webhook settings
	WebhookSecret string
}

// Service orchestrates all notification channels
type Service struct {
	config        Config
	db            *pgxpool.Pool
	emailClient   *EmailClient
	webhookClient *WebhookClient
}

// NewService creates a new notification service
func NewService(config Config) *Service {
	s := &Service{
		config: config,
	}

	// Initialize email client if configured
	if config.SMTPHost != "" {
		s.emailClient = NewEmailClient(EmailConfig{
			Host:     config.SMTPHost,
			Port:     config.SMTPPort,
			Username: config.SMTPUsername,
			Password: config.SMTPPassword,
			From:     config.SMTPFrom,
			FromName: config.SMTPFromName,
			UseTLS:   config.SMTPUseTLS,
		})
		log.Println("Email notification client initialized")
	}

	// Initialize webhook client
	s.webhookClient = NewWebhookClient(config.WebhookSecret)

	return s
}

// SetDatabase sets the database pool for webhook lookups
func (s *Service) SetDatabase(db *pgxpool.Pool) {
	s.db = db
}

// SendAlert sends alert notifications through all configured channels
func (s *Service) SendAlert(alert *alerting.Alert, device *alerting.DeviceMetrics, rule *alerting.AlertRule) error {
	var lastErr error

	// System-generated alerts (rule_id = NULL) send to all webhooks
	if rule == nil {
		if s.webhookClient != nil {
			if err := s.sendWebhookAlert(alert, device, nil); err != nil {
				log.Printf("Webhook notification failed for system alert: %v", err)
				lastErr = err
			}
		}
		return lastErr
	}

	// Check notification channels from rule
	for _, channel := range rule.NotificationChannels {
		switch channel {
		case "email":
			if s.emailClient != nil {
				if err := s.sendEmailAlert(alert, device, rule); err != nil {
					log.Printf("Email notification failed: %v", err)
					lastErr = err
				}
			}
		case "webhook":
			if s.webhookClient != nil {
				if err := s.sendWebhookAlert(alert, device, rule); err != nil {
					log.Printf("Webhook notification failed: %v", err)
					lastErr = err
				}
			}
		}
	}

	return lastErr
}

func (s *Service) sendEmailAlert(alert *alerting.Alert, device *alerting.DeviceMetrics, rule *alerting.AlertRule) error {
	// TODO: Get email recipients from settings or rule configuration
	// For now, this is a placeholder that would be expanded
	return nil
}

func (s *Service) sendWebhookAlert(alert *alerting.Alert, device *alerting.DeviceMetrics, rule *alerting.AlertRule) error {
	if s.db == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Query enabled webhooks that subscribe to alert.created events
	rows, err := s.db.Query(ctx, `
		SELECT id, url, COALESCE(secret, '')
		FROM webhooks
		WHERE organization_id = $1
		  AND is_enabled = true
		  AND 'alert.created' = ANY(events)
	`, constants.CurrentOrganizationID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var lastErr error
	for rows.Next() {
		var webhookID uuid.UUID
		var url, secret string
		if err := rows.Scan(&webhookID, &url, &secret); err != nil {
			continue
		}

		// Create webhook client with this webhook's secret
		client := NewWebhookClient(secret)

		// Send the webhook
		startTime := time.Now()
		err := client.SendAlert(url, alert, device, rule)
		duration := time.Since(startTime)

		// Record delivery attempt
		success := err == nil
		var errMsg string
		if err != nil {
			errMsg = err.Error()
			lastErr = err
		}

		payload, _ := json.Marshal(map[string]interface{}{
			"alert":  alert,
			"device": device,
			"rule":   rule,
		})

		// Update webhook status and log delivery
		s.db.Exec(ctx, `
			UPDATE webhooks
			SET last_triggered_at = NOW(),
			    last_status = $2,
			    last_error = $3,
			    failure_count = CASE WHEN $2 = 'success' THEN 0 ELSE failure_count + 1 END
			WHERE id = $1
		`, webhookID, map[bool]string{true: "success", false: "failed"}[success], errMsg)

		s.db.Exec(ctx, `
			INSERT INTO webhook_deliveries (webhook_id, event_type, payload, duration_ms, success, error)
			VALUES ($1, 'alert.created', $2, $3, $4, $5)
		`, webhookID, payload, int(duration.Milliseconds()), success, errMsg)

		if success {
			log.Printf("Webhook delivered to %s for alert %s", url, alert.ID)
		} else {
			log.Printf("Webhook failed to %s: %v", url, err)
		}
	}

	return lastErr
}

// SendTestEmail sends a test email to verify configuration
func (s *Service) SendTestEmail(to string) error {
	if s.emailClient == nil {
		return ErrEmailNotConfigured
	}
	return s.emailClient.SendTest(to)
}

// IsEmailConfigured returns true if email is configured
func (s *Service) IsEmailConfigured() bool {
	return s.emailClient != nil
}

// Alert notification data structure for external use
type AlertNotification struct {
	AlertID   uuid.UUID `json:"alertId"`
	DeviceID  uuid.UUID `json:"deviceId"`
	Hostname  string    `json:"hostname"`
	Severity  string    `json:"severity"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	RuleName  string    `json:"ruleName"`
	Timestamp string    `json:"timestamp"`
}
