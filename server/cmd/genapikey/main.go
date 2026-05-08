// genapikey — one-shot helper to bootstrap a managed admin API key.
//
// Reads DATABASE_URL from env, connects directly, calls APIKeyManager.CreateKey
// with permissions=["*"]. Prints the plaintext key to stdout (last time it's
// ever recoverable — bcrypt hash is what's stored).
//
// Usage:
//
//	DATABASE_URL=postgres://... ./genapikey "ops-bootstrap" "Initial admin key for sentinel-rollout CLI"
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sentinel/server/internal/credentials"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: genapikey <name> <description>")
		os.Exit(2)
	}
	name := os.Args[1]
	description := os.Args[2]

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "error: DATABASE_URL not set")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Look up an admin user to satisfy the credential_keys.created_by FK.
	// First admin in the users table is the operational owner; the FK is
	// audit metadata, not authorization (auth happens at validation time).
	var adminID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE role = 'admin' ORDER BY created_at LIMIT 1`).Scan(&adminID); err != nil {
		fmt.Fprintf(os.Stderr, "error: no admin user found: %v\n", err)
		os.Exit(1)
	}

	mgr := credentials.NewAPIKeyManager(pool, nil)
	key, err := mgr.CreateKey(ctx, credentials.CreateAPIKeyRequest{
		Name:        name,
		Description: description,
		Permissions: []string{"*"},
		CreatedBy:   adminID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: create: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(key.FullKey)
	fmt.Fprintf(os.Stderr, "Key %s created with full admin permissions. Save the value above — it is not recoverable.\n", key.ID)
}
