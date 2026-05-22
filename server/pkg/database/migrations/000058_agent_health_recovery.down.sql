DROP INDEX IF EXISTS idx_recovery_actions_created;
DROP INDEX IF EXISTS idx_recovery_actions_agent;
DROP TABLE IF EXISTS agent_recovery_actions;

DROP INDEX IF EXISTS idx_agent_health_status;
DROP INDEX IF EXISTS idx_agent_health_last_checkin;

ALTER TABLE agent_health
    DROP COLUMN IF EXISTS raw_payload,
    DROP COLUMN IF EXISTS mtls_cert_present,
    DROP COLUMN IF EXISTS layer5_kill_token_present,
    DROP COLUMN IF EXISTS layer4_schtask_present,
    DROP COLUMN IF EXISTS layer3_watchdog_poll_ok,
    DROP COLUMN IF EXISTS layer2_last_poll_ok,
    DROP COLUMN IF EXISTS layer1_ws_uptime_secs,
    DROP COLUMN IF EXISTS device_id;
