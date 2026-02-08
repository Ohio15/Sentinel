package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sentinel/server/internal/constants"
)

// AlertRule represents a configured alert rule from the database
type AlertRule struct {
	ID                   uuid.UUID `json:"id"`
	Name                 string    `json:"name"`
	Description          string    `json:"description"`
	Enabled              bool      `json:"enabled"`
	Metric               string    `json:"metric"`
	Operator             string    `json:"operator"`
	Threshold            float64   `json:"threshold"`
	DurationSeconds      int       `json:"durationSeconds"`
	Severity             string    `json:"severity"`
	CooldownMinutes      int       `json:"cooldownMinutes"`
	NotificationChannels []string  `json:"notificationChannels"`
}

// Alert represents an alert instance
type Alert struct {
	ID        uuid.UUID `json:"id"`
	DeviceID  uuid.UUID `json:"deviceId"`
	RuleID    uuid.UUID `json:"ruleId"`
	Severity  string    `json:"severity"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// DeviceMetrics represents the latest metrics for a device
type DeviceMetrics struct {
	DeviceID      uuid.UUID
	AgentID       string
	Hostname      string
	CPUPercent    float64
	MemoryPercent float64
	DiskPercent   float64
	Status        string
	LastSeen      time.Time
}

// WebSocketHub interface for broadcasting alerts
type WebSocketHub interface {
	BroadcastToDashboards(message []byte)
}

// Notifier interface for sending notifications
type Notifier interface {
	SendAlert(alert *Alert, device *DeviceMetrics, rule *AlertRule) error
}

// EngineConfig holds configuration for the alert engine
type EngineConfig struct {
	EvaluationInterval time.Duration
	OrganizationID     int
}

// Engine evaluates alert rules against device metrics
type Engine struct {
	db       *pgxpool.Pool
	hub      WebSocketHub
	notifier Notifier
	config   EngineConfig

	// Track last alert time per device+rule for cooldown
	lastAlertTime map[string]time.Time
	lastAlertMu   sync.RWMutex

	// Track sustained conditions for duration_seconds
	conditionStart map[string]time.Time
	conditionMu    sync.RWMutex

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewEngine creates a new alert evaluation engine
func NewEngine(db *pgxpool.Pool, hub WebSocketHub, notifier Notifier, config EngineConfig) *Engine {
	if config.EvaluationInterval == 0 {
		config.EvaluationInterval = 60 * time.Second
	}
	if config.OrganizationID == 0 {
		config.OrganizationID = constants.CurrentOrganizationID
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Engine{
		db:             db,
		hub:            hub,
		notifier:       notifier,
		config:         config,
		lastAlertTime:  make(map[string]time.Time),
		conditionStart: make(map[string]time.Time),
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Start begins the alert evaluation loop
func (e *Engine) Start() {
	e.wg.Add(1)
	go e.evaluationLoop()
	log.Println("Alert engine started")
}

// Stop gracefully shuts down the alert engine
func (e *Engine) Stop() {
	e.cancel()
	e.wg.Wait()
	log.Println("Alert engine stopped")
}

func (e *Engine) evaluationLoop() {
	defer e.wg.Done()

	ticker := time.NewTicker(e.config.EvaluationInterval)
	defer ticker.Stop()

	// Run immediately on start
	e.evaluateAllRules()

	for {
		select {
		case <-ticker.C:
			e.evaluateAllRules()
		case <-e.ctx.Done():
			return
		}
	}
}

func (e *Engine) evaluateAllRules() {
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()

	// Load enabled rules
	rules, err := e.loadEnabledRules(ctx)
	if err != nil {
		log.Printf("Alert engine: failed to load rules: %v", err)
		return
	}

	if len(rules) == 0 {
		return
	}

	// Load all devices with their latest metrics
	devices, err := e.loadDevicesWithMetrics(ctx)
	if err != nil {
		log.Printf("Alert engine: failed to load device metrics: %v", err)
		return
	}

	// Evaluate each rule against each device
	for _, rule := range rules {
		for _, device := range devices {
			e.evaluateRule(ctx, rule, device)
		}
	}
}

func (e *Engine) loadEnabledRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := e.db.Query(ctx, `
		SELECT id, name, COALESCE(description, ''), enabled, metric, operator,
		       threshold, duration_seconds, severity, cooldown_minutes,
		       COALESCE(notification_channels, '{}')
		FROM alert_rules
		WHERE enabled = true
		ORDER BY severity DESC, name
	`)
	if err != nil {
		return nil, fmt.Errorf("query rules: %w", err)
	}
	defer rows.Close()

	var rules []AlertRule
	for rows.Next() {
		var r AlertRule
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Description, &r.Enabled, &r.Metric, &r.Operator,
			&r.Threshold, &r.DurationSeconds, &r.Severity, &r.CooldownMinutes,
			&r.NotificationChannels,
		); err != nil {
			log.Printf("Alert engine: scan rule error: %v", err)
			continue
		}
		rules = append(rules, r)
	}

	return rules, nil
}

func (e *Engine) loadDevicesWithMetrics(ctx context.Context) ([]DeviceMetrics, error) {
	rows, err := e.db.Query(ctx, `
		SELECT d.id, d.agent_id, COALESCE(d.hostname, ''), d.status, d.last_seen,
		       COALESCE(m.cpu_percent, 0), COALESCE(m.memory_percent, 0), COALESCE(m.disk_percent, 0)
		FROM devices d
		LEFT JOIN LATERAL (
			SELECT cpu_percent, memory_percent, disk_percent
			FROM device_metrics
			WHERE device_id = d.id
			ORDER BY timestamp DESC
			LIMIT 1
		) m ON true
		WHERE d.organization_id = $1
		  AND d.is_disabled = false
		ORDER BY d.hostname
	`, e.config.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("query devices: %w", err)
	}
	defer rows.Close()

	var devices []DeviceMetrics
	for rows.Next() {
		var d DeviceMetrics
		if err := rows.Scan(
			&d.DeviceID, &d.AgentID, &d.Hostname, &d.Status, &d.LastSeen,
			&d.CPUPercent, &d.MemoryPercent, &d.DiskPercent,
		); err != nil {
			log.Printf("Alert engine: scan device error: %v", err)
			continue
		}
		devices = append(devices, d)
	}

	return devices, nil
}

func (e *Engine) evaluateRule(ctx context.Context, rule AlertRule, device DeviceMetrics) {
	// Get metric value based on rule's metric type
	var value float64
	switch rule.Metric {
	case "cpu_percent":
		value = device.CPUPercent
	case "memory_percent":
		value = device.MemoryPercent
	case "disk_percent":
		value = device.DiskPercent
	case "status":
		// For status checks, 1 = online, 0 = offline
		if device.Status == "online" {
			value = 1
		} else {
			value = 0
		}
	default:
		return // Unknown metric
	}

	// Check if condition is met
	conditionMet := e.checkCondition(value, rule.Operator, rule.Threshold)

	// Track condition duration
	conditionKey := fmt.Sprintf("%s:%s", device.DeviceID, rule.ID)

	if conditionMet {
		// Check duration requirement
		if rule.DurationSeconds > 0 {
			e.conditionMu.Lock()
			startTime, exists := e.conditionStart[conditionKey]
			if !exists {
				// First time condition is met
				e.conditionStart[conditionKey] = time.Now()
				e.conditionMu.Unlock()
				return
			}

			duration := time.Since(startTime)
			if duration.Seconds() < float64(rule.DurationSeconds) {
				// Condition not sustained long enough
				e.conditionMu.Unlock()
				return
			}
			e.conditionMu.Unlock()
		}

		// Check cooldown
		if !e.checkCooldown(conditionKey, rule.CooldownMinutes) {
			return
		}

		// Check if there's already an open alert for this device+rule
		hasOpenAlert, err := e.hasOpenAlert(ctx, device.DeviceID, rule.ID)
		if err != nil {
			log.Printf("Alert engine: check open alert error: %v", err)
			return
		}
		if hasOpenAlert {
			return
		}

		// Create alert
		alert, err := e.createAlert(ctx, device, rule, value)
		if err != nil {
			log.Printf("Alert engine: create alert error: %v", err)
			return
		}

		// Update cooldown
		e.updateCooldown(conditionKey)

		// Broadcast to dashboards
		e.broadcastAlert(alert, device)

		// Send notifications
		if e.notifier != nil {
			go func() {
				if err := e.notifier.SendAlert(alert, &device, &rule); err != nil {
					log.Printf("Alert engine: notification error: %v", err)
				}
			}()
		}

		log.Printf("Alert created: %s - %s on %s (value: %.2f, threshold: %.2f)",
			rule.Severity, rule.Name, device.Hostname, value, rule.Threshold)

	} else {
		// Condition not met - clear tracking
		e.conditionMu.Lock()
		delete(e.conditionStart, conditionKey)
		e.conditionMu.Unlock()
	}
}

func (e *Engine) checkCondition(value float64, operator string, threshold float64) bool {
	switch operator {
	case "gt":
		return value > threshold
	case "gte":
		return value >= threshold
	case "lt":
		return value < threshold
	case "lte":
		return value <= threshold
	case "eq":
		return value == threshold
	case "neq":
		return value != threshold
	default:
		return false
	}
}

func (e *Engine) checkCooldown(key string, cooldownMinutes int) bool {
	e.lastAlertMu.RLock()
	lastTime, exists := e.lastAlertTime[key]
	e.lastAlertMu.RUnlock()

	if !exists {
		return true
	}

	cooldownDuration := time.Duration(cooldownMinutes) * time.Minute
	return time.Since(lastTime) >= cooldownDuration
}

func (e *Engine) updateCooldown(key string) {
	e.lastAlertMu.Lock()
	e.lastAlertTime[key] = time.Now()
	e.lastAlertMu.Unlock()
}

func (e *Engine) hasOpenAlert(ctx context.Context, deviceID, ruleID uuid.UUID) (bool, error) {
	var count int
	err := e.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM alerts
		WHERE device_id = $1 AND rule_id = $2 AND status = 'open'
	`, deviceID, ruleID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (e *Engine) createAlert(ctx context.Context, device DeviceMetrics, rule AlertRule, value float64) (*Alert, error) {
	alertID := uuid.New()
	title := fmt.Sprintf("%s: %s", rule.Severity, rule.Name)
	message := fmt.Sprintf("%s on %s. Current value: %.2f%%, Threshold: %.2f%%",
		rule.Description, device.Hostname, value, rule.Threshold)

	_, err := e.db.Exec(ctx, `
		INSERT INTO alerts (id, device_id, rule_id, severity, title, message, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'open', NOW())
	`, alertID, device.DeviceID, rule.ID, rule.Severity, title, message)
	if err != nil {
		return nil, fmt.Errorf("insert alert: %w", err)
	}

	return &Alert{
		ID:        alertID,
		DeviceID:  device.DeviceID,
		RuleID:    rule.ID,
		Severity:  rule.Severity,
		Title:     title,
		Message:   message,
		Status:    "open",
		CreatedAt: time.Now(),
	}, nil
}

func (e *Engine) broadcastAlert(alert *Alert, device DeviceMetrics) {
	if e.hub == nil {
		return
	}

	msg, _ := json.Marshal(map[string]interface{}{
		"type": "new_alert",
		"alert": map[string]interface{}{
			"id":        alert.ID,
			"deviceId":  alert.DeviceID,
			"hostname":  device.Hostname,
			"severity":  alert.Severity,
			"title":     alert.Title,
			"message":   alert.Message,
			"status":    alert.Status,
			"createdAt": alert.CreatedAt,
		},
	})

	e.hub.BroadcastToDashboards(msg)
}

// AutoResolveOfflineAlerts resolves device offline alerts when devices come back online
func (e *Engine) AutoResolveOfflineAlerts(ctx context.Context, deviceID uuid.UUID) error {
	_, err := e.db.Exec(ctx, `
		UPDATE alerts
		SET status = 'resolved', resolved_at = NOW()
		WHERE device_id = $1
		  AND status = 'open'
		  AND title LIKE '%Device Offline%'
	`, deviceID)
	return err
}
