package push

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TokenType represents the type of push token
type TokenType string

const (
	TokenTypeAPNs     TokenType = "apns"
	TokenTypeAPNsVoIP TokenType = "apns_voip"
	TokenTypeFCM      TokenType = "fcm"
)

// NotificationType represents the type of notification
type NotificationType string

const (
	NotificationTypeWake    NotificationType = "wake"
	NotificationTypeCommand NotificationType = "command"
	NotificationTypeAlert   NotificationType = "alert"
	NotificationTypeMessage NotificationType = "message"
)

// Priority represents notification priority
type Priority string

const (
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
)

// PushToken represents a device push token
type PushToken struct {
	ID           uuid.UUID `json:"id"`
	DeviceID     uuid.UUID `json:"deviceId"`
	TokenType    TokenType `json:"tokenType"`
	Token        string    `json:"token"`
	AppBundleID  string    `json:"appBundleId"`
	Environment  string    `json:"environment"` // "sandbox" or "production"
	IsActive     bool      `json:"isActive"`
	LastUsedAt   time.Time `json:"lastUsedAt,omitempty"`
	RegisteredAt time.Time `json:"registeredAt"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
}

// Notification represents a push notification to send
type Notification struct {
	ID       uuid.UUID            `json:"id"`
	DeviceID uuid.UUID            `json:"deviceId"`
	Type     NotificationType     `json:"type"`
	Title    string               `json:"title,omitempty"`
	Body     string               `json:"body,omitempty"`
	Data     map[string]string    `json:"data,omitempty"`
	Priority Priority             `json:"priority"`
	Badge    *int                 `json:"badge,omitempty"`
	Sound    string               `json:"sound,omitempty"`
	Silent   bool                 `json:"silent"`
	TTL      int                  `json:"ttl,omitempty"` // seconds
}

// NotificationResult represents the result of sending a notification
type NotificationResult struct {
	NotificationID uuid.UUID `json:"notificationId"`
	DeviceID       uuid.UUID `json:"deviceId"`
	Success        bool      `json:"success"`
	MessageID      string    `json:"messageId,omitempty"` // APNs ID or FCM message ID
	Error          string    `json:"error,omitempty"`
	TokenInvalid   bool      `json:"tokenInvalid"`
}

// Provider defines the interface for push notification providers
type Provider interface {
	Send(ctx context.Context, token *PushToken, notification *Notification) (*NotificationResult, error)
	Name() string
}

// Config holds push notification configuration
type Config struct {
	// APNs configuration
	APNsKeyPath   string
	APNsKeyID     string
	APNsTeamID    string
	APNsBundleID  string
	APNsSandbox   bool

	// FCM configuration
	FCMCredentialsPath string
	FCMProjectID       string
}

// Service manages push notifications across providers
type Service struct {
	db        *pgxpool.Pool
	providers map[TokenType]Provider
	config    Config
	mu        sync.RWMutex

	// Metrics
	sentCount   int64
	failedCount int64
}

// NewService creates a new push notification service
func NewService(db *pgxpool.Pool, config Config) (*Service, error) {
	s := &Service{
		db:        db,
		providers: make(map[TokenType]Provider),
		config:    config,
	}

	// Initialize APNs provider if configured
	if config.APNsKeyPath != "" {
		apns, err := NewAPNsProvider(config)
		if err != nil {
			log.Printf("Warning: Failed to initialize APNs provider: %v", err)
		} else {
			s.providers[TokenTypeAPNs] = apns
			s.providers[TokenTypeAPNsVoIP] = apns
			log.Printf("APNs provider initialized (sandbox: %v)", config.APNsSandbox)
		}
	}

	// Initialize FCM provider if configured
	if config.FCMCredentialsPath != "" {
		fcm, err := NewFCMProvider(config)
		if err != nil {
			log.Printf("Warning: Failed to initialize FCM provider: %v", err)
		} else {
			s.providers[TokenTypeFCM] = fcm
			log.Printf("FCM provider initialized (project: %s)", config.FCMProjectID)
		}
	}

	return s, nil
}

// RegisterToken registers or updates a push token for a device
func (s *Service) RegisterToken(ctx context.Context, token *PushToken) error {
	query := `
		INSERT INTO push_tokens (device_id, token_type, token, app_bundle_id, environment, is_active, registered_at)
		VALUES ($1, $2, $3, $4, $5, TRUE, NOW())
		ON CONFLICT (device_id, token_type, token) DO UPDATE SET
			is_active = TRUE,
			app_bundle_id = EXCLUDED.app_bundle_id,
			environment = EXCLUDED.environment,
			registered_at = NOW()
		RETURNING id
	`

	err := s.db.QueryRow(ctx, query,
		token.DeviceID,
		token.TokenType,
		token.Token,
		token.AppBundleID,
		token.Environment,
	).Scan(&token.ID)

	if err != nil {
		return fmt.Errorf("failed to register token: %w", err)
	}

	log.Printf("Registered push token for device %s (type: %s)", token.DeviceID, token.TokenType)
	return nil
}

// DeactivateToken marks a token as inactive
func (s *Service) DeactivateToken(ctx context.Context, deviceID uuid.UUID, tokenType TokenType) error {
	query := `
		UPDATE push_tokens
		SET is_active = FALSE
		WHERE device_id = $1 AND token_type = $2
	`

	_, err := s.db.Exec(ctx, query, deviceID, tokenType)
	return err
}

// GetActiveTokens returns all active tokens for a device
func (s *Service) GetActiveTokens(ctx context.Context, deviceID uuid.UUID) ([]PushToken, error) {
	query := `
		SELECT id, device_id, token_type, token, app_bundle_id, environment, is_active, last_used_at, registered_at, expires_at
		FROM push_tokens
		WHERE device_id = $1 AND is_active = TRUE
	`

	rows, err := s.db.Query(ctx, query, deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tokens: %w", err)
	}
	defer rows.Close()

	var tokens []PushToken
	for rows.Next() {
		var t PushToken
		var lastUsed, expiresAt *time.Time
		err := rows.Scan(&t.ID, &t.DeviceID, &t.TokenType, &t.Token, &t.AppBundleID, &t.Environment, &t.IsActive, &lastUsed, &t.RegisteredAt, &expiresAt)
		if err != nil {
			return nil, err
		}
		if lastUsed != nil {
			t.LastUsedAt = *lastUsed
		}
		if expiresAt != nil {
			t.ExpiresAt = *expiresAt
		}
		tokens = append(tokens, t)
	}

	return tokens, nil
}

// Send sends a push notification to a device
func (s *Service) Send(ctx context.Context, notification *Notification) ([]NotificationResult, error) {
	if notification.ID == uuid.Nil {
		notification.ID = uuid.New()
	}

	// Get active tokens for device
	tokens, err := s.GetActiveTokens(ctx, notification.DeviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tokens: %w", err)
	}

	if len(tokens) == 0 {
		return nil, fmt.Errorf("no active push tokens for device %s", notification.DeviceID)
	}

	var results []NotificationResult
	var wg sync.WaitGroup

	resultsChan := make(chan NotificationResult, len(tokens))

	for _, token := range tokens {
		provider, ok := s.providers[token.TokenType]
		if !ok {
			results = append(results, NotificationResult{
				NotificationID: notification.ID,
				DeviceID:       notification.DeviceID,
				Success:        false,
				Error:          fmt.Sprintf("no provider for token type: %s", token.TokenType),
			})
			continue
		}

		wg.Add(1)
		go func(t PushToken, p Provider) {
			defer wg.Done()

			result, err := p.Send(ctx, &t, notification)
			if err != nil {
				result = &NotificationResult{
					NotificationID: notification.ID,
					DeviceID:       notification.DeviceID,
					Success:        false,
					Error:          err.Error(),
				}
			}

			// Record notification in database
			s.recordNotification(ctx, &t, notification, result)

			// Update token status
			if result.TokenInvalid {
				s.DeactivateToken(ctx, t.DeviceID, t.TokenType)
			} else if result.Success {
				s.updateTokenLastUsed(ctx, t.ID)
			}

			resultsChan <- *result
		}(token, provider)
	}

	wg.Wait()
	close(resultsChan)

	for result := range resultsChan {
		results = append(results, result)
		if result.Success {
			s.sentCount++
		} else {
			s.failedCount++
		}
	}

	return results, nil
}

// SendSilentWake sends a silent push to wake the app
func (s *Service) SendSilentWake(ctx context.Context, deviceID uuid.UUID, data map[string]string) ([]NotificationResult, error) {
	notification := &Notification{
		ID:       uuid.New(),
		DeviceID: deviceID,
		Type:     NotificationTypeWake,
		Data:     data,
		Priority: PriorityHigh,
		Silent:   true,
		TTL:      60, // 1 minute expiry for wake notifications
	}

	return s.Send(ctx, notification)
}

// SendCommand sends a command notification
func (s *Service) SendCommand(ctx context.Context, deviceID uuid.UUID, commandID string, commandType string) ([]NotificationResult, error) {
	notification := &Notification{
		ID:       uuid.New(),
		DeviceID: deviceID,
		Type:     NotificationTypeCommand,
		Data: map[string]string{
			"command_id":   commandID,
			"command_type": commandType,
		},
		Priority: PriorityHigh,
		Silent:   true,
		TTL:      300, // 5 minute expiry
	}

	return s.Send(ctx, notification)
}

// SendAlert sends a visible alert notification
func (s *Service) SendAlert(ctx context.Context, deviceID uuid.UUID, title, body string) ([]NotificationResult, error) {
	notification := &Notification{
		ID:       uuid.New(),
		DeviceID: deviceID,
		Type:     NotificationTypeAlert,
		Title:    title,
		Body:     body,
		Priority: PriorityHigh,
		Silent:   false,
		Sound:    "default",
	}

	return s.Send(ctx, notification)
}

// recordNotification stores notification in database
func (s *Service) recordNotification(ctx context.Context, token *PushToken, notification *Notification, result *NotificationResult) {
	payloadJSON, _ := json.Marshal(map[string]interface{}{
		"type":   notification.Type,
		"title":  notification.Title,
		"body":   notification.Body,
		"data":   notification.Data,
		"silent": notification.Silent,
	})

	query := `
		INSERT INTO push_notifications (
			id, device_id, token_id, notification_type, payload, priority, sent_at,
			delivered_at, failed_at, error_message, apns_id, fcm_message_id
		) VALUES ($1, $2, $3, $4, $5, $6, NOW(), $7, $8, $9, $10, $11)
	`

	var deliveredAt, failedAt *time.Time
	now := time.Now()
	if result.Success {
		deliveredAt = &now
	} else {
		failedAt = &now
	}

	var apnsID, fcmID *string
	if token.TokenType == TokenTypeAPNs || token.TokenType == TokenTypeAPNsVoIP {
		apnsID = &result.MessageID
	} else if token.TokenType == TokenTypeFCM {
		fcmID = &result.MessageID
	}

	_, err := s.db.Exec(ctx, query,
		notification.ID,
		notification.DeviceID,
		token.ID,
		notification.Type,
		payloadJSON,
		notification.Priority,
		deliveredAt,
		failedAt,
		result.Error,
		apnsID,
		fcmID,
	)

	if err != nil {
		log.Printf("Failed to record notification: %v", err)
	}
}

// updateTokenLastUsed updates the last used timestamp for a token
func (s *Service) updateTokenLastUsed(ctx context.Context, tokenID uuid.UUID) {
	query := `UPDATE push_tokens SET last_used_at = NOW() WHERE id = $1`
	s.db.Exec(ctx, query, tokenID)
}

// Stats returns service statistics
func (s *Service) Stats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	providers := make([]string, 0, len(s.providers))
	for tokenType := range s.providers {
		providers = append(providers, string(tokenType))
	}

	return map[string]interface{}{
		"providers":    providers,
		"sent_count":   s.sentCount,
		"failed_count": s.failedCount,
	}
}
