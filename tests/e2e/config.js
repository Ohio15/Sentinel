/**
 * Sentinel E2E Test Suite — Shared Configuration
 *
 * Environment variables override defaults:
 *   SENTINEL_URL, SENTINEL_USER, SENTINEL_PASS
 *   SSH_HOST, SSH_PORT, SSH_USER, SSH_KEY_PATH
 *   INSTALL_CODE
 */
const fs = require('fs');
const os = require('os');
const path = require('path');

// ---------------------------------------------------------------------------
// Sentinel server
// ---------------------------------------------------------------------------
const SENTINEL_URL = process.env.SENTINEL_URL || 'https://sentinelrmm.us';

// Sentinel admin credentials (used for API/agent tests that hit Sentinel)
const SENTINEL_USER = process.env.SENTINEL_USER || 'admin@sentinelrmm.us';
const SENTINEL_PASS = process.env.SENTINEL_PASS || '';  // Must be set via env

// ---------------------------------------------------------------------------
// APM server (Test Center submission)
// ---------------------------------------------------------------------------
const APM_URL = process.env.APM_URL || 'https://apm.sentinelrmm.us';
const APM_USER = process.env.APM_USER || 'admin@sentinelrmm.us';
const APM_PASS = process.env.APM_PASS || '';  // Must be set via env

// Install code for agent provisioning
const INSTALL_CODE = process.env.INSTALL_CODE || 'E2ET-ST01';

// ---------------------------------------------------------------------------
// Windows 11 VM (on NEXUS)
// ---------------------------------------------------------------------------
const sshKeyPath = process.env.SSH_KEY_PATH || path.join(os.homedir(), '.ssh', 'id_ed25519');
let sshPrivateKey = null;
try {
  sshPrivateKey = fs.readFileSync(sshKeyPath);
} catch (e) {
  // Key will be null — tests that need SSH will skip gracefully
}

const SSH_CONFIG = {
  host: process.env.SSH_HOST || 'localhost',
  port: parseInt(process.env.SSH_PORT || '2222', 10),
  username: process.env.SSH_USER || 'testadmin',
  privateKey: sshPrivateKey,
  readyTimeout: 15000,
  keepaliveInterval: 10000,
};

// ---------------------------------------------------------------------------
// Timeouts (ms)
// ---------------------------------------------------------------------------
const TIMEOUTS = {
  http: 15000,       // Individual HTTP request
  ssh: 60000,        // Individual SSH command
  download: 180000,  // Large binary download
  install: 120000,   // Installer execution
  suite: 600000,     // Entire suite
};

// ---------------------------------------------------------------------------
// Test metadata
// ---------------------------------------------------------------------------
const PROJECT_NAME = 'sentinel';
const BRANCH = 'master';
const RUNNER = os.hostname();
const ENVIRONMENT = process.env.NODE_ENV || 'production';

module.exports = {
  SENTINEL_URL,
  SENTINEL_USER,
  SENTINEL_PASS,
  APM_URL,
  APM_USER,
  APM_PASS,
  INSTALL_CODE,
  SSH_CONFIG,
  TIMEOUTS,
  PROJECT_NAME,
  BRANCH,
  RUNNER,
  ENVIRONMENT,
};
