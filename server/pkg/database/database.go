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

	// Available migrations
	migrationFiles := []string{
		"migrations/001_initial_schema.sql",
		"migrations/002_enrollment_tokens.sql",
		"migrations/003_metrics_partitioning.sql",
		"migrations/004_inventory_schema.sql",
		"migrations/005_mobile_devices.sql",
		"migrations/006_device_management.sql",
		"migrations/007_agent_certificates.sql",
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
