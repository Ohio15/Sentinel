-- Sentinel RMM PostgreSQL Schema
-- Migration: 002b_tickets

-- Tickets table
CREATE TABLE IF NOT EXISTS tickets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    ticket_number SERIAL,
    subject VARCHAR(500) NOT NULL,
    description TEXT,
    status VARCHAR(50) DEFAULT 'open',
    priority VARCHAR(20) DEFAULT 'medium',
    type VARCHAR(50) DEFAULT 'incident',
    device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
    requester_name VARCHAR(255),
    requester_email VARCHAR(255),
    assigned_to VARCHAR(255),
    tags JSONB DEFAULT '[]'::jsonb,
    due_date TIMESTAMP WITH TIME ZONE,
    resolved_at TIMESTAMP WITH TIME ZONE,
    closed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Ticket comments/responses table
CREATE TABLE IF NOT EXISTS ticket_comments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    ticket_id UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    is_internal BOOLEAN DEFAULT FALSE,
    author_name VARCHAR(255) NOT NULL,
    author_email VARCHAR(255),
    attachments JSONB DEFAULT '[]'::jsonb,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Ticket activity log
CREATE TABLE IF NOT EXISTS ticket_activity (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    ticket_id UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    action VARCHAR(100) NOT NULL,
    field_name VARCHAR(100),
    old_value TEXT,
    new_value TEXT,
    actor_name VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Quick response templates
CREATE TABLE IF NOT EXISTS ticket_templates (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    subject VARCHAR(500),
    content TEXT NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets(status);
CREATE INDEX IF NOT EXISTS idx_tickets_priority ON tickets(priority);
CREATE INDEX IF NOT EXISTS idx_tickets_device_id ON tickets(device_id);
CREATE INDEX IF NOT EXISTS idx_tickets_assigned_to ON tickets(assigned_to);
CREATE INDEX IF NOT EXISTS idx_tickets_created_at ON tickets(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tickets_ticket_number ON tickets(ticket_number);

CREATE INDEX IF NOT EXISTS idx_ticket_comments_ticket_id ON ticket_comments(ticket_id);
CREATE INDEX IF NOT EXISTS idx_ticket_comments_created_at ON ticket_comments(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ticket_activity_ticket_id ON ticket_activity(ticket_id);
CREATE INDEX IF NOT EXISTS idx_ticket_activity_created_at ON ticket_activity(created_at DESC);

-- Trigger for tickets updated_at
DROP TRIGGER IF EXISTS update_tickets_updated_at ON tickets;
CREATE TRIGGER update_tickets_updated_at
    BEFORE UPDATE ON tickets
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_ticket_templates_updated_at ON ticket_templates;
CREATE TRIGGER update_ticket_templates_updated_at
    BEFORE UPDATE ON ticket_templates
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Insert default quick response templates
INSERT INTO ticket_templates (name, subject, content)
SELECT * FROM (VALUES
    ('Acknowledgment', NULL, 'Thank you for contacting support. We have received your request and will get back to you shortly.'),
    ('Request More Info', NULL, 'Thank you for your ticket. To help us resolve this issue, could you please provide the following additional information:\n\n1. Steps to reproduce the issue\n2. Any error messages you''re seeing\n3. When did this issue first start occurring?'),
    ('Issue Resolved', NULL, 'We''re pleased to inform you that the issue has been resolved. Please let us know if you experience any further problems.'),
    ('Scheduled Maintenance', NULL, 'This issue is related to scheduled maintenance. The system should be back to normal operation within [timeframe]. We apologize for any inconvenience.')
) AS defaults(name, subject, content)
WHERE NOT EXISTS (SELECT 1 FROM ticket_templates LIMIT 1);
