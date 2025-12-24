# Sentinel RMM - Required Corrections

**Date:** 2025-12-23
**Priority:** CRITICAL
**Estimated Total Time:** 8 hours

This document outlines the specific corrections needed based on the architectural review. All P0 items must be completed before production deployment.

---

## PRIORITY 0: BLOCKING ISSUES (Must Fix Immediately)

### P0-1: Fix HubError Implementation (5 minutes)

**File:** `D:/Projects/Sentinel/server/internal/websocket/hub.go`

**Line:** 316

**Current Code:**
```go
type HubError struct {
    Message string
}
```

**Problem:** Does not implement the `error` interface.

**Fix:**
```go
type HubError struct {
    Message string
}

// Implement error interface
func (e *HubError) Error() string {
    return e.Message
}
```

**Alternative (Simpler):**
```go
// Replace lines 309-318 with:
var (
    ErrAgentNotConnected = errors.New("agent not connected")
    ErrSendFailed        = errors.New("failed to send message")
    ErrMessageTooLarge   = errors.New("message exceeds maximum size")
)
```

**Verification:**
```bash
cd D:/Projects/Sentinel/server
go test ./internal/websocket/... -v
```

---

### P0-2: Complete CSRF Token Rotation Integration (30 minutes)

**Issue:** The `RotateCSRFToken()` function exists but is never called when user privileges change.

**Step 1: Find User Update Handler**

**File:** `D:/Projects/Sentinel/server/internal/api/users.go` (if it exists) or `handlers.go`

Search for the function that updates user roles:
```bash
cd D:/Projects/Sentinel/server
grep -r "UPDATE users" internal/api/
```

**Step 2: Add CSRF Rotation**

**Add this code after successful role update:**
```go
func (r *Router) updateUser(c *gin.Context) {
    // ... existing user update logic ...

    // Check if role was changed
    var oldRole, newRole string
    // (Fetch oldRole from DB before update, newRole from request)

    if oldRole != newRole {
        // DC-001 FIX: Rotate CSRF token on privilege escalation
        newToken := middleware.RotateCSRFToken(c)
        log.Printf("Rotated CSRF token for user %s due to role change: %s -> %s",
            userID, oldRole, newRole)

        // Return new token to client
        c.JSON(http.StatusOK, gin.H{
            "user":      updatedUser,
            "csrfToken": newToken,  // Frontend must update stored token
        })
        return
    }

    c.JSON(http.StatusOK, gin.H{"user": updatedUser})
}
```

**Step 3: Update Frontend**

**File:** `D:/Projects/Sentinel/src/main/` (or wherever API calls are made)

Update the user update handler to save the new CSRF token:
```typescript
async function updateUser(userId: string, updates: UserUpdates) {
    const response = await fetch(`/api/users/${userId}`, {
        method: 'PUT',
        headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': getCurrentCSRFToken()
        },
        body: JSON.stringify(updates)
    });

    const data = await response.json();

    // If CSRF token was rotated, update it
    if (data.csrfToken) {
        saveCSRFToken(data.csrfToken);
    }

    return data.user;
}
```

**Verification:**
1. Update a user's role via the API
2. Verify the old CSRF token no longer works
3. Verify the new CSRF token is accepted

---

### P0-3: Fix Audit Context Handling (15 minutes)

**Issue:** Audit logging goroutines create new contexts, losing request cancellation.

**Files to Fix:**
1. `D:/Projects/Sentinel/server/internal/api/auth.go` (line 139)
2. `D:/Projects/Sentinel/server/internal/api/devices.go` (line 254)
3. `D:/Projects/Sentinel/server/internal/api/devices.go` (line 332)

**Current Pattern (WRONG):**
```go
go func() {
    auditCtx := context.Background()  // Creates orphaned context!
    _, auditErr := r.db.Pool().Exec(auditCtx, `
        INSERT INTO audit_log ...
    `, ...)
}()
```

**Fixed Pattern:**
```go
go func(ctx context.Context) {
    // Create timeout context to prevent hanging
    auditCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    _, auditErr := r.db.Pool().Exec(auditCtx, `
        INSERT INTO audit_log ...
    `, ...)
    if auditErr != nil {
        log.Printf("Failed to write audit log: %v", auditErr)
    }
}(c.Request.Context())  // Pass request context
```

**Apply to All 3 Locations:**

**auth.go (line ~139):**
```go
// DC-003 FIX: Log successful login to audit trail
go func(ctx context.Context) {
    auditCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    _, auditErr := r.db.Pool().Exec(auditCtx, `
        INSERT INTO audit_log (user_id, action, resource_type, resource_id, details, ip_address, user_agent)
        VALUES ($1, 'login_success', 'session', NULL, $2, $3, $4)
    `, user.ID, map[string]interface{}{"email": user.Email, "role": user.Role},
        c.ClientIP(), c.GetHeader("User-Agent"))
    if auditErr != nil {
        log.Printf("Failed to write audit log for login: %v", auditErr)
    }
}(c.Request.Context())
```

**devices.go (line ~254):**
```go
// DC-003 FIX: Log the device disable action to audit trail
go func(ctx context.Context) {
    auditCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    _, auditErr := r.db.Pool().Exec(auditCtx, `
        INSERT INTO audit_log (user_id, action, resource_type, resource_id, details, ip_address, user_agent)
        VALUES ($1, 'device_disabled', 'device', $2, $3, $4, $5)
    `, userID, id, map[string]interface{}{"hostname": hostname, "agentId": agentID},
        c.ClientIP(), c.GetHeader("User-Agent"))
    if auditErr != nil {
        log.Printf("Failed to write audit log for device disable: %v", auditErr)
    }
}(c.Request.Context())
```

**devices.go (line ~332):**
```go
// DC-003 FIX: Log the device enable action to audit trail
go func(ctx context.Context) {
    auditCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    _, auditErr := r.db.Pool().Exec(auditCtx, `
        INSERT INTO audit_log (user_id, action, resource_type, resource_id, details, ip_address, user_agent)
        VALUES ($1, 'device_enabled', 'device', $2, $3, $4, $5)
    `, userID, id, map[string]interface{}{"hostname": hostname},
        c.ClientIP(), c.GetHeader("User-Agent"))
    if auditErr != nil {
        log.Printf("Failed to write audit log for device enable: %v", auditErr)
    }
}(c.Request.Context())
```

**Verification:**
- Test that audit logs are written when requests complete normally
- Test that audit logs don't hang when requests are cancelled

---

## PRIORITY 1: HIGH PRIORITY (Complete This Week)

### P1-1: Inject Audit Logger (30 minutes)

**Purpose:** Centralize audit logging and enable testability.

**Step 1: Modify Router Struct**

**File:** `D:/Projects/Sentinel/server/internal/api/router.go`

**Change lines 17-22 from:**
```go
type Router struct {
    config *config.Config
    db     *database.DB
    cache  *cache.Cache
    hub    WebSocketHub
}
```

**To:**
```go
type Router struct {
    config      *config.Config
    db          *database.DB
    cache       *cache.Cache
    hub         WebSocketHub
    auditLogger *audit.Logger  // ADD THIS
}
```

**Step 2: Update Constructor**

**Change line 24 from:**
```go
func NewRouter(cfg *config.Config, db *database.DB, cache *cache.Cache, hub *websocket.Hub) *gin.Engine {
```

**To:**
```go
func NewRouter(cfg *config.Config, db *database.DB, cache *cache.Cache, hub *websocket.Hub, auditLogger *audit.Logger) *gin.Engine {
```

**Update initialization:**
```go
router := &Router{
    config:      cfg,
    db:          db,
    cache:       cache,
    hub:         hub,
    auditLogger: auditLogger,  // ADD THIS
}
```

**Step 3: Update Main**

**File:** `D:/Projects/Sentinel/server/cmd/server/main.go`

**Add before router initialization:**
```go
// Import at top
import (
    "github.com/sentinel/server/internal/audit"
    // ... existing imports
)

// In main():
auditLogger := audit.NewLogger(db.Pool())
router := api.NewRouter(cfg, db, cache, hub, auditLogger)
```

**Step 4: Refactor Audit Calls**

**Replace direct DB calls with audit logger:**

**auth.go (line ~139-149):**
```go
// BEFORE:
go func(ctx context.Context) {
    auditCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    _, auditErr := r.db.Pool().Exec(auditCtx, `INSERT INTO audit_log ...`)
}(c.Request.Context())

// AFTER:
go func(ctx context.Context) {
    auditCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    r.auditLogger.Log(auditCtx, audit.Entry{
        UserID:       &user.ID,
        Action:       audit.ActionLoginSuccess,
        ResourceType: audit.ResourceTypeSession,
        Details:      map[string]interface{}{"email": user.Email, "role": user.Role},
        IPAddress:    c.ClientIP(),
        UserAgent:    c.GetHeader("User-Agent"),
    })
}(c.Request.Context())
```

**Even Better - Use Helper:**
```go
r.auditLogger.LogSecurityEvent(c, audit.ActionLoginSuccess, true,
    map[string]interface{}{"email": user.Email, "role": user.Role})
```

---

### P1-2: Add Missing Audit Logs (1 hour)

**Add audit logging to these critical actions:**

#### 1. Failed Login Attempts

**File:** `D:/Projects/Sentinel/server/internal/api/auth.go`

**After line 98 (user not found):**
```go
if err != nil {
    bcrypt.CompareHashAndPassword(getDummyPasswordHash(), []byte(req.Password))

    // DC-003: Log failed login attempt
    r.auditLogger.LogSecurityEvent(c, audit.ActionLoginFailed, false,
        map[string]interface{}{"email": req.Email, "reason": "user_not_found"})

    middleware.RecordAuthResult(c, false)
    c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
    return
}
```

**After line 113 (wrong password):**
```go
if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
    // DC-003: Log failed login attempt
    r.auditLogger.LogSecurityEvent(c, audit.ActionLoginFailed, false,
        map[string]interface{}{"email": req.Email, "reason": "invalid_password"})

    middleware.RecordAuthResult(c, false)
    c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
    return
}
```

#### 2. Device Deletion

**File:** `D:/Projects/Sentinel/server/internal/api/devices.go`

**After successful deletion (line ~210):**
```go
// DC-003: Log device deletion
r.auditLogger.LogDeviceAction(c, audit.ActionDeviceDeleted, id,
    map[string]interface{}{"status": "uninstalling"})

c.JSON(http.StatusOK, gin.H{"message": "Device deleted"})
```

#### 3. Device Uninstall Command

**File:** `D:/Projects/Sentinel/server/internal/api/devices.go`

**After sending uninstall command (line ~761):**
```go
// Mark device as pending uninstall
if _, err := r.db.Pool().Exec(ctx, `
    UPDATE devices SET status = 'uninstalling', updated_at = NOW() WHERE id = $1
`, id); err != nil {
    log.Printf("Error updating device %s status to uninstalling: %v", id, err)
}

// DC-003: Log uninstall command
r.auditLogger.LogDeviceAction(c, audit.ActionDeviceUninstall, id,
    map[string]interface{}{
        "agentId":   agentID,
        "requestId": requestID,
    })

c.JSON(http.StatusOK, gin.H{...})
```

#### 4. Command Execution

**File:** `D:/Projects/Sentinel/server/internal/api/devices.go`

**After sending command to agent (line ~479):**
```go
// Update command status to running
if _, err := r.db.Pool().Exec(ctx, `
    UPDATE commands SET status = 'running', started_at = NOW() WHERE id = $1
`, commandID); err != nil {
    log.Printf("Error updating command %s status to running: %v", commandID, err)
}

// DC-003: Log command execution
r.auditLogger.LogAdminAction(c, audit.ActionCommandExecuted,
    audit.ResourceTypeCommand, &commandID,
    map[string]interface{}{
        "deviceId":    id.String(),
        "commandType": req.CommandType,
        "command":     req.Command,
    })

c.JSON(http.StatusOK, gin.H{...})
```

**Add Action Constants to audit.go:**
```go
// audit/audit.go - Add these constants if missing
const (
    ActionLoginFailed      = "login_failed"
    ActionDeviceDeleted    = "device_deleted"
    ActionDeviceUninstall  = "device_uninstall"
    ActionCommandExecuted  = "command_executed"
)
```

---

### P1-3: Create Database Migration System (4 hours)

**Step 1: Install golang-migrate**

```bash
cd D:/Projects/Sentinel/server
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

**Step 2: Extract Current Schema**

Connect to your development database and export the schema:

```bash
# PostgreSQL
pg_dump --schema-only --no-owner --no-privileges sentinel > schema.sql
```

**Step 3: Create Migration Files**

```bash
cd D:/Projects/Sentinel
mkdir -p migrations

# Create initial migration
cat > migrations/000001_initial_schema.up.sql << 'EOF'
-- Initial schema for Sentinel RMM

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Users table
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    role VARCHAR(20) NOT NULL DEFAULT 'viewer',
    is_active BOOLEAN DEFAULT true,
    last_login TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Sessions table
CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_hash VARCHAR(255) NOT NULL,
    ip_address VARCHAR(45),
    user_agent TEXT,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Devices table
CREATE TABLE devices (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    agent_id VARCHAR(255) UNIQUE NOT NULL,
    hostname VARCHAR(255),
    display_name VARCHAR(255),
    os_type VARCHAR(50),
    os_version VARCHAR(100),
    os_build VARCHAR(100),
    platform VARCHAR(100),
    platform_family VARCHAR(100),
    architecture VARCHAR(50),
    cpu_model VARCHAR(255),
    cpu_cores INTEGER,
    cpu_threads INTEGER,
    cpu_speed FLOAT,
    total_memory BIGINT,
    boot_time BIGINT,
    gpu JSONB,
    storage JSONB,
    serial_number VARCHAR(255),
    manufacturer VARCHAR(255),
    model VARCHAR(255),
    domain VARCHAR(255),
    agent_version VARCHAR(50),
    last_seen TIMESTAMP WITH TIME ZONE,
    status VARCHAR(20) DEFAULT 'offline',
    ip_address INET,
    public_ip INET,
    mac_address VARCHAR(17),
    tags TEXT[],
    metadata JSONB,
    is_disabled BOOLEAN DEFAULT false,
    disabled_at TIMESTAMP WITH TIME ZONE,
    disabled_by UUID REFERENCES users(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Add more tables as needed...
-- (Copy from your current schema)

-- Indexes
CREATE INDEX idx_devices_agent_id ON devices(agent_id);
CREATE INDEX idx_devices_status ON devices(status);
CREATE INDEX idx_devices_hostname ON devices(hostname);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

EOF

cat > migrations/000001_initial_schema.down.sql << 'EOF'
DROP TABLE IF EXISTS devices CASCADE;
DROP TABLE IF EXISTS sessions CASCADE;
DROP TABLE IF EXISTS users CASCADE;
DROP EXTENSION IF EXISTS "uuid-ossp";
EOF
```

**Step 4: Create Audit Log Migration**

```bash
cat > migrations/000002_add_audit_log.up.sql << 'EOF'
-- Audit log table for security tracking

CREATE TABLE audit_log (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id),
    action VARCHAR(100) NOT NULL,
    resource_type VARCHAR(50),
    resource_id UUID,
    details JSONB,
    ip_address VARCHAR(45),
    user_agent TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Indexes for audit queries
CREATE INDEX idx_audit_log_user_id ON audit_log(user_id);
CREATE INDEX idx_audit_log_action ON audit_log(action);
CREATE INDEX idx_audit_log_created_at ON audit_log(created_at);
CREATE INDEX idx_audit_log_resource ON audit_log(resource_type, resource_id);

EOF

cat > migrations/000002_add_audit_log.down.sql << 'EOF'
DROP TABLE IF EXISTS audit_log;
EOF
```

**Step 5: Update Application to Run Migrations**

**File:** `D:/Projects/Sentinel/server/cmd/server/main.go`

```go
import (
    "github.com/golang-migrate/migrate/v4"
    _ "github.com/golang-migrate/migrate/v4/database/postgres"
    _ "github.com/golang-migrate/migrate/v4/source/file"
)

func runMigrations(databaseURL string) error {
    m, err := migrate.New(
        "file://migrations",
        databaseURL,
    )
    if err != nil {
        return fmt.Errorf("failed to create migrate instance: %w", err)
    }

    if err := m.Up(); err != nil && err != migrate.ErrNoChange {
        return fmt.Errorf("failed to run migrations: %w", err)
    }

    log.Println("Database migrations completed successfully")
    return nil
}

func main() {
    // ... load config ...

    // Run migrations before starting server
    if err := runMigrations(cfg.DatabaseURL); err != nil {
        log.Fatalf("Migration error: %v", err)
    }

    // ... rest of main ...
}
```

**Step 6: Test Migrations**

```bash
# Test up
migrate -path ./migrations -database "postgresql://user:pass@localhost:5432/sentinel?sslmode=disable" up

# Test down
migrate -path ./migrations -database "postgresql://user:pass@localhost:5432/sentinel?sslmode=disable" down

# Check version
migrate -path ./migrations -database "postgresql://user:pass@localhost:5432/sentinel?sslmode=disable" version
```

---

## PRIORITY 2: MEDIUM PRIORITY (Next Sprint)

### P2-1: Add Frontend API Mock (10 minutes)

**File:** `D:/Projects/Sentinel/src/test/setup.ts`

**Add missing mock:**
```typescript
const createMockApi = () => ({
  // ... existing mocks ...

  // CW-003 FIX: Add missing getInfo method
  getInfo: vi.fn().mockResolvedValue({
    version: '1.0.0',
    serverUrl: 'http://localhost:8090',
    connected: true,
    serverStatus: 'online',
  }),

  // Also add any other missing methods used by components
});
```

**Verification:**
```bash
cd D:/Projects/Sentinel
npm test -- src/renderer/pages/Devices.test.tsx
```

---

### P2-2: Add Database Index for Pagination (5 minutes)

**Create New Migration:**

```bash
cat > migrations/000003_add_device_indexes.up.sql << 'EOF'
-- Optimize device list pagination query
CREATE INDEX IF NOT EXISTS idx_devices_hostname ON devices(hostname);

-- Optimize status filtering
CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status);

EOF

cat > migrations/000003_add_device_indexes.down.sql << 'EOF'
DROP INDEX IF EXISTS idx_devices_hostname;
DROP INDEX IF EXISTS idx_devices_status;
EOF
```

---

## TESTING CHECKLIST

After implementing all P0 fixes, verify:

### Backend Tests
```bash
cd D:/Projects/Sentinel/server

# Test WebSocket hub (P0-1)
go test ./internal/websocket/... -v

# Test middleware
go test ./internal/middleware/... -v

# Test audit package
go test ./internal/audit/... -v
```

### Frontend Tests
```bash
cd D:/Projects/Sentinel

# Test with fixed mocks (P2-1)
npm test -- src/renderer/pages/Devices.test.tsx
```

### Integration Tests

1. **Test Timing Attack Fix (CW-004)**
   ```bash
   # Measure login timing for existing vs non-existing users
   time curl -X POST http://localhost:8080/api/auth/login \
     -H "Content-Type: application/json" \
     -d '{"email":"admin@example.com","password":"wrong"}'

   time curl -X POST http://localhost:8080/api/auth/login \
     -H "Content-Type: application/json" \
     -d '{"email":"nonexistent@example.com","password":"wrong"}'

   # Times should be within 50ms of each other
   ```

2. **Test CSRF Token Rotation (P0-2)**
   ```bash
   # 1. Login and get CSRF token
   TOKEN=$(curl -c cookies.txt -X POST http://localhost:8080/api/auth/login \
     -H "Content-Type: application/json" \
     -d '{"email":"admin@example.com","password":"admin123"}' \
     | jq -r .csrfToken)

   # 2. Update user role (this should rotate token)
   NEW_TOKEN=$(curl -b cookies.txt -X PUT http://localhost:8080/api/users/123 \
     -H "Content-Type: application/json" \
     -H "X-CSRF-Token: $TOKEN" \
     -d '{"role":"admin"}' \
     | jq -r .csrfToken)

   # 3. Verify old token no longer works
   curl -b cookies.txt -X POST http://localhost:8080/api/devices/123/disable \
     -H "X-CSRF-Token: $TOKEN"
   # Should return 403 Forbidden

   # 4. Verify new token works
   curl -b cookies.txt -X POST http://localhost:8080/api/devices/123/disable \
     -H "X-CSRF-Token: $NEW_TOKEN"
   # Should return 200 OK
   ```

3. **Test Audit Logging (P1-2)**
   ```sql
   -- Check audit log entries are being created
   SELECT * FROM audit_log
   ORDER BY created_at DESC
   LIMIT 10;

   -- Should see entries for:
   -- - login_success
   -- - login_failed
   -- - device_disabled
   -- - device_enabled
   -- - device_deleted
   -- - device_uninstall
   -- - command_executed
   ```

---

## COMPLETION CRITERIA

**P0 Complete When:**
- [ ] All `go test` commands pass
- [ ] `HubError` implements `error` interface
- [ ] CSRF token rotates on role change
- [ ] Audit context handling uses request context
- [ ] No goroutine leaks in audit logging

**P1 Complete When:**
- [ ] Audit logger injected into Router
- [ ] Failed login attempts logged
- [ ] Device deletion logged
- [ ] Device uninstall logged
- [ ] Command execution logged
- [ ] Database migrations run successfully on fresh DB
- [ ] Migration rollback works (test with `migrate down`)

**P2 Complete When:**
- [ ] All frontend tests pass
- [ ] Device list query performance acceptable (< 100ms for 1000 devices)

---

## ESTIMATED TIMELINE

| Priority | Time | Recommended Completion |
|----------|------|----------------------|
| P0-1 (HubError) | 5 min | Today |
| P0-2 (CSRF Rotation) | 30 min | Today |
| P0-3 (Audit Context) | 15 min | Today |
| **P0 Total** | **50 min** | **End of Day** |
| P1-1 (Inject Logger) | 30 min | Tomorrow |
| P1-2 (Add Audit Logs) | 1 hour | Tomorrow |
| P1-3 (Migrations) | 4 hours | This Week |
| **P1 Total** | **5.5 hours** | **Within 3 Days** |
| P2-1 (Frontend Mock) | 10 min | Next Week |
| P2-2 (DB Index) | 5 min | Next Week |
| **P2 Total** | **15 min** | **Next Sprint** |

**Grand Total: ~6.5 hours of focused work**

---

## NOTES FOR LEAD ENGINEER

1. **P0 items are blocking.** Do not merge to main or deploy until these are fixed.

2. **Testing is critical.** The timing attack fix and CSRF rotation must be verified manually.

3. **Database migrations** are the longest task (~4 hours) but are essential for production.

4. **Audit logging completeness** improves security posture and compliance. Missing logs are a gap.

5. **Repository pattern** (not in this document) would improve architecture but is not blocking.

---

**Document Version:** 1.0
**Last Updated:** 2025-12-23
**Status:** Active - Awaiting Implementation
