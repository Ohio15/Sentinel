-- WebAuthn Credentials Migration
-- Stores registered passkeys for passwordless authentication

-- WebAuthn credentials table - stores user's registered passkeys
CREATE TABLE IF NOT EXISTS webauthn_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Credential identifiers
    credential_id BYTEA NOT NULL UNIQUE,
    public_key BYTEA NOT NULL,

    -- Credential metadata
    name VARCHAR(255) NOT NULL DEFAULT 'My Passkey',
    aaguid BYTEA,                          -- Authenticator Attestation GUID
    sign_count BIGINT NOT NULL DEFAULT 0,  -- Signature counter for clone detection

    -- Credential flags
    backup_eligible BOOLEAN NOT NULL DEFAULT false,
    backup_state BOOLEAN NOT NULL DEFAULT false,

    -- Attestation data (optional, for enterprise use)
    attestation_type VARCHAR(50),

    -- Transport hints (usb, nfc, ble, internal, hybrid)
    transports TEXT[],

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,

    -- Organization scoping
    organization_id INTEGER REFERENCES organizations(id) ON DELETE CASCADE
);

-- Index for looking up credentials by user
CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_user_id ON webauthn_credentials(user_id);

-- Index for looking up credentials by credential_id (used during authentication)
CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_credential_id ON webauthn_credentials(credential_id);

-- WebAuthn sessions table - temporary storage for challenges during ceremonies
CREATE TABLE IF NOT EXISTS webauthn_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Session identifier (for looking up during ceremony completion)
    session_id VARCHAR(255) NOT NULL UNIQUE,

    -- User association (NULL for authentication ceremonies before user is identified)
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,

    -- Challenge data
    challenge BYTEA NOT NULL,

    -- Session type: 'registration' or 'authentication'
    session_type VARCHAR(20) NOT NULL CHECK (session_type IN ('registration', 'authentication')),

    -- Allowed credentials (for authentication, stores credential IDs user can use)
    allowed_credentials BYTEA[],

    -- User verification requirement: 'required', 'preferred', 'discouraged'
    user_verification VARCHAR(20) NOT NULL DEFAULT 'preferred',

    -- Expiration (challenges should be short-lived)
    expires_at TIMESTAMPTZ NOT NULL,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Organization scoping
    organization_id INTEGER REFERENCES organizations(id) ON DELETE CASCADE
);

-- Index for session lookup
CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_session_id ON webauthn_sessions(session_id);

-- Index for cleaning up expired sessions
CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_expires_at ON webauthn_sessions(expires_at);

-- Index for user sessions
CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_user_id ON webauthn_sessions(user_id);

-- Cleanup function for expired sessions
CREATE OR REPLACE FUNCTION cleanup_expired_webauthn_sessions()
RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    DELETE FROM webauthn_sessions WHERE expires_at < NOW();
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;

-- Add comment for documentation
COMMENT ON TABLE webauthn_credentials IS 'Stores WebAuthn/FIDO2 passkey credentials for passwordless authentication';
COMMENT ON TABLE webauthn_sessions IS 'Temporary storage for WebAuthn ceremony challenges (5 minute expiry)';
COMMENT ON COLUMN webauthn_credentials.sign_count IS 'Signature counter for detecting cloned authenticators';
COMMENT ON COLUMN webauthn_credentials.aaguid IS 'Authenticator Attestation GUID - identifies the authenticator model';
COMMENT ON COLUMN webauthn_credentials.backup_eligible IS 'Whether the credential can be backed up (e.g., to iCloud Keychain)';
COMMENT ON COLUMN webauthn_credentials.backup_state IS 'Whether the credential is currently backed up';
