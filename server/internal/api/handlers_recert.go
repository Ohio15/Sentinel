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
	"encoding/pem"
	"errors"
	"log"
	"net/http"
	"sync"
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

// reCertRateLimiter enforces "max 5 re-cert calls per agent_id per hour".
// Keyed on the certificate CN (which is the agent_id). Stale buckets are pruned
// by a background goroutine that runs every 30 minutes — the bucket window is
// 1 hour so 30 minutes guarantees we never delete an active window.
type reCertRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*reCertBucket
}

type reCertBucket struct {
	windowStart time.Time
	count       int
}

const (
	reCertWindow      = time.Hour
	reCertMaxRequests = 5
)

var globalReCertLimiter = newReCertRateLimiter()

func newReCertRateLimiter() *reCertRateLimiter {
	l := &reCertRateLimiter{buckets: make(map[string]*reCertBucket)}
	go l.cleanupLoop()
	return l
}

// allow returns (allowed, retryAfter). When allowed=false the caller should
// respond 429 with a Retry-After header derived from retryAfter (seconds).
func (l *reCertRateLimiter) allow(agentID string) (bool, time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[agentID]
	if !ok || now.Sub(b.windowStart) >= reCertWindow {
		l.buckets[agentID] = &reCertBucket{windowStart: now, count: 1}
		return true, 0
	}

	if b.count >= reCertMaxRequests {
		// Window not yet over — caller should retry after the remaining duration.
		return false, reCertWindow - now.Sub(b.windowStart)
	}

	b.count++
	return true, 0
}

func (l *reCertRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-reCertWindow)
		l.mu.Lock()
		for k, b := range l.buckets {
			if b.windowStart.Before(cutoff) {
				delete(l.buckets, k)
			}
		}
		l.mu.Unlock()
	}
}

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
		//    the client_certificates table; a missing row returns false (not an
		//    error) because the cert may simply pre-date the tracking table.
		ctx := c.Request.Context()
		revoked, err := services.PKI.IsCertificateRevoked(ctx, oldSerial)
		if err != nil {
			log.Printf("[re-cert] Warning: revocation check failed for serial=%s: %v", oldSerial, err)
			// fall through — DB hiccup must not block legitimate re-cert
		} else if revoked {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Certificate has been revoked"})
			return
		}

		// 4. Per-agent rate limit (5/hour). Enforced after auth so probing an
		//    invalid cert doesn't burn a real agent's bucket.
		if allowed, retryAfter := globalReCertLimiter.allow(agentID); !allowed {
			seconds := int(retryAfter.Seconds())
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", time.Duration(seconds*int(time.Second)).String())
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "re_cert_rate_limited",
				"message": "Re-cert requests are limited to 5 per agent per hour.",
			})
			return
		}

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

		// 6. Look up the device by client_cert_serial. This is the primary
		//    contract: a re-cert request must identify an existing device row.
		//    If the row is missing we return 404 — the installer must fall back
		//    to a fresh install_code enrollment. We do NOT auto-recreate.
		var (
			deviceID uuid.UUID
			orgID    int
		)
		err = services.DB.Pool().QueryRow(ctx, `
			SELECT id, organization_id
			FROM devices
			WHERE client_cert_serial = $1
		`, oldSerial).Scan(&deviceID, &orgID)
		if err != nil {
			log.Printf("[re-cert] No device found for cert serial=%s agent=%s — installer should re-enroll", oldSerial, agentID)
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "device_not_found",
				"message": "This cert does not match a known device. Use a fresh install_code to re-enroll.",
			})
			return
		}

		// 7. Issue the new cert. PKI.IssueClientCertificate atomically:
		//      - generates a new ECDSA keypair
		//      - signs a new cert
		//      - inserts a row in client_certificates
		//      - UPDATEs devices SET client_cert_serial/issued_at/expires_at/fingerprint
		//    Concurrent re-cert calls serialize on the PKI struct's internal
		//    mutex, so two rapid requests from the same agent cannot tear state.
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
		if _, err := services.DB.Pool().Exec(ctx, `
			UPDATE devices
			SET ca_cert_distributed_at = NOW(),
			    updated_at = NOW()
			WHERE id = $1
		`, deviceID); err != nil {
			log.Printf("[re-cert] Warning: failed to bump distribution timestamps for device=%s: %v", deviceID, err)
		}

		// 10. Synchronous audit write — we want this entry durable before the
		//     200 response so investigators always see a reissue event paired
		//     with the response. Failure of the audit write is logged but does
		//     not fail the request (the cert is already issued and committed).
		auditDetails := map[string]any{
			"old_serial":     oldSerial,
			"new_serial":     bundle.SerialNumber,
			"requesting_ip":  c.ClientIP(),
			"agent_id":       agentID,
			"expires_at":     bundle.ExpiresAt.UTC().Format(time.RFC3339),
			"fingerprint":    bundle.Fingerprint,
		}
		if err := audit.LogEvent(
			ctx,
			services.DB.Pool(),
			audit.ActionDeviceCertReissue,
			audit.ResourceTypeDevice,
			&deviceID,
			nil, // no user — this is agent-initiated
			c.ClientIP(),
			audit.SeverityInfo,
			auditDetails,
		); err != nil {
			log.Printf("[re-cert] Warning: audit log write failed for agent=%s old=%s new=%s: %v",
				agentID, oldSerial, bundle.SerialNumber, err)
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
