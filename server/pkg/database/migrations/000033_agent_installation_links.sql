-- Migration: Agent Installation Links for Self-Service Portal
-- Creates tables for self-service agent download portal with email notifications

-- Add token_hash and is_legacy columns to enrollment_tokens if they don't exist
DO $$ 
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'enrollment_tokens' AND column_name = 'token_hash') THEN
        ALTER TABLE enrollment_tokens ADD COLUMN token_hash VARCHAR(255);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'enrollment_tokens' AND column_name = 'is_legacy') THEN
        ALTER TABLE enrollment_tokens ADD COLUMN is_legacy BOOLEAN DEFAULT FALSE;
    END IF;
END $$;

-- Main table for installation links
CREATE TABLE IF NOT EXISTS agent_installation_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Link identification
    download_token VARCHAR(64) UNIQUE NOT NULL,
    installation_code VARCHAR(12) UNIQUE,  -- Short code like "AB12-CD34"

    -- Multi-tenant support
    organization_id UUID DEFAULT '00000000-0000-0000-0000-000000000001',

    -- Device pre-registration
    device_name VARCHAR(255) NOT NULL,
    user_email VARCHAR(255),  -- Made optional for code-based enrollment
    user_name VARCHAR(255),

    -- Associated enrollment token
    enrollment_token_id UUID REFERENCES enrollment_tokens(id) ON DELETE SET NULL,

    -- Link lifecycle
    created_at TIMESTAMPTZ DEFAULT NOW(),
    created_by INTEGER,  -- User ID
    expires_at TIMESTAMPTZ NOT NULL,

    -- Download tracking
    downloaded_at TIMESTAMPTZ,
    download_ip VARCHAR(45),
    download_user_agent TEXT,
    download_count INTEGER DEFAULT 0,

    -- Installation tracking
    agent_connected_at TIMESTAMPTZ,
    device_id INTEGER,  -- Device ID

    -- Status: pending, downloaded, installing, installed, expired, revoked
    status VARCHAR(50) DEFAULT 'pending' CHECK (status IN ('pending', 'downloaded', 'installing', 'installed', 'expired', 'revoked')),
    revoked_at TIMESTAMPTZ,
    revoked_by INTEGER,

    -- Email notifications
    email_sent_at TIMESTAMPTZ,
    email_delivery_status VARCHAR(50),
    email_opened_at TIMESTAMPTZ,
    reminder_sent_at TIMESTAMPTZ,

    -- Metadata
    notes TEXT,
    metadata JSONB DEFAULT '{}'::jsonb,

    -- Soft delete support
    deleted_at TIMESTAMPTZ
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_agent_links_token ON agent_installation_links(download_token);
CREATE INDEX IF NOT EXISTS idx_agent_links_code ON agent_installation_links(installation_code);
CREATE INDEX IF NOT EXISTS idx_agent_links_org ON agent_installation_links(organization_id);
CREATE INDEX IF NOT EXISTS idx_agent_links_status ON agent_installation_links(status);
CREATE INDEX IF NOT EXISTS idx_agent_links_created ON agent_installation_links(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_links_device ON agent_installation_links(device_id);
CREATE INDEX IF NOT EXISTS idx_agent_links_enrollment ON agent_installation_links(enrollment_token_id);
CREATE INDEX IF NOT EXISTS idx_agent_links_expires ON agent_installation_links(expires_at);
CREATE INDEX IF NOT EXISTS idx_agent_links_email ON agent_installation_links(user_email);
CREATE INDEX IF NOT EXISTS idx_agent_links_not_deleted ON agent_installation_links(deleted_at) WHERE deleted_at IS NULL;

-- Audit table for link access attempts
CREATE TABLE IF NOT EXISTS agent_link_access_log (
    id SERIAL PRIMARY KEY,
    link_id UUID REFERENCES agent_installation_links(id) ON DELETE CASCADE,
    accessed_at TIMESTAMPTZ DEFAULT NOW(),
    ip_address VARCHAR(45),
    user_agent TEXT,
    action VARCHAR(50) NOT NULL,  -- view, download, validate, validate_code, status_check
    success BOOLEAN DEFAULT TRUE,
    error_message TEXT,
    metadata JSONB DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_link_access_link_id ON agent_link_access_log(link_id);
CREATE INDEX IF NOT EXISTS idx_link_access_time ON agent_link_access_log(accessed_at DESC);

-- Email configuration table
CREATE TABLE IF NOT EXISTS email_config (
    id SERIAL PRIMARY KEY,
    provider VARCHAR(50) NOT NULL DEFAULT 'sendgrid',
    api_key_encrypted TEXT,
    from_address VARCHAR(255) NOT NULL DEFAULT 'noreply@sentinel.local',
    from_name VARCHAR(255) DEFAULT 'Sentinel RMM',
    reply_to VARCHAR(255),
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Email templates table
CREATE TABLE IF NOT EXISTS email_templates (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    subject_template TEXT NOT NULL,
    html_template TEXT NOT NULL,
    text_template TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Comments
COMMENT ON TABLE agent_installation_links IS 'Self-service agent installation links for code-based and email-based enrollment';
COMMENT ON COLUMN agent_installation_links.installation_code IS 'Short installation code like AB12-CD34 for manual entry';
COMMENT ON COLUMN agent_installation_links.organization_id IS 'Multi-tenant organization ID';
