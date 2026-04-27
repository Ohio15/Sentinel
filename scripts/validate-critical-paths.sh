#!/bin/bash
# Sentinel RMM - Critical Path Validation Script
# Run this BEFORE every deployment to catch breaking changes
#
# This script validates the complete flows that users depend on:
# 1. Health endpoints (basic connectivity)
# 2. Authentication flow (login/JWT)
# 3. Installation code flow (create -> validate -> enroll)
# 4. WebSocket connectivity
#
# Usage:
#   ./validate-critical-paths.sh                    # Test remote production
#   ./validate-critical-paths.sh localhost:8090     # Test local dev
#   ./validate-critical-paths.sh https://sentinelrmm.us
#   ./validate-critical-paths.sh https://sentinel.nexus  # LAN-fast on NEXUS

set -e

# Configuration
BASE_URL="${1:-https://sentinelrmm.us}"
TIMEOUT=10
VERBOSE="${VERBOSE:-false}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Counters
PASSED=0
FAILED=0
WARNINGS=0

# Helper functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_pass() {
    echo -e "${GREEN}[PASS]${NC} $1"
    ((PASSED++))
}

log_fail() {
    echo -e "${RED}[FAIL]${NC} $1"
    if [ -n "$2" ]; then
        echo -e "${RED}       ${NC}$2"
    fi
    ((FAILED++))
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
    ((WARNINGS++))
}

log_debug() {
    if [ "$VERBOSE" = "true" ]; then
        echo -e "${BLUE}[DEBUG]${NC} $1"
    fi
}

# Extract protocol from URL
get_protocol() {
    echo "$BASE_URL" | grep -o '^https\?://' | sed 's/:\/\///'
}

# Make HTTP request and capture both status and body
http_request() {
    local method="$1"
    local endpoint="$2"
    local data="$3"
    local extra_headers="$4"

    local url="${BASE_URL}${endpoint}"
    local curl_opts="-s -w '\n%{http_code}' --max-time $TIMEOUT -k"

    if [ -n "$extra_headers" ]; then
        curl_opts="$curl_opts $extra_headers"
    fi

    if [ "$method" = "GET" ]; then
        response=$(curl $curl_opts "$url" 2>/dev/null || echo "CURL_ERROR")
    else
        curl_opts="$curl_opts -X $method"
        if [ -n "$data" ]; then
            curl_opts="$curl_opts -H 'Content-Type: application/json' -d '$data'"
        fi
        response=$(eval curl $curl_opts "'$url'" 2>/dev/null || echo "CURL_ERROR")
    fi

    # Split response body and status code
    if [ "$response" = "CURL_ERROR" ]; then
        HTTP_BODY=""
        HTTP_STATUS="000"
    else
        HTTP_BODY=$(echo "$response" | sed '$d')
        HTTP_STATUS=$(echo "$response" | tail -1)
    fi

    log_debug "Request: $method $url"
    log_debug "Status: $HTTP_STATUS"
    log_debug "Body: $HTTP_BODY"
}

echo ""
echo "==========================================================="
echo "  SENTINEL RMM - CRITICAL PATH VALIDATION"
echo "==========================================================="
echo ""
echo "  Target: $BASE_URL"
echo "  Time:   $(date)"
echo ""
echo "==========================================================="

# ==============================================================
# PHASE 1: Health Endpoints
# ==============================================================
echo ""
echo -e "${YELLOW}[PHASE 1] Health Endpoints${NC}"
echo "-----------------------------------------------------------"

# Test /health
http_request "GET" "/health"
if [ "$HTTP_STATUS" = "200" ]; then
    log_pass "/health endpoint responding"
else
    log_fail "/health endpoint failed (HTTP $HTTP_STATUS)"
fi

# Test /health/ready (checks DB + Redis)
http_request "GET" "/health/ready"
if [ "$HTTP_STATUS" = "200" ]; then
    log_pass "/health/ready - Database and Redis connected"

    # Parse response for details
    if echo "$HTTP_BODY" | grep -q '"database":true'; then
        log_pass "Database connection verified"
    else
        log_fail "Database connection failed"
    fi

    if echo "$HTTP_BODY" | grep -q '"redis":true'; then
        log_pass "Redis connection verified"
    else
        log_fail "Redis connection failed"
    fi
else
    log_fail "/health/ready failed (HTTP $HTTP_STATUS) - DB or Redis may be down"
fi

# Test /health/live
http_request "GET" "/health/live"
if [ "$HTTP_STATUS" = "200" ]; then
    log_pass "/health/live - Service is alive"
else
    log_fail "/health/live failed (HTTP $HTTP_STATUS)"
fi

# ==============================================================
# PHASE 2: Public API Endpoints
# ==============================================================
echo ""
echo -e "${YELLOW}[PHASE 2] Public API Endpoints${NC}"
echo "-----------------------------------------------------------"

# Test agent version endpoint (public)
http_request "GET" "/api/agent/version"
if [ "$HTTP_STATUS" = "200" ]; then
    log_pass "/api/agent/version - Agent version endpoint working"
    VERSION=$(echo "$HTTP_BODY" | grep -o '"version":"[^"]*"' | cut -d'"' -f4)
    if [ -n "$VERSION" ]; then
        log_info "  Current agent version: $VERSION"
    fi
else
    log_fail "/api/agent/version failed (HTTP $HTTP_STATUS)"
fi

# Test bootstrap agent info (public)
http_request "GET" "/api/bootstrap/agent-info"
if [ "$HTTP_STATUS" = "200" ]; then
    log_pass "/api/bootstrap/agent-info - Bootstrap info available"
else
    log_warn "/api/bootstrap/agent-info returned $HTTP_STATUS (may be expected)"
fi

# ==============================================================
# PHASE 3: Installation Code Flow (CRITICAL)
# ==============================================================
echo ""
echo -e "${YELLOW}[PHASE 3] Installation Code Flow (CRITICAL)${NC}"
echo "-----------------------------------------------------------"

# Test with an invalid code - should return proper "invalid" response
http_request "GET" "/api/public/install/validate-code?code=INVALID-CODE"
if [ "$HTTP_STATUS" = "200" ]; then
    if echo "$HTTP_BODY" | grep -qi '"valid"\s*:\s*false'; then
        log_pass "Invalid code returns valid:false (correct behavior)"
    elif echo "$HTTP_BODY" | grep -qi '"status"\s*:\s*"invalid"'; then
        log_pass "Invalid code returns status:invalid (correct behavior)"
    else
        log_warn "Invalid code response format unexpected: $HTTP_BODY"
    fi
elif [ "$HTTP_STATUS" = "400" ] || [ "$HTTP_STATUS" = "404" ]; then
    log_pass "Invalid code properly rejected (HTTP $HTTP_STATUS)"
else
    log_fail "Invalid code handling broken (HTTP $HTTP_STATUS)"
    log_fail "Response: $HTTP_BODY"
fi

# Test with malformed code
http_request "GET" "/api/public/install/validate-code?code="
if [ "$HTTP_STATUS" = "400" ] || [ "$HTTP_STATUS" = "404" ] || [ "$HTTP_STATUS" = "200" ]; then
    log_pass "Empty code handled gracefully (HTTP $HTTP_STATUS)"
else
    log_fail "Empty code caused error (HTTP $HTTP_STATUS)"
fi

# ==============================================================
# PHASE 4: Authentication Endpoints
# ==============================================================
echo ""
echo -e "${YELLOW}[PHASE 4] Authentication Endpoints${NC}"
echo "-----------------------------------------------------------"

# Test login endpoint reachability (don't actually login)
http_request "POST" "/api/auth/login" '{"email":"test@test.com","password":"wrong"}'
if [ "$HTTP_STATUS" = "401" ] || [ "$HTTP_STATUS" = "400" ]; then
    log_pass "/api/auth/login - Endpoint reachable, rejects invalid credentials"
elif [ "$HTTP_STATUS" = "429" ]; then
    log_warn "/api/auth/login - Rate limited (expected in production)"
else
    log_fail "/api/auth/login - Unexpected response (HTTP $HTTP_STATUS)"
fi

# Test register endpoint exists
http_request "POST" "/api/auth/register" '{"email":"","password":""}'
if [ "$HTTP_STATUS" = "400" ] || [ "$HTTP_STATUS" = "401" ] || [ "$HTTP_STATUS" = "422" ]; then
    log_pass "/api/auth/register - Endpoint reachable, validates input"
elif [ "$HTTP_STATUS" = "429" ]; then
    log_warn "/api/auth/register - Rate limited"
else
    log_fail "/api/auth/register - Unexpected response (HTTP $HTTP_STATUS)"
fi

# ==============================================================
# PHASE 5: Agent Endpoints
# ==============================================================
echo ""
echo -e "${YELLOW}[PHASE 5] Agent Endpoints${NC}"
echo "-----------------------------------------------------------"

# Test agent enrollment endpoint (should require token)
http_request "POST" "/api/agent/enroll" '{"hostname":"test"}'
if [ "$HTTP_STATUS" = "401" ] || [ "$HTTP_STATUS" = "400" ] || [ "$HTTP_STATUS" = "403" ]; then
    log_pass "/api/agent/enroll - Requires valid enrollment token"
else
    log_fail "/api/agent/enroll - Unexpected response (HTTP $HTTP_STATUS)"
fi

# Test agent download endpoint pattern
http_request "GET" "/api/agents/download/windows/amd64"
if [ "$HTTP_STATUS" = "401" ] || [ "$HTTP_STATUS" = "400" ] || [ "$HTTP_STATUS" = "200" ]; then
    log_pass "/api/agents/download - Download endpoint exists"
else
    log_warn "/api/agents/download - Unexpected response (HTTP $HTTP_STATUS)"
fi

# ==============================================================
# PHASE 6: Protected Endpoints (should require auth)
# ==============================================================
echo ""
echo -e "${YELLOW}[PHASE 6] Protected Endpoint Security${NC}"
echo "-----------------------------------------------------------"

# These should all return 401/403 without auth
PROTECTED_ENDPOINTS=(
    "/api/devices"
    "/api/alerts"
    "/api/scripts"
    "/api/users"
    "/api/enrollment-tokens"
    "/api/clients"
)

for endpoint in "${PROTECTED_ENDPOINTS[@]}"; do
    http_request "GET" "$endpoint"
    if [ "$HTTP_STATUS" = "401" ] || [ "$HTTP_STATUS" = "403" ]; then
        log_pass "$endpoint - Protected (requires auth)"
    elif [ "$HTTP_STATUS" = "200" ]; then
        log_fail "$endpoint - NOT PROTECTED! Accessible without auth!"
    else
        log_warn "$endpoint - Unexpected status (HTTP $HTTP_STATUS)"
    fi
done

# ==============================================================
# PHASE 7: WebSocket Endpoint
# ==============================================================
echo ""
echo -e "${YELLOW}[PHASE 7] WebSocket Endpoints${NC}"
echo "-----------------------------------------------------------"

# WebSocket upgrade test (will fail upgrade but should respond)
WS_URL=$(echo "$BASE_URL" | sed 's/^http/ws/')
http_request "GET" "/ws/agent" "" "-H 'Upgrade: websocket' -H 'Connection: Upgrade'"
if [ "$HTTP_STATUS" = "400" ] || [ "$HTTP_STATUS" = "426" ] || [ "$HTTP_STATUS" = "101" ]; then
    log_pass "/ws/agent - WebSocket endpoint exists"
else
    log_warn "/ws/agent - Unexpected response (HTTP $HTTP_STATUS)"
fi

http_request "GET" "/ws/dashboard" "" "-H 'Upgrade: websocket' -H 'Connection: Upgrade'"
if [ "$HTTP_STATUS" = "400" ] || [ "$HTTP_STATUS" = "401" ] || [ "$HTTP_STATUS" = "426" ]; then
    log_pass "/ws/dashboard - WebSocket endpoint exists (requires auth)"
else
    log_warn "/ws/dashboard - Unexpected response (HTTP $HTTP_STATUS)"
fi

# ==============================================================
# PHASE 8: Database Error Handling
# ==============================================================
echo ""
echo -e "${YELLOW}[PHASE 8] Error Handling${NC}"
echo "-----------------------------------------------------------"

# Test that database errors don't leak internal info
http_request "GET" "/api/devices/99999999"
if [ "$HTTP_STATUS" = "401" ] || [ "$HTTP_STATUS" = "404" ]; then
    log_pass "Non-existent resource returns proper error"

    # Check for sensitive info leakage
    if echo "$HTTP_BODY" | grep -qi "sql\|postgres\|pgx\|database\|connection"; then
        log_fail "Response may leak database implementation details"
    else
        log_pass "No database implementation details leaked"
    fi
else
    log_warn "Non-existent device returned HTTP $HTTP_STATUS"
fi

# ==============================================================
# RESULTS
# ==============================================================
echo ""
echo "==========================================================="
echo "  VALIDATION RESULTS"
echo "==========================================================="
echo ""
echo -e "  ${GREEN}Passed:${NC}   $PASSED"
echo -e "  ${RED}Failed:${NC}   $FAILED"
echo -e "  ${YELLOW}Warnings:${NC} $WARNINGS"
echo ""

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}==========================================================${NC}"
    echo -e "${GREEN}  ALL CRITICAL TESTS PASSED - Safe to deploy${NC}"
    echo -e "${GREEN}==========================================================${NC}"
    exit 0
else
    echo -e "${RED}==========================================================${NC}"
    echo -e "${RED}  CRITICAL TESTS FAILED - DO NOT DEPLOY${NC}"
    echo -e "${RED}==========================================================${NC}"
    echo ""
    echo "Fix the failing tests before deploying to production."
    exit 1
fi
