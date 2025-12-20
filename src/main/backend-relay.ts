/**
 * BackendRelay - Forwards API requests to external backend (Docker) when agents
 * are not connected to the local Electron server.
 *
 * This enables the Electron app to control agents that are connected to a
 * Docker/standalone backend instance.
 */

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

export class BackendRelay {
  private config: BackendConfig | null = null;
  private tokens: AuthTokens | null = null;
  private database: any; // Reference to database for getting settings

  constructor(database: any) {
    this.database = database;
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
    console.log('[BackendRelay] Backend URL set to:', url);
  }

  getBackendUrl(): string | null {
    return this.config?.url || null;
  }

  isConfigured(): boolean {
    return !!this.config?.url;
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
      return true;
    } catch (error) {
      console.error('[BackendRelay] Authentication error:', error);
      return false;
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

  // File operations - these may need WebSocket, so we'll handle them specially
  async listFiles(deviceId: string, path: string): Promise<any> {
    // File operations go through WebSocket in the backend
    // For now, we'll throw an error indicating this needs WebSocket
    throw new Error('File operations require direct WebSocket connection - not supported via relay');
  }

  async listDrives(deviceId: string): Promise<any> {
    throw new Error('File operations require direct WebSocket connection - not supported via relay');
  }

  // Terminal and Remote Desktop - these also need WebSocket
  async startTerminal(deviceId: string): Promise<any> {
    throw new Error('Terminal requires direct WebSocket connection - not supported via relay');
  }

  async startRemoteSession(deviceId: string): Promise<any> {
    throw new Error('Remote desktop requires direct WebSocket connection - not supported via relay');
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
}
