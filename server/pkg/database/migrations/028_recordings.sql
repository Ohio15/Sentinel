-- Migration: Performance Metrics Recording
-- Purpose: Store recorded performance metric sessions for later analysis and playback
-- Version: 028

-- Recordings table - tracks performance recording sessions
CREATE TABLE IF NOT EXISTS recordings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    -- Recording metadata
    name VARCHAR(255),
    description TEXT,

    -- Recording state
    status VARCHAR(20) NOT NULL DEFAULT 'recording'
        CHECK (status IN ('recording', 'completed', 'failed', 'cancelled')),

    -- Timing
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    duration_seconds INTEGER GENERATED ALWAYS AS (
        CASE
            WHEN ended_at IS NOT NULL THEN EXTRACT(EPOCH FROM (ended_at - started_at))::INTEGER
            ELSE NULL
        END
    ) STORED,

    -- Statistics
    metrics_count INTEGER DEFAULT 0,

    -- Attribution
    initiated_by UUID REFERENCES users(id) ON DELETE SET NULL,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Recording metrics table - stores the actual metric data points
CREATE TABLE IF NOT EXISTS recording_metrics (
    id BIGSERIAL PRIMARY KEY,
    recording_id UUID NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,

    -- Timestamp of the metric sample
    timestamp TIMESTAMPTZ NOT NULL,

    -- CPU metrics
    cpu_percent REAL,

    -- Memory metrics
    memory_percent REAL,
    memory_used_bytes BIGINT,
    memory_total_bytes BIGINT,

    -- Disk metrics
    disk_percent REAL,
    disk_used_bytes BIGINT,
    disk_total_bytes BIGINT,

    -- Network metrics
    network_rx_bytes BIGINT,
    network_tx_bytes BIGINT,
    network_rx_rate BIGINT,  -- bytes per second
    network_tx_rate BIGINT,  -- bytes per second

    -- Process metrics
    process_count INTEGER,

    -- Extended metrics (JSON for flexibility)
    extended_metrics JSONB DEFAULT '{}'::jsonb
);

-- Indexes for recordings table
CREATE INDEX IF NOT EXISTS idx_recordings_device_id
    ON recordings(device_id);

CREATE INDEX IF NOT EXISTS idx_recordings_org_id
    ON recordings(organization_id);

CREATE INDEX IF NOT EXISTS idx_recordings_status
    ON recordings(status);

CREATE INDEX IF NOT EXISTS idx_recordings_started_at
    ON recordings(started_at DESC);

CREATE INDEX IF NOT EXISTS idx_recordings_device_status
    ON recordings(device_id, status);

CREATE INDEX IF NOT EXISTS idx_recordings_initiated_by
    ON recordings(initiated_by)
    WHERE initiated_by IS NOT NULL;

-- Indexes for recording_metrics table
CREATE INDEX IF NOT EXISTS idx_recording_metrics_recording_id
    ON recording_metrics(recording_id);

CREATE INDEX IF NOT EXISTS idx_recording_metrics_recording_timestamp
    ON recording_metrics(recording_id, timestamp);

CREATE INDEX IF NOT EXISTS idx_recording_metrics_timestamp
    ON recording_metrics(timestamp DESC);

-- Trigger to update updated_at timestamp
DROP TRIGGER IF EXISTS trigger_recordings_updated_at ON recordings;
CREATE TRIGGER trigger_recordings_updated_at
    BEFORE UPDATE ON recordings
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Function to update metrics count on recording
CREATE OR REPLACE FUNCTION update_recording_metrics_count()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE recordings
        SET metrics_count = metrics_count + 1
        WHERE id = NEW.recording_id;
    ELSIF TG_OP = 'DELETE' THEN
        UPDATE recordings
        SET metrics_count = metrics_count - 1
        WHERE id = OLD.recording_id;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- Trigger to maintain metrics_count
DROP TRIGGER IF EXISTS trigger_update_recording_metrics_count ON recording_metrics;
CREATE TRIGGER trigger_update_recording_metrics_count
    AFTER INSERT OR DELETE ON recording_metrics
    FOR EACH ROW
    EXECUTE FUNCTION update_recording_metrics_count();

-- Function to get recording summary with statistics
CREATE OR REPLACE FUNCTION get_recording_summary(p_recording_id UUID)
RETURNS TABLE (
    id UUID,
    device_id UUID,
    name VARCHAR(255),
    status VARCHAR(20),
    started_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    duration_seconds INTEGER,
    metrics_count INTEGER,
    avg_cpu_percent REAL,
    max_cpu_percent REAL,
    avg_memory_percent REAL,
    max_memory_percent REAL,
    total_network_rx BIGINT,
    total_network_tx BIGINT
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        r.id,
        r.device_id,
        r.name,
        r.status,
        r.started_at,
        r.ended_at,
        r.duration_seconds,
        r.metrics_count,
        ROUND(AVG(rm.cpu_percent)::numeric, 2)::REAL as avg_cpu_percent,
        MAX(rm.cpu_percent) as max_cpu_percent,
        ROUND(AVG(rm.memory_percent)::numeric, 2)::REAL as avg_memory_percent,
        MAX(rm.memory_percent) as max_memory_percent,
        MAX(rm.network_rx_bytes) - MIN(rm.network_rx_bytes) as total_network_rx,
        MAX(rm.network_tx_bytes) - MIN(rm.network_tx_bytes) as total_network_tx
    FROM recordings r
    LEFT JOIN recording_metrics rm ON r.id = rm.recording_id
    WHERE r.id = p_recording_id
    GROUP BY r.id, r.device_id, r.name, r.status, r.started_at, r.ended_at, r.duration_seconds, r.metrics_count;
END;
$$ LANGUAGE plpgsql;

-- Function to cleanup old failed/cancelled recordings
CREATE OR REPLACE FUNCTION cleanup_old_recordings(
    p_retention_days INT DEFAULT 90,
    p_cleanup_failed_days INT DEFAULT 7
) RETURNS TABLE (
    deleted_recordings INT,
    deleted_metrics INT
) AS $$
DECLARE
    v_deleted_recordings INT := 0;
    v_deleted_metrics INT := 0;
BEGIN
    -- Delete old completed recordings beyond retention period
    WITH deleted AS (
        DELETE FROM recordings
        WHERE status = 'completed'
        AND created_at < NOW() - (p_retention_days || ' days')::INTERVAL
        RETURNING id
    )
    SELECT COUNT(*) INTO v_deleted_recordings FROM deleted;

    -- Delete failed/cancelled recordings older than cleanup period
    WITH deleted AS (
        DELETE FROM recordings
        WHERE status IN ('failed', 'cancelled')
        AND created_at < NOW() - (p_cleanup_failed_days || ' days')::INTERVAL
        RETURNING id
    )
    SELECT v_deleted_recordings + COUNT(*) INTO v_deleted_recordings FROM deleted;

    -- Note: recording_metrics are automatically deleted via CASCADE

    RETURN QUERY SELECT v_deleted_recordings, v_deleted_metrics;
END;
$$ LANGUAGE plpgsql;

-- View for active recordings
CREATE OR REPLACE VIEW active_recordings AS
SELECT
    r.id,
    r.device_id,
    d.hostname,
    d.display_name,
    r.name as recording_name,
    r.status,
    r.started_at,
    r.metrics_count,
    EXTRACT(EPOCH FROM (NOW() - r.started_at))::INTEGER as current_duration_seconds,
    r.initiated_by,
    u.email as initiated_by_email,
    r.organization_id
FROM recordings r
JOIN devices d ON r.device_id = d.id
LEFT JOIN users u ON r.initiated_by = u.id
WHERE r.status = 'recording';

COMMENT ON TABLE recordings IS 'Stores performance recording session metadata';
COMMENT ON TABLE recording_metrics IS 'Stores individual metric data points for recordings';
COMMENT ON FUNCTION get_recording_summary IS 'Returns summary statistics for a recording';
COMMENT ON FUNCTION cleanup_old_recordings IS 'Removes old recordings based on retention policy';
COMMENT ON VIEW active_recordings IS 'View of currently active recording sessions';
