/**
 * Service Adapter - Unified interface for Electron and Web modes
 *
 * In Electron mode: Uses window.api (IPC to main process)
 * In Web mode: Uses HTTP API and WebSocket directly
 */
import { isElectron, isWeb } from './env';
import { api } from './api';
import { wsService } from './websocket';

// Re-export environment detection
export { isElectron, isWeb } from './env';

// Type definitions for the unified interface
export interface Device {
  id: string;
  hostname: string;
  displayName?: string;
  status: string;
  platform: string;
  agentId: string;
  clientId?: string;
  lastSeen?: string;
  createdAt: string;
  [key: string]: unknown;
}

export interface DeviceMetrics {
  cpuPercent: number;
  memoryPercent: number;
  memoryTotalBytes?: number;
  diskPercent: number;
  diskTotalBytes?: number;
  diskUsedBytes?: number;
  timestamp: string;
  [key: string]: unknown;
}

export interface Alert {
  id: string;
  deviceId: string;
  severity: string;
  message: string;
  status: string;
  createdAt: string;
  [key: string]: unknown;
}

export interface AlertRule {
  id: string;
  name: string;
  description?: string;
  enabled: boolean;
  conditions: unknown[];
  severity: string;
  [key: string]: unknown;
}

// Devices Service Adapter
export const devices = {
  async list(clientId?: string): Promise<Device[]> {
    if (isElectron) {
      return (window as any).api.devices.list(clientId);
    }
    const result = await api!.getDevices();
    return (result as any).devices || result || [];
  },

  async get(id: string): Promise<Device> {
    if (isElectron) {
      return (window as any).api.devices.get(id);
    }
    return api!.getDevice(id) as Promise<Device>;
  },

  async getMetrics(deviceId: string, hours: number = 24): Promise<DeviceMetrics[]> {
    if (isElectron) {
      return (window as any).api.devices.getMetrics(deviceId, hours);
    }
    const result = await api!.getDeviceMetrics(deviceId, { limit: hours * 60 });
    return (result as any).metrics || result || [];
  },

  async delete(id: string): Promise<void> {
    if (isElectron) {
      return (window as any).api.devices.delete(id);
    }
    return api!.deleteDevice(id);
  },

  async disable(id: string): Promise<void> {
    if (isElectron) {
      return (window as any).api.devices.disable(id);
    }
    // Web mode - update device status
    return api!.updateDevice(id, { tags: ['disabled'] }) as unknown as void;
  },

  async enable(id: string): Promise<void> {
    if (isElectron) {
      return (window as any).api.devices.enable(id);
    }
    // Web mode - update device status
    return api!.updateDevice(id, { tags: [] }) as unknown as void;
  },

  async uninstall(id: string): Promise<void> {
    if (isElectron) {
      return (window as any).api.devices.uninstall(id);
    }
    return api!.uninstallAgent(id) as unknown as void;
  },

  async ping(id: string): Promise<{ success: boolean; latency?: number }> {
    if (isElectron) {
      return (window as any).api.devices.ping(id);
    }
    return api!.pingAgent(id) as Promise<{ success: boolean; latency?: number }>;
  },
};

// Alerts Service Adapter
export const alerts = {
  async list(): Promise<Alert[]> {
    if (isElectron) {
      return (window as any).api.alerts.list();
    }
    const result = await api!.getAlerts();
    return (result as any).alerts || result || [];
  },

  async acknowledge(id: string): Promise<void> {
    if (isElectron) {
      return (window as any).api.alerts.acknowledge(id);
    }
    return api!.acknowledgeAlert(id) as unknown as void;
  },

  async resolve(id: string): Promise<void> {
    if (isElectron) {
      return (window as any).api.alerts.resolve(id);
    }
    return api!.resolveAlert(id) as unknown as void;
  },

  async getRules(): Promise<AlertRule[]> {
    if (isElectron) {
      return (window as any).api.alerts.getRules();
    }
    return api!.getAlertRules() as Promise<AlertRule[]>;
  },

  async createRule(rule: Omit<AlertRule, 'id'>): Promise<AlertRule> {
    if (isElectron) {
      return (window as any).api.alerts.createRule(rule);
    }
    return api!.createAlertRule(rule as any) as Promise<AlertRule>;
  },

  async updateRule(id: string, updates: Partial<AlertRule>): Promise<AlertRule> {
    if (isElectron) {
      return (window as any).api.alerts.updateRule(id, updates);
    }
    return api!.updateAlertRule(id, updates as any) as Promise<AlertRule>;
  },

  async deleteRule(id: string): Promise<void> {
    if (isElectron) {
      return (window as any).api.alerts.deleteRule(id);
    }
    return api!.deleteAlertRule(id) as unknown as void;
  },

  onNew(handler: (alert: Alert) => void): () => void {
    if (isElectron) {
      return (window as any).api.alerts.onNew(handler);
    }
    // Web mode - subscribe to WebSocket
    return wsService!.on('alert', handler);
  },
};

// Terminal Service Adapter
export const terminal = {
  async start(deviceId: string): Promise<{ sessionId: string }> {
    if (isElectron) {
      return (window as any).api.terminal.start(deviceId);
    }
    // Web mode - send WebSocket message
    const sessionId = `term-${deviceId}-${Date.now()}`;
    wsService!.send('start_terminal', {
      deviceId,
      sessionId,
      cols: 80,
      rows: 24,
    });
    return { sessionId };
  },

  async send(sessionId: string, data: string): Promise<void> {
    if (isElectron) {
      return (window as any).api.terminal.send(sessionId, data);
    }
    wsService!.send('terminal_input', {
      sessionId,
      data,
    });
  },

  async close(sessionId: string): Promise<void> {
    if (isElectron) {
      return (window as any).api.terminal.close(sessionId);
    }
    wsService!.send('close_terminal', { sessionId });
  },

  async resize(sessionId: string, cols: number, rows: number): Promise<void> {
    if (isElectron) {
      return (window as any).api.terminal.resize?.(sessionId, cols, rows);
    }
    wsService!.send('terminal_resize', { sessionId, cols, rows });
  },

  onData(handler: (data: string) => void): () => void {
    if (isElectron) {
      return (window as any).api.terminal.onData(handler);
    }
    return wsService!.on('terminal_output', (payload: any) => {
      handler(payload.data);
    });
  },

  onError(handler: (error: { sessionId?: string; error: string }) => void): () => void {
    if (isElectron) {
      // Electron might not have this
      return () => {};
    }
    return wsService!.on('error', handler);
  },
};

// File Browser Service Adapter
export const files = {
  async list(deviceId: string, path: string): Promise<unknown[]> {
    if (isElectron) {
      return (window as any).api.files?.list?.(deviceId, path) || [];
    }
    // Web mode - send WebSocket message and wait for response
    return new Promise((resolve, reject) => {
      const requestId = `files-${Date.now()}`;
      const timeout = setTimeout(() => {
        unsubResponse();
        unsubError();
        reject(new Error('Request timed out'));
      }, 30000);

      const unsubResponse = wsService!.on('response', (data: any) => {
        if (data.requestId === requestId) {
          clearTimeout(timeout);
          unsubResponse();
          unsubError();
          if (data.success) {
            resolve(data.data?.files || []);
          } else {
            reject(new Error(data.error || 'Failed to list files'));
          }
        }
      });

      const unsubError = wsService!.on('error', (data: any) => {
        if (data.requestId === requestId) {
          clearTimeout(timeout);
          unsubResponse();
          unsubError();
          reject(new Error(data.error || 'Failed to list files'));
        }
      });

      wsService!.send('list_files', {
        deviceId,
        path,
        requestId,
      });
    });
  },

  async download(deviceId: string, path: string): Promise<void> {
    if (isElectron) {
      return (window as any).api.files?.download?.(deviceId, path);
    }
    wsService!.send('download_file', { deviceId, path });
  },
};

// Remote Desktop Service Adapter
export const remote = {
  async start(deviceId: string): Promise<{ sessionId: string }> {
    if (isElectron) {
      return (window as any).api.remote?.start?.(deviceId) || { sessionId: '' };
    }
    const sessionId = `remote-${deviceId}-${Date.now()}`;
    wsService!.send('start_remote', { deviceId, sessionId });
    return { sessionId };
  },

  async stop(sessionId: string): Promise<void> {
    if (isElectron) {
      return (window as any).api.remote?.stop?.(sessionId);
    }
    wsService!.send('stop_remote', { sessionId });
  },

  async sendInput(sessionId: string, inputType: string, data: unknown): Promise<void> {
    if (isElectron) {
      return (window as any).api.remote?.sendInput?.(sessionId, inputType, data);
    }
    wsService!.send('remote_input', { sessionId, inputType, data });
  },

  onFrame(handler: (frame: { sessionId: string; data: string; width: number; height: number }) => void): () => void {
    if (isElectron) {
      return (window as any).api.remote?.onFrame?.(handler) || (() => {});
    }
    return wsService!.on('remote_frame', handler);
  },

  onError(handler: (error: { sessionId?: string; error: string }) => void): () => void {
    if (isElectron) {
      return () => {};
    }
    return wsService!.on('error', handler);
  },
};

// Auth Service Adapter (Web-only features, Electron handles auth differently)
export const auth = {
  async login(identifier: string, password: string): Promise<{ accessToken: string; user: unknown }> {
    if (isElectron) {
      // Electron doesn't use this - auth is handled by the main process
      throw new Error('Login not available in Electron mode');
    }
    const result = await api!.login(identifier, password);
    if (result.accessToken) {
      localStorage.setItem('token', result.accessToken);
      localStorage.setItem('user', JSON.stringify(result.user));
    }
    return result;
  },

  async logout(): Promise<void> {
    if (isElectron) {
      return;
    }
    await api!.logout();
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    wsService!.disconnect();
  },

  async getCurrentUser(): Promise<unknown> {
    if (isElectron) {
      return null;
    }
    return api!.getCurrentUser();
  },

  async validateInvitation(token: string): Promise<unknown> {
    if (isElectron) {
      return null;
    }
    return api!.validateInvitation(token);
  },

  async register(data: {
    token: string;
    username: string;
    email: string;
    password: string;
    firstName?: string;
    lastName?: string;
  }): Promise<unknown> {
    if (isElectron) {
      throw new Error('Registration not available in Electron mode');
    }
    return api!.register(data);
  },

  isAuthenticated(): boolean {
    if (isElectron) {
      return true; // Electron handles its own auth
    }
    return !!localStorage.getItem('token');
  },

  getToken(): string | null {
    return localStorage.getItem('token');
  },

  getUser(): unknown {
    const user = localStorage.getItem('user');
    return user ? JSON.parse(user) : null;
  },
};

// Event subscription for real-time updates
export const events = {
  on(event: string, handler: (data: unknown) => void): () => void {
    if (isElectron) {
      return (window as any).api.on(event, handler);
    }
    return wsService!.on(event, handler);
  },

  off(event: string, handler: (data: unknown) => void): void {
    if (isElectron) {
      (window as any).api.off?.(event, handler);
    } else {
      wsService!.off(event, handler);
    }
  },
};

// WebSocket connection management for web mode
export const connection = {
  connect(): void {
    if (isWeb && wsService) {
      wsService.connect();
    }
  },

  disconnect(): void {
    if (isWeb && wsService) {
      wsService.disconnect();
    }
  },

  get isConnected(): boolean {
    if (isElectron) {
      return true; // Main process handles connection
    }
    return wsService?.isConnected || false;
  },
};

// Dashboard stats (web mode only for now)
export const dashboard = {
  async getStats(): Promise<unknown> {
    if (isElectron) {
      // Electron might have its own implementation
      return (window as any).api.dashboard?.getStats?.() || {};
    }
    return api!.getDashboardStats();
  },
};

// Scripts service
export const scripts = {
  async list(): Promise<unknown[]> {
    if (isElectron) {
      return (window as any).api.scripts?.list?.() || [];
    }
    const result = await api!.getScripts();
    return (result as any).scripts || result || [];
  },

  async create(data: {
    name: string;
    description?: string;
    language: string;
    content: string;
    osTypes: string[];
  }): Promise<unknown> {
    if (isElectron) {
      return (window as any).api.scripts?.create?.(data);
    }
    return api!.createScript(data);
  },

  async update(id: string, data: Partial<{
    name: string;
    description: string;
    content: string;
    osTypes: string[];
  }>): Promise<unknown> {
    if (isElectron) {
      return (window as any).api.scripts?.update?.(id, data);
    }
    return api!.updateScript(id, data);
  },

  async delete(id: string): Promise<void> {
    if (isElectron) {
      return (window as any).api.scripts?.delete?.(id);
    }
    return api!.deleteScript(id) as unknown as void;
  },

  async run(scriptId: string, deviceIds: string[]): Promise<unknown> {
    if (isElectron) {
      return (window as any).api.scripts?.run?.(scriptId, deviceIds);
    }
    return api!.runScript(scriptId, deviceIds);
  },
};

// Users management (web mode, admin features)
export const users = {
  async list(): Promise<unknown[]> {
    if (isElectron) {
      return [];
    }
    const result = await api!.getUsers();
    return (result as any).users || result || [];
  },

  async create(data: {
    email: string;
    password: string;
    firstName: string;
    lastName: string;
    role: string;
  }): Promise<unknown> {
    if (isElectron) {
      throw new Error('User management not available in Electron mode');
    }
    return api!.createUser(data);
  },

  async update(id: string, data: Partial<{
    firstName: string;
    lastName: string;
    role: string;
    password: string;
  }>): Promise<unknown> {
    if (isElectron) {
      throw new Error('User management not available in Electron mode');
    }
    return api!.updateUser(id, data);
  },

  async delete(id: string): Promise<void> {
    if (isElectron) {
      throw new Error('User management not available in Electron mode');
    }
    return api!.deleteUser(id) as unknown as void;
  },
};

// Invitations management
export const invitations = {
  async list(): Promise<unknown[]> {
    if (isElectron) {
      return [];
    }
    return api!.getInvitations();
  },

  async create(data: { email?: string; role: string }): Promise<unknown> {
    if (isElectron) {
      throw new Error('Invitation management not available in Electron mode');
    }
    return api!.createInvitation(data);
  },

  async delete(id: string): Promise<void> {
    if (isElectron) {
      throw new Error('Invitation management not available in Electron mode');
    }
    return api!.deleteInvitation(id) as unknown as void;
  },
};

// Enrollment tokens
export const enrollmentTokens = {
  async list(): Promise<unknown[]> {
    if (isElectron) {
      return (window as any).api.enrollmentTokens?.list?.() || [];
    }
    return api!.getEnrollmentTokens();
  },

  async create(data: {
    name: string;
    description?: string;
    expiresAt?: string;
    maxUses?: number;
    tags?: string[];
  }): Promise<unknown> {
    if (isElectron) {
      return (window as any).api.enrollmentTokens?.create?.(data);
    }
    return api!.createEnrollmentToken(data);
  },

  async delete(id: string): Promise<void> {
    if (isElectron) {
      return (window as any).api.enrollmentTokens?.delete?.(id);
    }
    return api!.deleteEnrollmentToken(id) as unknown as void;
  },

  async regenerate(id: string): Promise<unknown> {
    if (isElectron) {
      return (window as any).api.enrollmentTokens?.regenerate?.(id);
    }
    return api!.regenerateEnrollmentToken(id);
  },
};
