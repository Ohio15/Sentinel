-- +migrate Up
-- Script scheduling support

CREATE TABLE IF NOT EXISTS script_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
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

CREATE INDEX idx_script_schedules_organization ON script_schedules(organization_id);
CREATE INDEX idx_script_schedules_script ON script_schedules(script_id);
CREATE INDEX idx_script_schedules_next_run ON script_schedules(next_run_at) WHERE is_enabled = true;

-- Script execution history
CREATE TABLE IF NOT EXISTS script_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schedule_id UUID REFERENCES script_schedules(id) ON DELETE SET NULL,
    script_id UUID NOT NULL REFERENCES scripts(id) ON DELETE CASCADE,
    device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

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

CREATE INDEX idx_script_executions_schedule ON script_executions(schedule_id);
CREATE INDEX idx_script_executions_script ON script_executions(script_id);
CREATE INDEX idx_script_executions_device ON script_executions(device_id);
CREATE INDEX idx_script_executions_status ON script_executions(status);
CREATE INDEX idx_script_executions_created ON script_executions(created_at);

-- +migrate Down
DROP TABLE IF EXISTS script_executions;
DROP TABLE IF EXISTS script_schedules;
