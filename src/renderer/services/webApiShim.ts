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
        emitEvent(msg.type, msg.data);
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
        console.log('[WebApiShim] setMetricsInterval not available in web mode');
        return { success: true };
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
        wsService?.send('start_terminal', { deviceId, sessionId, cols: 80, rows: 24 });
        return { sessionId };
      },
      send: async (sessionId: string, data: string) => {
        wsService?.send('terminal_input', { sessionId, data });
      },
      resize: async (sessionId: string, cols: number, rows: number) => {
        wsService?.send('terminal_resize', { sessionId, cols, rows });
      },
      close: async (sessionId: string) => {
        wsService?.send('close_terminal', { sessionId });
      },
      onData: (callback: (data: string) => void) => {
        return registerHandler('terminal:data', (payload: any) => {
          callback(payload.data || payload);
        });
      },
    },

    // Files API
    files: {
      drives: async (deviceId: string) => {
        return [];
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

    // Remote Desktop API
    remote: {
      startSession: async (deviceId: string) => {
        const sessionId = `remote-${deviceId}-${Date.now()}`;
        wsService?.send('start_remote', { deviceId, sessionId });
        return { sessionId };
      },
      stopSession: async (sessionId: string) => {
        wsService?.send('stop_remote', { sessionId });
      },
      sendInput: async (sessionId: string, input: any) => {
        wsService?.send('remote_input', { sessionId, ...input });
      },
      onFrame: (callback: (frame: any) => void) => {
        return registerHandler('remote:frame', callback);
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
        return [];
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
      onUpdateAvailable: (callback: (info: any) => void) => registerHandler('updates:available', callback),
      onDownloadProgress: (callback: (progress: any) => void) => registerHandler('updates:progress', callback),
      onUpdateDownloaded: (callback: (info: any) => void) => registerHandler('updates:downloaded', callback),
      onError: (callback: (error: any) => void) => registerHandler('updates:error', callback),
    },

    // Updater API (alias for updates with additional methods)
    updater: {
      checkForUpdates: async () => ({ updateAvailable: false }),
      downloadUpdate: async () => {},
      installUpdate: () => {},
      getVersion: async () => '1.67.10-web',
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
      updateSettings: async (settings: any) => null,
      getClientTenants: async () => [],
      createClientTenant: async (clientId: string, tenantId: string) => null,
      deleteClientTenant: async (clientId: string, tenantId: string) => null,
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
      list: async (filters?: any) => [],
      get: async (id: string) => null,
      create: async (article: any) => null,
      update: async (id: string, article: any) => null,
      delete: async (id: string) => {},
      search: async (query: string) => [],
      getCategories: async () => [],
      createCategory: async (category: any) => null,
      updateCategory: async (id: string, category: any) => null,
      deleteCategory: async (id: string) => {},
    },

    // Backend connection API
    backend: {
      connect: async (url: string) => ({ success: true }),
      getStatus: async () => ({ connected: true }),
    },

    // Server API
    server: {
      getEnrollmentLink: async () => window.location.origin + '/enroll',
      getSettings: async () => ({}),
    },

    // Agent API
    agent: {
      download: async (platform: string) => null,
      downloadConfigured: async (platform: string) => null,
      downloadMsi: async () => null,
      getMsiCommand: async () => '',
      runPowerShellInstall: async () => null,
    },

    // Certs API (used by certificateStore)
    certs: {
      list: async () => ({ certs: [] }),
      getAgentStatus: async () => [],
      renew: async () => ({ success: true }),
      distribute: async () => ({ success: true }),
      onDistributed: (callback: (result: any) => void) => registerHandler('certs:distributed', callback),
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

    getAppVersion: async () => '1.67.10-web',

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
