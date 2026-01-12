-- Add username column to users table
ALTER TABLE users ADD COLUMN IF NOT EXISTS username VARCHAR(50) UNIQUE;

-- Update existing users to have a username based on email prefix
UPDATE users SET username = LOWER(SPLIT_PART(email, '@', 1))
WHERE username IS NULL;

-- Make username NOT NULL after populating
ALTER TABLE users ALTER COLUMN username SET NOT NULL;

-- Create index for username lookups
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

-- Create invitations table for secure registration
CREATE TABLE IF NOT EXISTS invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token VARCHAR(64) UNIQUE NOT NULL,
    email VARCHAR(255),
    role VARCHAR(50) DEFAULT 'viewer' CHECK (role IN ('admin', 'operator', 'viewer')),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    used_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Index for token lookups
CREATE INDEX IF NOT EXISTS idx_invitations_token ON invitations(token);

-- Index for cleanup of expired invitations
CREATE INDEX IF NOT EXISTS idx_invitations_expires_at ON invitations(expires_at);
