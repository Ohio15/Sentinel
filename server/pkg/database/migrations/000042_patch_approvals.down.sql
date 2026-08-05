-- Reverse 000042_patch_approvals.
-- Dropped in reverse dependency order: device_patch_assignments references
-- patch_policies, so the referencing tables go before the referenced one.
DROP TABLE IF EXISTS patch_installations;
DROP TABLE IF EXISTS device_patch_assignments;
DROP TABLE IF EXISTS patch_approvals;
DROP TABLE IF EXISTS patch_policies;
