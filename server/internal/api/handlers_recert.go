// Package api — re-certification endpoint for agent reinstall flows.
//
// POST /api/agent/re-cert is the server-side answer to "I'm reinstalling the
// Sentinel agent on an already-enrolled machine and need a fresh client cert
// without re-running enrollment". The endpoint authenticates the agent solely
// via its existing (still-valid) mTLS client cert, looks up the matching
// device row, issues a new cert atomically, and writes a 'device_cert_reissue'
// audit row.
//
// Design notes:
//   - Mounted on the dedicated mTLS listener (:8443) in agent_router.go. This
//     guarantees the connection terminated TLS in-process so PeerCertificates
//     is populated natively (no Traefik/proxy in the middle).
//   - If the cert does NOT map to a known device the endpoint returns 404 with
//     a machine-readable error code so the installer can fall back to a fresh
//     install_code re-enrollment. We do NOT auto-recreate the device row.
//   - Per-agent rate limit: 5 requests/hour. Enforced in-process via reCertRateLimiter
//     (sync.Map keyed on certificate CN). The mTLS listener already has a per-IP
//     limit; the per-agent limit prevents a single rogue agent from churning certs
//     even from changing IPs.
//   - The PKI.IssueClientCertificate call records the new cert row and atomically
//     updates devices.client_cert_* fields. Concurrent re-cert calls are serialized
//     by the PKI mutex; that satisfies the "no corrupted state on rapid re-cert"
//     requirement without an explicit SELECT FOR UPDATE in this handler.
package api

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/audit"
	"github.com/sentinel/server/internal/pki"
)

// errInvalidCSRPEM is returned when a re-cert request body contains a CSR
// field but the PEM block fails to decode or is the wrong type.
var errInvalidCSRPEM = errors.New("csr: invalid PEM (expected 'CERTIFICATE REQUEST' block)")

// reCertRequest is the optional JSON body for POST /api/agent/re-cert.
// Both fields are optional today. CSR is parsed and validated for shape but
// the current pki.IssueClientCertificate helper always generates a server-side
// keypair (the CSR-supplied public key path is reserved for a future PKI change
// and is rejected with 400 if supplied — see CSR-handling note in handler).
type reCertRequest struct {
	CSR string `json:"csr,omitempty"` // PEM-encoded CSR (optional, currently rejected — see note)
}

// reCertResponse is the success body for POST /api/agent/re-cert.
// ClientKey is omitted (zero-value JSON elision) when the agent supplied its
// own keypair via CSR. Today it's always present because CSR mode isn't wired.
type reCertResponse struct {
	ClientCert string    `json:"clientCert"`
	ClientKey  string    `json:"clientKey,omitempty"`
	CACert     string    `json:"caCert"`
	IssuedAt   time.Time `json:"issuedAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Serial     string    `json:"serial"`
}

// H3 (qa-butcher): rate limiting moved out of process and into the database.
// The previous in-memory reCertRateLimiter / reCertBucket / cleanupLoop was
// reset on every process restart and was per-replica only, making the 5/hour
// limit a soft suggestion that an attacker with stolen credentials could
// evade by triggering or waiting for restarts. The replacement counts rows
// in client_certificates (which already has issued_at and an agent_id index)
// inside the same transaction that issues the new cert. See step 6b in
// handleAgentReCert below.
const reCertHourLimit = 5

// handleAgentReCert returns the gin handler for POST /api/agent/re-cert.
// See package doc comment for the full design.
func handleAgentReCert(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. mTLS auth: client cert must be present. The TLS stack has already
		//    verified the chain against the CA because tlsconfig.LoadAgentMTLSConfig
		//    sets ClientAuth = tls.RequireAndVerifyClientCert. We still validate
		//    expiry defensively because the handshake-time check uses the TLS
		//    layer's clock; this guards against clock drift between the handshake
		//    and handler execution.
		if c.Request.TLS == nil || len(c.Request.TLS.PeerCertificates) == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Client certificate required"})
			return
		}

		clientCert := c.Request.TLS.PeerCertificates[0]
		now := time.Now()
		if now.Before(clientCert.NotBefore) || now.After(clientCert.NotAfter) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Client certificate is not valid (expired or not yet valid)"})
			return
		}

		agentID := pki.GetAgentIDFromCert(clientCert)
		if agentID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid certificate: missing agent ID in Common Name"})
			return
		}
		oldSerial := pki.GetSerialFromCert(clientCert)

		// 2. PKI service must be available — without it we cannot issue a cert.
		if services.PKI == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "PKI service unavailable"})
			return
		}

		// 3. Revocation check. The PKI helper does best-effort lookup against
		//    the client_certificates table; a missing row returns (false, nil)
		//    because the cert may simply pre-date the tracking table. A real
		//    error here means we cannot prove the cert is NOT revoked, so we
		//    MUST fail closed. The previous "fall through on error" behavior
		//    let a revoked cert reissue itself during a DB hiccup, which is
		//    exactly the scenario the revocation check exists to prevent.
		ctx := c.Request.Context()
		revoked, err := services.PKI.IsCertificateRevoked(ctx, oldSerial)
		if err != nil {
			log.Printf("[re-cert] Revocation check failed for serial=%s agent=%s: %v — failing closed", oldSerial, agentID, err)
			c.Header("Retry-After", strconv.Itoa(30))
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":   "revocation_check_failed",
				"message": "Could not verify cert revocation status. Retry in 30 seconds.",
			})
			return
		}
		if revoked {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Certificate has been revoked"})
			return
		}

		// 4. (was: in-process per-agent rate limit). Moved into the tx as step
		//    6b — see DB-backed check after the device lookup. The in-memory
		//    bucket couldn't survive restarts or scale across replicas.

		// 5. Optional CSR shape validation. The current PKI helper always
		//    generates a fresh keypair server-side; an agent-supplied CSR is
		//    rejected with 400 rather than silently ignored so the installer
		//    team gets a clear signal that CSR mode isn't yet implemented.
		var req reCertRequest
		if c.Request.ContentLength > 0 {
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON body", "details": err.Error()})
				return
			}
		}
		if req.CSR != "" {
			if _, err := parseCSR(req.CSR); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid CSR", "details": err.Error()})
				return
			}
			// TODO(re-cert): wire pki.IssueClientCertificateFromCSR once the helper
			// exists. For now, fail loudly so the agent doesn't think a CSR was
			// honoured when in fact a server-generated key was returned.
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "csr_not_supported",
				"message": "Server-side CSR signing is not yet implemented. Retry without the csr field; the server will generate a fresh keypair.",
			})
			return
		}

		// 6-10. Transactional block. Everything from the device lookup through
		//       the audit write happens inside a single tx so a partial failure
		//       cannot leave dashboard timestamps / audit rows / device pointer
		//       state inconsistent. The opening SELECT ... FOR UPDATE acquires
		//       a row lock on the device that is held until commit/rollback,
		//       which forces a second concurrent re-cert request for the same
		//       device to block at this point until the first completes.
		//       Combined with PKI's own internal mutex (which serializes cert
		//       *generation*), this gives us end-to-end serialization of
		//       same-device re-cert flows.
		//
		//       Caveat: PKI.IssueClientCertificate writes to client_certificates
		//       AND updates devices on its OWN pool connection (not via tx). If
		//       we roll back here AFTER PKI succeeded, the new cert row stays
		//       in client_certificates and devices.client_cert_* fields still
		//       point at the new serial. That's an orphaned cert record, not a
		//       security regression: the device row still references a valid
		//       (newly issued) cert, just one the agent never received because
		//       we 500'd. Operators can clean up via the cert-management UI.
		tx, err := services.DB.Pool().Begin(ctx)
		if err != nil {
			log.Printf("[re-cert] Failed to begin transaction for agent=%s: %v", agentID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}
		// Best-effort rollback on any path that doesn't reach Commit. pgx
		// treats Rollback-after-Commit as a no-op error we can safely ignore.
		defer func() {
			_ = tx.Rollback(ctx)
		}()

		// 6. Look up the device by client_cert_serial. This is the primary
		//    contract: a re-cert request must identify an existing device row.
		//    If the row is missing we return 404 — the installer must fall back
		//    to a fresh install_code enrollment. We do NOT auto-recreate.
		//
		//    FOR UPDATE acquires a row-level write lock that blocks any other
		//    transaction's SELECT FOR UPDATE / UPDATE / DELETE on this row
		//    until we commit. Concurrent re-cert flows for the SAME device
		//    will queue here; flows for DIFFERENT devices are unaffected.
		var (
			deviceID uuid.UUID
			orgID    int
		)
		err = tx.QueryRow(ctx, `
			SELECT id, organization_id
			FROM devices
			WHERE client_cert_serial = $1
			FOR UPDATE
		`, oldSerial).Scan(&deviceID, &orgID)
		if err != nil {
			log.Printf("[re-cert] No device found for cert serial=%s agent=%s — installer should re-enroll", oldSerial, agentID)
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "device_not_found",
				"message": "This cert does not match a known device. Use a fresh install_code to re-enroll.",
			})
			return
		}

		// 6b. DB-backed per-agent rate limit (5 reissues per rolling hour).
		//     Counts against client_certificates rather than an in-memory bucket
		//     so the limit survives restarts and applies uniformly across
		//     replicas. Uses the existing idx_client_certificates_agent_id index
		//     (a composite (agent_id, issued_at) would be marginally faster but
		//     the single-column index is correct and selective enough — agent_id
		//     cardinality matches fleet size).
		var recentCount int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM client_certificates
			WHERE agent_id = $1 AND issued_at > NOW() - INTERVAL '1 hour'
		`, agentID).Scan(&recentCount); err != nil {
			log.Printf("[re-cert] Rate limit lookup failed for agent=%s: %v", agentID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Rate limit check failed"})
			return
		}
		if recentCount >= reCertHourLimit {
			// Compute a real Retry-After based on when the oldest cert in the
			// window will age out. Falls back to a full hour on lookup failure.
			retryAfterSec := 3600
			var oldestIssued time.Time
			if err := tx.QueryRow(ctx, `
				SELECT MIN(issued_at) FROM client_certificates
				WHERE agent_id = $1 AND issued_at > NOW() - INTERVAL '1 hour'
			`, agentID).Scan(&oldestIssued); err == nil {
				if remaining := time.Until(oldestIssued.Add(time.Hour)); remaining > 0 {
					retryAfterSec = int(remaining.Seconds())
					if retryAfterSec < 1 {
						retryAfterSec = 1
					}
				}
			}
			c.Header("Retry-After", strconv.Itoa(retryAfterSec))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "re_cert_rate_limited",
				"message": "Re-cert requests are limited to 5 per agent per hour.",
			})
			return
		}

		// 7. Issue the new cert. PKI.IssueClientCertificate atomically:
		//      - generates a new ECDSA keypair
		//      - signs a new cert
		//      - inserts a row in client_certificates (on its own pool conn)
		//      - UPDATEs devices SET client_cert_serial/issued_at/expires_at/fingerprint
		//    Concurrent re-cert calls serialize on the PKI struct's internal
		//    mutex (for cert generation) AND on our outer SELECT FOR UPDATE
		//    (for the surrounding device-state operations).
		bundle, err := services.PKI.IssueClientCertificate(
			ctx,
			agentID,
			deviceID,
			orgID,
			services.Config.CertValidityYears,
		)
		if err != nil {
			log.Printf("[re-cert] Failed to issue new cert for agent=%s device=%s: %v", agentID, deviceID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to issue replacement certificate"})
			return
		}

		// 8. Revoke the old cert. The new cert is already active in the devices
		//    row; revoking the old serial prevents reuse and is logged with
		//    revoked_reason='reissued'. Failure here is non-fatal — the new
		//    cert remains valid and the operator can clean up via the cert
		//    management UI.
		if err := services.PKI.RevokeCertificate(ctx, oldSerial, "reissued"); err != nil {
			log.Printf("[re-cert] Warning: failed to revoke old serial=%s for agent=%s: %v", oldSerial, agentID, err)
		}

		// 9. Refresh ca_cert_distributed_at and updated_at so dashboard sorting
		//    by "recent cert activity" reflects the reissue. issued_at /
		//    expires_at / fingerprint were already updated by the PKI helper.
		//    Inside the tx so rollback restores prior timestamps.
		if _, err := tx.Exec(ctx, `
			UPDATE devices
			SET ca_cert_distributed_at = NOW(),
			    updated_at = NOW()
			WHERE id = $1
		`, deviceID); err != nil {
			log.Printf("[re-cert] Failed to bump distribution timestamps for device=%s: %v", deviceID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update device timestamps"})
			return
		}

		// 10. Synchronous audit write inside the tx. If the commit fails the
		//     audit row goes with it, but the cert in client_certificates
		//     stays (PKI used a separate connection) — which is the orphaned-
		//     cert caveat documented at the top of the tx block. We accept
		//     that trade-off: investigators see an entry for every committed
		//     re-cert; an uncommitted re-cert leaves a recoverable orphan,
		//     not a silent state change.
		auditDetails := map[string]any{
			"old_serial":    oldSerial,
			"new_serial":    bundle.SerialNumber,
			"requesting_ip": c.ClientIP(),
			"agent_id":      agentID,
			"expires_at":    bundle.ExpiresAt.UTC().Format(time.RFC3339),
			"fingerprint":   bundle.Fingerprint,
		}
		detailsJSON, err := json.Marshal(auditDetails)
		if err != nil {
			// Should be unreachable for a static-shape map[string]any with
			// only strings, but if it ever fires we want a hard signal not
			// a silent skip — the audit entry is part of the durability
			// contract for this endpoint.
			log.Printf("[re-cert] Failed to marshal audit details for agent=%s: %v", agentID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record audit entry"})
			return
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_log (
			    user_id, action, resource_type, resource_id, details,
			    ip_address, user_agent, severity, organization_id
			) VALUES (
			    NULL, $1, $2, $3, $4::jsonb,
			    NULLIF($5, '')::inet, 'sentinel-agent', $6, $7
			)
		`, audit.ActionDeviceCertReissue, audit.ResourceTypeDevice, deviceID,
			detailsJSON, c.ClientIP(), audit.SeverityInfo, orgID); err != nil {
			log.Printf("[re-cert] Audit insert failed for agent=%s old=%s new=%s: %v",
				agentID, oldSerial, bundle.SerialNumber, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record audit entry"})
			return
		}

		if err := tx.Commit(ctx); err != nil {
			log.Printf("[re-cert] Commit failed for agent=%s device=%s new_serial=%s: %v",
				agentID, deviceID, bundle.SerialNumber, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit re-cert transaction"})
			return
		}

		log.Printf("[re-cert] Issued new cert for agent=%s device=%s old_serial=%s new_serial=%s",
			agentID, deviceID, oldSerial, bundle.SerialNumber)

		c.JSON(http.StatusOK, reCertResponse{
			ClientCert: bundle.ClientCert,
			ClientKey:  bundle.ClientKey,
			CACert:     bundle.CACert,
			IssuedAt:   bundle.IssuedAt,
			ExpiresAt:  bundle.ExpiresAt,
			Serial:     bundle.SerialNumber,
		})
	}
}

// parseCSR decodes a PEM-encoded CSR and validates its signature. Returns the
// parsed CSR or an error. Used to fail-fast on malformed CSRs before they
// reach the PKI signing path.
func parseCSR(csrPEM string) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, errInvalidCSRPEM
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, err
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, err
	}
	return csr, nil
}
