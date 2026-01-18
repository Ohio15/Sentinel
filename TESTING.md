# Sentinel RMM Testing & Deployment Guide

This document describes the testing infrastructure designed to prevent "fix one thing, break another" deployment issues.

## Quick Reference

```bash
# Before ANY deployment - validate critical paths
./scripts/validate-critical-paths.sh https://sentinelrmm.us:4443
# PowerShell: .\scripts\Validate-CriticalPaths.ps1 -BaseUrl "https://sentinelrmm.us:4443"

# Full agent connection test (validates real agent-to-server communication)
./scripts/test-agent-flow.sh https://sentinelrmm.us:4443 --token "enrollment-token"
# PowerShell: .\scripts\test-agent-flow.ps1 -BaseUrl "https://sentinelrmm.us:4443" -EnrollmentToken "token"

# Safe deployment with backup and rollback
./scripts/deploy-safe.sh

# If something breaks - quick rollback
./scripts/rollback.sh

# Continuous monitoring
./scripts/monitor-production.sh --continuous
```

## Two Levels of Testing

### Level 1: API Smoke Tests (validate-critical-paths)
Fast tests that verify endpoints respond correctly. Good for quick checks.
- Tests HTTP status codes
- Validates error handling
- Checks authentication enforcement
- **Does NOT** test actual agent communication

### Level 2: Full Agent Flow Tests (test-agent-flow)
Complete end-to-end tests that simulate a real agent connecting.
- Creates actual enrollment
- Establishes WebSocket connection over TLS
- Sends/receives messages
- Validates the complete production flow

## Testing Workflow

```
┌─────────────────────────────────────────────────────────────┐
│ 1. Make Code Changes                                        │
│    └─ Edit files, fix bugs, add features                    │
└─────────────┬───────────────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────────┐
│ 2. Local Validation                                         │
│    └─ ./scripts/validate-critical-paths.sh localhost:8090   │
│    └─ Or start test environment:                            │
│       docker-compose -f docker-compose.test.yml up          │
└─────────────┬───────────────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────────┐
│ 3. Run Integration Tests                                    │
│    └─ cd server                                             │
│    └─ go test -v ./tests/integration/... -base-url=...      │
└─────────────┬───────────────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────────┐
│ 4. Commit & Push                                            │
│    └─ git add . && git commit -m "..." && git push          │
└─────────────┬───────────────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────────┐
│ 5. Safe Deployment                                          │
│    └─ ./scripts/deploy-safe.sh                              │
│    └─ Automatic backup, health checks, rollback on failure  │
└─────────────┬───────────────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────────┐
│ 6. Post-Deployment Verification                             │
│    └─ ./scripts/validate-critical-paths.sh                  │
│    └─ If issues: ./scripts/rollback.sh                      │
└─────────────────────────────────────────────────────────────┘
```

## Scripts

### validate-critical-paths.sh

**Purpose**: Validates all critical API paths are working correctly before deployment.

**Tests performed**:
1. Health endpoints (/health, /health/ready, /health/live)
2. Public API endpoints (agent version, bootstrap)
3. Installation code flow (validates invalid codes return proper errors)
4. Authentication endpoints (rejects bad credentials)
5. Protected endpoint security (requires auth)
6. WebSocket endpoints (reachable)
7. Error handling (no database details leaked)

**Usage**:
```bash
# Test remote production
./scripts/validate-critical-paths.sh https://sentinelrmm.us:4443

# Test local development
./scripts/validate-critical-paths.sh http://localhost:8090

# With verbose output
VERBOSE=true ./scripts/validate-critical-paths.sh
```

**Exit codes**:
- 0: All tests passed - safe to deploy
- 1: Tests failed - DO NOT deploy

### Validate-CriticalPaths.ps1 (PowerShell)

Same tests as the bash script, for Windows development.

```powershell
# Test production
.\scripts\Validate-CriticalPaths.ps1 -BaseUrl "https://sentinelrmm.us:4443"

# Test local
.\scripts\Validate-CriticalPaths.ps1 -BaseUrl "http://localhost:8090" -Verbose
```

### deploy-safe.sh

**Purpose**: Deploy to production with safety measures.

**What it does**:
1. Pre-deployment validation (compile check, existing production health)
2. Backup current Docker images and database
3. Pull latest code from git
4. Build new Docker images (--no-cache)
5. Deploy new version
6. Wait for services to stabilize
7. Post-deployment validation
8. Auto-rollback if validation fails

**Usage**:
```bash
# Full deployment with backup
./scripts/deploy-safe.sh

# Skip backup (for minor changes)
./scripts/deploy-safe.sh --skip-backup

# Don't auto-rollback on failure
./scripts/deploy-safe.sh --no-rollback
```

### rollback.sh

**Purpose**: Quick rollback to a previous version.

**Usage**:
```bash
# Rollback to most recent backup
./scripts/rollback.sh

# Rollback to specific backup
./scripts/rollback.sh backup-20260118-120000

# List available backups
./scripts/rollback.sh --list
```

### test-agent-flow.sh / test-agent-flow.ps1

**Purpose**: Full end-to-end test of agent-to-server communication over HTTPS/WSS.

**What it tests**:
1. Enrollment token validation (via installation code or direct token)
2. Agent enrollment (creates real device in database)
3. WebSocket connection over TLS/WSS
4. Heartbeat message exchange
5. Bidirectional communication

**Usage (Bash)**:
```bash
# With enrollment token
./scripts/test-agent-flow.sh https://sentinelrmm.us:4443 --token "your-enrollment-token"

# With installation code
./scripts/test-agent-flow.sh https://sentinelrmm.us:4443 --code "XXXX-XXXX"

# Auto-fetch token (if public endpoint available)
./scripts/test-agent-flow.sh https://sentinelrmm.us:4443
```

**Usage (PowerShell)**:
```powershell
# With enrollment token
.\scripts\test-agent-flow.ps1 -BaseUrl "https://sentinelrmm.us:4443" -EnrollmentToken "token"

# With installation code
.\scripts\test-agent-flow.ps1 -BaseUrl "https://sentinelrmm.us:4443" -InstallationCode "XXXX-XXXX"
```

**Requirements (Bash)**:
- `curl` - HTTP requests
- `jq` - JSON parsing (recommended)
- `websocat` - WebSocket testing (install: `cargo install websocat`)

**Why this is important**:
The API smoke tests only verify endpoints respond with correct status codes. This test validates that a real agent can:
- Successfully enroll with the server
- Establish a WebSocket connection over TLS
- Exchange messages in both directions

This catches issues that API tests miss, such as:
- TLS/mTLS certificate problems
- WebSocket upgrade failures
- Authentication header handling
- Database connection issues during enrollment

### monitor-production.sh

**Purpose**: Continuous monitoring of production health.

**Usage**:
```bash
# Single check
./scripts/monitor-production.sh

# Continuous monitoring (every 5 minutes)
./scripts/monitor-production.sh --continuous

# With webhook alerts
./scripts/monitor-production.sh --alert-webhook https://hooks.slack.com/...

# Custom interval (seconds)
./scripts/monitor-production.sh --continuous --interval 60
```

**Crontab setup** (every 5 minutes):
```bash
*/5 * * * * /opt/sentinel/scripts/monitor-production.sh >> /var/log/sentinel-monitor.log 2>&1
```

## Test Environment

### docker-compose.test.yml

Isolated test environment that doesn't affect production.

**Differences from production**:
- Different ports (8091 instead of 8090, 5433 instead of 5432, 6380 instead of 6379)
- Separate database (sentinel_test)
- Ephemeral storage (tmpfs for database)
- Test credentials

**Usage**:
```bash
# Start test environment
docker-compose -f docker-compose.test.yml up -d

# Wait for startup
sleep 30

# Run tests against test environment
./scripts/validate-critical-paths.sh http://localhost:8091

# Stop and clean up (removes all data)
docker-compose -f docker-compose.test.yml down -v
```

## Go Integration Tests

Located in `server/tests/integration/`

**Run tests**:
```bash
cd server

# Against test environment
go test -v ./tests/integration/... -base-url=http://localhost:8091

# Against production (skips destructive tests)
go test -v ./tests/integration/... -base-url=https://sentinelrmm.us:4443 -skip-destructive
```

**Tests included**:
- Health endpoints
- Installation code validation (the critical flow that broke)
- Authentication rejection
- Protected endpoint security
- Error handling (no database leaks)
- SQL injection protection
- Malformed input handling

## Critical Paths Tested

These are the flows that have historically broken:

### 1. Installation Code Flow
```
User clicks "Generate Install Code" in dashboard
  → POST /api/admin/installation-codes
  → Returns { code: "XXXX-XXXX" }

User copies code to installer
  → GET /api/public/install/validate-code?code=XXXX-XXXX
  → Returns { valid: true, enrollmentToken: "..." }
                 ↓
               (OR)
  → Returns { valid: false, status: "invalid" }

Agent uses token to enroll
  → POST /api/agent/enroll
  → Returns { deviceId: "..." }
```

**What can break**:
- pgx vs database/sql error handling (`ErrNoRows`)
- Soft-delete filtering (`deleted_at IS NULL`)
- Variable name typos
- Rate limiting
- CORS issues

### 2. Health Check Flow
```
Load balancer / monitoring
  → GET /health
  → Returns { status: "healthy" }

  → GET /health/ready
  → Returns { database: true, redis: true }
```

### 3. Authentication Flow
```
User login
  → POST /api/auth/login
  → Returns { token: "..." }

Protected API call
  → GET /api/devices
  → Header: Authorization: Bearer <token>
```

## Troubleshooting

### Tests failing on installation code validation
Check `server/internal/api/installation_codes.go`:
- Error handling uses `errors.Is(err, pgx.ErrNoRows)` not `err == sql.ErrNoRows`
- Query includes `AND deleted_at IS NULL`
- Variable names are correct

### Health check failing
1. Check container status: `docker-compose ps`
2. Check logs: `docker-compose logs backend`
3. Check database: `docker exec sentinel-postgres pg_isready`
4. Check Redis: `docker exec sentinel-redis redis-cli ping`

### Rollback not working
1. List available backups: `./scripts/rollback.sh --list`
2. Check Docker images: `docker images sentinel-backend`
3. Check database backups: `ls -la backups/`

## Best Practices

1. **Always run validation before deployment**
   ```bash
   ./scripts/validate-critical-paths.sh && ./scripts/deploy-safe.sh
   ```

2. **Test locally first**
   ```bash
   docker-compose -f docker-compose.test.yml up -d
   # Make changes, test...
   docker-compose -f docker-compose.test.yml down -v
   ```

3. **Keep backups**
   - Don't use `--skip-backup` unless you're sure
   - Periodically clean old backups: `find backups/ -mtime +30 -delete`

4. **Monitor after deployment**
   ```bash
   ./scripts/monitor-production.sh --continuous
   ```

5. **Commit testing changes**
   - These scripts should be in version control
   - Update tests when adding new critical features
