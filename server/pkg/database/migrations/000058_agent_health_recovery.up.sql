-- Migration 000058: agent_health pipeline + recovery audit log
--
-- Two related additions for the recovery-hardening work (PR #18):
--
-- 1) agent_health rows. Schema may already exist (referenced by code) — this
--    migration creates it idempotently and adds the columns the new
--    heartbeat-payload layer-state writer needs. Populated by heartbeat
--    handler on every agent check-in. Empty until 2026-05-22 — surfaced as a
--    blind spot in the recovery-layer audit.
--
-- 2) agent_recovery_actions audit table. The silent-agent detector writes a
--    row each time it decides on an action (WS repair push, fallback wait,
--    manual review needed). Backs the dashboard's "recovery feed" so the
--    operator sees exactly which agents needed automated help and which need
--    onsite intervention.

CREATE TABLE IF NOT EXISTS agent_health (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id           VARCHAR(128) UNIQUE NOT NULL,
    device_id          UUID,
    last_check_in      TIMESTAMPTZ,
    status             VARCHAR(32),

    -- Per-layer state, reported by the agent in its heartbeat payload.
    -- NULL means the agent didn't report it (older agent or boot incomplete).
    layer1_ws_uptime_secs    BIGINT,
    layer2_last_poll_ok      TIMESTAMPTZ,
    layer3_watchdog_poll_ok  TIMESTAMPTZ,
    layer4_schtask_present   BOOLEAN,
    layer5_kill_token_present BOOLEAN,
    mtls_cert_present        BOOLEAN,

    raw_payload        JSONB DEFAULT '{}'::jsonb,
    updated_at         TIMESTAMPTZ DEFAULT NOW()
);

-- Future-proof existing rows (if the table existed without these columns).
ALTER TABLE agent_health
    ADD COLUMN IF NOT EXISTS device_id UUID,
    ADD COLUMN IF NOT EXISTS layer1_ws_uptime_secs BIGINT,
    ADD COLUMN IF NOT EXISTS layer2_last_poll_ok TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS layer3_watchdog_poll_ok TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS layer4_schtask_present BOOLEAN,
    ADD COLUMN IF NOT EXISTS layer5_kill_token_present BOOLEAN,
    ADD COLUMN IF NOT EXISTS mtls_cert_present BOOLEAN,
    ADD COLUMN IF NOT EXISTS raw_payload JSONB DEFAULT '{}'::jsonb;

CREATE INDEX IF NOT EXISTS idx_agent_health_last_checkin ON agent_health(last_check_in DESC NULLS LAST);
CREATE INDEX IF NOT EXISTS idx_agent_health_status ON agent_health(status);

CREATE TABLE IF NOT EXISTS agent_recovery_actions (
    id          BIGSERIAL PRIMARY KEY,
    device_id   UUID,
    agent_id    VARCHAR(128) NOT NULL,
    action      VARCHAR(64) NOT NULL,
    payload     JSONB DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_recovery_actions_agent ON agent_recovery_actions(agent_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_recovery_actions_created ON agent_recovery_actions(created_at DESC);
