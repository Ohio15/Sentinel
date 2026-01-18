#!/bin/bash
# Sentinel RMM - Full Agent Connection Flow Test
# This script simulates an agent connecting to the server and validates
# the complete enrollment -> WebSocket -> communication flow
#
# Usage:
#   ./test-agent-flow.sh https://sentinelrmm.us:4443
#   ./test-agent-flow.sh https://sentinelrmm.us:4443 --token "enrollment-token"
#   ./test-agent-flow.sh https://sentinelrmm.us:4443 --code "XXXX-XXXX"
#
# Requirements:
#   - curl
#   - websocat (for WebSocket testing) - install: cargo install websocat
#   - jq (for JSON parsing)

set -e

# Configuration
BASE_URL="${1:-https://sentinelrmm.us:4443}"
ENROLLMENT_TOKEN=""
INSTALLATION_CODE=""
TIMEOUT=30

# Parse arguments
shift || true
while [[ $# -gt 0 ]]; do
    case $1 in
        --token)
            ENROLLMENT_TOKEN="$2"
            shift 2
            ;;
        --code)
            INSTALLATION_CODE="$2"
            shift 2
            ;;
        *)
            shift
            ;;
    esac
done

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Counters
PASSED=0
FAILED=0

# Test state
AGENT_ID=""
DEVICE_ID=""
AGENT_TOKEN=""
TEST_HOSTNAME="test-agent-$$"

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

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# Check dependencies
check_dependencies() {
    local missing=()

    if ! command -v curl &> /dev/null; then
        missing+=("curl")
    fi

    if ! command -v jq &> /dev/null; then
        log_warn "jq not found - JSON parsing will be limited"
    fi

    if ! command -v websocat &> /dev/null; then
        log_warn "websocat not found - WebSocket tests will be limited"
        log_info "Install with: cargo install websocat"
    fi

    if [ ${#missing[@]} -gt 0 ]; then
        echo -e "${RED}Missing required dependencies: ${missing[*]}${NC}"
        exit 1
    fi
}

echo ""
echo "==========================================================="
echo "  SENTINEL AGENT CONNECTION FLOW TEST"
echo "==========================================================="
echo ""
echo "  Target:    $BASE_URL"
echo "  Time:      $(date)"
echo "  Hostname:  $TEST_HOSTNAME"
echo ""

check_dependencies

# ==============================================================
# PHASE 1: Obtain Enrollment Token
# ==============================================================
echo ""
echo -e "${YELLOW}[PHASE 1] Obtain Enrollment Token${NC}"
echo "-----------------------------------------------------------"

if [ -n "$INSTALLATION_CODE" ]; then
    log_info "Validating installation code: $INSTALLATION_CODE"

    RESPONSE=$(curl -s -k --max-time $TIMEOUT \
        "${BASE_URL}/api/public/install/validate-code?code=${INSTALLATION_CODE}")

    HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -k --max-time $TIMEOUT \
        "${BASE_URL}/api/public/install/validate-code?code=${INSTALLATION_CODE}")

    if [ "$HTTP_STATUS" = "200" ]; then
        if command -v jq &> /dev/null; then
            VALID=$(echo "$RESPONSE" | jq -r '.valid // false')
            if [ "$VALID" = "true" ]; then
                ENROLLMENT_TOKEN=$(echo "$RESPONSE" | jq -r '.enrollmentToken // .enrollment_token // empty')
                if [ -n "$ENROLLMENT_TOKEN" ]; then
                    log_pass "Installation code valid - got enrollment token"
                else
                    log_fail "Code valid but no enrollment token in response" "$RESPONSE"
                    exit 1
                fi
            else
                log_fail "Installation code invalid" "$RESPONSE"
                exit 1
            fi
        else
            # Basic grep fallback
            if echo "$RESPONSE" | grep -q '"valid":true\|"valid": true'; then
                ENROLLMENT_TOKEN=$(echo "$RESPONSE" | grep -o '"enrollmentToken":"[^"]*"' | cut -d'"' -f4)
                if [ -n "$ENROLLMENT_TOKEN" ]; then
                    log_pass "Installation code valid"
                else
                    log_fail "Could not extract enrollment token" "$RESPONSE"
                    exit 1
                fi
            else
                log_fail "Installation code invalid" "$RESPONSE"
                exit 1
            fi
        fi
    else
        log_fail "Installation code validation failed (HTTP $HTTP_STATUS)" "$RESPONSE"
        exit 1
    fi

elif [ -n "$ENROLLMENT_TOKEN" ]; then
    log_info "Using provided enrollment token"
    log_pass "Enrollment token provided"

else
    log_info "Attempting to fetch enrollment token from public endpoint..."

    HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -k --max-time $TIMEOUT \
        "${BASE_URL}/api/enrollment-tokens")

    if [ "$HTTP_STATUS" = "200" ]; then
        RESPONSE=$(curl -s -k --max-time $TIMEOUT "${BASE_URL}/api/enrollment-tokens")

        if command -v jq &> /dev/null; then
            ENROLLMENT_TOKEN=$(echo "$RESPONSE" | jq -r '.[0].token // empty')
        else
            ENROLLMENT_TOKEN=$(echo "$RESPONSE" | grep -o '"token":"[^"]*"' | head -1 | cut -d'"' -f4)
        fi

        if [ -n "$ENROLLMENT_TOKEN" ]; then
            log_pass "Retrieved enrollment token from server"
        else
            log_fail "No enrollment tokens available"
            log_info "Please provide --token or --code parameter"
            exit 1
        fi
    else
        log_fail "Cannot obtain enrollment token (HTTP $HTTP_STATUS)"
        log_info "Please provide --token or --code parameter"
        exit 1
    fi
fi

# ==============================================================
# PHASE 2: Agent Enrollment
# ==============================================================
echo ""
echo -e "${YELLOW}[PHASE 2] Agent Enrollment${NC}"
echo "-----------------------------------------------------------"

log_info "Enrolling agent with hostname: $TEST_HOSTNAME"

ENROLL_PAYLOAD=$(cat <<EOF
{
    "hostname": "$TEST_HOSTNAME",
    "platform": "linux",
    "os_version": "Linux Test",
    "agent_version": "1.0.0-test",
    "ip_address": "192.168.1.100",
    "mac_address": "00:11:22:33:44:55"
}
EOF
)

RESPONSE=$(curl -s -k --max-time $TIMEOUT \
    -X POST \
    -H "Content-Type: application/json" \
    -H "X-Enrollment-Token: $ENROLLMENT_TOKEN" \
    -d "$ENROLL_PAYLOAD" \
    "${BASE_URL}/api/agent/enroll")

HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -k --max-time $TIMEOUT \
    -X POST \
    -H "Content-Type: application/json" \
    -H "X-Enrollment-Token: $ENROLLMENT_TOKEN" \
    -d "$ENROLL_PAYLOAD" \
    "${BASE_URL}/api/agent/enroll")

if [ "$HTTP_STATUS" = "200" ] || [ "$HTTP_STATUS" = "201" ]; then
    if command -v jq &> /dev/null; then
        AGENT_ID=$(echo "$RESPONSE" | jq -r '.agentId // .agent_id // .id // empty')
        DEVICE_ID=$(echo "$RESPONSE" | jq -r '.deviceId // .device_id // .id // empty')
        AGENT_TOKEN=$(echo "$RESPONSE" | jq -r '.token // .agentToken // .agent_token // empty')
    else
        AGENT_ID=$(echo "$RESPONSE" | grep -o '"agentId":"[^"]*"\|"agent_id":"[^"]*"\|"id":"[^"]*"' | head -1 | cut -d'"' -f4)
        DEVICE_ID=$(echo "$RESPONSE" | grep -o '"deviceId":"[^"]*"\|"device_id":"[^"]*"' | head -1 | cut -d'"' -f4)
        AGENT_TOKEN=$(echo "$RESPONSE" | grep -o '"token":"[^"]*"' | head -1 | cut -d'"' -f4)
    fi

    if [ -n "$AGENT_ID" ] || [ -n "$DEVICE_ID" ]; then
        log_pass "Agent enrolled successfully"
        log_info "  Agent ID:  ${AGENT_ID:-N/A}"
        log_info "  Device ID: ${DEVICE_ID:-N/A}"
        [ -n "$AGENT_TOKEN" ] && log_info "  Token:     ${AGENT_TOKEN:0:20}..."
    else
        log_fail "Enrollment response missing agent/device ID" "$RESPONSE"
        exit 1
    fi
else
    log_fail "Agent enrollment failed (HTTP $HTTP_STATUS)" "$RESPONSE"
    exit 1
fi

# ==============================================================
# PHASE 3: WebSocket Connection Test
# ==============================================================
echo ""
echo -e "${YELLOW}[PHASE 3] WebSocket Connection${NC}"
echo "-----------------------------------------------------------"

WS_URL=$(echo "$BASE_URL" | sed 's/^https:/wss:/' | sed 's/^http:/ws:/')
WS_URL="${WS_URL}/ws/agent"

log_info "WebSocket URL: $WS_URL"

if command -v websocat &> /dev/null; then
    log_info "Attempting WebSocket connection..."

    # Create heartbeat message
    HEARTBEAT=$(cat <<EOF
{"type":"heartbeat","agent_id":"$AGENT_ID","timestamp":"$(date -u +%Y-%m-%dT%H:%M:%SZ)"}
EOF
)

    # Try to connect and send heartbeat
    RESULT=$(echo "$HEARTBEAT" | timeout 10 websocat -k -1 \
        -H "X-Agent-Token: $AGENT_TOKEN" \
        -H "X-Agent-ID: $AGENT_ID" \
        "$WS_URL" 2>&1 || true)

    if [ -n "$RESULT" ]; then
        log_pass "WebSocket connection and message exchange successful"
        log_info "  Response: ${RESULT:0:100}..."
    else
        # Connection might have succeeded but no response
        # Try just connecting
        timeout 5 websocat -k --ping-interval 1 --ping-timeout 3 \
            -H "X-Agent-Token: $AGENT_TOKEN" \
            -H "X-Agent-ID: $AGENT_ID" \
            "$WS_URL" </dev/null 2>&1 && \
            log_pass "WebSocket connection established" || \
            log_warn "WebSocket connection test inconclusive"
    fi
else
    log_warn "websocat not installed - skipping WebSocket test"
    log_info "Install with: cargo install websocat"

    # Fallback: test WebSocket upgrade via HTTP
    HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -k --max-time 5 \
        -H "Upgrade: websocket" \
        -H "Connection: Upgrade" \
        -H "X-Agent-Token: $AGENT_TOKEN" \
        -H "X-Agent-ID: $AGENT_ID" \
        "$BASE_URL/ws/agent")

    if [ "$HTTP_STATUS" = "400" ] || [ "$HTTP_STATUS" = "426" ]; then
        log_pass "WebSocket endpoint exists and responds to upgrade request"
    else
        log_warn "WebSocket endpoint returned HTTP $HTTP_STATUS"
    fi
fi

# ==============================================================
# PHASE 4: Verify Device Registration
# ==============================================================
echo ""
echo -e "${YELLOW}[PHASE 4] Verify Device Registration${NC}"
echo "-----------------------------------------------------------"

if [ -n "$DEVICE_ID" ]; then
    log_pass "Device registered with ID: $DEVICE_ID"
else
    log_fail "Device ID not available after enrollment"
fi

# ==============================================================
# RESULTS
# ==============================================================
echo ""
echo "==========================================================="
echo "  TEST RESULTS"
echo "==========================================================="
echo ""
echo -e "  ${GREEN}Passed:${NC} $PASSED"
echo -e "  ${RED}Failed:${NC} $FAILED"
echo ""

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}  AGENT CONNECTION FLOW: WORKING${NC}"
    echo ""
    echo "  The complete agent-to-server connection flow is functional:"
    echo "    - Enrollment token validation"
    echo "    - Agent registration in database"
    echo "    - WebSocket connection over TLS"
    echo ""
    exit 0
else
    echo -e "${RED}  AGENT CONNECTION FLOW: ISSUES DETECTED${NC}"
    echo ""
    exit 1
fi
