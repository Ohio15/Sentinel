/**
 * Sentinel APM Web Frontend — E2E Smoke Tests
 *
 * HTTP-only smoke tests for the web frontend served at https://sentinelrmm.us.
 * Validates that pages load, assets are served, and API integrations work.
 * No browser automation — all checks use raw HTTP requests.
 *
 * Exports: runFrontendTests() → Promise<Array<{test, status, details, suite, durationMs}>>
 */
const https = require('https');
const http = require('http');
const { URL } = require('url');
const {
  SENTINEL_URL,
  SENTINEL_USER,
  SENTINEL_PASS,
  TIMEOUTS,
} = require('./config');

const SUITE = 'apm-frontend';

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
        try { json = JSON.parse(raw); } catch (_) {}
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

// Auth helper — reuse from api tests logic
let _token = null;
let _csrf = null;
let _cookies = null;

async function getAuth() {
  if (_token) return { token: _token, csrf: _csrf, cookies: _cookies };
  if (!SENTINEL_PASS) throw new Error('SENTINEL_PASS not set');

  const res = await httpRequest('POST', `${SENTINEL_URL}/api/auth/login`, {
    identifier: SENTINEL_USER,
    password: SENTINEL_PASS,
  });

  if (res.status !== 200 || !res.json) throw new Error(`Login failed: HTTP ${res.status}`);
  _token = res.json.token || res.json.accessToken;
  _csrf = res.json.csrfToken || '';

  const setCookies = res.headers['set-cookie'];
  if (setCookies) {
    _cookies = (Array.isArray(setCookies) ? setCookies : [setCookies])
      .map(c => c.split(';')[0]).join('; ');
  }

  return { token: _token, csrf: _csrf, cookies: _cookies };
}

function authHeaders(auth) {
  const h = { 'Authorization': `Bearer ${auth.token}` };
  if (auth.csrf) h['X-CSRF-Token'] = auth.csrf;
  if (auth.cookies) h['Cookie'] = auth.cookies;
  return h;
}

// ---------------------------------------------------------------------------
// Tests: Page Loading
// ---------------------------------------------------------------------------

async function testRootPageLoads() {
  const r = await timed(() => httpRequest('GET', SENTINEL_URL));
  if (r.error) return result('Root Page Loads', 'FAIL', r.error.message, r._ms);
  if (r.status === 200 && r.body.includes('<html')) {
    return result('Root Page Loads', 'PASS', `${r.body.length} bytes (${r._ms}ms)`, r._ms);
  }
  if (r.status === 301 || r.status === 302) {
    return result('Root Page Loads', 'PASS', `Redirects to: ${r.headers.location}`, r._ms);
  }
  return result('Root Page Loads', 'FAIL', `HTTP ${r.status}, no HTML content`, r._ms);
}

async function testLoginPageHTML() {
  const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}/login`));
  if (r.error) return result('Login Page HTML', 'FAIL', r.error.message, r._ms);

  // SPA usually returns the same HTML for all routes (client-side routing)
  if (r.status === 200 && r.body.includes('<html')) {
    const hasReactRoot = r.body.includes('id="root"') || r.body.includes('id="app"');
    const hasScripts = r.body.includes('<script');
    if (hasReactRoot && hasScripts) {
      return result('Login Page HTML', 'PASS', `SPA shell loaded (${r.body.length} bytes)`, r._ms);
    }
    return result('Login Page HTML', 'WARN', 'HTML returned but missing React root or scripts', r._ms);
  }
  // 404 might mean server-side routing doesn't have /login but SPA handles it
  if (r.status === 404) {
    return result('Login Page HTML', 'WARN', 'HTTP 404 — SPA may need history fallback', r._ms);
  }
  return result('Login Page HTML', 'FAIL', `HTTP ${r.status}`, r._ms);
}

async function testDashboardPageHTML() {
  // Dashboard is a protected route — SPA should still serve HTML (client handles auth redirect)
  const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}/dashboard`));
  if (r.error) return result('Dashboard Page HTML', 'FAIL', r.error.message, r._ms);
  if (r.status === 200 && r.body.includes('<html')) {
    return result('Dashboard Page HTML', 'PASS', `SPA shell served (${r.body.length} bytes)`, r._ms);
  }
  return result('Dashboard Page HTML', 'WARN', `HTTP ${r.status}`, r._ms);
}

// ---------------------------------------------------------------------------
// Tests: Static Assets
// ---------------------------------------------------------------------------

async function testJSBundleLoads() {
  // First get the HTML to find the JS bundle path
  const r = await timed(() => httpRequest('GET', SENTINEL_URL));
  if (r.error) return result('JS Bundle Loads', 'FAIL', r.error.message, r._ms);

  // Extract script src from HTML
  const scriptMatch = r.body.match(/src="([^"]*\.js[^"]*)"/);
  if (!scriptMatch) {
    return result('JS Bundle Loads', 'WARN', 'No JS bundle reference found in HTML', r._ms);
  }

  const scriptPath = scriptMatch[1].startsWith('http') ? scriptMatch[1] :
    scriptMatch[1].startsWith('/') ? `${SENTINEL_URL}${scriptMatch[1]}` :
    `${SENTINEL_URL}/${scriptMatch[1]}`;

  const jsR = await timed(() => httpRequest('GET', scriptPath));
  if (jsR.error) return result('JS Bundle Loads', 'FAIL', jsR.error.message, jsR._ms);
  if (jsR.status === 200 && jsR.body.length > 1000) {
    return result('JS Bundle Loads', 'PASS',
      `${(jsR.body.length / 1024).toFixed(0)} KB from ${scriptMatch[1].substring(0, 80)}`, jsR._ms);
  }
  return result('JS Bundle Loads', 'FAIL', `HTTP ${jsR.status}, size=${jsR.body.length}`, jsR._ms);
}

async function testCSSLoads() {
  const r = await timed(() => httpRequest('GET', SENTINEL_URL));
  if (r.error) return result('CSS Loads', 'FAIL', r.error.message, r._ms);

  // Extract CSS link from HTML
  const cssMatch = r.body.match(/href="([^"]*\.css[^"]*)"/);
  if (!cssMatch) {
    // CSS may be inlined or loaded by JS
    return result('CSS Loads', 'INFO', 'No external CSS link found (may be JS-loaded)', r._ms);
  }

  const cssPath = cssMatch[1].startsWith('http') ? cssMatch[1] :
    cssMatch[1].startsWith('/') ? `${SENTINEL_URL}${cssMatch[1]}` :
    `${SENTINEL_URL}/${cssMatch[1]}`;

  const cssR = await timed(() => httpRequest('GET', cssPath));
  if (cssR.error) return result('CSS Loads', 'FAIL', cssR.error.message, cssR._ms);
  if (cssR.status === 200) {
    return result('CSS Loads', 'PASS',
      `${(cssR.body.length / 1024).toFixed(0)} KB from ${cssMatch[1].substring(0, 80)}`, cssR._ms);
  }
  return result('CSS Loads', 'FAIL', `HTTP ${cssR.status}`, cssR._ms);
}

async function testFaviconExists() {
  const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}/favicon.ico`));
  if (r.error) return result('Favicon', 'INFO', r.error.message, r._ms);
  if (r.status === 200) {
    return result('Favicon', 'PASS', `${r.body.length} bytes`, r._ms);
  }
  // Try /favicon.svg or other variants
  const r2 = await timed(() => httpRequest('GET', `${SENTINEL_URL}/favicon.svg`));
  if (!r2.error && r2.status === 200) {
    return result('Favicon', 'PASS', `SVG favicon: ${r2.body.length} bytes`, r2._ms);
  }
  return result('Favicon', 'INFO', `No favicon found (HTTP ${r.status})`, r._ms);
}

// ---------------------------------------------------------------------------
// Tests: Dark Mode
// ---------------------------------------------------------------------------

async function testDarkModeCSS() {
  // Check that the CSS contains dark mode rules
  const r = await timed(() => httpRequest('GET', SENTINEL_URL));
  if (r.error) return result('Dark Mode CSS', 'FAIL', r.error.message, r._ms);

  // Find CSS bundle
  const cssMatch = r.body.match(/href="([^"]*\.css[^"]*)"/);
  if (!cssMatch) {
    // Check if dark mode classes are in the HTML (Tailwind or inline)
    const hasDark = r.body.includes('dark:') || r.body.includes('dark-mode') || r.body.includes('theme-dark');
    return result('Dark Mode CSS', hasDark ? 'PASS' : 'INFO',
      hasDark ? 'Dark mode classes found in HTML' : 'No external CSS, cannot verify dark mode', r._ms);
  }

  const cssPath = cssMatch[1].startsWith('/') ? `${SENTINEL_URL}${cssMatch[1]}` : cssMatch[1];
  const cssR = await timed(() => httpRequest('GET', cssPath));
  if (cssR.error) return result('Dark Mode CSS', 'FAIL', cssR.error.message, cssR._ms);

  const hasDarkMode = cssR.body.includes('dark') || cssR.body.includes('.dark') ||
                      cssR.body.includes('prefers-color-scheme') || cssR.body.includes('color-scheme');
  if (hasDarkMode) {
    return result('Dark Mode CSS', 'PASS', 'Dark mode rules found in CSS', cssR._ms);
  }
  return result('Dark Mode CSS', 'WARN', 'No dark mode rules detected in CSS', cssR._ms);
}

// ---------------------------------------------------------------------------
// Tests: API Integration (from frontend perspective)
// ---------------------------------------------------------------------------

async function testLoginAPIFromFrontend() {
  if (!SENTINEL_PASS) return result('Frontend Login API', 'WARN', 'Skipped — no credentials');
  const t0 = Date.now();
  try {
    const auth = await getAuth();
    return result('Frontend Login API', 'PASS', `Token obtained (${Date.now() - t0}ms)`, Date.now() - t0);
  } catch (e) {
    return result('Frontend Login API', 'FAIL', e.message, Date.now() - t0);
  }
}

async function testDevicesAPIForFrontend() {
  if (!SENTINEL_PASS) return result('Frontend Devices API', 'WARN', 'Skipped — no credentials');
  const t0 = Date.now();
  try {
    const auth = await getAuth();
    const r = await httpRequest('GET', `${SENTINEL_URL}/api/devices`, null, authHeaders(auth));
    const ms = Date.now() - t0;
    if (r.status === 200) {
      return result('Frontend Devices API', 'PASS', `Devices endpoint OK (${ms}ms)`, ms);
    }
    return result('Frontend Devices API', 'FAIL', `HTTP ${r.status}: ${r.body.substring(0, 100)}`, ms);
  } catch (e) {
    return result('Frontend Devices API', 'FAIL', e.message, Date.now() - t0);
  }
}

async function testAlertsAPIForFrontend() {
  if (!SENTINEL_PASS) return result('Frontend Alerts API', 'WARN', 'Skipped — no credentials');
  const t0 = Date.now();
  try {
    const auth = await getAuth();
    const r = await httpRequest('GET', `${SENTINEL_URL}/api/alerts`, null, authHeaders(auth));
    const ms = Date.now() - t0;
    if (r.status === 200) {
      return result('Frontend Alerts API', 'PASS', `Alerts endpoint OK (${ms}ms)`, ms);
    }
    return result('Frontend Alerts API', 'FAIL', `HTTP ${r.status}`, ms);
  } catch (e) {
    return result('Frontend Alerts API', 'FAIL', e.message, Date.now() - t0);
  }
}

async function testDashboardStatsAPIForFrontend() {
  if (!SENTINEL_PASS) return result('Frontend Dashboard Stats', 'WARN', 'Skipped — no credentials');
  const t0 = Date.now();
  try {
    const auth = await getAuth();
    const r = await httpRequest('GET', `${SENTINEL_URL}/api/dashboard/stats`, null, authHeaders(auth));
    const ms = Date.now() - t0;
    if (r.status === 200 && r.json) {
      return result('Frontend Dashboard Stats', 'PASS',
        `Stats keys: ${Object.keys(r.json).slice(0, 5).join(', ')}`, ms);
    }
    return result('Frontend Dashboard Stats', 'FAIL', `HTTP ${r.status}`, ms);
  } catch (e) {
    return result('Frontend Dashboard Stats', 'FAIL', e.message, Date.now() - t0);
  }
}

async function testScriptsAPIForFrontend() {
  if (!SENTINEL_PASS) return result('Frontend Scripts API', 'WARN', 'Skipped — no credentials');
  const t0 = Date.now();
  try {
    const auth = await getAuth();
    const r = await httpRequest('GET', `${SENTINEL_URL}/api/scripts`, null, authHeaders(auth));
    const ms = Date.now() - t0;
    if (r.status === 200) {
      return result('Frontend Scripts API', 'PASS', `Scripts endpoint OK (${ms}ms)`, ms);
    }
    return result('Frontend Scripts API', 'FAIL', `HTTP ${r.status}`, ms);
  } catch (e) {
    return result('Frontend Scripts API', 'FAIL', e.message, Date.now() - t0);
  }
}

async function testTicketsAPIForFrontend() {
  if (!SENTINEL_PASS) return result('Frontend Tickets API', 'WARN', 'Skipped — no credentials');
  const t0 = Date.now();
  try {
    const auth = await getAuth();
    const r = await httpRequest('GET', `${SENTINEL_URL}/api/tickets`, null, authHeaders(auth));
    const ms = Date.now() - t0;
    if (r.status === 200) {
      return result('Frontend Tickets API', 'PASS', `Tickets endpoint OK (${ms}ms)`, ms);
    }
    return result('Frontend Tickets API', 'FAIL', `HTTP ${r.status}`, ms);
  } catch (e) {
    return result('Frontend Tickets API', 'FAIL', e.message, Date.now() - t0);
  }
}

// ---------------------------------------------------------------------------
// Tests: SPA Navigation (check that various routes serve the SPA shell)
// ---------------------------------------------------------------------------

async function testSPARoutes() {
  const routes = [
    '/dashboard',
    '/devices',
    '/alerts',
    '/scripts',
    '/settings',
    '/tickets',
  ];

  const results = [];
  for (const route of routes) {
    const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}${route}`));
    if (r.error) {
      results.push(result(`SPA Route: ${route}`, 'FAIL', r.error.message, r._ms));
      continue;
    }

    if (r.status === 200 && (r.body.includes('<html') || r.body.includes('id="root"'))) {
      results.push(result(`SPA Route: ${route}`, 'PASS', `SPA shell served (${r._ms}ms)`, r._ms));
    } else if (r.status === 200) {
      results.push(result(`SPA Route: ${route}`, 'WARN', `HTTP 200 but no HTML root element`, r._ms));
    } else {
      results.push(result(`SPA Route: ${route}`, 'WARN', `HTTP ${r.status} — may need history fallback`, r._ms));
    }
  }
  return results;
}

// ---------------------------------------------------------------------------
// Tests: TLS / HTTPS
// ---------------------------------------------------------------------------

async function testTLSCertificate() {
  return new Promise((resolve) => {
    const parsed = new URL(SENTINEL_URL);
    const req = https.request({
      hostname: parsed.hostname,
      port: parsed.port || 443,
      path: '/health',
      method: 'GET',
      rejectUnauthorized: true,  // Actually verify the cert
    }, (res) => {
      const cert = res.socket.getPeerCertificate();
      if (cert && cert.subject) {
        const expiry = new Date(cert.valid_to);
        const daysLeft = Math.floor((expiry - Date.now()) / (1000 * 60 * 60 * 24));
        const status = daysLeft > 14 ? 'PASS' : daysLeft > 0 ? 'WARN' : 'FAIL';
        resolve(result('TLS Certificate', status,
          `CN=${cert.subject.CN || cert.subject.O}, expires in ${daysLeft} days (${cert.valid_to})`));
      } else {
        resolve(result('TLS Certificate', 'WARN', 'Could not read peer certificate'));
      }
      req.destroy();
    });

    req.on('error', (e) => {
      if (e.code === 'CERT_HAS_EXPIRED') {
        resolve(result('TLS Certificate', 'FAIL', 'Certificate has expired'));
      } else if (e.code === 'UNABLE_TO_VERIFY_LEAF_SIGNATURE' || e.code === 'DEPTH_ZERO_SELF_SIGNED_CERT') {
        resolve(result('TLS Certificate', 'WARN', `Self-signed or untrusted: ${e.code}`));
      } else {
        resolve(result('TLS Certificate', 'FAIL', e.message));
      }
    });

    req.setTimeout(TIMEOUTS.http, () => {
      req.destroy();
      resolve(result('TLS Certificate', 'FAIL', 'Timeout checking certificate'));
    });

    req.end();
  });
}

// ---------------------------------------------------------------------------
// Tests: Response performance
// ---------------------------------------------------------------------------

async function testResponseTime() {
  const endpoints = [
    { path: '/health', name: 'Health', threshold: 500 },
    { path: '/', name: 'Root Page', threshold: 2000 },
    { path: '/api/agent/version', name: 'Agent Version', threshold: 1000 },
  ];

  const results = [];
  for (const ep of endpoints) {
    const r = await timed(() => httpRequest('GET', `${SENTINEL_URL}${ep.path}`));
    if (r.error) {
      results.push(result(`Response Time: ${ep.name}`, 'FAIL', r.error.message, r._ms));
      continue;
    }
    const status = r._ms < ep.threshold ? 'PASS' : r._ms < ep.threshold * 2 ? 'WARN' : 'FAIL';
    results.push(result(`Response Time: ${ep.name}`, status,
      `${r._ms}ms (threshold: ${ep.threshold}ms)`, r._ms));
  }
  return results;
}

// ---------------------------------------------------------------------------
// Runner
// ---------------------------------------------------------------------------

async function runFrontendTests() {
  const results = [];

  // Page loading
  results.push(await testRootPageLoads());
  results.push(await testLoginPageHTML());
  results.push(await testDashboardPageHTML());

  // Static assets
  results.push(await testJSBundleLoads());
  results.push(await testCSSLoads());
  results.push(await testFaviconExists());

  // Dark mode
  results.push(await testDarkModeCSS());

  // SPA routes
  const spaResults = await testSPARoutes();
  results.push(...spaResults);

  // TLS
  results.push(await testTLSCertificate());

  // API integration (frontend perspective)
  results.push(await testLoginAPIFromFrontend());
  results.push(await testDevicesAPIForFrontend());
  results.push(await testAlertsAPIForFrontend());
  results.push(await testDashboardStatsAPIForFrontend());
  results.push(await testScriptsAPIForFrontend());
  results.push(await testTicketsAPIForFrontend());

  // Performance
  const perfResults = await testResponseTime();
  results.push(...perfResults);

  return results;
}

module.exports = { runFrontendTests };
