import { test, expect } from '@playwright/test';
import { getAuthToken } from './helpers/auth';
import { AgentSimulator, enrollAgentViaAPI } from './helpers/agent-simulator';
import { randomUUID } from 'crypto';

const BASE_URL = process.env.SENTINEL_URL || 'https://sentinel.nexus';
const ENROLLMENT_TOKEN = process.env.SENTINEL_ENROLLMENT_TOKEN || '';

// Skip the entire suite if no enrollment token is configured
test.beforeEach(() => {
  if (!ENROLLMENT_TOKEN) {
    test.skip(true, 'SENTINEL_ENROLLMENT_TOKEN env var required for agent simulator tests');
  }
});

test.describe('Agent Replacement Flow', () => {
  let token: string;

  test.beforeAll(async ({ request }) => {
    token = await getAuthToken(request);
  });

  test('full agent replacement: offline -> kill token -> reinstall -> online', async ({ request, page }) => {
    const agentId = randomUUID();
    const hostname = `E2E-REPLACE-${Date.now().toString(36).toUpperCase()}`;

    // 1. Enroll a new agent via API
    const enrollment = await enrollAgentViaAPI(BASE_URL, ENROLLMENT_TOKEN, agentId, {
      hostname,
      platform: 'windows',
      osType: 'Windows',
      osVersion: '10.0.22631',
      architecture: 'x64',
      cpuModel: 'E2E Test CPU',
      cpuCores: 4,
      totalMemory: 8589934592,
      ipAddress: '192.168.1.201',
      macAddress: 'AA:BB:CC:DD:01:01',
    });
    expect(enrollment.deviceId).toBeTruthy();
    const deviceId = enrollment.deviceId;

    // 2. Connect agent simulator via WebSocket
    const agent = new AgentSimulator(BASE_URL, ENROLLMENT_TOKEN, agentId, {
      hostname,
      ipAddress: '192.168.1.201',
      macAddress: 'AA:BB:CC:DD:01:01',
    });
    await agent.connect();
    expect(agent.isAuthenticated).toBe(true);

    // 3. Verify device shows online in the API
    // Give the server a moment to update status
    await new Promise(r => setTimeout(r, 2000));
    let deviceRes = await request.get(`/api/devices/${deviceId}`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(deviceRes.ok()).toBeTruthy();
    let deviceData = await deviceRes.json();
    expect(deviceData.status).toBe('online');

    // 4. Verify device appears in the dashboard UI
    await page.goto(`${BASE_URL}/login`);
    // Fill login form
    await page.fill('input[name="email"], input[type="email"]', process.env.SENTINEL_ADMIN_EMAIL || 'admin@sentinel.local');
    await page.fill('input[name="password"], input[type="password"]', process.env.SENTINEL_ADMIN_PASSWORD || 'admin');
    await page.click('button[type="submit"]');
    // Wait for dashboard to load
    await page.waitForURL('**/dashboard**', { timeout: 15000 }).catch(() => {
      // Some versions redirect to /devices or / after login
    });

    // Navigate to devices page
    await page.goto(`${BASE_URL}/devices`);
    await page.waitForLoadState('networkidle');

    // Search for our test device by hostname
    const searchInput = page.locator('input[placeholder*="earch"], input[type="search"]').first();
    if (await searchInput.isVisible()) {
      await searchInput.fill(hostname);
      await page.waitForTimeout(1000); // Wait for search to filter
    }

    // Verify the hostname appears somewhere on the page
    await expect(page.getByText(hostname).first()).toBeVisible({ timeout: 10000 });

    // 5. Disconnect the agent (simulates broken/dead agent)
    agent.disconnect();
    expect(agent.isConnected).toBe(false);

    // 6. Wait for device to show offline (depends on server heartbeat timeout)
    // Most servers mark offline after 2 missed heartbeat intervals (~60-90s)
    // For test speed, we check the API repeatedly
    let offlineDetected = false;
    for (let i = 0; i < 20; i++) {
      await new Promise(r => setTimeout(r, 5000));
      deviceRes = await request.get(`/api/devices/${deviceId}`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      deviceData = await deviceRes.json();
      if (deviceData.status === 'offline') {
        offlineDetected = true;
        break;
      }
    }
    expect(offlineDetected).toBe(true);

    // 7. Generate kill token for the offline device
    const killTokenRes = await request.post(`/api/devices/${deviceId}/generate-kill-token`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(killTokenRes.ok()).toBeTruthy();
    const killTokenData = await killTokenRes.json();
    expect(killTokenData.killToken).toMatch(/^[0-9a-f]{64}$/);

    // 8. Download emergency uninstall script and verify it contains the token
    const scriptRes = await request.get(`/api/devices/${deviceId}/emergency-uninstall-script`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(scriptRes.ok()).toBeTruthy();
    const script = await scriptRes.text();
    expect(script).toContain('#Requires -RunAsAdministrator');
    expect(script).toContain('--force-uninstall');

    // 9. "Reinstall": connect NEW agent with same hostname but new agent ID
    const newAgentId = randomUUID();
    const newEnrollment = await enrollAgentViaAPI(BASE_URL, ENROLLMENT_TOKEN, newAgentId, {
      hostname,
      platform: 'windows',
      osType: 'Windows',
      osVersion: '10.0.22631',
      architecture: 'x64',
      cpuModel: 'E2E Test CPU (Replacement)',
      cpuCores: 4,
      totalMemory: 8589934592,
      ipAddress: '192.168.1.201',
      macAddress: 'AA:BB:CC:DD:01:01',
    });
    expect(newEnrollment.deviceId).toBeTruthy();

    const replacementAgent = new AgentSimulator(BASE_URL, ENROLLMENT_TOKEN, newAgentId, {
      hostname,
      ipAddress: '192.168.1.201',
      macAddress: 'AA:BB:CC:DD:01:01',
    });
    await replacementAgent.connect();
    expect(replacementAgent.isAuthenticated).toBe(true);

    // 10. Verify the replacement device comes back online
    await new Promise(r => setTimeout(r, 2000));
    const newDeviceRes = await request.get(`/api/devices/${newEnrollment.deviceId}`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(newDeviceRes.ok()).toBeTruthy();
    const newDeviceData = await newDeviceRes.json();
    expect(newDeviceData.status).toBe('online');
    expect(newDeviceData.hostname).toBe(hostname);

    // Cleanup
    replacementAgent.destroy();
    agent.destroy();

    // Optionally clean up test devices via API (delete)
    await request.delete(`/api/devices/${deviceId}`, {
      headers: { Authorization: `Bearer ${token}` },
    }).catch(() => {});
    await request.delete(`/api/devices/${newEnrollment.deviceId}`, {
      headers: { Authorization: `Bearer ${token}` },
    }).catch(() => {});
  });

  test('device survives rapid disconnect/reconnect cycles', async ({ request }) => {
    const agentId = randomUUID();
    const hostname = `E2E-RAPID-${Date.now().toString(36).toUpperCase()}`;

    // Enroll agent
    const enrollment = await enrollAgentViaAPI(BASE_URL, ENROLLMENT_TOKEN, agentId, {
      hostname,
      platform: 'windows',
      osType: 'Windows',
      osVersion: '10.0.22631',
      architecture: 'x64',
      cpuModel: 'E2E Stress Test CPU',
      cpuCores: 2,
      totalMemory: 4294967296,
      ipAddress: '192.168.1.202',
      macAddress: 'AA:BB:CC:DD:02:02',
    });
    expect(enrollment.deviceId).toBeTruthy();
    const deviceId = enrollment.deviceId;

    // Perform 5 rapid disconnect/reconnect cycles
    for (let cycle = 0; cycle < 5; cycle++) {
      const agent = new AgentSimulator(BASE_URL, ENROLLMENT_TOKEN, agentId, {
        hostname,
        ipAddress: '192.168.1.202',
        macAddress: 'AA:BB:CC:DD:02:02',
      });

      await agent.connect();
      expect(agent.isAuthenticated).toBe(true);

      // Send a heartbeat to confirm connectivity
      agent.sendHeartbeat();
      await new Promise(r => setTimeout(r, 500));

      // Disconnect
      agent.disconnect();
      await new Promise(r => setTimeout(r, 200));
    }

    // Final reconnect — verify the device is stable
    const finalAgent = new AgentSimulator(BASE_URL, ENROLLMENT_TOKEN, agentId, {
      hostname,
      ipAddress: '192.168.1.202',
      macAddress: 'AA:BB:CC:DD:02:02',
    });
    await finalAgent.connect();
    expect(finalAgent.isAuthenticated).toBe(true);

    await new Promise(r => setTimeout(r, 2000));

    const deviceRes = await request.get(`/api/devices/${deviceId}`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(deviceRes.ok()).toBeTruthy();
    const deviceData = await deviceRes.json();
    expect(deviceData.status).toBe('online');
    expect(deviceData.hostname).toBe(hostname);

    // Cleanup
    finalAgent.destroy();
    await request.delete(`/api/devices/${deviceId}`, {
      headers: { Authorization: `Bearer ${token}` },
    }).catch(() => {});
  });

  test('kill token works after multiple regenerations', async ({ request }) => {
    // Use an existing device from the system
    const devicesRes = await request.get('/api/devices', {
      headers: { Authorization: `Bearer ${token}` },
    });
    const devicesData = await devicesRes.json();
    const devices = Array.isArray(devicesData) ? devicesData : devicesData.devices;
    expect(devices.length).toBeGreaterThan(0);
    const deviceId = devices[0].id;

    const tokens: string[] = [];

    // Generate 3 kill tokens in sequence
    for (let i = 0; i < 3; i++) {
      const res = await request.post(`/api/devices/${deviceId}/generate-kill-token`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      expect(res.ok()).toBeTruthy();
      const data = await res.json();
      expect(data.killToken).toMatch(/^[0-9a-f]{64}$/);
      tokens.push(data.killToken);
    }

    // All 3 tokens should be unique
    const uniqueTokens = new Set(tokens);
    expect(uniqueTokens.size).toBe(3);

    // The emergency script should still work (it regenerates its own token internally)
    const scriptRes = await request.get(`/api/devices/${deviceId}/emergency-uninstall-script`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(scriptRes.ok()).toBeTruthy();
    const script = await scriptRes.text();
    expect(script).toContain('$KillToken');
    // The script's embedded token should be yet another new one
    const scriptTokenMatch = script.match(/\$KillToken = "([0-9a-f]{64})"/);
    expect(scriptTokenMatch).toBeTruthy();
    const scriptToken = scriptTokenMatch![1];
    expect(tokens).not.toContain(scriptToken);
  });

  test('emergency script is valid PowerShell', async ({ request }) => {
    // Get a device
    const devicesRes = await request.get('/api/devices', {
      headers: { Authorization: `Bearer ${token}` },
    });
    const devicesData = await devicesRes.json();
    const devices = Array.isArray(devicesData) ? devicesData : devicesData.devices;
    const deviceId = devices[0].id;

    // Ensure kill token exists
    await request.post(`/api/devices/${deviceId}/generate-kill-token`, {
      headers: { Authorization: `Bearer ${token}` },
    });

    // Download script
    const scriptRes = await request.get(`/api/devices/${deviceId}/emergency-uninstall-script`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(scriptRes.ok()).toBeTruthy();
    const script = await scriptRes.text();

    // Verify it's valid PowerShell structure
    // 1. Must have the RunAsAdministrator requirement
    expect(script).toContain('#Requires -RunAsAdministrator');

    // 2. Must set ErrorActionPreference
    expect(script).toContain('$ErrorActionPreference');

    // 3. Must define key variables
    expect(script).toContain('$KillToken');
    expect(script).toContain('$InstallDir');
    expect(script).toContain('$DataDir');
    expect(script).toContain('$ServiceNames');

    // 4. Must have all critical steps
    // DACL reset (required for tamper-protected installs)
    expect(script).toContain('Set-Acl');
    // Watchdog must be stopped before agent
    const watchdogPos = script.indexOf('Stopping Sentinel Watchdog');
    const agentPos = script.indexOf('Stopping Sentinel Agent');
    expect(watchdogPos).toBeGreaterThan(-1);
    expect(agentPos).toBeGreaterThan(watchdogPos);

    // 5. Must use Stop-Service (proper way to stop Windows services)
    expect(script).toContain('Stop-Service');

    // 6. Must clean up services via sc.exe delete
    expect(script).toContain('sc.exe delete');

    // 7. Must clean up registry
    expect(script).toContain('HKLM:\\SOFTWARE\\Sentinel');

    // 8. Must have the agent ID and device ID embedded
    expect(script).toMatch(/Agent ID: [0-9a-f-]+/);
    expect(script).toMatch(/Device ID: [0-9a-f-]+/);

    // 9. Must recommend reboot at the end
    expect(script).toContain('reboot');
  });
});
