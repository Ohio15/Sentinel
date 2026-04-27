-- Migration: 041_webhooks_fix_org_id_type.sql
-- Purpose: Fix webhooks.organization_id column type from UUID to INTEGER.
--
-- Background: 030_webhooks.sql shipped with organization_id declared as UUID NOT NULL
-- REFERENCES organizations(id). The organizations.id column is SERIAL PRIMARY KEY
-- (INTEGER), so the foreign-key reference has been type-mismatched since 030 landed.
-- Any INSERT into webhooks would fail at runtime against a real organizations row.
--
-- This migration converts the column to INTEGER and re-establishes the FK. It is
-- idempotent: re-runs are a no-op once the column is INTEGER.
--
-- Note: The original 030_webhooks.sql edit attempted to fix this in-place, but 030
-- has already shipped to production. Editing a shipped migration in place is unsafe
-- because deployments past v041 would skip 030 entirely (currentVersion >= 41).
-- This forward-only fix is the correct mechanism.

DO $$
DECLARE
    col_type TEXT;
BEGIN
    -- Only run if webhooks table exists and column is still UUID
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'webhooks') THEN
        SELECT data_type INTO col_type
        FROM information_schema.columns
        WHERE table_name = 'webhooks' AND column_name = 'organization_id';

        IF col_type = 'uuid' THEN
            -- Drop any existing FK constraint referencing the bad-type column
            ALTER TABLE webhooks DROP CONSTRAINT IF EXISTS webhooks_organization_id_fkey;

            -- Convert UUID -> INTEGER. Webhooks rows with non-numeric UUIDs would be
            -- corrupt anyway (FK never resolved), so a forced cast via TRUNCATE-or-USING
            -- is appropriate. Empty tables convert with no rows; populated tables
            -- (which can only exist if FK was disabled) should be cleared first.
            ALTER TABLE webhooks
                ALTER COLUMN organization_id TYPE INTEGER USING NULL;

            -- Set NOT NULL via default 1 (Default Organization) for any historical rows
            UPDATE webhooks SET organization_id = 1 WHERE organization_id IS NULL;
            ALTER TABLE webhooks ALTER COLUMN organization_id SET NOT NULL;

            -- Re-establish the FK against organizations(id)
            ALTER TABLE webhooks
                ADD CONSTRAINT webhooks_organization_id_fkey
                FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE CASCADE;
        END IF;
    END IF;
END $$;
