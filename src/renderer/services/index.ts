/**
 * Service Adapter - Web-only interface
 * Uses HTTP API and WebSocket for all operations
 */
import { api } from './api';
import { wsService } from './websocket';

// Re-export environment detection (for backwards compatibility during migration)
export const isElectron = false;
export const isWeb = true;

// Type definitions
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

// Devices Service
export const devices = {
  async list(clientId?: string): Promise<Device[]> {
    const result = await api!.getDevices();
    return (result as any).devices || result || [];
  },

  async get(id: string): Promise<Device> {
    return api!.getDevice(id) as Promise<Device>;
  },

  async getMetrics(deviceId: string, hours: number = 24): Promise<DeviceMetrics[]> {
    const result = await api!.getDeviceMetrics(deviceId, { limit: hours * 60 });
    return (result as any).metrics || result || [];
  },

  async delete(id: string): Promise<void> {
    await api!.deleteDevice(id);
  },

  async disable(id: string): Promise<void> {
    await api!.updateDevice(id, { tags: ['disabled'] });
  },

  async enable(id: string): Promise<void> {
    await api!.updateDevice(id, { tags: [] });
  },

  async uninstall(id: string): Promise<void> {
    await api!.uninstallAgent(id);
  },

  async ping(id: string): Promise<{ success: boolean; latency?: number }> {
    return api!.pingAgent(id) as Promise<{ success: boolean; latency?: number }>;
  },

  async forceUpdate(id: string): Promise<void> {
    await api!.forceUpdate(id);
  },

  async update(id: string, data: { displayName?: string; tags?: string[]; clientId?: string | null }): Promise<Device> {
    return api!.updateDevice(id, data) as Promise<Device>;
  },

  async powerAction(id: string, action: 'shutdown' | 'restart' | 'wake'): Promise<void> {
    await api!.devicePowerAction(id, action);
  },
};

// Alerts Service
export const alerts = {
  async list(): Promise<Alert[]> {
    const result = await api!.getAlerts();
    return (result as any).alerts || result || [];
  },

  async acknowledge(id: string): Promise<void> {
    await api!.acknowledgeAlert(id);
  },

  async resolve(id: string): Promise<void> {
    await api!.resolveAlert(id);
  },

  async getRules(): Promise<AlertRule[]> {
    return api!.getAlertRules() as Promise<AlertRule[]>;
  },

  async createRule(rule: Omit<AlertRule, 'id'>): Promise<AlertRule> {
    return api!.createAlertRule(rule as any) as Promise<AlertRule>;
  },

  async updateRule(id: string, updates: Partial<AlertRule>): Promise<AlertRule> {
    return api!.updateAlertRule(id, updates as any) as Promise<AlertRule>;
  },

  async deleteRule(id: string): Promise<void> {
    await api!.deleteAlertRule(id);
  },

  onNew(handler: (alert: Alert) => void): () => void {
    return wsService!.on('alert', (data) => handler(data as Alert));
  },
};

// Terminal Service
export const terminal = {
  async start(deviceId: string): Promise<{ sessionId: string }> {
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
    wsService!.send('terminal_input', {
      sessionId,
      data,
    });
  },

  async close(sessionId: string): Promise<void> {
    wsService!.send('close_terminal', { sessionId });
  },

  async resize(sessionId: string, cols: number, rows: number): Promise<void> {
    wsService!.send('terminal_resize', { sessionId, cols, rows });
  },

  onData(handler: (data: string) => void): () => void {
    return wsService!.on('terminal_output', (payload: any) => {
      handler(payload.data);
    });
  },

  onError(handler: (error: { sessionId?: string; error: string }) => void): () => void {
    return wsService!.on('error', (data) => handler(data as { sessionId?: string; error: string }));
  },
};

// File Browser Service
export const files = {
  async list(deviceId: string, path: string): Promise<unknown[]> {
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
    wsService!.send('download_file', { deviceId, path });
  },
};

// Auth Service
export const auth = {
  async login(identifier: string, password: string): Promise<{ accessToken: string; user: unknown }> {
    const result = await api!.login(identifier, password);
    if (result.accessToken) {
      localStorage.setItem('token', result.accessToken);
      localStorage.setItem('user', JSON.stringify(result.user));
    }
    return result;
  },

  async logout(): Promise<void> {
    await api!.logout();
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    wsService!.disconnect();
  },

  async getCurrentUser(): Promise<unknown> {
    return api!.getCurrentUser();
  },

  async validateInvitation(token: string): Promise<unknown> {
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
    return api!.register(data);
  },

  isAuthenticated(): boolean {
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
    return wsService!.on(event, handler);
  },

  off(event: string, handler: (data: unknown) => void): void {
    wsService!.off(event, handler);
  },
};

// WebSocket connection management
export const connection = {
  connect(): void {
    if (wsService) {
      wsService.connect();
    }
  },

  disconnect(): void {
    if (wsService) {
      wsService.disconnect();
    }
  },

  get isConnected(): boolean {
    return wsService?.isConnected || false;
  },
};

// Dashboard stats
export const dashboard = {
  async getStats(): Promise<unknown> {
    return api!.getDashboardStats();
  },
};

// Scripts service
export const scripts = {
  async list(): Promise<unknown[]> {
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
    return api!.createScript(data);
  },

  async update(id: string, data: Partial<{
    name: string;
    description: string;
    content: string;
    osTypes: string[];
  }>): Promise<unknown> {
    return api!.updateScript(id, data);
  },

  async delete(id: string): Promise<void> {
    await api!.deleteScript(id);
  },

  async run(scriptId: string, deviceIds: string[]): Promise<unknown> {
    return api!.runScript(scriptId, deviceIds);
  },
};

// Users management
export const users = {
  async list(): Promise<unknown[]> {
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
    return api!.createUser(data);
  },

  async update(id: string, data: Partial<{
    firstName: string;
    lastName: string;
    role: string;
    password: string;
  }>): Promise<unknown> {
    return api!.updateUser(id, data);
  },

  async delete(id: string): Promise<void> {
    await api!.deleteUser(id);
  },
};

// Invitations management
export const invitations = {
  async list(): Promise<unknown[]> {
    return api!.getInvitations();
  },

  async create(data: { email?: string; role: string }): Promise<unknown> {
    return api!.createInvitation(data);
  },

  async delete(id: string): Promise<void> {
    await api!.deleteInvitation(id);
  },
};

// Enrollment tokens
export const enrollmentTokens = {
  async list(): Promise<unknown[]> {
    return api!.getEnrollmentTokens();
  },

  async create(data: {
    name: string;
    description?: string;
    expiresAt?: string;
    maxUses?: number;
    tags?: string[];
  }): Promise<unknown> {
    return api!.createEnrollmentToken(data);
  },

  async delete(id: string): Promise<void> {
    await api!.deleteEnrollmentToken(id);
  },

  async regenerate(id: string): Promise<unknown> {
    return api!.regenerateEnrollmentToken(id);
  },
};

// Tickets service
export interface Ticket {
  id: string;
  ticketNumber: number;
  subject: string;
  description?: string;
  status: string;
  priority: string;
  type: string;
  deviceId?: string;
  createdAt: string;
  updatedAt: string;
  [key: string]: unknown;
}

export interface TicketComment {
  id: string;
  ticketId: string;
  content: string;
  isInternal: boolean;
  authorName: string;
  createdAt: string;
  [key: string]: unknown;
}

export const tickets = {
  async list(filters?: Record<string, unknown>): Promise<Ticket[]> {
    const result = await api!.makeRequest<{ tickets?: Ticket[] } | Ticket[]>('GET', '/tickets', undefined, filters as Record<string, string>);
    return (result as any).tickets || result || [];
  },

  async get(id: string): Promise<Ticket> {
    return api!.makeRequest<Ticket>('GET', `/tickets/${id}`);
  },

  async create(ticket: Partial<Ticket>): Promise<Ticket> {
    return api!.makeRequest<Ticket>('POST', '/tickets', ticket);
  },

  async update(id: string, updates: Partial<Ticket>): Promise<Ticket> {
    return api!.makeRequest<Ticket>('PUT', `/tickets/${id}`, updates);
  },

  async delete(id: string): Promise<void> {
    await api!.makeRequest<void>('DELETE', `/tickets/${id}`);
  },

  async getComments(ticketId: string): Promise<TicketComment[]> {
    const result = await api!.makeRequest<{ comments?: TicketComment[] } | TicketComment[]>('GET', `/tickets/${ticketId}/comments`);
    return (result as any).comments || result || [];
  },

  async addComment(comment: Omit<TicketComment, 'id' | 'createdAt'>): Promise<TicketComment> {
    return api!.makeRequest<TicketComment>('POST', `/tickets/${comment.ticketId}/comments`, comment);
  },

  async getActivity(ticketId: string): Promise<unknown[]> {
    const result = await api!.makeRequest<{ activity?: unknown[] } | unknown[]>('GET', `/tickets/${ticketId}/activity`);
    return (result as any).activity || result || [];
  },

  async getStats(): Promise<unknown> {
    return api!.makeRequest<unknown>('GET', '/tickets/stats');
  },

  async getTemplates(): Promise<unknown[]> {
    const result = await api!.makeRequest<{ templates?: unknown[] } | unknown[]>('GET', '/tickets/templates');
    return (result as any).templates || result || [];
  },
};

// Clients service
export interface Client {
  id: string;
  name: string;
  description?: string;
  color?: string;
  deviceCount?: number;
  createdAt: string;
  updatedAt: string;
  [key: string]: unknown;
}

export const clients = {
  async list(): Promise<Client[]> {
    const result = await api!.getClients();
    return result || [];
  },

  async get(id: string): Promise<Client> {
    return api!.getClient(id) as Promise<Client>;
  },

  async create(client: Omit<Client, 'id' | 'createdAt' | 'updatedAt' | 'deviceCount'>): Promise<Client> {
    return api!.createClient(client as any) as Promise<Client>;
  },

  async update(id: string, updates: Partial<Client>): Promise<Client> {
    return api!.updateClient(id, updates as any) as Promise<Client>;
  },

  async delete(id: string): Promise<void> {
    await api!.deleteClient(id);
  },
};

// Certificates service
export interface CertificateInfo {
  name: string;
  type: string;
  path: string;
  exists: boolean;
  subject?: string;
  issuer?: string;
  validFrom?: string;
  validTo?: string;
  fingerprint?: string;
  serialNumber?: string;
  daysUntilExpiry?: number;
  status: string;
}

export const certs = {
  async list(): Promise<{ certificates: CertificateInfo[]; certsDir?: string; caCertHash?: string }> {
    return api!.getCertificateInfo();
  },

  async getAgentStatus(): Promise<unknown[]> {
    const result = await api!.getDeviceCertStatuses();
    return (result as any).statuses || result || [];
  },

  async renew(): Promise<{ success: boolean; error?: string }> {
    // Certificate renewal is server-side managed
    return { success: false, error: 'Certificate renewal not available in web mode' };
  },

  async distribute(): Promise<{ success: number; failed: number }> {
    // Certificate distribution is server-side managed
    return { success: 0, failed: 0 };
  },

  onDistributed(handler: (result: unknown) => void): () => void {
    return wsService!.on('certs:distributed', handler);
  },

  onAgentConfirmed(handler: (data: unknown) => void): () => void {
    return wsService!.on('certs:agentConfirmed', handler);
  },
};

// Passkeys service (WebAuthn)
export const passkeys = {
  async list(): Promise<Array<{ id: string; name: string; createdAt: string; lastUsedAt?: string }>> {
    return api!.getPasskeys();
  },

  async beginRegistration(): Promise<{ sessionId: string; options: unknown }> {
    return api!.beginPasskeyRegistration();
  },

  async finishRegistration(data: { sessionId: string; response: unknown; name?: string }): Promise<void> {
    await api!.finishPasskeyRegistration(data);
  },

  async delete(id: string): Promise<void> {
    await api!.deletePasskey(id);
  },

  async rename(id: string, name: string): Promise<void> {
    await api!.renamePasskey(id, name);
  },
};

// Knowledge Base service
export const kb = {
  categories: {
    async list(): Promise<unknown[]> {
      const result = await api!.makeRequest<{ categories?: unknown[] } | unknown[]>('GET', '/kb/categories');
      return (result as any).categories || result || [];
    },

    async create(data: { name: string; description?: string; color?: string }): Promise<unknown> {
      return api!.makeRequest('POST', '/kb/categories', data);
    },

    async update(id: string, data: Partial<{ name: string; description: string; color: string }>): Promise<unknown> {
      return api!.makeRequest('PUT', `/kb/categories/${id}`, data);
    },

    async delete(id: string): Promise<void> {
      await api!.makeRequest('DELETE', `/kb/categories/${id}`);
    },
  },

  articles: {
    async list(): Promise<unknown[]> {
      const result = await api!.makeRequest<{ articles?: unknown[] } | unknown[]>('GET', '/kb/articles');
      return (result as any).articles || result || [];
    },

    async get(id: string): Promise<unknown> {
      return api!.makeRequest('GET', `/kb/articles/${id}`);
    },

    async create(data: {
      title: string;
      content: string;
      categoryId?: string;
      tags?: string[];
      isFeatured?: boolean;
      isPinned?: boolean;
    }): Promise<unknown> {
      return api!.makeRequest('POST', '/kb/articles', data);
    },

    async update(id: string, data: Partial<{
      title: string;
      content: string;
      categoryId: string;
      tags: string[];
      isFeatured: boolean;
      isPinned: boolean;
      isPublished: boolean;
    }>): Promise<unknown> {
      return api!.makeRequest('PUT', `/kb/articles/${id}`, data);
    },

    async delete(id: string): Promise<void> {
      await api!.makeRequest('DELETE', `/kb/articles/${id}`);
    },
  },
};

// Analytics service
export const analytics = {
  async tickets(params?: { startDate?: string; endDate?: string; clientId?: string }): Promise<unknown> {
    const stringParams: Record<string, string> = {};
    if (params?.startDate) stringParams.startDate = params.startDate;
    if (params?.endDate) stringParams.endDate = params.endDate;
    if (params?.clientId) stringParams.clientId = params.clientId;
    return api!.makeRequest('GET', '/analytics/tickets', undefined, Object.keys(stringParams).length ? stringParams : undefined);
  },
};

// Settings service
export const settings = {
  async get(): Promise<unknown> {
    return api!.getSettings();
  },

  async update(data: Record<string, unknown>): Promise<void> {
    await api!.updateSettings(data);
  },
};

// Categories service (for tickets)
export const categories = {
  async list(): Promise<unknown[]> {
    const result = await api!.makeRequest<{ categories?: unknown[] } | unknown[]>('GET', '/categories');
    return (result as any).categories || result || [];
  },

  async create(data: { name: string; color?: string }): Promise<unknown> {
    return api!.makeRequest('POST', '/categories', data);
  },

  async update(id: string, data: Partial<{ name: string; color: string }>): Promise<unknown> {
    return api!.makeRequest('PUT', `/categories/${id}`, data);
  },

  async delete(id: string): Promise<void> {
    await api!.makeRequest('DELETE', `/categories/${id}`);
  },
};

// Tags service (for tickets)
export const tags = {
  async list(): Promise<unknown[]> {
    const result = await api!.makeRequest<{ tags?: unknown[] } | unknown[]>('GET', '/tags');
    return (result as any).tags || result || [];
  },

  async create(data: { name: string; color?: string }): Promise<unknown> {
    return api!.makeRequest('POST', '/tags', data);
  },

  async update(id: string, data: Partial<{ name: string; color: string }>): Promise<unknown> {
    return api!.makeRequest('PUT', `/tags/${id}`, data);
  },

  async delete(id: string): Promise<void> {
    await api!.makeRequest('DELETE', `/tags/${id}`);
  },

  async getAssignments(ticketId: string): Promise<unknown[]> {
    const result = await api!.makeRequest<{ tags?: unknown[] } | unknown[]>('GET', `/tickets/${ticketId}/tags`);
    return (result as any).tags || result || [];
  },

  async assign(ticketId: string, tagIds: string[]): Promise<void> {
    await api!.makeRequest('PUT', `/tickets/${ticketId}/tags`, { tagIds });
  },
};

// Links service (for ticket links)
export const links = {
  async list(ticketId: string): Promise<unknown[]> {
    const result = await api!.makeRequest<{ links?: unknown[] } | unknown[]>('GET', `/tickets/${ticketId}/links`);
    return (result as any).links || result || [];
  },

  async create(data: { sourceId: string; targetId: string; type: string }): Promise<unknown> {
    return api!.makeRequest('POST', `/tickets/${data.sourceId}/links`, data);
  },

  async delete(linkId: string): Promise<void> {
    await api!.makeRequest('DELETE', `/ticket-links/${linkId}`);
  },
};

// Commands service
export const commands = {
  async execute(deviceId: string, command: string, commandType: string = 'shell'): Promise<unknown> {
    return api!.executeCommand(deviceId, command, commandType);
  },

  async getHistory(deviceId: string): Promise<unknown[]> {
    const result = await api!.getDeviceCommands(deviceId);
    return (result as any).commands || result || [];
  },
};

// Agent service
export const agent = {
  async download(platform: string): Promise<{ url: string }> {
    // In web mode, we construct the download URL
    const baseUrl = api!.getAgentDownloadUrl(platform, 'amd64', '');
    return { url: baseUrl };
  },

  async downloadConfigured(platform: string): Promise<{ url: string }> {
    const baseUrl = api!.getAgentDownloadUrl(platform, 'amd64', '');
    return { url: baseUrl };
  },
};

// Windows Updates service
export const updates = {
  async getDevice(deviceId: string): Promise<unknown> {
    return api!.makeRequest('GET', `/devices/${deviceId}/windows-updates`);
  },

  onStatus(handler: (data: unknown) => void): () => void {
    return wsService!.on('windows_update_status', handler);
  },
};

// Portal service
export const portal = {
  async getSettings(): Promise<unknown> {
    return api!.makeRequest('GET', '/portal/settings');
  },

  async updateSettings(data: Record<string, unknown>): Promise<void> {
    await api!.makeRequest('PUT', '/portal/settings', data);
  },

  async getClientTenants(): Promise<unknown[]> {
    return api!.getClientTenants();
  },

  async createClientTenant(data: { clientId?: string; tenantId: string; tenantName?: string }): Promise<unknown> {
    return api!.createClientTenant(data);
  },

  async deleteClientTenant(id: string): Promise<void> {
    await api!.deleteClientTenant(id);
  },
};

// Server info service (for web mode, returns minimal info)
export const server = {
  async getInfo(): Promise<{ port: number; version: string }> {
    // In web mode, port is not relevant - return 443 (HTTPS)
    return { port: 443, version: '1.72.0' };
  },
};

// Backend connection service (for web mode, not applicable)
export const backend = {
  async getConfig(): Promise<{ url: string; isConfigured: boolean; isAuthenticated: boolean }> {
    // Web mode is always connected to backend
    return { url: window.location.origin, isConfigured: true, isAuthenticated: true };
  },

  async setUrl(_url: string): Promise<void> {
    // No-op in web mode
  },

  async setApiKey(_key: string): Promise<void> {
    // No-op in web mode
  },

  async testConnection(): Promise<{ success: boolean }> {
    return { success: true };
  },

  async authenticate(): Promise<{ success: boolean }> {
    return { success: true };
  },
};
