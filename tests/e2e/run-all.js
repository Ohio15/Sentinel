#!/usr/bin/env node
/**
 * Sentinel E2E Test Orchestrator
 *
 * Runs all test suites, collects results, posts to Test Center API,
 * and prints a summary to stdout.
 *
 * Usage:
 *   SENTINEL_PASS=xxx APM_PASS=xxx node run-all.js
 *   SENTINEL_PASS=xxx node run-all.js --suite api        # Run only API tests
 *   SENTINEL_PASS=xxx node run-all.js --suite agent       # Run only agent tests
 *   SENTINEL_PASS=xxx node run-all.js --suite frontend    # Run only frontend tests
 *   SENTINEL_PASS=xxx node run-all.js --no-submit         # Skip APM Test Center submission
 *   SENTINEL_PASS=xxx node run-all.js --json              # Output JSON to stdout
 *
 * Exit codes:
 *   0 — All tests passed (or only WARN/INFO)
 *   1 — One or more tests FAILED
 */
// Load .env file if present (handles special chars like ! that bash mangles)
const fs_env = require('fs');
const path_env = require('path');
const envPath = path_env.join(__dirname, '.env');
if (fs_env.existsSync(envPath)) {
  for (const line of fs_env.readFileSync(envPath, 'utf-8').split('\n')) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#')) continue;
    const eq = trimmed.indexOf('=');
    if (eq < 0) continue;
    const key = trimmed.substring(0, eq);
    const val = trimmed.substring(eq + 1);
    if (!process.env[key]) process.env[key] = val;
  }
}

const https = require('https');
const http = require('http');
const { URL } = require('url');
const {
  SENTINEL_URL,
  SENTINEL_USER,
  SENTINEL_PASS,
  APM_URL,
  APM_USER,
  APM_PASS,
  PROJECT_NAME,
  BRANCH,
  RUNNER,
  ENVIRONMENT,
  TIMEOUTS,
} = require('./config');

const { runApiTests } = require('./test-sentinel-api');
const { runAgentTests } = require('./test-sentinel-agent');
const { runFrontendTests } = require('./test-apm-frontend');

// ---------------------------------------------------------------------------
// CLI args
// ---------------------------------------------------------------------------

const args = process.argv.slice(2);
const suiteFilter = args.includes('--suite') ? args[args.indexOf('--suite') + 1] : null;
const noSubmit = args.includes('--no-submit');
const jsonOutput = args.includes('--json');

// ---------------------------------------------------------------------------
// Logging
// ---------------------------------------------------------------------------

const START = Date.now();

function log(msg) {
  if (jsonOutput) return;
  const elapsed = ((Date.now() - START) / 1000).toFixed(1);
  console.log(`[${elapsed}s] ${msg}`);
}

// ---------------------------------------------------------------------------
// HTTP helper for Test Center submission
// ---------------------------------------------------------------------------

function httpRequest(method, url, body, headers, timeout) {
  return new Promise((resolve, reject) => {
    const parsed = new URL(url);
    const mod = parsed.protocol === 'https:' ? https : http;
    const opts = {
      method,
      hostname: parsed.hostname,
      port: parsed.port || (parsed.protocol === 'https:' ? 443 : 80),
      path: parsed.pathname + parsed.search,
      headers: { ...headers },
      rejectUnauthorized: false,
      timeout: timeout || TIMEOUTS.http,
    };

    if (body) {
      const payload = typeof body === 'string' ? body : JSON.stringify(body);
      opts.headers['Content-Type'] = 'application/json';
      opts.headers['Content-Length'] = Buffer.byteLength(payload);
    }

    const req = mod.request(opts, (res) => {
      const chunks = [];
      res.on('data', (d) => chunks.push(d));
      res.on('end', () => {
        const raw = Buffer.concat(chunks).toString();
        let json = null;
        try { json = JSON.parse(raw); } catch (_) {}
        resolve({ status: res.statusCode, headers: res.headers, body: raw, json });
      });
    });

    req.on('timeout', () => { req.destroy(); reject(new Error('Request timeout')); });
    req.on('error', reject);
    if (body) req.write(typeof body === 'string' ? body : JSON.stringify(body));
    req.end();
  });
}

// ---------------------------------------------------------------------------
// Auth for Test Center submission
// ---------------------------------------------------------------------------

async function getApmAuthToken() {
  if (!APM_PASS) return null;

  const res = await httpRequest('POST', `${APM_URL}/api/auth/login`, {
    email: APM_USER,
    password: APM_PASS,
  });

  if (res.status !== 200 || !res.json) {
    log(`WARNING: APM login for Test Center submission failed: HTTP ${res.status}`);
    return null;
  }

  return {
    token: res.json.token || res.json.accessToken,
  };
}

// ---------------------------------------------------------------------------
// Submit results to Test Center
// ---------------------------------------------------------------------------

async function submitResults(allResults, durationMs) {
  if (noSubmit || !APM_PASS) {
    log('Skipping Test Center submission (--no-submit or no APM credentials)');
    return;
  }

  const auth = await getApmAuthToken();
  if (!auth || !auth.token) {
    log('WARNING: Could not authenticate with APM for Test Center submission');
    return;
  }

  const passed = allResults.filter(r => r.status === 'PASS').length;
  const failed = allResults.filter(r => r.status === 'FAIL').length;
  const skipped = allResults.filter(r => r.status === 'WARN' || r.status === 'INFO').length;
  const now = new Date();

  // Map test results to Test Center format
  const results = allResults.map(r => ({
    testName: r.test,
    suite: r.suite || 'unknown',
    status: r.status === 'PASS' ? 'passed' :
            r.status === 'FAIL' ? 'failed' :
            r.status === 'WARN' ? 'passed' :  // WARN counts as passed with note
            'skipped',
    durationMs: r.durationMs || null,
    errorMessage: r.status === 'FAIL' ? r.details : (r.status === 'WARN' ? `[WARN] ${r.details}` : null),
    retryCount: 0,
  }));

  const payload = {
    project: PROJECT_NAME,
    branch: BRANCH,
    triggerType: 'cron',
    status: failed > 0 ? 'failed' : 'completed',
    totalTests: allResults.length,
    passed,
    failed,
    skipped,
    durationMs,
    environment: ENVIRONMENT,
    runner: RUNNER,
    summary: `E2E: ${passed} passed, ${failed} failed, ${skipped} skipped`,
    startedAt: new Date(START).toISOString(),
    finishedAt: now.toISOString(),
    results,
  };

  try {
    const headers = {
      'Authorization': `Bearer ${auth.token}`,
    };

    const res = await httpRequest('POST', `${APM_URL}/api/test-results`, payload, headers);

    if (res.status === 200 || res.status === 201) {
      const runId = res.json?.id || res.json?.runId || 'unknown';
      log(`Test results submitted to Test Center (run: ${runId})`);
    } else {
      log(`WARNING: APM Test Center submission returned HTTP ${res.status}: ${res.body.substring(0, 200)}`);
    }
  } catch (e) {
    log(`WARNING: Failed to submit to APM Test Center: ${e.message}`);
  }
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main() {
  log('========================================');
  log('  Sentinel E2E Test Orchestrator');
  log(`  Target: ${SENTINEL_URL}`);
  log(`  Runner: ${RUNNER}`);
  log(`  Suite filter: ${suiteFilter || 'all'}`);
  log('========================================');
  log('');

  const allResults = [];

  // --- API Tests ---
  if (!suiteFilter || suiteFilter === 'api') {
    log('--- Running API Tests ---');
    try {
      const apiResults = await runApiTests();
      allResults.push(...apiResults);
      const apiPass = apiResults.filter(r => r.status === 'PASS').length;
      const apiFail = apiResults.filter(r => r.status === 'FAIL').length;
      log(`API Tests complete: ${apiPass} passed, ${apiFail} failed (${apiResults.length} total)`);
    } catch (e) {
      log(`API Tests CRASHED: ${e.message}`);
      allResults.push({ test: 'API Suite', status: 'FAIL', details: `Crash: ${e.message}`, suite: 'sentinel-api' });
    }
    log('');
  }

  // --- Agent Tests ---
  if (!suiteFilter || suiteFilter === 'agent') {
    log('--- Running Agent Tests ---');
    try {
      const agentResults = await runAgentTests();
      allResults.push(...agentResults);
      const agentPass = agentResults.filter(r => r.status === 'PASS').length;
      const agentFail = agentResults.filter(r => r.status === 'FAIL').length;
      log(`Agent Tests complete: ${agentPass} passed, ${agentFail} failed (${agentResults.length} total)`);
    } catch (e) {
      log(`Agent Tests CRASHED: ${e.message}`);
      allResults.push({ test: 'Agent Suite', status: 'FAIL', details: `Crash: ${e.message}`, suite: 'sentinel-agent' });
    }
    log('');
  }

  // --- Frontend Tests ---
  if (!suiteFilter || suiteFilter === 'frontend') {
    log('--- Running Frontend Tests ---');
    try {
      const frontendResults = await runFrontendTests();
      allResults.push(...frontendResults);
      const fePass = frontendResults.filter(r => r.status === 'PASS').length;
      const feFail = frontendResults.filter(r => r.status === 'FAIL').length;
      log(`Frontend Tests complete: ${fePass} passed, ${feFail} failed (${frontendResults.length} total)`);
    } catch (e) {
      log(`Frontend Tests CRASHED: ${e.message}`);
      allResults.push({ test: 'Frontend Suite', status: 'FAIL', details: `Crash: ${e.message}`, suite: 'apm-frontend' });
    }
    log('');
  }

  // --- Summary ---
  const durationMs = Date.now() - START;
  const pass = allResults.filter(r => r.status === 'PASS').length;
  const fail = allResults.filter(r => r.status === 'FAIL').length;
  const warn = allResults.filter(r => r.status === 'WARN').length;
  const info = allResults.filter(r => r.status === 'INFO').length;

  if (jsonOutput) {
    console.log(JSON.stringify({
      results: allResults,
      summary: { pass, fail, warn, info, total: allResults.length, durationMs },
      ts: new Date().toISOString(),
    }, null, 2));
  } else {
    log('========================================');
    log('  TEST SUMMARY');
    log('========================================');
    log(`  PASS: ${pass}  |  FAIL: ${fail}  |  WARN: ${warn}  |  INFO: ${info}`);
    log(`  Total: ${allResults.length} tests in ${(durationMs / 1000).toFixed(1)}s`);
    log('');

    if (fail > 0) {
      log('FAILURES:');
      allResults.filter(r => r.status === 'FAIL').forEach(r => {
        log(`  [FAIL] [${r.suite}] ${r.test}: ${r.details}`);
      });
      log('');
    }

    if (warn > 0) {
      log('WARNINGS:');
      allResults.filter(r => r.status === 'WARN').forEach(r => {
        log(`  [WARN] [${r.suite}] ${r.test}: ${r.details}`);
      });
      log('');
    }
  }

  // --- Submit to Test Center ---
  await submitResults(allResults, durationMs);

  // --- Exit code ---
  if (fail > 0) {
    log(`Exiting with code 1 (${fail} failures)`);
    process.exit(1);
  } else {
    log('All tests passed.');
    process.exit(0);
  }
}

main().catch((e) => {
  console.error('FATAL:', e);
  process.exit(1);
});
