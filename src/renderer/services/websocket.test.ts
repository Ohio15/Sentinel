import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock the env module
vi.mock('./env', () => ({
  isWeb: true,
  getWsBaseUrl: () => 'ws://localhost:3000',
}));

// We need to build a mock WebSocket class before importing the module
class MockWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;

  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSING = 2;
  readonly CLOSED = 3;

  readyState = MockWebSocket.CONNECTING;
  onopen: ((ev: Event) => void) | null = null;
  onclose: ((ev: CloseEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  url: string;
  sentMessages: string[] = [];

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  send(data: string) {
    this.sentMessages.push(data);
  }

  close(code?: number, reason?: string) {
    this.readyState = MockWebSocket.CLOSED;
    // Trigger onclose if set
    if (this.onclose) {
      this.onclose(new CloseEvent('close', { code: code || 1000, reason: reason || '' }));
    }
  }

  // Simulate connection open
  simulateOpen() {
    this.readyState = MockWebSocket.OPEN;
    if (this.onopen) {
      this.onopen(new Event('open'));
    }
  }

  // Simulate incoming message
  simulateMessage(data: unknown) {
    if (this.onmessage) {
      this.onmessage(new MessageEvent('message', { data: JSON.stringify(data) }));
    }
  }

  // Simulate error
  simulateError() {
    if (this.onerror) {
      this.onerror(new Event('error'));
    }
  }

  // Track all instances
  static instances: MockWebSocket[] = [];
  static reset() {
    MockWebSocket.instances = [];
  }
}

// Set up the global WebSocket mock
(global as any).WebSocket = MockWebSocket;

// We dynamically import to get a fresh module (since wsService is only created if isWeb is true)
// Instead, let's import and work with the class directly
// The module creates a singleton: `export const wsService = isWeb ? new ReliableWebSocket() : null;`

describe('ReliableWebSocket', () => {
  let wsService: any;

  beforeEach(async () => {
    localStorage.clear();
    vi.clearAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    MockWebSocket.reset();

    // Fresh import each time to get a clean instance
    vi.resetModules();

    // Set token before importing so connect() can find it
    localStorage.setItem('token', 'test-jwt-token');

    const mod = await import('./websocket');
    wsService = mod.wsService;
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('creates a WebSocket connection on connect()', () => {
    wsService.connect();

    expect(MockWebSocket.instances.length).toBe(1);
    expect(MockWebSocket.instances[0].url).toBe('ws://localhost:3000/ws/dashboard');
  });

  it('sends auth message with token on connection open', () => {
    wsService.connect();
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    expect(ws.sentMessages.length).toBeGreaterThanOrEqual(1);
    const authMsg = JSON.parse(ws.sentMessages[0]);
    expect(authMsg.type).toBe('auth');
    expect(authMsg.token).toBe('test-jwt-token');
  });

  it('does not connect without a token in localStorage', () => {
    localStorage.removeItem('token');
    wsService.connect();

    expect(MockWebSocket.instances.length).toBe(0);
  });

  it('sets isConnected to true after successful open', () => {
    wsService.connect();
    const ws = MockWebSocket.instances[0];

    expect(wsService.isConnected).toBe(false);

    ws.simulateOpen();

    expect(wsService.isConnected).toBe(true);
    expect(wsService.state).toBe('connected');
  });

  it('routes incoming messages to registered handlers', () => {
    wsService.connect();
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    const handler = vi.fn();
    wsService.on('device_status', handler);

    ws.simulateMessage({ type: 'device_status', deviceId: 'dev-1', status: 'online' });

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler).toHaveBeenCalledWith(
      expect.objectContaining({ deviceId: 'dev-1', status: 'online' })
    );
  });

  it('handles pong/heartbeat_ack messages without routing to handlers', () => {
    wsService.connect();
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    const handler = vi.fn();
    wsService.on('pong', handler);

    ws.simulateMessage({ type: 'pong' });

    // pong is consumed internally, not routed to handlers
    expect(handler).not.toHaveBeenCalled();
  });

  it('unsubscribes handler when returned function is called', () => {
    wsService.connect();
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    const handler = vi.fn();
    const unsub = wsService.on('test_event', handler);

    ws.simulateMessage({ type: 'test_event', data: 'first' });
    expect(handler).toHaveBeenCalledTimes(1);

    unsub();

    ws.simulateMessage({ type: 'test_event', data: 'second' });
    expect(handler).toHaveBeenCalledTimes(1); // Still 1, not 2
  });

  it('queues messages when disconnected and flushes on reconnect', () => {
    wsService.connect();
    const ws1 = MockWebSocket.instances[0];

    // Connection is in 'connecting' state, messages should queue
    wsService.send('test_msg', { foo: 'bar' });

    // The message should be queued (not sent since socket not OPEN yet)
    expect(ws1.sentMessages.length).toBe(0);

    // Now open the connection
    ws1.simulateOpen();

    // Queued messages should have been flushed
    const flushed = ws1.sentMessages.find(m => {
      const parsed = JSON.parse(m);
      return parsed.type === 'test_msg';
    });
    expect(flushed).toBeTruthy();
  });

  it('schedules reconnection with exponential backoff on close', async () => {
    wsService.connect();
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    const disconnectHandler = vi.fn();
    wsService.on('disconnected', disconnectHandler);

    // Close the connection (not manual)
    ws.readyState = MockWebSocket.CLOSED;
    if (ws.onclose) {
      ws.onclose(new CloseEvent('close', { code: 1006, reason: 'abnormal' }));
    }

    expect(disconnectHandler).toHaveBeenCalled();
    expect(wsService.state).toBe('reconnecting');

    // Advance time to allow reconnect attempt (base delay is 1000ms)
    await vi.advanceTimersByTimeAsync(2000);

    // Should have created a new WebSocket instance for reconnect
    expect(MockWebSocket.instances.length).toBeGreaterThan(1);
  });

  it('does not auto-reconnect after manual disconnect()', async () => {
    wsService.connect();
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    const instancesBefore = MockWebSocket.instances.length;

    wsService.disconnect();

    // Advance timers to check no reconnect happens
    await vi.advanceTimersByTimeAsync(10000);

    expect(MockWebSocket.instances.length).toBe(instancesBefore);
  });

  it('starts heartbeat on connection and sends ping messages', async () => {
    wsService.connect();
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    // Heartbeat period is 30s, advance past it
    await vi.advanceTimersByTimeAsync(31000);

    const pingMsg = ws.sentMessages.find(m => {
      const parsed = JSON.parse(m);
      return parsed.type === 'ping';
    });
    expect(pingMsg).toBeTruthy();
  });

  it('emits connected event with reconnected flag', () => {
    wsService.connect();
    const ws = MockWebSocket.instances[0];

    const connHandler = vi.fn();
    wsService.on('connected', connHandler);

    ws.simulateOpen();

    expect(connHandler).toHaveBeenCalledWith(
      expect.objectContaining({ reconnected: false })
    );
  });

  it('sends messages as JSON with type field', () => {
    wsService.connect();
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    wsService.send('start_terminal', { deviceId: 'dev-1', sessionId: 'sess-1' });

    const found = ws.sentMessages.find(m => {
      const parsed = JSON.parse(m);
      return parsed.type === 'start_terminal';
    });
    expect(found).toBeTruthy();
    const parsed = JSON.parse(found!);
    expect(parsed.type).toBe('start_terminal');
    expect(parsed.payload).toEqual(expect.objectContaining({ deviceId: 'dev-1' }));
  });

  it('tracks session for recovery and resubscribes on reconnect', () => {
    wsService.connect();
    const ws1 = MockWebSocket.instances[0];
    ws1.simulateOpen();

    // Register a session
    wsService.registerSession('sess-1', 'dev-1', 'terminal');

    expect(wsService.metrics.activeSessions).toBe(1);

    // Simulate disconnect and reconnect
    ws1.readyState = MockWebSocket.CLOSED;
    if (ws1.onclose) {
      ws1.onclose(new CloseEvent('close', { code: 1006 }));
    }

    // After reconnect (advance timer past backoff)
    vi.advanceTimersByTime(2000);

    const ws2 = MockWebSocket.instances[MockWebSocket.instances.length - 1];
    ws2.simulateOpen();

    // Should have sent a subscribe_session message on reconnect
    const subscribeMsgs = ws2.sentMessages.filter(m => {
      const parsed = JSON.parse(m);
      return parsed.type === 'subscribe_session';
    });
    expect(subscribeMsgs.length).toBe(1);

    // Clean up
    wsService.unregisterSession('sess-1');
    expect(wsService.metrics.activeSessions).toBe(0);
  });

  it('reports metrics correctly', () => {
    wsService.connect();
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    const metrics = wsService.metrics;
    expect(metrics).toEqual(expect.objectContaining({
      state: 'connected',
      messagesSent: expect.any(Number),
      messagesReceived: expect.any(Number),
      reconnectAttempts: 0,
      pendingRequests: 0,
      queuedMessages: 0,
      activeSessions: 0,
    }));
  });

  it('emits to wildcard handlers with full message', () => {
    wsService.connect();
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    const wildcardHandler = vi.fn();
    wsService.on('*', wildcardHandler);

    ws.simulateMessage({ type: 'some_event', payload: { data: 'test' } });

    expect(wildcardHandler).toHaveBeenCalledTimes(1);
    expect(wildcardHandler).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'some_event' })
    );
  });
});
