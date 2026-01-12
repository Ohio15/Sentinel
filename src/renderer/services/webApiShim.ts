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

  // Helper to emit events from WebSocket
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

  // Create the shim
  (window as any).api = {
    // Event subscription
    on: (event: string, handler: (data: any) => void) => {
      if (!eventHandlers.has(event)) {
        eventHandlers.set(event, new Set());
      }
      eventHandlers.get(event)!.add(handler);

      // Return unsubscribe function
      return () => {
        eventHandlers.get(event)?.delete(handler);
      };
    },

    off: (event: string, handler: (data: any) => void) => {
      eventHandlers.get(event)?.delete(handler);
    },

    // Devices
    devices: {
      list: async (clientId?: string) => {
        const result = await api!.getDevices();
        return (result as any).devices || result || [];
      },
      get: async (id: string) => {
        return api!.getDevice(id);
      },
      getMetrics: async (deviceId: string, hours: number = 24) => {
        const result = await api!.getDeviceMetrics(deviceId, { limit: hours * 60 });
        return (result as any).metrics || result || [];
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
      ping: async (id: string) => {
        return api!.pingAgent(id);
      },
    },

    // Alerts
    alerts: {
      list: async () => {
        const result = await api!.getAlerts();
        return (result as any).alerts || result || [];
      },
      acknowledge: async (id: string) => {
        return api!.acknowledgeAlert(id);
      },
      resolve: async (id: string) => {
        return api!.resolveAlert(id);
      },
      getRules: async () => {
        return api!.getAlertRules();
      },
      createRule: async (rule: any) => {
        return api!.createAlertRule(rule);
      },
      updateRule: async (id: string, updates: any) => {
        return api!.updateAlertRule(id, updates);
      },
      deleteRule: async (id: string) => {
        return api!.deleteAlertRule(id);
      },
      onNew: (handler: (alert: any) => void) => {
        return (window as any).api.on('alert', handler);
      },
    },

    // Scripts
    scripts: {
      list: async () => {
        const result = await api!.getScripts();
        return (result as any).scripts || result || [];
      },
      create: async (data: any) => {
        return api!.createScript(data);
      },
      update: async (id: string, data: any) => {
        return api!.updateScript(id, data);
      },
      delete: async (id: string) => {
        return api!.deleteScript(id);
      },
      run: async (scriptId: string, deviceIds: string[]) => {
        return api!.runScript(scriptId, deviceIds);
      },
    },

    // Tickets (stub - may not be available in web API)
    tickets: {
      list: async () => [],
      get: async () => null,
      create: async () => null,
      update: async () => null,
      delete: async () => {},
      getComments: async () => [],
      addComment: async () => null,
      getActivity: async () => [],
      getStats: async () => ({}),
      getTemplates: async () => [],
    },

    // Certificates (stub - may not be available in web API)
    certs: {
      list: async () => ({ certs: [] }),
      getAgentStatus: async () => [],
      renew: async () => ({ success: true }),
      distribute: async () => ({ success: true }),
      onDistributed: () => () => {},
    },

    // Clients
    clients: {
      list: async () => {
        const result = await api!.getClients();
        return (result as any).clients || result || [];
      },
      create: async (data: any) => {
        return api!.createClient(data);
      },
      update: async (id: string, data: any) => {
        return api!.updateClient(id, data);
      },
      delete: async (id: string) => {
        return api!.deleteClient(id);
      },
    },

    // Enrollment tokens
    enrollmentTokens: {
      list: async () => {
        return api!.getEnrollmentTokens();
      },
      create: async (data: any) => {
        return api!.createEnrollmentToken(data);
      },
      delete: async (id: string) => {
        return api!.deleteEnrollmentToken(id);
      },
      regenerate: async (id: string) => {
        return api!.regenerateEnrollmentToken(id);
      },
    },

    // Terminal
    terminal: {
      start: async (deviceId: string) => {
        const sessionId = `term-${deviceId}-${Date.now()}`;
        wsService?.send('start_terminal', { deviceId, sessionId, cols: 80, rows: 24 });
        return { sessionId };
      },
      send: async (sessionId: string, data: string) => {
        wsService?.send('terminal_input', { sessionId, data });
      },
      close: async (sessionId: string) => {
        wsService?.send('close_terminal', { sessionId });
      },
      resize: async (sessionId: string, cols: number, rows: number) => {
        wsService?.send('terminal_resize', { sessionId, cols, rows });
      },
      onData: (handler: (data: string) => void) => {
        return (window as any).api.on('terminal_output', (payload: any) => {
          handler(payload.data);
        });
      },
    },

    // Files
    files: {
      list: async (deviceId: string, path: string) => {
        return new Promise((resolve, reject) => {
          const requestId = `files-${Date.now()}`;
          const timeout = setTimeout(() => reject(new Error('Timeout')), 30000);

          const unsub = (window as any).api.on('response', (data: any) => {
            if (data.requestId === requestId) {
              clearTimeout(timeout);
              unsub();
              if (data.success) {
                resolve(data.data?.files || []);
              } else {
                reject(new Error(data.error));
              }
            }
          });

          wsService?.send('list_files', { deviceId, path, requestId });
        });
      },
      download: async (deviceId: string, path: string) => {
        wsService?.send('download_file', { deviceId, path });
      },
    },

    // Remote desktop
    remote: {
      start: async (deviceId: string) => {
        const sessionId = `remote-${deviceId}-${Date.now()}`;
        wsService?.send('start_remote', { deviceId, sessionId });
        return { sessionId };
      },
      stop: async (sessionId: string) => {
        wsService?.send('stop_remote', { sessionId });
      },
      sendInput: async (sessionId: string, inputType: string, data: any) => {
        wsService?.send('remote_input', { sessionId, inputType, data });
      },
      onFrame: (handler: (frame: any) => void) => {
        return (window as any).api.on('remote_frame', handler);
      },
    },

    // Dashboard
    dashboard: {
      getStats: async () => {
        return api!.getDashboardStats();
      },
    },

    // Updates (stub for web)
    updates: {
      check: async () => ({ updateAvailable: false }),
      download: async () => {},
      install: async () => {},
    },

    // Updater (stub for web - auto-updates not available in web mode)
    updater: {
      getVersion: async () => '1.67.10-web',
      checkForUpdates: async () => ({ updateAvailable: false }),
      downloadUpdate: async () => {},
      installUpdate: () => {},
      onUpdateAvailable: () => () => {},
      onUpdateNotAvailable: () => () => {},
      onDownloadProgress: () => () => {},
      onUpdateDownloaded: () => () => {},
      onError: () => () => {},
    },

    // Settings (stub for web)
    settings: {
      get: async () => ({}),
      set: async () => {},
    },

    // Logs (stub for web)
    logError: (error: any) => {
      console.error('[WebApiShim] Error logged:', error);
    },
  };

  console.log('[WebApiShim] window.api shim created');
}

export {};
