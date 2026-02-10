-- Migration: 035_credential_management.sql
-- Description: Enterprise credential management with rotation support
-- Date: 2026-02-10

-- Credential keys table with versioning and dual-key support
CREATE TABLE IF NOT EXISTS credential_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    credential_type VARCHAR(50) NOT NULL,  -- 'jwt_secret', 'api_key', 'encryption_key'
    key_value_encrypted BYTEA NOT NULL,    -- AES-256-GCM encrypted using server master key
    key_hash VARCHAR(128),                  -- For quick validation without decryption (bcrypt for api_key, sha256 for others)
    version INTEGER NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',  -- 'active', 'grace_period', 'retired', 'revoked'
    name VARCHAR(255),                      -- Human-readable name (for API keys)
    description TEXT,
    permissions JSONB DEFAULT '[]',         -- For API keys: ["devices:read", "scripts:execute"]
    ip_allowlist JSONB DEFAULT '[]',        -- Optional IP restrictions for API keys
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    activated_at TIMESTAMPTZ,
    grace_period_start TIMESTAMPTZ,
    grace_period_ends TIMESTAMPTZ,
    retired_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,                 -- Optional auto-expiration
    last_used_at TIMESTAMPTZ,
    use_count BIGINT DEFAULT 0,
    created_by UUID REFERENCES users(id),
    revoked_by UUID REFERENCES users(id),
    revocation_reason TEXT,
    metadata JSONB DEFAULT '{}',

    -- Constraints
    CONSTRAINT valid_status CHECK (status IN ('active', 'grace_period', 'retired', 'revoked')),
    CONSTRAINT valid_credential_type CHECK (credential_type IN ('jwt_secret', 'api_key', 'encryption_key', 'webhook_secret'))
);

-- Indexes for credential lookups
CREATE INDEX IF NOT EXISTS idx_credential_keys_type_status ON credential_keys(credential_type, status);
CREATE INDEX IF NOT EXISTS idx_credential_keys_type_version ON credential_keys(credential_type, version DESC);
CREATE INDEX IF NOT EXISTS idx_credential_keys_hash ON credential_keys(key_hash) WHERE key_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_credential_keys_active ON credential_keys(credential_type) WHERE status IN ('active', 'grace_period');

-- Credential rotation audit log (immutable append-only)
CREATE TABLE IF NOT EXISTS credential_rotation_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    credential_type VARCHAR(50) NOT NULL,
    credential_key_id UUID REFERENCES credential_keys(id),
    old_version INTEGER,
    new_version INTEGER NOT NULL,
    action VARCHAR(30) NOT NULL,  -- 'rotate', 'create', 'revoke', 'rollback', 'grace_expire', 'manual_retire'
    initiated_by UUID REFERENCES users(id),
    initiated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',  -- 'pending', 'in_progress', 'success', 'failed', 'rolled_back'
    pre_check_result JSONB,       -- Health check results before rotation
    post_check_result JSONB,      -- Health check results after rotation
    failure_reason TEXT,
    rollback_reason TEXT,
    affected_sessions INTEGER DEFAULT 0,
    affected_agents INTEGER DEFAULT 0,
    affected_api_clients INTEGER DEFAULT 0,
    grace_period_hours INTEGER,
    metadata JSONB DEFAULT '{}',

    -- Constraints
    CONSTRAINT valid_action CHECK (action IN ('rotate', 'create', 'revoke', 'rollback', 'grace_expire', 'manual_retire', 'emergency_revoke')),
    CONSTRAINT valid_log_status CHECK (status IN ('pending', 'in_progress', 'success', 'failed', 'rolled_back'))
);

-- Indexes for audit log queries
CREATE INDEX IF NOT EXISTS idx_rotation_log_type ON credential_rotation_log(credential_type);
CREATE INDEX IF NOT EXISTS idx_rotation_log_initiated_at ON credential_rotation_log(initiated_at DESC);
CREATE INDEX IF NOT EXISTS idx_rotation_log_status ON credential_rotation_log(status) WHERE status IN ('pending', 'in_progress');

-- Credential rotation schedule configuration
CREATE TABLE IF NOT EXISTS credential_rotation_schedule (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    credential_type VARCHAR(50) NOT NULL UNIQUE,
    rotation_interval_days INTEGER NOT NULL DEFAULT 90,
    grace_period_hours INTEGER NOT NULL DEFAULT 24,
    warning_threshold_days INTEGER NOT NULL DEFAULT 7,  -- Warn this many days before scheduled rotation
    last_rotation_at TIMESTAMPTZ,
    last_rotation_log_id UUID REFERENCES credential_rotation_log(id),
    next_scheduled_rotation TIMESTAMPTZ,
    auto_rotate BOOLEAN NOT NULL DEFAULT false,
    notify_on_rotation BOOLEAN NOT NULL DEFAULT true,
    notify_on_warning BOOLEAN NOT NULL DEFAULT true,
    notify_emails JSONB DEFAULT '[]',  -- Additional emails to notify
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by UUID REFERENCES users(id)
);

-- API Key usage tracking (for analytics and abuse detection)
CREATE TABLE IF NOT EXISTS api_key_usage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    api_key_id UUID NOT NULL REFERENCES credential_keys(id) ON DELETE CASCADE,
    endpoint VARCHAR(255) NOT NULL,
    method VARCHAR(10) NOT NULL,
    ip_address INET,
    user_agent TEXT,
    status_code INTEGER,
    response_time_ms INTEGER,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Partition API key usage by month for performance
CREATE INDEX IF NOT EXISTS idx_api_key_usage_key_timestamp ON api_key_usage(api_key_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_api_key_usage_timestamp ON api_key_usage(timestamp DESC);

-- Function to update credential last_used_at and use_count
CREATE OR REPLACE FUNCTION update_credential_usage()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE credential_keys
    SET last_used_at = NOW(),
        use_count = use_count + 1
    WHERE id = NEW.api_key_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger for API key usage tracking
DROP TRIGGER IF EXISTS trigger_update_credential_usage ON api_key_usage;
CREATE TRIGGER trigger_update_credential_usage
    AFTER INSERT ON api_key_usage
    FOR EACH ROW
    EXECUTE FUNCTION update_credential_usage();

-- Function to automatically retire keys when grace period ends
CREATE OR REPLACE FUNCTION retire_expired_grace_period_keys()
RETURNS INTEGER AS $$
DECLARE
    retired_count INTEGER;
BEGIN
    WITH retired AS (
        UPDATE credential_keys
        SET status = 'retired',
            retired_at = NOW()
        WHERE status = 'grace_period'
          AND grace_period_ends IS NOT NULL
          AND grace_period_ends < NOW()
        RETURNING id
    )
    SELECT COUNT(*) INTO retired_count FROM retired;

    -- Log the automatic retirements
    INSERT INTO credential_rotation_log (credential_type, credential_key_id, new_version, action, status, completed_at, metadata)
    SELECT
        ck.credential_type,
        ck.id,
        ck.version,
        'grace_expire',
        'success',
        NOW(),
        '{"automatic": true}'::jsonb
    FROM credential_keys ck
    WHERE ck.status = 'retired'
      AND ck.retired_at >= NOW() - INTERVAL '1 minute';

    RETURN retired_count;
END;
$$ LANGUAGE plpgsql;

-- Insert default rotation schedules
INSERT INTO credential_rotation_schedule (credential_type, rotation_interval_days, grace_period_hours, warning_threshold_days, auto_rotate)
VALUES
    ('jwt_secret', 90, 24, 7, false),
    ('api_key', 180, 48, 14, false),
    ('encryption_key', 365, 72, 30, false),
    ('webhook_secret', 90, 24, 7, false)
ON CONFLICT (credential_type) DO NOTHING;

-- View for credential status dashboard
CREATE OR REPLACE VIEW v_credential_status AS
SELECT
    ck.id,
    ck.credential_type,
    ck.name,
    ck.version,
    ck.status,
    ck.created_at,
    ck.activated_at,
    ck.grace_period_ends,
    ck.expires_at,
    ck.last_used_at,
    ck.use_count,
    crs.rotation_interval_days,
    crs.next_scheduled_rotation,
    crs.auto_rotate,
    CASE
        WHEN ck.status = 'revoked' THEN 'revoked'
        WHEN ck.expires_at IS NOT NULL AND ck.expires_at < NOW() THEN 'expired'
        WHEN ck.status = 'grace_period' THEN 'grace_period'
        WHEN crs.next_scheduled_rotation IS NOT NULL AND crs.next_scheduled_rotation < NOW() THEN 'overdue'
        WHEN crs.next_scheduled_rotation IS NOT NULL AND crs.next_scheduled_rotation < NOW() + (crs.warning_threshold_days || ' days')::INTERVAL THEN 'warning'
        ELSE 'healthy'
    END AS health_status,
    u.email AS created_by_email
FROM credential_keys ck
LEFT JOIN credential_rotation_schedule crs ON ck.credential_type = crs.credential_type
LEFT JOIN users u ON ck.created_by = u.id
WHERE ck.status IN ('active', 'grace_period')
ORDER BY ck.credential_type, ck.version DESC;

-- View for recent rotation activity
CREATE OR REPLACE VIEW v_rotation_activity AS
SELECT
    crl.id,
    crl.credential_type,
    crl.action,
    crl.old_version,
    crl.new_version,
    crl.status,
    crl.initiated_at,
    crl.completed_at,
    crl.failure_reason,
    crl.affected_sessions,
    crl.affected_agents,
    u.email AS initiated_by_email,
    EXTRACT(EPOCH FROM (crl.completed_at - crl.initiated_at)) AS duration_seconds
FROM credential_rotation_log crl
LEFT JOIN users u ON crl.initiated_by = u.id
ORDER BY crl.initiated_at DESC;

-- Grant permissions (adjust role name as needed)
-- GRANT SELECT ON v_credential_status TO sentinel_app;
-- GRANT SELECT ON v_rotation_activity TO sentinel_app;

COMMENT ON TABLE credential_keys IS 'Stores all managed credentials with versioning and rotation support. Keys are encrypted at rest.';
COMMENT ON TABLE credential_rotation_log IS 'Immutable audit log of all credential rotation events.';
COMMENT ON TABLE credential_rotation_schedule IS 'Configuration for automatic credential rotation schedules.';
COMMENT ON TABLE api_key_usage IS 'Tracks API key usage for analytics and abuse detection.';
COMMENT ON VIEW v_credential_status IS 'Dashboard view showing current credential health status.';
COMMENT ON VIEW v_rotation_activity IS 'Recent credential rotation activity for audit purposes.';
