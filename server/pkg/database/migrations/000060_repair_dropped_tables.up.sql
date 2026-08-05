-- Repair migration: recreate the objects that the legacy migration runner
-- created and then immediately dropped.
--
-- WHY THIS EXISTS
-- ---------------
-- Migrations 000042/000043/000044 were authored for rubenv/sql-migrate, which
-- puts both directions in ONE file and splits them on `-- +migrate Up` /
-- `-- +migrate Down` marker comments. This repo runs golang-migrate, which has
-- no notion of those markers and executes the WHOLE file. Every one of those
-- migrations therefore created its tables and then ran its own Down block
-- against them, back to back, in the same implicit transaction. Nothing
-- errored, schema_migrations recorded the version as applied, and the runner
-- exited 0 — the schema was simply missing. 000047 never executed at all.
-- (See TestNoSQLMigrateDirectivesInUpMigrations in migrations_test.go for the
-- static guard that now prevents a recurrence, and PR #70 which removed the
-- inline Down sections and corrected the organization_id FK types.)
--
-- Production is at schema_migrations version 59, dirty=f, and is missing
-- exactly:
--   tables  patch_policies, patch_approvals, device_patch_assignments,
--           patch_installations                                       (000042)
--           mfa_events                                                (000043)
--           script_schedules, script_executions                       (000044)
--           usb_file_transfers                                        (000047)
--   columns users.totp_secret, users.totp_enabled, users.totp_verified_at,
--           users.backup_codes, users.mfa_required                    (000043)
--           alerts.metadata (+ its GIN index)                         (000047)
--
-- Versions 42-44 and 47 are already recorded as applied, so golang-migrate
-- will never re-run them. A forward repair at version 60 is the only way to
-- reconcile an existing deployment.
--
-- IDEMPOTENCY DESIGN
-- ------------------
-- The definitions below are byte-faithful copies of the CORRECTED 000042 /
-- 000043 / 000044 / 000047 up-migrations on main after PR #70 (INTEGER
-- organization_id FKs matching organizations.id SERIAL from 000034, no inline
-- Down sections), with every statement made unconditional-safe:
-- CREATE TABLE IF NOT EXISTS, ADD COLUMN IF NOT EXISTS, CREATE INDEX IF NOT
-- EXISTS. On a FRESH database, 000042-000047 create all of this correctly and
-- this migration is a complete no-op. On the existing production database it
-- creates exactly what the legacy runner destroyed. No statement here depends
-- on whether any object already exists.
--
-- Deliberately NOT re-created: 000041's webhooks objects. Production has them;
-- this repair is scoped to the objects verified missing.

-- ===========================================================================
-- 000042_patch_approvals — patch approval workflow for Windows Updates
-- ===========================================================================

CREATE TABLE IF NOT EXISTS patch_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- INTEGER to match organizations.id (SERIAL, 000034), per the PR #70
    -- correction to 000042. A UUID here makes the inline FK fail outright:
    -- "Key columns organization_id and id are of incompatible types:
    -- uuid and integer".
    organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    auto_approve_security BOOLEAN NOT NULL DEFAULT true,  -- Auto-approve security updates
    auto_approve_critical BOOLEAN NOT NULL DEFAULT true,  -- Auto-approve critical updates
    auto_approve_after_days INTEGER DEFAULT 7,            -- Auto-approve other updates after X days
    require_manual_approval BOOLEAN NOT NULL DEFAULT false, -- Require manual approval for all
    maintenance_window_start TIME,                         -- Start of maintenance window
    maintenance_window_end TIME,                           -- End of maintenance window
    maintenance_days INTEGER[] DEFAULT '{0,6}',           -- Days of week (0=Sunday, 6=Saturday)
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_patch_policies_organization ON patch_policies(organization_id);

-- Individual patch approvals (overrides policy)
CREATE TABLE IF NOT EXISTS patch_approvals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- INTEGER to match organizations.id (SERIAL, 000034), per the PR #70
    -- correction to 000042.
    organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    kb_article VARCHAR(50) NOT NULL,                      -- KB number (e.g., "KB5001234")
    title TEXT NOT NULL,
    classification VARCHAR(100),                          -- Security, Critical, etc.
    status VARCHAR(20) NOT NULL DEFAULT 'pending',        -- pending, approved, denied, superseded
    approved_by UUID REFERENCES users(id),
    approved_at TIMESTAMP WITH TIME ZONE,
    denied_reason TEXT,
    auto_approved BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(organization_id, kb_article)
);

CREATE INDEX IF NOT EXISTS idx_patch_approvals_organization ON patch_approvals(organization_id);
CREATE INDEX IF NOT EXISTS idx_patch_approvals_status ON patch_approvals(status);
CREATE INDEX IF NOT EXISTS idx_patch_approvals_kb ON patch_approvals(kb_article);

-- Device-specific patch assignments
CREATE TABLE IF NOT EXISTS device_patch_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    policy_id UUID REFERENCES patch_policies(id) ON DELETE SET NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_device_patch_assignments_device ON device_patch_assignments(device_id);
CREATE INDEX IF NOT EXISTS idx_device_patch_assignments_policy ON device_patch_assignments(policy_id);

-- Patch installation history
CREATE TABLE IF NOT EXISTS patch_installations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    kb_article VARCHAR(50) NOT NULL,
    title TEXT,
    status VARCHAR(20) NOT NULL,                          -- pending, installing, installed, failed
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    error TEXT,
    reboot_required BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_patch_installations_device ON patch_installations(device_id);
CREATE INDEX IF NOT EXISTS idx_patch_installations_kb ON patch_installations(kb_article);
CREATE INDEX IF NOT EXISTS idx_patch_installations_status ON patch_installations(status);

-- ===========================================================================
-- 000043_mfa_totp — Multi-Factor Authentication (TOTP) support
-- ===========================================================================

-- Add MFA columns to users table. These were already IF NOT EXISTS in 000043;
-- they are missing in production because 000043's inline Down block dropped
-- them immediately after adding them.
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_secret TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_verified_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS backup_codes TEXT[];
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_required BOOLEAN NOT NULL DEFAULT false;

-- MFA audit log
CREATE TABLE IF NOT EXISTS mfa_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_type VARCHAR(50) NOT NULL,  -- enabled, disabled, verified, failed, backup_used
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mfa_events_user ON mfa_events(user_id);
CREATE INDEX IF NOT EXISTS idx_mfa_events_created ON mfa_events(created_at);

-- ===========================================================================
-- 000044_script_scheduling — script scheduling support
-- ===========================================================================

CREATE TABLE IF NOT EXISTS script_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- INTEGER to match organizations.id (SERIAL, 000034), per the PR #70
    -- correction to 000044.
    organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    script_id UUID NOT NULL REFERENCES scripts(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,

    -- Scheduling
    schedule_type VARCHAR(20) NOT NULL DEFAULT 'cron', -- cron, once, interval
    cron_expression VARCHAR(100),                       -- For cron schedules
    interval_minutes INTEGER,                           -- For interval schedules
    run_at TIMESTAMP WITH TIME ZONE,                   -- For one-time schedules
    timezone VARCHAR(50) NOT NULL DEFAULT 'UTC',

    -- Targeting
    target_type VARCHAR(20) NOT NULL DEFAULT 'all',    -- all, group, specific
    target_device_ids UUID[],                          -- For specific targeting
    target_group_id UUID,                              -- For group targeting

    -- Execution options
    timeout_seconds INTEGER DEFAULT 300,
    run_as_system BOOLEAN NOT NULL DEFAULT true,
    stop_on_error BOOLEAN NOT NULL DEFAULT false,

    -- Status
    is_enabled BOOLEAN NOT NULL DEFAULT true,
    last_run_at TIMESTAMP WITH TIME ZONE,
    next_run_at TIMESTAMP WITH TIME ZONE,
    last_run_status VARCHAR(20),                       -- success, failed, partial

    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_script_schedules_organization ON script_schedules(organization_id);
CREATE INDEX IF NOT EXISTS idx_script_schedules_script ON script_schedules(script_id);
-- Partial index predicate is IMMUTABLE (no NOW()), so it is valid index DDL.
CREATE INDEX IF NOT EXISTS idx_script_schedules_next_run ON script_schedules(next_run_at) WHERE is_enabled = true;

-- Script execution history
CREATE TABLE IF NOT EXISTS script_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schedule_id UUID REFERENCES script_schedules(id) ON DELETE SET NULL,
    script_id UUID NOT NULL REFERENCES scripts(id) ON DELETE CASCADE,
    device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
    -- INTEGER to match organizations.id (SERIAL, 000034), per the PR #70
    -- correction to 000044.
    organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    status VARCHAR(20) NOT NULL DEFAULT 'pending',    -- pending, running, success, failed, timeout
    exit_code INTEGER,
    output TEXT,
    error TEXT,
    duration_ms INTEGER,

    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_script_executions_schedule ON script_executions(schedule_id);
CREATE INDEX IF NOT EXISTS idx_script_executions_script ON script_executions(script_id);
CREATE INDEX IF NOT EXISTS idx_script_executions_device ON script_executions(device_id);
CREATE INDEX IF NOT EXISTS idx_script_executions_status ON script_executions(status);
CREATE INDEX IF NOT EXISTS idx_script_executions_created ON script_executions(created_at);

-- ===========================================================================
-- 000047_usb_file_transfers — USB file transfer tracking
-- Tracks files transferred to USB storage devices for security auditing
-- ===========================================================================

-- USB file transfers table (records files written to USB drives)
CREATE TABLE IF NOT EXISTS usb_file_transfers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    usb_device_id VARCHAR(255) NOT NULL,      -- Links to usb_devices.usb_device_id
    session_id UUID NOT NULL,                  -- Groups transfers in one connection session

    -- File details
    file_name VARCHAR(500) NOT NULL,
    file_path VARCHAR(1000),                   -- Relative path on USB
    file_size BIGINT NOT NULL,                 -- Bytes
    transfer_time TIMESTAMPTZ NOT NULL,

    -- Transfer metadata
    operation VARCHAR(20) NOT NULL DEFAULT 'write',  -- write, copy, move

    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_usb_transfers_device ON usb_file_transfers(device_id);
CREATE INDEX IF NOT EXISTS idx_usb_transfers_session ON usb_file_transfers(session_id);
CREATE INDEX IF NOT EXISTS idx_usb_transfers_usb_device ON usb_file_transfers(usb_device_id);
CREATE INDEX IF NOT EXISTS idx_usb_transfers_time ON usb_file_transfers(transfer_time DESC);

-- Add metadata column to alerts for linking file transfers and other contextual data
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}'::jsonb;

-- Index for efficient JSONB queries on alert metadata
CREATE INDEX IF NOT EXISTS idx_alerts_metadata ON alerts USING GIN (metadata);
