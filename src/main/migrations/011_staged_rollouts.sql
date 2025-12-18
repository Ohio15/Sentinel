-- Sentinel RMM PostgreSQL Schema
-- Migration: 011_staged_rollouts
-- Add staged rollout tracking

-- Create rollouts table - tracks a complete rollout campaign
CREATE TABLE IF NOT EXISTS rollouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    release_version VARCHAR(50) NOT NULL,
    name VARCHAR(255),
    description TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',  -- pending, active, paused, completed, failed, rolled_back
    download_url TEXT,                               -- URL to download the release
    checksum VARCHAR(128),                           -- SHA256 checksum of the release
    created_by VARCHAR(255),
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create rollout_stages table - tracks each group in the rollout
CREATE TABLE IF NOT EXISTS rollout_stages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rollout_id UUID NOT NULL REFERENCES rollouts(id) ON DELETE CASCADE,
    group_id UUID NOT NULL REFERENCES update_groups(id) ON DELETE CASCADE,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',  -- pending, active, completed, failed, skipped
    total_devices INTEGER DEFAULT 0,
    completed_devices INTEGER DEFAULT 0,
    failed_devices INTEGER DEFAULT 0,
    success_rate REAL,
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    promoted_at TIMESTAMP WITH TIME ZONE,           -- When promotion was triggered
    promoted_by VARCHAR(255),                        -- 'auto' or username
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create rollout_devices table - individual device status in rollout
CREATE TABLE IF NOT EXISTS rollout_devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rollout_id UUID NOT NULL REFERENCES rollouts(id) ON DELETE CASCADE,
    stage_id UUID NOT NULL REFERENCES rollout_stages(id) ON DELETE CASCADE,
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',  -- pending, downloading, verifying, staging, restarting, completed, failed, rolled_back
    from_version VARCHAR(50),
    to_version VARCHAR(50),
    error_message TEXT,
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    attempts INTEGER DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(rollout_id, device_id)
);

-- Create rollout_events table for audit trail
CREATE TABLE IF NOT EXISTS rollout_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rollout_id UUID NOT NULL REFERENCES rollouts(id) ON DELETE CASCADE,
    stage_id UUID REFERENCES rollout_stages(id) ON DELETE CASCADE,
    device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
    event_type VARCHAR(50) NOT NULL,  -- stage_started, stage_completed, device_updated, device_failed, rollback_triggered, promotion_triggered
    message TEXT,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_rollouts_status ON rollouts(status);
CREATE INDEX IF NOT EXISTS idx_rollouts_created_at ON rollouts(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_rollout_stages_rollout ON rollout_stages(rollout_id);
CREATE INDEX IF NOT EXISTS idx_rollout_stages_status ON rollout_stages(status);
CREATE INDEX IF NOT EXISTS idx_rollout_devices_rollout ON rollout_devices(rollout_id);
CREATE INDEX IF NOT EXISTS idx_rollout_devices_device ON rollout_devices(device_id);
CREATE INDEX IF NOT EXISTS idx_rollout_devices_status ON rollout_devices(status);
CREATE INDEX IF NOT EXISTS idx_rollout_events_rollout ON rollout_events(rollout_id);
CREATE INDEX IF NOT EXISTS idx_rollout_events_created ON rollout_events(created_at DESC);
