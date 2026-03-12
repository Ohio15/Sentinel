#!/usr/bin/env bash
# ============================================================================
# Sentinel E2E Test Suite — NEXUS Cron Setup
#
# Installs a weekly cron job on the NEXUS server (Ubuntu) that runs the
# E2E test suite every Sunday at 2:00 AM and logs output.
#
# Usage (run ON the NEXUS server):
#   chmod +x setup-cron.sh && ./setup-cron.sh
#
# Or from the local machine:
#   scp tests/e2e/* ohio_@192.168.1.20:~/e2e-tests/
#   ssh ohio_@192.168.1.20 "cd ~/e2e-tests && chmod +x setup-cron.sh && ./setup-cron.sh"
# ============================================================================

set -euo pipefail

E2E_DIR="${HOME}/e2e-tests"
LOG_DIR="${HOME}/e2e-tests/logs"
NODE_BIN=$(which node 2>/dev/null || echo "/usr/bin/node")
CRON_SCHEDULE="0 2 * * 0"  # Every Sunday at 2:00 AM

echo "=== Sentinel E2E Cron Setup ==="
echo "E2E directory: ${E2E_DIR}"
echo "Log directory:  ${LOG_DIR}"
echo "Node binary:    ${NODE_BIN}"
echo ""

# Verify node is available
if [ ! -x "${NODE_BIN}" ]; then
  echo "ERROR: Node.js not found at ${NODE_BIN}"
  echo "Install Node.js first: curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash - && sudo apt-get install -y nodejs"
  exit 1
fi

echo "Node version: $(${NODE_BIN} --version)"

# Create directories
mkdir -p "${E2E_DIR}" "${LOG_DIR}"

# Check if ssh2 is installed (required for agent tests)
if [ ! -d "${E2E_DIR}/node_modules/ssh2" ]; then
  echo "Installing ssh2 dependency..."
  cd "${E2E_DIR}"
  npm init -y 2>/dev/null || true
  npm install ssh2
  echo ""
fi

# Check that test files exist
if [ ! -f "${E2E_DIR}/run-all.js" ]; then
  echo "WARNING: run-all.js not found in ${E2E_DIR}"
  echo "Copy the test files first:"
  echo "  scp -r D:/Projects/Sentinel/tests/e2e/* ohio_@192.168.1.20:~/e2e-tests/"
  exit 1
fi

# Create the runner script that cron will execute
cat > "${E2E_DIR}/run-e2e-cron.sh" << 'RUNNER_EOF'
#!/usr/bin/env bash
# Auto-generated runner script for cron
set -euo pipefail

E2E_DIR="$(cd "$(dirname "$0")" && pwd)"
LOG_DIR="${E2E_DIR}/logs"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
LOG_FILE="${LOG_DIR}/e2e_${TIMESTAMP}.log"

# Source credentials from env file if it exists
if [ -f "${E2E_DIR}/.env" ]; then
  set -a
  source "${E2E_DIR}/.env"
  set +a
fi

# Ensure SENTINEL_PASS is set
if [ -z "${SENTINEL_PASS:-}" ]; then
  echo "ERROR: SENTINEL_PASS not set. Create ${E2E_DIR}/.env with SENTINEL_PASS=xxx" | tee "${LOG_FILE}"
  exit 1
fi

echo "=== E2E Test Run: $(date -Iseconds) ===" | tee "${LOG_FILE}"

# Run the test suite
cd "${E2E_DIR}"
node run-all.js 2>&1 | tee -a "${LOG_FILE}"
EXIT_CODE=${PIPESTATUS[0]}

echo "" >> "${LOG_FILE}"
echo "Exit code: ${EXIT_CODE}" >> "${LOG_FILE}"

# Clean up old logs (keep 30 days)
find "${LOG_DIR}" -name "e2e_*.log" -mtime +30 -delete 2>/dev/null || true

exit ${EXIT_CODE}
RUNNER_EOF

chmod +x "${E2E_DIR}/run-e2e-cron.sh"

# Create .env template if it doesn't exist
if [ ! -f "${E2E_DIR}/.env" ]; then
  cat > "${E2E_DIR}/.env" << 'ENV_EOF'
# Sentinel E2E Test Configuration
# Fill in the admin password for authenticated tests and Test Center submission
SENTINEL_URL=https://sentinelrmm.us
SENTINEL_USER=admin@sentinelrmm.us
SENTINEL_PASS=
INSTALL_CODE=E2ET-ST01
SSH_HOST=localhost
SSH_PORT=2222
SSH_USER=testadmin
ENV_EOF
  echo "Created ${E2E_DIR}/.env — EDIT THIS FILE to set SENTINEL_PASS"
fi

# Install cron job (idempotent — removes existing entry first)
CRON_CMD="${CRON_SCHEDULE} ${E2E_DIR}/run-e2e-cron.sh >> ${LOG_DIR}/cron.log 2>&1"
CRON_MARKER="# sentinel-e2e-weekly"

# Remove existing cron entry if present
(crontab -l 2>/dev/null || true) | grep -v "${CRON_MARKER}" | crontab -

# Add new cron entry
(crontab -l 2>/dev/null || true; echo "${CRON_CMD} ${CRON_MARKER}") | crontab -

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Cron job installed:"
crontab -l | grep sentinel-e2e
echo ""
echo "NEXT STEPS:"
echo "  1. Edit ${E2E_DIR}/.env and set SENTINEL_PASS"
echo "  2. Test manually: cd ${E2E_DIR} && source .env && node run-all.js"
echo "  3. Logs will appear in: ${LOG_DIR}/"
echo ""
echo "To run immediately: ${E2E_DIR}/run-e2e-cron.sh"
echo "To remove cron: crontab -l | grep -v sentinel-e2e | crontab -"
