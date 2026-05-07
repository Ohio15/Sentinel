-- Agent centralized log pipeline
-- Stores log entries forwarded from agents for centralized viewing and debugging

CREATE TABLE IF NOT EXISTS agent_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    level VARCHAR(10) NOT NULL DEFAULT 'info',
    source VARCHAR(50) NOT NULL DEFAULT 'agent',
    message TEXT NOT NULL,
    metadata JSONB,
    logged_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_logs_device_id ON agent_logs(device_id);
CREATE INDEX IF NOT EXISTS idx_agent_logs_logged_at ON agent_logs(logged_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_logs_level ON agent_logs(level);
CREATE INDEX IF NOT EXISTS idx_agent_logs_device_level ON agent_logs(device_id, level);
