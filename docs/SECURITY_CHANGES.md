# Security Changes (v1.76.12)

## Critical Fixes

### 1. First-Run Admin Password Generation
- Default admin account no longer uses hardcoded password
- Random 24-character password generated on first database initialization
- Printed to stdout on first run -- must be changed immediately
- File: `server/cmd/sentinel/main.go:ensureFirstRunAdmin()`

### 2. Startup Configuration Validation
- Server refuses to start with missing or weak configuration
- Required: DATABASE_URL, JWT_SECRET (32+ chars), ENROLLMENT_TOKEN, REDIS_URL
- Fatal error with clear message listing all missing values
- File: `server/pkg/config/config.go:Validate()`

### 3. HTTP Timeouts
- ReadTimeout: 30 seconds
- WriteTimeout: 60 seconds
- IdleTimeout: 120 seconds
- ReadHeaderTimeout: 10 seconds
- Prevents slowloris and resource exhaustion attacks

### 4. Error Response Sanitization
- Internal error details no longer leaked to API clients
- Generic error messages returned; real errors logged server-side
- Helper: `server/internal/api/errors.go:sanitizedError()`

### 5. Session Security
- Refresh tokens checked against `expires_at` in SQL
- Expired sessions automatically cleaned up
- Password complexity: 8+ chars, uppercase, lowercase, digit, special char

### 6. API Key Least Privilege
- New API keys default to 'operator' role (previously auto-granted admin)
- Existing keys migrated to explicit 'admin' role
- Migration: 038_security_hardening.sql

### 7. WebSocket Security
- Dashboard connections require valid Origin header
- Per-client rate limiting: 600/min (agents), 120/min (dashboards)
- Query string token deprecated (logged as warning)

### 8. gRPC Security
- Agent ID validated against database on stream start
- Device ID cached (5-min TTL) to avoid per-metric DB queries
- Recording state protected by RWMutex

### 9. Agent Improvements
- Terminal session audit logging (session start/end, commands)
- Command output limited to 1MB per stream
- Agent log rotation (10MB, 5 files)

### 10. Infrastructure
- CI pipeline blocks on test/lint failures
- Backup/restore scripts for PostgreSQL and Redis
- Deployment rollback on health check failure
- Database migration 038: token prefix index, session index, FK constraints

## Known Limitations
- CSP allows `style-src 'unsafe-inline'` (required by runtime CSS injection)
- Agent command whitelist is compiled-in (not server-configurable)
- Terminal audit log requires separate rotation configuration
- .env secrets in git history require credential rotation
