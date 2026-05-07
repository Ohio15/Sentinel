-- Sentinel RMM PostgreSQL Schema
-- Migration: 003_agent_updates
-- Add agent update tracking

-- Create agent_updates table to track version history
CREATE TABLE IF NOT EXISTS agent_updates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id VARCHAR(255) NOT NULL,
    from_version VARCHAR(50),
    to_version VARCHAR(50) NOT NULL,
    platform VARCHAR(50),
    architecture VARCHAR(50),
    ip_address VARCHAR(45),
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    completed_at TIMESTAMP WITH TIME ZONE
);

-- Create index for faster lookups
CREATE INDEX IF NOT EXISTS idx_agent_updates_agent_id ON agent_updates(agent_id);
CREATE INDEX IF NOT EXISTS idx_agent_updates_created_at ON agent_updates(created_at DESC);

-- Create agent_releases table for version metadata
CREATE TABLE IF NOT EXISTS agent_releases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version VARCHAR(50) NOT NULL UNIQUE,
    release_date TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    changelog TEXT,
    is_required BOOLEAN DEFAULT FALSE,
    platforms TEXT[] DEFAULT '{}',
    min_version VARCHAR(50),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Add previous version tracking to devices
ALTER TABLE devices ADD COLUMN IF NOT EXISTS previous_agent_version VARCHAR(50);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS last_update_check TIMESTAMP WITH TIME ZONE;

-- Insert initial version record
INSERT INTO agent_releases (version, release_date, changelog, platforms)
VALUES
    ('1.0.0', '2024-12-01', '- Initial release', ARRAY['windows', 'linux', 'darwin']),
    ('1.1.0', '2024-12-05', '- Added script execution
- Improved metrics collection', ARRAY['windows', 'linux', 'darwin']),
    ('1.2.0', '2024-12-07', '- Added remote terminal support
- Added file browser functionality', ARRAY['windows', 'linux', 'darwin']),
    ('1.3.0', '2024-12-08', '- Added GPU information collection
- Added detailed storage information
- Added Windows specifications', ARRAY['windows', 'linux', 'darwin']),
    ('1.3.1', '2024-12-08', '- Bug fixes for Windows service management
- Improved memory usage', ARRAY['windows', 'linux', 'darwin']),
    ('1.9.0', '2024-12-09', '- Full system monitoring
- Protection manager
- Remote desktop support
- Auto-update framework', ARRAY['windows', 'linux', 'darwin']),
    ('1.10.0', '2024-12-10', '- Added ticketing system
- Bug fixes', ARRAY['windows', 'linux', 'darwin']),
    ('1.11.0', '2024-12-10', '- Automatic diagnostic collection on ticket creation
- System error logs collection
- Application logs collection
- Active programs tracking', ARRAY['windows', 'linux', 'darwin']),
    ('1.3.2', '2024-12-09', '- Added autonomous update capability
- Extended system information collection
- Improved connection stability', ARRAY['windows', 'linux', 'darwin'])
ON CONFLICT (version) DO NOTHING;
