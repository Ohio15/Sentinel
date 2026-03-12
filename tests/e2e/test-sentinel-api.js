/**
 * Sentinel Server API — E2E Test Suite
 *
 * Tests health endpoints, authentication, device listing, agent downloads,
 * install code validation, WebSocket connectivity, and Test Center API.
 *
 * Exports: runApiTests() → Promise<Array<{test, status, details, suite, durationMs}>>
 */
const https = require('https');
const http = require('http');
const { URL } = require('url');
const {
  SENTINEL_URL,
  SENTINEL_USER,
  SENTINEL_PASS,
  INSTALL_CODE,
  TIMEOUTS,
} = require('./config');

const SUITE = 'sentinel-api';

// ---------------------------------------------------------------------------
// Helpers
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
      opts.headers['Content-Type'] = opts.headers['Content-Type'] || 'application/json';
      opts.headers['Content-Length'] = Buffer.byteLength(payload);
    }

    const req = mod.request(opts, (res) => {
      const chunks = [];
      res.on('data', (d) => chunks.push(d));
      res.on('end', () => {
        const raw = Buffer.concat(chunks).toString();
        let json = null;
        try { json = JSON.parse(raw); } catch (_) { /* not json */ }
        resolve({ status: res.statusCode, headers: res.headers, body: raw, json });
      });
    });

    req.on('timeout', () => { req.destroy(); reject(new Error('Request timeout')); });
    req.on('error', reject);

    if (body) {
      req.write(typeof body === 'string' ? body : JSON.stringify(body));
    }
    req.end();
  });
}

function result(test, status, details, durationMs) {
  return { test, status, details, suite: SUITE, durationMs: durationMs || 0 };
}

async function timed(fn) {
  const t0 = Date.now();
  try {
    const res = await fn();
    return { ...res, _ms: Date.now() - t0 };
  } catch (e) {
    return { error: e, _ms: Date.now() - t0 };
  }
}

// ---------------------------------------------------------------------------
// Auth helper — returns JWT token for protected routes
// ---------------------------------------------------------------------------

let _cachedToken = null;
let _cachedCSRF = null;
let _cachedCookies = null;

async function getAuthToken() {
  if (_cachedToken) return { token: _cachedToken, csrf: _cachedCSRF, cookies: _cachedCookies };

  if (!SENTINEL_PASS) {
    throw new Error('SENTINEL_PASS environment variable is required for authenticated tests');
  }

  const res = await httpRequest('POST', `${SENTINEL_URL}/api/auth/login`, {
    identifier: SENTINEL_USER,
    password: SENTINEL_PASS,
  });

  if (res.status !== 200 || !res.json) {
    throw new Error(`Login failed: HTTP ${res.status} — ${res.body.substring(0, 200)}`);
  }

  _cachedToken = res.json.token || res.json.accessToken;
  if (!_cachedToken) {
    throw new Error('Login response missing token field: ' + JSON.stringify(res.json).substring(0, 200));
  }

  // Extract CSRF token and cookies from response
  _cachedCSRF = res.json.csrfToken || '';
  const setCookies = res.headers['set-cookie'];
  if (setCookies) {
    _cachedCookies = (Array.isArray(setCookies) ? setCookies : [setCookies])
      .map(c => c.split(';')[0])
      .join('; ');
  }

  return { token: _cachedToken, csrf: _cachedCSRF, cookies: _cachedCookies };
}

function authHeaders(auth) {
  const h = { 'Authorization': `Bearer ${auth.token}` };
  if (auth.csrf) h['X-CSRF-Token'] = auth.csrf;
  if (auth.cookies) h['Cookie'] = auth.cookies;
  return h;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

async function testHealthEndpoint() {
  const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}/health`));
  if (r.error) return result('Health Endpoint', 'FAIL', r.error.message, r._ms);
  if (r.status === 200 && r.json && r.json.status === 'healthy') {
    return result('Health Endpoint', 'PASS', `Status: healthy (${r._ms}ms)`, r._ms);
  }
  return result('Health Endpoint', 'FAIL', `HTTP ${r.status}: ${r.body.substring(0, 200)}`, r._ms);
}

async function testHealthLive() {
  const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}/health/live`));
  if (r.error) return result('Health Live', 'FAIL', r.error.message, r._ms);
  if (r.status === 200) {
    return result('Health Live', 'PASS', `HTTP 200 (${r._ms}ms)`, r._ms);
  }
  return result('Health Live', 'FAIL', `HTTP ${r.status}`, r._ms);
}

async function testHealthReady() {
  const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}/health/ready`));
  if (r.error) return result('Health Ready', 'FAIL', r.error.message, r._ms);
  if (r.status === 200) {
    return result('Health Ready', 'PASS', `HTTP 200 (${r._ms}ms)`, r._ms);
  }
  return result('Health Ready', 'WARN', `HTTP ${r.status} — DB or cache may be degraded`, r._ms);
}

async function testAgentVersion() {
  const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}/api/agent/version`));
  if (r.error) return result('Agent Version Endpoint', 'FAIL', r.error.message, r._ms);
  if (r.status === 200 && r.json) {
    const ver = r.json.version || r.json.latestVersion || JSON.stringify(r.json).substring(0, 100);
    return result('Agent Version Endpoint', 'PASS', `Version: ${ver}`, r._ms);
  }
  return result('Agent Version Endpoint', 'FAIL', `HTTP ${r.status}: ${r.body.substring(0, 200)}`, r._ms);
}

async function testLoginSuccess() {
  if (!SENTINEL_PASS) {
    return result('Login (valid creds)', 'WARN', 'SENTINEL_PASS not set — skipped');
  }
  const t0 = Date.now();
  try {
    const auth = await getAuthToken();
    const ms = Date.now() - t0;
    return result('Login (valid creds)', 'PASS', `Got JWT token (${ms}ms)`, ms);
  } catch (e) {
    return result('Login (valid creds)', 'FAIL', e.message, Date.now() - t0);
  }
}

async function testLoginInvalidCreds() {
  const r = await timed(() => httpRequest('POST', `${SENTINEL_URL}/api/auth/login`, {
    identifier: 'invalid@example.com',
    password: 'wrongpassword123',
  }));
  if (r.error) return result('Login (invalid creds)', 'FAIL', r.error.message, r._ms);
  if (r.status === 401 || r.status === 400) {
    return result('Login (invalid creds)', 'PASS', `Correctly rejected: HTTP ${r.status}`, r._ms);
  }
  return result('Login (invalid creds)', 'FAIL', `Unexpected HTTP ${r.status}`, r._ms);
}

async function testLoginEmptyBody() {
  const r = await timed(() => httpRequest('POST', `${SENTINEL_URL}/api/auth/login`, {}));
  if (r.error) return result('Login (empty body)', 'FAIL', r.error.message, r._ms);
  if (r.status === 400 || r.status === 401) {
    return result('Login (empty body)', 'PASS', `Correctly rejected: HTTP ${r.status}`, r._ms);
  }
  return result('Login (empty body)', 'FAIL', `Unexpected HTTP ${r.status}`, r._ms);
}

async function testAuthMe() {
  if (!SENTINEL_PASS) return result('GET /auth/me', 'WARN', 'Skipped — no credentials');
  const t0 = Date.now();
  try {
    const auth = await getAuthToken();
    const r = await httpRequest('GET', `${SENTINEL_URL}/api/auth/me`, null, authHeaders(auth));
    const ms = Date.now() - t0;
    if (r.status === 200 && r.json) {
      const email = r.json.email || r.json.user?.email || 'unknown';
      return result('GET /auth/me', 'PASS', `User: ${email}`, ms);
    }
    return result('GET /auth/me', 'FAIL', `HTTP ${r.status}: ${r.body.substring(0, 200)}`, ms);
  } catch (e) {
    return result('GET /auth/me', 'FAIL', e.message, Date.now() - t0);
  }
}

async function testDevicesListUnauth() {
  const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}/api/devices`));
  if (r.error) return result('Devices (no auth)', 'FAIL', r.error.message, r._ms);
  if (r.status === 401) {
    return result('Devices (no auth)', 'PASS', 'Correctly returned 401', r._ms);
  }
  return result('Devices (no auth)', 'FAIL', `Expected 401, got ${r.status}`, r._ms);
}

async function testDevicesListAuth() {
  if (!SENTINEL_PASS) return result('Devices (auth)', 'WARN', 'Skipped — no credentials');
  const t0 = Date.now();
  try {
    const auth = await getAuthToken();
    const r = await httpRequest('GET', `${SENTINEL_URL}/api/devices`, null, authHeaders(auth));
    const ms = Date.now() - t0;
    if (r.status === 200) {
      const count = Array.isArray(r.json) ? r.json.length :
                    r.json?.devices ? r.json.devices.length :
                    r.json?.total ?? 'unknown';
      return result('Devices (auth)', 'PASS', `Device count: ${count}`, ms);
    }
    return result('Devices (auth)', 'FAIL', `HTTP ${r.status}: ${r.body.substring(0, 200)}`, ms);
  } catch (e) {
    return result('Devices (auth)', 'FAIL', e.message, Date.now() - t0);
  }
}

async function testDashboardStats() {
  if (!SENTINEL_PASS) return result('Dashboard Stats', 'WARN', 'Skipped — no credentials');
  const t0 = Date.now();
  try {
    const auth = await getAuthToken();
    const r = await httpRequest('GET', `${SENTINEL_URL}/api/dashboard/stats`, null, authHeaders(auth));
    const ms = Date.now() - t0;
    if (r.status === 200 && r.json) {
      return result('Dashboard Stats', 'PASS', `Keys: ${Object.keys(r.json).join(', ').substring(0, 100)}`, ms);
    }
    return result('Dashboard Stats', 'FAIL', `HTTP ${r.status}: ${r.body.substring(0, 200)}`, ms);
  } catch (e) {
    return result('Dashboard Stats', 'FAIL', e.message, Date.now() - t0);
  }
}

async function testAgentDownloadWindows() {
  const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}/api/download/agent/windows`, null, null, TIMEOUTS.download));
  if (r.error) return result('Agent Download (Windows)', 'FAIL', r.error.message, r._ms);
  if (r.status === 200) {
    const size = r.headers['content-length'] || r.body.length;
    return result('Agent Download (Windows)', 'PASS', `Size: ${(parseInt(size) / 1024 / 1024).toFixed(1)} MB`, r._ms);
  }
  // Redirect is also acceptable
  if (r.status === 301 || r.status === 302) {
    return result('Agent Download (Windows)', 'PASS', `Redirect to: ${r.headers.location || 'unknown'}`, r._ms);
  }
  return result('Agent Download (Windows)', 'FAIL', `HTTP ${r.status}`, r._ms);
}

async function testValidateCodeInvalid() {
  const r = await timed(() =>
    httpRequest('GET', `${SENTINEL_URL}/api/public/install/validate-code?code=INVALID-CODE-12345`)
  );
  if (r.error) return result('Validate Code (invalid)', 'FAIL', r.error.message, r._ms);
  if (r.status === 404 || r.status === 400 || (r.status === 200 && r.json && r.json.valid === false)) {
    return result('Validate Code (invalid)', 'PASS', `Correctly rejected: HTTP ${r.status}`, r._ms);
  }
  return result('Validate Code (invalid)', 'WARN', `Unexpected: HTTP ${r.status} body=${r.body.substring(0, 100)}`, r._ms);
}

async function testValidateCodeEmpty() {
  const r = await timed(() =>
    httpRequest('GET', `${SENTINEL_URL}/api/public/install/validate-code?code=`)
  );
  if (r.error) return result('Validate Code (empty)', 'FAIL', r.error.message, r._ms);
  if (r.status === 400 || r.status === 404 || (r.status === 200 && r.json && r.json.valid === false)) {
    return result('Validate Code (empty)', 'PASS', `Correctly rejected: HTTP ${r.status}`, r._ms);
  }
  return result('Validate Code (empty)', 'WARN', `Unexpected: HTTP ${r.status}`, r._ms);
}

async function testValidateCodeReal() {
  const r = await timed(() =>
    httpRequest('GET', `${SENTINEL_URL}/api/public/install/validate-code?code=${INSTALL_CODE}`)
  );
  if (r.error) return result('Validate Code (real)', 'FAIL', r.error.message, r._ms);
  if (r.status === 200 && r.json && r.json.valid === true) {
    return result('Validate Code (real)', 'PASS', 'Code validated successfully', r._ms);
  }
  // The code may not exist in all environments
  if (r.status === 404 || (r.status === 200 && r.json && r.json.valid === false)) {
    return result('Validate Code (real)', 'WARN', `Code ${INSTALL_CODE} not found — may not be provisioned`, r._ms);
  }
  return result('Validate Code (real)', 'WARN', `HTTP ${r.status}: ${r.body.substring(0, 100)}`, r._ms);
}

async function testWebSocketConnectivity() {
  return new Promise((resolve) => {
    const t0 = Date.now();
    const timer = setTimeout(() => {
      resolve(result('WebSocket Connectivity', 'FAIL', 'Connection timeout (10s)', Date.now() - t0));
    }, 10000);

    try {
      // We can't import ws in a zero-dependency setup, so we do a raw HTTP upgrade check
      const parsed = new URL(SENTINEL_URL);
      const opts = {
        hostname: parsed.hostname,
        port: parsed.port || 443,
        path: '/ws',
        method: 'GET',
        headers: {
          'Upgrade': 'websocket',
          'Connection': 'Upgrade',
          'Sec-WebSocket-Key': Buffer.from('e2e-test-' + Date.now()).toString('base64'),
          'Sec-WebSocket-Version': '13',
        },
        rejectUnauthorized: false,
      };

      const req = https.request(opts, (res) => {
        clearTimeout(timer);
        const ms = Date.now() - t0;
        if (res.statusCode === 101) {
          resolve(result('WebSocket Connectivity', 'PASS', `Upgrade accepted (${ms}ms)`, ms));
        } else if (res.statusCode === 400 || res.statusCode === 403) {
          // 400/403 means the WS endpoint exists but needs auth — still a valid response
          resolve(result('WebSocket Connectivity', 'PASS', `WS endpoint exists, auth required: HTTP ${res.statusCode}`, ms));
        } else {
          resolve(result('WebSocket Connectivity', 'WARN', `HTTP ${res.statusCode} (expected 101 or 400)`, ms));
        }
        req.destroy();
      });

      req.on('upgrade', (res, socket) => {
        clearTimeout(timer);
        const ms = Date.now() - t0;
        socket.destroy();
        resolve(result('WebSocket Connectivity', 'PASS', `WebSocket upgrade succeeded (${ms}ms)`, ms));
      });

      req.on('error', (e) => {
        clearTimeout(timer);
        resolve(result('WebSocket Connectivity', 'FAIL', e.message, Date.now() - t0));
      });

      req.end();
    } catch (e) {
      clearTimeout(timer);
      resolve(result('WebSocket Connectivity', 'FAIL', e.message, Date.now() - t0));
    }
  });
}

async function testDashboardWebSocket() {
  if (!SENTINEL_PASS) return result('Dashboard WebSocket', 'WARN', 'Skipped — no credentials');
  const t0 = Date.now();
  try {
    const auth = await getAuthToken();
    return new Promise((resolve) => {
      const timer = setTimeout(() => {
        resolve(result('Dashboard WebSocket', 'FAIL', 'Connection timeout (10s)', Date.now() - t0));
      }, 10000);

      const parsed = new URL(SENTINEL_URL);
      const opts = {
        hostname: parsed.hostname,
        port: parsed.port || 443,
        path: `/ws/dashboard?token=${auth.token}`,
        method: 'GET',
        headers: {
          'Upgrade': 'websocket',
          'Connection': 'Upgrade',
          'Sec-WebSocket-Key': Buffer.from('e2e-dash-' + Date.now()).toString('base64'),
          'Sec-WebSocket-Version': '13',
        },
        rejectUnauthorized: false,
      };

      const req = https.request(opts, (res) => {
        clearTimeout(timer);
        const ms = Date.now() - t0;
        if (res.statusCode === 101) {
          resolve(result('Dashboard WebSocket', 'PASS', `Upgrade accepted (${ms}ms)`, ms));
        } else {
          resolve(result('Dashboard WebSocket', 'WARN', `HTTP ${res.statusCode}`, ms));
        }
        req.destroy();
      });

      req.on('upgrade', (res, socket) => {
        clearTimeout(timer);
        const ms = Date.now() - t0;
        socket.destroy();
        resolve(result('Dashboard WebSocket', 'PASS', `Upgrade succeeded (${ms}ms)`, ms));
      });

      req.on('error', (e) => {
        clearTimeout(timer);
        resolve(result('Dashboard WebSocket', 'FAIL', e.message, Date.now() - t0));
      });

      req.end();
    });
  } catch (e) {
    return result('Dashboard WebSocket', 'FAIL', e.message, Date.now() - t0);
  }
}

// Test Center Stats removed — Test Center is in APM, not Sentinel

async function testUnauthProtectedEndpoints() {
  const endpoints = [
    '/api/devices',
    '/api/alerts',
    '/api/scripts',
    '/api/users',
    '/api/dashboard/stats',
    '/api/settings',
  ];

  const results = [];
  for (const ep of endpoints) {
    const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}${ep}`));
    if (r.error) {
      results.push(result(`Unauth ${ep}`, 'FAIL', r.error.message, r._ms));
    } else if (r.status === 401) {
      results.push(result(`Unauth ${ep}`, 'PASS', 'Correctly returned 401', r._ms));
    } else {
      results.push(result(`Unauth ${ep}`, 'FAIL', `Expected 401, got ${r.status}`, r._ms));
    }
  }
  return results;
}

async function testRateLimiting() {
  const t0 = Date.now();
  let hitLimit = false;
  let requestCount = 0;

  for (let i = 0; i < 30; i++) {
    try {
      const r = await httpRequest('POST', `${SENTINEL_URL}/api/auth/login`, {
        email: `ratelimit-${i}@example.com`,
        password: 'wrongpassword',
      });
      requestCount++;
      if (r.status === 429) {
        hitLimit = true;
        const ms = Date.now() - t0;
        return result('Rate Limiting (auth)', 'PASS', `Hit 429 after ${requestCount} requests`, ms);
      }
    } catch (_) { break; }
  }

  const ms = Date.now() - t0;
  if (!hitLimit) {
    return result('Rate Limiting (auth)', 'WARN',
      `No 429 after ${requestCount} requests (may have higher threshold)`, ms);
  }
}

async function testUpdateEndpointAuth() {
  const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}/api/agent/update/download`));
  if (r.error) return result('Update Auth (C-02)', 'FAIL', r.error.message, r._ms);
  if (r.status === 401 || r.status === 403) {
    return result('Update Auth (C-02)', 'PASS', `Correctly rejected unauthenticated: HTTP ${r.status}`, r._ms);
  }
  return result('Update Auth (C-02)', 'WARN', `Expected 401/403, got ${r.status}`, r._ms);
}

async function testEnrollNoToken() {
  const r = await timed(() => httpRequest('POST', `${SENTINEL_URL}/api/agent/enroll`, {
    hostname: 'e2e-fake-host',
    agentId: 'e2e-fake-agent',
  }));
  if (r.error) return result('Enroll (no token)', 'FAIL', r.error.message, r._ms);
  if (r.status === 401 || r.status === 403) {
    return result('Enroll (no token)', 'PASS', `Correctly rejected: HTTP ${r.status}`, r._ms);
  }
  return result('Enroll (no token)', 'FAIL', `Expected 401/403, got ${r.status}`, r._ms);
}

async function testApiInfo() {
  const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}/api`));
  if (r.error) return result('API Info', 'FAIL', r.error.message, r._ms);
  if (r.status === 200) {
    return result('API Info', 'PASS', `Returned API info (${r._ms}ms)`, r._ms);
  }
  return result('API Info', 'WARN', `HTTP ${r.status}`, r._ms);
}

async function testBootstrapAgentInfo() {
  const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}/api/bootstrap/agent-info`));
  if (r.error) return result('Bootstrap Agent Info', 'FAIL', r.error.message, r._ms);
  if (r.status === 200 && r.json) {
    return result('Bootstrap Agent Info', 'PASS', `Version: ${r.json.version || 'present'}`, r._ms);
  }
  return result('Bootstrap Agent Info', 'WARN', `HTTP ${r.status}: ${r.body.substring(0, 100)}`, r._ms);
}

async function testCORSHeaders() {
  const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}/health`));
  if (r.error) return result('CORS Headers', 'FAIL', r.error.message, r._ms);
  const acao = r.headers['access-control-allow-origin'];
  if (acao) {
    return result('CORS Headers', 'PASS', `ACAO: ${acao}`, r._ms);
  }
  return result('CORS Headers', 'INFO', 'No ACAO header on /health (may be fine)', r._ms);
}

async function testSecurityHeaders() {
  const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}/health`));
  if (r.error) return result('Security Headers', 'FAIL', r.error.message, r._ms);

  const checks = [];
  if (r.headers['x-content-type-options']) checks.push('X-Content-Type-Options');
  if (r.headers['x-frame-options']) checks.push('X-Frame-Options');
  if (r.headers['x-xss-protection']) checks.push('X-XSS-Protection');
  if (r.headers['strict-transport-security']) checks.push('HSTS');

  if (checks.length >= 2) {
    return result('Security Headers', 'PASS', `Present: ${checks.join(', ')}`, r._ms);
  }
  return result('Security Headers', 'WARN', `Only found: ${checks.join(', ') || 'none'}`, r._ms);
}

// ---------------------------------------------------------------------------
// Runner
// ---------------------------------------------------------------------------

async function runApiTests() {
  const results = [];

  // Public endpoints (no auth needed)
  results.push(await testHealthEndpoint());
  results.push(await testHealthLive());
  results.push(await testHealthReady());
  results.push(await testAgentVersion());
  results.push(await testApiInfo());
  results.push(await testBootstrapAgentInfo());
  results.push(await testCORSHeaders());
  results.push(await testSecurityHeaders());

  // Auth tests
  results.push(await testLoginSuccess());
  results.push(await testLoginInvalidCreds());
  results.push(await testLoginEmptyBody());
  results.push(await testAuthMe());

  // Protected endpoint access control
  results.push(await testDevicesListUnauth());
  const unauthResults = await testUnauthProtectedEndpoints();
  results.push(...unauthResults);

  // Authenticated data endpoints
  results.push(await testDevicesListAuth());
  results.push(await testDashboardStats());

  // Agent/install endpoints
  results.push(await testAgentDownloadWindows());
  results.push(await testValidateCodeInvalid());
  results.push(await testValidateCodeEmpty());
  results.push(await testValidateCodeReal());

  // WebSocket
  results.push(await testWebSocketConnectivity());
  results.push(await testDashboardWebSocket());

  // Security
  results.push(await testUpdateEndpointAuth());
  results.push(await testEnrollNoToken());
  results.push(await testRateLimiting());

  // Test Center
  // Test Center Stats test removed — Test Center is in APM

  return results.filter(Boolean);  // Remove nulls from rate limit test
}

module.exports = { runApiTests };
