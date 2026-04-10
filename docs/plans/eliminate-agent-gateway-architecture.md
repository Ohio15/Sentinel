# Eliminate sentinel-agent-gateway — Architecture Plan

**Status:** Proposed (planning only, no code changes yet)
**Author:** Planning pass, 2026-04-10
**Related:** NEXUS infra refactor 2026-04-10 (extracted Traefik edge into `~/infra/`)
**Target repo:** `D:\Projects\Sentinel\`

---

## Executive Summary

The Sentinel stack currently runs a dedicated Traefik v3 container (`sentinel-agent-gateway`) that fronts agent traffic on `:8443` (HTTP/mTLS → `sentinel-backend:8080`) and `:4444` (TCP passthrough → `sentinel-backend:4444`). This refactor eliminates that container entirely. The Go backend already terminates mTLS for gRPC on `:4444` via `crypto/tls` + `google.golang.org/grpc/credentials`, so the gRPC side becomes a pure Docker port-mapping change. The HTTP/mTLS side (`:8443`) requires running a second `http.Server` inside `sentinel-backend` with a `tls.Config` that mirrors the options in `configs/traefik-agent/dynamic/tls.yml`, plus a native Go rate limiter to replace Traefik's `agentRateLimit` middleware.

When complete, `sentinel-agent-gateway` and its entire `configs/traefik-agent/` directory can be deleted. No Traefik instance will remain inside the Sentinel stack — edge web traffic is already handled by `infra-traefik` outside of Sentinel.

---

## Target State Architecture

```
                           ┌─────────────────────────────┐
                           │         Internet            │
                           └──────────┬──────────────────┘
                                      │
                  ┌───────────────────┼───────────────────┐
                  │                   │                   │
            :80/:443/:4443          :8443              :4444
         (web, infra-traefik)  (agent mTLS, direct)  (gRPC mTLS, direct)
                  │                   │                   │
                  ▼                   ▼                   ▼
         ┌──────────────┐    ┌────────────────────────────────────┐
         │ infra-traefik│    │           sentinel-backend         │
         │  (in ~/infra)│    │ ┌────────────────────────────────┐ │
         └──────┬───────┘    │ │ http.Server :8080 (plaintext)  │ │
                │            │ │  → infra-traefik upstream      │ │
                │            │ └────────────────────────────────┘ │
                │            │ ┌────────────────────────────────┐ │
                └───────────►│ │ http.Server :8443 (mTLS)       │ │
                  :8080      │ │  tls.Config{                   │ │
                (plaintext)  │ │    ClientCAs: ca-cert.pem      │ │
                             │ │    ClientAuth:                 │ │
                             │ │      VerifyClientCertIfGiven   │ │
                             │ │  }                             │ │
                             │ │  + agent rate limiter middleware│ │
                             │ │  routes: /ws/agent, /ws/agent/ │ │
                             │ │    mtls, /api/agent, /health   │ │
                             │ └────────────────────────────────┘ │
                             │ ┌────────────────────────────────┐ │
                             │ │ grpc.Server :4444              │ │
                             │ │  grpc.Creds(credentials.NewTLS │ │
                             │ │   (tlsConfig))                 │ │
                             │ └────────────────────────────────┘ │
                             └────────────────────────────────────┘
```

Changes vs. current state:

- The `sentinel-agent-gateway` container is **deleted**.
- `sentinel-backend` publishes `:8443` and `:4444` directly on the host.
- `sentinel-backend` now runs **three** listeners in `main.go`: the existing plaintext `:8080` (infra-traefik upstream — used by the web/API, unchanged), a new mTLS `:8443` for agents, and the existing `:4444` gRPC (mTLS already in place, just no longer behind Traefik passthrough).

---

## Why

- **Removes an entire container** (~256 MB reservation), its image (`traefik:v3.0`), and the `configs/traefik-agent/` directory tree.
- **One less failure point.** A Traefik restart during deploy currently drops every agent WS for ~2–5 s.
- **Auditability.** The mTLS policy today lives in YAML (`configs/traefik-agent/dynamic/tls.yml`) on Traefik's terms. Moving it into Go puts it next to the handlers that consume it and makes it covered by Go unit tests.
- **Ecosystem rule B4 (ONE SYSTEM ONE JOB).** With `infra-traefik` handling edge, having a second Traefik in Sentinel purely for two ports is architectural dead weight.
- **Zero added dependencies.** Everything needed already exists: `crypto/tls`, `net/http`, `golang.org/x/time/rate` (stdlib-adjacent and the idiomatic Go choice). The gRPC path needs **no code changes at all** — `server/internal/grpc/server.go:514-577` already builds a full mTLS `tls.Config` with `ClientCAs`, `ClientAuth: VerifyClientCertIfGiven`, and hands it to `grpc.Creds(credentials.NewTLS(...))`.

---

## Scope Boundaries

### IN scope

- Adding an mTLS `http.Server` listener to `server/cmd/sentinel/main.go`.
- Building a shared `tls.Config` factory so HTTP mTLS and gRPC mTLS load certs once.
- A Go-native per-IP rate limiter for agent endpoints (replaces Traefik `agentRateLimit`).
- `docker-compose.yml`: delete `sentinel-agent-gateway` service, republish `:8443` and `:4444` on `sentinel-backend`, delete the `agent-traefik-logs` volume.
- Delete `configs/traefik-agent/` tree.
- Agent-side: **no code changes expected.** The agent already talks to `wss://host:8443/ws/agent/mtls` (see `agent/internal/mtls/mtls.go:155-194` and `agent/internal/client/client.go:370-410`). It does not care whether Traefik or Go is the TLS terminator as long as the cert chain, cipher suites, and routes match.
- Updating `tests/e2e/` and `server/tests/` fixtures if they hardcode Traefik-specific behavior.

### OUT of scope

- Changes to the agent enrollment / auth protocol (`handlers_mtls.go` stays as-is).
- Changes to the PKI service, CA structure, or cert rotation flow.
- Adding new features (OCSP stapling, cert pinning, per-agent rate limits, etc.).
- Moving gRPC mTLS code (already correct — just needs Docker port republishing).
- Cloudflare tunnel path (`GRPC_PLAINTEXT_PORT=4445`, already independent of the gateway).
- Changes to `infra-traefik` in `~/infra/`.

---

## Current State Analysis (code-grounded)

### HTTP server in `sentinel-backend`

`server/cmd/sentinel/main.go:287-314` builds exactly one `http.Server`:

```go
server := &http.Server{
    Addr:              fmt.Sprintf(":%d", cfg.Port),  // cfg.Port = 8080
    Handler:           router,
    ReadTimeout:       30 * time.Second,
    WriteTimeout:      60 * time.Second,
    IdleTimeout:       120 * time.Second,
    ReadHeaderTimeout: 10 * time.Second,
    MaxHeaderBytes:    1 << 20,
}
...
if err := server.ListenAndServe(); ...
```

This is plaintext HTTP. It's published to the host as `127.0.0.1:8090:8080` and joined to the `edge` Docker network so `infra-traefik` can reach it. There is **currently no TLS termination in Go for HTTP.** Traefik (`sentinel-agent-gateway`) is the sole TLS terminator for agent HTTP/WebSocket.

### Agent WebSocket routes

From `server/internal/api/router.go:632-650`:

```go
ws := r.Group("/ws")
{
    ws.GET("/agent", handleAgentWebSocketWithServices(services))     // token auth
    ws.GET("/agent/mtls", handleAgentWebSocketMTLS(services))        // cert auth
    ws.GET("/dashboard", handleDashboardWebSocketWithServices(services))
}
agentCerts := api.Group("/agent/certs")
{
    agentCerts.POST("/renew", handleCertRenewal(services))
}
r.GET("/ws", handleAgentWebSocketWithServices(services))  // legacy
```

`handleAgentWebSocketMTLS` (`server/internal/api/handlers_mtls.go:23-161`) reads `c.Request.TLS.PeerCertificates[0]` and extracts the agent ID via `pki.GetAgentIDFromCert`. **Today this only works because Traefik terminates TLS and forwards the raw connection…** wait — let me re-check. Traefik terminates TLS (`http` entrypoint with `tls: options: mtls@file` in `traefik.yml:28-30`), so `c.Request.TLS` on the Go side is actually **nil** unless Traefik is configured to forward client cert info as a header. This is a potential current-state bug worth investigating (see "Open Questions" below). The refactor will fix it unambiguously because the Go server terminates TLS itself and populates `r.TLS.PeerCertificates` natively.

### Gin router is reused

The entire Gin engine (`router := api.NewRouterWithServices(services)`) handles both admin/API and agent WS routes. This makes the HTTP mTLS listener simple: we can reuse the **same Gin handler** on the new `:8443` listener — or alternatively filter to a subset via a second Gin engine. See "Proposed New Architecture" for the tradeoff.

### gRPC server (already correct)

`server/internal/grpc/server.go:514-577` (`StartServer`):

```go
cert, err := tls.LoadX509KeyPair(config.TLSCertFile, config.TLSKeyFile)
...
tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    MinVersion:   tls.VersionTLS12,
}
if config.CACertFile != "" {
    caCert, _ := os.ReadFile(config.CACertFile)
    caCertPool := x509.NewCertPool()
    if caCertPool.AppendCertsFromPEM(caCert) {
        tlsConfig.ClientCAs = caCertPool
        tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
    }
}
opts = append(opts, grpc.Creds(credentials.NewTLS(tlsConfig)))
```

Called from `main.go:351-374` with `cfg.TLSCertPath`, `cfg.TLSKeyPath`, `cfg.CACertPath`, `cfg.EnableMTLS`. The current Traefik config for gRPC is **TCP passthrough** (`configs/traefik-agent/dynamic/agent-routes.yml:44-58`) with `tls: passthrough: true` — so the backend is already the TLS terminator. Removing Traefik from this path is a no-op for Go code; only `docker-compose.yml` needs to publish `:4444` on `sentinel-backend` instead of on `sentinel-agent-gateway`.

### Config loader

`server/pkg/config/config.go`:
- `Port int` (env `PORT`, default `8080`) — line 13 / 91
- `GRPCPort int` (env `GRPC_PORT`, default `4444`) — line 61 / 139
- `TLSCertPath` (env `TLS_CERT_PATH`, default `/certs/server-cert.pem`) — line 63 / 141
- `TLSKeyPath` (env `TLS_KEY_PATH`, default `/certs/server-key.pem`) — line 64 / 142
- `CACertPath` (env `CA_CERT_PATH`, default `/certs/ca-cert.pem`) — line 55 / 133
- `EnableMTLS bool` (env `ENABLE_MTLS`, default `true`) — line 57 / 135

**Missing:** there is no `AgentMTLSPort` or `MTLSHTTPPort` field. This needs to be added (env `AGENT_MTLS_PORT`, default `8443`).

### Rate limiting

`server/internal/middleware/auth_ratelimit.go` implements an IP-based limiter with sliding window and exponential backoff — but it's designed for `/api/auth/login`, **not** for the 300 req/min sustained rate the Traefik middleware permits for agents. A separate, simpler limiter tuned for the agent path is appropriate. There is no existing use of `golang.org/x/time/rate` in the `server/` tree (confirmed via grep), but it's in the Go ecosystem's stdlib-adjacent `x/time` module and is the idiomatic choice.

Traefik `agentRateLimit` (`configs/traefik-agent/dynamic/middlewares.yml`):
```yaml
rateLimit:
  average: 300   # per period
  burst: 100
  period: 1m
  sourceCriterion:
    ipStrategy:
      depth: 1
```

Translation: 300 req/min per source IP, burst of 100. The Go equivalent with `golang.org/x/time/rate` is `rate.NewLimiter(rate.Every(time.Minute/300), 100)` per IP, stored in a `sync.Map` keyed by client IP, with periodic cleanup (this pattern is straightforward and well-documented).

### Certificates on disk

`certs/` directory contains `ca-cert.pem`, `ca-key.pem`, `server-cert.pem`, `server-key.pem`, `server.csr`, `server-ext.cnf`. Generated by `scripts/generate-ca.sh`. The Go backend already mounts this directory read-only at `/certs`. **No cert generation or rotation changes needed.**

### Tests that reference agent connectivity

- `server/tests/agent_simulator.go` — uses WebSocket dialer; check for hardcoded port 8443.
- `server/tests/integration/critical_paths_test.go` — integration tests for agent flow.
- `tests/e2e/playwright/helpers/agent-simulator.ts` — E2E helper (JS/TS, matches `8443` per grep).

All three will need a grep-level audit during implementation to confirm they reach `sentinel-backend:8443` (new direct) vs `sentinel-agent-gateway:8443` (old).

---

## Proposed New Architecture

### 1. Shared TLS config factory (new file)

**New file:** `server/pkg/tlsconfig/tlsconfig.go` (~60 lines)

```go
package tlsconfig

// LoadAgentMTLSConfig builds a *tls.Config suitable for BOTH the HTTP mTLS
// listener and the gRPC server. Centralizing this ensures cipher/curve/version
// policy stays consistent and is unit-testable.
//
// Mirrors configs/traefik-agent/dynamic/tls.yml exactly:
//   - MinVersion = TLS 1.2, MaxVersion = TLS 1.3
//   - ClientAuth = VerifyClientCertIfGiven (backward compat with token agents)
//   - ClientCAs loaded from caCertPath
//   - CipherSuites and CurvePreferences match Traefik config
func LoadAgentMTLSConfig(certPath, keyPath, caCertPath string) (*tls.Config, error)
```

The gRPC server's existing `StartServer` will be refactored to accept a prebuilt `*tls.Config` (or call this factory internally) so duplication is eliminated. This is a minor refactor — the current inline version in `grpc/server.go:533-561` becomes a 3-line call.

### 2. New mTLS HTTP listener in `main.go`

After the existing `http.Server` block (around line 296), add a second server:

```go
if cfg.EnableMTLS && cfg.AgentMTLSPort > 0 {
    mtlsTLSConfig, err := tlsconfig.LoadAgentMTLSConfig(
        cfg.TLSCertPath, cfg.TLSKeyPath, cfg.CACertPath,
    )
    if err != nil {
        log.Fatalf("Failed to load agent mTLS config: %v", err)
    }

    mtlsServer := &http.Server{
        Addr:              fmt.Sprintf(":%d", cfg.AgentMTLSPort),
        Handler:           router,                 // same Gin engine
        TLSConfig:         mtlsTLSConfig,
        ReadTimeout:       0,                      // WebSocket: no read timeout
        WriteTimeout:      0,                      // WebSocket: no write timeout
        IdleTimeout:       300 * time.Second,      // matches traefik.yml:27
        ReadHeaderTimeout: 10 * time.Second,
        MaxHeaderBytes:    1 << 20,
    }

    go func() {
        logger.Info("Agent mTLS server listening", "addr", mtlsServer.Addr)
        // Empty strings because certs are in TLSConfig
        if err := mtlsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
            log.Fatalf("Agent mTLS server failed: %v", err)
        }
    }()
    // Add to shutdown list
}
```

**Critical detail:** `http.Server.ListenAndServeTLS` with empty filenames uses `TLSConfig.Certificates` directly — this is the documented way to use a pre-built `tls.Config`.

**Question to resolve in planning:** should the mTLS listener use the **same** Gin engine (meaning all admin/API routes are exposed on `:8443` too) or a **separate** engine that only routes `/ws/agent*`, `/api/agent*`, `/api/agent/certs/renew`, and `/health`? Traefik today is configured to only route those four path prefixes (`configs/traefik-agent/dynamic/agent-routes.yml:3-36`). Security preference: **separate engine with explicit allow-list of agent routes** — this matches current Traefik behavior and prevents accidental exposure of admin endpoints on the mTLS port. Requires extracting route registration into a reusable function.

### 3. Agent rate limiter middleware (new file)

**New file:** `server/internal/middleware/agent_ratelimit.go` (~80 lines)

```go
package middleware

import (
    "net/http"
    "sync"
    "time"

    "github.com/gin-gonic/gin"
    "golang.org/x/time/rate"
)

// AgentRateLimiter replaces the Traefik `agentRateLimit` middleware.
// 300 req/min per source IP, burst 100, automatic cleanup of stale entries.
type AgentRateLimiter struct {
    limiters sync.Map // map[string]*rate.Limiter
    rate     rate.Limit
    burst    int
}

func NewAgentRateLimiter() *AgentRateLimiter { ... }
func (l *AgentRateLimiter) Middleware() gin.HandlerFunc { ... }
func (l *AgentRateLimiter) cleanupLoop() { ... }
```

Applied to the agent-mTLS engine's WebSocket + `/api/agent*` routes (but not `/health`, matching Traefik priority 5 in `agent-routes.yml:36`).

### 4. Docker changes

**`docker-compose.yml`:**
- Delete lines 2-29 (entire `sentinel-agent-gateway:` service).
- Delete the `agent-traefik-logs` volume definition (search at bottom of file).
- On `sentinel-backend`, change the `ports:` block:
  ```yaml
  ports:
    - "127.0.0.1:8090:8080"           # keep: local dev plaintext
    - "127.0.0.1:4445:4445"           # keep: CF tunnel plaintext gRPC
    - "8443:8443"                      # NEW: agent mTLS HTTP
    - "4444:4444"                      # NEW: agent mTLS gRPC (was: expose only)
    - "${TURN_PORT:-3478}:${TURN_PORT:-3478}/udp"
    - "${TURN_PORT:-3478}:${TURN_PORT:-3478}/tcp"
    - "${TURN_MIN_PORT}-${TURN_MAX_PORT}:${TURN_MIN_PORT}-${TURN_MAX_PORT}/udp"
  ```
- Remove the `expose: - "4444"` block (now in `ports`).
- Add env var: `AGENT_MTLS_PORT=8443`.
- Remove the `edge` network from the backend's network list **only if** no other service still needs to reach it via edge (likely still needed for `infra-traefik` to reach `:8080`; leave it alone).

**`configs/traefik-agent/` directory:** deleted entirely.

### 5. Health check on `:8443`

The existing `:8080` health check (`GET /health`) continues to work unchanged for Docker and `infra-traefik`. The mTLS engine also exposes `/health` on `:8443` for parity with Traefik's `agent-health` router, but uses a non-mTLS-required route (matching Traefik's absence of `middlewares:` on that router). Because the mTLS listener uses `VerifyClientCertIfGiven`, unauth health checks still work on `:8443`.

---

## Config Changes

Add to `server/pkg/config/config.go`:

```go
// Agent mTLS HTTP listener
AgentMTLSPort int // Port for mTLS HTTP listener (default 8443, 0 disables)
```

In `Load()`:
```go
AgentMTLSPort: getEnvInt("AGENT_MTLS_PORT", 8443),
```

Validation: if `EnableMTLS && AgentMTLSPort > 0`, require `TLSCertPath`, `TLSKeyPath`, `CACertPath` to exist on disk. Fail fast.

---

## Testing Strategy

### Unit tests (new)

- `server/pkg/tlsconfig/tlsconfig_test.go` — load certs, verify cipher suites, verify `ClientAuth` mode.
- `server/internal/middleware/agent_ratelimit_test.go` — hit a handler 400 times from one IP, confirm ~100 pass (burst) and the rest are 429-rate-limited.

### Integration tests

- `server/tests/integration/mtls_listener_test.go` (new) — boot a test backend with mTLS enabled, issue a client cert via PKI service, connect `websocket.Dialer` to `wss://localhost:8443/ws/agent/mtls`, confirm upgrade succeeds and auth_response arrives.
- Existing `critical_paths_test.go` needs auditing to confirm it points at the right listener.

### Manual verification (cutover day)

1. **Before cutover:** capture `docker compose ps`, agent count from `/api/devices?status=online`.
2. **Deploy** the new backend image with the mTLS listener built in (gateway still running alongside — dual-listening is safe because we'll change the compose file AFTER).
3. **Smoke test from inside the Docker network:** `curl --cacert ca-cert.pem --cert client.pem --key client.key https://sentinel-backend:8443/health` — must return 200.
4. **Smoke test gRPC (no change, but verify):** use `grpcurl -cacert ... -cert ... -key ... sentinel-backend:4444 sentinel.dataplane.DataPlaneService/GetActiveStreams`.
5. **Cut the compose file:** remove `sentinel-agent-gateway`, republish ports on backend.
6. **`docker compose up -d sentinel-backend`** (and remove the gateway container).
7. **Verify** all previously-connected agents reconnect within 2 minutes (their reconnect loop is in `agent/internal/client/client.go:336-Connect`).
8. **Check** `/api/devices?status=online` count matches the pre-cutover baseline.

### Backward compat

The existing token-auth agent flow (`/ws/agent` with JSON auth message, not cert-based) **also** runs through Traefik today. It's on the same `:8443` mTLS entrypoint because Traefik has `ClientAuth: VerifyClientCertIfGiven` — agents without certs still connect, then auth via token. The Go listener must do the same:
- `tls.VerifyClientCertIfGiven` is the right `ClientAuth` mode (**confirmed** — this is a real `crypto/tls` constant, same name as Traefik uses).
- The route `/ws/agent` is handled by `handleAgentWebSocketWithServices` which expects a JSON auth message — unchanged.
- The route `/ws/agent/mtls` is handled by `handleAgentWebSocketMTLS` which expects `c.Request.TLS.PeerCertificates[0]` — in native Go this will **actually be populated** (unlike the possibly-broken current Traefik path, see Open Questions).

---

## Rollback Strategy

Because the refactor is scoped to `docker-compose.yml` + `server/` Go code, rollback is clean:

1. **If deploy fails at the image stage:** the new backend image fails health check, old `sentinel-agent-gateway` + old backend image keep running. No agent impact.
2. **If deploy succeeds but agents can't connect:** `git revert` the docker-compose change, `docker compose up -d sentinel-agent-gateway sentinel-backend`. Traefik comes back up, agents reconnect. Time-to-rollback: ~2 minutes.
3. **If a subtle bug appears days later** (e.g., cipher mismatch with one OS): roll back compose and backend image together via `git revert`, then diagnose offline. The `configs/traefik-agent/` files can be kept in git history (not force-pushed) so they're recoverable.

Git branching discipline:
- Branch name: `refactor/eliminate-agent-gateway`
- Keep `configs/traefik-agent/` deletion as the **very last commit** in the branch so `git revert HEAD~1` can restore the gateway in isolation.
- Do **not** rebase/squash during the cutover week.

---

## Open Questions (for Ron before execution)

1. **Does Traefik currently forward client certs to the backend at all?** `configs/traefik-agent/dynamic/tls.yml` sets `clientAuthType: VerifyClientCertIfGiven` on the entrypoint, and `handleAgentWebSocketMTLS` reads `c.Request.TLS.PeerCertificates[0]` — but with Traefik terminating TLS and forwarding HTTP to `sentinel-backend:8080`, `r.TLS` on the Go side should be `nil`. Either (a) Traefik is using a header-based cert passthrough that I couldn't locate in the config, (b) the mTLS WebSocket path is currently broken and nothing uses it, or (c) something in Traefik's HTTP/WebSocket upstream forwarding I don't fully understand. **This needs a quick live check** (`docker logs sentinel-backend | grep mTLS` on production, or test with a real agent) before execution — the refactor will trivially fix whichever case this is, but it changes how urgent the refactor is.

2. **Separate Gin engine or same Gin engine for `:8443`?** Recommendation: **separate** engine, allow-list routes. Requires a modest refactor to extract agent-facing route registration into a function that can be called twice. Agree?

3. **Rate limiter storage — in-memory or Redis?** In-memory matches current Traefik behavior (per-Traefik-instance, non-distributed) and is simpler. If the backend ever scales to multiple replicas, per-replica limits will be lenient by a factor of N. For now, in-memory is fine. Confirm?

4. **Do we keep `/ws/agent` (token auth) on `:8443`?** Today it's reachable there via Traefik. The plan assumes yes. If Ron wants to force all new agents to mTLS-only, this is the sprint to do it — but that's an auth-protocol change and technically "OUT of scope" per the boundaries above. Confirm assumption.

5. **Certificate hot-reload?** Traefik watches cert files and reloads on change. Native Go `http.Server` with a prebuilt `tls.Config` does **not** — rotating certs requires a backend restart. Acceptable for a quarterly cert rotation? Or should the plan include a `GetCertificate` callback that re-reads from disk (trivial addition, ~15 lines)?

---

## Pending Verification

- `tls.VerifyClientCertIfGiven` — **verified** as a real constant in `crypto/tls` (matches the value currently used in `server/internal/grpc/server.go:553`).
- `http.Server.ListenAndServeTLS("", "")` with prebuilt `TLSConfig.Certificates` — **verified** as the documented Go idiom (stdlib docs).
- `golang.org/x/time/rate.NewLimiter` API — **not verified in this plan.** Needs a 5-minute check of the package docs before implementation to confirm exact signature. It's a well-known package and the usage pattern is standard, but not a line I should hand-wave on.
- Whether removing `sentinel-agent-gateway` from the Docker network map breaks any other service's `depends_on` — **quick grep at implementation time** to confirm nothing else references it.
