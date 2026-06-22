import React, { useEffect, useRef, useState, useCallback, memo } from 'react';
import { useTerminalStore, setupTerminalHandler } from '../stores/terminalStore';
import { terminal as terminalService } from '../services';

interface TerminalProps {
  deviceId: string;
  isOnline: boolean;
}

const MAX_INPUT_LENGTH = 4096; // 4KB max per command

export const Terminal = memo(function Terminal({ deviceId, isOnline }: TerminalProps) {
  const [input, setInput] = useState('');
  const [connecting, setConnecting] = useState(false);
  const outputRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  
  // Use store for session persistence
  const session = useTerminalStore((state) => state.sessions.get(deviceId));
  const createSession = useTerminalStore((state) => state.createSession);
  const closeSession = useTerminalStore((state) => state.closeSession);
  const addOutput = useTerminalStore((state) => state.addOutput);

  const connected = session?.connected ?? false;
  const output = session?.output ?? [];
  const sessionId = session?.sessionId;

  // Setup global terminal handler once
  useEffect(() => {
    setupTerminalHandler();
  }, []);

  // Auto-scroll output
  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [output]);

  const handleConnect = useCallback(async () => {
    if (!isOnline || connecting) return;

    setConnecting(true);
    try {
      console.log('[Terminal] Starting session for device:', deviceId);
      const result = await terminalService.start(deviceId);
      console.log('[Terminal] Session started:', result.sessionId);
      createSession(deviceId, result.sessionId);
      inputRef.current?.focus();
    } catch (error: unknown) {
      console.error('[Terminal] Failed to start:', error);
      const errMsg = error instanceof Error ? error.message : 'Unknown error';
      addOutput(deviceId, 'Failed to connect: ' + errMsg + '\n');
    } finally {
      setConnecting(false);
    }
  }, [deviceId, isOnline, connecting, createSession, addOutput]);

  const handleDisconnect = useCallback(() => {
    if (session) {
      closeSession(deviceId);
    }
  }, [deviceId, session, closeSession]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!sessionId || !input.trim()) return;

    // Validate input length
    if (input.length > MAX_INPUT_LENGTH) {
      addOutput(deviceId, `Error: Command too long (${input.length} chars, max ${MAX_INPUT_LENGTH})\n`);
      return;
    }

    addOutput(deviceId, '$ ' + input + '\n');
    await terminalService.send(sessionId, input + '\n');
    setInput('');
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Tab') {
      e.preventDefault();
    }
  };

  if (!isOnline) {
    return (
      <div className="h-96 flex items-center justify-center bg-gray-900">
        <p className="text-gray-400">Device is offline. Terminal is not available.</p>
      </div>
    );
  }

  const statusClass = 'w-3 h-3 rounded-full ' + (connected ? 'bg-green-500' : 'bg-red-500');

  return (
    <div className="flex flex-col h-96">
      <div className="flex items-center justify-between px-4 py-2 bg-gray-800 border-b border-gray-700">
        <div className="flex items-center gap-2">
          <div className={statusClass} />
          <span className="text-sm text-gray-300">
            {connected ? 'Connected' : 'Disconnected'}
          </span>
        </div>
        {connected ? (
          <button
            onClick={handleDisconnect}
            className="px-3 py-1 text-sm bg-red-600 text-white rounded hover:bg-red-700 transition-colors"
          >
            Disconnect
          </button>
        ) : (
          <button
            onClick={() => { void handleConnect(); }}
            disabled={connecting}
            className="px-3 py-1 text-sm bg-green-600 text-white rounded hover:bg-green-700 transition-colors disabled:opacity-50"
          >
            {connecting ? 'Connecting...' : 'Connect'}
          </button>
        )}
      </div>

      <div
        ref={outputRef}
        className="flex-1 overflow-auto p-4 bg-gray-900 font-mono text-sm text-gray-100"
        onClick={() => inputRef.current?.focus()}
      >
        {output.map((line, i) => (
          <pre key={i} className="whitespace-pre-wrap">{line}</pre>
        ))}
        {connected && (
          <form onSubmit={(e) => { void handleSubmit(e); }} className="flex items-center">
            <span className="text-green-400">$</span>
            <input
              ref={inputRef}
              type="text"
              value={input}
              onChange={e => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              className="flex-1 ml-2 bg-transparent text-gray-100 outline-none"
              maxLength={MAX_INPUT_LENGTH + 1}
              autoFocus
            />
            {input.length > MAX_INPUT_LENGTH * 0.9 && (
              <span className={`text-xs ml-2 whitespace-nowrap ${input.length > MAX_INPUT_LENGTH ? 'text-red-400 font-bold' : 'text-yellow-400'}`}>
                {input.length}/{MAX_INPUT_LENGTH}
              </span>
            )}
          </form>
        )}
      </div>
    </div>
  );
});
