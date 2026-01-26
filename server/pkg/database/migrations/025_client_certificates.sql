-- Migration: Client Certificate Tracking for mTLS Authentication
-- This enables agents to authenticate using client certificates instead of tokens

-- Step 1: Add client certificate fields to devices table
ALTER TABLE devices ADD COLUMN IF NOT EXISTS client_cert_serial VARCHAR(64);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS client_cert_issued_at TIMESTAMPTZ;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS client_cert_expires_at TIMESTAMPTZ;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS client_cert_fingerprint VARCHAR(64);

-- Step 2: Create client_certificates audit and revocation table
CREATE TABLE IF NOT EXISTS client_certificates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id VARCHAR(255) NOT NULL,
    device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
    serial_number VARCHAR(64) UNIQUE NOT NULL,
    fingerprint VARCHAR(64) NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    revoked_reason VARCHAR(255),
    organization_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Step 3: Create indexes for lookups
CREATE INDEX IF NOT EXISTS idx_client_certificates_agent_id ON client_certificates(agent_id);
CREATE INDEX IF NOT EXISTS idx_client_certificates_device_id ON client_certificates(device_id);
CREATE INDEX IF NOT EXISTS idx_client_certificates_fingerprint ON client_certificates(fingerprint);
CREATE INDEX IF NOT EXISTS idx_client_certificates_expires_at ON client_certificates(expires_at) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_devices_client_cert_serial ON devices(client_cert_serial) WHERE client_cert_serial IS NOT NULL;

-- Step 4: Add comments for documentation
COMMENT ON TABLE client_certificates IS 'Audit log of all client certificates issued for mTLS authentication';
COMMENT ON COLUMN client_certificates.serial_number IS 'Unique serial number of the certificate (hex encoded)';
COMMENT ON COLUMN client_certificates.fingerprint IS 'SHA-256 fingerprint of the certificate DER';
COMMENT ON COLUMN client_certificates.revoked_at IS 'Timestamp when certificate was revoked, NULL if valid';
COMMENT ON COLUMN client_certificates.revoked_reason IS 'Reason for revocation (renewed, compromised, admin_action)';
COMMENT ON COLUMN devices.client_cert_serial IS 'Serial number of currently active client certificate';
COMMENT ON COLUMN devices.client_cert_expires_at IS 'Expiration time of client certificate for renewal tracking';
