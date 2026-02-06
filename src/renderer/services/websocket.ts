/**
 * Reliable WebSocket Service for Web mode
 * Features:
 * - Infinite reconnection with exponential backoff
 * - Request/response correlation with requestId
 * - Session recovery on reconnect
 * - Input queue during disconnection
 * - Heartbeat/keepalive handling
 */
import { isWeb, getWsBaseUrl } from './env';

type MessageHandler = (data: unknown) => void;

interface WebSocketMessage {
  type: string;
  requestId?: string;
  messageId?: string;
  sessionId?: string;
  deviceId?: string;
  payload?: unknown;
  [key: string]: unknown;
}

interface PendingRequest {
  resolve: (value: unknown) => void;
  reject: (reason: Error) => void;
  timeout: ReturnType<typeof setTimeout>;
  type: string;
  sentAt: number;
}

interface SessionInfo {
  sessionId: string;
  deviceId: string;
  type: 'terminal' | 'rdp' | 'files';
}

// Connection state
type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting';

class ReliableWebSocket {
  private ws: WebSocket | null = null;
  private reconnectAttempts = 0;
  private readonly maxReconnectDelay = 60000; // Cap at 1 minute
  private readonly baseReconnectDelay = 1000;
  private handlers: Map<string, Set<MessageHandler>> = new Map();
  private isConnecting = false;
  private connectionState: ConnectionState = 'disconnected';

  // Request/response correlation
  private pendingRequests = new Map<string, PendingRequest>();
  private readonly defaultRequestTimeout = 30000; // 30 seconds

  // Session recovery
  private activeSessions = new Map<string, SessionInfo>();

  // Message queue during disconnection
  private messageQueue: WebSocketMessage[] = [];
  private readonly maxQueueSize = 100;

  // Heartbeat
  private heartbeatInterval: ReturnType<typeof setInterval> | null = null;
  private readonly heartbeatPeriod = 30000; // 30 seconds
  private lastPongAt = 0;

  // Metrics
  private messagesSent = 0;
  private messagesReceived = 0;
  private reconnectCount = 0;

  connect() {
    if (this.ws?.readyState === WebSocket.OPEN || this.isConnecting) {
      return;
    }

    const token = localStorage.getItem('token');
    if (!token) {
      console.warn('[WebSocket] No auth token, skipping connection');
      return;
    }

    this.isConnecting = true;
    this.connectionState = this.reconnectAttempts > 0 ? 'reconnecting' : 'connecting';
    const wsUrl = `${getWsBaseUrl()}/ws/dashboard?token=${token}`;
    console.log(`[WebSocket] Connecting to: ${wsUrl} (attempt ${this.reconnectAttempts + 1})`);

    try {
      this.ws = new WebSocket(wsUrl);

      this.ws.onopen = () => {
        console.log('[WebSocket] Connected');
        this.isConnecting = false;
        this.connectionState = 'connected';
        this.lastPongAt = Date.now();

        // Reset reconnect counter on successful connection
        if (this.reconnectAttempts > 0) {
          this.reconnectCount++;
          console.log(`[WebSocket] Reconnected after ${this.reconnectAttempts} attempts (total reconnects: ${this.reconnectCount})`);
        }
        this.reconnectAttempts = 0;

        // Start heartbeat
        this.startHeartbeat();

        // Recover sessions
        this.recoverSessions();

        // Flush queued messages
        this.flushMessageQueue();

        this.emit('connected', { reconnected: this.reconnectCount > 0 });
      };

      this.ws.onclose = (event) => {
        console.log(`[WebSocket] Disconnected (code: ${event.code}, reason: ${event.reason || 'none'})`);
        this.isConnecting = false;
        this.connectionState = 'disconnected';
        this.stopHeartbeat();

        // Fail pending requests
        this.failPendingRequests('WebSocket connection closed');

        this.emit('disconnected', { code: event.code, reason: event.reason });

        // Always attempt to reconnect (infinite reconnection)
        this.scheduleReconnect();
      };

      this.ws.onerror = (error) => {
        console.error('[WebSocket] Error:', error);
        this.isConnecting = false;
        // Connection error will be followed by onclose, which handles reconnection
      };

      this.ws.onmessage = (event) => {
        try {
          this.messagesReceived++;
          const message = JSON.parse(event.data) as WebSocketMessage;

          // Handle heartbeat response
          if (message.type === 'pong' || message.type === 'heartbeat_ack') {
            this.lastPongAt = Date.now();
            return;
          }

          // Check if this is a response to a pending request
          if (message.requestId && this.pendingRequests.has(message.requestId)) {
            this.handleResponse(message);
            return;
          }

          // Route to handlers
          const data = this.extractPayload(message);

          // Translate server event types to frontend event types (matching Electron behavior)
          if (message.type === 'device_metrics') {
            // Server sends 'device_metrics' but deviceStore listens for 'metrics:updated'
            const metricsData = {
              deviceId: message.deviceId,
              metrics: message.metrics || (data as any)?.metrics,
              source: 'websocket',
            };
            console.log('[WebSocket] device_metrics received, emitting metrics:updated:',
              metricsData.deviceId, 'CPU:', (metricsData.metrics as any)?.cpuPercent);
            this.emit('metrics:updated', metricsData, message);
          }

          this.emit(message.type, data, message);
        } catch (err) {
          console.error('[WebSocket] Failed to parse message:', err);
        }
      };
    } catch (err) {
      console.error('[WebSocket] Failed to create connection:', err);
      this.isConnecting = false;
      this.scheduleReconnect();
    }
  }

  disconnect() {
    console.log('[WebSocket] Disconnecting (manual)');
    this.stopHeartbeat();
    if (this.ws) {
      this.ws.close(1000, 'Client disconnect');
      this.ws = null;
    }
    // Don't auto-reconnect on manual disconnect
    this.reconnectAttempts = Infinity;
    this.connectionState = 'disconnected';
  }

  private scheduleReconnect() {
    this.reconnectAttempts++;
    const delay = this.calculateReconnectDelay();
    console.log(`[WebSocket] Reconnecting in ${Math.round(delay / 1000)}s (attempt ${this.reconnectAttempts})`);
    this.connectionState = 'reconnecting';

    setTimeout(() => {
      this.connect();
    }, delay);
  }

  private calculateReconnectDelay(): number {
    // Exponential backoff with jitter, capped at maxReconnectDelay
    const exponentialDelay = Math.min(
      this.baseReconnectDelay * Math.pow(2, this.reconnectAttempts - 1),
      this.maxReconnectDelay
    );
    // Add 0-30% jitter to prevent thundering herd
    const jitter = exponentialDelay * Math.random() * 0.3;
    return exponentialDelay + jitter;
  }

  private startHeartbeat() {
    this.stopHeartbeat();
    this.heartbeatInterval = setInterval(() => {
      if (this.ws?.readyState === WebSocket.OPEN) {
        // Check for dead connection
        if (Date.now() - this.lastPongAt > this.heartbeatPeriod * 2) {
          console.warn('[WebSocket] No heartbeat response, connection may be dead');
          this.ws.close(4000, 'Heartbeat timeout');
          return;
        }

        // Send heartbeat
        this.send('ping', { timestamp: Date.now() });
      }
    }, this.heartbeatPeriod);
  }

  private stopHeartbeat() {
    if (this.heartbeatInterval) {
      clearInterval(this.heartbeatInterval);
      this.heartbeatInterval = null;
    }
  }

  private recoverSessions() {
    if (this.activeSessions.size === 0) {
      return;
    }

    console.log(`[WebSocket] Recovering ${this.activeSessions.size} sessions`);
    for (const [sessionId, info] of this.activeSessions) {
      // Re-subscribe to session
      this.send('subscribe_session', {
        sessionId,
        deviceId: info.deviceId,
        type: info.type,
      });
    }
  }

  private flushMessageQueue() {
    if (this.messageQueue.length === 0) {
      return;
    }

    console.log(`[WebSocket] Flushing ${this.messageQueue.length} queued messages`);
    const queue = [...this.messageQueue];
    this.messageQueue = [];

    for (const msg of queue) {
      this.sendRaw(msg);
    }
  }

  private failPendingRequests(reason: string) {
    for (const [requestId, pending] of this.pendingRequests) {
      clearTimeout(pending.timeout);
      pending.reject(new Error(`${reason} (requestId: ${requestId})`));
    }
    this.pendingRequests.clear();
  }

  private handleResponse(message: WebSocketMessage) {
    const requestId = message.requestId!;
    const pending = this.pendingRequests.get(requestId);
    if (!pending) {
      return;
    }

    this.pendingRequests.delete(requestId);
    clearTimeout(pending.timeout);

    const latency = Date.now() - pending.sentAt;
    if (latency > 5000) {
      console.warn(`[WebSocket] Slow response for ${pending.type}: ${latency}ms`);
    }

    // Check for error in response
    const payload = this.extractPayload(message);
    if (message.type === 'error' || (payload as Record<string, unknown>)?.error) {
      pending.reject(new Error((payload as Record<string, unknown>)?.error as string || 'Request failed'));
    } else {
      pending.resolve(payload);
    }
  }

  private extractPayload(message: WebSocketMessage): unknown {
    // If message has payload field (and it's not null), use it
    if (message.payload !== undefined && message.payload !== null) {
      return message.payload;
    }
    // Otherwise return the whole message minus type and standard fields
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    const { type, requestId, messageId, ...rest } = message;
    return rest;
  }

  send(type: string, payload?: unknown) {
    const message: WebSocketMessage = { type };

    if (payload && typeof payload === 'object') {
      const p = payload as Record<string, unknown>;
      if (p.requestId) {
        message.requestId = p.requestId as string;
      }
      message.payload = payload;
    } else if (payload !== undefined) {
      message.payload = payload;
    }

    this.sendRaw(message);
  }

  private sendRaw(message: WebSocketMessage) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      const msgStr = JSON.stringify(message);
      console.log(`[WebSocket] Sending message: ${message.type}`, msgStr.substring(0, 200));
      this.ws.send(msgStr);
      this.messagesSent++;
    } else if (this.connectionState === 'reconnecting') {
      // Queue message for later
      if (this.messageQueue.length < this.maxQueueSize) {
        this.messageQueue.push(message);
        console.log(`[WebSocket] Queued message (${this.messageQueue.length}/${this.maxQueueSize}): ${message.type}`);
      } else {
        console.warn(`[WebSocket] Message queue full, dropping: ${message.type}`);
      }
    } else {
      console.warn(`[WebSocket] Not connected, cannot send: ${message.type}`);
    }
  }

  /**
   * Send a request and wait for a response with the matching requestId
   */
  async sendRequest<T>(type: string, payload?: Record<string, unknown>, timeout?: number): Promise<T> {
    const requestId = crypto.randomUUID();
    const timeoutMs = timeout ?? this.defaultRequestTimeout;

    return new Promise((resolve, reject) => {
      const timeoutHandle = setTimeout(() => {
        this.pendingRequests.delete(requestId);
        reject(new Error(`Request timeout after ${timeoutMs}ms (type: ${type})`));
      }, timeoutMs);

      this.pendingRequests.set(requestId, {
        resolve: resolve as (value: unknown) => void,
        reject,
        timeout: timeoutHandle,
        type,
        sentAt: Date.now(),
      });

      this.send(type, { ...payload, requestId });
    });
  }

  /**
   * Register an active session for recovery on reconnect
   */
  registerSession(sessionId: string, deviceId: string, type: 'terminal' | 'rdp' | 'files') {
    this.activeSessions.set(sessionId, { sessionId, deviceId, type });
  }

  /**
   * Unregister a session (when closed)
   */
  unregisterSession(sessionId: string) {
    this.activeSessions.delete(sessionId);
  }

  on(type: string, handler: MessageHandler) {
    if (!this.handlers.has(type)) {
      this.handlers.set(type, new Set());
    }
    this.handlers.get(type)!.add(handler);

    // Return unsubscribe function
    return () => {
      this.handlers.get(type)?.delete(handler);
    };
  }

  off(type: string, handler: MessageHandler) {
    this.handlers.get(type)?.delete(handler);
  }

  private emit(type: string, data: unknown, fullMessage?: WebSocketMessage) {
    this.handlers.get(type)?.forEach((handler) => {
      try {
        handler(data);
      } catch (err) {
        console.error(`[WebSocket] Error in handler for ${type}:`, err);
      }
    });

    // Emit to wildcard handlers - pass full message so they can access requestId etc.
    this.handlers.get('*')?.forEach((handler) => {
      try {
        handler(fullMessage || { type, data });
      } catch (err) {
        console.error('[WebSocket] Error in wildcard handler:', err);
      }
    });
  }

  get isConnected() {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  get state(): ConnectionState {
    return this.connectionState;
  }

  get metrics() {
    return {
      state: this.connectionState,
      messagesSent: this.messagesSent,
      messagesReceived: this.messagesReceived,
      reconnectAttempts: this.reconnectAttempts,
      reconnectCount: this.reconnectCount,
      pendingRequests: this.pendingRequests.size,
      queuedMessages: this.messageQueue.length,
      activeSessions: this.activeSessions.size,
    };
  }
}

// Only create instance if in web mode
export const wsService = isWeb ? new ReliableWebSocket() : null;
export default wsService;
