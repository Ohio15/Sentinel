-- Fix os_types column type from TEXT[] to JSONB
-- This handles databases created with older schema

DO $$
BEGIN
    -- Check if os_types is TEXT[] and convert to JSONB
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'scripts'
        AND column_name = 'os_types'
        AND data_type = 'ARRAY'
    ) THEN
        -- Convert TEXT[] to JSONB
        ALTER TABLE scripts
        ALTER COLUMN os_types TYPE JSONB
        USING COALESCE(to_jsonb(os_types), '[]'::jsonb);

        -- Set default
        ALTER TABLE scripts
        ALTER COLUMN os_types SET DEFAULT '[]'::jsonb;

        RAISE NOTICE 'Converted os_types from TEXT[] to JSONB';
    ELSE
        RAISE NOTICE 'os_types column is already JSONB or does not exist';
    END IF;
END $$;
