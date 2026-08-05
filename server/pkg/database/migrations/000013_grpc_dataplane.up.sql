-- Migration 004: gRPC Data Plane tables
-- Tables for storing data streamed via the gRPC Data Plane channel

-- agent_logs is NOT defined here. It is owned by 000008_agent_logs.up.sql,
-- whose shape uses logged_at/received_at (there is no "timestamp" column), and
-- 000009 renames the legacy columns on databases that predate that shape.
--
-- This migration used to re-declare agent_logs with a "timestamp" column plus
-- indexes on it. On any FRESH database that was a hard boot failure: 000008 had
-- already created the table, so this CREATE TABLE IF NOT EXISTS was silently
-- skipped, and the following CREATE INDEX ... ON agent_logs(timestamp DESC)
-- aborted the migration with:
--     column "timestamp" does not exist (SQLSTATE 42703)
-- Existing deployments were unaffected only because their schema_migrations was
-- already past version 13. Do not reintroduce a table definition here: a table
-- must have exactly one owning migration, and every later statement in a file
-- must only reference columns that migration is guaranteed to have produced.

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

-- Function to clean old logs (retention policy).
-- Filters on logged_at, matching the agent_logs shape owned by 000008. It used
-- to filter on a "timestamp" column that never exists on a correctly migrated
-- database. Nothing calls this function today — the live retention job is the
-- inline DELETE in cmd/sentinel/main.go, which already uses logged_at — but it
-- is kept and corrected rather than dropped so that a freshly migrated database
-- and an existing deployment (where CREATE OR REPLACE has already run and will
-- not re-run) do not diverge in schema.
CREATE OR REPLACE FUNCTION clean_old_agent_logs(retention_days INTEGER DEFAULT 30)
RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    DELETE FROM agent_logs
    WHERE logged_at < NOW() - (retention_days || ' days')::INTERVAL;
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;
