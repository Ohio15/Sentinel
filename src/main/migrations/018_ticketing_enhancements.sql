-- Migration 018: Ticketing System Enhancements
-- Adds SLA policies, hierarchical categories, managed tags, ticket linking, and custom fields

-- ============================================================================
-- SLA POLICIES
-- ============================================================================

-- SLA policies table - defines response and resolution time targets
CREATE TABLE IF NOT EXISTS sla_policies (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    priority VARCHAR(20) NOT NULL,  -- urgent, high, medium, low
    response_target_minutes INTEGER NOT NULL,
    resolution_target_minutes INTEGER NOT NULL,
    business_hours_only BOOLEAN DEFAULT TRUE,
    business_hours_start TIME DEFAULT '09:00:00',
    business_hours_end TIME DEFAULT '17:00:00',
    business_days INTEGER[] DEFAULT ARRAY[1,2,3,4,5],  -- Mon-Fri (1=Mon, 7=Sun)
    client_id UUID REFERENCES clients(id) ON DELETE CASCADE,  -- NULL = global policy
    is_default BOOLEAN DEFAULT FALSE,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Add SLA tracking fields to tickets
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS sla_policy_id UUID REFERENCES sla_policies(id) ON DELETE SET NULL;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS first_response_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS first_response_due_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS resolution_due_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS sla_response_breached BOOLEAN DEFAULT FALSE;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS sla_resolution_breached BOOLEAN DEFAULT FALSE;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS sla_paused_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS sla_paused_duration_minutes INTEGER DEFAULT 0;

-- Create indexes for SLA queries
CREATE INDEX IF NOT EXISTS idx_sla_policies_priority ON sla_policies(priority);
CREATE INDEX IF NOT EXISTS idx_sla_policies_client ON sla_policies(client_id);
CREATE INDEX IF NOT EXISTS idx_tickets_sla_policy ON tickets(sla_policy_id);
CREATE INDEX IF NOT EXISTS idx_tickets_first_response_due ON tickets(first_response_due_at) WHERE first_response_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_tickets_resolution_due ON tickets(resolution_due_at) WHERE closed_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_tickets_sla_breaches ON tickets(sla_response_breached, sla_resolution_breached);

-- Insert default SLA policies
INSERT INTO sla_policies (name, priority, response_target_minutes, resolution_target_minutes, is_default)
SELECT * FROM (VALUES
    ('Critical - 1 Hour Response', 'urgent', 60, 240, TRUE),
    ('High - 4 Hour Response', 'high', 240, 480, TRUE),
    ('Medium - 8 Hour Response', 'medium', 480, 1440, TRUE),
    ('Low - 24 Hour Response', 'low', 1440, 2880, TRUE)
) AS defaults(name, priority, response_target_minutes, resolution_target_minutes, is_default)
WHERE NOT EXISTS (SELECT 1 FROM sla_policies LIMIT 1);

-- ============================================================================
-- HIERARCHICAL CATEGORIES
-- ============================================================================

-- Ticket categories table - supports parent-child hierarchy
CREATE TABLE IF NOT EXISTS ticket_categories (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    parent_id UUID REFERENCES ticket_categories(id) ON DELETE SET NULL,
    color VARCHAR(7) DEFAULT '#6B7280',
    icon VARCHAR(50) DEFAULT 'folder',
    sort_order INTEGER DEFAULT 0,
    is_active BOOLEAN DEFAULT TRUE,
    client_id UUID REFERENCES clients(id) ON DELETE CASCADE,  -- NULL = global category
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Add category reference to tickets
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS category_id UUID REFERENCES ticket_categories(id) ON DELETE SET NULL;

-- Create indexes for category queries
CREATE INDEX IF NOT EXISTS idx_ticket_categories_parent ON ticket_categories(parent_id);
CREATE INDEX IF NOT EXISTS idx_ticket_categories_client ON ticket_categories(client_id);
CREATE INDEX IF NOT EXISTS idx_ticket_categories_active ON ticket_categories(is_active);
CREATE INDEX IF NOT EXISTS idx_tickets_category ON tickets(category_id);

-- Insert default categories
INSERT INTO ticket_categories (name, description, color, icon, sort_order)
SELECT * FROM (VALUES
    ('Hardware', 'Physical device and hardware issues', '#EF4444', 'cpu', 1),
    ('Software', 'Software installation, updates, and issues', '#3B82F6', 'code', 2),
    ('Network', 'Network connectivity and configuration', '#10B981', 'wifi', 3),
    ('Security', 'Security incidents and concerns', '#F59E0B', 'shield', 4),
    ('Account', 'User account and access management', '#8B5CF6', 'user', 5),
    ('General', 'General inquiries and other requests', '#6B7280', 'help-circle', 6)
) AS defaults(name, description, color, icon, sort_order)
WHERE NOT EXISTS (SELECT 1 FROM ticket_categories LIMIT 1);

-- ============================================================================
-- MANAGED TAGS
-- ============================================================================

-- Managed tags table - pre-defined tags with usage tracking
CREATE TABLE IF NOT EXISTS ticket_tags (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL,
    color VARCHAR(7) DEFAULT '#6B7280',
    description TEXT,
    usage_count INTEGER DEFAULT 0,
    client_id UUID REFERENCES clients(id) ON DELETE CASCADE,  -- NULL = global tag
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(name, client_id)
);

-- Tag assignments (many-to-many relationship)
CREATE TABLE IF NOT EXISTS ticket_tag_assignments (
    ticket_id UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    tag_id UUID NOT NULL REFERENCES ticket_tags(id) ON DELETE CASCADE,
    assigned_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    assigned_by VARCHAR(255),
    PRIMARY KEY (ticket_id, tag_id)
);

-- Create indexes for tag queries
CREATE INDEX IF NOT EXISTS idx_ticket_tags_client ON ticket_tags(client_id);
CREATE INDEX IF NOT EXISTS idx_ticket_tags_usage ON ticket_tags(usage_count DESC);
CREATE INDEX IF NOT EXISTS idx_ticket_tag_assignments_ticket ON ticket_tag_assignments(ticket_id);
CREATE INDEX IF NOT EXISTS idx_ticket_tag_assignments_tag ON ticket_tag_assignments(tag_id);

-- Function to update tag usage count
CREATE OR REPLACE FUNCTION update_tag_usage_count() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE ticket_tags SET usage_count = usage_count + 1 WHERE id = NEW.tag_id;
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        UPDATE ticket_tags SET usage_count = usage_count - 1 WHERE id = OLD.tag_id;
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- Trigger to maintain tag usage counts
DROP TRIGGER IF EXISTS trigger_update_tag_usage ON ticket_tag_assignments;
CREATE TRIGGER trigger_update_tag_usage
    AFTER INSERT OR DELETE ON ticket_tag_assignments
    FOR EACH ROW EXECUTE FUNCTION update_tag_usage_count();

-- Insert default tags
INSERT INTO ticket_tags (name, color, description)
SELECT * FROM (VALUES
    ('urgent', '#EF4444', 'Requires immediate attention'),
    ('waiting-on-user', '#F59E0B', 'Waiting for user response'),
    ('waiting-on-vendor', '#8B5CF6', 'Waiting for vendor response'),
    ('escalated', '#DC2626', 'Escalated to higher tier'),
    ('recurring', '#6366F1', 'Issue has occurred before'),
    ('vip', '#EC4899', 'VIP customer'),
    ('documentation-needed', '#06B6D4', 'Needs documentation update'),
    ('training-opportunity', '#14B8A6', 'Could be prevented with training')
) AS defaults(name, color, description)
WHERE NOT EXISTS (SELECT 1 FROM ticket_tags LIMIT 1);

-- ============================================================================
-- TICKET LINKING
-- ============================================================================

-- Ticket links table - relationships between tickets
CREATE TABLE IF NOT EXISTS ticket_links (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    source_ticket_id UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    target_ticket_id UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    link_type VARCHAR(50) NOT NULL,  -- parent, child, related, duplicate, blocks, blocked_by
    created_by VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(source_ticket_id, target_ticket_id, link_type),
    CHECK (source_ticket_id != target_ticket_id)
);

-- Create indexes for link queries
CREATE INDEX IF NOT EXISTS idx_ticket_links_source ON ticket_links(source_ticket_id);
CREATE INDEX IF NOT EXISTS idx_ticket_links_target ON ticket_links(target_ticket_id);
CREATE INDEX IF NOT EXISTS idx_ticket_links_type ON ticket_links(link_type);

-- ============================================================================
-- CUSTOM FIELDS
-- ============================================================================

-- Custom field definitions table
CREATE TABLE IF NOT EXISTS custom_field_definitions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    field_key VARCHAR(100) NOT NULL,  -- Machine-readable key (e.g., 'affected_users')
    field_type VARCHAR(50) NOT NULL,  -- text, number, date, select, multiselect, checkbox, url
    description TEXT,
    placeholder VARCHAR(255),
    options JSONB DEFAULT '[]',  -- For select/multiselect: [{"value": "opt1", "label": "Option 1"}]
    default_value TEXT,
    is_required BOOLEAN DEFAULT FALSE,
    applies_to_type VARCHAR(50),  -- NULL = all types, or 'incident', 'request', 'problem', 'change'
    sort_order INTEGER DEFAULT 0,
    is_active BOOLEAN DEFAULT TRUE,
    client_id UUID REFERENCES clients(id) ON DELETE CASCADE,  -- NULL = global field
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(field_key, client_id)
);

-- Add custom fields storage to tickets
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS custom_fields JSONB DEFAULT '{}';

-- Create indexes for custom field queries
CREATE INDEX IF NOT EXISTS idx_custom_field_definitions_client ON custom_field_definitions(client_id);
CREATE INDEX IF NOT EXISTS idx_custom_field_definitions_type ON custom_field_definitions(applies_to_type);
CREATE INDEX IF NOT EXISTS idx_tickets_custom_fields ON tickets USING GIN (custom_fields);

-- ============================================================================
-- FULL-TEXT SEARCH INDEX FOR TICKETS
-- ============================================================================

-- Create full-text search index for ticket searching
CREATE INDEX IF NOT EXISTS idx_tickets_fulltext_search ON tickets
    USING GIN (to_tsvector('english', COALESCE(subject, '') || ' ' || COALESCE(description, '')));

-- ============================================================================
-- UPDATE TRIGGERS
-- ============================================================================

-- Trigger for sla_policies updated_at
DROP TRIGGER IF EXISTS update_sla_policies_updated_at ON sla_policies;
CREATE TRIGGER update_sla_policies_updated_at
    BEFORE UPDATE ON sla_policies
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Trigger for ticket_categories updated_at
DROP TRIGGER IF EXISTS update_ticket_categories_updated_at ON ticket_categories;
CREATE TRIGGER update_ticket_categories_updated_at
    BEFORE UPDATE ON ticket_categories
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Trigger for custom_field_definitions updated_at
DROP TRIGGER IF EXISTS update_custom_field_definitions_updated_at ON custom_field_definitions;
CREATE TRIGGER update_custom_field_definitions_updated_at
    BEFORE UPDATE ON custom_field_definitions
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
