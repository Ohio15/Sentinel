-- Migration 015: Portal Branding and Device Name
-- Adds device_name field to tickets for troubleshooting

-- Add device_name field to tickets table
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS device_name VARCHAR(255);

-- Note: clients table already has 'color' and 'logo_url' columns from migration 005
-- We'll use those existing fields for branding
