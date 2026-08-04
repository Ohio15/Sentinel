-- +migrate Up
-- Webhook configurations for alert and event notifications

CREATE TABLE IF NOT EXISTS webhooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- INTEGER to match organizations.id (SERIAL, 000034). Was UUID, which made
    -- this CREATE TABLE fail outright on any fresh database: PostgreSQL rejects
    -- the inline FK with "Key columns organization_id and id are of incompatible
    -- types: uuid and integer". 000052 already fixed the identical mistake in
    -- webhooks forward-only for existing deployments; that repair stays, and
    -- this in-place correction is what lets a FRESH database get past here.
    organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    url TEXT NOT NULL,
    secret TEXT, -- HMAC signing secret
    events TEXT[] NOT NULL DEFAULT '{}', -- Events to subscribe to: alert.created, alert.resolved, device.online, device.offline
    headers JSONB DEFAULT '{}', -- Additional custom headers
    is_enabled BOOLEAN NOT NULL DEFAULT true,
    last_triggered_at TIMESTAMP WITH TIME ZONE,
    last_status VARCHAR(50), -- success, failed
    last_error TEXT,
    failure_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);

CREATE INDEX idx_webhooks_organization ON webhooks(organization_id);
CREATE INDEX idx_webhooks_enabled ON webhooks(is_enabled) WHERE is_enabled = true;

-- Webhook delivery log for debugging
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id UUID NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    response_status INTEGER,
    response_body TEXT,
    duration_ms INTEGER,
    success BOOLEAN NOT NULL,
    error TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_webhook_deliveries_webhook ON webhook_deliveries(webhook_id);
CREATE INDEX idx_webhook_deliveries_created ON webhook_deliveries(created_at);

-- Retention sweeps over old deliveries are served by
-- idx_webhook_deliveries_created above.
--
-- There used to be a partial index here with the predicate
--   WHERE created_at < NOW() - INTERVAL '30 days'
-- which PostgreSQL rejects outright ("functions in index predicate must be
-- marked IMMUTABLE"), aborting the migration on every fresh database. NOW() is
-- STABLE, not IMMUTABLE. The predicate was also meaningless even if it had been
-- accepted: an index predicate is evaluated per row at write time against a
-- fixed expression, so it would have frozen a moving 30-day window at whatever
-- instant the index was built. The plain created_at index is the correct tool.

-- +migrate Down
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhooks;
