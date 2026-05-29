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
--
-- IDEMPOTENT UPGRADE NOTE (issue #28):
-- The first cut of this migration crashed in production because an unrelated
-- earlier migration had already created `agent_health` with a totally
-- different column set (health_score, metrics_failures, etc. — no
-- last_check_in, no agent_id, no layer*_* columns). `CREATE TABLE IF NOT
-- EXISTS` was skipped, the ALTER block missed `last_check_in` + `agent_id`,
-- and the subsequent CREATE INDEX on last_check_in failed with column-doesn't-
-- exist. The whole tx rolled back, leaving schema_migrations=(58,dirty=true)
-- and the backend stuck in a restart loop until manual surgery.
--
-- This rewrite makes EVERY column add explicit, adds the UNIQUE constraint
-- via plpgsql DO block (since ADD CONSTRAINT IF NOT EXISTS doesn't exist for
-- table constraints in PostgreSQL), and ensures every CREATE INDEX is
-- reachable regardless of starting state.

-- 1) Create-if-fresh.
CREATE TABLE IF NOT EXISTS agent_health (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id           VARCHAR(128) NOT NULL,
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

-- 2) Upgrade-in-place: every column the heartbeat upsert + the indexes need
--    must exist regardless of what shape the table had before. ADD COLUMN
--    IF NOT EXISTS is the safe form. Listed in the same order as the CREATE
--    TABLE above to keep them in sync.
ALTER TABLE agent_health
    ADD COLUMN IF NOT EXISTS agent_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS device_id UUID,
    ADD COLUMN IF NOT EXISTS last_check_in TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS status VARCHAR(32),
    ADD COLUMN IF NOT EXISTS layer1_ws_uptime_secs BIGINT,
    ADD COLUMN IF NOT EXISTS layer2_last_poll_ok TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS layer3_watchdog_poll_ok TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS layer4_schtask_present BOOLEAN,
    ADD COLUMN IF NOT EXISTS layer5_kill_token_present BOOLEAN,
    ADD COLUMN IF NOT EXISTS mtls_cert_present BOOLEAN,
    ADD COLUMN IF NOT EXISTS raw_payload JSONB DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();

-- 3) UNIQUE constraint on agent_id. The heartbeat upsert uses ON CONFLICT
--    (agent_id), which requires a UNIQUE constraint to exist on the column.
--    PostgreSQL has no ADD CONSTRAINT IF NOT EXISTS for table constraints, so
--    we wrap in a plpgsql DO block that consults pg_constraint first.
--    NULLs are allowed (multiple rows with NULL agent_id wouldn't conflict
--    under PostgreSQL UNIQUE semantics) — adequate because rows the heartbeat
--    writer cares about always carry a non-NULL agent_id.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'agent_health_agent_id_unique'
          AND conrelid = 'agent_health'::regclass
    ) THEN
        -- The constraint might fail if existing rows hold duplicates. In that
        -- case the migration should fail loudly so an operator can resolve
        -- the data before re-running — don't silently weaken the constraint.
        ALTER TABLE agent_health
            ADD CONSTRAINT agent_health_agent_id_unique UNIQUE (agent_id);
    END IF;
END $$;

-- 4) Indexes. Runs AFTER every required column is guaranteed to exist via the
--    ALTER block above, so the first-cut failure mode (index on absent
--    last_check_in) can't recur.
CREATE INDEX IF NOT EXISTS idx_agent_health_last_checkin ON agent_health(last_check_in DESC NULLS LAST);
CREATE INDEX IF NOT EXISTS idx_agent_health_status ON agent_health(status);

-- 5) Recovery audit table — silent-agent detector writes here.
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
