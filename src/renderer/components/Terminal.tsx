import React, { useEffect, useRef, useState } from 'react';

interface TerminalProps {
  deviceId: string;
  isOnline: boolean;
}

export function Terminal({ deviceId, isOnline }: TerminalProps) {
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [output, setOutput] = useState<string[]>([]);
  const [input, setInput] = useState('');
  const [connecting, setConnecting] = useState(false);
  const [connected, setConnected] = useState(false);
  const outputRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    // Subscribe to terminal data
    const unsub = window.api.terminal.onData((data: string) => {
      setOutput(prev => [...prev, data]);
    });

    return () => {
      unsub();
      if (sessionId) {
        window.api.terminal.close(sessionId);
      }
    };
  }, [sessionId]);

  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [output]);

  const handleConnect = async () => {
    if (!isOnline) return;

    setConnecting(true);
    try {
      const result = await window.api.terminal.start(deviceId);
      setSessionId(result.sessionId);
      setConnected(true);
      setOutput(['Connected to remote terminal.\n']);
      inputRef.current?.focus();
    } catch (error: any) {
      setOutput([`Failed to connect: ${error.message}\n`]);
    } finally {
      setConnecting(false);
    }
  };

  const handleDisconnect = async () => {
    if (sessionId) {
      await window.api.terminal.close(sessionId);
      setSessionId(null);
      setConnected(false);
      setOutput(prev => [...prev, '\nDisconnected.\n']);
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!sessionId || !input.trim()) return;

    setOutput(prev => [...prev, `$ ${input}\n`]);
    await window.api.terminal.send(sessionId, input + '\n');
    setInput('');
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Tab') {
      e.preventDefault();
      // Could implement tab completion here
    }
  };

  if (!isOnline) {
    return (
      <div className="h-96 flex items-center justify-center bg-gray-900">
        <p className="text-gray-400">Device is offline. Terminal is not available.</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-96">
      {/* Terminal Header */}
      <div className="flex items-center justify-between px-4 py-2 bg-gray-800 border-b border-gray-700">
        <div className="flex items-center gap-2">
          <div className={`w-3 h-3 rounded-full ${connected ? 'bg-green-500' : 'bg-red-500'}`} />
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
            onClick={handleConnect}
            disabled={connecting}
            className="px-3 py-1 text-sm bg-green-600 text-white rounded hover:bg-green-700 transition-colors disabled:opacity-50"
          >
            {connecting ? 'Connecting...' : 'Connect'}
          </button>
        )}
      </div>

      {/* Terminal Output */}
      <div
        ref={outputRef}
        className="flex-1 overflow-auto p-4 bg-gray-900 font-mono text-sm text-gray-100"
        onClick={() => inputRef.current?.focus()}
      >
        {output.map((line, i) => (
          <pre key={i} className="whitespace-pre-wrap">{line}</pre>
        ))}
        {connected && (
          <form onSubmit={handleSubmit} className="flex items-center">
            <span className="text-green-400">$</span>
            <input
              ref={inputRef}
              type="text"
              value={input}
              onChange={e => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              className="flex-1 ml-2 bg-transparent text-gray-100 outline-none"
              autoFocus
            />
          </form>
        )}
      </div>
    </div>
  );
}
