-- Migration: Test Center
-- Adds tables for automated test run tracking, individual test results, and aggregated issue management.

-- 1. test_runs: One row per execution of the test suite
CREATE TABLE IF NOT EXISTS test_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id INTEGER NOT NULL DEFAULT 1,
    project         VARCHAR(100) NOT NULL,              -- e.g. 'sentinel-agent', 'sentinel-server', 'sentinel-frontend'
    branch          VARCHAR(255) NOT NULL DEFAULT 'master',
    commit_sha      VARCHAR(64),
    trigger_type    VARCHAR(30)  NOT NULL DEFAULT 'cron', -- cron | manual | ci
    status          VARCHAR(20)  NOT NULL DEFAULT 'running', -- running | passed | failed | error
    total_tests     INTEGER NOT NULL DEFAULT 0,
    passed          INTEGER NOT NULL DEFAULT 0,
    failed          INTEGER NOT NULL DEFAULT 0,
    skipped         INTEGER NOT NULL DEFAULT 0,
    duration_ms     INTEGER,                             -- wall-clock time for the whole run
    environment     VARCHAR(50),                         -- e.g. 'production', 'staging', 'local'
    runner          VARCHAR(100),                        -- hostname or CI runner name
    summary         TEXT,                                -- free-form markdown summary
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_test_runs_org_project   ON test_runs(organization_id, project);
CREATE INDEX IF NOT EXISTS idx_test_runs_status        ON test_runs(status);
CREATE INDEX IF NOT EXISTS idx_test_runs_started_at    ON test_runs(started_at DESC);

-- 2. test_results: Individual test results within a run
CREATE TABLE IF NOT EXISTS test_results (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL REFERENCES test_runs(id) ON DELETE CASCADE,
    test_name       VARCHAR(500) NOT NULL,               -- fully qualified test name
    suite           VARCHAR(255),                        -- test suite / file grouping
    status          VARCHAR(20)  NOT NULL DEFAULT 'passed', -- passed | failed | skipped | error
    duration_ms     INTEGER,
    error_message   TEXT,
    stack_trace     TEXT,
    retry_count     INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_test_results_run_id     ON test_results(run_id);
CREATE INDEX IF NOT EXISTS idx_test_results_status     ON test_results(status) WHERE status != 'passed';
CREATE INDEX IF NOT EXISTS idx_test_results_test_name  ON test_results(test_name);

-- 3. test_issues: Aggregated issues tracked across runs
CREATE TABLE IF NOT EXISTS test_issues (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id INTEGER NOT NULL DEFAULT 1,
    project         VARCHAR(100) NOT NULL,
    test_name       VARCHAR(500) NOT NULL,               -- the failing test identifier
    title           VARCHAR(500) NOT NULL,               -- human-readable issue title
    status          VARCHAR(20)  NOT NULL DEFAULT 'open', -- open | acknowledged | resolved | wontfix
    severity        VARCHAR(20)  NOT NULL DEFAULT 'medium', -- critical | high | medium | low
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    occurrence_count INTEGER NOT NULL DEFAULT 1,
    first_run_id    UUID REFERENCES test_runs(id) ON DELETE SET NULL,
    last_run_id     UUID REFERENCES test_runs(id) ON DELETE SET NULL,
    assigned_to     UUID REFERENCES users(id) ON DELETE SET NULL,
    resolved_by     UUID REFERENCES users(id) ON DELETE SET NULL,
    resolved_at     TIMESTAMPTZ,
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_test_issues_org_project ON test_issues(organization_id, project);
CREATE INDEX IF NOT EXISTS idx_test_issues_status      ON test_issues(status);
CREATE INDEX IF NOT EXISTS idx_test_issues_severity    ON test_issues(severity);
CREATE INDEX IF NOT EXISTS idx_test_issues_test_name   ON test_issues(test_name);

-- Unique constraint: one open issue per test per project
CREATE UNIQUE INDEX IF NOT EXISTS idx_test_issues_unique_open
    ON test_issues(organization_id, project, test_name) WHERE status IN ('open', 'acknowledged');
