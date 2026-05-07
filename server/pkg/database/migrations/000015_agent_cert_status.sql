-- Sentinel RMM PostgreSQL Schema
-- Migration: 006_agent_cert_status
-- Track certificate distribution to agents

-- Agent certificate status table
CREATE TABLE IF NOT EXISTS agent_cert_status (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    agent_id VARCHAR(255) NOT NULL UNIQUE,
    ca_cert_hash VARCHAR(64),  -- SHA256 hash of the CA cert the agent has
    distributed_at TIMESTAMP WITH TIME ZONE,  -- When we sent the cert
    confirmed_at TIMESTAMP WITH TIME ZONE,    -- When agent confirmed receipt
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_agent_cert_status_agent_id ON agent_cert_status(agent_id);
CREATE INDEX IF NOT EXISTS idx_agent_cert_status_ca_cert_hash ON agent_cert_status(ca_cert_hash);

-- Trigger for updated_at
DROP TRIGGER IF EXISTS update_agent_cert_status_updated_at ON agent_cert_status;
CREATE TRIGGER update_agent_cert_status_updated_at
    BEFORE UPDATE ON agent_cert_status
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
