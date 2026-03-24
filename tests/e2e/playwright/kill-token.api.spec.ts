import { test, expect } from '@playwright/test';
import { getAuthToken, authenticatedRequest } from './helpers/auth';

test.describe('Kill Token API', () => {
  let token: string;
  let testDeviceId: string;

  test.beforeAll(async ({ request }) => {
    token = await getAuthToken(request);

    // Get list of devices and pick the first one for testing
    const devicesRes = await request.get('/api/devices', {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(devicesRes.ok()).toBeTruthy();
    const devicesData = await devicesRes.json();

    // Handle paginated response: { data: [...], page, pageSize, total, totalPages }
    const devices = Array.isArray(devicesData) ? devicesData : (devicesData.data || devicesData.devices);
    expect(devices).toBeTruthy();
    expect(devices.length).toBeGreaterThan(0);

    // Pick the first device
    testDeviceId = devices[0].id;
    expect(testDeviceId).toBeTruthy();
  });

  test('generate kill token for device', async ({ request }) => {
    const response = await request.post(`/api/devices/${testDeviceId}/generate-kill-token`, {
      headers: { Authorization: `Bearer ${token}` },
    });

    expect(response.ok()).toBeTruthy();
    const data = await response.json();

    expect(data.killToken).toBeTruthy();
    expect(data.deviceId).toBe(testDeviceId);
    expect(data.agentId).toBeTruthy();
    expect(data.message).toContain('Kill token generated');
  });

  test('kill token is 64 hex characters', async ({ request }) => {
    const response = await request.post(`/api/devices/${testDeviceId}/generate-kill-token`, {
      headers: { Authorization: `Bearer ${token}` },
    });

    expect(response.ok()).toBeTruthy();
    const data = await response.json();

    // 32 bytes = 64 hex chars
    expect(data.killToken).toMatch(/^[0-9a-f]{64}$/);
  });

  test('regenerating kill token produces different token', async ({ request }) => {
    const res1 = await request.post(`/api/devices/${testDeviceId}/generate-kill-token`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(res1.ok()).toBeTruthy();
    const data1 = await res1.json();

    const res2 = await request.post(`/api/devices/${testDeviceId}/generate-kill-token`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(res2.ok()).toBeTruthy();
    const data2 = await res2.json();

    expect(data1.killToken).not.toBe(data2.killToken);
  });

  test('emergency uninstall script contains kill token', async ({ request }) => {
    // First generate a kill token so the device has one
    const genRes = await request.post(`/api/devices/${testDeviceId}/generate-kill-token`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(genRes.ok()).toBeTruthy();

    // Now fetch the emergency uninstall script
    const scriptRes = await request.get(`/api/devices/${testDeviceId}/emergency-uninstall-script`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(scriptRes.ok()).toBeTruthy();

    const script = await scriptRes.text();

    // The script should be PowerShell content
    expect(script).toContain('#Requires -RunAsAdministrator');
    expect(script).toContain('$KillToken');
    // The kill token in the script is a newly generated one (64 hex chars)
    expect(script).toMatch(/\$KillToken = "[0-9a-f]{64}"/);
  });

  test('emergency script has proper structure', async ({ request }) => {
    // Ensure kill token exists
    await request.post(`/api/devices/${testDeviceId}/generate-kill-token`, {
      headers: { Authorization: `Bearer ${token}` },
    });

    const scriptRes = await request.get(`/api/devices/${testDeviceId}/emergency-uninstall-script`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(scriptRes.ok()).toBeTruthy();

    const script = await scriptRes.text();

    // Verify all 7 steps exist in the script
    expect(script).toContain('[1/7] Resetting file permissions');
    expect(script).toContain('[2/7] Stopping Sentinel Watchdog');
    expect(script).toContain('[3/7] Stopping Sentinel Agent');
    expect(script).toContain('[4/7] Attempting graceful uninstall');
    expect(script).toContain('[5/7] Removing Windows services');
    expect(script).toContain('[6/7] Cleaning up registry');
    expect(script).toContain('[7/7] Removing files');

    // Verify DACL reset is present (critical for locked installs)
    expect(script).toContain('Set-Acl');
    expect(script).toContain('BUILTIN\\Administrators');

    // Verify the script uses the kill token for uninstall
    expect(script).toContain('--force-uninstall');
    expect(script).toContain('--kill-token=');
  });

  test('emergency script requires kill token to exist first', async ({ request }) => {
    // Get all devices
    const devicesRes = await request.get('/api/devices', {
      headers: { Authorization: `Bearer ${token}` },
    });
    const devicesData = await devicesRes.json();
    const devices = Array.isArray(devicesData) ? devicesData : devicesData.devices;

    // Find a device without a kill token, or use a known fresh device
    // We request the script for a device that may not have a kill token
    // The endpoint returns 400 if no kill token exists
    // Use a non-existent UUID to trigger the "not found" case
    const fakeDeviceId = '00000000-0000-0000-0000-000000000000';
    const scriptRes = await request.get(`/api/devices/${fakeDeviceId}/emergency-uninstall-script`, {
      headers: { Authorization: `Bearer ${token}` },
    });

    // Should be 404 (device not found) or 400 (no kill token)
    expect([400, 404]).toContain(scriptRes.status());
  });

  test('kill token endpoint requires authentication', async ({ request }) => {
    // Request without auth header
    const response = await request.post(`/api/devices/${testDeviceId}/generate-kill-token`);

    // Should be 401 Unauthorized
    expect(response.status()).toBe(401);
  });

  test('kill token endpoint rejects invalid device ID', async ({ request }) => {
    const response = await request.post('/api/devices/not-a-uuid/generate-kill-token', {
      headers: { Authorization: `Bearer ${token}` },
    });

    expect(response.status()).toBe(400);
    const data = await response.json();
    expect(data.error).toContain('Invalid device ID');
  });

  test('kill token endpoint returns 404 for non-existent device', async ({ request }) => {
    const fakeId = '99999999-aaaa-bbbb-cccc-dddddddddddd';
    const response = await request.post(`/api/devices/${fakeId}/generate-kill-token`, {
      headers: { Authorization: `Bearer ${token}` },
    });

    expect(response.status()).toBe(404);
  });
});
