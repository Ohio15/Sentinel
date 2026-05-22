package api

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LayerState is the recovery-layer state agents report inside heartbeats so
// the server can populate agent_health for monitoring and the silent-agent
// detector can decide which fallback to push. All fields are optional — older
// agents without layer-state instrumentation simply send nothing.
type LayerState struct {
	Layer1WSUptimeSecs     *int64     `json:"layer1_ws_uptime_secs,omitempty"`
	Layer2LastPollOK       *time.Time `json:"layer2_last_poll_ok,omitempty"`
	Layer3WatchdogPollOK   *time.Time `json:"layer3_watchdog_poll_ok,omitempty"`
	Layer4SchtaskPresent   *bool      `json:"layer4_schtask_present,omitempty"`
	Layer5KillTokenPresent *bool      `json:"layer5_kill_token_present,omitempty"`
	MTLSCertPresent        *bool      `json:"mtls_cert_present,omitempty"`
}

// upsertAgentHealth writes the agent's recovery-layer state into agent_health.
// Called from the heartbeat handler. Best-effort: write failures log but don't
// fail the heartbeat path. Schema: see migration 000058.
func upsertAgentHealth(ctx context.Context, db *pgxpool.Pool, agentID string, deviceID uuid.UUID, status string, ls *LayerState) {
	if agentID == "" {
		return
	}
	if ls == nil {
		ls = &LayerState{}
	}
	rawJSON, _ := json.Marshal(ls)
	_, err := db.Exec(ctx, `
		INSERT INTO agent_health (
			agent_id, device_id, last_check_in, status,
			layer1_ws_uptime_secs, layer2_last_poll_ok, layer3_watchdog_poll_ok,
			layer4_schtask_present, layer5_kill_token_present, mtls_cert_present,
			raw_payload, updated_at
		) VALUES ($1, $2, NOW(), $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		ON CONFLICT (agent_id) DO UPDATE SET
			device_id = COALESCE(EXCLUDED.device_id, agent_health.device_id),
			last_check_in = EXCLUDED.last_check_in,
			status = EXCLUDED.status,
			layer1_ws_uptime_secs = COALESCE(EXCLUDED.layer1_ws_uptime_secs, agent_health.layer1_ws_uptime_secs),
			layer2_last_poll_ok = COALESCE(EXCLUDED.layer2_last_poll_ok, agent_health.layer2_last_poll_ok),
			layer3_watchdog_poll_ok = COALESCE(EXCLUDED.layer3_watchdog_poll_ok, agent_health.layer3_watchdog_poll_ok),
			layer4_schtask_present = COALESCE(EXCLUDED.layer4_schtask_present, agent_health.layer4_schtask_present),
			layer5_kill_token_present = COALESCE(EXCLUDED.layer5_kill_token_present, agent_health.layer5_kill_token_present),
			mtls_cert_present = COALESCE(EXCLUDED.mtls_cert_present, agent_health.mtls_cert_present),
			raw_payload = EXCLUDED.raw_payload,
			updated_at = NOW()
	`,
		agentID, deviceID, status,
		ls.Layer1WSUptimeSecs, ls.Layer2LastPollOK, ls.Layer3WatchdogPollOK,
		ls.Layer4SchtaskPresent, ls.Layer5KillTokenPresent, ls.MTLSCertPresent,
		rawJSON,
	)
	if err != nil {
		log.Printf("[agent_health] upsert failed for %s: %v", agentID, err)
	}
}
