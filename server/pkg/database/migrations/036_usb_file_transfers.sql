-- Migration: USB File Transfer Tracking
-- Tracks files transferred to USB storage devices for security auditing

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
