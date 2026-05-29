package pki

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// DefaultIssuanceHourlyLimit caps how many LIVE (un-revoked) client certs the
// server will issue for a single agent_id inside a rolling 1-hour window. The
// limit applies uniformly across re-cert and enrollment paths.
//
// 5/hour matches what handlers_recert.go enforces for the rotation path; the
// same number applies to enrollment-time issuance so an attacker who can spoof
// "I don't have a cert yet" repeatedly can't churn certs without bound.
//
// Counting only un-revoked certs means a legitimate rotation flow (which
// revokes the prior serial on every success) does not consume budget toward
// future issuance — the bound is on simultaneously-valid certs an agent can
// hold, not total issuance attempts.
const DefaultIssuanceHourlyLimit = 5

// ErrIssuanceRateLimited is returned by CheckIssuanceRate when the agent has
// already accumulated DefaultIssuanceHourlyLimit live certs in the last hour.
// Callers should NOT issue a new cert and should propagate / log accordingly.
// Use errors.Is to test.
var ErrIssuanceRateLimited = errors.New("pki: cert issuance rate limit exceeded for agent")

// CheckIssuanceRate returns (allowed, retryAfter, err) for the supplied agent.
//
// When allowed=false, retryAfter is the wall-clock duration until the oldest
// live cert in the rolling window ages out (i.e., until the agent's budget
// frees up by 1). A 1-hour fallback is returned if the oldest-issued lookup
// itself fails.
//
// Errors from the DB are returned (not silently swallowed) so callers can
// fail-closed under DB outages — the same pattern handlers_recert.go uses
// for revocation check failures.
//
// Inside a transaction? Pass the tx as a [pgx.Tx]; outside, pass a *pgxpool.Pool.
// Both satisfy the small subset of pgx.Conn surface used here.
func (p *PKI) CheckIssuanceRate(ctx context.Context, agentID string) (bool, time.Duration, error) {
	if agentID == "" {
		// Empty agent_id is a misuse — refuse loudly rather than silently
		// counting against an empty bucket.
		return false, 0, errors.New("pki: CheckIssuanceRate called with empty agentID")
	}
	return checkIssuanceRateOnConn(ctx, queryRowAdapter{exec: p.db.QueryRow}, agentID)
}

// CheckIssuanceRateTx is the explicit-transaction variant: callers that have
// already opened a tx (e.g., the re-cert handler holding a SELECT FOR UPDATE
// on the device row) should use this form so the rate-limit lookup sees the
// transaction's snapshot and serializes consistently with surrounding writes.
func CheckIssuanceRateTx(ctx context.Context, tx pgx.Tx, agentID string) (bool, time.Duration, error) {
	if agentID == "" {
		return false, 0, errors.New("pki: CheckIssuanceRateTx called with empty agentID")
	}
	return checkIssuanceRateOnConn(ctx, queryRowAdapter{exec: tx.QueryRow}, agentID)
}

// queryRowAdapter abstracts the QueryRow method shared by *pgxpool.Pool and
// pgx.Tx so the core rate-limit logic stays in one place. We only need
// QueryRow — no Exec, no Begin, nothing else.
type queryRowAdapter struct {
	exec func(ctx context.Context, sql string, args ...any) pgx.Row
}

func checkIssuanceRateOnConn(ctx context.Context, q queryRowAdapter, agentID string) (bool, time.Duration, error) {
	var recentCount int
	err := q.exec(ctx, `
		SELECT COUNT(*) FROM client_certificates
		WHERE agent_id = $1
		  AND issued_at > NOW() - INTERVAL '1 hour'
		  AND revoked_at IS NULL
	`, agentID).Scan(&recentCount)
	if err != nil {
		return false, 0, fmt.Errorf("pki: rate limit lookup failed for agent=%s: %w", agentID, err)
	}
	if recentCount < DefaultIssuanceHourlyLimit {
		return true, 0, nil
	}

	// Over limit — compute remaining time until the oldest live cert in the
	// window ages out. Falls back to a full hour on a secondary lookup
	// failure (logged at caller; we don't want the rate-limit response to
	// itself be unrecoverable if we just can't compute a precise Retry-After).
	retryAfter := time.Hour
	var oldestIssued time.Time
	if err := q.exec(ctx, `
		SELECT MIN(issued_at) FROM client_certificates
		WHERE agent_id = $1
		  AND issued_at > NOW() - INTERVAL '1 hour'
		  AND revoked_at IS NULL
	`, agentID).Scan(&oldestIssued); err == nil {
		if remaining := time.Until(oldestIssued.Add(time.Hour)); remaining > 0 {
			retryAfter = remaining
		}
	}
	return false, retryAfter, nil
}
