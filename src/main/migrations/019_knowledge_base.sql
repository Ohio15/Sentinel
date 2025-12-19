-- Migration 019: Knowledge Base
-- Adds knowledge base categories, articles, and feedback system

-- ============================================================================
-- KB CATEGORIES
-- ============================================================================

-- Knowledge base categories table - supports hierarchy
CREATE TABLE IF NOT EXISTS kb_categories (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(255) NOT NULL UNIQUE,
    description TEXT,
    parent_id UUID REFERENCES kb_categories(id) ON DELETE SET NULL,
    icon VARCHAR(50) DEFAULT 'folder',
    color VARCHAR(7) DEFAULT '#6B7280',
    sort_order INTEGER DEFAULT 0,
    is_active BOOLEAN DEFAULT TRUE,
    article_count INTEGER DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for KB category queries
CREATE INDEX IF NOT EXISTS idx_kb_categories_slug ON kb_categories(slug);
CREATE INDEX IF NOT EXISTS idx_kb_categories_parent ON kb_categories(parent_id);
CREATE INDEX IF NOT EXISTS idx_kb_categories_active ON kb_categories(is_active);
CREATE INDEX IF NOT EXISTS idx_kb_categories_sort ON kb_categories(sort_order);

-- ============================================================================
-- KB ARTICLES
-- ============================================================================

-- Knowledge base articles table
CREATE TABLE IF NOT EXISTS kb_articles (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    title VARCHAR(500) NOT NULL,
    slug VARCHAR(500) NOT NULL UNIQUE,
    content TEXT,  -- Markdown content
    content_html TEXT,  -- Pre-rendered HTML for portal display
    summary TEXT,  -- Short description for search results
    category_id UUID REFERENCES kb_categories(id) ON DELETE SET NULL,
    status VARCHAR(50) DEFAULT 'draft',  -- draft, published, archived
    is_featured BOOLEAN DEFAULT FALSE,
    is_pinned BOOLEAN DEFAULT FALSE,
    tags JSONB DEFAULT '[]',  -- Simple string array: ["vpn", "security"]
    keywords JSONB DEFAULT '[]',  -- SEO keywords for search
    view_count INTEGER DEFAULT 0,
    helpful_count INTEGER DEFAULT 0,
    not_helpful_count INTEGER DEFAULT 0,
    author_name VARCHAR(255),
    author_email VARCHAR(255),
    last_reviewed_at TIMESTAMP WITH TIME ZONE,
    last_reviewed_by VARCHAR(255),
    published_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for KB article queries
CREATE INDEX IF NOT EXISTS idx_kb_articles_slug ON kb_articles(slug);
CREATE INDEX IF NOT EXISTS idx_kb_articles_category ON kb_articles(category_id);
CREATE INDEX IF NOT EXISTS idx_kb_articles_status ON kb_articles(status);
CREATE INDEX IF NOT EXISTS idx_kb_articles_featured ON kb_articles(is_featured) WHERE is_featured = TRUE;
CREATE INDEX IF NOT EXISTS idx_kb_articles_published ON kb_articles(published_at DESC) WHERE status = 'published';
CREATE INDEX IF NOT EXISTS idx_kb_articles_views ON kb_articles(view_count DESC);
CREATE INDEX IF NOT EXISTS idx_kb_articles_helpful ON kb_articles(helpful_count DESC);

-- Full-text search index for KB articles
CREATE INDEX IF NOT EXISTS idx_kb_articles_fulltext ON kb_articles
    USING GIN (to_tsvector('english',
        COALESCE(title, '') || ' ' ||
        COALESCE(content, '') || ' ' ||
        COALESCE(summary, '')
    ));

-- GIN index for tags/keywords arrays
CREATE INDEX IF NOT EXISTS idx_kb_articles_tags ON kb_articles USING GIN (tags);
CREATE INDEX IF NOT EXISTS idx_kb_articles_keywords ON kb_articles USING GIN (keywords);

-- ============================================================================
-- KB ARTICLE FEEDBACK
-- ============================================================================

-- Feedback table to track user responses
CREATE TABLE IF NOT EXISTS kb_article_feedback (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    article_id UUID NOT NULL REFERENCES kb_articles(id) ON DELETE CASCADE,
    is_helpful BOOLEAN NOT NULL,
    comment TEXT,
    user_email VARCHAR(255),
    user_name VARCHAR(255),
    ip_address VARCHAR(45),  -- Support IPv6
    user_agent TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create index for feedback queries
CREATE INDEX IF NOT EXISTS idx_kb_article_feedback_article ON kb_article_feedback(article_id);
CREATE INDEX IF NOT EXISTS idx_kb_article_feedback_helpful ON kb_article_feedback(is_helpful);
CREATE INDEX IF NOT EXISTS idx_kb_article_feedback_created ON kb_article_feedback(created_at DESC);

-- ============================================================================
-- KB ARTICLE VIEWS
-- ============================================================================

-- Track article views for analytics
CREATE TABLE IF NOT EXISTS kb_article_views (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    article_id UUID NOT NULL REFERENCES kb_articles(id) ON DELETE CASCADE,
    user_email VARCHAR(255),
    session_id VARCHAR(255),
    ip_address VARCHAR(45),
    referrer TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for view tracking
CREATE INDEX IF NOT EXISTS idx_kb_article_views_article ON kb_article_views(article_id);
CREATE INDEX IF NOT EXISTS idx_kb_article_views_created ON kb_article_views(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_kb_article_views_date ON kb_article_views(DATE(created_at));

-- ============================================================================
-- KB RELATED ARTICLES
-- ============================================================================

-- Manually linked related articles
CREATE TABLE IF NOT EXISTS kb_related_articles (
    source_article_id UUID NOT NULL REFERENCES kb_articles(id) ON DELETE CASCADE,
    target_article_id UUID NOT NULL REFERENCES kb_articles(id) ON DELETE CASCADE,
    sort_order INTEGER DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (source_article_id, target_article_id),
    CHECK (source_article_id != target_article_id)
);

-- Create index for related article queries
CREATE INDEX IF NOT EXISTS idx_kb_related_articles_source ON kb_related_articles(source_article_id);

-- ============================================================================
-- FUNCTIONS FOR KB MANAGEMENT
-- ============================================================================

-- Function to generate URL-friendly slug from title
CREATE OR REPLACE FUNCTION generate_kb_slug(title TEXT) RETURNS TEXT AS $$
BEGIN
    RETURN LOWER(
        REGEXP_REPLACE(
            REGEXP_REPLACE(
                REGEXP_REPLACE(title, '[^a-zA-Z0-9\s-]', '', 'g'),  -- Remove special chars
                '\s+', '-', 'g'  -- Replace spaces with hyphens
            ),
            '-+', '-', 'g'  -- Collapse multiple hyphens
        )
    );
END;
$$ LANGUAGE plpgsql;

-- Function to update article counts in categories
CREATE OR REPLACE FUNCTION update_kb_category_article_count() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.category_id IS NOT NULL AND NEW.status = 'published' THEN
            UPDATE kb_categories SET article_count = article_count + 1 WHERE id = NEW.category_id;
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'UPDATE' THEN
        -- Handle category change
        IF OLD.category_id IS DISTINCT FROM NEW.category_id OR OLD.status IS DISTINCT FROM NEW.status THEN
            -- Decrease old category count
            IF OLD.category_id IS NOT NULL AND OLD.status = 'published' THEN
                UPDATE kb_categories SET article_count = article_count - 1 WHERE id = OLD.category_id;
            END IF;
            -- Increase new category count
            IF NEW.category_id IS NOT NULL AND NEW.status = 'published' THEN
                UPDATE kb_categories SET article_count = article_count + 1 WHERE id = NEW.category_id;
            END IF;
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        IF OLD.category_id IS NOT NULL AND OLD.status = 'published' THEN
            UPDATE kb_categories SET article_count = article_count - 1 WHERE id = OLD.category_id;
        END IF;
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- Trigger to maintain category article counts
DROP TRIGGER IF EXISTS trigger_update_kb_category_count ON kb_articles;
CREATE TRIGGER trigger_update_kb_category_count
    AFTER INSERT OR UPDATE OR DELETE ON kb_articles
    FOR EACH ROW EXECUTE FUNCTION update_kb_category_article_count();

-- Function to update helpful/not helpful counts
CREATE OR REPLACE FUNCTION update_kb_article_feedback_counts() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.is_helpful THEN
            UPDATE kb_articles SET helpful_count = helpful_count + 1 WHERE id = NEW.article_id;
        ELSE
            UPDATE kb_articles SET not_helpful_count = not_helpful_count + 1 WHERE id = NEW.article_id;
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        IF OLD.is_helpful THEN
            UPDATE kb_articles SET helpful_count = helpful_count - 1 WHERE id = OLD.article_id;
        ELSE
            UPDATE kb_articles SET not_helpful_count = not_helpful_count - 1 WHERE id = OLD.article_id;
        END IF;
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- Trigger to maintain feedback counts
DROP TRIGGER IF EXISTS trigger_update_kb_feedback_counts ON kb_article_feedback;
CREATE TRIGGER trigger_update_kb_feedback_counts
    AFTER INSERT OR DELETE ON kb_article_feedback
    FOR EACH ROW EXECUTE FUNCTION update_kb_article_feedback_counts();

-- ============================================================================
-- UPDATE TRIGGERS
-- ============================================================================

-- Trigger for kb_categories updated_at
DROP TRIGGER IF EXISTS update_kb_categories_updated_at ON kb_categories;
CREATE TRIGGER update_kb_categories_updated_at
    BEFORE UPDATE ON kb_categories
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Trigger for kb_articles updated_at
DROP TRIGGER IF EXISTS update_kb_articles_updated_at ON kb_articles;
CREATE TRIGGER update_kb_articles_updated_at
    BEFORE UPDATE ON kb_articles
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============================================================================
-- INSERT DEFAULT KB CATEGORIES
-- ============================================================================

INSERT INTO kb_categories (name, slug, description, icon, color, sort_order)
SELECT * FROM (VALUES
    ('Getting Started', 'getting-started', 'Basic guides for new users', 'play-circle', '#10B981', 1),
    ('Troubleshooting', 'troubleshooting', 'Common problems and solutions', 'wrench', '#F59E0B', 2),
    ('How-To Guides', 'how-to-guides', 'Step-by-step instructions', 'book-open', '#3B82F6', 3),
    ('FAQ', 'faq', 'Frequently asked questions', 'help-circle', '#8B5CF6', 4),
    ('Best Practices', 'best-practices', 'Recommended approaches and tips', 'star', '#EC4899', 5),
    ('Release Notes', 'release-notes', 'Version updates and changelogs', 'file-text', '#6B7280', 6)
) AS defaults(name, slug, description, icon, color, sort_order)
WHERE NOT EXISTS (SELECT 1 FROM kb_categories LIMIT 1);

-- ============================================================================
-- INSERT SAMPLE KB ARTICLES
-- ============================================================================

INSERT INTO kb_articles (title, slug, content, summary, category_id, status, is_featured, author_name, published_at)
SELECT
    'Getting Started with Support Portal',
    'getting-started-support-portal',
    E'# Getting Started with Support Portal\n\nWelcome to the Support Portal! This guide will help you get started with creating and managing support tickets.\n\n## Creating a Ticket\n\n1. Click the "New Ticket" button\n2. Fill in the subject and description\n3. Select a priority level\n4. Click "Submit"\n\n## Checking Ticket Status\n\nYou can view all your tickets in the "My Tickets" section. Each ticket shows its current status:\n\n- **Open**: Ticket is being reviewed\n- **In Progress**: Work is underway\n- **Waiting**: Awaiting your response\n- **Resolved**: Issue has been fixed\n- **Closed**: Ticket is complete\n\n## Tips for Better Support\n\n- Be specific about your issue\n- Include any error messages\n- Attach screenshots when helpful',
    'Learn how to create and manage support tickets in the portal',
    (SELECT id FROM kb_categories WHERE slug = 'getting-started' LIMIT 1),
    'published',
    TRUE,
    'System',
    CURRENT_TIMESTAMP
WHERE NOT EXISTS (SELECT 1 FROM kb_articles WHERE slug = 'getting-started-support-portal');

INSERT INTO kb_articles (title, slug, content, summary, category_id, status, is_featured, author_name, published_at)
SELECT
    'Common Connection Issues',
    'common-connection-issues',
    E'# Common Connection Issues\n\nThis guide covers the most common connectivity problems and their solutions.\n\n## Unable to Connect to Network\n\n### Check Physical Connections\n1. Ensure your Ethernet cable is properly connected\n2. Try a different cable or port\n3. Check if the network light is on\n\n### Restart Network Equipment\n1. Turn off your modem/router\n2. Wait 30 seconds\n3. Turn it back on\n4. Wait for all lights to stabilize\n\n## Slow Internet Speed\n\n- Check for bandwidth-heavy applications\n- Run a speed test at speedtest.net\n- Contact your ISP if speeds are consistently low\n\n## VPN Connection Failed\n\n1. Verify your credentials\n2. Check if VPN server is accessible\n3. Try an alternative server\n4. Ensure your firewall allows VPN traffic',
    'Solutions for common network and connectivity problems',
    (SELECT id FROM kb_categories WHERE slug = 'troubleshooting' LIMIT 1),
    'published',
    TRUE,
    'System',
    CURRENT_TIMESTAMP
WHERE NOT EXISTS (SELECT 1 FROM kb_articles WHERE slug = 'common-connection-issues');
