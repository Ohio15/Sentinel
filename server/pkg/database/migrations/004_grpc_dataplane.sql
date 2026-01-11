-- Migration 004: gRPC Data Plane tables
-- Tables for storing data streamed via the gRPC Data Plane channel

-- Agent logs table - stores logs streamed from agents
CREATE TABLE IF NOT EXISTS agent_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    level VARCHAR(20) NOT NULL DEFAULT 'info',
    source VARCHAR(255),
    message TEXT NOT NULL,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for efficient log queries
CREATE INDEX IF NOT EXISTS idx_agent_logs_device_id ON agent_logs(device_id);
CREATE INDEX IF NOT EXISTS idx_agent_logs_timestamp ON agent_logs(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_agent_logs_level ON agent_logs(level);
CREATE INDEX IF NOT EXISTS idx_agent_logs_device_timestamp ON agent_logs(device_id, timestamp DESC);

-- Software inventory table - stores installed software from agents
CREATE TABLE IF NOT EXISTS software_inventory (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    version VARCHAR(100),
    publisher VARCHAR(255),
    install_date DATE,
    install_location TEXT,
    size_bytes BIGINT,
    is_system_component BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(device_id, name, version)
);

-- Indexes for software inventory queries
CREATE INDEX IF NOT EXISTS idx_software_inventory_device_id ON software_inventory(device_id);
CREATE INDEX IF NOT EXISTS idx_software_inventory_name ON software_inventory(name);
CREATE INDEX IF NOT EXISTS idx_software_inventory_publisher ON software_inventory(publisher);

-- Bulk data uploads table - tracks large data uploads from agents
CREATE TABLE IF NOT EXISTS bulk_data_uploads (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    data_type VARCHAR(50) NOT NULL,
    request_id VARCHAR(100),
    size_bytes BIGINT NOT NULL,
    status VARCHAR(20) DEFAULT 'completed',
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_bulk_data_uploads_device_id ON bulk_data_uploads(device_id);
CREATE INDEX IF NOT EXISTS idx_bulk_data_uploads_data_type ON bulk_data_uploads(data_type);

-- Add grpc_connected column to track gRPC Data Plane connection status
ALTER TABLE devices ADD COLUMN IF NOT EXISTS grpc_connected BOOLEAN DEFAULT FALSE;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS grpc_last_seen TIMESTAMP WITH TIME ZONE;

-- Function to clean old logs (retention policy)
CREATE OR REPLACE FUNCTION clean_old_agent_logs(retention_days INTEGER DEFAULT 30)
RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    DELETE FROM agent_logs
    WHERE timestamp < NOW() - (retention_days || ' days')::INTERVAL;
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;
