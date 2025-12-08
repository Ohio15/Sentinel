-- Sentinel RMM Initial Schema

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Users table
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    role VARCHAR(50) DEFAULT 'viewer' CHECK (role IN ('admin', 'operator', 'viewer')),
    is_active BOOLEAN DEFAULT TRUE,
    last_login TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Create default admin user (password: admin123 - CHANGE IN PRODUCTION)
INSERT INTO users (email, password_hash, first_name, last_name, role) VALUES
    ('admin@sentinel.local', '$2b$10$HF.QSMYbYa6XVlKdwZ88juRwQ4zSK1CN8q8JZTsYTM5W3KCyjpQxy', 'Admin', 'User', 'admin');

-- Devices table
CREATE TABLE devices (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    agent_id VARCHAR(255) UNIQUE NOT NULL,
    hostname VARCHAR(255),
    display_name VARCHAR(255),
    os_type VARCHAR(50),
    os_version VARCHAR(100),
    os_build VARCHAR(100),
    architecture VARCHAR(20),
    agent_version VARCHAR(50),
    last_seen TIMESTAMPTZ,
    status VARCHAR(50) DEFAULT 'offline' CHECK (status IN ('online', 'offline', 'warning', 'critical')),
    ip_address INET,
    public_ip INET,
    mac_address VARCHAR(17),
    tags TEXT[] DEFAULT '{}',
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Device metrics table (time-series data)
CREATE TABLE device_metrics (
    id BIGSERIAL,
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    timestamp TIMESTAMPTZ DEFAULT NOW(),
    cpu_percent REAL,
    memory_percent REAL,
    memory_used_bytes BIGINT,
    memory_total_bytes BIGINT,
    disk_percent REAL,
    disk_used_bytes BIGINT,
    disk_total_bytes BIGINT,
    network_rx_bytes BIGINT,
    network_tx_bytes BIGINT,
    process_count INTEGER,
    PRIMARY KEY (device_id, timestamp)
);

-- Create index for efficient time-range queries
CREATE INDEX idx_device_metrics_timestamp ON device_metrics(device_id, timestamp DESC);

-- Commands table
CREATE TABLE commands (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    command_type VARCHAR(50) NOT NULL,
    command TEXT NOT NULL,
    status VARCHAR(50) DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed', 'timeout')),
    output TEXT,
    error_message TEXT,
    exit_code INTEGER,
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_commands_device_status ON commands(device_id, status);
CREATE INDEX idx_commands_created_at ON commands(created_at DESC);

-- Scripts table
CREATE TABLE scripts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    language VARCHAR(50) NOT NULL CHECK (language IN ('powershell', 'bash', 'python', 'batch')),
    content TEXT NOT NULL,
    os_types TEXT[] DEFAULT '{}',
    parameters JSONB DEFAULT '[]',
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Alerts table
CREATE TABLE alerts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    rule_id UUID,
    severity VARCHAR(50) NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    title VARCHAR(255) NOT NULL,
    message TEXT,
    status VARCHAR(50) DEFAULT 'open' CHECK (status IN ('open', 'acknowledged', 'resolved')),
    acknowledged_by UUID REFERENCES users(id),
    acknowledged_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_alerts_status ON alerts(status);
CREATE INDEX idx_alerts_device ON alerts(device_id);
CREATE INDEX idx_alerts_created_at ON alerts(created_at DESC);

-- Alert rules table
CREATE TABLE alert_rules (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    enabled BOOLEAN DEFAULT TRUE,
    metric VARCHAR(100) NOT NULL,
    operator VARCHAR(20) NOT NULL CHECK (operator IN ('gt', 'gte', 'lt', 'lte', 'eq', 'neq')),
    threshold REAL NOT NULL,
    duration_seconds INTEGER DEFAULT 0,
    severity VARCHAR(50) NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    cooldown_minutes INTEGER DEFAULT 15,
    notification_channels TEXT[] DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Default alert rules
INSERT INTO alert_rules (name, description, metric, operator, threshold, severity, cooldown_minutes) VALUES
    ('High CPU Usage', 'Alert when CPU usage exceeds 90%', 'cpu_percent', 'gt', 90, 'warning', 15),
    ('Critical CPU Usage', 'Alert when CPU usage exceeds 95%', 'cpu_percent', 'gt', 95, 'critical', 5),
    ('High Memory Usage', 'Alert when memory usage exceeds 85%', 'memory_percent', 'gt', 85, 'warning', 15),
    ('Critical Memory Usage', 'Alert when memory usage exceeds 95%', 'memory_percent', 'gt', 95, 'critical', 5),
    ('Low Disk Space', 'Alert when disk usage exceeds 90%', 'disk_percent', 'gt', 90, 'critical', 60),
    ('Device Offline', 'Alert when device goes offline', 'status', 'eq', 0, 'critical', 5);

-- Sessions table (for JWT refresh tokens)
CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_hash VARCHAR(255) NOT NULL,
    user_agent TEXT,
    ip_address INET,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

-- Audit log table
CREATE TABLE audit_log (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID REFERENCES users(id),
    action VARCHAR(100) NOT NULL,
    resource_type VARCHAR(100),
    resource_id UUID,
    details JSONB,
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_audit_log_user ON audit_log(user_id);
CREATE INDEX idx_audit_log_created_at ON audit_log(created_at DESC);
CREATE INDEX idx_audit_log_action ON audit_log(action);

-- Settings table (key-value store)
CREATE TABLE settings (
    key VARCHAR(255) PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Default settings
INSERT INTO settings (key, value) VALUES
    ('enrollment_enabled', 'true'),
    ('metrics_retention_days', '30'),
    ('alert_email_enabled', 'false'),
    ('alert_email_recipients', '[]'),
    ('organization_name', 'Sentinel'),
    ('enrollment_token', 'sentinel-enrollment-default'),
    ('agent_heartbeat_interval', '30'),
    ('agent_metrics_interval', '60');

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Apply updated_at trigger to relevant tables
CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_devices_updated_at BEFORE UPDATE ON devices
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_scripts_updated_at BEFORE UPDATE ON scripts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_alert_rules_updated_at BEFORE UPDATE ON alert_rules
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_settings_updated_at BEFORE UPDATE ON settings
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
