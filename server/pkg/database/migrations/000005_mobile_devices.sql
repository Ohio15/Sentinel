-- Migration: Mobile device support for iOS and Android agents
-- Extends the devices table and adds mobile-specific tables

-- =============================================================================
-- EXTEND DEVICES TABLE
-- =============================================================================

-- Add device type and platform columns to existing devices table
ALTER TABLE devices ADD COLUMN IF NOT EXISTS device_type VARCHAR(20) DEFAULT 'desktop';
-- Values: 'desktop', 'laptop', 'server', 'mobile', 'tablet'

ALTER TABLE devices ADD COLUMN IF NOT EXISTS platform VARCHAR(20);
-- Values: 'windows', 'macos', 'linux', 'ios', 'android'

-- Add MDM enrollment status
ALTER TABLE devices ADD COLUMN IF NOT EXISTS mdm_enrolled BOOLEAN DEFAULT FALSE;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS mdm_enrollment_date TIMESTAMPTZ;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS mdm_profile_id VARCHAR(255);

-- Create index for device type queries
CREATE INDEX IF NOT EXISTS idx_devices_type ON devices(device_type);
CREATE INDEX IF NOT EXISTS idx_devices_platform ON devices(platform);
CREATE INDEX IF NOT EXISTS idx_devices_mdm ON devices(mdm_enrolled);

-- =============================================================================
-- MOBILE DEVICE DETAILS
-- =============================================================================

-- Extended mobile device information
CREATE TABLE IF NOT EXISTS mobile_devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL UNIQUE REFERENCES devices(id) ON DELETE CASCADE,

    -- Device identification
    imei VARCHAR(20),
    imei2 VARCHAR(20), -- Dual SIM devices
    meid VARCHAR(18),
    serial_number VARCHAR(100),

    -- Hardware info
    model_name VARCHAR(255),
    model_number VARCHAR(100),
    manufacturer VARCHAR(100),
    hardware_model VARCHAR(100), -- e.g., "iPhone14,2"

    -- OS info
    os_version VARCHAR(50),
    os_build VARCHAR(100),
    security_patch_level VARCHAR(50), -- Android only
    bootloader_version VARCHAR(100),
    baseband_version VARCHAR(100),
    kernel_version VARCHAR(200),

    -- Carrier info
    carrier_name VARCHAR(100),
    carrier_country VARCHAR(10),
    phone_number VARCHAR(50),
    iccid VARCHAR(30),

    -- Device state
    is_supervised BOOLEAN DEFAULT FALSE, -- iOS
    is_device_owner BOOLEAN DEFAULT FALSE, -- Android
    is_work_profile BOOLEAN DEFAULT FALSE, -- Android Enterprise
    knox_version VARCHAR(50), -- Samsung Knox

    -- Security state
    is_jailbroken BOOLEAN DEFAULT FALSE,
    is_rooted BOOLEAN DEFAULT FALSE,
    passcode_present BOOLEAN,
    passcode_compliant BOOLEAN,
    biometric_enabled BOOLEAN,
    encryption_enabled BOOLEAN,

    -- Activation lock (iOS)
    activation_lock_enabled BOOLEAN,
    find_my_enabled BOOLEAN,

    -- Last known location
    last_latitude DECIMAL(10, 8),
    last_longitude DECIMAL(11, 8),
    last_location_accuracy DECIMAL(10, 2), -- meters
    last_location_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_mobile_devices_device ON mobile_devices(device_id);
CREATE INDEX idx_mobile_devices_imei ON mobile_devices(imei);
CREATE INDEX idx_mobile_devices_serial ON mobile_devices(serial_number);

-- =============================================================================
-- PUSH NOTIFICATION TOKENS
-- =============================================================================

-- Store APNs and FCM tokens for push notifications
CREATE TABLE IF NOT EXISTS push_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    token_type VARCHAR(20) NOT NULL, -- 'apns', 'fcm', 'apns_voip'
    token TEXT NOT NULL,
    app_bundle_id VARCHAR(255),
    environment VARCHAR(20) DEFAULT 'production', -- 'sandbox', 'production'
    is_active BOOLEAN DEFAULT TRUE,
    last_used_at TIMESTAMPTZ,
    registered_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    UNIQUE(device_id, token_type, token)
);

CREATE INDEX idx_push_tokens_device ON push_tokens(device_id);
CREATE INDEX idx_push_tokens_active ON push_tokens(is_active) WHERE is_active = TRUE;

-- Push notification history
CREATE TABLE IF NOT EXISTS push_notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    token_id UUID REFERENCES push_tokens(id) ON DELETE SET NULL,
    notification_type VARCHAR(50) NOT NULL, -- 'wake', 'command', 'alert', 'message'
    payload JSONB,
    priority VARCHAR(20) DEFAULT 'normal', -- 'normal', 'high'
    sent_at TIMESTAMPTZ DEFAULT NOW(),
    delivered_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    error_message TEXT,
    apns_id VARCHAR(100), -- APNs response ID
    fcm_message_id VARCHAR(200) -- FCM message ID
);

CREATE INDEX idx_push_notifications_device ON push_notifications(device_id);
CREATE INDEX idx_push_notifications_sent ON push_notifications(sent_at DESC);

-- =============================================================================
-- MOBILE METRICS
-- =============================================================================

-- Mobile-specific metrics (separate from device_metrics for mobile-specific data)
CREATE TABLE IF NOT EXISTS mobile_metrics (
    id BIGSERIAL,
    device_id UUID NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Battery
    battery_level INTEGER, -- 0-100
    battery_state VARCHAR(20), -- 'charging', 'discharging', 'full', 'not_charging'
    battery_health INTEGER, -- 0-100

    -- Storage
    storage_total_bytes BIGINT,
    storage_available_bytes BIGINT,
    storage_system_bytes BIGINT,
    storage_apps_bytes BIGINT,

    -- Memory
    memory_total_bytes BIGINT,
    memory_available_bytes BIGINT,

    -- Network
    wifi_connected BOOLEAN,
    wifi_ssid VARCHAR(100),
    wifi_bssid VARCHAR(20),
    cellular_connected BOOLEAN,
    cellular_type VARCHAR(20), -- '4G', '5G', 'LTE', '3G'
    cellular_signal_strength INTEGER, -- dBm
    data_roaming_enabled BOOLEAN,

    -- Location (if permitted)
    latitude DECIMAL(10, 8),
    longitude DECIMAL(11, 8),
    location_accuracy DECIMAL(10, 2),
    altitude DECIMAL(10, 2),
    speed DECIMAL(10, 2), -- m/s

    PRIMARY KEY (timestamp, device_id)
) PARTITION BY RANGE (timestamp);

-- Create initial partitions
DO $$
DECLARE
    start_date DATE;
    end_date DATE;
    partition_name TEXT;
BEGIN
    FOR i IN 0..3 LOOP
        start_date := date_trunc('month', CURRENT_DATE + (i || ' months')::interval);
        end_date := start_date + '1 month'::interval;
        partition_name := 'mobile_metrics_' || to_char(start_date, 'YYYY_MM');

        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF mobile_metrics
             FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
    END LOOP;
END $$;

CREATE INDEX idx_mobile_metrics_device_time ON mobile_metrics(device_id, timestamp DESC);
CREATE INDEX idx_mobile_metrics_brin ON mobile_metrics USING BRIN (timestamp) WITH (pages_per_range = 128);

-- =============================================================================
-- MOBILE APPS (Android primarily)
-- =============================================================================

CREATE TABLE IF NOT EXISTS mobile_apps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    package_name VARCHAR(255) NOT NULL, -- e.g., "com.example.app"
    app_name VARCHAR(255),
    version_name VARCHAR(50),
    version_code BIGINT,
    install_source VARCHAR(100), -- 'play_store', 'app_store', 'sideload', 'enterprise'
    installer_package VARCHAR(255), -- Which app installed it

    -- App attributes
    is_system_app BOOLEAN DEFAULT FALSE,
    is_enabled BOOLEAN DEFAULT TRUE,
    is_work_profile BOOLEAN DEFAULT FALSE,

    -- App size
    app_size_bytes BIGINT,
    data_size_bytes BIGINT,
    cache_size_bytes BIGINT,

    -- Permissions (Android)
    requested_permissions TEXT[],
    granted_permissions TEXT[],

    -- Signing info
    signing_certificate_hash VARCHAR(100),
    is_debuggable BOOLEAN DEFAULT FALSE,

    first_installed_at TIMESTAMPTZ,
    last_updated_at TIMESTAMPTZ,
    first_seen_at TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ DEFAULT NOW(),
    removed_at TIMESTAMPTZ,

    UNIQUE(device_id, package_name)
);

CREATE INDEX idx_mobile_apps_device ON mobile_apps(device_id);
CREATE INDEX idx_mobile_apps_package ON mobile_apps(package_name);
CREATE INDEX idx_mobile_apps_install_source ON mobile_apps(install_source);

-- =============================================================================
-- COMPLIANCE POLICIES
-- =============================================================================

-- Mobile compliance policy definitions
CREATE TABLE IF NOT EXISTS mobile_compliance_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    platform VARCHAR(20), -- 'ios', 'android', 'all'
    is_active BOOLEAN DEFAULT TRUE,

    -- Policy rules (JSONB for flexibility)
    rules JSONB NOT NULL,
    /*
    Example rules:
    {
        "passcode_required": true,
        "passcode_min_length": 6,
        "passcode_alphanumeric": false,
        "encryption_required": true,
        "jailbreak_blocked": true,
        "root_blocked": true,
        "min_os_version": "15.0",
        "blocked_apps": ["com.example.blocked"],
        "required_apps": ["com.example.required"],
        "max_days_inactive": 30,
        "location_services_required": false
    }
    */

    -- Actions on non-compliance
    non_compliance_actions JSONB,
    /*
    {
        "notify_user": true,
        "notify_admin": true,
        "block_access": false,
        "remote_wipe_after_days": null
    }
    */

    created_by UUID,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_compliance_policies_platform ON mobile_compliance_policies(platform);
CREATE INDEX idx_compliance_policies_active ON mobile_compliance_policies(is_active);

-- Device compliance status
CREATE TABLE IF NOT EXISTS mobile_compliance_status (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    policy_id UUID NOT NULL REFERENCES mobile_compliance_policies(id) ON DELETE CASCADE,

    is_compliant BOOLEAN NOT NULL,
    compliance_score INTEGER, -- 0-100

    -- Detailed violation info
    violations JSONB,
    /*
    [
        {"rule": "passcode_required", "expected": true, "actual": false},
        {"rule": "min_os_version", "expected": "15.0", "actual": "14.8"}
    ]
    */

    last_evaluated_at TIMESTAMPTZ DEFAULT NOW(),
    became_non_compliant_at TIMESTAMPTZ,
    remediation_deadline TIMESTAMPTZ,

    UNIQUE(device_id, policy_id)
);

CREATE INDEX idx_compliance_status_device ON mobile_compliance_status(device_id);
CREATE INDEX idx_compliance_status_compliant ON mobile_compliance_status(is_compliant);
CREATE INDEX idx_compliance_status_deadline ON mobile_compliance_status(remediation_deadline)
    WHERE remediation_deadline IS NOT NULL;

-- =============================================================================
-- MOBILE COMMANDS
-- =============================================================================

-- Mobile-specific commands and their status
CREATE TABLE IF NOT EXISTS mobile_commands (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    command_type VARCHAR(50) NOT NULL,
    -- Types: 'lock', 'unlock', 'wipe', 'reset_passcode', 'locate',
    --        'clear_passcode', 'restart', 'shutdown', 'ring',
    --        'install_app', 'remove_app', 'install_profile', 'remove_profile'

    command_data JSONB, -- Command-specific parameters

    status VARCHAR(20) DEFAULT 'pending',
    -- Status: 'pending', 'queued', 'sent', 'acknowledged', 'completed', 'failed', 'expired'

    created_by UUID,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    queued_at TIMESTAMPTZ,
    sent_at TIMESTAMPTZ,
    acknowledged_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,

    result JSONB,
    error_message TEXT,

    -- Expiry for time-sensitive commands
    expires_at TIMESTAMPTZ,

    -- For tracking via push notification
    push_notification_id UUID REFERENCES push_notifications(id)
);

CREATE INDEX idx_mobile_commands_device ON mobile_commands(device_id);
CREATE INDEX idx_mobile_commands_status ON mobile_commands(status);
CREATE INDEX idx_mobile_commands_pending ON mobile_commands(device_id, status) WHERE status = 'pending';

-- =============================================================================
-- GEOFENCING
-- =============================================================================

-- Define geofence regions
CREATE TABLE IF NOT EXISTS geofences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT,

    -- Circle geofence
    center_latitude DECIMAL(10, 8),
    center_longitude DECIMAL(11, 8),
    radius_meters DECIMAL(10, 2),

    -- Polygon geofence (alternative to circle)
    polygon_coordinates JSONB, -- Array of [lat, lng] points

    is_active BOOLEAN DEFAULT TRUE,

    -- Actions when device enters/exits
    on_enter_action JSONB,
    on_exit_action JSONB,
    /*
    {
        "notify_admin": true,
        "log_event": true,
        "trigger_command": "lock"
    }
    */

    created_by UUID,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_geofences_active ON geofences(is_active);
CREATE INDEX idx_geofences_location ON geofences(center_latitude, center_longitude);

-- Device geofence assignments
CREATE TABLE IF NOT EXISTS device_geofences (
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    geofence_id UUID NOT NULL REFERENCES geofences(id) ON DELETE CASCADE,
    is_inside BOOLEAN,
    last_transition VARCHAR(20), -- 'enter', 'exit'
    last_transition_at TIMESTAMPTZ,
    PRIMARY KEY (device_id, geofence_id)
);

-- Geofence events
CREATE TABLE IF NOT EXISTS geofence_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    geofence_id UUID NOT NULL REFERENCES geofences(id) ON DELETE CASCADE,
    event_type VARCHAR(20) NOT NULL, -- 'enter', 'exit', 'dwell'
    latitude DECIMAL(10, 8),
    longitude DECIMAL(11, 8),
    accuracy DECIMAL(10, 2),
    recorded_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_geofence_events_device ON geofence_events(device_id);
CREATE INDEX idx_geofence_events_recorded ON geofence_events(recorded_at DESC);

-- =============================================================================
-- FUNCTIONS
-- =============================================================================

-- Function to evaluate device compliance
CREATE OR REPLACE FUNCTION evaluate_mobile_compliance(p_device_id UUID)
RETURNS TABLE (
    policy_id UUID,
    policy_name VARCHAR(255),
    is_compliant BOOLEAN,
    violations JSONB
) AS $$
DECLARE
    mobile mobile_devices%ROWTYPE;
    policy mobile_compliance_policies%ROWTYPE;
    rules JSONB;
    v_violations JSONB;
    v_compliant BOOLEAN;
BEGIN
    -- Get mobile device info
    SELECT * INTO mobile FROM mobile_devices WHERE device_id = p_device_id;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- Evaluate each active policy
    FOR policy IN
        SELECT * FROM mobile_compliance_policies
        WHERE is_active = TRUE
        AND (platform = 'all' OR platform = (
            SELECT platform FROM devices WHERE id = p_device_id
        ))
    LOOP
        rules := policy.rules;
        v_violations := '[]'::jsonb;
        v_compliant := TRUE;

        -- Check passcode
        IF (rules->>'passcode_required')::boolean = TRUE AND NOT COALESCE(mobile.passcode_present, FALSE) THEN
            v_violations := v_violations || jsonb_build_object('rule', 'passcode_required', 'expected', true, 'actual', false);
            v_compliant := FALSE;
        END IF;

        -- Check encryption
        IF (rules->>'encryption_required')::boolean = TRUE AND NOT COALESCE(mobile.encryption_enabled, FALSE) THEN
            v_violations := v_violations || jsonb_build_object('rule', 'encryption_required', 'expected', true, 'actual', false);
            v_compliant := FALSE;
        END IF;

        -- Check jailbreak
        IF (rules->>'jailbreak_blocked')::boolean = TRUE AND COALESCE(mobile.is_jailbroken, FALSE) THEN
            v_violations := v_violations || jsonb_build_object('rule', 'jailbreak_blocked', 'expected', false, 'actual', true);
            v_compliant := FALSE;
        END IF;

        -- Check root
        IF (rules->>'root_blocked')::boolean = TRUE AND COALESCE(mobile.is_rooted, FALSE) THEN
            v_violations := v_violations || jsonb_build_object('rule', 'root_blocked', 'expected', false, 'actual', true);
            v_compliant := FALSE;
        END IF;

        -- Update compliance status
        INSERT INTO mobile_compliance_status (device_id, policy_id, is_compliant, violations, last_evaluated_at)
        VALUES (p_device_id, policy.id, v_compliant, v_violations, NOW())
        ON CONFLICT (device_id, policy_id) DO UPDATE SET
            is_compliant = EXCLUDED.is_compliant,
            violations = EXCLUDED.violations,
            last_evaluated_at = EXCLUDED.last_evaluated_at,
            became_non_compliant_at = CASE
                WHEN mobile_compliance_status.is_compliant AND NOT EXCLUDED.is_compliant THEN NOW()
                WHEN NOT mobile_compliance_status.is_compliant AND EXCLUDED.is_compliant THEN NULL
                ELSE mobile_compliance_status.became_non_compliant_at
            END;

        RETURN QUERY SELECT policy.id, policy.name, v_compliant, v_violations;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- Function to create mobile metrics partition
CREATE OR REPLACE FUNCTION create_mobile_metrics_partition()
RETURNS void AS $$
DECLARE
    next_month DATE;
    partition_name TEXT;
    start_date DATE;
    end_date DATE;
BEGIN
    next_month := date_trunc('month', CURRENT_DATE + '2 months'::interval);
    partition_name := 'mobile_metrics_' || to_char(next_month, 'YYYY_MM');
    start_date := next_month;
    end_date := next_month + '1 month'::interval;

    IF NOT EXISTS (
        SELECT 1 FROM pg_tables
        WHERE tablename = partition_name
    ) THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF mobile_metrics
             FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
        RAISE NOTICE 'Created mobile metrics partition: %', partition_name;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- =============================================================================
-- DEFAULT DATA
-- =============================================================================

-- Insert default compliance policy
INSERT INTO mobile_compliance_policies (name, description, platform, rules, non_compliance_actions)
VALUES (
    'Basic Security Policy',
    'Default security policy for all mobile devices',
    'all',
    '{
        "passcode_required": true,
        "encryption_required": true,
        "jailbreak_blocked": true,
        "root_blocked": true
    }'::jsonb,
    '{
        "notify_user": true,
        "notify_admin": true,
        "block_access": false
    }'::jsonb
) ON CONFLICT DO NOTHING;

-- Add mobile retention policy
INSERT INTO retention_policies (table_name, retention_days) VALUES
    ('mobile_metrics', 30),
    ('mobile_commands', 90),
    ('push_notifications', 30),
    ('geofence_events', 90)
ON CONFLICT (table_name) DO NOTHING;
