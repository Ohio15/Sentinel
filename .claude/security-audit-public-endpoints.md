# Security Audit: Public Installation Portal Endpoints

**Date:** 2026-01-14
**Auditor:** Internal Security Review
**Scope:** Public-facing agent installation portal endpoints

## Executive Summary

The public installation portal (`/api/public/install/*`) has several security controls in place but requires improvements in rate limiting and token exposure mitigation.

---

## Endpoints Audited

1. `GET /api/public/install/:downloadToken` - Validate installation link
2. `GET /api/public/install/:downloadToken/download` - Download installer package
3. `GET /api/public/install/:downloadToken/status` - Check installation status

---

## Security Findings

### HIGH PRIORITY

#### 1. Enrollment Token Exposed in Plaintext (HIGH)
**File:** `agent_links_public.go:318-326`
**Description:** The enrollment token is embedded in plain text in `config/agent.json` inside the ZIP package. Anyone who obtains the ZIP can extract and reuse this token.
**Risk:** Token reuse, unauthorized agent enrollment
**Recommendation:**
- Generate unique one-time enrollment tokens per installation link
- Consider encrypting the config file with a machine-specific key derived at install time
- Add token binding to the specific download link ID

### MEDIUM PRIORITY

#### 2. No Rate Limiting on Public Endpoints (MEDIUM) - FIXED 2026-01-14
**File:** `configs/traefik/dynamic/routers.yml`
**Description:** The frontend route (which handles `/install/*`) had no rate limiting middleware applied.
**Status:** FIXED - Added `install-portal` and `install-portal-local` routes with `rateLimit` middleware. Also added rate limiting to main frontend routes.
**Remaining:**
- Consider stricter limits (e.g., 10 requests/minute per IP) specifically for install endpoints
- Add CAPTCHA after N failed validation attempts

#### 3. Download Token Logging in URLs (MEDIUM)
**Description:** Download tokens appear in URL paths which may be logged by:
- Reverse proxies (Traefik logs)
- Browser history
- Server access logs
- Network monitoring tools
**Risk:** Token exposure through log files
**Recommendation:**
- Implement token rotation after successful download
- Set short expiration (current 48-72 hours is reasonable)
- Consider POST-based token validation instead of GET

### LOW PRIORITY

#### 4. Access Log IP Tracking Without Anonymization (LOW)
**File:** `agent_links_public.go:568-582`
**Description:** IP addresses stored in `agent_link_access_log` without any anonymization
**Risk:** Privacy compliance (GDPR)
**Recommendation:**
- Consider hashing or truncating IPs after N days
- Document retention policy

#### 5. User Agent Stored Unbounded (LOW)
**File:** `agent_links_public.go:579`
**Description:** User agent string stored without length validation
**Risk:** Potential for large data storage if malicious user-agents sent
**Recommendation:**
- Truncate user agent to reasonable length (e.g., 500 chars)

---

## Positive Security Controls

1. **Strong Token Generation** - Uses `crypto/rand` with 256 bits entropy (32 bytes hex encoded)
2. **Download Count Limits** - Max 5 downloads per token (line 207-211)
3. **Access Logging** - All access attempts logged for audit trail
4. **Link Expiration** - Links expire and are validated on each request
5. **Revocation Support** - Links can be revoked by administrators
6. **Generic Error Messages** - Don't leak internal information
7. **Organization Isolation** - Links scoped to organization (multi-tenant safe)

---

## Recommended Actions

### Immediate (Before Next Release)
1. ~~Add rate limiting to public endpoints in Traefik config~~ **DONE 2026-01-14**
2. Truncate user agent storage to 500 characters

### Short Term (Next Sprint)
3. Implement one-time-use enrollment tokens
4. Add IP-based blocking after N failed validation attempts

### Medium Term
5. Encrypt config.json in ZIP with install-time derived key
6. Add audit log retention policy
7. Consider CAPTCHA for download endpoint

---

## Files Referenced

- `server/internal/api/agent_links_public.go`
- `server/internal/api/agent_links.go`
- `server/internal/api/agent_links_routes.go`
- `configs/traefik/dynamic/routers.yml`
- `configs/traefik/dynamic/middlewares.yml`
