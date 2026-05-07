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
