# Sentinel RMM Security Review Report

**Date:** December 17, 2024
**Version Reviewed:** 1.55.1
**Reviewer:** Security Audit

---

## Executive Summary

This security review identified **24 security vulnerabilities** across the Sentinel RMM application and agent. Of these:
- **6 CRITICAL** issues requiring immediate attention
- **9 HIGH** severity issues
- **7 MEDIUM** severity issues
- **2 LOW** severity issues

The most significant risks are:
1. **All network communications are unencrypted** (HTTP/WS/gRPC without TLS)
2. **No input validation on remote command execution** (arbitrary code execution)
3. **Path traversal vulnerabilities in file operations**
4. **Hardcoded credentials and weak token management**

---

## Critical Findings

### C1. Unencrypted gRPC Data Plane
**Severity:** CRITICAL
**Files:** `src/main/grpc-server.ts:54`, `agent/internal/grpc/dataplane.go:65`

```typescript
// Server
grpc.ServerCredentials.createInsecure()

// Agent
grpc.WithTransportCredentials(insecure.NewCredentials())
```

**Risk:** All metrics, inventory data, logs, and diagnostics transmitted in plaintext. Vulnerable to network eavesdropping and MITM attacks.

**Recommendation:** Implement mutual TLS (mTLS) with certificate validation.

---

### C2. Unencrypted WebSocket and HTTP Communications
**Severity:** CRITICAL
**File:** `src/main/server.ts:144,190,239`

All endpoints use `http://` and `ws://` protocols:
- Agent enrollment over plaintext HTTP
- Command execution over plaintext WebSocket
- File transfers unencrypted
- Terminal sessions unencrypted

**Risk:** Authentication tokens, commands, and sensitive data exposed on network.

**Recommendation:** Enable TLS/SSL for all communications. Use `https://` and `wss://`.

---

### C3. No Command Input Validation (Remote Code Execution)
**Severity:** CRITICAL
**File:** `agent/internal/executor/executor.go:36-95`

```go
// Commands passed directly to shell with no validation
cmd := exec.Command("powershell", "-NonInteractive", "-Command", command)
cmd := exec.Command("cmd", "/C", command)
cmd := exec.Command("bash", "-c", command)
```

**Risk:** Arbitrary command execution with SYSTEM privileges. No whitelist, length limits, or dangerous command filtering.

**Recommendation:**
- Implement command whitelist for allowed operations
- Add input sanitization and validation
- Consider sandboxed execution environment

---

### C4. Path Traversal in File Operations
**Severity:** CRITICAL
**File:** `agent/internal/filetransfer/filetransfer.go:52-415`

```go
path = filepath.Clean(path)  // Insufficient - doesn't prevent traversal
```

Functions affected:
- `ListDirectory` - Can enumerate any directory
- `ReadFile` - Can read any file (system files, credentials)
- `WriteFile` - Can write to any location
- `DeleteFile` - Can delete entire directory trees

**Risk:** Full filesystem access. Attackers can read `/etc/shadow`, write malware, delete system files.

**Recommendation:**
- Implement path boundary validation
- Check resolved paths stay within allowed directories
- Add symlink attack protection with `filepath.EvalSymlinks`

---

### C5. Hardcoded Database Password
**Severity:** CRITICAL
**File:** `src/main/database.ts:27`

```typescript
password: process.env.DB_PASSWORD || config?.password || 'sentinel_dev_password_32chars!!',
```

**Risk:** Default credentials in production. Password exposed in source code and compiled application.

**Recommendation:** Remove hardcoded password. Require environment variable configuration.

---

### C6. Disabled SSL Certificate Validation
**Severity:** CRITICAL
**File:** `src/main/database.ts:46`

```typescript
poolConfig.ssl = { rejectUnauthorized: false };
```

**Risk:** MITM attacks on database connection. Attacker can intercept/modify all database traffic.

**Recommendation:** Set `rejectUnauthorized: true` and configure proper CA certificates.

---

## High Severity Findings

### H1. Wildcard CORS Configuration
**File:** `src/main/server.ts:62`

```typescript
res.header('Access-Control-Allow-Origin', '*');
```

**Risk:** Cross-site request forgery (CSRF) attacks possible from any origin.

---

### H2. Enrollment Token Embedded in Binaries
**File:** `src/main/server.ts:13-36`

Tokens embedded as plaintext with simple string replacement. Easily extracted via binary analysis.

**Risk:** Compromised binaries expose all active enrollment tokens.

---

### H3. No Per-Device Authentication
**File:** `src/main/server.ts:494-528`

All agents share one enrollment token. No token expiration or rotation.

**Risk:** One compromised token affects all devices. No ability to revoke individual agents.

---

### H4. Debug Logging Exposes Credentials
**File:** `src/main/server.ts:497`

```typescript
console.log('Raw auth message:', JSON.stringify(message, null, 2));
```

**Risk:** Authentication tokens logged to console/disk in plaintext.

---

### H5. Unrestricted Terminal Shell Access
**File:** `agent/internal/terminal/terminal.go:42-121`

Full shell access with SYSTEM privileges. No input filtering or command restrictions.

---

### H6. Script Execution Bypasses Security
**File:** `agent/internal/executor/executor.go:111-115`

```go
cmd := exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
```

PowerShell execution policy bypassed for all scripts.

---

### H7. Admin Account Manipulation Without Limits
**File:** `agent/internal/admin/admin_windows.go:260-282`

Can demote/restore admin accounts without audit trail or approval workflow.

---

### H8. Dashboard Auto-Authentication
**File:** `src/main/server.ts:479`

```typescript
let authenticated = isDashboardConnection;
```

Dashboard connections auto-authenticated without token validation.

---

### H9. Tokens Exposed in Shell History
**File:** `src/main/server.ts:735-741`

Installation commands include tokens in command-line arguments, visible in shell history.

---

## Medium Severity Findings

### M1. No Rate Limiting
No protection against brute-force attacks on enrollment endpoint.

### M2. Unprotected Health Endpoint
`/health` endpoint leaks server information without authentication.

### M3. Config Files Unencrypted
Agent config stored in plaintext at `C:\ProgramData\Sentinel\config.json`.

### M4. No WebRTC SDP Validation
WebRTC offers accepted without validation.

### M5. Missing Authentication on Some Endpoints
`/api/agent/downloads` and `/api/agent/releases` lack authentication.

### M6. Service Runs as SYSTEM
Agent service runs with highest privileges, increasing blast radius.

### M7. No Symbolic Link Protection
File operations vulnerable to symlink attacks.

---

## Low Severity Findings

### L1. Unprotected Health Check
Information disclosure via `/health` endpoint.

### L2. Missing Certificate Pinning
No certificate pinning for TLS connections (when implemented).

---

## Remediation Priority Matrix

| Priority | Issue | Effort | Impact |
|----------|-------|--------|--------|
| P0 | Enable TLS/mTLS for all communications | High | Critical |
| P0 | Add command input validation/whitelist | Medium | Critical |
| P0 | Fix path traversal vulnerabilities | Medium | Critical |
| P0 | Remove hardcoded credentials | Low | Critical |
| P1 | Implement per-device tokens with expiration | High | High |
| P1 | Fix CORS configuration | Low | High |
| P1 | Remove credential logging | Low | High |
| P1 | Add rate limiting | Medium | High |
| P2 | Encrypt config files | Medium | Medium |
| P2 | Add dashboard authentication | Medium | Medium |
| P2 | Implement audit logging | Medium | Medium |

---

## Recommended Security Architecture Changes

### 1. Transport Security
```
┌─────────────┐         TLS 1.3          ┌─────────────┐
│   Agent     │ ◄───────────────────────► │   Server    │
└─────────────┘    mTLS + Cert Pinning   └─────────────┘
```

### 2. Authentication Flow
```
1. Agent generates CSR with hardware-bound key
2. Server issues signed certificate (valid 30 days)
3. All communications use mTLS with this cert
4. Automatic rotation before expiry
```

### 3. Command Execution Security
```
┌──────────────────────────────────────────────────┐
│                Command Pipeline                   │
├──────────────────────────────────────────────────┤
│ 1. Receive command                               │
│ 2. Validate against whitelist                    │
│ 3. Check rate limits                             │
│ 4. Log to audit trail                            │
│ 5. Execute in sandbox (optional)                 │
│ 6. Return sanitized output                       │
└──────────────────────────────────────────────────┘
```

---

## Compliance Considerations

### NIST Cybersecurity Framework
- **Identify:** Asset inventory via agent ✓
- **Protect:** Encryption needed ✗, Access control needed ✗
- **Detect:** Monitoring capabilities ✓
- **Respond:** Remote remediation ✓
- **Recover:** Agent reinstall capability ✓

### CIS Controls Alignment
- Control 1 (Inventory): Supported ✓
- Control 3 (Data Protection): NOT MET - No encryption ✗
- Control 4 (Secure Configuration): Partial - Hardcoded creds ✗
- Control 6 (Access Control): NOT MET - No per-device auth ✗
- Control 8 (Audit Logs): Partial - Sensitive data logged ✗

---

## Immediate Actions Required

1. **TODAY:** Remove debug logging of authentication messages
2. **TODAY:** Remove hardcoded database password
3. **THIS WEEK:** Implement TLS for all communications
4. **THIS WEEK:** Add command input validation
5. **THIS WEEK:** Fix path traversal vulnerabilities
6. **THIS MONTH:** Implement per-device authentication
7. **THIS MONTH:** Add rate limiting and audit logging

---

## Conclusion

Sentinel RMM has significant security vulnerabilities that must be addressed before production deployment. The lack of transport encryption and input validation creates critical risks for organizations using this tool.

The core functionality is sound, but security hardening is essential. Priority should be given to:
1. Enabling TLS/mTLS everywhere
2. Implementing proper authentication
3. Adding input validation for all remote operations

With these changes, Sentinel can meet industry security standards for RMM tools.
