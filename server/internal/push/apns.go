package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	apnsProductionHost = "https://api.push.apple.com"
	apnsSandboxHost    = "https://api.sandbox.push.apple.com"
	apnsPort           = 443

	// Token refresh interval (APNs tokens valid for 1 hour, refresh at 50 minutes)
	tokenRefreshInterval = 50 * time.Minute
)

// APNsProvider implements push notifications for Apple devices
type APNsProvider struct {
	config     Config
	httpClient *http.Client
	host       string

	// JWT token management
	privateKey *ecdsa.PrivateKey
	token      string
	tokenMu    sync.RWMutex
	tokenExp   time.Time
}

// APNsPayload represents the APNs notification payload
type APNsPayload struct {
	Aps  APNsAps           `json:"aps"`
	Data map[string]string `json:"data,omitempty"`
}

// APNsAps represents the aps dictionary
type APNsAps struct {
	Alert            *APNsAlert `json:"alert,omitempty"`
	Badge            *int       `json:"badge,omitempty"`
	Sound            string     `json:"sound,omitempty"`
	ContentAvailable int        `json:"content-available,omitempty"`
	MutableContent   int        `json:"mutable-content,omitempty"`
	Category         string     `json:"category,omitempty"`
	ThreadID         string     `json:"thread-id,omitempty"`
}

// APNsAlert represents the alert content
type APNsAlert struct {
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
	Body     string `json:"body,omitempty"`
}

// APNsResponse represents the APNs response
type APNsResponse struct {
	Reason    string `json:"reason"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// NewAPNsProvider creates a new APNs provider
func NewAPNsProvider(config Config) (*APNsProvider, error) {
	// Load private key
	keyData, err := os.ReadFile(config.APNsKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read APNs key file: %w", err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	ecdsaKey, ok := privateKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not ECDSA")
	}

	// Create HTTP client with HTTP/2 support
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	transport := &http.Transport{
		TLSClientConfig:     tlsConfig,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	host := apnsProductionHost
	if config.APNsSandbox {
		host = apnsSandboxHost
	}

	provider := &APNsProvider{
		config:     config,
		httpClient: httpClient,
		host:       host,
		privateKey: ecdsaKey,
	}

	// Generate initial token
	if err := provider.refreshToken(); err != nil {
		return nil, fmt.Errorf("failed to generate initial token: %w", err)
	}

	// Start token refresh goroutine
	go provider.tokenRefreshLoop()

	return provider, nil
}

// Name returns the provider name
func (p *APNsProvider) Name() string {
	return "apns"
}

// Send sends a push notification via APNs
func (p *APNsProvider) Send(ctx context.Context, token *PushToken, notification *Notification) (*NotificationResult, error) {
	result := &NotificationResult{
		NotificationID: notification.ID,
		DeviceID:       notification.DeviceID,
	}

	// Build payload
	payload := p.buildPayload(notification)
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		result.Error = fmt.Sprintf("failed to marshal payload: %v", err)
		return result, nil
	}

	// Create request
	url := fmt.Sprintf("%s:%d/3/device/%s", p.host, apnsPort, token.Token)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payloadJSON))
	if err != nil {
		result.Error = fmt.Sprintf("failed to create request: %v", err)
		return result, nil
	}

	// Set headers
	p.tokenMu.RLock()
	authToken := p.token
	p.tokenMu.RUnlock()

	req.Header.Set("Authorization", "bearer "+authToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apns-topic", p.getBundleID(token))
	req.Header.Set("apns-push-type", p.getPushType(notification))

	if notification.Priority == PriorityHigh {
		req.Header.Set("apns-priority", "10")
	} else {
		req.Header.Set("apns-priority", "5")
	}

	if notification.TTL > 0 {
		expiration := time.Now().Add(time.Duration(notification.TTL) * time.Second).Unix()
		req.Header.Set("apns-expiration", fmt.Sprintf("%d", expiration))
	}

	// Generate unique notification ID
	apnsID := uuid.New().String()
	req.Header.Set("apns-id", apnsID)

	// Send request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		result.Error = fmt.Sprintf("failed to send request: %v", err)
		return result, nil
	}
	defer resp.Body.Close()

	result.MessageID = resp.Header.Get("apns-id")

	// Check response
	if resp.StatusCode == http.StatusOK {
		result.Success = true
		return result, nil
	}

	// Parse error response
	body, _ := io.ReadAll(resp.Body)
	var apnsResp APNsResponse
	json.Unmarshal(body, &apnsResp)

	result.Error = fmt.Sprintf("APNs error: %s (status: %d)", apnsResp.Reason, resp.StatusCode)

	// Check for invalid token
	switch apnsResp.Reason {
	case "BadDeviceToken", "Unregistered", "ExpiredProviderToken":
		result.TokenInvalid = true
	case "DeviceTokenNotForTopic":
		result.TokenInvalid = true
	}

	return result, nil
}

// buildPayload creates the APNs payload from notification
func (p *APNsProvider) buildPayload(notification *Notification) APNsPayload {
	payload := APNsPayload{
		Data: notification.Data,
	}

	if notification.Silent {
		// Silent notification for background wake
		payload.Aps = APNsAps{
			ContentAvailable: 1,
		}
	} else {
		// Visible notification
		payload.Aps = APNsAps{
			Alert: &APNsAlert{
				Title: notification.Title,
				Body:  notification.Body,
			},
			Sound: notification.Sound,
			Badge: notification.Badge,
		}
	}

	return payload
}

// getBundleID returns the appropriate bundle ID for the token
func (p *APNsProvider) getBundleID(token *PushToken) string {
	if token.AppBundleID != "" {
		return token.AppBundleID
	}
	return p.config.APNsBundleID
}

// getPushType returns the apns-push-type header value
func (p *APNsProvider) getPushType(notification *Notification) string {
	if notification.Silent {
		return "background"
	}
	return "alert"
}

// refreshToken generates a new JWT token for APNs
func (p *APNsProvider) refreshToken() error {
	now := time.Now()

	claims := jwt.MapClaims{
		"iss": p.config.APNsTeamID,
		"iat": now.Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = p.config.APNsKeyID

	signedToken, err := token.SignedString(p.privateKey)
	if err != nil {
		return fmt.Errorf("failed to sign token: %w", err)
	}

	p.tokenMu.Lock()
	p.token = signedToken
	p.tokenExp = now.Add(tokenRefreshInterval)
	p.tokenMu.Unlock()

	return nil
}

// tokenRefreshLoop periodically refreshes the JWT token
func (p *APNsProvider) tokenRefreshLoop() {
	ticker := time.NewTicker(tokenRefreshInterval)
	defer ticker.Stop()

	for range ticker.C {
		if err := p.refreshToken(); err != nil {
			fmt.Printf("Failed to refresh APNs token: %v\n", err)
		}
	}
}
