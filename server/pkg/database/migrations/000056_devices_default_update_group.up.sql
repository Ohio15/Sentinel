-- 000056_devices_default_update_group.up.sql
--
-- Phase 6 prep: assign every existing device to the 'production' update group
-- so the staged-rollout pipeline (added in 000055) has a populated targeting
-- surface from day one. Devices created before this migration had
-- update_group_id = NULL because the assignment endpoint didn't exist.
--
-- The 'production' group was seeded by migration 000021. Choosing it as the
-- default (vs 'pilot' or 'test') is the conservative call — it has the
-- lowest auto_promote rate and the highest min_devices_for_decision, so
-- backfilled devices get the slowest, safest rollout path until an admin
-- explicitly cohorts them differently.
--
-- Idempotent: only updates rows where update_group_id IS NULL.
UPDATE devices
SET update_group_id = (SELECT id FROM update_groups WHERE name = 'production' LIMIT 1)
WHERE update_group_id IS NULL
  AND EXISTS (SELECT 1 FROM update_groups WHERE name = 'production');
