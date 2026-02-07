/**
 * Web API Shim - Provides window.api interface for web mode
 *
 * In Electron, window.api is provided by the preload script.
 * In Web mode, we need to provide a compatible interface that uses HTTP/WebSocket.
 */
import { isWeb } from './env';
import { api } from './api';
import { wsService } from './websocket';

// Only create shim if in web mode and window.api doesn't exist
if (isWeb && typeof (window as any).api === 'undefined') {
  console.log('[WebApiShim] Creating window.api shim for web mode');

  // Event handlers storage
  const eventHandlers: Map<string, Set<(data: any) => void>> = new Map();

  // Terminal session to device mapping (needed for routing)
  const terminalSessionDevices: Map<string, string> = new Map();

  // Helper to register event handler
  const registerHandler = (event: string, handler: (data: any) => void) => {
    if (!eventHandlers.has(event)) {
      eventHandlers.set(event, new Set());
    }
    eventHandlers.get(event)!.add(handler);
    return () => {
      eventHandlers.get(event)?.delete(handler);
    };
  };

  // Helper to emit events
  const emitEvent = (event: string, data: any) => {
    eventHandlers.get(event)?.forEach(handler => {
      try {
        handler(data);
      } catch (err) {
        console.error(`[WebApiShim] Error in event handler for ${event}:`, err);
      }
    });
  };

  // Subscribe to WebSocket events and route them
  if (wsService) {
    wsService.on('*', (msg: any) => {
      if (msg.type) {
        // For 'response' type, pass the full message since requestId is at root level
        // For other types, pass just the data field for backward compatibility
        if (msg.type === 'response') {
          emitEvent(msg.type, msg);
        } else if (msg.type === 'device_metrics') {
          // Server sends 'device_metrics' but frontend expects 'metrics:updated'
          // The message format is: { type: 'device_metrics', deviceId: '...', metrics: {...} }
          // deviceStore expects: { deviceId: '...', metrics: {...} }
          const metricsData = {
            deviceId: msg.deviceId,
            metrics: msg.metrics,
            source: 'websocket',
          };
          console.log('[WebApiShim] device_metrics received:', metricsData.deviceId, 'CPU:', metricsData.metrics?.cpuPercent);
          emitEvent('metrics:updated', metricsData);
          emitEvent('device_metrics', metricsData);
        } else {
          emitEvent(msg.type, msg.data);
        }
      }
    });
  }

  // Create the complete shim matching ElectronAPI interface
  (window as any).api = {
    // Generic event subscription
    on: (channel: string, callback: (data: any) => void) => {
      return registerHandler(channel, callback);
    },

    // Devices API
    devices: {
      list: async (clientId?: string) => {
        const result = await api!.getDevices();
        return (result as any).devices || result || [];
      },
      get: async (id: string) => {
        return api!.getDevice(id);
      },
      ping: async (deviceId: string) => {
        return api!.pingAgent(deviceId);
      },
      delete: async (id: string) => {
        return api!.deleteDevice(id);
      },
      disable: async (id: string) => {
        return api!.updateDevice(id, { tags: ['disabled'] });
      },
      enable: async (id: string) => {
        return api!.updateDevice(id, { tags: [] });
      },
      uninstall: async (id: string) => {
        return api!.uninstallAgent(id);
      },
      update: async (id: string, updates: { displayName?: string; tags?: string[] }) => {
        return api!.updateDevice(id, updates);
      },
      getMetrics: async (deviceId: string, hours: number = 24) => {
        const result = await api!.getDeviceMetrics(deviceId, { limit: hours * 60 });
        return (result as any).metrics || result || [];
      },
      setMetricsInterval: async (deviceId: string, intervalMs: number) => {
        console.log(`[WebApiShim] setMetricsInterval: device=${deviceId}, interval=${intervalMs}ms`);
        // Send WebSocket message to server to change agent's metrics interval
        if (wsService) {
          wsService.send('set_metrics_interval', { deviceId, intervalMs });
          return { success: true };
        }
        return { success: false, error: 'WebSocket not connected' };
      },
    },

    // Commands API
    commands: {
      execute: async (deviceId: string, command: string, type: string) => {
        wsService?.send('execute_command', { deviceId, command, type });
        return { success: true };
      },
      getHistory: async (deviceId: string) => {
        return [];
      },
    },

    // Terminal API
    terminal: {
      start: async (deviceId: string) => {
        const sessionId = `term-${deviceId}-${Date.now()}`;
        // Store the deviceId for this session so we can include it in subsequent messages
        terminalSessionDevices.set(sessionId, deviceId);
        wsService?.send('start_terminal', { deviceId, sessionId, cols: 80, rows: 24 });
        return { sessionId };
      },
      send: async (sessionId: string, data: string) => {
        const deviceId = terminalSessionDevices.get(sessionId);
        wsService?.send('terminal_input', { sessionId, deviceId, data });
      },
      resize: async (sessionId: string, cols: number, rows: number) => {
        const deviceId = terminalSessionDevices.get(sessionId);
        wsService?.send('terminal_resize', { sessionId, deviceId, cols, rows });
      },
      close: async (sessionId: string) => {
        const deviceId = terminalSessionDevices.get(sessionId);
        wsService?.send('close_terminal', { sessionId, deviceId });
        // Clean up the mapping
        terminalSessionDevices.delete(sessionId);
      },
      onData: (callback: (data: string, sessionId?: string) => void) => {
        return registerHandler('terminal_output', (payload: any) => {
          if (payload && typeof payload === 'object' && payload.data) {
            callback(payload.data, payload.sessionId);
          } else if (payload && typeof payload === 'string') {
            callback(payload);
          }
        });
      },
    },

    // Files API
    files: {
      drives: async (deviceId: string) => {
        console.log('[WebApiShim] files.drives called for device:', deviceId);
        return new Promise((resolve, reject) => {
          const requestId = `drives-${Date.now()}`;
          console.log('[WebApiShim] Sending list_drives with requestId:', requestId);
          const timeout = setTimeout(() => {
            console.log('[WebApiShim] list_drives timeout for requestId:', requestId);
            reject(new Error('Timeout waiting for drives list'));
          }, 30000);

          const unsub = registerHandler('response', (data: any) => {
            console.log('[WebApiShim] Received response:', data);
            if (data.requestId === requestId) {
              clearTimeout(timeout);
              unsub();
              if (data.success) {
                console.log('[WebApiShim] list_drives success, drives:', data.data?.drives);
                resolve(data.data?.drives || []);
              } else {
                console.log('[WebApiShim] list_drives failed:', data.error);
                reject(new Error(data.error || 'Failed to get drives'));
              }
            }
          });

          wsService?.send('list_drives', { deviceId, requestId });
          console.log('[WebApiShim] list_drives message sent');
        });
      },
      list: async (deviceId: string, path: string) => {
        return new Promise((resolve, reject) => {
          const requestId = `files-${Date.now()}`;
          const timeout = setTimeout(() => {
            reject(new Error('Timeout'));
          }, 30000);

          const unsub = registerHandler('response', (data: any) => {
            if (data.requestId === requestId) {
              clearTimeout(timeout);
              unsub();
              if (data.success) {
                resolve(data.data?.files || []);
              } else {
                reject(new Error(data.error || 'Failed'));
              }
            }
          });

          wsService?.send('list_files', { deviceId, path, requestId });
        });
      },
      download: async (deviceId: string, remotePath: string, localPath: string) => {
        wsService?.send('download_file', { deviceId, remotePath, localPath });
        return { success: true };
      },
      upload: async (deviceId: string, localPath: string, remotePath: string) => {
        console.log('[WebApiShim] File upload not available in web mode');
        return { success: false, error: 'Not available in web mode' };
      },
      scan: async (deviceId: string, path: string, maxDepth: number) => {
        return [];
      },
      downloadToSandbox: async (deviceId: string, remotePath: string) => {
        wsService?.send('download_file', { deviceId, path: remotePath });
        return { success: true };
      },
      onProgress: (callback: (progress: any) => void) => {
        return registerHandler('files:progress', callback);
      },
    },

    // WebRTC API
    webrtc: {
      start: async (deviceId: string, offer: { type: string; sdp?: string; quality: string }) => {
        wsService?.send('webrtc_start', { deviceId, offer });
        return { success: true };
      },
      stop: async (deviceId: string) => {
        wsService?.send('webrtc_stop', { deviceId });
      },
      sendSignal: async (deviceId: string, signal: any) => {
        wsService?.send('webrtc_signal', { deviceId, signal });
      },
      setQuality: async (deviceId: string, quality: string) => {
        wsService?.send('webrtc_quality', { deviceId, quality });
      },
      onSignal: (callback: (signal: any) => void) => {
        return registerHandler('webrtc:signal', callback);
      },
    },

    // Alerts API
    alerts: {
      list: async () => {
        const result = await api!.getAlerts();
        return (result as any).alerts || result || [];
      },
      acknowledge: async (id: string) => {
        return api!.acknowledgeAlert(id);
      },
      dismiss: async (id: string) => {
        return api!.resolveAlert(id);
      },
      resolve: async (id: string) => {
        return api!.resolveAlert(id);
      },
      onNew: (callback: (alert: any) => void) => {
        return registerHandler('alerts:new', callback);
      },
    },

    // Settings API
    settings: {
      get: async () => {
        return {};
      },
      update: async (settings: any) => {
        console.log('[WebApiShim] Settings update not available in web mode');
        return { success: true };
      },
    },

    // Scripts API
    scripts: {
      list: async () => {
        const result = await api!.getScripts();
        return (result as any).scripts || result || [];
      },
      get: async (id: string) => {
        return api!.getScript(id);
      },
      create: async (script: any) => {
        return api!.createScript(script);
      },
      update: async (id: string, script: any) => {
        return api!.updateScript(id, script);
      },
      delete: async (id: string) => {
        return api!.deleteScript(id);
      },
      execute: async (deviceId: string, scriptId: string, parameters?: any) => {
        return api!.runScript(scriptId, [deviceId]);
      },
    },

    // Clients API
    clients: {
      list: async () => {
        const result = await api!.getClients();
        return (result as any).clients || result || [];
      },
      get: async (id: string) => {
        return api!.getClient(id);
      },
      create: async (client: any) => {
        return api!.createClient(client);
      },
      update: async (id: string, client: any) => {
        return api!.updateClient(id, client);
      },
      delete: async (id: string) => {
        return api!.deleteClient(id);
      },
      getDevices: async (clientId: string) => {
        const result = await api!.getDevices();
        const devices = (result as any).devices || result || [];
        return devices.filter((d: any) => d.clientId === clientId);
      },
      assignDevice: async (deviceId: string, clientId: string) => {
        return api!.updateDevice(deviceId, { clientId } as any);
      },
    },

    // Certificates API
    certificates: {
      list: async () => {
        try {
          const result = await api!.getDeviceCertStatuses();
          return result || [];
        } catch (e) {
          console.error('[WebApiShim] Failed to get certificates:', e);
          return [];
        }
      },
      get: async (id: string) => {
        return null;
      },
      download: async (id: string) => {
        return null;
      },
      verify: async (deviceId: string) => {
        return { valid: true };
      },
    },

    // Tickets API (stub - tickets may not be in web API)
    tickets: {
      list: async (filters?: any) => [],
      get: async (id: string) => null,
      create: async (ticket: any) => null,
      update: async (id: string, ticket: any) => null,
      delete: async (id: string) => {},
      addComment: async (ticketId: string, comment: any) => null,
      getStats: async () => ({ open: 0, inProgress: 0, resolved: 0, total: 0 }),
    },

    // Knowledge Base API
    knowledge: {
      list: async (filters?: any) => [],
      get: async (id: string) => null,
      create: async (article: any) => null,
      update: async (id: string, article: any) => null,
      delete: async (id: string) => {},
      search: async (query: string) => [],
    },

    // Updates API
    updates: {
      checkForUpdates: async () => ({ updateAvailable: false }),
      downloadUpdate: async () => {},
      installUpdate: () => {},
      getDevice: async (deviceId: string) => api!.getDevice(deviceId),
      onUpdateAvailable: (callback: (info: any) => void) => registerHandler('updates:available', callback),
      onDownloadProgress: (callback: (progress: any) => void) => registerHandler('updates:progress', callback),
      onUpdateDownloaded: (callback: (info: any) => void) => registerHandler('updates:downloaded', callback),
      onError: (callback: (error: any) => void) => registerHandler('updates:error', callback),
      onStatus: (callback: (status: any) => void) => registerHandler('updates:status', callback),
    },

    // Updater API (alias for updates with additional methods)
    updater: {
      checkForUpdates: async () => ({ updateAvailable: false }),
      downloadUpdate: async () => {},
      installUpdate: () => {},
      getVersion: async () => '1.70.0-web',
      onUpdateAvailable: (callback: (info: any) => void) => registerHandler('updates:available', callback),
      onUpdateNotAvailable: (callback: () => void) => registerHandler('updates:notAvailable', callback),
      onDownloadProgress: (callback: (progress: any) => void) => registerHandler('updates:progress', callback),
      onUpdateDownloaded: (callback: (info: any) => void) => registerHandler('updates:downloaded', callback),
      onError: (callback: (error: any) => void) => registerHandler('updates:error', callback),
      getDevice: async (deviceId: string) => api!.getDevice(deviceId),
      onStatus: (callback: (status: any) => void) => registerHandler('updates:status', callback),
    },

    // Portal API (optional)
    portal: {
      getPortal: async (subdomain: string) => null,
      updateBranding: async (subdomain: string, branding: any) => null,
      getDevices: async (subdomain: string) => [],
      getDevice: async (subdomain: string, deviceId: string) => null,
      getSettings: async () => ({}),
      getInfo: async () => ({ version: '1.70.0-web', port: 443, environment: 'production' }),
      updateSettings: async (settings: any) => null,
      getClientTenants: async () => {
        try {
          return await api!.getClientTenants();
        } catch (err) {
          console.error('[WebApiShim] Error getting client tenants:', err);
          return [];
        }
      },
      createClientTenant: async (data: { clientId?: string; tenantId: string; tenantName?: string }) => {
        try {
          return await api!.createClientTenant(data);
        } catch (err) {
          console.error('[WebApiShim] Error creating client tenant:', err);
          throw err;
        }
      },
      deleteClientTenant: async (id: string) => {
        try {
          return await api!.deleteClientTenant(id);
        } catch (err) {
          console.error('[WebApiShim] Error deleting client tenant:', err);
          throw err;
        }
      },
    },

    // Passkey / WebAuthn API
    passkeys: {
      list: async () => {
        try {
          return await api!.getPasskeys();
        } catch (err) {
          console.error('[WebApiShim] Error listing passkeys:', err);
          return [];
        }
      },
      beginRegistration: async () => {
        return await api!.beginPasskeyRegistration();
      },
      finishRegistration: async (data: { sessionId: string; response: unknown; name?: string }) => {
        return await api!.finishPasskeyRegistration(data);
      },
      beginAuthentication: async () => {
        return await api!.beginPasskeyAuthentication();
      },
      finishAuthentication: async (data: { sessionId: string; response: unknown }) => {
        return await api!.finishPasskeyAuthentication(data);
      },
      delete: async (id: string) => {
        return await api!.deletePasskey(id);
      },
      rename: async (id: string, name: string) => {
        return await api!.renamePasskey(id, name);
      },
    },

    // Installers API (optional)
    installers: {
      downloadAgent: async (platform: string) => {
        console.log('[WebApiShim] Agent download not available in web mode');
        return null;
      },
    },

    // Knowledge Base alias
    kb: {
      articles: {
        list: async (filters?: any) => [],
        get: async (id: string) => null,
        create: async (article: any) => null,
        update: async (id: string, article: any) => null,
        delete: async (id: string) => {},
        search: async (query: string) => [],
      },
      categories: {
        list: async () => [],
        get: async (id: string) => null,
        create: async (category: any) => null,
        update: async (id: string, category: any) => null,
        delete: async (id: string) => {},
      },
    },

    // Backend connection API with localStorage persistence
    backend: {
      connect: async (url: string) => {
        localStorage.setItem('backend_url', url);
        return { success: true };
      },
      getStatus: async () => {
        const url = localStorage.getItem('backend_url');
        const apiKey = localStorage.getItem('backend_api_key');
        return {
          connected: !!(url && apiKey),
          url: url || '',
        };
      },
      getConfig: async () => {
        const url = localStorage.getItem('backend_url') || window.location.origin;
        const apiKey = localStorage.getItem('backend_api_key') || '';
        return {
          url,
          apiKey,
          backendUrl: url,
          wsUrl: url.replace(/^http/, 'ws'),
          isConfigured: !!url,
          isAuthenticated: !!apiKey,
        };
      },
      setUrl: async (url: string) => {
        localStorage.setItem('backend_url', url);
        // Update the API service base URL
        if (api) {
          (api as any).baseUrl = url.endsWith('/api') ? url : `${url}/api`;
        }
        return { success: true };
      },
      setApiKey: async (apiKey: string) => {
        localStorage.setItem('backend_api_key', apiKey);
        return { success: true };
      },
      testConnection: async () => {
        try {
          const url = localStorage.getItem('backend_url');
          const apiKey = localStorage.getItem('backend_api_key');
          if (!url) return { success: false, error: 'No backend URL configured' };

          const response = await fetch(`${url}/health`, {
            headers: apiKey ? { 'X-API-Key': apiKey } : {},
          });

          if (response.ok) {
            return { success: true };
          }
          return { success: false, error: `Server returned ${response.status}` };
        } catch (err) {
          return { success: false, error: err instanceof Error ? err.message : 'Connection failed' };
        }
      },
      authenticate: async (email: string, password: string) => {
        try {
          const url = localStorage.getItem('backend_url');
          if (!url) return { success: false, error: 'No backend URL configured' };

          const response = await fetch(`${url}/api/auth/login`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ identifier: email, password }),
          });

          if (response.ok) {
            const data = await response.json();
            if (data.accessToken) {
              localStorage.setItem('backend_api_key', data.accessToken);
              localStorage.setItem('token', data.accessToken);
              return { success: true };
            }
          }
          const error = await response.json().catch(() => ({}));
          return { success: false, error: error.error || 'Authentication failed' };
        } catch (err) {
          return { success: false, error: err instanceof Error ? err.message : 'Connection failed' };
        }
      },
      disconnect: async () => {
        localStorage.removeItem('backend_url');
        localStorage.removeItem('backend_api_key');
        return { success: true };
      },
    },

    // Server API
    server: {
      getEnrollmentLink: async () => window.location.origin + '/enroll',
      getSettings: async () => ({}),
      getInfo: async () => ({ version: '1.70.0-web', port: 443, environment: 'production' }),
    },

    // Agent API - for web, trigger browser downloads from server
    agent: {
      download: async (platform: string) => {
        // Download pre-configured installer from API (includes server URL and enrollment token)
        const validPlatforms = ['windows', 'linux', 'macos'];
        if (!validPlatforms.includes(platform)) {
          return { success: false, error: 'Unknown platform' };
        }

        try {
          // Use the authenticated API endpoint that generates a pre-configured script
          const token = localStorage.getItem('token') || localStorage.getItem('backend_api_key');
          const csrfToken = localStorage.getItem('csrf_token');

          const headers: Record<string, string> = {
            Accept: 'application/octet-stream',
          };
          if (token) {
            headers['Authorization'] = `Bearer ${token}`;
          }
          if (csrfToken) {
            headers['X-CSRF-Token'] = csrfToken;
          }

          const response = await fetch(`/api/installer/${platform}`, { headers });
          if (!response.ok) {
            const error = await response.json().catch(() => ({ error: 'Download failed' }));
            throw new Error(error.error || `Download failed: ${response.status}`);
          }

          const blob = await response.blob();
          const filename = platform === 'windows' ? 'sentinel-install.ps1' : 'sentinel-install.sh';

          const url = URL.createObjectURL(blob);
          const link = document.createElement('a');
          link.href = url;
          link.download = filename;
          document.body.appendChild(link);
          link.click();
          document.body.removeChild(link);
          URL.revokeObjectURL(url);

          return {
            success: true,
            size: blob.size,
            note: platform === 'windows'
              ? 'Right-click the downloaded file and select "Run with PowerShell". UAC will prompt for administrator access.'
              : 'Run with: chmod +x sentinel-install.sh && sudo ./sentinel-install.sh',
          };
        } catch (err) {
          console.error('[WebApiShim] Agent download failed:', err);
          return { success: false, error: (err as Error).message };
        }
      },
      downloadConfigured: async (platform: string) => {
        // Same as download - all downloads are now pre-configured
        return (window as any).api.agent.download(platform);
      },
      downloadMsi: async () => ({ success: false, error: 'MSI download not available in web mode' }),
      getMsiCommand: async () => '',
      runPowerShellInstall: async () => ({ success: false, error: 'PowerShell install not available in web mode' }),
    },

    // Certs API (used by certificateStore)
    certs: {
      list: async () => {
        try {
          const result = await api!.getCertificateInfo();
          return {
            certificates: result?.certificates || [],
            certsDir: result?.certsDir || '',
            caCertHash: result?.caCertHash || null,
          };
        } catch (e) {
          console.error('[WebApiShim] Failed to get certificate info:', e);
          return { certificates: [], certsDir: '', caCertHash: null };
        }
      },
      getAgentStatus: async () => {
        try {
          const result = await api!.getDeviceCertStatuses();
          return result || [];
        } catch (e) {
          return [];
        }
      },
      renew: async () => ({ success: true }),
      distribute: async () => ({ success: true }),
      onDistributed: (callback: (result: any) => void) => registerHandler('certs:distributed', callback),
      onAgentConfirmed: (callback: (result: any) => void) => registerHandler('certs:agentConfirmed', callback),
    },

    // Enrollment tokens API
    enrollmentTokens: {
      list: async () => api!.getEnrollmentTokens(),
      create: async (data: any) => api!.createEnrollmentToken(data),
      delete: async (id: string) => api!.deleteEnrollmentToken(id),
      regenerate: async (id: string) => api!.regenerateEnrollmentToken(id),
    },

    // Dashboard API
    dashboard: {
      getStats: async () => api!.getDashboardStats(),
    },

    // Utility methods
    logError: (error: { message: string; stack?: string; componentStack?: string }) => {
      console.error('[WebApiShim] Error logged:', error);
    },

    getAppVersion: async () => '1.70.0-web',

    onDeviceUpdate: (callback: (device: any) => void) => {
      return registerHandler('devices:updated', callback);
    },

    onAlertUpdate: (callback: (alert: any) => void) => {
      return registerHandler('alerts:new', callback);
    },
  };

  console.log('[WebApiShim] window.api shim created with full API coverage');
}

export {};
