/**
 * BackendWebSocket - WebSocket proxy to external Docker backend
 *
 * Connects to the Docker server's /ws/dashboard endpoint to enable:
 * - Terminal operations (start, input, output, resize, close)
 * - File operations (list drives, list files, download, upload)
 * - Real-time metrics updates
 */

import WebSocket from 'ws';
import { EventEmitter } from 'events';
import { v4 as uuidv4 } from 'uuid';

interface PendingRequest {
  resolve: (data: any) => void;
  reject: (error: Error) => void;
  timeout: NodeJS.Timeout;
}

export class BackendWebSocket extends EventEmitter {
  private ws: WebSocket | null = null;
  private backendUrl: string | null = null;
  private accessToken: string | null = null;
  private pendingRequests: Map<string, PendingRequest> = new Map();
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 5;
  private reconnectDelay = 2000;
  private isConnecting = false;
  private shouldReconnect = true;
  private pingInterval: NodeJS.Timeout | null = null;

  constructor() {
    super();
  }

  setCredentials(backendUrl: string, accessToken: string): void {
    this.backendUrl = backendUrl.replace(/\/$/, '');
    this.accessToken = accessToken;
  }

  isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  async connect(): Promise<void> {
    if (this.isConnecting) {
      console.log('[BackendWS] Already connecting...');
      return;
    }

    if (!this.backendUrl || !this.accessToken) {
      throw new Error('Backend URL and access token required');
    }

    this.isConnecting = true;
    this.shouldReconnect = true;

    return new Promise((resolve, reject) => {
      try {
        // Convert HTTP URL to WebSocket URL
        const wsUrl = this.backendUrl!
          .replace(/^http:/, 'ws:')
          .replace(/^https:/, 'wss:');

        const fullUrl = `${wsUrl}/ws/dashboard?token=${this.accessToken}`;
        console.log('[BackendWS] Connecting to:', wsUrl + '/ws/dashboard');

        this.ws = new WebSocket(fullUrl);

        this.ws.on('open', () => {
          console.log('[BackendWS] Connected to backend');
          this.isConnecting = false;
          this.reconnectAttempts = 0;
          this.startPingInterval();
          resolve();
        });

        this.ws.on('message', (data: WebSocket.Data) => {
          try {
            const message = JSON.parse(data.toString());
            this.handleMessage(message);
          } catch (error) {
            console.error('[BackendWS] Failed to parse message:', error);
          }
        });

        this.ws.on('close', (code, reason) => {
          console.log('[BackendWS] Connection closed:', code, reason.toString());
          this.cleanup();
          if (this.shouldReconnect && this.reconnectAttempts < this.maxReconnectAttempts) {
            this.scheduleReconnect();
          }
        });

        this.ws.on('error', (error) => {
          console.error('[BackendWS] WebSocket error:', error.message);
          this.isConnecting = false;
          if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
            reject(error);
          }
        });

        // Connection timeout
        setTimeout(() => {
          if (this.isConnecting) {
            this.isConnecting = false;
            this.ws?.close();
            reject(new Error('Connection timeout'));
          }
        }, 10000);

      } catch (error) {
        this.isConnecting = false;
        reject(error);
      }
    });
  }

  disconnect(): void {
    this.shouldReconnect = false;
    this.cleanup();
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  private cleanup(): void {
    if (this.pingInterval) {
      clearInterval(this.pingInterval);
      this.pingInterval = null;
    }
    // Reject all pending requests
    for (const [requestId, pending] of this.pendingRequests) {
      clearTimeout(pending.timeout);
      pending.reject(new Error('WebSocket connection closed'));
    }
    this.pendingRequests.clear();
  }

  private startPingInterval(): void {
    this.pingInterval = setInterval(() => {
      if (this.ws?.readyState === WebSocket.OPEN) {
        this.ws.ping();
      }
    }, 30000);
  }

  private scheduleReconnect(): void {
    this.reconnectAttempts++;
    const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1);
    console.log(`[BackendWS] Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts}/${this.maxReconnectAttempts})`);
    setTimeout(() => {
      this.connect().catch(err => {
        console.error('[BackendWS] Reconnection failed:', err.message);
      });
    }, delay);
  }

  private handleMessage(message: any): void {
    // Check if this is a response to a pending request
    if (message.requestId && this.pendingRequests.has(message.requestId)) {
      const pending = this.pendingRequests.get(message.requestId)!;
      clearTimeout(pending.timeout);
      this.pendingRequests.delete(message.requestId);

      if (message.type === 'error' || message.error || message.success === false) {
        pending.reject(new Error(message.error || message.message || 'Request failed'));
      } else {
        pending.resolve(message.data || message);
      }
      return;
    }

    // Handle broadcast messages (terminal output, file progress, metrics, etc.)
    switch (message.type) {
      case 'terminal_output':
        this.emit('terminal:data', {
          sessionId: message.sessionId,
          data: message.data || message.payload?.data,
        });
        break;

      case 'file_progress':
      case 'file_data':
        this.emit('files:progress', {
          deviceId: message.deviceId,
          filename: message.filename,
          bytesTransferred: message.bytesTransferred,
          totalBytes: message.totalBytes,
          percentage: message.percentage,
        });
        break;

      case 'metrics':
        this.emit('metrics:updated', message.payload || message);
        break;

      case 'device_online':
        this.emit('devices:online', message.payload || message);
        break;

      case 'device_offline':
        this.emit('devices:offline', message.payload || message);
        break;

      default:
        // Emit generic message for other types
        this.emit('message', message);
    }
  }

  private async sendRequest(message: any, timeout = 30000): Promise<any> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('Not connected to backend');
    }

    const requestId = message.requestId || uuidv4();
    message.requestId = requestId;

    return new Promise((resolve, reject) => {
      const timeoutHandle = setTimeout(() => {
        this.pendingRequests.delete(requestId);
        reject(new Error('Request timeout'));
      }, timeout);

      this.pendingRequests.set(requestId, {
        resolve,
        reject,
        timeout: timeoutHandle,
      });

      this.ws!.send(JSON.stringify(message));
    });
  }

  // Terminal operations
  async startTerminal(deviceId: string, agentId: string): Promise<{ sessionId: string }> {
    const sessionId = uuidv4();
    const response = await this.sendRequest({
      type: 'start_terminal',
      agentId,
      deviceId,
      sessionId,
    });
    return { sessionId: response.sessionId || sessionId };
  }

  async sendTerminalInput(sessionId: string, agentId: string, data: string): Promise<void> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('Not connected to backend');
    }
    // Terminal input is fire-and-forget (no response expected)
    this.ws.send(JSON.stringify({
      type: 'terminal_input',
      sessionId,
      agentId,
      data,
    }));
  }

  async resizeTerminal(sessionId: string, agentId: string, cols: number, rows: number): Promise<void> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('Not connected to backend');
    }
    this.ws.send(JSON.stringify({
      type: 'terminal_resize',
      sessionId,
      agentId,
      cols,
      rows,
    }));
  }

  async closeTerminal(sessionId: string, agentId: string): Promise<void> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('Not connected to backend');
    }
    this.ws.send(JSON.stringify({
      type: 'close_terminal',
      sessionId,
      agentId,
    }));
  }

  // File operations
  async listDrives(deviceId: string, agentId: string): Promise<any[]> {
    const response = await this.sendRequest({
      type: 'list_drives',
      agentId,
      deviceId,
    });
    return response.drives || response.payload?.drives || [];
  }

  async listFiles(deviceId: string, agentId: string, path: string): Promise<any[]> {
    const response = await this.sendRequest({
      type: 'list_files',
      agentId,
      deviceId,
      path,
    });
    return response.files || response.payload?.files || [];
  }

  async downloadFile(deviceId: string, agentId: string, remotePath: string, localPath: string): Promise<void> {
    // File download is more complex - needs to handle streaming
    // For now, send the request and let the backend handle it
    await this.sendRequest({
      type: 'download_file',
      agentId,
      deviceId,
      remotePath,
      localPath,
    }, 300000); // 5 minute timeout for downloads
  }

  async uploadFile(deviceId: string, agentId: string, localPath: string, remotePath: string): Promise<void> {
    await this.sendRequest({
      type: 'upload_file',
      agentId,
      deviceId,
      localPath,
      remotePath,
    }, 300000); // 5 minute timeout for uploads
  }

  async scanDirectory(deviceId: string, agentId: string, path: string, maxDepth: number): Promise<any> {
    const response = await this.sendRequest({
      type: 'scan_directory',
      agentId,
      deviceId,
      path,
      maxDepth,
    }, 600000); // 10 minute timeout for scans
    return response;
  }

  // Metrics interval
  async setMetricsInterval(deviceId: string, agentId: string, intervalMs: number): Promise<void> {
    await this.sendRequest({
      type: 'set_metrics_interval',
      agentId,
      deviceId,
      intervalMs,
    });
  }
}
