-- Device update status tracking
CREATE TABLE IF NOT EXISTS device_updates (
    id SERIAL PRIMARY KEY,
    device_id TEXT NOT NULL UNIQUE REFERENCES devices(id) ON DELETE CASCADE,
    pending_count INTEGER DEFAULT 0,
    security_update_count INTEGER DEFAULT 0,
    reboot_required BOOLEAN DEFAULT FALSE,
    last_checked TIMESTAMP WITH TIME ZONE,
    last_update_installed TIMESTAMP WITH TIME ZONE,
    pending_updates JSONB DEFAULT '[]'::jsonb,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Index for quick device lookups
CREATE INDEX IF NOT EXISTS idx_device_updates_device_id ON device_updates(device_id);

-- Index for finding devices with pending security updates
CREATE INDEX IF NOT EXISTS idx_device_updates_security ON device_updates(security_update_count);

-- Index for finding devices requiring reboot
CREATE INDEX IF NOT EXISTS idx_device_updates_reboot ON device_updates(reboot_required);
