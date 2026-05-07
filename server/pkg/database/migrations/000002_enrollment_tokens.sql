-- Enrollment tokens table for agent installation
-- Each token generates a unique agent when used

CREATE TABLE enrollment_tokens (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    token VARCHAR(64) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    created_by UUID REFERENCES users(id),
    expires_at TIMESTAMPTZ,
    max_uses INTEGER DEFAULT NULL,  -- NULL = unlimited
    use_count INTEGER DEFAULT 0,
    is_active BOOLEAN DEFAULT TRUE,
    tags TEXT[] DEFAULT '{}',       -- Default tags to apply to enrolled devices
    metadata JSONB DEFAULT '{}',    -- Default metadata for enrolled devices
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_enrollment_tokens_token ON enrollment_tokens(token);
CREATE INDEX idx_enrollment_tokens_active ON enrollment_tokens(is_active);

-- Agent downloads audit log
CREATE TABLE agent_downloads (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    token_id UUID REFERENCES enrollment_tokens(id) ON DELETE SET NULL,
    platform VARCHAR(50) NOT NULL,  -- windows, linux, macos
    architecture VARCHAR(20),       -- x64, arm64
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_agent_downloads_token ON agent_downloads(token_id);
CREATE INDEX idx_agent_downloads_created_at ON agent_downloads(created_at DESC);

-- Apply updated_at trigger
CREATE TRIGGER update_enrollment_tokens_updated_at BEFORE UPDATE ON enrollment_tokens
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Create a default enrollment token
INSERT INTO enrollment_tokens (token, name, description) VALUES
    ('sentinel_default_enrollment_token', 'Default Token', 'Default enrollment token for initial setup');
