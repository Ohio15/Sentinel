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

	// Available migrations (position = version - 1)
	migrationFiles := []string{
		"migrations/001_initial_schema.sql",          // v1
		"migrations/002_enrollment_tokens.sql",       // v2
		"migrations/003_metrics_partitioning.sql",    // v3
		"migrations/004_inventory_schema.sql",        // v4
		"migrations/005_mobile_devices.sql",          // v5
		"migrations/006_device_management.sql",       // v6
		"migrations/007_agent_certificates.sql",      // v7
		"migrations/008_agent_logs.sql",              // v8
		"migrations/009_fix_agent_logs_columns.sql",  // v9
		"migrations/002_extended_device_info.sql",    // v10
		"migrations/002b_tickets.sql",                // v11
		"migrations/003_agent_updates.sql",           // v12
		"migrations/004_grpc_dataplane.sql",          // v13
		"migrations/005_clients.sql",                 // v14
		"migrations/006_agent_cert_status.sql",       // v15
		"migrations/006b_fix_os_types_column.sql",    // v16
		"migrations/007_preset_scripts.sql",          // v17
		"migrations/007b_fix_device_updates.sql",     // v18
		"migrations/008_device_updates.sql",          // v19
		"migrations/009_extended_metrics.sql",        // v20
		"migrations/010_update_groups.sql",           // v21
		"migrations/011_staged_rollouts.sql",         // v22
		"migrations/012_command_queue.sql",           // v23
		"migrations/013_agent_health.sql",            // v24
		"migrations/014_portal_users.sql",            // v25
		"migrations/015_portal_branding.sql",         // v26
		"migrations/016_logo_sizing.sql",             // v27
		"migrations/017_portal_enhancements.sql",     // v28
		"migrations/018_ticketing_enhancements.sql",  // v29
		"migrations/019_knowledge_base.sql",          // v30
		"migrations/020_username_and_invitations.sql", // v31
		"migrations/021_token_hashing.sql",           // v32
		"migrations/022_agent_installation_links.sql", // v33
		"migrations/023_organization_multi_tenant.sql", // v34
		"migrations/024_installation_codes.sql",      // v35
		"migrations/025_client_certificates.sql",     // v36
		"migrations/026_session_persistence.sql",     // v37
		"migrations/027_webauthn_credentials.sql",    // v38
		"migrations/028_recordings.sql",              // v39
		"migrations/029_power_management.sql",        // v40
		"migrations/030_webhooks.sql",                // v41
		"migrations/031_patch_approvals.sql",         // v42
		"migrations/032_mfa_totp.sql",                // v43
		"migrations/033_script_scheduling.sql",       // v44
		"migrations/034_usb_devices.sql",             // v45
		"migrations/035_credential_management.sql",   // v46
		"migrations/036_usb_file_transfers.sql",      // v47
		"migrations/037_router_audit_log.sql",        // v48
		"migrations/038_security_hardening.sql",      // v49
		"migrations/039_test_center.sql",             // v50
		"migrations/040_kill_token.sql",              // v51
		"migrations/041_webhooks_fix_org_id_type.sql", // v52
		"migrations/042_alert_rules_sla_policies_dedupe_uniqueness.sql", // v53
	}

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
