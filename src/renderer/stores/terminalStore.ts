/**
 * Terminal Store - Manages terminal sessions outside component lifecycle
 * This ensures sessions persist even when components remount.
 *
 * Enhanced with:
 * - Connection state tracking (connected, disconnected, reconnecting)
 * - Input queue during disconnection
 * - Session recovery support
 */
import { create } from 'zustand';
import wsService from '../services/websocket';

type ConnectionState = 'connected' | 'disconnected' | 'reconnecting';

interface TerminalSession {
  sessionId: string;
  deviceId: string;
  connected: boolean;
  connectionState: ConnectionState;
  output: string[];
  inputQueue: string[]; // Queue input during disconnection
  lastActivityAt: number;
}

interface TerminalStore {
  sessions: Map<string, TerminalSession>;
  getSession: (deviceId: string) => TerminalSession | undefined;
  createSession: (deviceId: string, sessionId: string) => void;
  closeSession: (deviceId: string) => void;
  addOutput: (deviceId: string, data: string) => void;
  clearOutput: (deviceId: string) => void;
  setConnectionState: (deviceId: string, state: ConnectionState) => void;
  queueInput: (deviceId: string, input: string) => void;
  flushInputQueue: (deviceId: string) => string[];
  updateActivity: (deviceId: string) => void;
}

export const useTerminalStore = create<TerminalStore>((set, get) => ({
  sessions: new Map(),

  getSession: (deviceId: string) => {
    return get().sessions.get(deviceId);
  },

  createSession: (deviceId: string, sessionId: string) => {
    set((state) => {
      const newSessions = new Map(state.sessions);
      newSessions.set(deviceId, {
        sessionId,
        deviceId,
        connected: true,
        connectionState: 'connected',
        output: ['Connected to remote terminal.\n'],
        inputQueue: [],
        lastActivityAt: Date.now(),
      });
      return { sessions: newSessions };
    });

    // Register session for recovery on websocket reconnect
    if (wsService) {
      wsService.registerSession(sessionId, deviceId, 'terminal');
    }
  },

  closeSession: (deviceId: string) => {
    const session = get().sessions.get(deviceId);
    if (session) {
      // Close the actual terminal
      window.api.terminal.close(session.sessionId).catch(() => {});

      // Unregister from websocket session recovery
      if (wsService) {
        wsService.unregisterSession(session.sessionId);
      }
    }
    set((state) => {
      const newSessions = new Map(state.sessions);
      newSessions.delete(deviceId);
      return { sessions: newSessions };
    });
  },

  addOutput: (deviceId: string, data: string) => {
    set((state) => {
      const session = state.sessions.get(deviceId);
      if (!session) return state;

      const newSessions = new Map(state.sessions);
      newSessions.set(deviceId, {
        ...session,
        output: [...session.output, data],
        lastActivityAt: Date.now(),
      });
      return { sessions: newSessions };
    });
  },

  clearOutput: (deviceId: string) => {
    set((state) => {
      const session = state.sessions.get(deviceId);
      if (!session) return state;

      const newSessions = new Map(state.sessions);
      newSessions.set(deviceId, {
        ...session,
        output: [],
      });
      return { sessions: newSessions };
    });
  },

  setConnectionState: (deviceId: string, connectionState: ConnectionState) => {
    set((state) => {
      const session = state.sessions.get(deviceId);
      if (!session) return state;

      const newSessions = new Map(state.sessions);
      newSessions.set(deviceId, {
        ...session,
        connected: connectionState === 'connected',
        connectionState,
      });
      return { sessions: newSessions };
    });
  },

  queueInput: (deviceId: string, input: string) => {
    set((state) => {
      const session = state.sessions.get(deviceId);
      if (!session) return state;

      // Limit queue size to prevent memory issues
      const maxQueueSize = 100;
      const newQueue = [...session.inputQueue, input].slice(-maxQueueSize);

      const newSessions = new Map(state.sessions);
      newSessions.set(deviceId, {
        ...session,
        inputQueue: newQueue,
      });
      return { sessions: newSessions };
    });
  },

  flushInputQueue: (deviceId: string) => {
    const session = get().sessions.get(deviceId);
    if (!session || session.inputQueue.length === 0) {
      return [];
    }

    const queue = [...session.inputQueue];

    set((state) => {
      const currentSession = state.sessions.get(deviceId);
      if (!currentSession) return state;

      const newSessions = new Map(state.sessions);
      newSessions.set(deviceId, {
        ...currentSession,
        inputQueue: [],
      });
      return { sessions: newSessions };
    });

    return queue;
  },

  updateActivity: (deviceId: string) => {
    set((state) => {
      const session = state.sessions.get(deviceId);
      if (!session) return state;

      const newSessions = new Map(state.sessions);
      newSessions.set(deviceId, {
        ...session,
        lastActivityAt: Date.now(),
      });
      return { sessions: newSessions };
    });
  },
}));

// Global terminal data handler - set up once
let terminalHandlerSetup = false;

export function setupTerminalHandler() {
  if (terminalHandlerSetup) return;
  terminalHandlerSetup = true;

  console.log('[TerminalStore] Setting up global terminal data handler');

  // Handle terminal output
  window.api.terminal.onData((data: string, sessionId?: string) => {
    const { sessions, addOutput } = useTerminalStore.getState();

    // If sessionId provided, only send to matching session
    if (sessionId) {
      sessions.forEach((session, deviceId) => {
        if (session.connected && session.sessionId === sessionId) {
          addOutput(deviceId, data);
        }
      });
    } else {
      // Fallback: broadcast to all connected sessions (legacy behavior)
      sessions.forEach((session, deviceId) => {
        if (session.connected) {
          addOutput(deviceId, data);
        }
      });
    }
  });

  // Handle websocket connection state changes (for web mode)
  if (wsService) {
    wsService.on('connected', (data: unknown) => {
      const { reconnected } = data as { reconnected: boolean };
      const { sessions, setConnectionState, flushInputQueue } = useTerminalStore.getState();

      console.log(`[TerminalStore] WebSocket connected (reconnected: ${reconnected})`);

      // Mark all sessions as connected and flush queued input
      sessions.forEach((session, deviceId) => {
        setConnectionState(deviceId, 'connected');

        if (reconnected) {
          // Flush any queued input
          const queue = flushInputQueue(deviceId);
          if (queue.length > 0) {
            console.log(`[TerminalStore] Flushing ${queue.length} queued inputs for ${deviceId}`);
            queue.forEach((input) => {
              window.api.terminal.write(session.sessionId, input).catch((err: Error) => {
                console.error('[TerminalStore] Failed to flush input:', err);
              });
            });
          }
        }
      });
    });

    wsService.on('disconnected', () => {
      const { sessions, setConnectionState } = useTerminalStore.getState();

      console.log('[TerminalStore] WebSocket disconnected, marking sessions as reconnecting');

      // Mark all sessions as reconnecting (not disconnected - session may recover)
      sessions.forEach((_, deviceId) => {
        setConnectionState(deviceId, 'reconnecting');
      });
    });
  }
}
