// sentinel-audit-backfill — one-shot helper that infers historical device-row
// deletions from orphaned cert records and writes them into audit_log.
//
// Why this exists:
//
// The audit_log table existed since the beginning but was never wired to the
// deleteDevice handler (we wired that in the same PR that introduces this
// tool). That means there is no trail for any device row removed prior to the
// audit instrumentation landing — investigators staring at audit_log see an
// empty table and have no way to reconstruct who/what deleted a missing
// device.
//
// Approximation: for every row in client_certificates that is NOT revoked AND
// whose serial does NOT appear in any current devices.client_cert_serial, the
// only way that pairing came to exist is that a device row used to exist
// (since IssueClientCertificate atomically writes both a client_certificates
// row AND updates devices.client_cert_serial — see internal/pki/pki.go) and
// has since been deleted without the matching cert being revoked. We INSERT
// a 'device_delete_inferred' audit row capturing what we can infer.
//
// Idempotent: ON CONFLICT DO NOTHING via a unique partial index on
// (details->>'cert_serial') for action='device_delete_inferred'. The
// expression and predicate in the ON CONFLICT target MUST exactly match the
// index definition below — Postgres pairs partial-index conflict targets by
// textual equality of the predicate expression. The current pairing is:
//
//	index:    ON audit_log ((details->>'cert_serial')) WHERE action = 'device_delete_inferred'
//	conflict: ON CONFLICT ((details->>'cert_serial')) WHERE action = 'device_delete_inferred'
//
// To verify the pairing on a real DB:
//  1. Populate client_certificates with N orphaned rows.
//  2. Run the tool once: expect inserted=N, skipped_duplicate=0.
//  3. Run it again with no DB changes: expect inserted=0, skipped_duplicate=N.
//
// Usage:
//
//	DATABASE_URL=postgres://... sentinel-audit-backfill [--dry-run]
//
// Default mode runs the inserts. --dry-run prints what would be inserted.
//
// Exit codes:
//   - 0  : completed (even if some per-row inserts failed; counts are reported).
//   - 2  : DATABASE_URL not set.
//   - 3  : fatal setup error (cannot connect, cannot create index, cannot run query).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "print the inferred deletions without writing")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL not set")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Printf("pgxpool.New: %v", err)
		os.Exit(3)
	}
	defer pool.Close()

	// Idempotency index — created once, cheap to retry. We key on the cert
	// serial extracted from details JSONB to ensure a second run doesn't
	// double-insert the same orphan. Predicate MUST match the ON CONFLICT
	// predicate in the INSERT below exactly.
	if _, err := pool.Exec(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS idx_audit_log_inferred_cert_serial
		ON audit_log ((details->>'cert_serial'))
		WHERE action = 'device_delete_inferred'
	`); err != nil {
		log.Printf("create idempotency index: %v", err)
		os.Exit(3)
	}

	// Query orphaned certs. We carry organization_id, device_id, agent_id,
	// and issued_at so the audit row can preserve as much pre-delete context
	// as exists. The query is intentionally conservative — we only consider
	// certs that were never revoked, since a revoked-then-device-deleted flow
	// is a different (and intentional) story.
	rows, err := pool.Query(ctx, `
		SELECT cc.serial_number, cc.agent_id, cc.device_id, cc.organization_id,
		       cc.issued_at, cc.expires_at, cc.fingerprint
		FROM client_certificates cc
		WHERE cc.revoked_at IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM devices d
		      WHERE d.client_cert_serial = cc.serial_number
		  )
		ORDER BY cc.issued_at
	`)
	if err != nil {
		log.Printf("query orphans: %v", err)
		os.Exit(3)
	}
	defer rows.Close()

	var (
		considered int
		inserted   int
		skipped    int
		failed     int
	)
	for rows.Next() {
		var (
			serial      string
			agentID     string
			deviceID    *uuid.UUID
			orgID       int
			issuedAt    time.Time
			expiresAt   time.Time
			fingerprint string
		)
		if err := rows.Scan(&serial, &agentID, &deviceID, &orgID, &issuedAt, &expiresAt, &fingerprint); err != nil {
			log.Printf("scan row: %v (skipping)", err)
			failed++
			continue
		}
		considered++

		// Build details payload via json.Marshal so we never have to think about
		// string escaping for agent IDs, serials, or fingerprints. Keys mirror
		// what the live deleteDevice handler writes so downstream consumers can
		// treat both event types uniformly.
		detailsMap := map[string]any{
			"cert_serial":      serial,
			"agent_id":         agentID,
			"cert_issued_at":   issuedAt.UTC().Format(time.RFC3339),
			"cert_expires_at":  expiresAt.UTC().Format(time.RFC3339),
			"fingerprint":      fingerprint,
			"inferred_reason":  "cert exists in client_certificates with no matching devices.client_cert_serial; device row presumed deleted before audit instrumentation landed",
		}
		if deviceID != nil {
			detailsMap["device_id"] = deviceID.String()
		} else {
			detailsMap["device_id"] = nil
		}

		detailsJSON, err := json.Marshal(detailsMap)
		if err != nil {
			// Should be impossible for the static shape above, but if it
			// happens log and skip rather than tear down the whole batch.
			log.Printf("marshal details for serial=%s: %v (skipping)", serial, err)
			failed++
			continue
		}

		if *dryRun {
			fmt.Printf("would insert: serial=%s agent=%s org=%d issued=%s\n",
				serial, agentID, orgID, issuedAt.Format(time.RFC3339))
			continue
		}

		// Use the cert's issued_at as the audit created_at so the timeline
		// reflects when the device existed, not when this backfill ran.
		// ON CONFLICT skip thanks to the partial unique index. The predicate
		// here MUST be byte-identical to the one in the CREATE INDEX above
		// or Postgres will treat the index as not matching the conflict target.
		tag, err := pool.Exec(ctx, `
			INSERT INTO audit_log (
			    user_id, action, resource_type, resource_id, details,
			    ip_address, user_agent, severity, organization_id, created_at
			) VALUES (
			    NULL, 'device_delete_inferred', 'device',
			    $1::uuid,
			    $2::jsonb,
			    NULL, 'sentinel-audit-backfill', 'warning',
			    $3, $4
			)
			ON CONFLICT ((details->>'cert_serial')) WHERE action = 'device_delete_inferred'
			DO NOTHING
		`, deviceID, detailsJSON, orgID, issuedAt)
		if err != nil {
			// One poisoned row must not halt the batch. Investigators can
			// re-run after fixing the underlying cause; the unique index
			// guarantees inserted rows are not duplicated on retry.
			log.Printf("insert audit row for serial=%s: %v (skipping)", serial, err)
			failed++
			continue
		}
		if tag.RowsAffected() == 0 {
			skipped++
		} else {
			inserted++
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration: %v", err)
		os.Exit(3)
	}

	mode := "applied"
	if *dryRun {
		mode = "dry-run"
	}
	fmt.Printf("backfill complete (%s): considered=%d inserted=%d skipped_duplicate=%d failed=%d\n",
		mode, considered, inserted, skipped, failed)
}
