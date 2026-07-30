ALTER TABLE devices
    DROP COLUMN IF EXISTS hidden_by,
    DROP COLUMN IF EXISTS hidden_at;
