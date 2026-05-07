-- Migration: 023_organization_multi_tenant.sql
-- Purpose: Add organization_id to all tables for future multi-tenant support
-- Note: Currently hardcoded to organization_id = 1 (single tenant)
-- When ready for multi-tenant, replace hardcoded 1 with dynamic tenant resolution

-- Create organizations table
CREATE TABLE IF NOT EXISTS organizations (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(100) UNIQUE NOT NULL,
    settings JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    is_active BOOLEAN DEFAULT true
);

-- Insert default organization
INSERT INTO organizations (id, name, slug)
VALUES (1, 'Default Organization', 'default')
ON CONFLICT (id) DO NOTHING;

-- Reset sequence to ensure future inserts start at 2
SELECT setval('organizations_id_seq', GREATEST((SELECT MAX(id) FROM organizations), 1));

-- Add organization_id to users table
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'users' AND column_name = 'organization_id') THEN
        ALTER TABLE users ADD COLUMN organization_id INTEGER DEFAULT 1 REFERENCES organizations(id);
    END IF;
END $$;

-- Add organization_id to devices table
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'devices' AND column_name = 'organization_id') THEN
        ALTER TABLE devices ADD COLUMN organization_id INTEGER DEFAULT 1 REFERENCES organizations(id);
    END IF;
END $$;

-- Add organization_id to enrollment_tokens table
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'enrollment_tokens' AND column_name = 'organization_id') THEN
        ALTER TABLE enrollment_tokens ADD COLUMN organization_id INTEGER DEFAULT 1 REFERENCES organizations(id);
    END IF;
END $$;

-- Add organization_id to device_metrics table
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'device_metrics' AND column_name = 'organization_id') THEN
        ALTER TABLE device_metrics ADD COLUMN organization_id INTEGER DEFAULT 1;
    END IF;
END $$;

-- Add organization_id to commands table
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'commands' AND column_name = 'organization_id') THEN
        ALTER TABLE commands ADD COLUMN organization_id INTEGER DEFAULT 1 REFERENCES organizations(id);
    END IF;
END $$;

-- Add organization_id to scripts table
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'scripts' AND column_name = 'organization_id') THEN
        ALTER TABLE scripts ADD COLUMN organization_id INTEGER DEFAULT 1 REFERENCES organizations(id);
    END IF;
END $$;

-- Add organization_id to alerts table
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'alerts' AND column_name = 'organization_id') THEN
        ALTER TABLE alerts ADD COLUMN organization_id INTEGER DEFAULT 1 REFERENCES organizations(id);
    END IF;
END $$;

-- Add organization_id to alert_rules table
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'alert_rules' AND column_name = 'organization_id') THEN
        ALTER TABLE alert_rules ADD COLUMN organization_id INTEGER DEFAULT 1 REFERENCES organizations(id);
    END IF;
END $$;

-- Add organization_id to sessions table
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'sessions' AND column_name = 'organization_id') THEN
        ALTER TABLE sessions ADD COLUMN organization_id INTEGER DEFAULT 1 REFERENCES organizations(id);
    END IF;
END $$;

-- Add organization_id to audit_log table
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'audit_log' AND column_name = 'organization_id') THEN
        ALTER TABLE audit_log ADD COLUMN organization_id INTEGER DEFAULT 1;
    END IF;
END $$;

-- Add organization_id to agent_updates table
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'agent_updates' AND column_name = 'organization_id') THEN
        ALTER TABLE agent_updates ADD COLUMN organization_id INTEGER DEFAULT 1 REFERENCES organizations(id);
    END IF;
END $$;

-- Add organization_id to agent_installation_links table
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'agent_installation_links' AND column_name = 'organization_id') THEN
        ALTER TABLE agent_installation_links ADD COLUMN organization_id INTEGER DEFAULT 1 REFERENCES organizations(id);
    END IF;
END $$;

-- Add organization_id to tickets table (if exists)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'tickets') THEN
        IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'tickets' AND column_name = 'organization_id') THEN
            ALTER TABLE tickets ADD COLUMN organization_id INTEGER DEFAULT 1 REFERENCES organizations(id);
        END IF;
    END IF;
END $$;

-- Add organization_id to clients table (if exists)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'clients') THEN
        IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'clients' AND column_name = 'organization_id') THEN
            ALTER TABLE clients ADD COLUMN organization_id INTEGER DEFAULT 1 REFERENCES organizations(id);
        END IF;
    END IF;
END $$;

-- Add organization_id to portal_users table (if exists)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'portal_users') THEN
        IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'portal_users' AND column_name = 'organization_id') THEN
            ALTER TABLE portal_users ADD COLUMN organization_id INTEGER DEFAULT 1 REFERENCES organizations(id);
        END IF;
    END IF;
END $$;

-- Add organization_id to knowledge_base_articles table (if exists)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'knowledge_base_articles') THEN
        IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'knowledge_base_articles' AND column_name = 'organization_id') THEN
            ALTER TABLE knowledge_base_articles ADD COLUMN organization_id INTEGER DEFAULT 1 REFERENCES organizations(id);
        END IF;
    END IF;
END $$;

-- Update all existing records to have organization_id = 1
UPDATE users SET organization_id = 1 WHERE organization_id IS NULL;
UPDATE devices SET organization_id = 1 WHERE organization_id IS NULL;
UPDATE enrollment_tokens SET organization_id = 1 WHERE organization_id IS NULL;
UPDATE device_metrics SET organization_id = 1 WHERE organization_id IS NULL;
UPDATE commands SET organization_id = 1 WHERE organization_id IS NULL;
UPDATE scripts SET organization_id = 1 WHERE organization_id IS NULL;
UPDATE alerts SET organization_id = 1 WHERE organization_id IS NULL;
UPDATE alert_rules SET organization_id = 1 WHERE organization_id IS NULL;
UPDATE sessions SET organization_id = 1 WHERE organization_id IS NULL;
UPDATE audit_log SET organization_id = 1 WHERE organization_id IS NULL;
UPDATE agent_updates SET organization_id = 1 WHERE organization_id IS NULL;
UPDATE agent_installation_links SET organization_id = 1 WHERE organization_id IS NULL;

-- Update optional tables if they exist
DO $$ BEGIN UPDATE tickets SET organization_id = 1 WHERE organization_id IS NULL; EXCEPTION WHEN undefined_table THEN NULL; END $$;
DO $$ BEGIN UPDATE clients SET organization_id = 1 WHERE organization_id IS NULL; EXCEPTION WHEN undefined_table THEN NULL; END $$;
DO $$ BEGIN UPDATE portal_users SET organization_id = 1 WHERE organization_id IS NULL; EXCEPTION WHEN undefined_table THEN NULL; END $$;
DO $$ BEGIN UPDATE knowledge_base_articles SET organization_id = 1 WHERE organization_id IS NULL; EXCEPTION WHEN undefined_table THEN NULL; END $$;

-- Make organization_id NOT NULL on core tables (after data migration)
ALTER TABLE users ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE devices ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE enrollment_tokens ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE commands ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE scripts ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE alerts ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE alert_rules ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE agent_updates ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE agent_installation_links ALTER COLUMN organization_id SET NOT NULL;

-- Create indexes for performance
CREATE INDEX IF NOT EXISTS idx_users_org ON users(organization_id);
CREATE INDEX IF NOT EXISTS idx_devices_org ON devices(organization_id);
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_org ON enrollment_tokens(organization_id);
CREATE INDEX IF NOT EXISTS idx_device_metrics_org ON device_metrics(organization_id);
CREATE INDEX IF NOT EXISTS idx_commands_org ON commands(organization_id);
CREATE INDEX IF NOT EXISTS idx_scripts_org ON scripts(organization_id);
CREATE INDEX IF NOT EXISTS idx_alerts_org ON alerts(organization_id);
CREATE INDEX IF NOT EXISTS idx_alert_rules_org ON alert_rules(organization_id);
CREATE INDEX IF NOT EXISTS idx_agent_updates_org ON agent_updates(organization_id);
CREATE INDEX IF NOT EXISTS idx_agent_installation_links_org ON agent_installation_links(organization_id);

-- Composite indexes for common queries
CREATE INDEX IF NOT EXISTS idx_devices_org_status ON devices(organization_id, status);
CREATE INDEX IF NOT EXISTS idx_device_metrics_org_device ON device_metrics(organization_id, device_id);
CREATE INDEX IF NOT EXISTS idx_commands_org_device ON commands(organization_id, device_id);
CREATE INDEX IF NOT EXISTS idx_alerts_org_status ON alerts(organization_id, status);
