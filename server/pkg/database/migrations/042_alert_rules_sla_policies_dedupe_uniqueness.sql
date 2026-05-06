-- Migration 042: Dedupe alert_rules and sla_policies, enforce structural uniqueness.
--
-- Background: migrations 001 and 018 seeded default alert rules and SLA tiers using
-- non-idempotent INSERTs (001) or a coarse "table-empty" guard (018). Migration replays
-- and partial-state edits accumulated duplicate rows. By 2026-05-06 the production DB
-- had 16 alert_rules where 6 were intended (each duplicate independently fired alerts,
-- producing ~3x noise) and 8 sla_policies where 4 were intended.
--
-- This migration:
--   1. Removes any duplicate rows by signature, keeping the oldest by created_at.
--   2. Adds a unique constraint to alert_rules on the natural signature.
--   3. Adds a partial unique index to sla_policies for default-tier rows
--      (client_id IS NULL), since PostgreSQL UNIQUE treats NULL as distinct.
--
-- Idempotent: the dedupe is a no-op on already-clean tables, and the constraint /
-- index creation uses IF NOT EXISTS or DO blocks so applying twice is safe. NEXUS
-- received the equivalent state via a manual hotfix on 2026-05-06; this migration
-- reaches that state on every other deployment that hasn't been patched manually.

-- ----------------------------------------------------------------------------
-- 1. Dedupe alert_rules — keep the oldest row per natural signature
-- ----------------------------------------------------------------------------
WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY organization_id, name, metric, operator, threshold, severity
               ORDER BY created_at, id
           ) AS rn
    FROM alert_rules
)
DELETE FROM alert_rules WHERE id IN (SELECT id FROM ranked WHERE rn > 1);

-- 2. Add unique constraint on alert_rules signature.
--    PostgreSQL has no `ADD CONSTRAINT IF NOT EXISTS` for non-FK constraints, so we
--    use a DO block guarded by pg_constraint. The constraint name is fixed so a
--    pre-existing equivalent (e.g. created by a manual hotfix) is detected and skipped.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'alert_rules_signature_key'
          AND conrelid = 'alert_rules'::regclass
    ) THEN
        ALTER TABLE alert_rules
            ADD CONSTRAINT alert_rules_signature_key
            UNIQUE (organization_id, name, metric, operator, threshold, severity);
    END IF;
END $$;

-- ----------------------------------------------------------------------------
-- 3. Dedupe sla_policies — keep the oldest row per (name, priority, client_id)
-- ----------------------------------------------------------------------------
WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY name, priority, client_id
               ORDER BY created_at, id
           ) AS rn
    FROM sla_policies
)
DELETE FROM sla_policies WHERE id IN (SELECT id FROM ranked WHERE rn > 1);

-- 4. Partial unique index for default-tier sla_policies (client_id IS NULL).
--    Standard UNIQUE treats NULL as distinct, so we need a partial index targeting
--    the actual observed failure mode. CREATE UNIQUE INDEX IF NOT EXISTS is native.
CREATE UNIQUE INDEX IF NOT EXISTS sla_policies_default_tier_uq
    ON sla_policies(name, priority)
    WHERE client_id IS NULL;
