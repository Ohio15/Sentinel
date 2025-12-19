-- Migration: Partition device_metrics table by time for improved performance at scale
-- This enables efficient data retention and query optimization for 5000+ devices

-- Step 1: Create partitioned version of device_metrics
CREATE TABLE IF NOT EXISTS device_metrics_partitioned (
    id BIGSERIAL,
    device_id UUID NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cpu_percent REAL,
    memory_percent REAL,
    memory_used_bytes BIGINT,
    memory_total_bytes BIGINT,
    disk_percent REAL,
    disk_used_bytes BIGINT,
    disk_total_bytes BIGINT,
    network_rx_bytes BIGINT,
    network_tx_bytes BIGINT,
    process_count INTEGER,
    PRIMARY KEY (timestamp, device_id)
) PARTITION BY RANGE (timestamp);

-- Step 2: Create partitions for current and next 3 months
DO $$
DECLARE
    start_date DATE;
    end_date DATE;
    partition_name TEXT;
BEGIN
    FOR i IN 0..3 LOOP
        start_date := date_trunc('month', CURRENT_DATE + (i || ' months')::interval);
        end_date := start_date + '1 month'::interval;
        partition_name := 'device_metrics_' || to_char(start_date, 'YYYY_MM');

        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF device_metrics_partitioned
             FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
    END LOOP;
END $$;

-- Step 3: Create indexes on partitioned table
CREATE INDEX IF NOT EXISTS idx_metrics_part_device_time
    ON device_metrics_partitioned(device_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_metrics_part_timestamp
    ON device_metrics_partitioned(timestamp DESC);

-- Step 4: Add BRIN index for efficient time-based queries (very efficient for time-series)
CREATE INDEX IF NOT EXISTS idx_metrics_brin
    ON device_metrics_partitioned USING BRIN (timestamp) WITH (pages_per_range = 128);

-- Step 5: Create function to auto-create partitions
CREATE OR REPLACE FUNCTION create_metrics_partition()
RETURNS void AS $$
DECLARE
    next_month DATE;
    partition_name TEXT;
    start_date DATE;
    end_date DATE;
BEGIN
    -- Create partition for 2 months ahead
    next_month := date_trunc('month', CURRENT_DATE + '2 months'::interval);
    partition_name := 'device_metrics_' || to_char(next_month, 'YYYY_MM');
    start_date := next_month;
    end_date := next_month + '1 month'::interval;

    IF NOT EXISTS (
        SELECT 1 FROM pg_tables
        WHERE tablename = partition_name
    ) THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF device_metrics_partitioned
             FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
        RAISE NOTICE 'Created partition: %', partition_name;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- Step 6: Create retention cleanup function
CREATE OR REPLACE FUNCTION cleanup_old_metrics(retention_days INTEGER DEFAULT 30)
RETURNS INTEGER AS $$
DECLARE
    dropped_count INTEGER := 0;
    partition_record RECORD;
    cutoff_date DATE;
BEGIN
    cutoff_date := CURRENT_DATE - (retention_days || ' days')::interval;

    FOR partition_record IN
        SELECT tablename
        FROM pg_tables
        WHERE tablename LIKE 'device_metrics_20%'
        AND tablename < 'device_metrics_' || to_char(cutoff_date, 'YYYY_MM')
    LOOP
        EXECUTE format('DROP TABLE IF EXISTS %I', partition_record.tablename);
        dropped_count := dropped_count + 1;
        RAISE NOTICE 'Dropped old partition: %', partition_record.tablename;
    END LOOP;

    RETURN dropped_count;
END;
$$ LANGUAGE plpgsql;

-- Step 7: Create view for backward compatibility (if using new table)
-- Uncomment and run after migrating data
-- CREATE OR REPLACE VIEW device_metrics AS
--     SELECT * FROM device_metrics_partitioned;

-- Step 8: Data Retention Policy Table
CREATE TABLE IF NOT EXISTS retention_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    table_name VARCHAR(100) NOT NULL UNIQUE,
    retention_days INTEGER NOT NULL DEFAULT 90,
    enabled BOOLEAN DEFAULT TRUE,
    last_cleanup TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Insert default retention policies
INSERT INTO retention_policies (table_name, retention_days) VALUES
    ('device_metrics', 30),
    ('device_security', 90),
    ('device_sessions', 30),
    ('inventory_snapshots', 365),
    ('audit_log', 365)
ON CONFLICT (table_name) DO NOTHING;

-- Step 9: Create scheduled cleanup trigger (run manually or via cron)
CREATE OR REPLACE FUNCTION run_retention_cleanup()
RETURNS void AS $$
DECLARE
    policy RECORD;
    rows_deleted INTEGER;
BEGIN
    FOR policy IN SELECT * FROM retention_policies WHERE enabled = TRUE LOOP
        -- Handle metrics partition drops
        IF policy.table_name = 'device_metrics' THEN
            PERFORM cleanup_old_metrics(policy.retention_days);
        ELSE
            -- Standard table cleanup (if table has created_at or timestamp column)
            BEGIN
                EXECUTE format(
                    'DELETE FROM %I WHERE created_at < NOW() - interval ''%s days''',
                    policy.table_name, policy.retention_days
                );
                GET DIAGNOSTICS rows_deleted = ROW_COUNT;
                IF rows_deleted > 0 THEN
                    RAISE NOTICE 'Deleted % rows from %', rows_deleted, policy.table_name;
                END IF;
            EXCEPTION WHEN undefined_column THEN
                -- Try timestamp column instead
                BEGIN
                    EXECUTE format(
                        'DELETE FROM %I WHERE timestamp < NOW() - interval ''%s days''',
                        policy.table_name, policy.retention_days
                    );
                EXCEPTION WHEN undefined_column THEN
                    NULL; -- Table doesn't have expected columns
                END;
            END;
        END IF;

        UPDATE retention_policies
        SET last_cleanup = NOW(), updated_at = NOW()
        WHERE id = policy.id;
    END LOOP;
END;
$$ LANGUAGE plpgsql;
