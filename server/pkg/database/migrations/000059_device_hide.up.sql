-- Migration 000059: device hide (display-only)
--
-- "Hidden" is purely a display concern and is deliberately NOT the same thing
-- as "disabled" (migration 000006). A disabled device is rejected at agent
-- auth; a hidden device keeps connecting, keeps reporting metrics, keeps
-- firing alerts — it is simply filtered out of the default device list so
-- operators can park decommissioned-looking or noisy machines without losing
-- the data.
--
-- Because hiding is display-only, a hidden device un-hides itself the next time
-- its agent establishes a connection or re-enrolls (the auth/enroll UPDATEs
-- clear hidden_at). Ongoing traffic from an already-connected agent — heartbeats,
-- gRPC metrics, inventory — deliberately does NOT clear it, so a hide survives
-- for as long as the current session lasts.

-- Step 1: Timestamp of when the device was hidden. NULL = visible.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS hidden_at TIMESTAMPTZ;

-- Step 2: Who hid it, for the audit trail alongside audit_log. NULL when the
-- hide came from a non-user actor (e.g. the static API key).
ALTER TABLE devices ADD COLUMN IF NOT EXISTS hidden_by UUID REFERENCES users(id);

-- No index: the default listing filters on "hidden_at IS NULL", which a partial
-- index on the hidden rows cannot serve, and nothing queries "IS NOT NULL".

-- Step 3: Documentation.
COMMENT ON COLUMN devices.hidden_at IS 'When set, the device is filtered out of the default device list. Display-only: the agent still connects and reports. Cleared when the agent next authenticates a new connection or re-enrolls.';
COMMENT ON COLUMN devices.hidden_by IS 'User who hid the device; NULL when hidden by a non-user actor such as the static API key';
