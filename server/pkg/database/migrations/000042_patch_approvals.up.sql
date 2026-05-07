-- +migrate Up
-- Patch approval workflow for Windows Updates

CREATE TABLE IF NOT EXISTS patch_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
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

CREATE INDEX idx_patch_policies_organization ON patch_policies(organization_id);

-- Individual patch approvals (overrides policy)
CREATE TABLE IF NOT EXISTS patch_approvals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
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

CREATE INDEX idx_patch_approvals_organization ON patch_approvals(organization_id);
CREATE INDEX idx_patch_approvals_status ON patch_approvals(status);
CREATE INDEX idx_patch_approvals_kb ON patch_approvals(kb_article);

-- Device-specific patch assignments
CREATE TABLE IF NOT EXISTS device_patch_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    policy_id UUID REFERENCES patch_policies(id) ON DELETE SET NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_device_patch_assignments_device ON device_patch_assignments(device_id);
CREATE INDEX idx_device_patch_assignments_policy ON device_patch_assignments(policy_id);

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

CREATE INDEX idx_patch_installations_device ON patch_installations(device_id);
CREATE INDEX idx_patch_installations_kb ON patch_installations(kb_article);
CREATE INDEX idx_patch_installations_status ON patch_installations(status);

-- +migrate Down
DROP TABLE IF EXISTS patch_installations;
DROP TABLE IF EXISTS device_patch_assignments;
DROP TABLE IF EXISTS patch_approvals;
DROP TABLE IF EXISTS patch_policies;
