-- Sentinel RMM PostgreSQL Schema
-- Migration: 012_command_queue
-- Add command queue for offline agents and metrics backlog

-- Create command_queue table for offline agents
CREATE TABLE IF NOT EXISTS command_queue (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    command_type VARCHAR(50) NOT NULL,              -- execute_command, check_update, execute_script, collect_diagnostics
    priority INTEGER DEFAULT 50,                     -- 0=highest, 100=lowest
    payload JSONB NOT NULL,                          -- Command-specific data
    status VARCHAR(50) DEFAULT 'queued',             -- queued, delivered, acknowledged, completed, failed, expired, cancelled
    expires_at TIMESTAMP WITH TIME ZONE,             -- NULL = never expires
    max_attempts INTEGER DEFAULT 3,
    attempt_count INTEGER DEFAULT 0,
    last_attempt_at TIMESTAMP WITH TIME ZONE,
    delivered_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    result JSONB,                                    -- Response data from agent
    error_message TEXT,
    created_by VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create metrics_backlog table for offline agent metrics
CREATE TABLE IF NOT EXISTS metrics_backlog (
    id BIGSERIAL PRIMARY KEY,
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    collected_at TIMESTAMP WITH TIME ZONE NOT NULL,  -- Original collection time on agent
    metrics JSONB NOT NULL,
    synced BOOLEAN DEFAULT FALSE,
    synced_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create indexes for efficient queue processing
CREATE INDEX IF NOT EXISTS idx_command_queue_device ON command_queue(device_id);
CREATE INDEX IF NOT EXISTS idx_command_queue_status ON command_queue(status);
CREATE INDEX IF NOT EXISTS idx_command_queue_priority ON command_queue(status, priority, created_at) WHERE status = 'queued';
CREATE INDEX IF NOT EXISTS idx_command_queue_expires ON command_queue(expires_at) WHERE expires_at IS NOT NULL AND status = 'queued';

-- Create indexes for metrics backlog
CREATE INDEX IF NOT EXISTS idx_metrics_backlog_device ON metrics_backlog(device_id);
CREATE INDEX IF NOT EXISTS idx_metrics_backlog_unsynced ON metrics_backlog(device_id, synced) WHERE synced = FALSE;
CREATE INDEX IF NOT EXISTS idx_metrics_backlog_collected ON metrics_backlog(collected_at);

-- Function to expire old queued commands
CREATE OR REPLACE FUNCTION expire_old_commands()
RETURNS INTEGER AS $$
DECLARE
    expired_count INTEGER;
BEGIN
    UPDATE command_queue
    SET status = 'expired', updated_at = NOW()
    WHERE status = 'queued' AND expires_at IS NOT NULL AND expires_at < NOW();
    GET DIAGNOSTICS expired_count = ROW_COUNT;
    RETURN expired_count;
END;
$$ LANGUAGE plpgsql;

-- Function to clean old metrics backlog (call periodically)
CREATE OR REPLACE FUNCTION clean_metrics_backlog(retention_days INTEGER DEFAULT 7)
RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    DELETE FROM metrics_backlog
    WHERE synced = TRUE AND synced_at < NOW() - (retention_days || ' days')::INTERVAL;
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;
