-- 000055_rollouts_mvp.up.sql
--
-- Phase 6 of the agent-update saga (v1.77.30). Promotes the existing
-- rollouts/rollout_stages/rollout_devices/rollout_events schema (000022)
-- from "designed but unused" to a working MVP. Adds the columns the
-- backend handlers need to dispatch, dedup, and finalise rollouts, and
-- relaxes a constraint that blocks ad-hoc targeting.
--
-- Idempotent: every column add and constraint change is guarded by an
-- information_schema or pg_constraint lookup so re-running is a no-op.
--
-- Schema additions vs. 000022:
--   rollouts.organization_id           multi-tenant scoping (FK to organizations)
--   rollouts.mode                      'immediate' (MVP) | future 'staged'
--   rollouts.channel                   'stable'    (MVP) | future 'beta','canary'
--   rollouts.target_type               'all-online' | 'device-list' | 'update-group'
--   rollouts.target_spec               JSONB; e.g. {"device_ids":[...]} or {}
--   rollouts.target_hash               sha256 of canonicalised target — idempotency key
--   rollouts.failure_threshold_percent rollout fails if more than this % of devices fail
--   rollout_stages.group_id            relaxed to NULLABLE: NULL = ad-hoc stage (MVP)
--   rollout_devices.dispatched_at      distinct from started_at; ack-delivery moment

-- ---- rollouts: new columns ------------------------------------------------
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'rollouts' AND column_name = 'organization_id') THEN
        ALTER TABLE rollouts ADD COLUMN organization_id INTEGER NOT NULL DEFAULT 1
            REFERENCES organizations(id);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'rollouts' AND column_name = 'mode') THEN
        ALTER TABLE rollouts ADD COLUMN mode VARCHAR(20) NOT NULL DEFAULT 'immediate';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'rollouts' AND column_name = 'channel') THEN
        ALTER TABLE rollouts ADD COLUMN channel VARCHAR(20) NOT NULL DEFAULT 'stable';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'rollouts' AND column_name = 'target_type') THEN
        ALTER TABLE rollouts ADD COLUMN target_type VARCHAR(20) NOT NULL DEFAULT 'device-list';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'rollouts' AND column_name = 'target_spec') THEN
        ALTER TABLE rollouts ADD COLUMN target_spec JSONB NOT NULL DEFAULT '{}'::jsonb;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'rollouts' AND column_name = 'target_hash') THEN
        ALTER TABLE rollouts ADD COLUMN target_hash VARCHAR(64) NOT NULL DEFAULT '';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'rollouts' AND column_name = 'failure_threshold_percent') THEN
        ALTER TABLE rollouts ADD COLUMN failure_threshold_percent REAL NOT NULL DEFAULT 20.0;
    END IF;
END $$;

-- ---- rollout_stages.group_id: relax NOT NULL -----------------------------
-- 000022 made group_id mandatory because it modeled stages-of-update-groups.
-- MVP needs ad-hoc stages (group_id IS NULL) to support all-online / device-list
-- targeting. NULLability is forward-compatible: a future staged-mode rollout
-- can still set group_id to a real update_groups row.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'rollout_stages'
          AND column_name = 'group_id'
          AND is_nullable = 'NO'
    ) THEN
        ALTER TABLE rollout_stages ALTER COLUMN group_id DROP NOT NULL;
    END IF;
END $$;

-- ---- rollout_devices: dispatched_at column -------------------------------
-- started_at = agent began applying (set when status leaves 'pending')
-- dispatched_at = ack told agent to update (set when heartbeat-ack served)
-- These can drift: an agent may receive an ack and then take a while to act.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'rollout_devices' AND column_name = 'dispatched_at') THEN
        ALTER TABLE rollout_devices ADD COLUMN dispatched_at TIMESTAMP WITH TIME ZONE;
    END IF;
END $$;

-- ---- indexes -------------------------------------------------------------

-- Idempotency dedup: at most one in-flight rollout per (org, version, target_hash).
-- Partial index restricts the uniqueness to active-ish rollouts so a fleet can
-- re-target the same version after the previous campaign has completed/cancelled.
CREATE UNIQUE INDEX IF NOT EXISTS rollouts_idem_active
    ON rollouts (organization_id, release_version, target_hash)
    WHERE status IN ('pending', 'active', 'paused');

-- Heartbeat-ack hot path: per-device pending lookup runs on every heartbeat for
-- every device. Partial index on status='pending' keeps the index tiny and the
-- lookup index-only-scannable.
CREATE INDEX IF NOT EXISTS idx_rollout_devices_pending_lookup
    ON rollout_devices (device_id, status)
    WHERE status = 'pending';

-- Organization scope filter for list endpoints.
CREATE INDEX IF NOT EXISTS idx_rollouts_org_status
    ON rollouts (organization_id, status);

-- Backfill organization_id for any rollouts created prior to this migration
-- (defensive — DEFAULT 1 already covers existing rows; this catches NULLs that
-- could exist on databases where the column was added without DEFAULT).
UPDATE rollouts SET organization_id = 1 WHERE organization_id IS NULL;
