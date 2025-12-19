-- Migration: Comprehensive inventory schema for industry-standard RMM data collection
-- Supports: Software inventory, security posture, user/access, hardware assets

-- =============================================================================
-- SOFTWARE INVENTORY
-- =============================================================================

-- Installed applications
CREATE TABLE IF NOT EXISTS device_software (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    name VARCHAR(500) NOT NULL,
    version VARCHAR(100),
    publisher VARCHAR(255),
    install_date DATE,
    install_location VARCHAR(1000),
    install_source VARCHAR(50), -- 'registry', 'msi', 'dpkg', 'rpm', 'brew', 'app_store'
    size_bytes BIGINT,
    architecture VARCHAR(20), -- 'x86', 'x64', 'arm64'
    uninstall_string VARCHAR(2000),
    is_system BOOLEAN DEFAULT FALSE,
    is_hidden BOOLEAN DEFAULT FALSE,
    first_seen_at TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ DEFAULT NOW(),
    removed_at TIMESTAMPTZ,
    UNIQUE(device_id, name, version, install_location)
);

CREATE INDEX idx_device_software_device ON device_software(device_id);
CREATE INDEX idx_device_software_name ON device_software(name);
CREATE INDEX idx_device_software_publisher ON device_software(publisher);
CREATE INDEX idx_device_software_last_seen ON device_software(last_seen_at);

-- Windows services / systemd units / launchd services
CREATE TABLE IF NOT EXISTS device_services (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    display_name VARCHAR(500),
    description TEXT,
    service_type VARCHAR(50), -- 'windows_service', 'systemd', 'launchd', 'init'
    start_type VARCHAR(50), -- 'automatic', 'manual', 'disabled', 'automatic_delayed'
    current_state VARCHAR(50), -- 'running', 'stopped', 'paused', 'starting', 'stopping'
    path_to_executable VARCHAR(2000),
    account VARCHAR(255), -- Service account
    pid INTEGER,
    dependencies TEXT[], -- Array of service names
    first_seen_at TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ DEFAULT NOW(),
    state_changed_at TIMESTAMPTZ,
    UNIQUE(device_id, name, service_type)
);

CREATE INDEX idx_device_services_device ON device_services(device_id);
CREATE INDEX idx_device_services_name ON device_services(name);
CREATE INDEX idx_device_services_state ON device_services(current_state);

-- Startup programs
CREATE TABLE IF NOT EXISTS device_startup (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    name VARCHAR(500) NOT NULL,
    command VARCHAR(2000),
    location VARCHAR(500), -- Registry path, startup folder, plist location
    location_type VARCHAR(50), -- 'registry_run', 'registry_runonce', 'startup_folder', 'scheduled_task', 'launchd', 'cron'
    enabled BOOLEAN DEFAULT TRUE,
    user_specific BOOLEAN DEFAULT FALSE,
    username VARCHAR(255),
    publisher VARCHAR(255),
    first_seen_at TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(device_id, name, location)
);

CREATE INDEX idx_device_startup_device ON device_startup(device_id);
CREATE INDEX idx_device_startup_enabled ON device_startup(enabled);

-- Browser extensions
CREATE TABLE IF NOT EXISTS device_extensions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    browser VARCHAR(50) NOT NULL, -- 'chrome', 'firefox', 'edge', 'safari', 'brave'
    extension_id VARCHAR(255) NOT NULL,
    name VARCHAR(500),
    version VARCHAR(50),
    description TEXT,
    enabled BOOLEAN DEFAULT TRUE,
    user_profile VARCHAR(255),
    permissions TEXT[],
    homepage_url VARCHAR(1000),
    install_type VARCHAR(50), -- 'normal', 'admin', 'development', 'sideload'
    first_seen_at TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(device_id, browser, extension_id, user_profile)
);

CREATE INDEX idx_device_extensions_device ON device_extensions(device_id);
CREATE INDEX idx_device_extensions_browser ON device_extensions(browser);

-- Drivers
CREATE TABLE IF NOT EXISTS device_drivers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    name VARCHAR(500) NOT NULL,
    display_name VARCHAR(500),
    description TEXT,
    driver_type VARCHAR(50), -- 'kernel', 'file_system', 'network', 'display', 'audio', 'usb'
    version VARCHAR(100),
    provider VARCHAR(255),
    date_signed DATE,
    signer VARCHAR(255),
    inf_name VARCHAR(255),
    hardware_id VARCHAR(500),
    is_signed BOOLEAN,
    is_microsoft BOOLEAN DEFAULT FALSE,
    first_seen_at TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(device_id, name, hardware_id)
);

CREATE INDEX idx_device_drivers_device ON device_drivers(device_id);
CREATE INDEX idx_device_drivers_signed ON device_drivers(is_signed);

-- =============================================================================
-- SECURITY POSTURE
-- =============================================================================

-- Comprehensive security status (JSONB for flexibility)
CREATE TABLE IF NOT EXISTS device_security (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Antivirus/EDR status
    antivirus_product VARCHAR(255),
    antivirus_version VARCHAR(100),
    antivirus_enabled BOOLEAN,
    antivirus_up_to_date BOOLEAN,
    antivirus_last_scan TIMESTAMPTZ,
    antivirus_realtime_enabled BOOLEAN,

    -- Firewall status
    firewall_enabled BOOLEAN,
    firewall_profiles JSONB, -- {domain: true, private: true, public: true}

    -- Encryption status
    disk_encryption_enabled BOOLEAN,
    disk_encryption_type VARCHAR(50), -- 'bitlocker', 'filevault', 'luks', 'veracrypt'
    disk_encryption_percent INTEGER,
    tpm_present BOOLEAN,
    tpm_version VARCHAR(20),
    tpm_enabled BOOLEAN,
    secure_boot_enabled BOOLEAN,

    -- OS Security
    uac_enabled BOOLEAN,
    uac_level INTEGER,
    secure_attention_required BOOLEAN,
    screen_lock_enabled BOOLEAN,
    screen_lock_timeout INTEGER, -- seconds
    password_policy JSONB, -- {min_length, complexity, expiry_days}

    -- Additional security indicators
    remote_desktop_enabled BOOLEAN,
    guest_account_enabled BOOLEAN,
    auto_login_enabled BOOLEAN,
    developer_mode_enabled BOOLEAN,

    -- Compliance score (calculated)
    security_score INTEGER, -- 0-100
    risk_factors TEXT[],

    UNIQUE(device_id, collected_at)
);

CREATE INDEX idx_device_security_device ON device_security(device_id);
CREATE INDEX idx_device_security_collected ON device_security(collected_at DESC);
CREATE INDEX idx_device_security_score ON device_security(security_score);

-- Windows Update / Patch status
CREATE TABLE IF NOT EXISTS device_pending_updates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    update_id VARCHAR(100) NOT NULL,
    title VARCHAR(1000),
    description TEXT,
    kb_article VARCHAR(50),
    severity VARCHAR(50), -- 'critical', 'important', 'moderate', 'low', 'unspecified'
    category VARCHAR(100), -- 'security', 'critical_update', 'feature_pack', 'driver', 'definition'
    size_bytes BIGINT,
    is_downloaded BOOLEAN DEFAULT FALSE,
    is_mandatory BOOLEAN DEFAULT FALSE,
    release_date DATE,
    deadline DATE,
    reboot_required BOOLEAN DEFAULT FALSE,
    first_seen_at TIMESTAMPTZ DEFAULT NOW(),
    installed_at TIMESTAMPTZ,
    UNIQUE(device_id, update_id)
);

CREATE INDEX idx_device_updates_device ON device_pending_updates(device_id);
CREATE INDEX idx_device_updates_severity ON device_pending_updates(severity);
CREATE INDEX idx_device_updates_installed ON device_pending_updates(installed_at);

-- Vulnerability tracking
CREATE TABLE IF NOT EXISTS device_vulnerabilities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    cve_id VARCHAR(50),
    title VARCHAR(500),
    description TEXT,
    severity VARCHAR(20), -- 'critical', 'high', 'medium', 'low'
    cvss_score DECIMAL(3,1),
    affected_product VARCHAR(500),
    affected_version VARCHAR(100),
    remediation TEXT,
    patch_available BOOLEAN,
    patch_kb VARCHAR(50),
    detected_at TIMESTAMPTZ DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    source VARCHAR(50), -- 'windows_update', 'scan', 'manual'
    UNIQUE(device_id, cve_id, affected_product)
);

CREATE INDEX idx_device_vulns_device ON device_vulnerabilities(device_id);
CREATE INDEX idx_device_vulns_cve ON device_vulnerabilities(cve_id);
CREATE INDEX idx_device_vulns_severity ON device_vulnerabilities(severity);
CREATE INDEX idx_device_vulns_unresolved ON device_vulnerabilities(device_id) WHERE resolved_at IS NULL;

-- =============================================================================
-- USERS & ACCESS
-- =============================================================================

-- Local user accounts
CREATE TABLE IF NOT EXISTS device_users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    username VARCHAR(255) NOT NULL,
    full_name VARCHAR(500),
    sid VARCHAR(255), -- Windows SID
    uid INTEGER, -- Unix UID
    user_type VARCHAR(50), -- 'local', 'domain', 'microsoft_account', 'azure_ad'
    is_admin BOOLEAN DEFAULT FALSE,
    is_disabled BOOLEAN DEFAULT FALSE,
    is_locked BOOLEAN DEFAULT FALSE,
    password_required BOOLEAN DEFAULT TRUE,
    password_changeable BOOLEAN DEFAULT TRUE,
    password_expires BOOLEAN DEFAULT TRUE,
    password_last_set TIMESTAMPTZ,
    password_expiry TIMESTAMPTZ,
    last_logon TIMESTAMPTZ,
    logon_count INTEGER,
    home_directory VARCHAR(1000),
    profile_path VARCHAR(1000),
    description TEXT,
    first_seen_at TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(device_id, username, sid)
);

CREATE INDEX idx_device_users_device ON device_users(device_id);
CREATE INDEX idx_device_users_admin ON device_users(is_admin);
CREATE INDEX idx_device_users_type ON device_users(user_type);

-- Local groups
CREATE TABLE IF NOT EXISTS device_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    group_name VARCHAR(255) NOT NULL,
    sid VARCHAR(255),
    gid INTEGER,
    description TEXT,
    is_builtin BOOLEAN DEFAULT FALSE,
    members TEXT[], -- Array of usernames/SIDs
    first_seen_at TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(device_id, group_name, sid)
);

CREATE INDEX idx_device_groups_device ON device_groups(device_id);

-- Active login sessions
CREATE TABLE IF NOT EXISTS device_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    session_id VARCHAR(100),
    username VARCHAR(255) NOT NULL,
    domain VARCHAR(255),
    session_type VARCHAR(50), -- 'console', 'rdp', 'network', 'service', 'batch'
    state VARCHAR(50), -- 'active', 'disconnected', 'idle'
    client_name VARCHAR(255),
    client_address VARCHAR(100),
    logon_time TIMESTAMPTZ,
    disconnect_time TIMESTAMPTZ,
    idle_time INTEGER, -- seconds
    is_elevated BOOLEAN,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_device_sessions_device ON device_sessions(device_id);
CREATE INDEX idx_device_sessions_user ON device_sessions(username);
CREATE INDEX idx_device_sessions_active ON device_sessions(device_id, state) WHERE state = 'active';

-- =============================================================================
-- HARDWARE & ASSETS
-- =============================================================================

-- USB devices (current and historical)
CREATE TABLE IF NOT EXISTS device_usb (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    device_instance_id VARCHAR(500),
    vendor_id VARCHAR(10),
    product_id VARCHAR(10),
    serial_number VARCHAR(255),
    device_class VARCHAR(100), -- 'storage', 'hid', 'audio', 'video', 'network', 'printer'
    friendly_name VARCHAR(500),
    manufacturer VARCHAR(255),
    description TEXT,
    first_connected_at TIMESTAMPTZ DEFAULT NOW(),
    last_connected_at TIMESTAMPTZ DEFAULT NOW(),
    is_currently_connected BOOLEAN DEFAULT TRUE,
    is_blocked BOOLEAN DEFAULT FALSE,
    UNIQUE(device_id, device_instance_id)
);

CREATE INDEX idx_device_usb_device ON device_usb(device_id);
CREATE INDEX idx_device_usb_connected ON device_usb(is_currently_connected);
CREATE INDEX idx_device_usb_class ON device_usb(device_class);

-- Monitors/Displays
CREATE TABLE IF NOT EXISTS device_monitors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    monitor_id VARCHAR(255),
    name VARCHAR(255),
    manufacturer VARCHAR(255),
    model VARCHAR(255),
    serial_number VARCHAR(100),
    manufacture_year INTEGER,
    manufacture_week INTEGER,
    resolution_width INTEGER,
    resolution_height INTEGER,
    physical_width_mm INTEGER,
    physical_height_mm INTEGER,
    refresh_rate INTEGER,
    bits_per_pixel INTEGER,
    is_primary BOOLEAN DEFAULT FALSE,
    connection_type VARCHAR(50), -- 'hdmi', 'displayport', 'vga', 'dvi', 'usbc', 'internal'
    first_seen_at TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(device_id, monitor_id)
);

CREATE INDEX idx_device_monitors_device ON device_monitors(device_id);

-- Printers
CREATE TABLE IF NOT EXISTS device_printers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    name VARCHAR(500) NOT NULL,
    driver_name VARCHAR(500),
    port_name VARCHAR(255),
    share_name VARCHAR(255),
    printer_type VARCHAR(50), -- 'local', 'network', 'virtual', 'shared'
    is_default BOOLEAN DEFAULT FALSE,
    is_network BOOLEAN DEFAULT FALSE,
    is_shared BOOLEAN DEFAULT FALSE,
    status VARCHAR(100),
    location VARCHAR(500),
    comment TEXT,
    server_name VARCHAR(255),
    first_seen_at TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(device_id, name, port_name)
);

CREATE INDEX idx_device_printers_device ON device_printers(device_id);

-- BIOS/UEFI information
CREATE TABLE IF NOT EXISTS device_bios (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    manufacturer VARCHAR(255),
    version VARCHAR(100),
    release_date DATE,
    serial_number VARCHAR(255),
    smbios_version VARCHAR(50),
    is_uefi BOOLEAN,
    secure_boot_capable BOOLEAN,
    secure_boot_enabled BOOLEAN,
    virtualization_enabled BOOLEAN,
    tpm_present BOOLEAN,
    firmware_type VARCHAR(50), -- 'uefi', 'bios', 'unknown'
    collected_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(device_id)
);

CREATE INDEX idx_device_bios_device ON device_bios(device_id);

-- Battery status (laptops/mobile)
CREATE TABLE IF NOT EXISTS device_battery (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    battery_id VARCHAR(100),
    name VARCHAR(255),
    manufacturer VARCHAR(255),
    chemistry VARCHAR(50), -- 'LiIon', 'LiPo', 'NiMH', 'NiCd'
    design_capacity_mwh INTEGER,
    full_charge_capacity_mwh INTEGER,
    current_capacity_mwh INTEGER,
    voltage_mv INTEGER,
    charge_rate_mw INTEGER,
    discharge_rate_mw INTEGER,
    cycle_count INTEGER,
    health_percent INTEGER, -- full_charge / design_capacity * 100
    status VARCHAR(50), -- 'charging', 'discharging', 'full', 'not_charging'
    time_to_empty_minutes INTEGER,
    time_to_full_minutes INTEGER,
    collected_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_device_battery_device ON device_battery(device_id);
CREATE INDEX idx_device_battery_collected ON device_battery(collected_at DESC);

-- =============================================================================
-- INVENTORY SNAPSHOTS & HISTORY
-- =============================================================================

-- Full inventory snapshots for historical comparison
CREATE TABLE IF NOT EXISTS inventory_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    snapshot_type VARCHAR(50) NOT NULL, -- 'full', 'software', 'security', 'hardware'
    snapshot_data JSONB NOT NULL,
    software_count INTEGER,
    service_count INTEGER,
    user_count INTEGER,
    security_score INTEGER,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_inventory_snapshots_device ON inventory_snapshots(device_id);
CREATE INDEX idx_inventory_snapshots_type ON inventory_snapshots(snapshot_type);
CREATE INDEX idx_inventory_snapshots_created ON inventory_snapshots(created_at DESC);

-- Change tracking for audit
CREATE TABLE IF NOT EXISTS inventory_changes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    change_type VARCHAR(50) NOT NULL, -- 'software_added', 'software_removed', 'service_changed', 'user_added', etc.
    entity_type VARCHAR(50) NOT NULL, -- 'software', 'service', 'user', 'security', etc.
    entity_name VARCHAR(500),
    old_value JSONB,
    new_value JSONB,
    detected_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_inventory_changes_device ON inventory_changes(device_id);
CREATE INDEX idx_inventory_changes_type ON inventory_changes(change_type);
CREATE INDEX idx_inventory_changes_detected ON inventory_changes(detected_at DESC);

-- =============================================================================
-- FLEET-WIDE VIEWS
-- =============================================================================

-- Software across all devices (for license management, vulnerability scanning)
CREATE OR REPLACE VIEW fleet_software AS
SELECT
    ds.name,
    ds.version,
    ds.publisher,
    COUNT(DISTINCT ds.device_id) as device_count,
    array_agg(DISTINCT d.hostname) as hostnames
FROM device_software ds
JOIN devices d ON ds.device_id = d.id
WHERE ds.removed_at IS NULL
GROUP BY ds.name, ds.version, ds.publisher;

-- Security posture summary
CREATE OR REPLACE VIEW fleet_security_summary AS
SELECT
    d.id as device_id,
    d.hostname,
    sec.antivirus_enabled,
    sec.antivirus_up_to_date,
    sec.firewall_enabled,
    sec.disk_encryption_enabled,
    sec.security_score,
    (SELECT COUNT(*) FROM device_pending_updates pu WHERE pu.device_id = d.id AND pu.severity = 'critical' AND pu.installed_at IS NULL) as critical_updates_pending,
    (SELECT COUNT(*) FROM device_vulnerabilities v WHERE v.device_id = d.id AND v.resolved_at IS NULL) as open_vulnerabilities
FROM devices d
LEFT JOIN LATERAL (
    SELECT * FROM device_security
    WHERE device_id = d.id
    ORDER BY collected_at DESC
    LIMIT 1
) sec ON true;

-- Admin users across fleet
CREATE OR REPLACE VIEW fleet_admin_users AS
SELECT
    d.id as device_id,
    d.hostname,
    du.username,
    du.full_name,
    du.user_type,
    du.last_logon
FROM device_users du
JOIN devices d ON du.device_id = d.id
WHERE du.is_admin = TRUE AND du.is_disabled = FALSE;

-- =============================================================================
-- FUNCTIONS
-- =============================================================================

-- Function to calculate security score
CREATE OR REPLACE FUNCTION calculate_security_score(p_device_id UUID)
RETURNS INTEGER AS $$
DECLARE
    score INTEGER := 100;
    sec device_security%ROWTYPE;
    pending_critical INTEGER;
    open_vulns INTEGER;
BEGIN
    -- Get latest security status
    SELECT * INTO sec FROM device_security
    WHERE device_id = p_device_id
    ORDER BY collected_at DESC LIMIT 1;

    IF NOT FOUND THEN
        RETURN 0;
    END IF;

    -- Deductions for security issues
    IF NOT COALESCE(sec.antivirus_enabled, FALSE) THEN score := score - 20; END IF;
    IF NOT COALESCE(sec.antivirus_up_to_date, FALSE) THEN score := score - 10; END IF;
    IF NOT COALESCE(sec.firewall_enabled, FALSE) THEN score := score - 15; END IF;
    IF NOT COALESCE(sec.disk_encryption_enabled, FALSE) THEN score := score - 15; END IF;
    IF NOT COALESCE(sec.secure_boot_enabled, FALSE) THEN score := score - 5; END IF;
    IF COALESCE(sec.guest_account_enabled, FALSE) THEN score := score - 5; END IF;
    IF COALESCE(sec.auto_login_enabled, FALSE) THEN score := score - 10; END IF;
    IF NOT COALESCE(sec.screen_lock_enabled, FALSE) THEN score := score - 5; END IF;

    -- Check for pending critical updates
    SELECT COUNT(*) INTO pending_critical
    FROM device_pending_updates
    WHERE device_id = p_device_id AND severity = 'critical' AND installed_at IS NULL;
    score := score - (pending_critical * 3);

    -- Check for open vulnerabilities
    SELECT COUNT(*) INTO open_vulns
    FROM device_vulnerabilities
    WHERE device_id = p_device_id AND resolved_at IS NULL;
    score := score - (open_vulns * 2);

    RETURN GREATEST(0, score);
END;
$$ LANGUAGE plpgsql;

-- Function to record inventory change
CREATE OR REPLACE FUNCTION record_inventory_change(
    p_device_id UUID,
    p_change_type VARCHAR(50),
    p_entity_type VARCHAR(50),
    p_entity_name VARCHAR(500),
    p_old_value JSONB DEFAULT NULL,
    p_new_value JSONB DEFAULT NULL
) RETURNS UUID AS $$
DECLARE
    change_id UUID;
BEGIN
    INSERT INTO inventory_changes (device_id, change_type, entity_type, entity_name, old_value, new_value)
    VALUES (p_device_id, p_change_type, p_entity_type, p_entity_name, p_old_value, p_new_value)
    RETURNING id INTO change_id;

    RETURN change_id;
END;
$$ LANGUAGE plpgsql;

-- =============================================================================
-- TRIGGERS
-- =============================================================================

-- Auto-record software changes
CREATE OR REPLACE FUNCTION trigger_software_change()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        PERFORM record_inventory_change(
            NEW.device_id, 'software_added', 'software', NEW.name,
            NULL, jsonb_build_object('name', NEW.name, 'version', NEW.version, 'publisher', NEW.publisher)
        );
    ELSIF TG_OP = 'UPDATE' AND OLD.removed_at IS NULL AND NEW.removed_at IS NOT NULL THEN
        PERFORM record_inventory_change(
            NEW.device_id, 'software_removed', 'software', NEW.name,
            jsonb_build_object('name', OLD.name, 'version', OLD.version), NULL
        );
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_software_change
    AFTER INSERT OR UPDATE ON device_software
    FOR EACH ROW EXECUTE FUNCTION trigger_software_change();

-- Auto-record service state changes
CREATE OR REPLACE FUNCTION trigger_service_state_change()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.current_state IS DISTINCT FROM NEW.current_state THEN
        NEW.state_changed_at := NOW();
        PERFORM record_inventory_change(
            NEW.device_id, 'service_state_changed', 'service', NEW.name,
            jsonb_build_object('state', OLD.current_state),
            jsonb_build_object('state', NEW.current_state)
        );
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_service_state_change
    BEFORE UPDATE ON device_services
    FOR EACH ROW EXECUTE FUNCTION trigger_service_state_change();
