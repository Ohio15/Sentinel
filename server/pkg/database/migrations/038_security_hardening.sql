-- Migration: Security Hardening
-- Adds indexes and columns for auth performance and security

-- 1. Token prefix index for O(1) enrollment token lookup
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'enrollment_tokens' AND column_name = 'token_prefix') THEN
        ALTER TABLE enrollment_tokens ADD COLUMN token_prefix VARCHAR(16);
    END IF;
END $$;

UPDATE enrollment_tokens SET token_prefix = LEFT(token_hash, 16) WHERE token_prefix IS NULL AND token_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_prefix ON enrollment_tokens(token_prefix) WHERE is_active = TRUE;

-- 2. Session expiry index for efficient cleanup
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

-- 3. API key role column (previously auto-granted admin)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'api_keys' AND column_name = 'role') THEN
        ALTER TABLE api_keys ADD COLUMN role VARCHAR(20) DEFAULT 'operator';
        UPDATE api_keys SET role = 'admin' WHERE role = 'operator';
    END IF;
END $$;

-- 4. Audit log severity tracking
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'audit_log' AND column_name = 'severity') THEN
        ALTER TABLE audit_log ADD COLUMN severity VARCHAR(20) DEFAULT 'info';
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS idx_audit_log_severity ON audit_log(severity);

-- 5. Metrics retention cleanup function
CREATE OR REPLACE FUNCTION cleanup_old_metrics(retention_days INTEGER DEFAULT 90)
RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    DELETE FROM device_metrics WHERE timestamp < NOW() - (retention_days || ' days')::INTERVAL;
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;

-- 6. Foreign key constraints (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_commands_device_id') THEN
        BEGIN
            ALTER TABLE commands ADD CONSTRAINT fk_commands_device_id
                FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE;
        EXCEPTION WHEN others THEN
            RAISE NOTICE 'Could not add fk_commands_device_id: %', SQLERRM;
        END;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_alerts_device_id') THEN
        BEGIN
            ALTER TABLE alerts ADD CONSTRAINT fk_alerts_device_id
                FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE;
        EXCEPTION WHEN others THEN
            RAISE NOTICE 'Could not add fk_alerts_device_id: %', SQLERRM;
        END;
    END IF;
END $$;
