-- cleanup-401-failure-suppression.sql
--
-- One-off post-deploy data cleanup for the v1.77.15 Wave 1 hotfix
-- (incident shared-brain memory df7a7ff8).
--
-- WHEN TO RUN:
--   AFTER the v1.77.15 deploy lands and sentinel-backend is healthy.
--   The new release-status gate will already be live by then; this query
--   only clears the historical state that would otherwise mask the fix.
--
-- WHY:
--   The heartbeat handler suppresses updateAvailable=true for any agent
--   with a 'failed' update row in the past 30 minutes (handlers.go
--   hasRecentUpdateFailure). Until those rows age out, agents that just
--   re-evaluated under the new gate look identical to agents that
--   would-still-have-the-401 — making it impossible to confirm the fix
--   actually worked. Marking pre-fix 401 failures as 'superseded' clears
--   the suppression and lets the next heartbeat be the truthful
--   observation point.
--
-- IDEMPOTENT: yes — re-running is a no-op (only matches rows that haven't
-- already been superseded, and the time window is bounded to 30 minutes).
--
-- NOT a schema migration. Do NOT add to the migrations runner.

UPDATE agent_updates
   SET status = 'superseded',
       updated_at = NOW()
 WHERE status = 'failed'
   AND error_message LIKE '%401%'
   AND created_at > NOW() - INTERVAL '30 minutes';

-- Verification (run separately to inspect impact):
--   SELECT COUNT(*) AS superseded_rows
--     FROM agent_updates
--    WHERE status = 'superseded'
--      AND error_message LIKE '%401%'
--      AND updated_at > NOW() - INTERVAL '5 minutes';
