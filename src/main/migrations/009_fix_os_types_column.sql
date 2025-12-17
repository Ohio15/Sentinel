-- Fix os_types column type from TEXT[] to JSONB
-- This handles databases created with older schema

-- Drop and recreate the column to ensure correct type
-- First check if it's not already JSONB
DO $$
DECLARE
    col_type text;
BEGIN
    -- Get the actual column type
    SELECT udt_name INTO col_type
    FROM information_schema.columns
    WHERE table_name = 'scripts' AND column_name = 'os_types';

    RAISE NOTICE 'Current os_types type: %', col_type;

    -- If it's _text (TEXT[]), convert it
    IF col_type = '_text' THEN
        -- Add temp column
        ALTER TABLE scripts ADD COLUMN os_types_new JSONB DEFAULT '[]'::jsonb;

        -- Copy data converting TEXT[] to JSONB
        UPDATE scripts SET os_types_new = COALESCE(to_jsonb(os_types), '[]'::jsonb);

        -- Drop old column
        ALTER TABLE scripts DROP COLUMN os_types;

        -- Rename new column
        ALTER TABLE scripts RENAME COLUMN os_types_new TO os_types;

        RAISE NOTICE 'Converted os_types from TEXT[] to JSONB';
    ELSIF col_type IS NULL THEN
        -- Column doesn't exist, add it
        ALTER TABLE scripts ADD COLUMN os_types JSONB DEFAULT '[]'::jsonb;
        RAISE NOTICE 'Added os_types column as JSONB';
    ELSE
        RAISE NOTICE 'os_types column is already correct type: %', col_type;
    END IF;
END $$;
