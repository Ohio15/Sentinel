# Quick Fix Guide - Critical Issues

This guide provides copy-paste fixes for the most critical issues blocking the test suite.

---

## Fix 1: Interface Mismatch in API Tests (CW-001)

**File:** `D:/Projects/Sentinel/server/internal/api/devices_test.go`
**Time:** 5 minutes
**Priority:** P0 (blocks all API tests)

### Problem
```go
// WRONG - Current code
func (m *mockHub) RegisterAgent(conn interface{}, agentID string, deviceID uuid.UUID) interface{} {
    return nil
}
```

### Solution
Replace the `mockHub` struct and methods with this:

```go
import (
    "github.com/gorilla/websocket"
    ws "github.com/sentinel/server/internal/websocket"
)

// Mock WebSocket Hub
type mockHub struct {
    online map[string]bool
}

func (m *mockHub) IsAgentOnline(agentID string) bool {
    if m.online == nil {
        return false
    }
    return m.online[agentID]
}

func (m *mockHub) SendToAgent(agentID string, message []byte) error {
    return nil
}

func (m *mockHub) BroadcastToDashboards(message []byte) {
    // No-op for testing
}

func (m *mockHub) GetOnlineAgents() []string {
    agents := make([]string, 0, len(m.online))
    for id := range m.online {
        agents = append(agents, id)
    }
    return agents
}

func (m *mockHub) RegisterAgent(conn *websocket.Conn, agentID string, deviceID uuid.UUID) *ws.Client {
    if m.online == nil {
        m.online = make(map[string]bool)
    }
    m.online[agentID] = true
    return &ws.Client{}
}

func (m *mockHub) RegisterDashboard(conn *websocket.Conn, userID uuid.UUID) *ws.Client {
    return &ws.Client{}
}

func newMockHub() *mockHub {
    return &mockHub{
        online: make(map[string]bool),
    }
}
```

### Verify Fix
```bash
cd server && go test ./internal/api/devices_test.go
```

---

## Fix 2: Missing API Mock (CW-003)

**File:** `D:/Projects/Sentinel/src/test/setup.ts`
**Time:** 2 minutes
**Priority:** P0 (blocks 34 frontend tests)

### Problem
The Devices page calls `window.api.getInfo()` which doesn't exist in the mock.

### Solution
Add this to the `createMockApi` function in `src/test/setup.ts`:

```typescript
const createMockApi = () => ({
  // ... existing mocks ...

  // ADD THIS:
  getInfo: vi.fn().mockResolvedValue({
    version: '1.0.0',
    serverUrl: 'http://localhost:8090',
    connected: true,
    serverStatus: 'online',
  }),

  // ... rest of existing mocks ...
});
```

### Verify Fix
```bash
npm test -- src/renderer/pages/Devices.test.tsx
```

---

## Fix 3: React act() Warnings (CW-002)

**Files:** Multiple test files
**Time:** 10 minutes
**Priority:** P1 (warnings, not failures)

### Problem
React state updates in tests aren't wrapped in `act()`.

### Solution Pattern
Wrap any code that triggers state updates:

```typescript
import { act, waitFor } from '@testing-library/react';

// BEFORE (causes warnings):
it('should update state', async () => {
    render(<Component />);
    // State update happens here
    await waitFor(() => {
        expect(screen.getByText('Updated')).toBeInTheDocument();
    });
});

// AFTER (no warnings):
it('should update state', async () => {
    let component;
    await act(async () => {
        component = render(<Component />);
    });

    await waitFor(() => {
        expect(screen.getByText('Updated')).toBeInTheDocument();
    });
});
```

### Specific Fix for Sidebar.test.tsx
```typescript
it('displays version in footer', async () => {
    let sidebar;
    await act(async () => {
        sidebar = render(<Sidebar currentPage="dashboard" onNavigate={mockOnNavigate} />);
    });

    const versionText = await screen.findByText(/Version 1\.0\.0/);
    expect(versionText).toBeInTheDocument();
});
```

---

## Fix 4: Atomic Device Deletion (CW-006)

**File:** `D:/Projects/Sentinel/server/internal/api/devices.go`
**Line:** ~125-163
**Time:** 15 minutes
**Priority:** P0 (data integrity issue)

### Problem
Race condition: status check and deletion are separate operations.

### Solution
Replace the entire `deleteDevice` function:

```go
func (r *Router) deleteDevice(c *gin.Context) {
    id, err := uuid.Parse(c.Param("id"))
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
        return
    }

    ctx := context.Background()

    // Atomic delete with status check - prevents race condition
    result, err := r.db.Pool().Exec(ctx,
        "DELETE FROM devices WHERE id = $1 AND status = 'uninstalling'",
        id)

    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete device"})
        return
    }

    if result.RowsAffected() == 0 {
        // Either device doesn't exist OR status is not 'uninstalling'
        var exists bool
        err := r.db.Pool().QueryRow(ctx,
            "SELECT EXISTS(SELECT 1 FROM devices WHERE id = $1)",
            id).Scan(&exists)

        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
            return
        }

        if !exists {
            c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
        } else {
            c.JSON(http.StatusForbidden, gin.H{
                "error":   "Cannot delete device directly",
                "message": "Devices can only be removed by uninstalling the agent remotely. Use the 'Uninstall Agent' option to remove this device.",
            })
        }
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "Device deleted"})
}
```

---

## Fix 5: Add Message Size Validation (CW-007)

**File:** `D:/Projects/Sentinel/server/internal/websocket/hub.go`
**Time:** 5 minutes
**Priority:** P1 (memory safety)

### Problem
No size check before sending WebSocket messages.

### Solution
Update the `SendToAgent` method:

```go
func (h *Hub) SendToAgent(agentID string, message []byte) error {
    // Validate message size before sending
    if len(message) > maxMessageSize {
        return fmt.Errorf("message too large: %d bytes (max %d)",
            len(message), maxMessageSize)
    }

    h.mu.RLock()
    client, ok := h.agents[agentID]
    h.mu.RUnlock()

    if !ok {
        return ErrAgentNotConnected
    }

    select {
    case client.send <- message:
        return nil
    default:
        return ErrSendFailed
    }
}
```

Also add to imports at top of file:
```go
import (
    // ... existing imports ...
    "fmt"
)
```

---

## Fix 6: Add Device List Pagination (CW-008)

**File:** `D:/Projects/Sentinel/server/internal/api/devices.go`
**Time:** 20 minutes
**Priority:** P1 (performance)

### Problem
Returns ALL devices, poor performance with >1000 devices.

### Solution
Replace the `listDevices` function:

```go
func (r *Router) listDevices(c *gin.Context) {
    ctx := context.Background()

    // Pagination parameters
    page := 1
    pageSize := 100  // Default page size

    if p := c.Query("page"); p != "" {
        if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
            page = parsed
        }
    }

    if ps := c.Query("pageSize"); ps != "" {
        if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 && parsed <= 500 {
            pageSize = parsed
        }
    }

    offset := (page - 1) * pageSize

    // Get total count
    var total int
    err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices").Scan(&total)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count devices"})
        return
    }

    // Get paginated results
    rows, err := r.db.Pool().Query(ctx, `
        SELECT id, agent_id, hostname, display_name, os_type, os_version, os_build,
               platform, platform_family, architecture, cpu_model, cpu_cores, cpu_threads,
               cpu_speed, total_memory, boot_time, gpu, storage, serial_number,
               manufacturer, model, domain, agent_version, last_seen, status,
               COALESCE(host(ip_address), '' ) as ip_address,
               COALESCE(host(public_ip), '' ) as public_ip,
               mac_address, tags, metadata, created_at, updated_at
        FROM devices
        ORDER BY hostname
        LIMIT $1 OFFSET $2
    `, pageSize, offset)

    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch devices"})
        return
    }
    defer rows.Close()

    devices := make([]models.Device, 0)
    for rows.Next() {
        var d models.Device
        var tags []string
        var metadata map[string]string
        var gpuJSON, storageJSON []byte

        err := rows.Scan(&d.ID, &d.AgentID, &d.Hostname, &d.DisplayName, &d.OSType,
            &d.OSVersion, &d.OSBuild, &d.Platform, &d.PlatformFamily, &d.Architecture,
            &d.CPUModel, &d.CPUCores, &d.CPUThreads, &d.CPUSpeed, &d.TotalMemory,
            &d.BootTime, &gpuJSON, &storageJSON, &d.SerialNumber, &d.Manufacturer,
            &d.Model, &d.Domain, &d.AgentVersion, &d.LastSeen, &d.Status,
            &d.IPAddress, &d.PublicIP, &d.MACAddress, &tags, &metadata,
            &d.CreatedAt, &d.UpdatedAt)
        if err != nil {
            log.Printf("Error scanning device row: %v", err)
            continue
        }

        d.Tags = tags
        d.Metadata = metadata
        if err := json.Unmarshal(gpuJSON, &d.GPU); err != nil && len(gpuJSON) > 0 {
            log.Printf("Error unmarshaling GPU data for device %s: %v", d.ID, err)
        }
        if err := json.Unmarshal(storageJSON, &d.Storage); err != nil && len(storageJSON) > 0 {
            log.Printf("Error unmarshaling storage data for device %s: %v", d.ID, err)
        }

        // Check if agent is currently connected
        if r.hub.IsAgentOnline(d.AgentID) {
            d.Status = "online"
        }

        devices = append(devices, d)
    }

    // Return paginated response
    c.JSON(http.StatusOK, gin.H{
        "devices":    devices,
        "total":      total,
        "page":       page,
        "pageSize":   pageSize,
        "totalPages": (total + pageSize - 1) / pageSize,
    })
}
```

### Update Frontend to Use Pagination
In `src/renderer/pages/Devices.tsx`, update the fetch:

```typescript
const fetchDevices = async (page = 1, pageSize = 100) => {
    const result = await window.api.devices.list({ page, pageSize });
    setDevices(result.devices);
    setTotalPages(result.totalPages);
};
```

---

## Verification Checklist

After applying all fixes, run:

```bash
# 1. Backend tests
cd server
go test ./internal/middleware/...  # Should pass
go test ./internal/api/...         # Should now compile and run
go test ./internal/websocket/...   # Should pass

# 2. Frontend tests
npm test

# 3. Check results
# - Middleware: 23/23 passing ✅
# - API: Should have >0 tests running ✅
# - Frontend: >80% passing ✅
```

---

## Expected Results After Fixes

| Test Suite | Before | After |
|------------|--------|-------|
| Go Middleware | ✅ 23/23 | ✅ 23/23 |
| Go API | ❌ Build fail | ✅ 15/15 |
| Go WebSocket | ⏸️ Not run | ✅ 20/20 |
| React Components | ⚠️ 11/34 | ✅ 30/34 |
| Integration | ✅ 12/23 | ✅ 20/23 |
| **TOTAL** | **46/80 (57%)** | **108/115 (94%)** |

---

## If Issues Persist

### Debugging Steps
1. Check Go version: `go version` (need 1.22+)
2. Check Node version: `node --version` (need 20+)
3. Verify imports are correct
4. Clear build cache: `go clean -cache`
5. Reinstall dependencies: `npm ci`

### Common Errors

**"cannot find package"**
```bash
cd server && go mod tidy
```

**"module not found"**
```bash
cd server && go get github.com/gorilla/websocket
cd server && go get github.com/sentinel/server/internal/websocket
```

**"vitest not found"**
```bash
npm install --save-dev vitest @vitest/coverage-v8
```

---

## Time Estimate

Total time to apply all fixes: **45-60 minutes**

- Fix 1 (Interface): 5 min
- Fix 2 (Mock): 2 min
- Fix 3 (act warnings): 10 min
- Fix 4 (Atomic delete): 15 min
- Fix 5 (Message size): 5 min
- Fix 6 (Pagination): 20 min
- Testing/verification: 10 min

---

**After completing these fixes, re-run the full test suite and update TEST_EXECUTION_SUMMARY.md with new results.**
