-- Revert: drop redeem columns and restore original status CHECK constraint.

DROP INDEX IF EXISTS idx_agent_links_redeemed;

ALTER TABLE agent_installation_links
    DROP CONSTRAINT IF EXISTS agent_installation_links_status_check;

ALTER TABLE agent_installation_links
    ADD CONSTRAINT agent_installation_links_status_check
    CHECK (status IN ('pending', 'downloaded', 'installing', 'installed', 'expired', 'revoked'));

ALTER TABLE agent_installation_links
    DROP COLUMN IF EXISTS redeemed_ip,
    DROP COLUMN IF EXISTS redeemed_at;

DROP INDEX IF EXISTS idx_agent_downloads_artifact;
ALTER TABLE agent_downloads DROP COLUMN IF EXISTS artifact;
