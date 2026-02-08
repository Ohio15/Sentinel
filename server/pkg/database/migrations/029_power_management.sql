-- Add power management column to devices table
-- Tracks Wake-on-LAN and Intel AMT/vPro capabilities

ALTER TABLE devices ADD COLUMN IF NOT EXISTS power_management JSONB DEFAULT '{}';

-- Add index for devices with WoL enabled (common query for wake operations)
CREATE INDEX IF NOT EXISTS idx_devices_wol ON devices ((power_management->>'wol_supported'))
    WHERE power_management->>'wol_supported' = 'true';
