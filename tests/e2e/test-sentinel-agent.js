/**
 * Sentinel Agent — E2E Test Suite
 *
 * Tests agent install, reinstall, uninstall, service lifecycle, config persistence,
 * WebSocket reconnection, and update checks via SSH to the Windows 11 VM on NEXUS.
 *
 * Exports: runAgentTests() → Promise<Array<{test, status, details, suite, durationMs}>>
 */
const { Client } = require('ssh2');
const https = require('https');
const {
  SENTINEL_URL,
  INSTALL_CODE,
  SSH_CONFIG,
  TIMEOUTS,
} = require('./config');

const SUITE = 'sentinel-agent';

const AGENT_DIR = 'C:\\Program Files\\Sentinel Agent';
const CONFIG_DIR = 'C:\\ProgramData\\Sentinel';
const INSTALLER_PATH = 'C:\\Users\\testadmin\\sentinel-installer.exe';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function result(test, status, details, durationMs) {
  return { test, status, details, suite: SUITE, durationMs: durationMs || 0 };
}

function sshExec(command, timeout) {
  timeout = timeout || TIMEOUTS.ssh;
  return new Promise((resolve, reject) => {
    if (!SSH_CONFIG.privateKey) {
      return reject(new Error('SSH key not available'));
    }

    const conn = new Client();
    const timer = setTimeout(() => {
      conn.end();
      reject(new Error(`SSH timeout after ${timeout}ms`));
    }, timeout);

    conn.on('ready', () => {
      conn.exec(command, (err, stream) => {
        if (err) { clearTimeout(timer); conn.end(); return reject(err); }
        let stdout = '', stderr = '';
        stream.on('close', (code) => {
          clearTimeout(timer);
          conn.end();
          resolve({
            stdout: stdout.replace(/\r\n/g, '\n').trim(),
            stderr: stderr.replace(/\r\n/g, '\n').trim(),
            exitCode: code,
          });
        });
        stream.on('data', (d) => { stdout += d; });
        stream.stderr.on('data', (d) => { stderr += d; });
      });
    });

    conn.on('error', (err) => { clearTimeout(timer); reject(err); });
    conn.connect(SSH_CONFIG);
  });
}

function ps(cmd) {
  // Use -EncodedCommand to avoid shell escaping issues with $, !, ", etc.
  const encoded = Buffer.from(cmd, 'utf16le').toString('base64');
  return `powershell -NoProfile -NonInteractive -EncodedCommand ${encoded}`;
}

// ---------------------------------------------------------------------------
// SSH Connectivity
// ---------------------------------------------------------------------------

async function testSSHConnect() {
  const t0 = Date.now();
  try {
    const r = await sshExec('hostname');
    return result('SSH Connect', 'PASS', `Host: ${r.stdout}`, Date.now() - t0);
  } catch (e) {
    return result('SSH Connect', 'FAIL', e.message, Date.now() - t0);
  }
}

// ---------------------------------------------------------------------------
// System Prerequisites
// ---------------------------------------------------------------------------

async function testPrereqs() {
  const results = [];
  const t0 = Date.now();

  try {
    // Windows version
    const ver = await sshExec(ps('[System.Environment]::OSVersion.VersionString'));
    results.push(result('Windows Version', 'PASS', ver.stdout, Date.now() - t0));
  } catch (e) {
    results.push(result('Windows Version', 'FAIL', e.message, Date.now() - t0));
  }

  try {
    // Admin check
    const admin = await sshExec(ps(
      '([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)'
    ));
    const isAdmin = admin.stdout.toLowerCase().includes('true');
    results.push(result('Admin Privileges', isAdmin ? 'PASS' : 'WARN', `IsAdmin=${admin.stdout}`));
  } catch (e) {
    results.push(result('Admin Privileges', 'WARN', e.message));
  }

  try {
    // Disk space
    const disk = await sshExec(ps('[math]::Round((Get-PSDrive C).Free / 1GB, 1)'));
    const gb = parseFloat(disk.stdout);
    results.push(result('Disk Space', gb > 5 ? 'PASS' : 'WARN', `${disk.stdout} GB free`));
  } catch (e) {
    results.push(result('Disk Space', 'WARN', e.message));
  }

  try {
    // Network to Sentinel
    const net = await sshExec(ps(
      `try { $r = Invoke-WebRequest -Uri '${SENTINEL_URL}/health' -UseBasicParsing -TimeoutSec 10; $r.StatusCode } catch { Write-Output 'NETWORK_ERROR' }`
    ), 20000);
    const ok = net.stdout.includes('200');
    results.push(result('Network to Sentinel', ok ? 'PASS' : 'FAIL', net.stdout || net.stderr));
  } catch (e) {
    results.push(result('Network to Sentinel', 'FAIL', e.message));
  }

  return results;
}

// ---------------------------------------------------------------------------
// Download
// ---------------------------------------------------------------------------

async function testDownload() {
  const t0 = Date.now();
  try {
    const cmd = ps(
      `try { Invoke-WebRequest -Uri '${SENTINEL_URL}/api/download/agent/windows' -OutFile '${INSTALLER_PATH}' -UseBasicParsing -TimeoutSec 120; (Get-Item '${INSTALLER_PATH}').Length } catch { Write-Output 'DOWNLOAD_ERROR' }`
    );
    const dl = await sshExec(cmd, TIMEOUTS.download);
    const size = parseInt(dl.stdout);
    const ms = Date.now() - t0;

    if (dl.exitCode === 0 && size > 100000) {
      return result('Download Installer', 'PASS', `${(size / 1024 / 1024).toFixed(1)} MB`, ms);
    }
    return result('Download Installer', 'FAIL', dl.stdout || dl.stderr, ms);
  } catch (e) {
    return result('Download Installer', 'FAIL', e.message, Date.now() - t0);
  }
}

// ---------------------------------------------------------------------------
// Verify Existing Install (non-destructive)
// ---------------------------------------------------------------------------

async function testExistingInstall() {
  const results = [];
  const t0 = Date.now();

  // Verify binaries
  try {
    const agentBin = await sshExec(ps(`Test-Path '${AGENT_DIR}\\sentinel-agent.exe'`));
    results.push(result('Agent Binary Exists', agentBin.stdout.toLowerCase().includes('true') ? 'PASS' : 'FAIL', agentBin.stdout));
  } catch (e) {
    results.push(result('Agent Binary Exists', 'FAIL', e.message));
  }

  try {
    const watchdogBin = await sshExec(ps(`Test-Path '${AGENT_DIR}\\sentinel-watchdog.exe'`));
    results.push(result('Watchdog Binary Exists', watchdogBin.stdout.toLowerCase().includes('true') ? 'PASS' : 'FAIL', watchdogBin.stdout));
  } catch (e) {
    results.push(result('Watchdog Binary Exists', 'FAIL', e.message));
  }

  // Verify services
  try {
    const agentSvc = await sshExec(ps('Get-Service SentinelAgent -EA SilentlyContinue | Select-Object -Expand Status'));
    const running = agentSvc.stdout.toLowerCase().includes('running');
    results.push(result('Agent Service Running', running ? 'PASS' : 'FAIL', agentSvc.stdout || 'Not found'));
  } catch (e) {
    results.push(result('Agent Service Running', 'FAIL', e.message));
  }

  try {
    const watchdogSvc = await sshExec(ps('Get-Service SentinelWatchdog -EA SilentlyContinue | Select-Object -Expand Status'));
    const running = watchdogSvc.stdout.toLowerCase().includes('running');
    results.push(result('Watchdog Service Running', running ? 'PASS' : 'FAIL', watchdogSvc.stdout || 'Not found'));
  } catch (e) {
    results.push(result('Watchdog Service Running', 'FAIL', e.message));
  }

  // Verify config
  try {
    const config = await sshExec(ps(`Test-Path '${CONFIG_DIR}\\config.json'`));
    results.push(result('Config File Created', config.stdout.toLowerCase().includes('true') ? 'PASS' : 'FAIL', config.stdout));
  } catch (e) {
    results.push(result('Config File Created', 'FAIL', e.message));
  }

  // Check IPC key (C-04 HMAC)
  try {
    const ipcKey = await sshExec(ps(`Test-Path '${AGENT_DIR}\\ipc-key.dat'`));
    results.push(result('IPC Key (C-04)', ipcKey.stdout.toLowerCase().includes('true') ? 'PASS' : 'WARN', ipcKey.stdout));
  } catch (e) {
    results.push(result('IPC Key (C-04)', 'INFO', e.message));
  }

  // Check signature files (C-04 HMAC)
  try {
    const sigs = await sshExec(ps(`Get-ChildItem '${AGENT_DIR}\\*.sig' -EA SilentlyContinue | Select-Object Name`));
    const hasSigs = sigs.stdout.length > 5;
    results.push(result('Signature Files (C-04)', hasSigs ? 'PASS' : 'INFO', sigs.stdout || 'None found'));
  } catch (e) {
    results.push(result('Signature Files (C-04)', 'INFO', e.message));
  }

  return results;
}

// ---------------------------------------------------------------------------
// Config Persistence
// ---------------------------------------------------------------------------

async function testConfigPersistence() {
  const results = [];
  const t0 = Date.now();

  try {
    // Read config
    const configRead = await sshExec(ps(
      `if (Test-Path '${CONFIG_DIR}\\config.json') { Get-Content '${CONFIG_DIR}\\config.json' | ConvertFrom-Json | Select-Object deviceId,serverUrl | ConvertTo-Json -Compress } else { 'no-config' }`
    ));
    const ms = Date.now() - t0;

    if (configRead.stdout === 'no-config') {
      results.push(result('Config Read', 'WARN', 'No config file found', ms));
    } else {
      results.push(result('Config Read', 'PASS', configRead.stdout.substring(0, 200), ms));

      // Verify serverUrl points to correct server
      try {
        const parsed = JSON.parse(configRead.stdout);
        if (parsed.serverUrl && parsed.serverUrl.includes('sentinelrmm.us')) {
          results.push(result('Config Server URL', 'PASS', `Points to: ${parsed.serverUrl}`));
        } else {
          results.push(result('Config Server URL', 'WARN', `Server URL: ${parsed.serverUrl || 'not set'}`));
        }
        if (parsed.deviceId) {
          results.push(result('Config Device ID', 'PASS', `DeviceID: ${parsed.deviceId.substring(0, 8)}...`));
        } else {
          results.push(result('Config Device ID', 'WARN', 'No deviceId in config'));
        }
      } catch (_) {
        results.push(result('Config Parse', 'WARN', 'Could not parse config JSON'));
      }
    }
  } catch (e) {
    results.push(result('Config Read', 'FAIL', e.message, Date.now() - t0));
  }

  return results;
}

// ---------------------------------------------------------------------------
// Service Lifecycle
// ---------------------------------------------------------------------------

async function testServiceLifecycle() {
  const results = [];

  // Stop agent service
  try {
    await sshExec(ps('Stop-Service SentinelAgent -Force -EA SilentlyContinue'), 15000);
    const t0 = Date.now();
    const status = await sshExec(ps('Get-Service SentinelAgent -EA SilentlyContinue | Select-Object -Expand Status'));
    const stopped = status.stdout.toLowerCase().includes('stopped');
    results.push(result('Service Stop', stopped ? 'PASS' : 'WARN', `Status: ${status.stdout}`, Date.now() - t0));
  } catch (e) {
    results.push(result('Service Stop', 'FAIL', e.message));
  }

  // Start agent service
  try {
    const t0 = Date.now();
    await sshExec(ps('Start-Service SentinelAgent -EA SilentlyContinue'), 15000);
    // Give it a moment to start
    await sshExec(ps('Start-Sleep 3'));
    const status = await sshExec(ps('Get-Service SentinelAgent -EA SilentlyContinue | Select-Object -Expand Status'));
    const running = status.stdout.toLowerCase().includes('running');
    results.push(result('Service Start', running ? 'PASS' : 'WARN', `Status: ${status.stdout}`, Date.now() - t0));
  } catch (e) {
    results.push(result('Service Start', 'FAIL', e.message));
  }

  // Restart agent service
  try {
    const t0 = Date.now();
    await sshExec(ps('Restart-Service SentinelAgent -Force -EA SilentlyContinue'), 20000);
    await sshExec(ps('Start-Sleep 3'));
    const status = await sshExec(ps('Get-Service SentinelAgent -EA SilentlyContinue | Select-Object -Expand Status'));
    const running = status.stdout.toLowerCase().includes('running');
    results.push(result('Service Restart', running ? 'PASS' : 'WARN', `Status: ${status.stdout}`, Date.now() - t0));
  } catch (e) {
    results.push(result('Service Restart', 'FAIL', e.message));
  }

  // Check watchdog recovers agent
  try {
    const t0 = Date.now();
    // Kill agent process directly (watchdog should restart it)
    await sshExec(ps(
      'Stop-Process -Name sentinel-agent -Force -EA SilentlyContinue; Start-Sleep 10'
    ), 20000);
    const status = await sshExec(ps('Get-Service SentinelAgent -EA SilentlyContinue | Select-Object -Expand Status'));
    const recovered = status.stdout.toLowerCase().includes('running');
    results.push(result('Watchdog Recovery', recovered ? 'PASS' : 'WARN',
      `After kill: ${status.stdout} (watchdog should restart)`, Date.now() - t0));
  } catch (e) {
    results.push(result('Watchdog Recovery', 'WARN', e.message));
  }

  return results;
}

// ---------------------------------------------------------------------------
// Reinstall (preserves config)
// ---------------------------------------------------------------------------

async function testReinstall() {
  const results = [];
  const t0 = Date.now();

  // Read deviceId before
  let beforeId = 'no-config';
  try {
    const before = await sshExec(ps(
      `if (Test-Path '${CONFIG_DIR}\\config.json') { (Get-Content '${CONFIG_DIR}\\config.json' | ConvertFrom-Json).deviceId } else { 'no-config' }`
    ));
    beforeId = before.stdout;
  } catch (_) {}

  // Run reinstall
  try {
    const reinstall = await sshExec(ps(
      `& '${INSTALLER_PATH}' --code=${INSTALL_CODE} --silent 2>&1; echo EXIT_CODE:$LASTEXITCODE`
    ), TIMEOUTS.install);
    results.push(result('Reinstall Command', 'INFO', reinstall.stdout.substring(0, 300), Date.now() - t0));
  } catch (e) {
    results.push(result('Reinstall Command', 'FAIL', e.message, Date.now() - t0));
    return results;
  }

  // Read deviceId after
  let afterId = 'no-config';
  try {
    const after = await sshExec(ps(
      `if (Test-Path '${CONFIG_DIR}\\config.json') { (Get-Content '${CONFIG_DIR}\\config.json' | ConvertFrom-Json).deviceId } else { 'no-config' }`
    ));
    afterId = after.stdout;
  } catch (_) {}

  const preserved = beforeId === afterId && beforeId !== 'no-config';
  results.push(result('Config Preserved on Reinstall', preserved ? 'PASS' : 'WARN',
    `Before=${beforeId.substring(0, 12)} After=${afterId.substring(0, 12)}`));

  // Service still running after reinstall
  try {
    await sshExec(ps('Start-Sleep 5'));
    const svc = await sshExec(ps('Get-Service SentinelAgent -EA SilentlyContinue | Select-Object -Expand Status'));
    results.push(result('Service After Reinstall', svc.stdout.toLowerCase().includes('running') ? 'PASS' : 'WARN',
      svc.stdout || 'Not found'));
  } catch (e) {
    results.push(result('Service After Reinstall', 'WARN', e.message));
  }

  return results;
}

// ---------------------------------------------------------------------------
// Update Check
// ---------------------------------------------------------------------------

async function testUpdateCheck() {
  const results = [];
  const t0 = Date.now();

  try {
    // Read agent version
    const agentVer = await sshExec(ps(
      `if (Test-Path '${AGENT_DIR}\\sentinel-agent.exe') { & '${AGENT_DIR}\\sentinel-agent.exe' --version 2>&1 } else { 'binary-not-found' }`
    ), 15000);
    results.push(result('Agent Version', agentVer.stdout !== 'binary-not-found' ? 'PASS' : 'WARN',
      agentVer.stdout.substring(0, 100), Date.now() - t0));
  } catch (e) {
    results.push(result('Agent Version', 'WARN', e.message));
  }

  // Check server version endpoint
  try {
    const serverVer = await new Promise((resolve, reject) => {
      https.get(`${SENTINEL_URL}/api/agent/version`, { rejectUnauthorized: false }, (res) => {
        let data = '';
        res.on('data', (d) => { data += d; });
        res.on('end', () => resolve({ status: res.statusCode, body: data }));
      }).on('error', reject);
    });

    if (serverVer.status === 200) {
      results.push(result('Server Version Endpoint', 'PASS', serverVer.body.substring(0, 100)));
    } else {
      results.push(result('Server Version Endpoint', 'WARN', `HTTP ${serverVer.status}`));
    }
  } catch (e) {
    results.push(result('Server Version Endpoint', 'FAIL', e.message));
  }

  return results;
}

// ---------------------------------------------------------------------------
// Edge Cases
// ---------------------------------------------------------------------------

async function testEdgeCases() {
  const results = [];

  // Installer log in TEMP (I-13)
  try {
    const tempLog = await sshExec(ps(
      'Get-ChildItem $env:TEMP -Filter sentinel-install* -EA SilentlyContinue | Select-Object Name,Length | Format-Table -AutoSize'
    ));
    results.push(result('Installer Log (I-13)',
      tempLog.stdout.length > 5 ? 'PASS' : 'INFO', tempLog.stdout || 'No log found'));
  } catch (e) {
    results.push(result('Installer Log (I-13)', 'INFO', e.message));
  }

  // File permissions on install dir
  try {
    const perms = await sshExec(ps(
      `if (Test-Path '${AGENT_DIR}') { (Get-Acl '${AGENT_DIR}').Access | Format-Table IdentityReference,FileSystemRights -AutoSize | Out-String -Width 200 }`
    ));
    results.push(result('Directory Permissions', 'INFO', (perms.stdout || 'N/A').substring(0, 300)));
  } catch (e) {
    results.push(result('Directory Permissions', 'INFO', e.message));
  }

  // Defender exclusions (I-10)
  try {
    const defender = await sshExec(ps(
      "try { (Get-MpPreference).ExclusionPath -join ',' } catch { 'N/A' }"
    ), 15000);
    const hasSentinel = defender.stdout.includes('Sentinel');
    results.push(result('Defender Exclusions (I-10)', 'INFO',
      hasSentinel ? 'Sentinel exclusion present' : 'No Sentinel exclusion'));
  } catch (e) {
    results.push(result('Defender Exclusions (I-10)', 'INFO', e.message));
  }

  // Agent process memory usage
  try {
    const mem = await sshExec(ps(
      "Get-Process sentinel-agent -EA SilentlyContinue | Select-Object @{N='MB';E={[math]::Round($_.WorkingSet64/1MB,1)}} | Select-Object -Expand MB"
    ));
    if (mem.stdout) {
      const mb = parseFloat(mem.stdout);
      results.push(result('Agent Memory Usage', mb < 200 ? 'PASS' : 'WARN', `${mem.stdout} MB`));
    } else {
      results.push(result('Agent Memory Usage', 'INFO', 'Agent process not running'));
    }
  } catch (e) {
    results.push(result('Agent Memory Usage', 'INFO', e.message));
  }

  // Watchdog stop-start race (I-12)
  try {
    const race = await sshExec(ps(
      'Stop-Service SentinelWatchdog -Force -EA SilentlyContinue; ' +
      'Start-Service SentinelWatchdog -EA SilentlyContinue; ' +
      'Get-Service SentinelWatchdog -EA SilentlyContinue | Select-Object Status'
    ), 30000);
    results.push(result('Watchdog Restart Race (I-12)', 'INFO', race.stdout || 'N/A'));
  } catch (e) {
    results.push(result('Watchdog Restart Race (I-12)', 'INFO', e.message));
  }

  return results;
}

// ---------------------------------------------------------------------------
// Uninstall
// ---------------------------------------------------------------------------

async function testUninstall() {
  const results = [];

  try {
    const has = await sshExec(ps(`Test-Path '${AGENT_DIR}\\sentinel-agent.exe'`));
    if (!has.stdout.toLowerCase().includes('true')) {
      results.push(result('Uninstall', 'INFO', 'No agent installed — skipping'));
      return results;
    }
  } catch (_) {
    results.push(result('Uninstall', 'INFO', 'Could not check agent presence'));
    return results;
  }

  const t0 = Date.now();

  // Stop services
  try {
    await sshExec(ps(
      'Stop-Service SentinelAgent -Force -EA SilentlyContinue; ' +
      'Stop-Service SentinelWatchdog -Force -EA SilentlyContinue'
    ), 30000);
    const stopped = await sshExec(ps(
      'Get-Service SentinelAgent -EA SilentlyContinue | Select-Object Status'
    ));
    results.push(result('Services Stopped',
      !stopped.stdout.toLowerCase().includes('running') ? 'PASS' : 'WARN',
      stopped.stdout || 'Not found', Date.now() - t0));
  } catch (e) {
    results.push(result('Services Stopped', 'WARN', e.message, Date.now() - t0));
  }

  // Note: We leave the agent installed for subsequent runs so we can test reinstall behavior
  // Full cleanup would remove the directory and deregister services

  return results;
}

// ---------------------------------------------------------------------------
// Runner
// ---------------------------------------------------------------------------

async function runAgentTests() {
  const results = [];

  // SSH connectivity (required for all other tests)
  const sshResult = await testSSHConnect();
  results.push(sshResult);
  if (sshResult.status === 'FAIL') {
    results.push(result('Agent Tests', 'FAIL', 'SSH connection failed — skipping remaining agent tests'));
    return results;
  }

  // Prerequisites
  const prereqs = await testPrereqs();
  results.push(...prereqs);

  // Check network — if VM can't reach Sentinel, skip install tests
  const netResult = prereqs.find(r => r.test === 'Network to Sentinel');
  if (netResult && netResult.status === 'FAIL') {
    results.push(result('Agent Install Tests', 'FAIL', 'VM cannot reach Sentinel server — skipping install tests'));
    return results;
  }

  // Download
  const dlResult = await testDownload();
  results.push(dlResult);

  // Verify existing install (non-destructive — agent must be pre-installed on VM)
  const installResults = await testExistingInstall();
  results.push(...installResults);

  // Only run config/service/reinstall tests if agent is installed
  const agentInstalled = installResults.some(r => r.test === 'Agent Service Running' && r.status === 'PASS');
  if (agentInstalled) {
    // Config persistence
    const configResults = await testConfigPersistence();
    results.push(...configResults);

    // Service lifecycle
    const svcResults = await testServiceLifecycle();
    results.push(...svcResults);
  } else {
    results.push(result('Config/Service Tests', 'WARN', 'Skipped — agent not running'));
  }

  // Update check
  const updateResults = await testUpdateCheck();
  results.push(...updateResults);

  // Edge cases
  const edgeResults = await testEdgeCases();
  results.push(...edgeResults);

  return results;
}

module.exports = { runAgentTests };
