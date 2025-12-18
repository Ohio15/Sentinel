-- Sentinel RMM PostgreSQL Schema
-- Migration: 013_agent_health
-- Add agent health scoring and tracking

-- Create agent_health table for current health status
CREATE TABLE IF NOT EXISTS agent_health (
    id SERIAL PRIMARY KEY,
    device_id UUID NOT NULL UNIQUE REFERENCES devices(id) ON DELETE CASCADE,
    health_score INTEGER NOT NULL DEFAULT 100,        -- 0-100, higher is better
    status VARCHAR(20) DEFAULT 'unknown',             -- healthy, degraded, unhealthy, unknown
    last_heartbeat TIMESTAMP WITH TIME ZONE,
    heartbeat_failures INTEGER DEFAULT 0,
    last_metrics TIMESTAMP WITH TIME ZONE,
    metrics_failures INTEGER DEFAULT 0,
    last_update_check TIMESTAMP WITH TIME ZONE,
    update_failures INTEGER DEFAULT 0,
    last_command_success TIMESTAMP WITH TIME ZONE,
    command_failures INTEGER DEFAULT 0,
    connection_stability REAL DEFAULT 100.0,          -- % of expected heartbeats received
    avg_response_time_ms INTEGER,                     -- Average command response time
    components JSONB DEFAULT '{}',                    -- Individual component health
    factors JSONB DEFAULT '{}',                       -- Breakdown of score factors
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create agent_health_history table for trending
CREATE TABLE IF NOT EXISTS agent_health_history (
    id BIGSERIAL PRIMARY KEY,
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    health_score INTEGER NOT NULL,
    status VARCHAR(20),
    factors JSONB,
    recorded_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_agent_health_device ON agent_health(device_id);
CREATE INDEX IF NOT EXISTS idx_agent_health_score ON agent_health(health_score);
CREATE INDEX IF NOT EXISTS idx_agent_health_status ON agent_health(status);
CREATE INDEX IF NOT EXISTS idx_agent_health_history_device ON agent_health_history(device_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_health_history_recorded ON agent_health_history(recorded_at DESC);

-- Function to record health history (call periodically or on significant changes)
CREATE OR REPLACE FUNCTION record_health_snapshot()
RETURNS INTEGER AS $$
DECLARE
    inserted_count INTEGER;
BEGIN
    INSERT INTO agent_health_history (device_id, health_score, status, factors)
    SELECT device_id, health_score, status, factors
    FROM agent_health;
    GET DIAGNOSTICS inserted_count = ROW_COUNT;
    RETURN inserted_count;
END;
$$ LANGUAGE plpgsql;

-- Function to clean old health history (call periodically)
CREATE OR REPLACE FUNCTION clean_health_history(retention_days INTEGER DEFAULT 30)
RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    DELETE FROM agent_health_history
    WHERE recorded_at < NOW() - (retention_days || ' days')::INTERVAL;
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;

-- Function to calculate health status from score
CREATE OR REPLACE FUNCTION get_health_status(score INTEGER)
RETURNS VARCHAR(20) AS $$
BEGIN
    IF score >= 80 THEN
        RETURN 'healthy';
    ELSIF score >= 50 THEN
        RETURN 'degraded';
    ELSIF score > 0 THEN
        RETURN 'unhealthy';
    ELSE
        RETURN 'unknown';
    END IF;
END;
$$ LANGUAGE plpgsql;
