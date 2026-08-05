-- Reverse 000043_mfa_totp.
-- mfa_events references users(id), so the table goes before the users columns.
DROP TABLE IF EXISTS mfa_events;
ALTER TABLE users DROP COLUMN IF EXISTS backup_codes;
ALTER TABLE users DROP COLUMN IF EXISTS mfa_required;
ALTER TABLE users DROP COLUMN IF EXISTS totp_verified_at;
ALTER TABLE users DROP COLUMN IF EXISTS totp_enabled;
ALTER TABLE users DROP COLUMN IF EXISTS totp_secret;
