package pki

import (
	"context"
	"strings"
	"testing"
)

// TestCheckIssuanceRate_EmptyAgentID confirms the empty-agentID guard.
// Caller should never pass empty; the helper refuses loudly rather than
// silently counting against an empty bucket (which would let anything
// without an agent_id evade the rate limit).
func TestCheckIssuanceRate_EmptyAgentID(t *testing.T) {
	p := &PKI{} // pool is nil but we never reach the query — short-circuit on empty agentID
	allowed, retryAfter, err := p.CheckIssuanceRate(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty agentID, got nil")
	}
	if !strings.Contains(err.Error(), "empty agentID") {
		t.Errorf("expected error to mention empty agentID, got: %v", err)
	}
	if allowed {
		t.Error("expected allowed=false on empty agentID")
	}
	if retryAfter != 0 {
		t.Errorf("expected retryAfter=0 on empty agentID, got %v", retryAfter)
	}
}

// TestCheckIssuanceRateTx_EmptyAgentID confirms the tx-variant guard too,
// since callers might mistakenly pass empty here and we want symmetric
// behavior with the pool-based form.
func TestCheckIssuanceRateTx_EmptyAgentID(t *testing.T) {
	allowed, retryAfter, err := CheckIssuanceRateTx(context.Background(), nil, "")
	if err == nil {
		t.Fatal("expected error for empty agentID, got nil")
	}
	if !strings.Contains(err.Error(), "empty agentID") {
		t.Errorf("expected error to mention empty agentID, got: %v", err)
	}
	if allowed {
		t.Error("expected allowed=false on empty agentID")
	}
	if retryAfter != 0 {
		t.Errorf("expected retryAfter=0 on empty agentID, got %v", retryAfter)
	}
}

// Note: full DB-integration tests for CheckIssuanceRate (5 cert rows -> 6th
// returns allowed=false with a real Retry-After) need a test postgres + a
// fixture that pre-populates client_certificates. Deferred to follow-up; the
// recert handler's existing TestParseCSR* coverage exercises the same SQL
// shape from the inside.
