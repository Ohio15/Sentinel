# Eliminate sentinel-agent-gateway — Implementation Runbook

**Paired with:** `eliminate-agent-gateway-architecture.md`
**Status:** Planning only. No execution yet.
**Branch plan:** `refactor/eliminate-agent-gateway`

This runbook breaks the architecture plan into phases. Each phase is independently verifiable, reviewable, and (where possible) reversible without touching later phases. Phases 1–4 are **additive** (new code running alongside the gateway — zero production risk). Phase 5 is the cutover. Phase 6 is cleanup.

---

## Phase 0 — Pre-flight answers and verification

**Goal:** Resolve the five open questions from the architecture doc before touching code.

**Effort:** small (30–60 min of Ron's time + one live check)

**Actions:**

1. **Live cert-passthrough check.** SSH to the NEXUS deployment, run:
   ```bash
   docker exec sentinel-backend sh -c 'grep -ri "PeerCertificates\|r.TLS" /proc/1/fd/1 2>/dev/null || true'
   docker logs sentinel-backend 2>&1 | grep -iE "mtls|peercert" | tail -50
   ```
   Determine whether the `/ws/agent/mtls` path is actually serving traffic today. Expected finding: **it's either broken or serving zero agents**, because Go can't see the client cert when Traefik is terminating TLS upstream of a plaintext forward.
2. **Count agents on each path.** From the backend logs, count `[mTLS] Agent %s authenticated via certificate` log lines vs `handleAgentWebSocketWithServices` log lines over the last 24 hours. If mTLS count is 0, the refactor is also a bugfix for the mTLS path.
3. **Get Ron's answers** on Open Questions 2–5 (architecture doc).
4. **Verify `golang.org/x/time/rate` API** by reading the pkg.go.dev page (2 minutes).

**Verification:** A short note added to this runbook with the answers, filed before Phase 1 starts.

**Rollback:** N/A (no changes).

---

## Phase 1 — Shared TLS config factory

**Goal:** Extract the mTLS `*tls.Config` construction into a single place so HTTP and gRPC share it. No runtime behavior change.

**Effort:** small

**Files:**
- `server/pkg/tlsconfig/tlsconfig.go` — **new**, ~60 lines
- `server/pkg/tlsconfig/tlsconfig_test.go` — **new**, ~80 lines
- `server/internal/grpc/server.go` — **modify** `StartServer` to call the factory (delete ~25 inline lines, add ~5 lines)

**What the factory does:**
- Loads `server-cert.pem` + `server-key.pem` via `tls.LoadX509KeyPair`
- Loads `ca-cert.pem` into an `x509.CertPool`
- Sets `MinVersion = TLS12`, `MaxVersion = TLS13`
- Sets `ClientAuth = VerifyClientCertIfGiven`
- Sets `CipherSuites` to the four ECDHE AES-GCM suites from `configs/traefik-agent/dynamic/tls.yml:11-15`
- Sets `CurvePreferences` to `X25519, CurveP384, CurveP256`
- Returns `*tls.Config, error`

**Verification:**
- `go test ./server/pkg/tlsconfig/...` passes
- `go test ./server/internal/grpc/...` passes
- `docker compose up -d sentinel-backend` starts cleanly
- Agent WebSocket and gRPC still work (no observable change — gateway still running)

**Rollback:** `git revert` the Phase 1 commit. Zero risk because the gateway is still in front.

---

## Phase 2 — Agent rate limiter middleware

**Goal:** Build a Go replacement for Traefik's `agentRateLimit`. Not yet applied anywhere.

**Effort:** small

**Files:**
- `server/internal/middleware/agent_ratelimit.go` — **new**, ~80 lines
- `server/internal/middleware/agent_ratelimit_test.go` — **new**, ~100 lines
- `server/go.mod` / `go.sum` — **modify** to add `golang.org/x/time` (likely already present transitively; verify with `go list -m golang.org/x/time`)

**Implementation sketch:**
```go
type AgentRateLimiter struct {
    limiters sync.Map  // string → *clientLimiter
    rate     rate.Limit
    burst    int
}
type clientLimiter struct {
    limiter  *rate.Limiter
    lastSeen time.Time
}

// Matches configs/traefik-agent/dynamic/middlewares.yml: 300/min, burst 100
func NewAgentRateLimiter() *AgentRateLimiter {
    l := &AgentRateLimiter{
        rate:  rate.Every(time.Minute / 300),
        burst: 100,
    }
    go l.cleanupLoop()
    return l
}

func (l *AgentRateLimiter) Middleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        ip := c.ClientIP()
        cl, _ := l.limiters.LoadOrStore(ip, &clientLimiter{
            limiter:  rate.NewLimiter(l.rate, l.burst),
            lastSeen: time.Now(),
        })
        entry := cl.(*clientLimiter)
        entry.lastSeen = time.Now()
        if !entry.limiter.Allow() {
            c.AbortWithStatus(http.StatusTooManyRequests)
            return
        }
        c.Next()
    }
}
```

**Cleanup loop:** every 5 min, range over the sync.Map and delete entries where `time.Since(lastSeen) > 15*time.Minute`.

**Verification:**
- Unit test: hit middleware 400× from one IP, assert ~100 pass and ~300 get 429
- Unit test: cleanup removes stale entries
- `go vet ./server/...` passes

**Rollback:** `git revert`. Nothing runtime-affecting yet.

---

## Phase 3 — Config field and agent-route subrouter

**Goal:** Add the `AgentMTLSPort` config field and extract a reusable agent-route registration function, so the new mTLS engine can call it without duplicating route definitions.

**Effort:** medium

**Files:**
- `server/pkg/config/config.go` — **modify**, add `AgentMTLSPort int` field + loader + validation (~10 lines)
- `server/internal/api/router.go` — **modify**, extract `registerAgentRoutes(r *gin.Engine, services *Services)` helper that mounts `/ws/agent`, `/ws/agent/mtls`, `/api/agent/*`, `/api/agent/certs/renew`, `/health` (~30 lines refactor, no behavior change to existing caller)
- `server/internal/api/agent_router.go` — **new**, ~40 lines: `NewAgentMTLSRouter(services *Services, limiter *middleware.AgentRateLimiter) *gin.Engine` builds a fresh Gin engine with only the agent routes, the rate limiter applied to all except `/health`, and security headers.

**Verification:**
- `go build ./server/...` compiles
- `NewRouterWithServices` still works (call site in `main.go` unchanged)
- `NewAgentMTLSRouter` can be instantiated but is **not yet wired up** to a listener
- Existing integration tests pass

**Rollback:** `git revert`. The new function is unreferenced.

---

## Phase 4 — Add the mTLS HTTP listener to main.go (dual-running)

**Goal:** Start the Go mTLS listener on `:8443` **alongside** the existing Traefik gateway. Since Traefik publishes `:8443` on the host, Go will bind to `:8443` inside the container but Docker won't publish it yet — so production traffic still goes through Traefik. We verify the Go listener from inside the Docker network only.

**Effort:** medium

**Files:**
- `server/cmd/sentinel/main.go` — **modify**, add ~40 lines:
  - Instantiate `tlsconfig.LoadAgentMTLSConfig`
  - Instantiate `middleware.NewAgentRateLimiter`
  - Instantiate `api.NewAgentMTLSRouter`
  - Build `http.Server` with `TLSConfig` set
  - `go mtlsServer.ListenAndServeTLS("", "")`
  - Add to graceful shutdown sequence
- `docker-compose.yml` — **modify**, add env var `AGENT_MTLS_PORT=8443` to `sentinel-backend`. Do **NOT** publish the port yet. Do **NOT** remove the gateway.

**In-network verification (no production impact):**
```bash
docker compose up -d --build sentinel-backend
# Confirm both listeners are up:
docker exec sentinel-backend netstat -tln | grep -E ':(8080|8443|4444)'
# Hit the new Go mTLS listener from another container on the same network:
docker run --rm --network sentinel_sentinel-network \
  -v $PWD/certs:/certs:ro curlimages/curl \
  curl --cacert /certs/ca-cert.pem \
       --cert /certs/server-cert.pem --key /certs/server-key.pem \
       https://sentinel-backend:8443/health
# Expect: HTTP 200 {"status":"healthy",...}
```

**Also verify with a real test agent** pointed at `sentinel-backend:8443` directly (bypassing Traefik) from a test VM on the sentinel network.

**Rollback:** `git revert` the main.go changes. Traefik gateway unaffected.

---

## Phase 5 — Cutover: publish ports on backend, remove gateway

**Goal:** Flip agent traffic from Traefik to native Go.

**Effort:** small (in keystrokes), **HIGH** (in blast radius — this touches the production agent hot path)

**Pre-cutover checklist:**
- [ ] Phase 4 has been running in production for **at least 24 hours** without issue
- [ ] In-network `curl` smoke test from Phase 4 was successful
- [ ] `docker logs sentinel-backend 2>&1 | grep "Agent mTLS server listening"` confirms the listener started
- [ ] At least one real agent has successfully connected via the Go listener (even in test)
- [ ] `scripts/deploy-production.sh` has been reviewed for any hardcoded gateway references
- [ ] Baseline metrics captured: online agent count, gRPC active stream count, error rate over last hour
- [ ] Rollback commit is identified in advance: `git log --oneline -1` before the cutover commit
- [ ] Maintenance window announced in #ops (even for a zero-downtime cutover)

**Files:**
- `docker-compose.yml` — **modify**:
  - **Delete** lines 2-29 (the entire `sentinel-agent-gateway:` service)
  - **Delete** the `agent-traefik-logs` volume entry at the bottom
  - On `sentinel-backend`, change `ports:` to publish `8443:8443` and `4444:4444`
  - **Remove** the `expose: - "4444"` block (now redundant)

**Cutover sequence:**
1. Pull the branch on the deployment host:
   ```bash
   cd ~/Sentinel && git fetch && git checkout refactor/eliminate-agent-gateway
   ```
2. Bring down the gateway without touching backend:
   ```bash
   docker compose stop sentinel-agent-gateway
   ```
   **Agents will disconnect for 1–2 seconds**, then reconnect through the newly-exposed `sentinel-backend:8443` once step 3 completes.
3. Apply the new compose file:
   ```bash
   docker compose up -d sentinel-backend
   ```
   This removes the old published-port config and recreates the container with `8443:8443` and `4444:4444` published directly.
4. Remove the stopped gateway container:
   ```bash
   docker compose rm -f sentinel-agent-gateway
   ```

**Post-cutover verification (do in this order):**
1. **Immediate (within 30 s):**
   ```bash
   docker compose ps   # sentinel-agent-gateway must be absent
   ss -ltn | grep -E ':(8443|4444)'  # host must now have 0.0.0.0:8443 and 0.0.0.0:4444
   curl -sk https://localhost:8443/health   # expect 200
   ```
2. **Within 2 minutes:** online agent count from `/api/devices?status=online` should return to baseline. The agent reconnect loop (`agent/internal/client/client.go`) has backoff but reconnects within ~30 seconds typically.
3. **Within 5 minutes:** gRPC metric streams resume — check the dashboard for live CPU/memory updates on a known-good agent.
4. **Within 15 minutes:** check `docker logs sentinel-backend 2>&1 | grep -iE 'error|fatal|panic' | tail -50` for anything abnormal.
5. **Within 1 hour:** compare online-agent baseline to cutover baseline. Variance >2 agents = investigate.

**If any step fails:** execute rollback (below) immediately. Do not try to debug forward during the cutover window.

**Rollback:**
```bash
git revert HEAD             # reverts the docker-compose cutover commit
docker compose up -d sentinel-agent-gateway sentinel-backend
```
Expected rollback time: <2 minutes. Agents reconnect through Traefik as before.

---

## Phase 6 — Cleanup

**Goal:** Delete the no-longer-referenced Traefik config files.

**Effort:** small. Should be done **at least 48 hours after Phase 5** to give rollback a comfortable window.

**Files to delete:**
- `configs/traefik-agent/traefik.yml`
- `configs/traefik-agent/dynamic/tls.yml`
- `configs/traefik-agent/dynamic/middlewares.yml`
- `configs/traefik-agent/dynamic/agent-routes.yml`
- `configs/traefik-agent/` (directory)

**Also audit and update:**
- `scripts/deploy-production.sh`, `scripts/deploy-safe.sh` — grep for `sentinel-agent-gateway`, `traefik-agent`, `8443`
- `docker-compose.local.yml`, `docker-compose.test.yml` — same audit
- `ARCHITECTURE.md`, `TLS_ARCHITECTURE.md`, `TLS_IMPLEMENTATION.md`, `TLS_README.md` — update any diagrams or prose that reference the gateway
- `tests/e2e/playwright/helpers/agent-simulator.ts` — update if it connects to `sentinel-agent-gateway` by name
- `server/tests/agent_simulator.go`, `server/tests/integration/critical_paths_test.go` — audit

**Verification:**
- `grep -r "sentinel-agent-gateway\|traefik-agent" D:/Projects/Sentinel/ --exclude-dir=node_modules --exclude-dir=.git` returns **zero** non-historical hits
- `go build ./server/...` passes
- `docker compose config` parses cleanly
- E2E tests pass one full run on NEXUS

**Rollback:** `git revert`. Files come back.

---

## Effort Summary

| Phase | Goal | Effort | Risk |
|-------|------|--------|------|
| 0 | Pre-flight answers | small | none |
| 1 | Shared TLS factory | small | low |
| 2 | Agent rate limiter | small | low |
| 3 | Config field + subrouter helper | medium | low |
| 4 | Dual-running mTLS listener | medium | low (not in traffic path yet) |
| 5 | Cutover | small (LOC) | **high blast radius** |
| 6 | Cleanup | small | none |

Rough Go file count: **~6 files modified, ~4 files created, ~4 Traefik YAMLs deleted.** Total delta in Go code is modest — roughly 200–300 new lines plus a small refactor in `grpc/server.go` and `api/router.go`. This is **days, not weeks** of engineering — probably 2–3 engineering days for an engineer familiar with the codebase, plus a cutover window.

---

## Open Items (carried from architecture doc)

Before Phase 1 starts, Ron needs to decide:

1. **Separate Gin engine or shared?** → Plan assumes **separate**. Needs confirmation.
2. **Rate limiter storage** → Plan assumes **in-memory**. Confirm.
3. **Keep `/ws/agent` token-auth path on `:8443`?** → Plan assumes **yes**. Confirm.
4. **Cert hot-reload via `GetCertificate` callback?** → Plan assumes **no** (backend restart on cert rotation, same as today's Traefik-behind-cert-file behavior; Traefik does hot-reload, but given quarterly rotation this isn't hot-path). If hot-reload is desired, add ~15 lines to the TLS factory in Phase 1.
5. **Live verification that the current mTLS path works at all** (Open Question 1 in architecture doc). If it's broken, this refactor is also fixing an undetected bug.

---

## Pre-Execution Sanity Checks (day-of)

Run these one more time right before Phase 5:

```bash
# Confirm certs exist and are readable
docker exec sentinel-backend ls -la /certs/ca-cert.pem /certs/server-cert.pem /certs/server-key.pem

# Confirm the Go mTLS listener has been running successfully during Phase 4
docker logs sentinel-backend 2>&1 | grep "Agent mTLS server listening"
docker logs sentinel-backend 2>&1 | grep -i "agent mtls" | tail -20

# Confirm no panics in Phase 4 dual-running period
docker logs sentinel-backend --since=24h 2>&1 | grep -iE "panic|fatal" | wc -l
# Expected: 0

# Confirm current agent baseline before flip
curl -s -H "Authorization: Bearer $ADMIN_TOKEN" https://sentinelrmm.us/api/devices?status=online | jq '.data | length'
```

If any of these is unhealthy, **postpone Phase 5**.
