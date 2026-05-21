-- Migration: Add redeemed state to agent_installation_links
--
-- Adds the columns and status value needed for the new single-use signed
-- bootstrap-redeem URL flow (see server/internal/api/install_url_signing.go).
-- The installer fetches a signed redeem URL via /api/public/install/validate-code,
-- then GETs that URL to receive the enrollment token. The atomic UPDATE to
-- status='redeemed' is the single-use guard — a second hit returns 410 Gone.

ALTER TABLE agent_installation_links
    ADD COLUMN IF NOT EXISTS redeemed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS redeemed_ip VARCHAR(45);

-- Relax the status CHECK to allow the new 'redeemed' value. The original
-- constraint is implicitly named — re-create it permissively.
DO $$
DECLARE
    constraint_name TEXT;
BEGIN
    SELECT conname INTO constraint_name
    FROM pg_constraint
    WHERE conrelid = 'agent_installation_links'::regclass
      AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%status%';
    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE agent_installation_links DROP CONSTRAINT %I', constraint_name);
    END IF;
END $$;

ALTER TABLE agent_installation_links
    ADD CONSTRAINT agent_installation_links_status_check
    CHECK (status IN ('pending', 'downloaded', 'redeemed', 'installing', 'installed', 'expired', 'revoked'));

CREATE INDEX IF NOT EXISTS idx_agent_links_redeemed ON agent_installation_links(redeemed_at)
    WHERE redeemed_at IS NOT NULL;

-- Audit-log gap fix: bootstrap downloads were unaudited. Add an artifact column
-- to agent_downloads so a single table can capture installer-template, agent,
-- bootstrap, watchdog, and desktop-helper downloads distinguishably.
ALTER TABLE agent_downloads
    ADD COLUMN IF NOT EXISTS artifact VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_agent_downloads_artifact ON agent_downloads(artifact)
    WHERE artifact IS NOT NULL;
