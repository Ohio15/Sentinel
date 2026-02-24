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

	// Evaluate system-generated alerts (no rules needed)
	e.evaluateUpdateFailures(ctx)

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
		INSERT INTO alerts (id, device_id, rule_id, severity, title, message, status, organization_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'open', $7, NOW())
	`, alertID, device.DeviceID, rule.ID, rule.Severity, title, message, e.config.OrganizationID)
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

// createSystemAlert creates an alert not tied to any rule (rule_id = NULL)
func (e *Engine) createSystemAlert(ctx context.Context, deviceID uuid.UUID, hostname, severity, title, message string) (*Alert, error) {
	alertID := uuid.New()

	_, err := e.db.Exec(ctx, `
		INSERT INTO alerts (id, device_id, rule_id, severity, title, message, status, organization_id, created_at)
		VALUES ($1, $2, NULL, $3, $4, $5, 'open', $6, NOW())
	`, alertID, deviceID, severity, title, message, e.config.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("insert system alert: %w", err)
	}

	alert := &Alert{
		ID:        alertID,
		DeviceID:  deviceID,
		Severity:  severity,
		Title:     title,
		Message:   message,
		Status:    "open",
		CreatedAt: time.Now(),
	}

	// Broadcast to dashboards
	e.broadcastAlert(alert, DeviceMetrics{DeviceID: deviceID, Hostname: hostname})

	// Send notifications (no rule context for system alerts)
	if e.notifier != nil {
		device := &DeviceMetrics{DeviceID: deviceID, Hostname: hostname}
		go func() {
			if err := e.notifier.SendAlert(alert, device, nil); err != nil {
				log.Printf("Alert engine: system alert notification error: %v", err)
			}
		}()
	}

	return alert, nil
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

// AutoResolveUpdateLoopAlerts resolves update-related alerts when an agent successfully updates
func (e *Engine) AutoResolveUpdateLoopAlerts(ctx context.Context, agentID string) error {
	result, err := e.db.Exec(ctx, `
		UPDATE alerts
		SET status = 'resolved', resolved_at = NOW()
		WHERE device_id = (SELECT id FROM devices WHERE agent_id = $1 LIMIT 1)
		  AND status = 'open'
		  AND (title LIKE '%Update Loop%' OR title LIKE '%Download Failed%' OR title LIKE '%Rolled Back%')
	`, agentID)
	if err != nil {
		return err
	}

	if result.RowsAffected() > 0 {
		log.Printf("Alert engine: auto-resolved update loop alert for agent %s", agentID)

		// Broadcast resolution to dashboards
		if e.hub != nil {
			var deviceID uuid.UUID
			var hostname string
			_ = e.db.QueryRow(ctx, `SELECT id, COALESCE(hostname, '') FROM devices WHERE agent_id = $1`, agentID).Scan(&deviceID, &hostname)

			msg, _ := json.Marshal(map[string]interface{}{
				"type": "alert_resolved",
				"alert": map[string]interface{}{
					"deviceId": deviceID,
					"hostname": hostname,
					"title":    "Agent Update Loop Resolved",
					"status":   "resolved",
				},
			})
			e.hub.BroadcastToDashboards(msg)
		}
	}

	return nil
}

// stuckUpdateAgent represents an agent stuck in an update loop
type stuckUpdateAgent struct {
	AgentID      string
	DeviceID     uuid.UUID
	Hostname     string
	ToVersion    string
	AttemptCount int
	LastError    string
	FirstAttempt time.Time
	LastAttempt  time.Time
}

// evaluateUpdateFailures detects agents stuck in failed update loops
func (e *Engine) evaluateUpdateFailures(ctx context.Context) {
	rows, err := e.db.Query(ctx, `
		SELECT au.agent_id, d.id as device_id, COALESCE(d.hostname, ''), au.to_version,
		       COUNT(*) as attempt_count,
		       COALESCE(MAX(au.error_message), '') as last_error,
		       MIN(au.created_at) as first_attempt,
		       MAX(au.created_at) as last_attempt
		FROM agent_updates au
		JOIN devices d ON d.agent_id = au.agent_id
		WHERE au.status IN ('downloading', 'failed')
		  AND au.created_at > NOW() - INTERVAL '2 hours'
		  AND au.organization_id = $1
		  AND NOT EXISTS (
		    SELECT 1 FROM agent_updates au2
		    WHERE au2.agent_id = au.agent_id
		      AND au2.to_version = au.to_version
		      AND au2.status = 'completed'
		  )
		GROUP BY au.agent_id, d.id, d.hostname, au.to_version
		HAVING COUNT(*) >= 3
	`, e.config.OrganizationID)
	if err != nil {
		log.Printf("Alert engine: failed to query update failures: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var s stuckUpdateAgent
		if err := rows.Scan(&s.AgentID, &s.DeviceID, &s.Hostname, &s.ToVersion,
			&s.AttemptCount, &s.LastError, &s.FirstAttempt, &s.LastAttempt); err != nil {
			log.Printf("Alert engine: scan update failure error: %v", err)
			continue
		}

		// Check cooldown (60 minutes per device)
		cooldownKey := fmt.Sprintf("update_loop:%s", s.DeviceID)
		if !e.checkCooldown(cooldownKey, 60) {
			continue
		}

		// Check if there's already an open update loop alert for this device
		var openCount int
		err := e.db.QueryRow(ctx, `
			SELECT COUNT(*) FROM alerts
			WHERE device_id = $1 AND status = 'open' AND title LIKE '%Update Loop%'
		`, s.DeviceID).Scan(&openCount)
		if err != nil {
			log.Printf("Alert engine: check existing update loop alert error: %v", err)
			continue
		}
		if openCount > 0 {
			continue
		}

		// Build alert message
		title := "critical: Agent Update Loop Detected"
		message := fmt.Sprintf(`Agent on %s is stuck in a failed update loop.

Cause: %d failed attempts to update to version %s in the last 2 hours. Last error: %s.
This causes repeated large binary downloads (~25MB each), consuming bandwidth and server resources.

Resolution:
1. Manually update the agent on this device to the latest version
2. Check the device's network connectivity and firewall rules
3. Verify the agent binary at installers/sentinel-agent-[platform]-[arch] is not corrupted
4. Review agent logs at C:\ProgramData\Sentinel\sentinel-agent.log (Windows) or /var/log/sentinel/agent.log (Linux)`,
			s.Hostname, s.AttemptCount, s.ToVersion, s.LastError)

		alert, err := e.createSystemAlert(ctx, s.DeviceID, s.Hostname, "critical", title, message)
		if err != nil {
			log.Printf("Alert engine: failed to create update loop alert: %v", err)
			continue
		}

		e.updateCooldown(cooldownKey)

		log.Printf("Alert engine: update loop detected for agent %s (%s) - %d failed attempts to version %s",
			s.AgentID, s.Hostname, s.AttemptCount, s.ToVersion)

		_ = alert // alert already broadcast and notified by createSystemAlert
	}
}
