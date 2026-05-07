-- Sentinel RMM PostgreSQL Schema
-- Migration: 014_portal_users
-- Support portal with M365 SSO authentication

-- Client tenant mapping (Azure AD tenant ID → Sentinel client)
CREATE TABLE IF NOT EXISTS client_tenants (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    client_id UUID REFERENCES clients(id) ON DELETE CASCADE,
    tenant_id VARCHAR(255) NOT NULL UNIQUE,  -- Azure AD tenant ID (GUID)
    tenant_name VARCHAR(255),                 -- Display name, e.g., "Contoso Corp"
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Portal user sessions
CREATE TABLE IF NOT EXISTS portal_sessions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_email VARCHAR(255) NOT NULL,
    user_name VARCHAR(255),
    tenant_id VARCHAR(255) NOT NULL,
    client_id UUID REFERENCES clients(id) ON DELETE CASCADE,
    access_token TEXT,
    refresh_token TEXT,
    id_token TEXT,
    expires_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    last_activity TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Add submitter info to tickets (from portal submissions)
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS submitter_email VARCHAR(255);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS submitter_name VARCHAR(255);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS source VARCHAR(50) DEFAULT 'internal';

-- Email notification queue
CREATE TABLE IF NOT EXISTS email_queue (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    to_addresses JSONB NOT NULL,
    cc_addresses JSONB DEFAULT '[]'::jsonb,
    subject VARCHAR(500) NOT NULL,
    body_html TEXT,
    body_text TEXT,
    template_name VARCHAR(100),
    template_data JSONB DEFAULT '{}'::jsonb,
    status VARCHAR(50) DEFAULT 'pending',  -- pending, sent, failed
    error_message TEXT,
    retry_count INTEGER DEFAULT 0,
    scheduled_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    sent_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Email templates
CREATE TABLE IF NOT EXISTS email_templates (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL UNIQUE,
    subject VARCHAR(500) NOT NULL,
    body_html TEXT NOT NULL,
    body_text TEXT,
    description TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_client_tenants_tenant_id ON client_tenants(tenant_id);
CREATE INDEX IF NOT EXISTS idx_client_tenants_client_id ON client_tenants(client_id);
CREATE INDEX IF NOT EXISTS idx_portal_sessions_user_email ON portal_sessions(user_email);
CREATE INDEX IF NOT EXISTS idx_portal_sessions_tenant_id ON portal_sessions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_portal_sessions_expires_at ON portal_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_tickets_submitter_email ON tickets(submitter_email);
CREATE INDEX IF NOT EXISTS idx_tickets_source ON tickets(source);
CREATE INDEX IF NOT EXISTS idx_email_queue_status ON email_queue(status);
CREATE INDEX IF NOT EXISTS idx_email_queue_scheduled_at ON email_queue(scheduled_at);

-- Triggers for updated_at
DROP TRIGGER IF EXISTS update_client_tenants_updated_at ON client_tenants;
CREATE TRIGGER update_client_tenants_updated_at
    BEFORE UPDATE ON client_tenants
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_email_templates_updated_at ON email_templates;
CREATE TRIGGER update_email_templates_updated_at
    BEFORE UPDATE ON email_templates
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Insert default email templates
INSERT INTO email_templates (name, subject, body_html, body_text, description)
SELECT * FROM (VALUES
    ('ticket_created',
     'New Support Ticket #{{ticket.number}}: {{ticket.subject}}',
     '<h2>New Support Ticket Created</h2>
<p>A new support ticket has been submitted.</p>
<table style="border-collapse: collapse; margin: 20px 0;">
<tr><td style="padding: 8px; font-weight: bold;">Ticket #:</td><td style="padding: 8px;">{{ticket.number}}</td></tr>
<tr><td style="padding: 8px; font-weight: bold;">Subject:</td><td style="padding: 8px;">{{ticket.subject}}</td></tr>
<tr><td style="padding: 8px; font-weight: bold;">Priority:</td><td style="padding: 8px;">{{ticket.priority}}</td></tr>
<tr><td style="padding: 8px; font-weight: bold;">Submitted By:</td><td style="padding: 8px;">{{submitter.name}} ({{submitter.email}})</td></tr>
</table>
<p><strong>Description:</strong></p>
<div style="background: #f5f5f5; padding: 15px; border-radius: 5px;">{{ticket.description}}</div>
<p style="margin-top: 20px;"><a href="{{portal.ticketUrl}}">View Ticket</a></p>',
     'New Support Ticket Created\n\nTicket #: {{ticket.number}}\nSubject: {{ticket.subject}}\nPriority: {{ticket.priority}}\nSubmitted By: {{submitter.name}} ({{submitter.email}})\n\nDescription:\n{{ticket.description}}\n\nView Ticket: {{portal.ticketUrl}}',
     'Sent to technicians when a new ticket is created'),

    ('ticket_updated',
     'Ticket #{{ticket.number}} Updated: {{ticket.subject}}',
     '<h2>Support Ticket Updated</h2>
<p>Your support ticket has been updated.</p>
<table style="border-collapse: collapse; margin: 20px 0;">
<tr><td style="padding: 8px; font-weight: bold;">Ticket #:</td><td style="padding: 8px;">{{ticket.number}}</td></tr>
<tr><td style="padding: 8px; font-weight: bold;">Subject:</td><td style="padding: 8px;">{{ticket.subject}}</td></tr>
<tr><td style="padding: 8px; font-weight: bold;">Status:</td><td style="padding: 8px;">{{ticket.status}}</td></tr>
</table>
<p><strong>Update:</strong></p>
<div style="background: #f5f5f5; padding: 15px; border-radius: 5px;">{{update.message}}</div>
<p style="margin-top: 20px;"><a href="{{portal.ticketUrl}}">View Ticket</a></p>',
     'Support Ticket Updated\n\nTicket #: {{ticket.number}}\nSubject: {{ticket.subject}}\nStatus: {{ticket.status}}\n\nUpdate:\n{{update.message}}\n\nView Ticket: {{portal.ticketUrl}}',
     'Sent to submitter when ticket is updated'),

    ('ticket_comment',
     'New Comment on Ticket #{{ticket.number}}: {{ticket.subject}}',
     '<h2>New Comment Added</h2>
<p>A new comment has been added to your support ticket.</p>
<table style="border-collapse: collapse; margin: 20px 0;">
<tr><td style="padding: 8px; font-weight: bold;">Ticket #:</td><td style="padding: 8px;">{{ticket.number}}</td></tr>
<tr><td style="padding: 8px; font-weight: bold;">Subject:</td><td style="padding: 8px;">{{ticket.subject}}</td></tr>
<tr><td style="padding: 8px; font-weight: bold;">Comment By:</td><td style="padding: 8px;">{{comment.author}}</td></tr>
</table>
<p><strong>Comment:</strong></p>
<div style="background: #f5f5f5; padding: 15px; border-radius: 5px;">{{comment.content}}</div>
<p style="margin-top: 20px;"><a href="{{portal.ticketUrl}}">View Ticket</a></p>',
     'New Comment Added\n\nTicket #: {{ticket.number}}\nSubject: {{ticket.subject}}\nComment By: {{comment.author}}\n\nComment:\n{{comment.content}}\n\nView Ticket: {{portal.ticketUrl}}',
     'Sent when a new comment is added'),

    ('ticket_resolved',
     'Ticket #{{ticket.number}} Resolved: {{ticket.subject}}',
     '<h2>Support Ticket Resolved</h2>
<p>Your support ticket has been marked as resolved.</p>
<table style="border-collapse: collapse; margin: 20px 0;">
<tr><td style="padding: 8px; font-weight: bold;">Ticket #:</td><td style="padding: 8px;">{{ticket.number}}</td></tr>
<tr><td style="padding: 8px; font-weight: bold;">Subject:</td><td style="padding: 8px;">{{ticket.subject}}</td></tr>
<tr><td style="padding: 8px; font-weight: bold;">Resolution:</td><td style="padding: 8px;">{{ticket.resolution}}</td></tr>
</table>
<p>If you believe this issue has not been fully addressed, please reply to this email or reopen the ticket.</p>
<p style="margin-top: 20px;"><a href="{{portal.ticketUrl}}">View Ticket</a></p>',
     'Support Ticket Resolved\n\nTicket #: {{ticket.number}}\nSubject: {{ticket.subject}}\nResolution: {{ticket.resolution}}\n\nIf you believe this issue has not been fully addressed, please reply to this email or reopen the ticket.\n\nView Ticket: {{portal.ticketUrl}}',
     'Sent to submitter when ticket is resolved')
) AS defaults(name, subject, body_html, body_text, description)
WHERE NOT EXISTS (SELECT 1 FROM email_templates WHERE name = 'ticket_created');
