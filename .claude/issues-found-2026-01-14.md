# Issues Found During Session: 2026-01-14

## Task Context
Working on: Installation portal auto-download not working - needed to add frontend route and fix URL generation

---

## Unrelated Issues Discovered

### 1. Pre-existing TypeScript Errors
**Files affected:**
- `src/renderer/App.tsx` - Props mismatch on TicketsKanban, TicketsCalendar (onViewChange not in types)
- `src/renderer/components/layout/Header.tsx` - Optional chaining issues with `window.api.server`
- `src/renderer/components/layout/Sidebar.test.tsx` - Missing jest-dom matchers
- `src/renderer/components/RemoteDesktop.tsx` - Multiple 'possibly null' errors on wsService

**Impact:** Build warnings, type safety compromised
**Recommendation:** Run `npm run build` fails silently; these need fixing

### 2. Export Inconsistency Pattern
**Description:** Mix of default and named exports across pages:
- `InstallationPortal.tsx` uses `export default function`
- Most other pages use `export function X` (named exports)

**Impact:** Import confusion, potential runtime errors
**Recommendation:** Standardize on one pattern

### 3. Docker Compose Warnings
**Messages:**
- `version` attribute is obsolete
- Volume `sentinel_postgres-data` not created by Docker Compose

**Impact:** None immediate, but future compatibility
**Recommendation:** Remove `version: '3.8'` from docker-compose.yml, mark volume as external

### 4. File Edit Tool Reliability
**Description:** Multiple "File has been unexpectedly modified" errors when trying to edit `App.tsx`
**Workaround Used:** Node.js scripts to make edits
**Impact:** Development friction
**Recommendation:** Investigate if file watchers or IDE causing conflicts

### 5. Missing publicApi Service
**Description:** `InstallationPortal.tsx` was importing `publicApi` from `@/services/publicApi` but the file didn't exist
**Status:** FIXED in this session - created the service
**Impact:** Would have caused runtime error

---

## Security Issues Found (Separate Audit)
See: `.claude/security-audit-public-endpoints.md`

---

## Action Items
- [ ] Fix TypeScript errors in App.tsx props
- [ ] Fix Header.tsx optional chaining
- [ ] Add jest-dom types/setup for tests
- [ ] Fix RemoteDesktop.tsx null checks
- [ ] Standardize export pattern
- [ ] Update docker-compose.yml to remove obsolete version
- [ ] Implement security audit recommendations
