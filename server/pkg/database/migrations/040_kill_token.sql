-- Migration: Kill Token for Offline Emergency Uninstall
-- Adds kill_token_hash column to devices table for pre-shared kill token verification.
-- The plaintext token is returned to the agent during enrollment and stored locally.
-- Only the SHA-256 hash is stored server-side.

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'devices') THEN
        IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'devices' AND column_name = 'kill_token_hash') THEN
            ALTER TABLE devices ADD COLUMN kill_token_hash TEXT;
        END IF;
    END IF;
END $$;
