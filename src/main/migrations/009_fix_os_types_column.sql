-- Fix os_types column type - ensure it's JSONB
-- This migration safely handles both TEXT[] and JSONB columns

DO $$
BEGIN
    -- Try to drop the temp column if it exists from a failed migration
    BEGIN
        ALTER TABLE scripts DROP COLUMN IF EXISTS os_types_new;
    EXCEPTION WHEN OTHERS THEN
        -- Ignore errors
    END;

    -- Check if scripts table exists
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'scripts') THEN
        -- Check if os_types column exists and its type
        IF EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_name = 'scripts' AND column_name = 'os_types' AND udt_name = '_text'
        ) THEN
            RAISE NOTICE 'Converting os_types from TEXT[] to JSONB...';

            -- Create new JSONB column
            ALTER TABLE scripts ADD COLUMN os_types_new JSONB DEFAULT '[]'::jsonb;

            -- Copy and convert data
            UPDATE scripts SET os_types_new = COALESCE(to_jsonb(os_types), '[]'::jsonb);

            -- Drop old column and rename new
            ALTER TABLE scripts DROP COLUMN os_types;
            ALTER TABLE scripts RENAME COLUMN os_types_new TO os_types;

            RAISE NOTICE 'Successfully converted os_types to JSONB';
        ELSIF NOT EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_name = 'scripts' AND column_name = 'os_types'
        ) THEN
            -- Column doesn't exist, add it
            ALTER TABLE scripts ADD COLUMN os_types JSONB DEFAULT '[]'::jsonb;
            RAISE NOTICE 'Added os_types column as JSONB';
        ELSE
            RAISE NOTICE 'os_types column already exists with correct type';
        END IF;
    END IF;
END $$;
