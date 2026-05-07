-- Migration: Installation Codes for Agent Deployment
-- Adds installation code support for no-email agent installation workflow

-- Add installation_code column to existing table
ALTER TABLE agent_installation_links
ADD COLUMN IF NOT EXISTS installation_code VARCHAR(10) UNIQUE;

-- Make user_email optional (no longer required for code-based installation)
ALTER TABLE agent_installation_links
ALTER COLUMN user_email DROP NOT NULL;

-- Create index for fast code lookups
CREATE INDEX IF NOT EXISTS idx_agent_links_installation_code ON agent_installation_links(installation_code) WHERE installation_code IS NOT NULL;

-- Generate codes for existing records that don't have one
-- Format: XXXX-XXXX using uppercase alphanumeric (excluding ambiguous chars 0,O,1,I,L)
DO $$
DECLARE
    chars TEXT := 'ABCDEFGHJKMNPQRSTUVWXYZ23456789';
    rec RECORD;
    new_code TEXT;
    code_exists BOOLEAN;
BEGIN
    FOR rec IN SELECT id FROM agent_installation_links WHERE installation_code IS NULL AND deleted_at IS NULL LOOP
        LOOP
            -- Generate random 8-char code
            new_code := '';
            FOR i IN 1..8 LOOP
                new_code := new_code || substr(chars, floor(random() * length(chars) + 1)::int, 1);
            END LOOP;
            -- Format as XXXX-XXXX
            new_code := substr(new_code, 1, 4) || '-' || substr(new_code, 5, 4);

            -- Check if code already exists
            SELECT EXISTS(SELECT 1 FROM agent_installation_links WHERE installation_code = new_code) INTO code_exists;

            EXIT WHEN NOT code_exists;
        END LOOP;

        UPDATE agent_installation_links SET installation_code = new_code WHERE id = rec.id;
    END LOOP;
END $$;

-- Add comments for documentation
COMMENT ON COLUMN agent_installation_links.installation_code IS 'Short user-friendly code for installation (format: XXXX-XXXX)';
