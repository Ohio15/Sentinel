-- Fix device_updates table if it exists with wrong column type
DO $$
BEGIN
    -- Drop the table if it exists with wrong type - it will be recreated by 008
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'device_updates'
        AND column_name = 'device_id'
        AND data_type = 'text'
    ) THEN
        DROP TABLE device_updates CASCADE;
        RAISE NOTICE 'Dropped device_updates table with incorrect column type';
    END IF;
END $$;
