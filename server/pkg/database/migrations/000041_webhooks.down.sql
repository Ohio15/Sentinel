-- Reverse 000041_webhooks.
-- Dropped in reverse dependency order: webhook_deliveries holds an FK to
-- webhooks, so it must go first.
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhooks;
