import WebSocket from 'ws';
import { randomUUID } from 'crypto';

export interface DeviceInfo {
  hostname: string;
  platform: string;
  osType: string;
  osVersion: string;
  architecture: string;
  cpuModel: string;
  cpuCores: number;
  totalMemory: number;
  ipAddress: string;
  macAddress: string;
}

interface WSMessage {
  type: string;
  requestId?: string;
  agentId?: string;
  timestamp?: string;
  payload?: unknown;
}

const DEFAULT_DEVICE_INFO: DeviceInfo = {
  hostname: 'TEST-AGENT-' + Math.random().toString(36).substring(2, 8).toUpperCase(),
  platform: 'windows',
  osType: 'Windows',
  osVersion: '10.0.22631',
  architecture: 'x64',
  cpuModel: 'Test CPU (E2E Simulator)',
  cpuCores: 4,
  totalMemory: 8589934592,
  ipAddress: '192.168.1.200',
  macAddress: 'AA:BB:CC:DD:EE:FF',
};

/**
 * Simulates a Sentinel agent connecting via WebSocket.
 * Used in E2E tests to create realistic agent connections.
 */
export class AgentSimulator {
  private ws: WebSocket | null = null;
  private heartbeatInterval: ReturnType<typeof setInterval> | null = null;
  private connected = false;
  private authenticated = false;
  private messageHandlers: Map<string, (msg: WSMessage) => void> = new Map();

  public readonly agentId: string;
  public readonly deviceInfo: DeviceInfo;
  private readonly serverUrl: string;
  private readonly enrollmentToken: string;

  constructor(
    serverUrl: string,
    enrollmentToken: string,
    agentId?: string,
    deviceInfo?: Partial<DeviceInfo>
  ) {
    // Convert https:// to wss:// and http:// to ws://
    this.serverUrl = serverUrl
      .replace(/^https:\/\//, 'wss://')
      .replace(/^http:\/\//, 'ws://');
    this.enrollmentToken = enrollmentToken;
    this.agentId = agentId || randomUUID();
    this.deviceInfo = { ...DEFAULT_DEVICE_INFO, ...deviceInfo };
  }

  /**
   * Connect to the Sentinel WebSocket endpoint and authenticate.
   * Resolves when auth_response with success=true is received.
   * Rejects on connection error or auth failure.
   */
  async connect(): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      const wsUrl = `${this.serverUrl}/ws/agent`;

      this.ws = new WebSocket(wsUrl, {
        rejectUnauthorized: false, // Allow self-signed certs in test
      });

      const authTimeout = setTimeout(() => {
        reject(new Error(`Agent ${this.agentId}: Auth timeout after 15s`));
        this.destroy();
      }, 15000);

      this.ws.on('open', () => {
        this.connected = true;

        // Send auth message
        const authMsg: WSMessage = {
          type: 'auth',
          payload: {
            agentId: this.agentId,
            token: this.enrollmentToken,
            deviceInfo: this.deviceInfo,
          },
        };
        this.ws!.send(JSON.stringify(authMsg));
      });

      this.ws.on('message', (data: WebSocket.Data) => {
        let msg: WSMessage;
        try {
          msg = JSON.parse(data.toString());
        } catch {
          return;
        }

        // Handle auth response
        if (msg.type === 'auth_response') {
          clearTimeout(authTimeout);
          const payload = msg.payload as { success: boolean; error?: string; deviceId?: string };
          if (payload.success) {
            this.authenticated = true;
            this.startHeartbeat();
            resolve();
          } else {
            reject(new Error(`Agent ${this.agentId}: Auth failed: ${payload.error || 'unknown'}`));
            this.destroy();
          }
          return;
        }

        // Handle heartbeat ack
        if (msg.type === 'heartbeat_ack') {
          return;
        }

        // Handle ping with pong
        if (msg.type === 'ping') {
          this.send({ type: 'pong' });
          return;
        }

        // Dispatch to registered handlers
        const handler = this.messageHandlers.get(msg.type);
        if (handler) {
          handler(msg);
        }
      });

      this.ws.on('error', (err: Error) => {
        clearTimeout(authTimeout);
        reject(new Error(`Agent ${this.agentId}: WebSocket error: ${err.message}`));
      });

      this.ws.on('close', () => {
        this.connected = false;
        this.authenticated = false;
        this.stopHeartbeat();
      });
    });
  }

  /**
   * Send a heartbeat message to the server.
   */
  sendHeartbeat(): void {
    if (!this.connected || !this.authenticated) return;
    this.send({
      type: 'heartbeat',
      agentId: this.agentId,
      timestamp: new Date().toISOString(),
      payload: {
        uptime: Math.floor(Math.random() * 86400),
        cpuPercent: Math.random() * 100,
        memoryPercent: 30 + Math.random() * 40,
        diskPercent: 40 + Math.random() * 30,
      },
    });
  }

  /**
   * Wait until the agent is authenticated and online.
   * Returns immediately if already connected.
   */
  async waitForOnline(timeoutMs = 10000): Promise<void> {
    if (this.authenticated) return;

    return new Promise<void>((resolve, reject) => {
      const start = Date.now();
      const check = setInterval(() => {
        if (this.authenticated) {
          clearInterval(check);
          resolve();
        } else if (Date.now() - start > timeoutMs) {
          clearInterval(check);
          reject(new Error(`Agent ${this.agentId}: Did not come online within ${timeoutMs}ms`));
        }
      }, 100);
    });
  }

  /**
   * Register a handler for a specific message type.
   */
  onMessage(type: string, handler: (msg: WSMessage) => void): void {
    this.messageHandlers.set(type, handler);
  }

  /**
   * Gracefully disconnect the agent.
   */
  disconnect(): void {
    this.stopHeartbeat();
    if (this.ws && this.connected) {
      this.ws.close(1000, 'Test agent disconnecting');
    }
    this.connected = false;
    this.authenticated = false;
  }

  /**
   * Forcefully destroy the agent simulator and clean up all resources.
   */
  destroy(): void {
    this.stopHeartbeat();
    this.messageHandlers.clear();
    if (this.ws) {
      try {
        this.ws.terminate();
      } catch {
        // Ignore errors during cleanup
      }
      this.ws = null;
    }
    this.connected = false;
    this.authenticated = false;
  }

  get isConnected(): boolean {
    return this.connected;
  }

  get isAuthenticated(): boolean {
    return this.authenticated;
  }

  private send(msg: WSMessage): void {
    if (this.ws && this.connected) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  private startHeartbeat(): void {
    this.stopHeartbeat();
    // Send heartbeat every 30 seconds (matches real agent behavior)
    this.heartbeatInterval = setInterval(() => {
      this.sendHeartbeat();
    }, 30000);
    // Send an initial heartbeat immediately
    this.sendHeartbeat();
  }

  private stopHeartbeat(): void {
    if (this.heartbeatInterval) {
      clearInterval(this.heartbeatInterval);
      this.heartbeatInterval = null;
    }
  }
}

/**
 * Enroll a new agent via the REST API and return the device ID.
 * This creates the device record server-side before the WS connection.
 */
export async function enrollAgentViaAPI(
  baseUrl: string,
  enrollmentToken: string,
  agentId: string,
  deviceInfo: DeviceInfo
): Promise<{ deviceId: string; killToken?: string }> {
  const response = await fetch(`${baseUrl}/api/agent/enroll`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Enrollment-Token': enrollmentToken,
    },
    body: JSON.stringify({
      agentId,
      hostname: deviceInfo.hostname,
      platform: deviceInfo.platform,
      osType: deviceInfo.osType,
      osVersion: deviceInfo.osVersion,
      architecture: deviceInfo.architecture,
      cpuModel: deviceInfo.cpuModel,
      cpuCores: deviceInfo.cpuCores,
      totalMemory: deviceInfo.totalMemory,
      ipAddress: deviceInfo.ipAddress,
      macAddress: deviceInfo.macAddress,
    }),
  });

  if (!response.ok) {
    const body = await response.text();
    throw new Error(`Enrollment failed (${response.status}): ${body}`);
  }

  const data = await response.json();
  return {
    deviceId: data.deviceId || data.device_id,
    killToken: data.killToken,
  };
}
