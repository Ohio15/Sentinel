/**
 * LocalStore - Lightweight local storage for Electron app settings
 * Uses electron-store for persistent JSON-based storage
 * This replaces PostgreSQL database for local-only operations
 */

import Store from 'electron-store';

interface Settings {
  externalBackendUrl?: string;
  azureClientId?: string;
  azureClientSecret?: string;
  azureRedirectUri?: string;
  emailNotificationsEnabled?: string;
  portalUrl?: string;
  smtpHost?: string;
  smtpPort?: string;
  smtpSecure?: string;
  smtpUser?: string;
  smtpPassword?: string;
  smtpFromAddress?: string;
  smtpFromName?: string;
  metricsRetentionDays?: string;
  commandHistoryRetentionDays?: string;
  alertRetentionDays?: string;
  [key: string]: string | undefined;
}

interface ClientTenant {
  id: string;
  clientId: string;
  tenantId: string;
  tenantName?: string;
  createdAt: string;
}

interface StoreSchema {
  settings: Settings;
  clientTenants: ClientTenant[];
  cachedDevices: any[];
  alertRules: any[];
  scripts: any[];
}

export class LocalStore {
  private store: Store<StoreSchema>;

  constructor() {
    this.store = new Store<StoreSchema>({
      name: 'sentinel-local',
      defaults: {
        settings: {
          metricsRetentionDays: '30',
          commandHistoryRetentionDays: '90',
          alertRetentionDays: '30',
        },
        clientTenants: [],
        cachedDevices: [],
        alertRules: [],
        scripts: [],
      },
    });
  }

  async initialize(): Promise<void> {
    console.log('[LocalStore] Initialized with electron-store');
    console.log('[LocalStore] Store path:', this.store.path);
  }

  async close(): Promise<void> {
    // No-op for electron-store, but kept for interface compatibility
    console.log('[LocalStore] Closed');
  }

  // ==================== SETTINGS ====================

  async getSettings(): Promise<Settings> {
    return this.store.get('settings', {});
  }

  async getSetting(key: string): Promise<string | undefined> {
    const settings = this.store.get('settings', {});
    return settings[key];
  }

  async updateSettings(updates: Partial<Settings>): Promise<void> {
    const current = this.store.get('settings', {});
    this.store.set('settings', { ...current, ...updates });
  }

  // ==================== CLIENT TENANTS ====================

  async getClientTenants(): Promise<ClientTenant[]> {
    return this.store.get('clientTenants', []);
  }

  async createClientTenant(data: { clientId: string; tenantId: string; tenantName?: string }): Promise<ClientTenant> {
    const tenant: ClientTenant = {
      id: crypto.randomUUID(),
      clientId: data.clientId,
      tenantId: data.tenantId,
      tenantName: data.tenantName,
      createdAt: new Date().toISOString(),
    };
    const tenants = this.store.get('clientTenants', []);
    tenants.push(tenant);
    this.store.set('clientTenants', tenants);
    return tenant;
  }

  async deleteClientTenant(id: string): Promise<void> {
    const tenants = this.store.get('clientTenants', []);
    this.store.set('clientTenants', tenants.filter((t: ClientTenant) => t.id !== id));
  }

  // ==================== LOCAL CACHE OPERATIONS ====================
  // These are for offline/fallback mode when backend is not connected

  async getCachedDevices(): Promise<any[]> {
    return this.store.get('cachedDevices', []);
  }

  async setCachedDevices(devices: any[]): Promise<void> {
    this.store.set('cachedDevices', devices);
  }

  async getCachedDevice(id: string): Promise<any | null> {
    const devices = this.store.get('cachedDevices', []);
    return devices.find((d: any) => d.id === id) || null;
  }

  async updateCachedDevice(id: string, updates: any): Promise<void> {
    const devices = this.store.get('cachedDevices', []);
    const index = devices.findIndex((d: any) => d.id === id);
    if (index !== -1) {
      devices[index] = { ...devices[index], ...updates };
      this.store.set('cachedDevices', devices);
    }
  }

  // ==================== LOCAL ALERT RULES ====================
  // For offline mode

  async getAlertRules(): Promise<any[]> {
    return this.store.get('alertRules', []);
  }

  async createAlertRule(rule: any): Promise<any> {
    const rules = this.store.get('alertRules', []);
    const newRule = { ...rule, id: crypto.randomUUID(), createdAt: new Date().toISOString() };
    rules.push(newRule);
    this.store.set('alertRules', rules);
    return newRule;
  }

  async updateAlertRule(id: string, updates: any): Promise<void> {
    const rules = this.store.get('alertRules', []);
    const index = rules.findIndex((r: any) => r.id === id);
    if (index !== -1) {
      rules[index] = { ...rules[index], ...updates };
      this.store.set('alertRules', rules);
    }
  }

  async deleteAlertRule(id: string): Promise<void> {
    const rules = this.store.get('alertRules', []);
    this.store.set('alertRules', rules.filter((r: any) => r.id !== id));
  }

  // ==================== LOCAL SCRIPTS ====================
  // For offline mode

  async getScripts(): Promise<any[]> {
    return this.store.get('scripts', []);
  }

  async createScript(script: any): Promise<any> {
    const scripts = this.store.get('scripts', []);
    const newScript = { ...script, id: crypto.randomUUID(), createdAt: new Date().toISOString() };
    scripts.push(newScript);
    this.store.set('scripts', scripts);
    return newScript;
  }

  async updateScript(id: string, updates: any): Promise<void> {
    const scripts = this.store.get('scripts', []);
    const index = scripts.findIndex((s: any) => s.id === id);
    if (index !== -1) {
      scripts[index] = { ...scripts[index], ...updates };
      this.store.set('scripts', scripts);
    }
  }

  async deleteScript(id: string): Promise<void> {
    const scripts = this.store.get('scripts', []);
    this.store.set('scripts', scripts.filter((s: any) => s.id !== id));
  }

  // ==================== STUB METHODS FOR COMPATIBILITY ====================
  // These return empty results but prevent crashes when backend is not connected

  async getAlerts(): Promise<any[]> {
    return [];
  }

  async acknowledgeAlert(_id: string): Promise<void> {
    // No-op in local mode
  }

  async resolveAlert(_id: string): Promise<void> {
    // No-op in local mode
  }

  async getTickets(_filters?: any): Promise<any[]> {
    return [];
  }

  async getTicket(_id: string): Promise<any | null> {
    return null;
  }

  async createTicket(_ticket: any): Promise<any> {
    throw new Error('Cannot create tickets in offline mode. Please connect to the backend server.');
  }

  async updateTicket(_id: string, _updates: any): Promise<void> {
    throw new Error('Cannot update tickets in offline mode. Please connect to the backend server.');
  }

  async deleteTicket(_id: string): Promise<void> {
    throw new Error('Cannot delete tickets in offline mode. Please connect to the backend server.');
  }

  async getTicketComments(_ticketId: string): Promise<any[]> {
    return [];
  }

  async createTicketComment(_comment: any): Promise<any> {
    throw new Error('Cannot create comments in offline mode. Please connect to the backend server.');
  }

  async getTicketActivity(_ticketId: string): Promise<any[]> {
    return [];
  }

  async getTicketStats(): Promise<any> {
    return { total: 0, open: 0, inProgress: 0, resolved: 0, closed: 0 };
  }

  async getTicketTemplates(): Promise<any[]> {
    return [];
  }

  async createTicketTemplate(_template: any): Promise<any> {
    throw new Error('Cannot create templates in offline mode. Please connect to the backend server.');
  }

  async updateTicketTemplate(_id: string, _template: any): Promise<void> {
    throw new Error('Cannot update templates in offline mode. Please connect to the backend server.');
  }

  async deleteTicketTemplate(_id: string): Promise<void> {
    throw new Error('Cannot delete templates in offline mode. Please connect to the backend server.');
  }

  async getSLAPolicies(_clientId?: string): Promise<any[]> {
    return [];
  }

  async getSLAPolicy(_id: string): Promise<any | null> {
    return null;
  }

  async createSLAPolicy(_policy: any): Promise<any> {
    throw new Error('Cannot create SLA policies in offline mode. Please connect to the backend server.');
  }

  async updateSLAPolicy(_id: string, _updates: any): Promise<void> {
    throw new Error('Cannot update SLA policies in offline mode. Please connect to the backend server.');
  }

  async deleteSLAPolicy(_id: string): Promise<void> {
    throw new Error('Cannot delete SLA policies in offline mode. Please connect to the backend server.');
  }

  async calculateSLADueDates(_ticketId: string): Promise<any> {
    return null;
  }

  async recordFirstResponse(_ticketId: string): Promise<void> {
    // No-op
  }

  async pauseSLA(_ticketId: string): Promise<void> {
    // No-op
  }

  async resumeSLA(_ticketId: string): Promise<void> {
    // No-op
  }

  async checkSLABreaches(): Promise<any[]> {
    return [];
  }

  async getTicketCategories(_clientId?: string): Promise<any[]> {
    return [];
  }

  async getTicketCategory(_id: string): Promise<any | null> {
    return null;
  }

  async createTicketCategory(_category: any): Promise<any> {
    throw new Error('Cannot create categories in offline mode. Please connect to the backend server.');
  }

  async updateTicketCategory(_id: string, _updates: any): Promise<void> {
    throw new Error('Cannot update categories in offline mode. Please connect to the backend server.');
  }

  async deleteTicketCategory(_id: string): Promise<void> {
    throw new Error('Cannot delete categories in offline mode. Please connect to the backend server.');
  }

  async getTicketTags(_clientId?: string): Promise<any[]> {
    return [];
  }

  async getTicketTag(_id: string): Promise<any | null> {
    return null;
  }

  async createTicketTag(_tag: any): Promise<any> {
    throw new Error('Cannot create tags in offline mode. Please connect to the backend server.');
  }

  async updateTicketTag(_id: string, _updates: any): Promise<void> {
    throw new Error('Cannot update tags in offline mode. Please connect to the backend server.');
  }

  async deleteTicketTag(_id: string): Promise<void> {
    throw new Error('Cannot delete tags in offline mode. Please connect to the backend server.');
  }

  async getTicketTagAssignments(_ticketId: string): Promise<any[]> {
    return [];
  }

  async assignTagsToTicket(_ticketId: string, _tagIds: string[], _assignedBy?: string): Promise<void> {
    throw new Error('Cannot assign tags in offline mode. Please connect to the backend server.');
  }

  async getTicketLinks(_ticketId: string): Promise<any[]> {
    return [];
  }

  async createTicketLink(_link: any): Promise<any> {
    throw new Error('Cannot create links in offline mode. Please connect to the backend server.');
  }

  async deleteTicketLink(_id: string): Promise<void> {
    throw new Error('Cannot delete links in offline mode. Please connect to the backend server.');
  }

  async getTicketAnalytics(_params: any): Promise<any> {
    return {};
  }

  async getKBCategories(): Promise<any[]> {
    return [];
  }

  async getKBCategory(_id: string): Promise<any | null> {
    return null;
  }

  async createKBCategory(_category: any): Promise<any> {
    throw new Error('Cannot create KB categories in offline mode. Please connect to the backend server.');
  }

  async updateKBCategory(_id: string, _updates: any): Promise<void> {
    throw new Error('Cannot update KB categories in offline mode. Please connect to the backend server.');
  }

  async deleteKBCategory(_id: string): Promise<void> {
    throw new Error('Cannot delete KB categories in offline mode. Please connect to the backend server.');
  }

  async getKBArticles(_options?: any): Promise<any[]> {
    return [];
  }

  async getKBArticle(_id: string): Promise<any | null> {
    return null;
  }

  async getKBArticleBySlug(_slug: string): Promise<any | null> {
    return null;
  }

  async createKBArticle(_article: any): Promise<any> {
    throw new Error('Cannot create KB articles in offline mode. Please connect to the backend server.');
  }

  async updateKBArticle(_id: string, _updates: any): Promise<void> {
    throw new Error('Cannot update KB articles in offline mode. Please connect to the backend server.');
  }

  async deleteKBArticle(_id: string): Promise<void> {
    throw new Error('Cannot delete KB articles in offline mode. Please connect to the backend server.');
  }

  async searchKBArticles(_query: string, _limit?: number): Promise<any[]> {
    return [];
  }

  async suggestKBArticles(_ticketSubject: string, _limit?: number): Promise<any[]> {
    return [];
  }

  async getKBFeaturedArticles(_limit?: number): Promise<any[]> {
    return [];
  }

  async getKBRelatedArticles(_articleId: string): Promise<any[]> {
    return [];
  }

  async getClientsWithCounts(): Promise<any[]> {
    return [];
  }

  async getClient(_id: string): Promise<any | null> {
    return null;
  }

  async createClient(_client: any): Promise<any> {
    throw new Error('Cannot create clients in offline mode. Please connect to the backend server.');
  }

  async updateClient(_id: string, _updates: any): Promise<void> {
    throw new Error('Cannot update clients in offline mode. Please connect to the backend server.');
  }

  async deleteClient(_id: string): Promise<void> {
    throw new Error('Cannot delete clients in offline mode. Please connect to the backend server.');
  }

  async getDevices(_clientId?: string): Promise<any[]> {
    return this.getCachedDevices();
  }

  async getDevice(id: string): Promise<any | null> {
    return this.getCachedDevice(id);
  }

  async getDeviceStatus(_id: string): Promise<string> {
    return 'unknown';
  }

  async deleteDevice(_id: string): Promise<void> {
    throw new Error('Cannot delete devices in offline mode. Please connect to the backend server.');
  }

  async updateDevice(id: string, updates: any): Promise<void> {
    return this.updateCachedDevice(id, updates);
  }

  async assignDeviceToClient(deviceId: string, clientId: string | null): Promise<void> {
    return this.updateCachedDevice(deviceId, { clientId });
  }

  async bulkAssignDevicesToClient(deviceIds: string[], clientId: string | null): Promise<void> {
    for (const deviceId of deviceIds) {
      await this.updateCachedDevice(deviceId, { clientId });
    }
  }

  async getDeviceMetrics(_deviceId: string, _hours: number): Promise<any[]> {
    return [];
  }

  async getCommandHistory(_deviceId: string): Promise<any[]> {
    return [];
  }

  async getAgentCertStatuses(): Promise<any[]> {
    return [];
  }

  async getAllDeviceUpdateStatuses(): Promise<any[]> {
    return [];
  }

  async getDeviceUpdateStatus(_deviceId: string): Promise<any | null> {
    return null;
  }

  async getDevicesWithPendingUpdates(_minCount?: number): Promise<any[]> {
    return [];
  }

  async getDevicesWithSecurityUpdates(): Promise<any[]> {
    return [];
  }

  async getDevicesRequiringReboot(): Promise<any[]> {
    return [];
  }

  // For BackendRelay compatibility
  async updateDeviceFromBackend(id: string, updates: any): Promise<void> {
    return this.updateCachedDevice(id, updates);
  }

  async createDeviceFromBackend(device: any): Promise<void> {
    const devices = this.store.get('cachedDevices', []);
    devices.push(device);
    this.store.set('cachedDevices', devices);
  }
}
