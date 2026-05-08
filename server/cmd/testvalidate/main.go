// One-shot diagnostic for APIKeyManager.ValidateKey path. Reads a key from
// stdin, walks the same SELECT + bcrypt comparison the production middleware
// uses, prints each step's outcome. Disposable diagnostic — delete after the
// auth bug is closed.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: testvalidate <full-key>")
		os.Exit(2)
	}
	full := os.Args[1]

	dbURL := os.Getenv("DATABASE_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	prefix := full[:16]
	fmt.Printf("prefix: %q\n", prefix)

	var (
		id          string
		bcryptHash  []byte
		permsBytes  []byte
		ipBytes     []byte
		status      string
	)
	err = pool.QueryRow(ctx, `
		SELECT id, key_value_encrypted, permissions, ip_allowlist, status
		FROM credential_keys
		WHERE credential_type = 'api_key'
		  AND key_hash = $1
	`, prefix).Scan(&id, &bcryptHash, &permsBytes, &ipBytes, &status)
	if err != nil {
		fmt.Printf("select error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("found id=%s status=%s\n", id, status)
	fmt.Printf("bcryptHash bytes (len=%d, first 30 hex): %x\n", len(bcryptHash), bcryptHash[:min(30, len(bcryptHash))])
	fmt.Printf("bcryptHash as string: %q\n", string(bcryptHash))
	fmt.Printf("permissions raw: %s\n", permsBytes)

	var perms []string
	if err := json.Unmarshal(permsBytes, &perms); err != nil {
		fmt.Printf("permissions unmarshal error: %v\n", err)
	} else {
		fmt.Printf("permissions parsed: %v\n", perms)
	}

	if err := bcrypt.CompareHashAndPassword(bcryptHash, []byte(full)); err != nil {
		fmt.Printf("BCRYPT FAIL: %v\n", err)
	} else {
		fmt.Println("BCRYPT OK")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
