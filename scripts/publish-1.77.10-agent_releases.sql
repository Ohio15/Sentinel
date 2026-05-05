-- publish-1.77.10-agent_releases.sql
--
-- Phase 2 Stage 1 release-pipeline-gap fix (incident df7a7ff8).
--
-- WHEN TO RUN:
--   AFTER the v1.77.10 agent binaries have been placed in
--   ~/Sentinel/installers/sentinel-agent-{windows-amd64,windows-386,linux-amd64,linux-arm64}
--   AND installers/version.json has been reverted to "1.77.10" (the matching binary
--   version). Both conditions must hold before running this; otherwise Wave 1's gate
--   will release announcements for a version with no serveable artifact.
--
-- WHY:
--   scripts/release.ps1 has never inserted into agent_releases. The migration
--   003_agent_updates.sql seeded rows for v1.0.0 → v1.11.0 in 2024-12; nothing
--   has been written to the table since. The result: from v1.12.0 onward, every
--   release shipped without an agent_releases row, and Wave 1 (v1.77.15) made
--   that empty-table state the suppression trigger. Until this row exists, the
--   fleet is gated and cannot auto-upgrade.
--
-- IDEMPOTENT: yes — ON CONFLICT (version) DO NOTHING. Re-running has no effect
-- if the row already exists.
--
-- NOT a schema migration. Do NOT add to the migrations runner.
--
-- Audit trail: see PR opening "fix: publish v1.77.10 binaries + agent_releases row".

INSERT INTO agent_releases (version, release_date, changelog, is_required, platforms)
VALUES (
    '1.77.10',
    '2026-04-27 19:28:00+00',
    'Security patches: gRPC 1.64.0 -> 1.79.3 (CVE-2026-33186 auth bypass), pgx 5.5.2 -> 5.9.2 (CVE-2026-33816 SQL injection), oauth2 0.23.0 -> 0.34.0 (CVE-2025-22868), JWT 5.2.0 -> 5.2.2 (CVE-2025-30204), x/crypto 0.33.0 -> 0.46.0 (CVE-2025-22869), x/net 0.35.0 -> 0.48.0, redis 9.4.0 -> 9.6.3, Go toolchain go1.25.9 (resolves 9 stdlib advisories). Originally tagged 2026-04-27 but binaries were not published until 2026-05-05 as part of Phase 2 release-pipeline-gap fix.',
    false,
    ARRAY['windows', 'linux']
)
ON CONFLICT (version) DO NOTHING;

-- Verification (run separately to inspect impact):
--   SELECT version, release_date, platforms FROM agent_releases ORDER BY release_date DESC LIMIT 3;
--   -- Should now show 1.77.10 at the top.
--
--   SELECT version, last_seen FROM devices WHERE last_seen > NOW() - INTERVAL '5 minutes';
--   -- Watch agent_version field flip from 1.77.3/1.77.4 to 1.77.10 over the next several minutes.
