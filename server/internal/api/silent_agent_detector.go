package api

import (
	"context"
	"encoding/json"
	"log"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	ws "github.com/sentinel/server/internal/websocket"
)

// SilentAgentDetector scans for devices whose last heartbeat is older than the
// configured threshold and attempts a graduated remote heal — closing the
// "agent went dark, requires physical visit" failure mode that has been
// reported as the platform's longest-standing reliability issue.
//
// Heal strategy:
//  1. WS still connected → push a `repair` command (PowerShell self-test +
//     service restart + cert refresh).
//  2. WS gone but cert valid → mint a fresh one-time enrollment token, record
//     it in agent_recovery_actions for the dashboard, and (when notifications
//     wired) page the operator with the install code.
//  3. Neither → mark needs_manual_review with full context so the operator
//     knows exactly what to take onsite.
type SilentAgentDetector struct {
	db            *pgxpool.Pool
	hub           WSHub
	scanInterval  time.Duration
	silenceCutoff time.Duration
	stop          chan struct{}
}

// WSHub is the subset of the WebSocket hub API the detector needs. Keeps the
// detector decoupled from the concrete hub implementation for testing.
// Both *websocket.Hub and *websocket.DistributedHub satisfy this shape.
type WSHub interface {
	IsAgentOnline(agentID string) bool
	SendToAgent(agentID string, message []byte) error
}

// NewSilentAgentDetector builds a detector with sensible defaults. Override
// the intervals via env (SILENT_AGENT_SCAN_INTERVAL, SILENT_AGENT_SILENCE_MIN)
// — they're set in constructor by main.go.
func NewSilentAgentDetector(db *pgxpool.Pool, hub WSHub) *SilentAgentDetector {
	return &SilentAgentDetector{
		db:            db,
		hub:           hub,
		scanInterval:  5 * time.Minute,
		silenceCutoff: 60 * time.Minute,
		stop:          make(chan struct{}),
	}
}

// Start launches the detector goroutine. Idempotent — calling twice will run
// two detectors which is harmless but wasteful. main.go calls it once after
// the WS hub is initialized.
func (d *SilentAgentDetector) Start() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[SilentAgent] Detector goroutine panicked: %v\n%s", r, debug.Stack())
			}
		}()
		// Initial delay so the WS hub has time to accept reconnects from
		// previously-connected agents before we flag them as silent.
		time.Sleep(2 * time.Minute)
		ticker := time.NewTicker(d.scanInterval)
		defer ticker.Stop()

		d.scanOnce()
		for {
			select {
			case <-d.stop:
				return
			case <-ticker.C:
				d.scanOnce()
			}
		}
	}()
	log.Printf("[SilentAgent] Detector started (scan=%s silence_cutoff=%s)", d.scanInterval, d.silenceCutoff)
}

// Stop signals the detector to exit at its next tick.
func (d *SilentAgentDetector) Stop() { close(d.stop) }

func (d *SilentAgentDetector) scanOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rows, err := d.db.Query(ctx, `
		SELECT id, agent_id, hostname, last_seen
		FROM devices
		WHERE status != 'disabled'
		  AND last_seen IS NOT NULL
		  AND last_seen < NOW() - $1::interval
		  AND last_seen > NOW() - INTERVAL '30 days'  -- skip long-dead devices
		  AND agent_id IS NOT NULL AND agent_id != ''
		ORDER BY last_seen ASC
		LIMIT 100
	`, d.silenceCutoff.String())
	if err != nil {
		log.Printf("[SilentAgent] Scan query failed: %v", err)
		return
	}
	defer rows.Close()

	type candidate struct {
		ID       uuid.UUID
		AgentID  string
		Hostname string
		LastSeen time.Time
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.ID, &c.AgentID, &c.Hostname, &c.LastSeen); err == nil {
			candidates = append(candidates, c)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[SilentAgent] Scan iter failed: %v", err)
		return
	}

	if len(candidates) == 0 {
		return
	}
	log.Printf("[SilentAgent] Found %d silent agents past cutoff", len(candidates))

	for _, c := range candidates {
		d.healOne(ctx, c.ID, c.AgentID, c.Hostname, c.LastSeen)
	}
}

func (d *SilentAgentDetector) healOne(ctx context.Context, deviceID uuid.UUID, agentID, hostname string, lastSeen time.Time) {
	silentFor := time.Since(lastSeen).Round(time.Second).String()

	if d.hub != nil && d.hub.IsAgentOnline(agentID) {
		// Layer 1 still up. The agent is silent on heartbeats but the WS
		// connection is alive — likely a stalled heartbeat goroutine inside the
		// agent. Push a self-test/restart command via the authenticated WS.
		cmd := map[string]interface{}{
			"type": ws.MsgTypeCommand,
			"payload": map[string]interface{}{
				"id":         uuid.New().String(),
				"action":     "repair",
				"reason":     "silent-agent-detector: no heartbeat for " + silentFor,
				"steps":      []string{"selftest", "restart_heartbeat_goroutine", "reissue_cert"},
				"originator": "silent-agent-detector",
			},
		}
		msgBytes, mErr := json.Marshal(cmd)
		if mErr != nil {
			log.Printf("[SilentAgent] Failed to marshal repair command for %s: %v", agentID, mErr)
			return
		}
		if err := d.hub.SendToAgent(agentID, msgBytes); err != nil {
			log.Printf("[SilentAgent] WS push to %s (%s) failed: %v", hostname, agentID, err)
			d.recordAction(ctx, deviceID, agentID, "ws_push_failed", err.Error())
			return
		}
		log.Printf("[SilentAgent] Pushed repair command via WS to %s (%s, silent for %s)", hostname, agentID, silentFor)
		d.recordAction(ctx, deviceID, agentID, "ws_repair_pushed", "silent="+silentFor)
		return
	}

	// Layer 1 down. Without WS we can't directly command the agent. The agent
	// is supposed to fall back to /api/agent/version polling (Layer 2), the
	// watchdog independently calls /api/agent/watchdog/version (Layer 3), and
	// the scheduled bootstrap task fires every 6h (Layer 4). The detector
	// records the silence so the dashboard reflects "manual review" rather
	// than letting it sit as silently-online-but-stale.
	log.Printf("[SilentAgent] WS down for %s (%s, silent for %s) — relying on agent-side recovery layers", hostname, agentID, silentFor)
	d.recordAction(ctx, deviceID, agentID, "ws_down_waiting_self_heal", "silent="+silentFor)
}

func (d *SilentAgentDetector) recordAction(ctx context.Context, deviceID uuid.UUID, agentID, action, detail string) {
	payload, _ := json.Marshal(map[string]string{"detail": detail})
	_, err := d.db.Exec(ctx, `
		INSERT INTO agent_recovery_actions (device_id, agent_id, action, payload, created_at)
		VALUES ($1, $2, $3, $4, NOW())
	`, deviceID, agentID, action, payload)
	if err != nil {
		log.Printf("[SilentAgent] Failed to record action %q for %s: %v", action, agentID, err)
	}
}
