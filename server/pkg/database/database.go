package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// stdDBFromPool exposes a pgxpool.Pool as a *sql.DB so golang-migrate's
// pgx/v5 driver (which takes *sql.DB via WithInstance) can wrap it without
// opening a second connection. Closing the returned *sql.DB does NOT close
// the underlying pool — that's owned by the application.
func stdDBFromPool(pool *pgxpool.Pool) *sql.DB {
	return stdlib.OpenDBFromPool(pool)
}

//go:embed migrations/*.sql
var migrations embed.FS

// migrationFilesRegistry is the canonical list of migrations in version order.
// Position == version - 1. Filenames carry their canonical version number as
// a 6-digit prefix; the slice index is enforced to match that prefix by a CI
// guard test (see migrations_test.go). Renamed in v1.77.24 from semantic-prefix
// names to canonical-version names to remove the filename↔version drift that
// produced the dead-letter migration class of bug.
//
// Phase 3 of the golang-migrate cutover will retire this slice; the file
// system order alone will determine application order. Until then, this slice
// is the authoritative registry and the test asserts it stays in sync with
// the embedded directory.
var migrationFilesRegistry = []string{
	"migrations/000001_initial_schema.up.sql",                              // v1
	"migrations/000002_enrollment_tokens.up.sql",                           // v2
	"migrations/000003_metrics_partitioning.up.sql",                        // v3
	"migrations/000004_inventory_schema.up.sql",                            // v4
	"migrations/000005_mobile_devices.up.sql",                              // v5
	"migrations/000006_device_management.up.sql",                           // v6
	"migrations/000007_agent_certificates.up.sql",                          // v7
	"migrations/000008_agent_logs.up.sql",                                  // v8
	"migrations/000009_fix_agent_logs_columns.up.sql",                      // v9
	"migrations/000010_extended_device_info.up.sql",                        // v10
	"migrations/000011_tickets.up.sql",                                     // v11
	"migrations/000012_agent_updates.up.sql",                               // v12
	"migrations/000013_grpc_dataplane.up.sql",                              // v13
	"migrations/000014_clients.up.sql",                                     // v14
	"migrations/000015_agent_cert_status.up.sql",                           // v15
	"migrations/000016_fix_os_types_column.up.sql",                         // v16
	"migrations/000017_preset_scripts.up.sql",                              // v17
	"migrations/000018_fix_device_updates.up.sql",                          // v18
	"migrations/000019_device_updates.up.sql",                              // v19
	"migrations/000020_extended_metrics.up.sql",                            // v20
	"migrations/000021_update_groups.up.sql",                               // v21
	"migrations/000022_staged_rollouts.up.sql",                             // v22
	"migrations/000023_command_queue.up.sql",                               // v23
	"migrations/000024_agent_health.up.sql",                                // v24
	"migrations/000025_portal_users.up.sql",                                // v25
	"migrations/000026_portal_branding.up.sql",                             // v26
	"migrations/000027_logo_sizing.up.sql",                                 // v27
	"migrations/000028_portal_enhancements.up.sql",                         // v28
	"migrations/000029_ticketing_enhancements.up.sql",                      // v29
	"migrations/000030_knowledge_base.up.sql",                              // v30
	"migrations/000031_username_and_invitations.up.sql",                    // v31
	"migrations/000032_token_hashing.up.sql",                               // v32
	"migrations/000033_agent_installation_links.up.sql",                    // v33
	"migrations/000034_organization_multi_tenant.up.sql",                   // v34
	"migrations/000035_installation_codes.up.sql",                          // v35
	"migrations/000036_client_certificates.up.sql",                         // v36
	"migrations/000037_session_persistence.up.sql",                         // v37
	"migrations/000038_webauthn_credentials.up.sql",                        // v38
	"migrations/000039_recordings.up.sql",                                  // v39
	"migrations/000040_power_management.up.sql",                            // v40
	"migrations/000041_webhooks.up.sql",                                    // v41
	"migrations/000042_patch_approvals.up.sql",                             // v42
	"migrations/000043_mfa_totp.up.sql",                                    // v43
	"migrations/000044_script_scheduling.up.sql",                           // v44
	"migrations/000045_usb_devices.up.sql",                                 // v45
	"migrations/000046_credential_management.up.sql",                       // v46
	"migrations/000047_usb_file_transfers.up.sql",                          // v47
	"migrations/000048_router_audit_log.up.sql",                            // v48
	"migrations/000049_security_hardening.up.sql",                          // v49
	"migrations/000050_test_center.up.sql",                                 // v50
	"migrations/000051_kill_token.up.sql",                                  // v51
	"migrations/000052_webhooks_fix_org_id_type.up.sql",                    // v52
	"migrations/000053_alert_rules_sla_policies_dedupe_uniqueness.up.sql",  // v53
}

// registeredMigrationFilesForTest exposes the registry to tests in this
// package only. Production code reads migrationFilesRegistry directly.
func registeredMigrationFilesForTest() []string {
	out := make([]string, len(migrationFilesRegistry))
	copy(out, migrationFilesRegistry)
	return out
}

// Config holds database connection configuration
type Config struct {
	URL      string
	MaxConns int32
	MinConns int32
}

// DB represents the database connection
type DB struct {
	pool *pgxpool.Pool
}

// Database is an alias for DB for compatibility
type Database = DB

// New creates a new database connection with default settings
func New(databaseURL string) (*DB, error) {
	return NewWithConfig(&Config{
		URL:      databaseURL,
		MaxConns: 25,
		MinConns: 5,
	})
}

// NewWithConfig creates a new database connection with custom configuration
func NewWithConfig(cfg *Config) (*DB, error) {
	config, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	// Apply configuration
	if cfg.MaxConns > 0 {
		config.MaxConns = cfg.MaxConns
	} else {
		config.MaxConns = 25
	}
	if cfg.MinConns > 0 {
		config.MinConns = cfg.MinConns
	} else {
		config.MinConns = 5
	}
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{pool: pool}, nil
}

// Pool returns the underlying connection pool
func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

// AsDB returns the DB as itself (for compatibility with handler wrappers)
func (db *DB) AsDB() *DB {
	return db
}

// Close closes the database connection
func (db *DB) Close() {
	db.pool.Close()
}

// Migrate runs database migrations using golang-migrate.
//
// Phase 3 of the golang-migrate cutover (v1.77.26). Replaces the previous
// hand-maintained slice runner with golang-migrate's filesystem-driven
// runner. Filenames in migrations/ are the canonical version source
// (000001_… through 000NNN_…); see migrations_test.go for the invariant
// guard.
//
// On first run against a database that was previously managed by the
// legacy runner (schema_migrations.applied_at column present), an
// idempotent bootstrap renames the legacy tracking table to
// schema_migrations_legacy and creates a fresh schema_migrations with
// golang-migrate's expected (version BIGINT, dirty BOOLEAN) shape, seeded
// with the legacy table's MAX(version). The legacy table is retained
// indefinitely for forensic value (53 rows of applied_at timestamps).
//
// On a fresh database, the bootstrap is a no-op (legacy column absent)
// and golang-migrate creates schema_migrations itself.
func (db *DB) Migrate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := db.bootstrapLegacySchemaMigrations(ctx); err != nil {
		return fmt.Errorf("bootstrap legacy schema_migrations: %w", err)
	}

	src, err := iofs.New(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("open embedded migrations source: %w", err)
	}

	// golang-migrate's pgx/v5 driver wraps an existing pool's underlying
	// *pgx.Conn; we hand it a fresh connection from the pool's config.
	driver, err := pgxmigrate.WithInstance(stdDBFromPool(db.pool), &pgxmigrate.Config{
		MigrationsTable: "schema_migrations",
	})
	if err != nil {
		return fmt.Errorf("init pgx migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		return fmt.Errorf("init migrate instance: %w", err)
	}
	defer m.Close()

	switch err := m.Up(); {
	case errors.Is(err, migrate.ErrNoChange):
		v, _, _ := m.Version()
		log.Printf("[migrate] no change; current version %d", v)
		return nil
	case err != nil:
		return fmt.Errorf("apply migrations: %w", err)
	default:
		v, _, _ := m.Version()
		log.Printf("[migrate] applied; current version %d", v)
		return nil
	}
}

// bootstrapLegacySchemaMigrations transitions a legacy-managed
// schema_migrations table to golang-migrate's expected shape exactly once.
// The DO block is idempotent: it inspects information_schema and only
// fires when the legacy `applied_at` column is present and the
// golang-migrate `dirty` column is absent.
func (db *DB) bootstrapLegacySchemaMigrations(ctx context.Context) error {
	const stmt = `
DO $$
DECLARE
    legacy_max INT;
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'schema_migrations'
          AND column_name = 'applied_at'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'schema_migrations'
          AND column_name = 'dirty'
    ) THEN
        SELECT COALESCE(MAX(version), 0) INTO legacy_max FROM schema_migrations;
        ALTER TABLE schema_migrations RENAME TO schema_migrations_legacy;
        CREATE TABLE schema_migrations (
            version BIGINT NOT NULL PRIMARY KEY,
            dirty   BOOLEAN NOT NULL
        );
        IF legacy_max > 0 THEN
            INSERT INTO schema_migrations (version, dirty) VALUES (legacy_max, FALSE);
        END IF;
        RAISE NOTICE 'schema_migrations bootstrapped from legacy at version %', legacy_max;
    END IF;
END $$;
`
	if _, err := db.pool.Exec(ctx, stmt); err != nil {
		return err
	}
	return nil
}
