-- Migration: Secure token hashing
-- CW-003 Security Fix: Hash enrollment tokens with bcrypt

-- Add token_hash column for hashed tokens
ALTER TABLE enrollment_tokens ADD COLUMN IF NOT EXISTS token_hash VARCHAR(255);

-- Add is_legacy flag to identify pre-hashed tokens  
ALTER TABLE enrollment_tokens ADD COLUMN IF NOT EXISTS is_legacy BOOLEAN DEFAULT FALSE;

-- Mark existing tokens as legacy (plain text)
UPDATE enrollment_tokens SET is_legacy = TRUE WHERE token_hash IS NULL;

-- Copy existing plain tokens to token_hash column temporarily
-- These will be validated differently until properly hashed
UPDATE enrollment_tokens SET token_hash = token WHERE token_hash IS NULL;

-- Create index on token_hash for lookup performance
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_hash ON enrollment_tokens(token_hash);

-- Note: The token column is kept for backwards compatibility
-- Legacy tokens will be validated by direct comparison
-- New tokens will be validated by bcrypt comparison

-- Add comment for documentation
COMMENT ON COLUMN enrollment_tokens.token_hash IS 'Bcrypt hash of the token (or plain text for legacy tokens)';
COMMENT ON COLUMN enrollment_tokens.is_legacy IS 'True if token is stored in plain text (pre-migration)';
