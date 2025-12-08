-- Sentinel RMM PostgreSQL Schema
-- Migration: 001_initial_schema

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Devices table
CREATE TABLE IF NOT EXISTS devices (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    agent_id VARCHAR(255) UNIQUE NOT NULL,
    hostname VARCHAR(255),
    display_name VARCHAR(255),
    os_type VARCHAR(50),
    os_version VARCHAR(100),
    os_build VARCHAR(100),
    architecture VARCHAR(50),
    agent_version VARCHAR(50),
    last_seen TIMESTAMP WITH TIME ZONE,
    status VARCHAR(20) DEFAULT 'offline',
    ip_address VARCHAR(45),
    mac_address VARCHAR(17),
    tags JSONB DEFAULT '[]'::jsonb,
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Device metrics table (time-series data)
CREATE TABLE IF NOT EXISTS device_metrics (
    id BIGSERIAL PRIMARY KEY,
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    cpu_percent REAL,
    memory_percent REAL,
    memory_used_bytes BIGINT,
    disk_percent REAL,
    disk_used_bytes BIGINT,
    network_rx_bytes BIGINT,
    network_tx_bytes BIGINT,
    process_count INTEGER
);

-- Commands table
CREATE TABLE IF NOT EXISTS commands (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    command_type VARCHAR(50) NOT NULL,
    command TEXT NOT NULL,
    status VARCHAR(20) DEFAULT 'pending',
    output TEXT,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE
);

-- Scripts table
CREATE TABLE IF NOT EXISTS scripts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    language VARCHAR(50) NOT NULL,
    content TEXT NOT NULL,
    os_types JSONB DEFAULT '[]'::jsonb,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Alerts table
CREATE TABLE IF NOT EXISTS alerts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    rule_id UUID,
    severity VARCHAR(20) NOT NULL,
    title VARCHAR(255) NOT NULL,
    message TEXT,
    status VARCHAR(20) DEFAULT 'open',
    acknowledged_at TIMESTAMP WITH TIME ZONE,
    resolved_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Alert rules table
CREATE TABLE IF NOT EXISTS alert_rules (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    enabled BOOLEAN DEFAULT TRUE,
    metric VARCHAR(50) NOT NULL,
    operator VARCHAR(10) NOT NULL,
    threshold REAL NOT NULL,
    severity VARCHAR(20) NOT NULL,
    cooldown_minutes INTEGER DEFAULT 15,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Settings table (key-value store)
CREATE TABLE IF NOT EXISTS settings (
    key VARCHAR(255) PRIMARY KEY,
    value TEXT NOT NULL
);

-- Create indexes for common queries
CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status);
CREATE INDEX IF NOT EXISTS idx_devices_last_seen ON devices(last_seen DESC);
CREATE INDEX IF NOT EXISTS idx_devices_agent_id ON devices(agent_id);

CREATE INDEX IF NOT EXISTS idx_metrics_device_timestamp ON device_metrics(device_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON device_metrics(timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_commands_device_status ON commands(device_id, status);
CREATE INDEX IF NOT EXISTS idx_commands_created_at ON commands(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts(status);
CREATE INDEX IF NOT EXISTS idx_alerts_device_id ON alerts(device_id);
CREATE INDEX IF NOT EXISTS idx_alerts_created_at ON alerts(created_at DESC);

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Triggers for updated_at
DROP TRIGGER IF EXISTS update_devices_updated_at ON devices;
CREATE TRIGGER update_devices_updated_at
    BEFORE UPDATE ON devices
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_scripts_updated_at ON scripts;
CREATE TRIGGER update_scripts_updated_at
    BEFORE UPDATE ON scripts
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Insert default settings if not exists
INSERT INTO settings (key, value) VALUES
    ('serverPort', '8080'),
    ('agentCheckInterval', '30'),
    ('metricsRetentionDays', '30'),
    ('alertEmailEnabled', 'false'),
    ('alertEmail', ''),
    ('theme', 'system')
ON CONFLICT (key) DO NOTHING;

-- Insert default alert rules if table is empty
INSERT INTO alert_rules (name, description, metric, operator, threshold, severity, cooldown_minutes)
SELECT * FROM (VALUES
    ('High CPU Usage', 'Alert when CPU usage exceeds 90%', 'cpu_percent', 'gt', 90::REAL, 'warning', 15),
    ('Critical CPU Usage', 'Alert when CPU usage exceeds 95%', 'cpu_percent', 'gt', 95::REAL, 'critical', 5),
    ('High Memory Usage', 'Alert when memory usage exceeds 85%', 'memory_percent', 'gt', 85::REAL, 'warning', 15),
    ('Low Disk Space', 'Alert when disk usage exceeds 90%', 'disk_percent', 'gt', 90::REAL, 'critical', 60)
) AS defaults(name, description, metric, operator, threshold, severity, cooldown_minutes)
WHERE NOT EXISTS (SELECT 1 FROM alert_rules LIMIT 1);
