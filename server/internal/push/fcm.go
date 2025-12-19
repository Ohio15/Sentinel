package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	fcmEndpointFormat = "https://fcm.googleapis.com/v1/projects/%s/messages:send"
	fcmScope          = "https://www.googleapis.com/auth/firebase.messaging"

	// Token refresh buffer (refresh 5 minutes before expiry)
	tokenRefreshBuffer = 5 * time.Minute
)

// FCMProvider implements push notifications for Android devices via Firebase Cloud Messaging
type FCMProvider struct {
	config     Config
	httpClient *http.Client
	endpoint   string

	// OAuth2 token management
	tokenSource oauth2.TokenSource
	token       *oauth2.Token
	tokenMu     sync.RWMutex
}

// FCMMessage represents a complete FCM message
type FCMMessage struct {
	Message FCMMessageBody `json:"message"`
}

// FCMMessageBody represents the message body
type FCMMessageBody struct {
	Token        string                 `json:"token"`
	Notification *FCMNotification       `json:"notification,omitempty"`
	Data         map[string]string      `json:"data,omitempty"`
	Android      *FCMAndroidConfig      `json:"android,omitempty"`
	Webpush      *FCMWebpushConfig      `json:"webpush,omitempty"`
	APNS         *FCMAPNSConfig         `json:"apns,omitempty"`
	FCMOptions   *FCMOptions            `json:"fcm_options,omitempty"`
}

// FCMNotification represents the notification content
type FCMNotification struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
	Image string `json:"image,omitempty"`
}

// FCMAndroidConfig represents Android-specific configuration
type FCMAndroidConfig struct {
	CollapseKey           string                  `json:"collapse_key,omitempty"`
	Priority              string                  `json:"priority,omitempty"` // "normal" or "high"
	TTL                   string                  `json:"ttl,omitempty"`      // Duration format, e.g., "3600s"
	RestrictedPackageName string                  `json:"restricted_package_name,omitempty"`
	Data                  map[string]string       `json:"data,omitempty"`
	Notification          *FCMAndroidNotification `json:"notification,omitempty"`
	DirectBootOk          bool                    `json:"direct_boot_ok,omitempty"`
}

// FCMAndroidNotification represents Android notification customization
type FCMAndroidNotification struct {
	Title                 string   `json:"title,omitempty"`
	Body                  string   `json:"body,omitempty"`
	Icon                  string   `json:"icon,omitempty"`
	Color                 string   `json:"color,omitempty"`
	Sound                 string   `json:"sound,omitempty"`
	Tag                   string   `json:"tag,omitempty"`
	ClickAction           string   `json:"click_action,omitempty"`
	ChannelID             string   `json:"channel_id,omitempty"`
	DefaultSound          bool     `json:"default_sound,omitempty"`
	DefaultVibrateTimings bool     `json:"default_vibrate_timings,omitempty"`
	DefaultLightSettings  bool     `json:"default_light_settings,omitempty"`
	NotificationPriority  string   `json:"notification_priority,omitempty"`
	Visibility            string   `json:"visibility,omitempty"` // "private", "public", "secret"
}

// FCMWebpushConfig represents web push configuration
type FCMWebpushConfig struct {
	Headers      map[string]string `json:"headers,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
	Notification map[string]string `json:"notification,omitempty"`
}

// FCMAPNSConfig represents APNs configuration for FCM (cross-platform)
type FCMAPNSConfig struct {
	Headers map[string]string `json:"headers,omitempty"`
	Payload map[string]any    `json:"payload,omitempty"`
}

// FCMOptions represents additional FCM options
type FCMOptions struct {
	AnalyticsLabel string `json:"analytics_label,omitempty"`
}

// FCMResponse represents the FCM API response
type FCMResponse struct {
	Name string `json:"name"` // projects/{project_id}/messages/{message_id}
}

// FCMErrorResponse represents an FCM error response
type FCMErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
		Details []struct {
			Type         string `json:"@type"`
			ErrorCode    string `json:"errorCode,omitempty"`
			FieldViolations []struct {
				Field       string `json:"field"`
				Description string `json:"description"`
			} `json:"fieldViolations,omitempty"`
		} `json:"details,omitempty"`
	} `json:"error"`
}

// NewFCMProvider creates a new FCM provider
func NewFCMProvider(config Config) (*FCMProvider, error) {
	// Load service account credentials
	credentialsJSON, err := os.ReadFile(config.FCMCredentialsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read FCM credentials: %w", err)
	}

	// Parse credentials to get project ID if not provided
	var creds struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(credentialsJSON, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	projectID := config.FCMProjectID
	if projectID == "" {
		projectID = creds.ProjectID
	}
	if projectID == "" {
		return nil, fmt.Errorf("FCM project ID not specified")
	}

	// Create OAuth2 token source
	jwtConfig, err := google.JWTConfigFromJSON(credentialsJSON, fcmScope)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT config: %w", err)
	}

	tokenSource := jwtConfig.TokenSource(context.Background())

	// Create HTTP client
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	provider := &FCMProvider{
		config:      config,
		httpClient:  httpClient,
		endpoint:    fmt.Sprintf(fcmEndpointFormat, projectID),
		tokenSource: tokenSource,
	}

	// Get initial token
	if _, err := provider.getToken(); err != nil {
		return nil, fmt.Errorf("failed to get initial token: %w", err)
	}

	return provider, nil
}

// Name returns the provider name
func (p *FCMProvider) Name() string {
	return "fcm"
}

// Send sends a push notification via FCM
func (p *FCMProvider) Send(ctx context.Context, token *PushToken, notification *Notification) (*NotificationResult, error) {
	result := &NotificationResult{
		NotificationID: notification.ID,
		DeviceID:       notification.DeviceID,
	}

	// Build FCM message
	message := p.buildMessage(token, notification)
	messageJSON, err := json.Marshal(message)
	if err != nil {
		result.Error = fmt.Sprintf("failed to marshal message: %v", err)
		return result, nil
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, bytes.NewReader(messageJSON))
	if err != nil {
		result.Error = fmt.Sprintf("failed to create request: %v", err)
		return result, nil
	}

	// Get OAuth2 token
	oauthToken, err := p.getToken()
	if err != nil {
		result.Error = fmt.Sprintf("failed to get auth token: %v", err)
		return result, nil
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+oauthToken.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		result.Error = fmt.Sprintf("failed to send request: %v", err)
		return result, nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Check response
	if resp.StatusCode == http.StatusOK {
		var fcmResp FCMResponse
		json.Unmarshal(body, &fcmResp)
		result.Success = true
		result.MessageID = fcmResp.Name
		return result, nil
	}

	// Parse error response
	var errResp FCMErrorResponse
	json.Unmarshal(body, &errResp)

	result.Error = fmt.Sprintf("FCM error: %s (code: %d, status: %s)",
		errResp.Error.Message, errResp.Error.Code, errResp.Error.Status)

	// Check for invalid token errors
	for _, detail := range errResp.Error.Details {
		switch detail.ErrorCode {
		case "UNREGISTERED", "INVALID_ARGUMENT":
			// Token is no longer valid
			if containsTokenError(errResp.Error.Message) {
				result.TokenInvalid = true
			}
		}
	}

	// Also check status codes
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		result.TokenInvalid = true
	}

	return result, nil
}

// buildMessage creates the FCM message from notification
func (p *FCMProvider) buildMessage(token *PushToken, notification *Notification) FCMMessage {
	msg := FCMMessage{
		Message: FCMMessageBody{
			Token: token.Token,
			Data:  notification.Data,
		},
	}

	// Set Android-specific config
	androidConfig := &FCMAndroidConfig{}

	if notification.Priority == PriorityHigh {
		androidConfig.Priority = "high"
	} else {
		androidConfig.Priority = "normal"
	}

	if notification.TTL > 0 {
		androidConfig.TTL = fmt.Sprintf("%ds", notification.TTL)
	}

	if notification.Silent {
		// Data-only message for silent delivery
		// Don't set notification payload to ensure it's data-only
		androidConfig.DirectBootOk = true
	} else {
		// Visible notification
		msg.Message.Notification = &FCMNotification{
			Title: notification.Title,
			Body:  notification.Body,
		}

		androidConfig.Notification = &FCMAndroidNotification{
			ChannelID: "sentinel_default",
		}

		if notification.Sound != "" {
			androidConfig.Notification.Sound = notification.Sound
		} else {
			androidConfig.Notification.DefaultSound = true
		}
	}

	msg.Message.Android = androidConfig

	return msg
}

// getToken returns a valid OAuth2 token, refreshing if necessary
func (p *FCMProvider) getToken() (*oauth2.Token, error) {
	p.tokenMu.RLock()
	if p.token != nil && p.token.Valid() && time.Until(p.token.Expiry) > tokenRefreshBuffer {
		token := p.token
		p.tokenMu.RUnlock()
		return token, nil
	}
	p.tokenMu.RUnlock()

	// Need to refresh token
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()

	// Double-check after acquiring write lock
	if p.token != nil && p.token.Valid() && time.Until(p.token.Expiry) > tokenRefreshBuffer {
		return p.token, nil
	}

	token, err := p.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	p.token = token
	return token, nil
}

// containsTokenError checks if the error message indicates an invalid token
func containsTokenError(message string) bool {
	tokenErrors := []string{
		"not a valid FCM registration token",
		"registration token is not registered",
		"token is invalid",
		"invalid registration",
	}

	for _, err := range tokenErrors {
		if contains(message, err) {
			return true
		}
	}
	return false
}

// contains checks if s contains substr (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
