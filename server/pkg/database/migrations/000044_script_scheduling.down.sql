-- Reverse 000044_script_scheduling.
-- Dropped in reverse dependency order: script_executions references
-- script_schedules(id), so it must go first.
DROP TABLE IF EXISTS script_executions;
DROP TABLE IF EXISTS script_schedules;
