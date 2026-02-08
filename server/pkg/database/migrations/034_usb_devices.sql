-- Migration: USB Device Tracking and Alerts
-- Tracks external USB device connections and provides alerting for security

-- USB device connections table (stores device events)
CREATE TABLE IF NOT EXISTS usb_devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,

    -- USB device identification
    usb_device_id VARCHAR(255) NOT NULL,       -- Unique ID (VID:PID:Serial or instance path)
    instance_path VARCHAR(512),                 -- Platform-specific path
    vendor_id VARCHAR(10) NOT NULL,             -- USB Vendor ID (e.g., "0x8086")
    product_id VARCHAR(10) NOT NULL,            -- USB Product ID (e.g., "0x1234")
    serial_number VARCHAR(255),                 -- Device serial number

    -- Descriptive info
    manufacturer VARCHAR(255),
    product_name VARCHAR(255),
    device_class VARCHAR(50) NOT NULL,          -- mass_storage, hid, audio, etc.
    class_code INTEGER DEFAULT 0,
    subclass_code INTEGER DEFAULT 0,
    protocol_code INTEGER DEFAULT 0,

    -- Connection details
    bus_number INTEGER,
    port_number INTEGER,
    device_speed VARCHAR(20),                   -- low, full, high, super, super+
    parent_device VARCHAR(255),

    -- Storage info (for mass storage devices)
    drive_letter VARCHAR(10),
    mount_point VARCHAR(255),
    volume_label VARCHAR(255),
    file_system VARCHAR(50),
    total_size BIGINT,
    free_space BIGINT,

    -- State
    is_connected BOOLEAN DEFAULT true,
    connection_time TIMESTAMPTZ NOT NULL,
    disconnection_time TIMESTAMPTZ,

    -- Security flags
    is_removable BOOLEAN DEFAULT false,
    is_bootable BOOLEAN DEFAULT false,
    is_encrypted BOOLEAN DEFAULT false,
    is_approved BOOLEAN DEFAULT false,          -- Whether device is in approved list

    -- Metadata
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Unique constraint for upsert operations
CREATE UNIQUE INDEX IF NOT EXISTS idx_usb_devices_unique_device ON usb_devices(device_id, usb_device_id);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_usb_devices_device_id ON usb_devices(device_id);
CREATE INDEX IF NOT EXISTS idx_usb_devices_vendor_id ON usb_devices(vendor_id);
CREATE INDEX IF NOT EXISTS idx_usb_devices_device_class ON usb_devices(device_class);
CREATE INDEX IF NOT EXISTS idx_usb_devices_is_connected ON usb_devices(is_connected);
CREATE INDEX IF NOT EXISTS idx_usb_devices_connection_time ON usb_devices(connection_time DESC);
CREATE INDEX IF NOT EXISTS idx_usb_devices_usb_device_id ON usb_devices(usb_device_id);

-- USB device events (audit log of all connection/disconnection events)
CREATE TABLE IF NOT EXISTS usb_device_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    usb_device_id UUID REFERENCES usb_devices(id) ON DELETE SET NULL,

    -- Event info
    event_type VARCHAR(50) NOT NULL,            -- connected, disconnected, changed, blocked

    -- Device snapshot at time of event
    vendor_id VARCHAR(10) NOT NULL,
    product_id VARCHAR(10) NOT NULL,
    serial_number VARCHAR(255),
    manufacturer VARCHAR(255),
    product_name VARCHAR(255),
    device_class VARCHAR(50),

    -- Storage info at time of event
    drive_letter VARCHAR(10),
    mount_point VARCHAR(255),
    volume_label VARCHAR(255),
    total_size BIGINT,

    -- Security assessment at time of event
    is_approved BOOLEAN DEFAULT false,
    policy_matched VARCHAR(255),                -- Which policy rule matched (if any)
    was_blocked BOOLEAN DEFAULT false,          -- If device was blocked by policy

    -- Alert info
    alert_generated BOOLEAN DEFAULT false,
    alert_id UUID REFERENCES alerts(id) ON DELETE SET NULL,

    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_usb_device_events_device_id ON usb_device_events(device_id);
CREATE INDEX IF NOT EXISTS idx_usb_device_events_event_type ON usb_device_events(event_type);
CREATE INDEX IF NOT EXISTS idx_usb_device_events_created_at ON usb_device_events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_usb_device_events_vendor_product ON usb_device_events(vendor_id, product_id);

-- USB device policies (allow/block rules)
CREATE TABLE IF NOT EXISTS usb_device_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT,

    -- Scope
    organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    applies_to_all BOOLEAN DEFAULT true,        -- If false, use device_groups

    -- Policy type
    policy_type VARCHAR(20) NOT NULL DEFAULT 'block', -- allow, block, alert
    priority INTEGER DEFAULT 100,               -- Lower = higher priority

    -- Matching criteria (all optional, AND logic)
    vendor_ids JSONB DEFAULT '[]'::jsonb,       -- ["0x8086", "0x1234"]
    product_ids JSONB DEFAULT '[]'::jsonb,      -- ["0x8086:0x1234"]
    serial_numbers JSONB DEFAULT '[]'::jsonb,   -- Specific serial numbers
    device_classes JSONB DEFAULT '[]'::jsonb,   -- ["mass_storage", "hid"]

    -- Actions
    generate_alert BOOLEAN DEFAULT true,
    alert_severity VARCHAR(20) DEFAULT 'warning',
    block_device BOOLEAN DEFAULT false,         -- Future: attempt to disable device

    -- Status
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_usb_policies_org ON usb_device_policies(organization_id);
CREATE INDEX IF NOT EXISTS idx_usb_policies_active ON usb_device_policies(is_active, priority);

-- Approved devices list (trusted devices that won't trigger alerts)
CREATE TABLE IF NOT EXISTS usb_approved_devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Scope
    organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    device_id UUID REFERENCES devices(id) ON DELETE CASCADE, -- NULL = org-wide

    -- Device identification (match any non-null criteria)
    vendor_id VARCHAR(10),
    product_id VARCHAR(10),
    serial_number VARCHAR(255),

    -- Description
    name VARCHAR(255) NOT NULL,
    description TEXT,
    approved_by VARCHAR(255),

    created_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ                      -- NULL = never expires
);

CREATE INDEX IF NOT EXISTS idx_usb_approved_org ON usb_approved_devices(organization_id);
CREATE INDEX IF NOT EXISTS idx_usb_approved_device ON usb_approved_devices(device_id);
CREATE INDEX IF NOT EXISTS idx_usb_approved_vendor_product ON usb_approved_devices(vendor_id, product_id);
CREATE INDEX IF NOT EXISTS idx_usb_approved_serial ON usb_approved_devices(serial_number);

-- Add USB-related alert rule types
-- These are added to support USB events in the alert evaluation engine
INSERT INTO alert_rule_types (name, description, category, parameters_schema)
VALUES
    ('usb_device_connected', 'Alert when any USB device is connected', 'security',
     '{"device_classes": {"type": "array", "items": {"type": "string"}}, "exclude_approved": {"type": "boolean", "default": true}}'::jsonb),
    ('usb_mass_storage_connected', 'Alert when USB mass storage device is connected', 'security',
     '{"exclude_approved": {"type": "boolean", "default": true}}'::jsonb),
    ('usb_unauthorized_device', 'Alert when unauthorized USB device is connected', 'security',
     '{"check_vendor_whitelist": {"type": "boolean", "default": true}}'::jsonb),
    ('usb_device_disconnected', 'Alert when USB device is disconnected', 'security',
     '{"device_classes": {"type": "array", "items": {"type": "string"}}}'::jsonb)
ON CONFLICT (name) DO NOTHING;

-- Function to update timestamp on USB device changes
CREATE OR REPLACE FUNCTION update_usb_device_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger for auto-updating timestamps
DROP TRIGGER IF EXISTS usb_devices_updated_at ON usb_devices;
CREATE TRIGGER usb_devices_updated_at
    BEFORE UPDATE ON usb_devices
    FOR EACH ROW
    EXECUTE FUNCTION update_usb_device_timestamp();

DROP TRIGGER IF EXISTS usb_policies_updated_at ON usb_device_policies;
CREATE TRIGGER usb_policies_updated_at
    BEFORE UPDATE ON usb_device_policies
    FOR EACH ROW
    EXECUTE FUNCTION update_usb_device_timestamp();
