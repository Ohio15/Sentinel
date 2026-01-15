/**
 * Terminal Store - Manages terminal sessions outside component lifecycle
 * This ensures sessions persist even when components remount
 */
import { create } from 'zustand';

interface TerminalSession {
  sessionId: string;
  deviceId: string;
  connected: boolean;
  output: string[];
}

interface TerminalStore {
  sessions: Map<string, TerminalSession>;
  getSession: (deviceId: string) => TerminalSession | undefined;
  createSession: (deviceId: string, sessionId: string) => void;
  closeSession: (deviceId: string) => void;
  addOutput: (deviceId: string, data: string) => void;
  clearOutput: (deviceId: string) => void;
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
        output: ['Connected to remote terminal.\n'],
      });
      return { sessions: newSessions };
    });
  },

  closeSession: (deviceId: string) => {
    const session = get().sessions.get(deviceId);
    if (session) {
      // Close the actual terminal
      window.api.terminal.close(session.sessionId).catch(() => {});
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
}));

// Global terminal data handler - set up once
let terminalHandlerSetup = false;

export function setupTerminalHandler() {
  if (terminalHandlerSetup) return;
  terminalHandlerSetup = true;

  console.log('[TerminalStore] Setting up global terminal data handler');
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
}
