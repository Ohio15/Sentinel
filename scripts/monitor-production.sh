#!/bin/bash
# Sentinel RMM - Production Monitoring Script
# Run continuously or via cron to detect issues
#
# Usage:
#   ./monitor-production.sh                          # Single check
#   ./monitor-production.sh --continuous             # Run every 5 minutes
#   ./monitor-production.sh --alert-webhook <url>    # Send alerts to webhook
#
# Crontab example (every 5 minutes):
#   */5 * * * * /opt/sentinel/scripts/monitor-production.sh >> /var/log/sentinel-monitor.log 2>&1

set -e

# Configuration
BASE_URL="${BASE_URL:-https://sentinelrmm.us:4443}"
LOG_FILE="${LOG_FILE:-/var/log/sentinel-monitor.log}"
ALERT_WEBHOOK="${ALERT_WEBHOOK:-}"
CHECK_INTERVAL=300  # 5 minutes for continuous mode

# Parse arguments
CONTINUOUS=false
while [[ $# -gt 0 ]]; do
    case $1 in
        --continuous|-c)
            CONTINUOUS=true
            shift
            ;;
        --alert-webhook)
            ALERT_WEBHOOK="$2"
            shift 2
            ;;
        --interval)
            CHECK_INTERVAL="$2"
            shift 2
            ;;
        *)
            BASE_URL="$1"
            shift
            ;;
    esac
done

# Colors (only if terminal)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    NC=''
fi

# Timestamp
timestamp() {
    date '+%Y-%m-%d %H:%M:%S'
}

# Logging
log_info() {
    echo "[$(timestamp)] [INFO] $1"
}

log_success() {
    echo -e "[$(timestamp)] ${GREEN}[OK]${NC} $1"
}

log_warn() {
    echo -e "[$(timestamp)] ${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "[$(timestamp)] ${RED}[ERROR]${NC} $1"
}

# Send alert to webhook (if configured)
send_alert() {
    local severity="$1"
    local message="$2"
    local details="$3"

    if [ -n "$ALERT_WEBHOOK" ]; then
        curl -s -X POST "$ALERT_WEBHOOK" \
            -H "Content-Type: application/json" \
            -d "{
                \"severity\": \"$severity\",
                \"message\": \"$message\",
                \"details\": \"$details\",
                \"timestamp\": \"$(timestamp)\",
                \"source\": \"sentinel-monitor\"
            }" > /dev/null 2>&1 || true
    fi
}

# Check a single endpoint
check_endpoint() {
    local endpoint="$1"
    local expected_status="$2"
    local name="$3"

    local url="${BASE_URL}${endpoint}"
    local start_time=$(date +%s%N)

    local http_code
    http_code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 -k "$url" 2>/dev/null || echo "000")

    local end_time=$(date +%s%N)
    local duration_ms=$(( (end_time - start_time) / 1000000 ))

    if [ "$http_code" = "$expected_status" ]; then
        log_success "$name: HTTP $http_code (${duration_ms}ms)"
        return 0
    else
        log_error "$name: HTTP $http_code (expected $expected_status)"
        send_alert "error" "Endpoint failure: $name" "Expected HTTP $expected_status, got $http_code"
        return 1
    fi
}

# Run all health checks
run_health_checks() {
    local failures=0

    log_info "Running health checks on $BASE_URL..."

    # Critical endpoints
    check_endpoint "/health" "200" "Health endpoint" || ((failures++))
    check_endpoint "/health/ready" "200" "Readiness (DB+Redis)" || ((failures++))
    check_endpoint "/health/live" "200" "Liveness" || ((failures++))

    # API endpoints
    check_endpoint "/api/agent/version" "200" "Agent version API" || ((failures++))

    # Protected endpoints should return 401
    check_endpoint "/api/devices" "401" "Protected endpoint security" || ((failures++))

    # Check response times
    local response_time
    response_time=$(curl -s -o /dev/null -w "%{time_total}" --max-time 10 -k "${BASE_URL}/health" 2>/dev/null || echo "99")
    response_time_ms=$(echo "$response_time * 1000" | bc 2>/dev/null || echo "9999")

    if [ "${response_time_ms%.*}" -gt 5000 ]; then
        log_warn "Response time high: ${response_time_ms}ms (threshold: 5000ms)"
        ((failures++))
    else
        log_success "Response time: ${response_time_ms%.*}ms"
    fi

    return $failures
}

# Check container health (if running locally)
check_containers() {
    if command -v docker &> /dev/null; then
        log_info "Checking container health..."

        containers=("sentinel-backend" "sentinel-postgres" "sentinel-redis" "sentinel-traefik")
        for container in "${containers[@]}"; do
            status=$(docker inspect --format='{{.State.Status}}' "$container" 2>/dev/null || echo "not_found")
            health=$(docker inspect --format='{{.State.Health.Status}}' "$container" 2>/dev/null || echo "N/A")

            if [ "$status" = "running" ]; then
                if [ "$health" = "healthy" ] || [ "$health" = "N/A" ]; then
                    log_success "Container $container: $status (health: $health)"
                else
                    log_warn "Container $container: $status (health: $health)"
                fi
            else
                log_error "Container $container: $status"
                send_alert "critical" "Container not running: $container" "Status: $status"
            fi
        done
    fi
}

# Check disk space
check_disk_space() {
    if command -v df &> /dev/null; then
        log_info "Checking disk space..."

        disk_usage=$(df -h / | awk 'NR==2 {print $5}' | tr -d '%')
        if [ "$disk_usage" -gt 90 ]; then
            log_error "Disk usage critical: ${disk_usage}%"
            send_alert "critical" "Disk space critical" "Usage: ${disk_usage}%"
        elif [ "$disk_usage" -gt 80 ]; then
            log_warn "Disk usage high: ${disk_usage}%"
        else
            log_success "Disk usage: ${disk_usage}%"
        fi
    fi
}

# Check memory
check_memory() {
    if command -v free &> /dev/null; then
        log_info "Checking memory..."

        mem_usage=$(free | awk '/Mem:/ {printf("%.0f", $3/$2 * 100)}')
        if [ "$mem_usage" -gt 90 ]; then
            log_error "Memory usage critical: ${mem_usage}%"
            send_alert "critical" "Memory critical" "Usage: ${mem_usage}%"
        elif [ "$mem_usage" -gt 80 ]; then
            log_warn "Memory usage high: ${mem_usage}%"
        else
            log_success "Memory usage: ${mem_usage}%"
        fi
    fi
}

# Main monitoring function
run_monitoring() {
    echo ""
    echo "==========================================================="
    echo "  SENTINEL PRODUCTION MONITORING"
    echo "  Time: $(timestamp)"
    echo "==========================================================="
    echo ""

    local total_failures=0

    run_health_checks
    total_failures=$?

    check_containers
    check_disk_space
    check_memory

    echo ""
    echo "-----------------------------------------------------------"
    if [ $total_failures -eq 0 ]; then
        log_success "All checks passed"
    else
        log_error "$total_failures check(s) failed"
    fi
    echo "-----------------------------------------------------------"
    echo ""

    return $total_failures
}

# Main execution
if [ "$CONTINUOUS" = true ]; then
    log_info "Starting continuous monitoring (interval: ${CHECK_INTERVAL}s)..."
    log_info "Press Ctrl+C to stop"

    while true; do
        run_monitoring
        log_info "Next check in ${CHECK_INTERVAL} seconds..."
        sleep $CHECK_INTERVAL
    done
else
    run_monitoring
fi
