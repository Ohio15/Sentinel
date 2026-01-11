-- Migration: Add logo sizing fields to clients table
-- These allow portal administrators to configure how the client logo appears

ALTER TABLE clients ADD COLUMN IF NOT EXISTS logo_width INTEGER DEFAULT 32;
ALTER TABLE clients ADD COLUMN IF NOT EXISTS logo_height INTEGER DEFAULT 32;
