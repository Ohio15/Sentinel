package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sentinel/server/internal/middleware"
)

// NewAgentMTLSRouter creates a dedicated Gin engine for the mTLS listener on
// :8443. It exposes ONLY agent-facing routes — no admin, user, or dashboard
// endpoints are reachable on this port. This matches the security allow-list
// that the Traefik agent-gateway previously enforced via route configuration.
//
// Routes mounted:
//   - GET  /ws/agent/mtls           — mTLS-authenticated agent WebSocket
//   - POST /api/agent/certs/renew   — certificate renewal (requires mTLS)
//   - POST /api/agent/re-cert       — fresh cert issuance for reinstall flow (requires mTLS)
//   - GET  /health                  — health check (no auth required)
//
// The rate limiter is applied to all routes except /health, matching the
// Traefik agentRateLimit middleware behavior.
func NewAgentMTLSRouter(services *Services, limiter *middleware.AgentRateLimiter) *gin.Engine {
	if services.Config.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(agentSecurityHeaders())

	// Health check — no rate limit, no auth (matches Traefik agent-health
	// router which had no middlewares applied)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"listener":  "mtls",
			"timestamp": time.Now().UTC(),
		})
	})

	// Rate-limited agent routes
	limited := r.Group("")
	limited.Use(limiter.Middleware())
	{
		// mTLS-authenticated WebSocket — the primary agent connection path.
		// c.Request.TLS.PeerCertificates is populated natively because Go
		// terminates TLS directly (no Traefik in the middle).
		limited.GET("/ws/agent/mtls", handleAgentWebSocketMTLS(services))

		// Certificate renewal endpoint — requires mTLS client cert
		limited.POST("/api/agent/certs/renew", handleCertRenewal(services))

		// Re-cert endpoint — used by installer when reinstalling an agent on a
		// machine that still has a valid client cert. Mounted on /api/agent
		// (NOT /api/agent/certs) to keep the installer-facing URL stable and
		// distinct from the agent-self-service /certs/renew path. Per-agent
		// rate limit (5/hour) is enforced inside the handler in addition to
		// the per-IP limiter applied here.
		limited.POST("/api/agent/re-cert", handleAgentReCert(services))
	}

	return r
}

// agentSecurityHeaders returns a minimal set of security headers for the agent
// listener. Agents are not browsers, so CSP/frame/XSS headers are omitted.
func agentSecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Next()
	}
}
