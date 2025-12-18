-- Sentinel RMM PostgreSQL Schema
-- Migration: 010_update_groups
-- Add update groups for staged rollouts

-- Create update_groups table
CREATE TABLE IF NOT EXISTS update_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE,
    description TEXT,
    priority INTEGER NOT NULL DEFAULT 0,           -- Lower = earlier in rollout (0=test, 10=pilot, 100=production)
    auto_promote BOOLEAN DEFAULT FALSE,            -- Automatically promote to next stage on success
    success_threshold_percent REAL DEFAULT 95.0,   -- Required success rate to auto-promote
    failure_threshold_percent REAL DEFAULT 10.0,   -- Failure rate that triggers rollback
    min_devices_for_decision INTEGER DEFAULT 3,    -- Min devices before making promote/rollback decision
    wait_time_minutes INTEGER DEFAULT 60,          -- Time to wait after stage completion before promotion
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Add group membership to devices
ALTER TABLE devices ADD COLUMN IF NOT EXISTS update_group_id UUID REFERENCES update_groups(id) ON DELETE SET NULL;

-- Create index for device group lookups
CREATE INDEX IF NOT EXISTS idx_devices_update_group ON devices(update_group_id);
CREATE INDEX IF NOT EXISTS idx_update_groups_priority ON update_groups(priority);

-- Insert default groups
INSERT INTO update_groups (name, description, priority, auto_promote, success_threshold_percent, failure_threshold_percent, min_devices_for_decision, wait_time_minutes)
VALUES
    ('test', 'Test devices for initial update validation', 0, TRUE, 100.0, 5.0, 1, 30),
    ('pilot', 'Pilot group for broader testing before production', 10, TRUE, 95.0, 10.0, 3, 60),
    ('production', 'Production devices - main deployment group', 100, FALSE, 90.0, 15.0, 5, 120)
ON CONFLICT (name) DO NOTHING;
