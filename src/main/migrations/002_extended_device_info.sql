-- Sentinel RMM PostgreSQL Schema
-- Migration: 002_extended_device_info
-- Add extended device information columns

-- Add new columns to devices table
ALTER TABLE devices ADD COLUMN IF NOT EXISTS platform VARCHAR(100);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS platform_family VARCHAR(100);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS cpu_model VARCHAR(255);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS cpu_cores INTEGER DEFAULT 0;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS cpu_threads INTEGER DEFAULT 0;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS cpu_speed REAL DEFAULT 0;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS total_memory BIGINT DEFAULT 0;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS boot_time BIGINT DEFAULT 0;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS gpu JSONB DEFAULT '[]'::jsonb;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS storage JSONB DEFAULT '[]'::jsonb;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS serial_number VARCHAR(255);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS manufacturer VARCHAR(255);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS model VARCHAR(255);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS domain VARCHAR(255);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS public_ip VARCHAR(45);

-- Add memory_total_bytes to device_metrics if not exists
ALTER TABLE device_metrics ADD COLUMN IF NOT EXISTS memory_total_bytes BIGINT DEFAULT 0;
ALTER TABLE device_metrics ADD COLUMN IF NOT EXISTS disk_total_bytes BIGINT DEFAULT 0;
