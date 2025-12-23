/**
 * BackendRelay - Forwards API requests to external backend (Docker) when agents
 * are not connected to the local Electron server.
 *
 * This enables the Electron app to control agents that are connected to a
 * Docker/standalone backend instance.
 */

import { BackendWebSocket } from './backend-websocket';

// Native fetch available in Node.js 18+ (bundled with Electron 25+)

interface BackendConfig {
  url: string;
  username?: string;
  password?: string;
}

interface AuthTokens {
  accessToken: string;
  refreshToken: string;
  expiresAt: number;
}

// Terminal session tracking for relay
interface RelayTerminalSession {
  sessionId: string;
  deviceId: string;
  agentId: string;
}

export class BackendRelay {
  private config: BackendConfig | null = null;
  private tokens: AuthTokens | null = null;
  private database: any; // Reference to database for getting settings
  private websocket: BackendWebSocket;
  private terminalSessions: Map<string, RelayTerminalSession> = new Map();
  private notifyRenderer: ((channel: string, data: any) => void) | null = null;

  constructor(database: any) {
    this.database = database;
    this.websocket = new BackendWebSocket();
    this.setupWebSocketListeners();
  }

  // Sync devices from Docker backend to local SQLite
  async syncDevices(): Promise<void> {
    if (!this.isAuthenticated()) {
      console.log('[BackendRelay] Not authenticated, skipping device sync');
      return;
    }

    try {
      console.log('[BackendRelay] Syncing devices from backend...');
      const devices = await this.makeRequest('GET', '/api/devices');

      if (!Array.isArray(devices)) {
        console.log('[BackendRelay] No devices returned from backend');
        return;
      }

      console.log(`[BackendRelay] Got ${devices.length} devices from backend`);

      for (const device of devices) {
        try {
          // Check if device exists locally
          const existingDevice = await this.database.getDevice(device.id);

          // Sync ALL device fields (not just basic ones) for complete offline access
          const fullDeviceData = {
            id: device.id,
            agentId: device.agentId,
            hostname: device.hostname,
            displayName: device.displayName,
            osType: device.osType,
            osVersion: device.osVersion,
            osBuild: device.osBuild,
            platform: device.platform,
            platformFamily: device.platformFamily,
            architecture: device.architecture,
            cpuModel: device.cpuModel,
            cpuCores: device.cpuCores,
            cpuThreads: device.cpuThreads,
            cpuSpeed: device.cpuSpeed,
            totalMemory: device.totalMemory,
            bootTime: device.bootTime,
            gpu: device.gpu,
            storage: device.storage,
            serialNumber: device.serialNumber,
            manufacturer: device.manufacturer,
            model: device.model,
            domain: device.domain,
            agentVersion: device.agentVersion,
            lastSeen: device.lastSeen,
            status: device.status,
            ipAddress: device.ipAddress,
            publicIp: device.publicIp,
            macAddress: device.macAddress,
            tags: device.tags,
            metadata: device.metadata,
            clientId: device.clientId,
            createdAt: device.createdAt,
            updatedAt: device.updatedAt,
          };

          if (existingDevice) {
            // Update existing device with all fields
            await this.database.updateDeviceFromBackend(device.id, fullDeviceData);
          } else {
            // Create new device with all fields
            await this.database.createDeviceFromBackend(fullDeviceData);
          }
        } catch (error) {
          console.error(`[BackendRelay] Failed to sync device ${device.id}:`, error);
        }
      }

      console.log('[BackendRelay] Device sync complete');
    } catch (error) {
      console.error('[BackendRelay] Device sync failed:', error);
    }
  }

  private setupWebSocketListeners(): void {
    // Forward terminal output to renderer
    this.websocket.on('terminal:data', (data) => {
      if (this.notifyRenderer) {
        this.notifyRenderer('terminal:data', data);
      }
    });

    // Forward file progress to renderer
    this.websocket.on('files:progress', (data) => {
      if (this.notifyRenderer) {
        this.notifyRenderer('files:progress', data);
      }
    });

    // Forward metrics updates to renderer
    this.websocket.on('metrics:updated', (data) => {
      if (this.notifyRenderer) {
        this.notifyRenderer('metrics:updated', data);
      }
    });

    // Forward device status updates
    this.websocket.on('devices:online', (data) => {
      if (this.notifyRenderer) {
        this.notifyRenderer('devices:online', data);
      }
    });

    this.websocket.on('devices:offline', (data) => {
      if (this.notifyRenderer) {
        this.notifyRenderer('devices:offline', data);
      }
    });
  }

  setNotifyRenderer(fn: (channel: string, data: any) => void): void {
    this.notifyRenderer = fn;
  }

  async initialize(): Promise<void> {
    const DEFAULT_BACKEND_URL = 'http://localhost:8090';

    try {
      // Load backend URL from settings, with default fallback
      let backendUrl = await this.database.getSetting('externalBackendUrl');

      // Use default URL if none configured
      if (!backendUrl) {
        backendUrl = DEFAULT_BACKEND_URL;
        console.log('[BackendRelay] No URL configured, using default:', DEFAULT_BACKEND_URL);
      }

      this.config = { url: backendUrl };
      console.log('[BackendRelay] Initialized with URL:', backendUrl);

      // Load saved credentials and auto-authenticate
      const savedUsername = await this.database.getSetting('backendUsername');
      const savedPassword = await this.database.getSetting('backendPassword');

      if (savedUsername && savedPassword) {
        console.log('[BackendRelay] Found saved credentials, auto-authenticating...');
        try {
          const success = await this.authenticate(savedUsername, savedPassword, true);
          if (success) {
            console.log('[BackendRelay] Auto-authentication successful');
          } else {
            console.log('[BackendRelay] Auto-authentication failed, credentials may have changed');
          }
        } catch (authError) {
          console.error('[BackendRelay] Auto-authentication error:', authError);
        }
      }
    } catch (error) {
      console.log('[BackendRelay] No external backend configured, using default');
      this.config = { url: DEFAULT_BACKEND_URL };
    }
  }


  setBackendUrl(url: string): void {
    this.config = { url: url.replace(/\/$/, '') }; // Remove trailing slash
    this.tokens = null; // Clear tokens when URL changes
    this.websocket.disconnect(); // Disconnect WebSocket when URL changes
    console.log('[BackendRelay] Backend URL set to:', url);
  }

  getBackendUrl(): string | null {
    return this.config?.url || null;
  }

  isConfigured(): boolean {
    return !!this.config?.url;
  }

  isAuthenticated(): boolean {
    return !!this.tokens && Date.now() < this.tokens.expiresAt;
  }

  isWebSocketConnected(): boolean {
    return this.websocket.isConnected();
  }

  async authenticate(username: string, password: string, isAutoLogin: boolean = false): Promise<boolean> {
    if (!this.config?.url) {
      throw new Error('Backend URL not configured');
    }

    try {
      const response = await fetch(`${this.config.url}/api/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: username, password }),
      });

      if (!response.ok) {
        console.error('[BackendRelay] Authentication failed:', response.status);
        return false;
      }

      const data = await response.json() as any;
      this.tokens = {
        accessToken: data.accessToken,
        refreshToken: data.refreshToken,
        expiresAt: Date.now() + (data.expiresIn * 1000) - 60000, // Refresh 1 min before expiry
      };
      this.config.username = username;
      this.config.password = password;
      console.log('[BackendRelay] Authentication successful');

      // Save credentials for auto-login on next startup (only on manual login)
      if (!isAutoLogin) {
        try {
          await this.database.updateSettings({
            backendUsername: username,
            backendPassword: password
          });
          console.log('[BackendRelay] Credentials saved for auto-login');
        } catch (saveError) {
          console.error('[BackendRelay] Failed to save credentials:', saveError);
        }
      }

      // Connect WebSocket with the new token
      try {
        this.websocket.setCredentials(this.config.url, this.tokens.accessToken);
        await this.websocket.connect();
        console.log('[BackendRelay] WebSocket connected');
      } catch (wsError) {
        console.error('[BackendRelay] WebSocket connection failed:', wsError);
        // Continue without WebSocket - HTTP operations will still work
      }

      // Sync devices from backend to local database
      try {
        await this.syncDevices();
      } catch (syncError) {
        console.error('[BackendRelay] Initial device sync failed:', syncError);
      }

      return true;
    } catch (error) {
      console.error('[BackendRelay] Authentication error:', error);
      return false;
    }
  }

  async ensureWebSocketConnected(): Promise<void> {
    if (!this.websocket.isConnected()) {
      await this.ensureAuthenticated();
      if (this.tokens && this.config?.url) {
        this.websocket.setCredentials(this.config.url, this.tokens.accessToken);
        await this.websocket.connect();
      }
    }
  }

  private async ensureAuthenticated(): Promise<void> {
    if (!this.tokens || Date.now() > this.tokens.expiresAt) {
      // Try to refresh token
      if (this.tokens?.refreshToken) {
        try {
          const response = await fetch(`${this.config!.url}/api/auth/refresh`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ refreshToken: this.tokens.refreshToken }),
          });

          if (response.ok) {
            const data = await response.json() as any;
            this.tokens = {
              accessToken: data.accessToken,
              refreshToken: data.refreshToken || this.tokens.refreshToken,
              expiresAt: Date.now() + (data.expiresIn * 1000) - 60000,
            };
            // Update WebSocket with new token
            if (this.config?.url) {
              this.websocket.setCredentials(this.config.url, this.tokens.accessToken);
            }
            return;
          }
        } catch (error) {
          console.error('[BackendRelay] Token refresh failed:', error);
        }
      }

      // Re-authenticate with stored credentials
      if (this.config?.username && this.config?.password) {
        await this.authenticate(this.config.username, this.config.password);
      } else {
        throw new Error('Not authenticated with backend');
      }
    }
  }

  private async makeRequest(
    method: string,
    path: string,
    body?: any
  ): Promise<any> {
    if (!this.config?.url) {
      throw new Error('Backend URL not configured');
    }

    await this.ensureAuthenticated();

    const options: any = {
      method,
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${this.tokens!.accessToken}`,
      },
    };

    if (body) {
      options.body = JSON.stringify(body);
    }

    const response = await fetch(`${this.config.url}${path}`, options);

    if (!response.ok) {
      const errorText = await response.text();
      throw new Error(`Backend request failed: ${response.status} ${errorText}`);
    }

    return response.json();
  }

  // Device operations
  async executeCommand(deviceId: string, command: string, commandType: string): Promise<any> {
    return this.makeRequest('POST', `/api/devices/${deviceId}/commands`, {
      command,
      commandType,
    });
  }

  async pingDevice(deviceId: string): Promise<any> {
    return this.makeRequest('POST', `/api/devices/${deviceId}/ping`);
  }

  async getDeviceMetrics(deviceId: string, hours: number = 24): Promise<any> {
    return this.makeRequest('GET', `/api/devices/${deviceId}/metrics?hours=${hours}`);
  }

  async getDevice(deviceId: string): Promise<any> {
    return this.makeRequest('GET', `/api/devices/${deviceId}`);
  }

  // Script execution
  async executeScript(scriptId: string, deviceIds: string[]): Promise<any> {
    return this.makeRequest('POST', `/api/scripts/${scriptId}/execute`, {
      deviceIds,
    });
  }

  // Inventory
  async getDeviceInventory(deviceId: string): Promise<any> {
    return this.makeRequest('GET', `/api/devices/${deviceId}/inventory`);
  }

  async triggerInventoryCollection(deviceId: string): Promise<any> {
    return this.makeRequest('POST', `/api/devices/${deviceId}/inventory/collect`);
  }

  // Terminal operations (via WebSocket)
  async startTerminal(deviceId: string, agentId: string): Promise<{ sessionId: string }> {
    await this.ensureWebSocketConnected();
    const result = await this.websocket.startTerminal(deviceId, agentId);
    // Track the session
    this.terminalSessions.set(result.sessionId, {
      sessionId: result.sessionId,
      deviceId,
      agentId,
    });
    return result;
  }

  async sendTerminalData(sessionId: string, data: string): Promise<void> {
    const session = this.terminalSessions.get(sessionId);
    if (!session) {
      throw new Error('Terminal session not found');
    }
    await this.ensureWebSocketConnected();
    await this.websocket.sendTerminalInput(sessionId, session.agentId, data);
  }

  async resizeTerminal(sessionId: string, cols: number, rows: number): Promise<void> {
    const session = this.terminalSessions.get(sessionId);
    if (!session) {
      throw new Error('Terminal session not found');
    }
    await this.ensureWebSocketConnected();
    await this.websocket.resizeTerminal(sessionId, session.agentId, cols, rows);
  }

  async closeTerminal(sessionId: string): Promise<void> {
    const session = this.terminalSessions.get(sessionId);
    if (!session) {
      // Session may already be closed
      return;
    }
    try {
      if (this.websocket.isConnected()) {
        await this.websocket.closeTerminal(sessionId, session.agentId);
      }
    } finally {
      this.terminalSessions.delete(sessionId);
    }
  }

  // File operations (via WebSocket)
  async listDrives(deviceId: string, agentId: string): Promise<any[]> {
    await this.ensureWebSocketConnected();
    return this.websocket.listDrives(deviceId, agentId);
  }

  async listFiles(deviceId: string, agentId: string, path: string): Promise<any[]> {
    await this.ensureWebSocketConnected();
    return this.websocket.listFiles(deviceId, agentId, path);
  }

  async downloadFile(deviceId: string, agentId: string, remotePath: string, localPath: string): Promise<void> {
    await this.ensureWebSocketConnected();
    return this.websocket.downloadFile(deviceId, agentId, remotePath, localPath);
  }

  async uploadFile(deviceId: string, agentId: string, localPath: string, remotePath: string): Promise<void> {
    await this.ensureWebSocketConnected();
    return this.websocket.uploadFile(deviceId, agentId, localPath, remotePath);
  }

  async scanDirectory(deviceId: string, agentId: string, path: string, maxDepth: number): Promise<any> {
    await this.ensureWebSocketConnected();
    return this.websocket.scanDirectory(deviceId, agentId, path, maxDepth);
  }

  // Metrics interval (via WebSocket)
  async setMetricsInterval(deviceId: string, agentId: string, intervalMs: number): Promise<void> {
    await this.ensureWebSocketConnected();
    return this.websocket.setMetricsInterval(deviceId, agentId, intervalMs);
  }

  // Agent management
  async uninstallAgent(deviceId: string): Promise<any> {
    return this.makeRequest('POST', `/api/devices/${deviceId}/uninstall`);
  }

  async disableDevice(deviceId: string): Promise<any> {
    return this.makeRequest('POST', `/api/devices/${deviceId}/disable`);
  }

  async enableDevice(deviceId: string): Promise<any> {
    return this.makeRequest('POST', `/api/devices/${deviceId}/enable`);
  }

  // Check if a terminal session is a relay session
  isRelaySession(sessionId: string): boolean {
    return this.terminalSessions.has(sessionId);
  }

  getRelaySession(sessionId: string): RelayTerminalSession | undefined {
    return this.terminalSessions.get(sessionId);
  }

  // ==================== CLIENT MANAGEMENT ====================

  async getClients(): Promise<any[]> {
    return this.makeRequest('GET', '/api/clients');
  }

  async getClient(clientId: string): Promise<any> {
    return this.makeRequest('GET', `/api/clients/${clientId}`);
  }

  async createClient(data: any): Promise<any> {
    return this.makeRequest('POST', '/api/clients', data);
  }

  async updateClient(clientId: string, data: any): Promise<any> {
    return this.makeRequest('PUT', `/api/clients/${clientId}`, data);
  }

  async deleteClient(clientId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/clients/${clientId}`);
  }

  async getClientDevices(clientId: string): Promise<any[]> {
    return this.makeRequest('GET', `/api/clients/${clientId}/devices`);
  }

  async getClientTickets(clientId: string): Promise<any[]> {
    return this.makeRequest('GET', `/api/clients/${clientId}/tickets`);
  }

  // ==================== TICKET MANAGEMENT ====================

  async getTickets(filters?: { status?: string; priority?: string; clientId?: string }): Promise<any[]> {
    const params = new URLSearchParams();
    if (filters?.status) params.append('status', filters.status);
    if (filters?.priority) params.append('priority', filters.priority);
    if (filters?.clientId) params.append('clientId', filters.clientId);
    const query = params.toString();
    return this.makeRequest('GET', `/api/tickets${query ? '?' + query : ''}`);
  }

  async getTicket(ticketId: string): Promise<any> {
    return this.makeRequest('GET', `/api/tickets/${ticketId}`);
  }

  async createTicket(data: any): Promise<any> {
    return this.makeRequest('POST', '/api/tickets', data);
  }

  async updateTicket(ticketId: string, data: any): Promise<any> {
    return this.makeRequest('PUT', `/api/tickets/${ticketId}`, data);
  }

  async deleteTicket(ticketId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/tickets/${ticketId}`);
  }

  async getTicketComments(ticketId: string): Promise<any[]> {
    return this.makeRequest('GET', `/api/tickets/${ticketId}/comments`);
  }

  async addTicketComment(ticketId: string, content: string, isInternal: boolean = false): Promise<any> {
    return this.makeRequest('POST', `/api/tickets/${ticketId}/comments`, { content, isInternal });
  }

  async updateTicketComment(ticketId: string, commentId: string, content: string): Promise<any> {
    return this.makeRequest('PUT', `/api/tickets/${ticketId}/comments/${commentId}`, { content });
  }

  async deleteTicketComment(ticketId: string, commentId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/tickets/${ticketId}/comments/${commentId}`);
  }

  async getTicketAttachments(ticketId: string): Promise<any[]> {
    return this.makeRequest('GET', `/api/tickets/${ticketId}/attachments`);
  }

  async getTicketTimeline(ticketId: string): Promise<any[]> {
    return this.makeRequest('GET', `/api/tickets/${ticketId}/timeline`);
  }

  async getTicketStats(): Promise<any> {
    const response = await this.makeRequest('GET', '/api/tickets/stats');
    // Transform server response format to match frontend expected format
    const byStatus = response.byStatus || {};
    return {
      openCount: byStatus.open || 0,
      inProgressCount: byStatus.inProgress || 0,
      waitingCount: byStatus.waiting || 0,
      resolvedCount: byStatus.resolved || 0,
      closedCount: byStatus.closed || 0,
      totalCount: (byStatus.open || 0) + (byStatus.inProgress || 0) + (byStatus.waiting || 0) + (byStatus.resolved || 0) + (byStatus.closed || 0),
    };
  }

  // ==================== TICKET TEMPLATES ====================

  async getTicketTemplates(): Promise<any[]> {
    return this.makeRequest('GET', '/api/ticket-templates');
  }

  async getTicketTemplate(templateId: string): Promise<any> {
    return this.makeRequest('GET', `/api/ticket-templates/${templateId}`);
  }

  async createTicketTemplate(data: any): Promise<any> {
    return this.makeRequest('POST', '/api/ticket-templates', data);
  }

  async updateTicketTemplate(templateId: string, data: any): Promise<any> {
    return this.makeRequest('PUT', `/api/ticket-templates/${templateId}`, data);
  }

  async deleteTicketTemplate(templateId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/ticket-templates/${templateId}`);
  }

  // ==================== SLA POLICIES ====================

  async getSLAPolicies(clientId?: string): Promise<any[]> {
    const query = clientId ? `?clientId=${clientId}` : '';
    return this.makeRequest('GET', `/api/sla-policies${query}`);
  }

  async getSLAPolicy(policyId: string): Promise<any> {
    return this.makeRequest('GET', `/api/sla-policies/${policyId}`);
  }

  async createSLAPolicy(data: any): Promise<any> {
    return this.makeRequest('POST', '/api/sla-policies', data);
  }

  async updateSLAPolicy(policyId: string, data: any): Promise<any> {
    return this.makeRequest('PUT', `/api/sla-policies/${policyId}`, data);
  }

  async deleteSLAPolicy(policyId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/sla-policies/${policyId}`);
  }

  // ==================== TICKET CATEGORIES ====================

  async getTicketCategories(): Promise<any[]> {
    return this.makeRequest('GET', '/api/ticket-categories');
  }

  async getTicketCategory(categoryId: string): Promise<any> {
    return this.makeRequest('GET', `/api/ticket-categories/${categoryId}`);
  }

  async createTicketCategory(data: any): Promise<any> {
    return this.makeRequest('POST', '/api/ticket-categories', data);
  }

  async updateTicketCategory(categoryId: string, data: any): Promise<any> {
    return this.makeRequest('PUT', `/api/ticket-categories/${categoryId}`, data);
  }

  async deleteTicketCategory(categoryId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/ticket-categories/${categoryId}`);
  }

  // ==================== TICKET TAGS ====================

  async getTicketTags(): Promise<any[]> {
    return this.makeRequest('GET', '/api/ticket-tags');
  }

  async getTicketTag(tagId: string): Promise<any> {
    return this.makeRequest('GET', `/api/ticket-tags/${tagId}`);
  }

  async createTicketTag(data: any): Promise<any> {
    return this.makeRequest('POST', '/api/ticket-tags', data);
  }

  async updateTicketTag(tagId: string, data: any): Promise<any> {
    return this.makeRequest('PUT', `/api/ticket-tags/${tagId}`, data);
  }

  async deleteTicketTag(tagId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/ticket-tags/${tagId}`);
  }

  async addTagToTicket(ticketId: string, tagId: string): Promise<any> {
    return this.makeRequest('POST', `/api/tickets/${ticketId}/tags/${tagId}`);
  }

  async removeTagFromTicket(ticketId: string, tagId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/tickets/${ticketId}/tags/${tagId}`);
  }

  // ==================== TICKET LINKS ====================

  async getTicketLinks(ticketId: string): Promise<any[]> {
    return this.makeRequest('GET', `/api/tickets/${ticketId}/links`);
  }

  async createTicketLink(ticketId: string, data: { linkedTicketId: string; linkType: string }): Promise<any> {
    return this.makeRequest('POST', `/api/tickets/${ticketId}/links`, data);
  }

  async deleteTicketLink(ticketId: string, linkId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/tickets/${ticketId}/links/${linkId}`);
  }

  // ==================== CUSTOM FIELDS ====================

  async getCustomFieldDefinitions(): Promise<any[]> {
    return this.makeRequest('GET', '/api/custom-fields');
  }

  async getCustomFieldDefinition(fieldId: string): Promise<any> {
    return this.makeRequest('GET', `/api/custom-fields/${fieldId}`);
  }

  async createCustomFieldDefinition(data: any): Promise<any> {
    return this.makeRequest('POST', '/api/custom-fields', data);
  }

  async updateCustomFieldDefinition(fieldId: string, data: any): Promise<any> {
    return this.makeRequest('PUT', `/api/custom-fields/${fieldId}`, data);
  }

  async deleteCustomFieldDefinition(fieldId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/custom-fields/${fieldId}`);
  }

  async getTicketCustomFields(ticketId: string): Promise<any[]> {
    return this.makeRequest('GET', `/api/tickets/${ticketId}/custom-fields`);
  }

  async setTicketCustomField(ticketId: string, fieldId: string, value: any): Promise<any> {
    return this.makeRequest('PUT', `/api/tickets/${ticketId}/custom-fields/${fieldId}`, { value });
  }

  // ==================== KNOWLEDGE BASE ====================

  async getKBCategories(): Promise<any[]> {
    return this.makeRequest('GET', '/api/kb/categories');
  }

  async getKBCategory(categoryId: string): Promise<any> {
    return this.makeRequest('GET', `/api/kb/categories/${categoryId}`);
  }

  async createKBCategory(data: any): Promise<any> {
    return this.makeRequest('POST', '/api/kb/categories', data);
  }

  async updateKBCategory(categoryId: string, data: any): Promise<any> {
    return this.makeRequest('PUT', `/api/kb/categories/${categoryId}`, data);
  }

  async deleteKBCategory(categoryId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/kb/categories/${categoryId}`);
  }

  async getKBArticles(categoryId?: string, search?: string): Promise<any[]> {
    const params = new URLSearchParams();
    if (categoryId) params.append('categoryId', categoryId);
    if (search) params.append('search', search);
    const query = params.toString();
    return this.makeRequest('GET', `/api/kb/articles${query ? '?' + query : ''}`);
  }

  async getKBArticle(articleId: string): Promise<any> {
    return this.makeRequest('GET', `/api/kb/articles/${articleId}`);
  }

  async createKBArticle(data: any): Promise<any> {
    return this.makeRequest('POST', '/api/kb/articles', data);
  }

  async updateKBArticle(articleId: string, data: any): Promise<any> {
    return this.makeRequest('PUT', `/api/kb/articles/${articleId}`, data);
  }

  async deleteKBArticle(articleId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/kb/articles/${articleId}`);
  }

  // ==================== DEVICE MANAGEMENT ====================

  async getDevices(): Promise<any[]> {
    return this.makeRequest('GET', '/api/devices');
  }

  async updateDevice(deviceId: string, data: any): Promise<any> {
    return this.makeRequest('PUT', `/api/devices/${deviceId}`, data);
  }

  async deleteDevice(deviceId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/devices/${deviceId}`);
  }

  async getDeviceAlerts(deviceId: string): Promise<any[]> {
    return this.makeRequest('GET', `/api/devices/${deviceId}/alerts`);
  }

  async getAgentCertStatuses(): Promise<any[]> {
    return this.makeRequest('GET', '/api/devices/cert-status');
  }

  async getDeviceSoftware(deviceId: string): Promise<any[]> {
    return this.makeRequest('GET', `/api/devices/${deviceId}/software`);
  }

  async getDeviceServices(deviceId: string): Promise<any[]> {
    return this.makeRequest('GET', `/api/devices/${deviceId}/services`);
  }

  async getDeviceProcesses(deviceId: string): Promise<any[]> {
    return this.makeRequest('GET', `/api/devices/${deviceId}/processes`);
  }

  async restartDevice(deviceId: string, delay?: number): Promise<any> {
    return this.makeRequest('POST', `/api/devices/${deviceId}/restart`, { delay });
  }

  async shutdownDevice(deviceId: string, delay?: number): Promise<any> {
    return this.makeRequest('POST', `/api/devices/${deviceId}/shutdown`, { delay });
  }

  // ==================== ALERTS ====================

  async getAlerts(filters?: { deviceId?: string; severity?: string; acknowledged?: boolean }): Promise<any[]> {
    const params = new URLSearchParams();
    if (filters?.deviceId) params.append('deviceId', filters.deviceId);
    if (filters?.severity) params.append('severity', filters.severity);
    if (filters?.acknowledged !== undefined) params.append('acknowledged', String(filters.acknowledged));
    const query = params.toString();
    return this.makeRequest('GET', `/api/alerts${query ? '?' + query : ''}`);
  }

  async getAlert(alertId: string): Promise<any> {
    return this.makeRequest('GET', `/api/alerts/${alertId}`);
  }

  async acknowledgeAlert(alertId: string): Promise<any> {
    return this.makeRequest('POST', `/api/alerts/${alertId}/acknowledge`);
  }

  async resolveAlert(alertId: string): Promise<any> {
    return this.makeRequest('POST', `/api/alerts/${alertId}/resolve`);
  }

  async deleteAlert(alertId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/alerts/${alertId}`);
  }

  // ==================== ALERT RULES ====================

  async getAlertRules(): Promise<any[]> {
    return this.makeRequest('GET', '/api/alert-rules');
  }

  async getAlertRule(ruleId: string): Promise<any> {
    return this.makeRequest('GET', `/api/alert-rules/${ruleId}`);
  }

  async createAlertRule(data: any): Promise<any> {
    return this.makeRequest('POST', '/api/alert-rules', data);
  }

  async updateAlertRule(ruleId: string, data: any): Promise<any> {
    return this.makeRequest('PUT', `/api/alert-rules/${ruleId}`, data);
  }

  async deleteAlertRule(ruleId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/alert-rules/${ruleId}`);
  }

  // ==================== SCRIPTS ====================

  async getScripts(): Promise<any[]> {
    return this.makeRequest('GET', '/api/scripts');
  }

  async getScript(scriptId: string): Promise<any> {
    return this.makeRequest('GET', `/api/scripts/${scriptId}`);
  }

  async createScript(data: any): Promise<any> {
    return this.makeRequest('POST', '/api/scripts', data);
  }

  async updateScript(scriptId: string, data: any): Promise<any> {
    return this.makeRequest('PUT', `/api/scripts/${scriptId}`, data);
  }

  async deleteScript(scriptId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/scripts/${scriptId}`);
  }

  async getScriptExecutions(scriptId: string): Promise<any[]> {
    return this.makeRequest('GET', `/api/scripts/${scriptId}/executions`);
  }

  // ==================== USERS ====================

  async getUsers(): Promise<any[]> {
    return this.makeRequest('GET', '/api/users');
  }

  async getUser(userId: string): Promise<any> {
    return this.makeRequest('GET', `/api/users/${userId}`);
  }

  async createUser(data: any): Promise<any> {
    return this.makeRequest('POST', '/api/users', data);
  }

  async updateUser(userId: string, data: any): Promise<any> {
    return this.makeRequest('PUT', `/api/users/${userId}`, data);
  }

  async deleteUser(userId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/users/${userId}`);
  }

  async getCurrentUser(): Promise<any> {
    return this.makeRequest('GET', '/api/users/me');
  }

  async updateCurrentUser(data: any): Promise<any> {
    return this.makeRequest('PUT', '/api/users/me', data);
  }

  async changePassword(currentPassword: string, newPassword: string): Promise<any> {
    return this.makeRequest('POST', '/api/users/me/password', { currentPassword, newPassword });
  }

  // ==================== ENROLLMENT TOKENS ====================

  async getEnrollmentTokens(): Promise<any[]> {
    return this.makeRequest('GET', '/api/enrollment-tokens');
  }

  async getEnrollmentToken(tokenId: string): Promise<any> {
    return this.makeRequest('GET', `/api/enrollment-tokens/${tokenId}`);
  }

  async createEnrollmentToken(data: any): Promise<any> {
    return this.makeRequest('POST', '/api/enrollment-tokens', data);
  }

  async revokeEnrollmentToken(tokenId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/enrollment-tokens/${tokenId}`);
  }

  // ==================== DASHBOARD ====================

  async getDashboardStats(): Promise<any> {
    return this.makeRequest('GET', '/api/dashboard/stats');
  }

  async getDashboardAlerts(): Promise<any[]> {
    return this.makeRequest('GET', '/api/dashboard/alerts');
  }

  async getDashboardActivity(): Promise<any[]> {
    return this.makeRequest('GET', '/api/dashboard/activity');
  }

  // ==================== SETTINGS ====================

  async getSettings(): Promise<any> {
    return this.makeRequest('GET', '/api/settings');
  }

  async updateSettings(data: any): Promise<any> {
    return this.makeRequest('PUT', '/api/settings', data);
  }

  // ==================== PORTAL SESSIONS ====================

  async getPortalSessions(): Promise<any[]> {
    return this.makeRequest('GET', '/api/portal-sessions');
  }

  async createPortalSession(clientId: string): Promise<any> {
    return this.makeRequest('POST', '/api/portal-sessions', { clientId });
  }

  async deletePortalSession(sessionId: string): Promise<any> {
    return this.makeRequest('DELETE', `/api/portal-sessions/${sessionId}`);
  }

  // ==================== AUDIT LOG ====================

  async getAuditLogs(filters?: { userId?: string; action?: string; from?: string; to?: string }): Promise<any[]> {
    const params = new URLSearchParams();
    if (filters?.userId) params.append('userId', filters.userId);
    if (filters?.action) params.append('action', filters.action);
    if (filters?.from) params.append('from', filters.from);
    if (filters?.to) params.append('to', filters.to);
    const query = params.toString();
    return this.makeRequest('GET', `/api/audit-logs${query ? '?' + query : ''}`);
  }
}
