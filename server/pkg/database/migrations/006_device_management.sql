-- Migration: Device Management Enhancements
-- Adds is_disabled column and updates status constraint for device management

-- Step 1: Add is_disabled column to devices table
ALTER TABLE devices ADD COLUMN IF NOT EXISTS is_disabled BOOLEAN DEFAULT FALSE;

-- Step 2: Add disabled_at timestamp
ALTER TABLE devices ADD COLUMN IF NOT EXISTS disabled_at TIMESTAMPTZ;

-- Step 3: Add disabled_by to track who disabled the device
ALTER TABLE devices ADD COLUMN IF NOT EXISTS disabled_by UUID REFERENCES users(id);

-- Step 4: Update status constraint to include 'uninstalling' and 'disabled' states
-- First drop the existing constraint
ALTER TABLE devices DROP CONSTRAINT IF EXISTS devices_status_check;

-- Then add the new constraint with additional statuses
ALTER TABLE devices ADD CONSTRAINT devices_status_check
    CHECK (status IN ('online', 'offline', 'warning', 'critical', 'uninstalling', 'disabled'));

-- Step 5: Create index for disabled devices (useful for filtering)
CREATE INDEX IF NOT EXISTS idx_devices_is_disabled ON devices(is_disabled) WHERE is_disabled = TRUE;

-- Step 6: Add comment for documentation
COMMENT ON COLUMN devices.is_disabled IS 'When true, the device agent will be rejected from connecting';
COMMENT ON COLUMN devices.disabled_at IS 'Timestamp when the device was disabled';
COMMENT ON COLUMN devices.disabled_by IS 'User who disabled the device';
