/**
 * WebSocket Service for Web mode
 * In Electron mode, real-time updates come through IPC from the main process
 */
import { isWeb, getWsBaseUrl } from './env';

type MessageHandler = (data: unknown) => void;

interface WebSocketMessage {
  type: string;
  payload?: unknown;
}

class WebSocketService {
  private ws: WebSocket | null = null;
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 5;
  private reconnectDelay = 1000;
  private handlers: Map<string, Set<MessageHandler>> = new Map();
  private isConnecting = false;

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
    const wsUrl = `${getWsBaseUrl()}/ws/dashboard?token=${token}`;
    console.log('[WebSocket] Connecting to:', wsUrl);

    try {
      this.ws = new WebSocket(wsUrl);

      this.ws.onopen = () => {
        console.log('[WebSocket] Connected');
        this.isConnecting = false;
        this.reconnectAttempts = 0;
        this.emit('connected', null);
      };

      this.ws.onclose = (event) => {
        console.log('[WebSocket] Disconnected', event.code, event.reason);
        this.isConnecting = false;
        this.emit('disconnected', null);
        this.attemptReconnect();
      };

      this.ws.onerror = (error) => {
        console.error('[WebSocket] Error:', error);
        this.isConnecting = false;
      };

      this.ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data);
          // If message has payload field (and it's not null), use it; otherwise pass the whole message (minus type)
          const data = (message.payload !== undefined && message.payload !== null) ? message.payload : (() => {
            const { type, ...rest } = message;
            return rest;
          })();
          this.emit(message.type, data);
        } catch (err) {
          console.error('[WebSocket] Failed to parse message:', err);
        }
      };
    } catch (err) {
      console.error('[WebSocket] Failed to create connection:', err);
      this.isConnecting = false;
      this.attemptReconnect();
    }
  }

  disconnect() {
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    this.reconnectAttempts = this.maxReconnectAttempts;
  }

  private attemptReconnect() {
    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      console.log('[WebSocket] Max reconnection attempts reached');
      return;
    }

    this.reconnectAttempts++;
    const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1);
    console.log(`[WebSocket] Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`);

    setTimeout(() => {
      this.connect();
    }, delay);
  }

  send(type: string, payload?: unknown) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      // Extract requestId to root level for server compatibility
      const message: Record<string, unknown> = { type };
      if (payload && typeof payload === 'object') {
        const p = payload as Record<string, unknown>;
        if (p.requestId) {
          message.requestId = p.requestId;
        }
        message.payload = payload;
      } else {
        message.payload = payload;
      }
      const msgStr = JSON.stringify(message);
      console.log('[WebSocket] Sending message:', type, msgStr.length, 'bytes');
      this.ws.send(msgStr);
    } else {
      console.warn('[WebSocket] Not connected, cannot send message. readyState:', this.ws?.readyState);
    }
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

  private emit(type: string, data: unknown) {
    this.handlers.get(type)?.forEach((handler) => {
      try {
        handler(data);
      } catch (err) {
        console.error(`[WebSocket] Error in handler for ${type}:`, err);
      }
    });

    // Emit to wildcard handlers
    this.handlers.get('*')?.forEach((handler) => {
      try {
        handler({ type, data });
      } catch (err) {
        console.error('[WebSocket] Error in wildcard handler:', err);
      }
    });
  }

  get isConnected() {
    return this.ws?.readyState === WebSocket.OPEN;
  }
}

// Only create instance if in web mode
export const wsService = isWeb ? new WebSocketService() : null;
export default wsService;
