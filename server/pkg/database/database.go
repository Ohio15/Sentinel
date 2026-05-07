package database

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
	"migrations/000001_initial_schema.sql",                              // v1
	"migrations/000002_enrollment_tokens.sql",                           // v2
	"migrations/000003_metrics_partitioning.sql",                        // v3
	"migrations/000004_inventory_schema.sql",                            // v4
	"migrations/000005_mobile_devices.sql",                              // v5
	"migrations/000006_device_management.sql",                           // v6
	"migrations/000007_agent_certificates.sql",                          // v7
	"migrations/000008_agent_logs.sql",                                  // v8
	"migrations/000009_fix_agent_logs_columns.sql",                      // v9
	"migrations/000010_extended_device_info.sql",                        // v10
	"migrations/000011_tickets.sql",                                     // v11
	"migrations/000012_agent_updates.sql",                               // v12
	"migrations/000013_grpc_dataplane.sql",                              // v13
	"migrations/000014_clients.sql",                                     // v14
	"migrations/000015_agent_cert_status.sql",                           // v15
	"migrations/000016_fix_os_types_column.sql",                         // v16
	"migrations/000017_preset_scripts.sql",                              // v17
	"migrations/000018_fix_device_updates.sql",                          // v18
	"migrations/000019_device_updates.sql",                              // v19
	"migrations/000020_extended_metrics.sql",                            // v20
	"migrations/000021_update_groups.sql",                               // v21
	"migrations/000022_staged_rollouts.sql",                             // v22
	"migrations/000023_command_queue.sql",                               // v23
	"migrations/000024_agent_health.sql",                                // v24
	"migrations/000025_portal_users.sql",                                // v25
	"migrations/000026_portal_branding.sql",                             // v26
	"migrations/000027_logo_sizing.sql",                                 // v27
	"migrations/000028_portal_enhancements.sql",                         // v28
	"migrations/000029_ticketing_enhancements.sql",                      // v29
	"migrations/000030_knowledge_base.sql",                              // v30
	"migrations/000031_username_and_invitations.sql",                    // v31
	"migrations/000032_token_hashing.sql",                               // v32
	"migrations/000033_agent_installation_links.sql",                    // v33
	"migrations/000034_organization_multi_tenant.sql",                   // v34
	"migrations/000035_installation_codes.sql",                          // v35
	"migrations/000036_client_certificates.sql",                         // v36
	"migrations/000037_session_persistence.sql",                         // v37
	"migrations/000038_webauthn_credentials.sql",                        // v38
	"migrations/000039_recordings.sql",                                  // v39
	"migrations/000040_power_management.sql",                            // v40
	"migrations/000041_webhooks.sql",                                    // v41
	"migrations/000042_patch_approvals.sql",                             // v42
	"migrations/000043_mfa_totp.sql",                                    // v43
	"migrations/000044_script_scheduling.sql",                           // v44
	"migrations/000045_usb_devices.sql",                                 // v45
	"migrations/000046_credential_management.sql",                       // v46
	"migrations/000047_usb_file_transfers.sql",                          // v47
	"migrations/000048_router_audit_log.sql",                            // v48
	"migrations/000049_security_hardening.sql",                          // v49
	"migrations/000050_test_center.sql",                                 // v50
	"migrations/000051_kill_token.sql",                                  // v51
	"migrations/000052_webhooks_fix_org_id_type.sql",                    // v52
	"migrations/000053_alert_rules_sla_policies_dedupe_uniqueness.sql",  // v53
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

// Migrate runs database migrations
func (db *DB) Migrate() error {
	ctx := context.Background()

	// Create migrations table if not exists
	_, err := db.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INT PRIMARY KEY,
			applied_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get current version
	var currentVersion int
	err = db.pool.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentVersion)
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	migrationFiles := migrationFilesRegistry

	// Run pending migrations
	for i := currentVersion; i < len(migrationFiles); i++ {
		schema, err := migrations.ReadFile(migrationFiles[i])
		if err != nil {
			// Migration file not found, skip
			continue
		}

		_, err = db.pool.Exec(ctx, string(schema))
		if err != nil {
			return fmt.Errorf("failed to apply migration %d: %w", i+1, err)
		}

		_, err = db.pool.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", i+1)
		if err != nil {
			return fmt.Errorf("failed to record migration %d: %w", i+1, err)
		}
	}

	return nil
}
