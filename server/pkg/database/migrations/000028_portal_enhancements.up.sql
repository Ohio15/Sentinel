-- Migration 017: Portal Enhancements
-- Adds public ticket IDs (TKT-XXXXXX), comment editing, and attachments support

-- Add public_id for human-readable ticket identifiers (TKT-XXXXXX)
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS public_id VARCHAR(12) UNIQUE;

-- Comment editing support
ALTER TABLE ticket_comments ADD COLUMN IF NOT EXISTS edited_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE ticket_comments ADD COLUMN IF NOT EXISTS edited_by VARCHAR(255);
ALTER TABLE ticket_comments ADD COLUMN IF NOT EXISTS original_content TEXT;

-- Attachments table (screenshots stored in filesystem, metadata in DB)
CREATE TABLE IF NOT EXISTS ticket_attachments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    ticket_id UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    comment_id UUID REFERENCES ticket_comments(id) ON DELETE CASCADE,
    filename VARCHAR(255) NOT NULL,
    original_filename VARCHAR(255) NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    file_size INTEGER NOT NULL,
    storage_path VARCHAR(500) NOT NULL,
    thumbnail_path VARCHAR(500),
    uploaded_by_email VARCHAR(255),
    uploaded_by_name VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create index for faster attachment lookups
CREATE INDEX IF NOT EXISTS idx_ticket_attachments_ticket_id ON ticket_attachments(ticket_id);
CREATE INDEX IF NOT EXISTS idx_ticket_attachments_comment_id ON ticket_attachments(comment_id);

-- Auto-generate TKT-XXXXXX on insert
-- Uses uppercase letters (excluding confusing O, I, L) and numbers (excluding 0, 1)
CREATE OR REPLACE FUNCTION generate_ticket_public_id() RETURNS VARCHAR(12) AS $$
DECLARE
    chars VARCHAR(32) := 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789';
    result VARCHAR(12) := 'TKT-';
    i INTEGER;
BEGIN
    FOR i IN 1..6 LOOP
        result := result || substr(chars, floor(random() * length(chars) + 1)::int, 1);
    END LOOP;
    RETURN result;
END;
$$ LANGUAGE plpgsql;

-- Trigger function to set public_id on insert with collision handling
CREATE OR REPLACE FUNCTION set_ticket_public_id() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.public_id IS NULL THEN
        LOOP
            NEW.public_id := generate_ticket_public_id();
            EXIT WHEN NOT EXISTS (SELECT 1 FROM tickets WHERE public_id = NEW.public_id);
        END LOOP;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger for auto-generation
DROP TRIGGER IF EXISTS trigger_set_ticket_public_id ON tickets;
CREATE TRIGGER trigger_set_ticket_public_id BEFORE INSERT ON tickets
    FOR EACH ROW EXECUTE FUNCTION set_ticket_public_id();

-- Backfill existing tickets with public_id
DO $$
DECLARE
    ticket_record RECORD;
    new_public_id VARCHAR(12);
BEGIN
    FOR ticket_record IN SELECT id FROM tickets WHERE public_id IS NULL LOOP
        LOOP
            new_public_id := generate_ticket_public_id();
            EXIT WHEN NOT EXISTS (SELECT 1 FROM tickets WHERE public_id = new_public_id);
        END LOOP;
        UPDATE tickets SET public_id = new_public_id WHERE id = ticket_record.id;
    END LOOP;
END $$;
