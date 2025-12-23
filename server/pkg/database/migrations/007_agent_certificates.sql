-- Migration: Agent Certificate Tracking
-- Tracks which CA certificate each agent has installed

-- Step 1: Add ca_cert_hash column to devices table
-- Stores the SHA-256 hash of the CA certificate the agent currently has
ALTER TABLE devices ADD COLUMN IF NOT EXISTS ca_cert_hash VARCHAR(64);

-- Step 2: Add ca_cert_updated_at timestamp
-- Records when the agent last updated its certificate
ALTER TABLE devices ADD COLUMN IF NOT EXISTS ca_cert_updated_at TIMESTAMPTZ;

-- Step 3: Add ca_cert_distributed_at timestamp
-- Records when we last sent a certificate update to this agent
ALTER TABLE devices ADD COLUMN IF NOT EXISTS ca_cert_distributed_at TIMESTAMPTZ;

-- Step 4: Create index for certificate hash lookups (useful for finding outdated agents)
CREATE INDEX IF NOT EXISTS idx_devices_ca_cert_hash ON devices(ca_cert_hash) WHERE ca_cert_hash IS NOT NULL;

-- Step 5: Add comment for documentation
COMMENT ON COLUMN devices.ca_cert_hash IS 'SHA-256 hash of the CA certificate currently installed on the agent';
COMMENT ON COLUMN devices.ca_cert_updated_at IS 'Timestamp when the agent confirmed receiving the certificate';
COMMENT ON COLUMN devices.ca_cert_distributed_at IS 'Timestamp when certificate was last distributed to this agent';
