package api

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// agentTokenAuthWithCert counts agent WebSocket authentications where the agent
// presented its enrollment token over the tunnel *despite* already holding an
// active mTLS client certificate. This is the core observability signal for the
// token→cert binding hardening: in WARN mode it lets us enumerate exactly which
// agents would be disconnected once ENFORCE_AGENT_CERT_BINDING is flipped on.
//
// Labels:
//   - agent_id: the hardware-fingerprint agent identity (bounded by fleet size,
//     consistent with the other per-agent gauges in metrics.go).
//   - mode: "warn" when the connection was allowed, "enforced" when it was
//     rejected because enforcement is active.
var agentTokenAuthWithCert = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "sentinel_agent_token_auth_with_cert_total",
		Help: "Agent WS authentications made with the enrollment token while the agent already holds an active mTLS client certificate. Labeled by agent_id and mode (warn|enforced).",
	},
	[]string{"agent_id", "mode"},
)

// certBindingDecision is the outcome of the token→cert binding check for a single
// agent WebSocket authentication. It is produced by decideCertBinding, which is a
// pure function so the warn-vs-enforce branching can be unit-tested without a
// live database (ValidateDatabaseToken / the cert lookup both require a concrete
// *pgxpool.Pool that cannot be easily mocked — see decideCertBinding tests).
type certBindingDecision struct {
	HoldsCert bool // agent has an active, non-revoked, unexpired client certificate
	Enforced  bool // ENFORCE_AGENT_CERT_BINDING is on
	Reject    bool // the connection must be rejected (only when HoldsCert && Enforced)
}

// decideCertBinding computes the binding decision from the two inputs that matter:
// whether the agent holds a usable client cert, and whether enforcement is on.
//
// Truth table:
//   holdsCert=false            -> allow, no signal (agent legitimately token-only)
//   holdsCert=true, enforce=F  -> allow, WARN + metric(mode=warn)   [OBSERVE]
//   holdsCert=true, enforce=T  -> REJECT, WARN + metric(mode=enforced)
//
// TODO(phase-2): proof-of-possession-over-tunnel. Instead of a binary
// allow/reject when holdsCert && enforce, the server should issue a challenge and
// require the agent to sign it with the private key bound to its client cert,
// proving possession even though CF terminates TLS. That challenge/verify step
// would slot in here (or immediately after this decision) and, on success,
// upgrade the connection to cert-equivalent trust instead of rejecting it.
func decideCertBinding(holdsCert, enforce bool) certBindingDecision {
	return certBindingDecision{
		HoldsCert: holdsCert,
		Enforced:  enforce,
		Reject:    holdsCert && enforce,
	}
}

// agentHasActiveClientCert reports whether the given agent_id has at least one
// ACTIVE client certificate: not revoked (revoked_at IS NULL) and not expired
// (expires_at > NOW()). Expiry is part of "active" on purpose — an expired cert
// cannot be used for direct mTLS, so flagging its holder would be a false
// positive against the enforcement precondition ("all cert-holding agents can
// reach direct mTLS").
//
// Schema reference: migration 000036_client_certificates — table
// client_certificates(agent_id, revoked_at, expires_at, ...), indexed on
// agent_id and on expires_at WHERE revoked_at IS NULL.
func agentHasActiveClientCert(ctx context.Context, pool *pgxpool.Pool, agentID string) (bool, error) {
	if pool == nil || agentID == "" {
		return false, nil
	}
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM client_certificates
			WHERE agent_id = $1
			  AND revoked_at IS NULL
			  AND expires_at > NOW()
		)
	`, agentID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// evaluateAgentCertBinding runs the full token→cert binding check for an agent
// that has just passed enrollment-token WS auth. It performs the cert lookup,
// applies the enforcement flag, emits the structured WARN log and Prometheus
// counter when the agent holds a cert, and returns whether the connection must
// be rejected.
//
// Fail-open policy: if the cert lookup errors (transient DB failure), we log and
// ALLOW the connection rather than lock out the fleet on a database blip. The
// binding check is an added control on top of already-valid token auth; degrading
// it to allow-on-error preserves availability without weakening the pre-existing
// auth guarantee. This mirrors the fail-open-to-token behavior of the PKI
// issuance rate-limit check in the same handler.
func evaluateAgentCertBinding(ctx context.Context, pool *pgxpool.Pool, agentID, clientIP string, enforce bool) (reject bool) {
	holdsCert, err := agentHasActiveClientCert(ctx, pool, agentID)
	if err != nil {
		log.Printf("[CERT-BINDING] cert lookup failed for agent %s from %s: %v — allowing (fail-open)", agentID, clientIP, err)
		return false
	}

	decision := decideCertBinding(holdsCert, enforce)
	if !decision.HoldsCert {
		// Agent legitimately has no cert; token auth is the expected path.
		return false
	}

	mode := "warn"
	if decision.Reject {
		mode = "enforced"
	}
	agentTokenAuthWithCert.WithLabelValues(agentID, mode).Inc()

	// Structured, greppable WARN. [CERT-BINDING] + would_break lets us enumerate
	// the agents that must reach direct mTLS before enforcement can be enabled.
	log.Printf("[CERT-BINDING] WARN agent=%s ip=%s holds_active_client_cert=true auth_method=enrollment_token mode=%s would_break=%t — cert-holding agent authenticated by token over tunnel",
		agentID, clientIP, mode, decision.Reject)

	return decision.Reject
}
