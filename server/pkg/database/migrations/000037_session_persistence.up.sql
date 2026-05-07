-- Migration: Session Persistence
-- Purpose: Store terminal, RDP, and file transfer sessions for reconnection recovery
-- Version: 026

-- Terminal sessions - tracks remote terminal sessions that can survive disconnects
CREATE TABLE IF NOT EXISTS terminal_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    agent_id VARCHAR(255) NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    session_id VARCHAR(255) NOT NULL UNIQUE,

    -- Session state
    status VARCHAR(50) DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'closed')),

    -- Terminal configuration (for session recovery)
    cols INT DEFAULT 80,
    rows INT DEFAULT 24,
    shell_type VARCHAR(50), -- powershell, bash, cmd, etc.
    working_directory TEXT,

    -- Timestamps
    created_at TIMESTAMPTZ DEFAULT NOW(),
    last_activity_at TIMESTAMPTZ DEFAULT NOW(),
    suspended_at TIMESTAMPTZ,
    closed_at TIMESTAMPTZ,
    close_reason VARCHAR(255)
);

-- RDP/Remote Desktop sessions - tracks remote desktop sessions
CREATE TABLE IF NOT EXISTS rdp_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    agent_id VARCHAR(255) NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    session_id VARCHAR(255) NOT NULL UNIQUE,

    -- Session state
    status VARCHAR(50) DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'closed')),

    -- RDP configuration
    width INT,
    height INT,
    quality VARCHAR(50) DEFAULT 'balanced', -- low, balanced, high

    -- Timestamps
    created_at TIMESTAMPTZ DEFAULT NOW(),
    last_activity_at TIMESTAMPTZ DEFAULT NOW(),
    suspended_at TIMESTAMPTZ,
    closed_at TIMESTAMPTZ,

    -- Metadata (JSON for flexibility - can store connection info, capabilities, etc.)
    metadata JSONB DEFAULT '{}'::jsonb
);

-- File transfers - tracks file operations for resume support
CREATE TABLE IF NOT EXISTS file_transfers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    agent_id VARCHAR(255) NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    transfer_id VARCHAR(255) NOT NULL UNIQUE,

    -- Transfer details
    operation VARCHAR(50) NOT NULL CHECK (operation IN ('upload', 'download')),
    remote_path TEXT NOT NULL,
    local_path TEXT, -- For uploads, where the temp file is stored
    file_name VARCHAR(512),
    file_size BIGINT,
    mime_type VARCHAR(255),

    -- Progress tracking
    bytes_transferred BIGINT DEFAULT 0,
    chunk_size INT DEFAULT 65536, -- Chunk size being used
    total_chunks INT,
    completed_chunks INT DEFAULT 0,

    -- State
    status VARCHAR(50) DEFAULT 'pending' CHECK (status IN ('pending', 'in_progress', 'paused', 'completed', 'failed', 'cancelled')),
    error_message TEXT,

    -- Integrity
    checksum VARCHAR(128), -- SHA256 of the file
    checksum_partial VARCHAR(128), -- Checksum of transferred portion (for resume verification)

    -- Timestamps
    created_at TIMESTAMPTZ DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    last_chunk_at TIMESTAMPTZ
);

-- Dashboard subscriptions - tracks which dashboards are watching which devices
-- This enables targeted message routing instead of broadcast
CREATE TABLE IF NOT EXISTS dashboard_subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    connection_id VARCHAR(255) NOT NULL, -- WebSocket connection identifier

    -- What the dashboard is subscribed to
    device_id UUID REFERENCES devices(id) ON DELETE CASCADE, -- NULL means "all devices" subscription
    subscription_type VARCHAR(50) NOT NULL, -- 'device_metrics', 'device_status', 'alerts', 'terminal', 'rdp', 'files'
    session_id VARCHAR(255), -- For session-specific subscriptions (terminal, rdp, files)

    -- State
    is_active BOOLEAN DEFAULT true,

    -- Timestamps
    created_at TIMESTAMPTZ DEFAULT NOW(),
    last_activity_at TIMESTAMPTZ DEFAULT NOW(),

    -- Ensure unique subscriptions per connection
    UNIQUE (connection_id, device_id, subscription_type, session_id)
);

-- Indexes for terminal sessions
CREATE INDEX IF NOT EXISTS idx_terminal_sessions_device_active
    ON terminal_sessions(device_id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_terminal_sessions_user
    ON terminal_sessions(user_id)
    WHERE status IN ('active', 'suspended');

CREATE INDEX IF NOT EXISTS idx_terminal_sessions_agent
    ON terminal_sessions(agent_id)
    WHERE status IN ('active', 'suspended');

CREATE INDEX IF NOT EXISTS idx_terminal_sessions_org
    ON terminal_sessions(organization_id);

-- Indexes for RDP sessions
CREATE INDEX IF NOT EXISTS idx_rdp_sessions_device_active
    ON rdp_sessions(device_id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_rdp_sessions_user
    ON rdp_sessions(user_id)
    WHERE status IN ('active', 'suspended');

CREATE INDEX IF NOT EXISTS idx_rdp_sessions_agent
    ON rdp_sessions(agent_id)
    WHERE status IN ('active', 'suspended');

CREATE INDEX IF NOT EXISTS idx_rdp_sessions_org
    ON rdp_sessions(organization_id);

-- Indexes for file transfers
CREATE INDEX IF NOT EXISTS idx_file_transfers_device_active
    ON file_transfers(device_id)
    WHERE status IN ('pending', 'in_progress', 'paused');

CREATE INDEX IF NOT EXISTS idx_file_transfers_user
    ON file_transfers(user_id)
    WHERE status IN ('pending', 'in_progress', 'paused');

CREATE INDEX IF NOT EXISTS idx_file_transfers_agent
    ON file_transfers(agent_id)
    WHERE status IN ('pending', 'in_progress', 'paused');

CREATE INDEX IF NOT EXISTS idx_file_transfers_org
    ON file_transfers(organization_id);

-- Indexes for dashboard subscriptions
CREATE INDEX IF NOT EXISTS idx_dashboard_subs_user_active
    ON dashboard_subscriptions(user_id)
    WHERE is_active = true;

CREATE INDEX IF NOT EXISTS idx_dashboard_subs_device
    ON dashboard_subscriptions(device_id)
    WHERE is_active = true AND device_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_dashboard_subs_connection
    ON dashboard_subscriptions(connection_id)
    WHERE is_active = true;

CREATE INDEX IF NOT EXISTS idx_dashboard_subs_session
    ON dashboard_subscriptions(session_id)
    WHERE is_active = true AND session_id IS NOT NULL;

-- Function to update last_activity_at automatically
CREATE OR REPLACE FUNCTION update_session_activity()
RETURNS TRIGGER AS $$
BEGIN
    NEW.last_activity_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Triggers to update activity timestamps
DROP TRIGGER IF EXISTS trigger_terminal_session_activity ON terminal_sessions;
CREATE TRIGGER trigger_terminal_session_activity
    BEFORE UPDATE ON terminal_sessions
    FOR EACH ROW
    WHEN (OLD.* IS DISTINCT FROM NEW.*)
    EXECUTE FUNCTION update_session_activity();

DROP TRIGGER IF EXISTS trigger_rdp_session_activity ON rdp_sessions;
CREATE TRIGGER trigger_rdp_session_activity
    BEFORE UPDATE ON rdp_sessions
    FOR EACH ROW
    WHEN (OLD.* IS DISTINCT FROM NEW.*)
    EXECUTE FUNCTION update_session_activity();

-- Function to cleanup old sessions (called by background job)
CREATE OR REPLACE FUNCTION cleanup_stale_sessions(
    terminal_inactive_minutes INT DEFAULT 30,
    rdp_inactive_minutes INT DEFAULT 30,
    transfer_inactive_hours INT DEFAULT 24
) RETURNS TABLE (
    terminal_closed INT,
    rdp_closed INT,
    transfers_failed INT
) AS $$
DECLARE
    v_terminal_closed INT := 0;
    v_rdp_closed INT := 0;
    v_transfers_failed INT := 0;
BEGIN
    -- Close inactive terminal sessions
    WITH closed AS (
        UPDATE terminal_sessions
        SET status = 'closed',
            closed_at = NOW(),
            close_reason = 'inactivity_timeout'
        WHERE status = 'active'
        AND last_activity_at < NOW() - (terminal_inactive_minutes || ' minutes')::INTERVAL
        RETURNING id
    )
    SELECT COUNT(*) INTO v_terminal_closed FROM closed;

    -- Close inactive RDP sessions
    WITH closed AS (
        UPDATE rdp_sessions
        SET status = 'closed',
            closed_at = NOW()
        WHERE status = 'active'
        AND last_activity_at < NOW() - (rdp_inactive_minutes || ' minutes')::INTERVAL
        RETURNING id
    )
    SELECT COUNT(*) INTO v_rdp_closed FROM closed;

    -- Fail stale file transfers
    WITH failed AS (
        UPDATE file_transfers
        SET status = 'failed',
            error_message = 'Transfer timed out due to inactivity'
        WHERE status IN ('pending', 'in_progress', 'paused')
        AND COALESCE(last_chunk_at, created_at) < NOW() - (transfer_inactive_hours || ' hours')::INTERVAL
        RETURNING id
    )
    SELECT COUNT(*) INTO v_transfers_failed FROM failed;

    RETURN QUERY SELECT v_terminal_closed, v_rdp_closed, v_transfers_failed;
END;
$$ LANGUAGE plpgsql;

-- View for active sessions overview
CREATE OR REPLACE VIEW active_sessions_summary AS
SELECT
    d.id as device_id,
    d.hostname,
    d.agent_id,
    COALESCE(ts.terminal_count, 0) as active_terminals,
    COALESCE(rs.rdp_count, 0) as active_rdp,
    COALESCE(ft.transfer_count, 0) as active_transfers
FROM devices d
LEFT JOIN (
    SELECT device_id, COUNT(*) as terminal_count
    FROM terminal_sessions
    WHERE status = 'active'
    GROUP BY device_id
) ts ON d.id = ts.device_id
LEFT JOIN (
    SELECT device_id, COUNT(*) as rdp_count
    FROM rdp_sessions
    WHERE status = 'active'
    GROUP BY device_id
) rs ON d.id = rs.device_id
LEFT JOIN (
    SELECT device_id, COUNT(*) as transfer_count
    FROM file_transfers
    WHERE status IN ('pending', 'in_progress', 'paused')
    GROUP BY device_id
) ft ON d.id = ft.device_id
WHERE ts.terminal_count > 0
   OR rs.rdp_count > 0
   OR ft.transfer_count > 0;

COMMENT ON TABLE terminal_sessions IS 'Stores terminal session state for reconnection recovery';
COMMENT ON TABLE rdp_sessions IS 'Stores RDP session state for reconnection recovery';
COMMENT ON TABLE file_transfers IS 'Stores file transfer state for resume support';
COMMENT ON TABLE dashboard_subscriptions IS 'Tracks which dashboards are watching which devices for targeted routing';
COMMENT ON FUNCTION cleanup_stale_sessions IS 'Automatically closes inactive sessions and fails stale transfers';
