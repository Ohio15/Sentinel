-- 000054_idx_devices_last_seen.up.sql
--
-- Phase 4 smoke migration for the golang-migrate cutover (v1.77.28).
-- Real, useful work: adds an index on devices.last_seen DESC to support
-- fleet queries that order or filter by recency (e.g. "show offline
-- devices oldest first", "list 10 most-recently-seen agents"). The
-- index is currently absent and these queries seq-scan today.
--
-- Safe to apply on a live database: CREATE INDEX (without CONCURRENTLY)
-- briefly takes a SHARE lock against devices, but the table is small
-- (low hundreds of rows in production) so the lock window is sub-second.
-- IF NOT EXISTS makes the migration idempotent.
CREATE INDEX IF NOT EXISTS idx_devices_last_seen
    ON devices (last_seen DESC NULLS LAST);
