-- Sentinel RMM PostgreSQL Schema
-- Migration: 009_extended_metrics
-- Add extended metrics columns for comprehensive device monitoring

-- Uptime tracking
ALTER TABLE device_metrics ADD COLUMN IF NOT EXISTS uptime_seconds BIGINT DEFAULT 0;

-- Disk I/O metrics (bytes per second)
ALTER TABLE device_metrics ADD COLUMN IF NOT EXISTS disk_read_bytes_sec BIGINT DEFAULT 0;
ALTER TABLE device_metrics ADD COLUMN IF NOT EXISTS disk_write_bytes_sec BIGINT DEFAULT 0;

-- Extended memory metrics
ALTER TABLE device_metrics ADD COLUMN IF NOT EXISTS memory_committed BIGINT DEFAULT 0;
ALTER TABLE device_metrics ADD COLUMN IF NOT EXISTS memory_cached BIGINT DEFAULT 0;
ALTER TABLE device_metrics ADD COLUMN IF NOT EXISTS memory_paged_pool BIGINT DEFAULT 0;
ALTER TABLE device_metrics ADD COLUMN IF NOT EXISTS memory_non_paged_pool BIGINT DEFAULT 0;

-- GPU metrics (JSONB array to support multiple GPUs)
-- Structure: [{"name": "GPU Name", "utilization": 45.5, "memoryUsed": bytes, "memoryTotal": bytes, "temperature": celsius, "powerDraw": watts}]
ALTER TABLE device_metrics ADD COLUMN IF NOT EXISTS gpu_metrics JSONB DEFAULT '[]'::jsonb;

-- Network per-interface metrics (JSONB array)
-- Structure: [{"name": "Ethernet", "rxBytesPerSec": bytes, "txBytesPerSec": bytes, "rxPackets": count, "txPackets": count, "errorsIn": count, "errorsOut": count}]
ALTER TABLE device_metrics ADD COLUMN IF NOT EXISTS network_interfaces JSONB DEFAULT '[]'::jsonb;
