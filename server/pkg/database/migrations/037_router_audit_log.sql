-- Router scheduled actions table
CREATE TABLE IF NOT EXISTS router_scheduled_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    action_type TEXT NOT NULL,
    cron_expression TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT false,
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_router_scheduled_actions_active ON router_scheduled_actions (is_active);

-- Router audit log table
CREATE TABLE IF NOT EXISTS router_audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    target_mac TEXT,
    metadata JSONB DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'success',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_router_audit_log_action ON router_audit_log (action);
CREATE INDEX IF NOT EXISTS idx_router_audit_log_created_at ON router_audit_log (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_router_audit_log_action_created ON router_audit_log (action, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_router_audit_log_target_mac ON router_audit_log (target_mac) WHERE target_mac IS NOT NULL;
