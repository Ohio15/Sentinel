-- Fix agent_logs column names
-- Migration 008 was initially deployed with 'timestamp' and 'created_at' columns
-- but server code expects 'logged_at' and 'received_at'

-- Rename 'timestamp' to 'logged_at' if it exists
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agent_logs' AND column_name='timestamp') THEN
        ALTER TABLE agent_logs RENAME COLUMN "timestamp" TO logged_at;
    END IF;
END $$;

-- Rename 'created_at' to 'received_at' if it exists
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agent_logs' AND column_name='created_at') THEN
        ALTER TABLE agent_logs RENAME COLUMN created_at TO received_at;
    END IF;
END $$;

-- Ensure the indexes exist with correct names
CREATE INDEX IF NOT EXISTS idx_agent_logs_logged_at ON agent_logs(logged_at DESC);
