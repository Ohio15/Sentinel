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

          if (existingDevice) {
            // Update existing device
            await this.database.updateDeviceFromBackend(device.id, {
              hostname: device.hostname,
              displayName: device.displayName,
              osType: device.osType,
              osVersion: device.osVersion,
              architecture: device.architecture,
              agentVersion: device.agentVersion,
              ipAddress: device.ipAddress,
              macAddress: device.macAddress,
              status: device.status,
              lastSeen: device.lastSeen,
              agentId: device.agentId,
            });
          } else {
            // Create new device
            await this.database.createDeviceFromBackend({
              id: device.id,
              hostname: device.hostname,
              displayName: device.displayName,
              osType: device.osType,
              osVersion: device.osVersion,
              architecture: device.architecture,
              agentVersion: device.agentVersion,
              ipAddress: device.ipAddress,
              macAddress: device.macAddress,
              status: device.status,
              lastSeen: device.lastSeen,
              agentId: device.agentId,
              clientId: device.clientId,
            });
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
    try {
      // Load backend URL from settings
      const backendUrl = await this.database.getSetting('externalBackendUrl');
      if (backendUrl) {
        this.config = { url: backendUrl };
        console.log('[BackendRelay] Initialized with URL:', backendUrl);
      }
    } catch (error) {
      console.log('[BackendRelay] No external backend configured');
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

  async authenticate(username: string, password: string): Promise<boolean> {
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
}
