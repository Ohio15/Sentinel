import { useEffect, useRef, useCallback, useState } from 'react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import '@xterm/xterm/css/xterm.css';
import { wsService } from '@/services/websocket';
import { Button } from '@/components/ui';
import { X, Maximize2, Minimize2 } from 'lucide-react';

interface TerminalProps {
  deviceId: string;
  agentId: string;
  onClose: () => void;
}

export function Terminal({ deviceId, agentId, onClose }: TerminalProps) {
  const terminalRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const sessionIdRef = useRef<string>('');
  const [isConnected, setIsConnected] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);

  const handleTerminalOutput = useCallback((data: unknown) => {
    const payload = data as { sessionId?: string; data?: string };
    if (payload.sessionId === sessionIdRef.current && payload.data && xtermRef.current) {
      xtermRef.current.write(payload.data);
    }
  }, []);

  const startTerminalSession = useCallback(() => {
    sessionIdRef.current = `term-${deviceId}-${Date.now()}`;

    const cols = xtermRef.current?.cols || 80;
    const rows = xtermRef.current?.rows || 24;

    wsService.send('start_terminal', {
      deviceId,
      agentId,
      sessionId: sessionIdRef.current,
      cols,
      rows,
    });

    setIsConnected(true);
  }, [deviceId, agentId]);

  const closeTerminalSession = useCallback(() => {
    if (sessionIdRef.current) {
      wsService.send('close_terminal', {
        deviceId,
        agentId,
        sessionId: sessionIdRef.current,
      });
    }
    setIsConnected(false);
  }, [deviceId, agentId]);

  useEffect(() => {
    if (!terminalRef.current) return;

    // Create terminal instance
    const xterm = new XTerm({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'Consolas, "Courier New", monospace',
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
        cursor: '#d4d4d4',
        cursorAccent: '#1e1e1e',
        selectionBackground: '#264f78',
        black: '#000000',
        red: '#cd3131',
        green: '#0dbc79',
        yellow: '#e5e510',
        blue: '#2472c8',
        magenta: '#bc3fbc',
        cyan: '#11a8cd',
        white: '#e5e5e5',
        brightBlack: '#666666',
        brightRed: '#f14c4c',
        brightGreen: '#23d18b',
        brightYellow: '#f5f543',
        brightBlue: '#3b8eea',
        brightMagenta: '#d670d6',
        brightCyan: '#29b8db',
        brightWhite: '#ffffff',
      },
    });

    const fitAddon = new FitAddon();
    const webLinksAddon = new WebLinksAddon();

    xterm.loadAddon(fitAddon);
    xterm.loadAddon(webLinksAddon);

    xterm.open(terminalRef.current);
    fitAddon.fit();

    xtermRef.current = xterm;
    fitAddonRef.current = fitAddon;

    // Handle user input
    xterm.onData((data) => {
      if (sessionIdRef.current && isConnected) {
        wsService.send('terminal_input', {
          deviceId,
          agentId,
          sessionId: sessionIdRef.current,
          data,
        });
      }
    });

    // Handle resize
    const handleResize = () => {
      if (fitAddonRef.current) {
        fitAddonRef.current.fit();
        if (sessionIdRef.current && xtermRef.current) {
          wsService.send('terminal_resize', {
            deviceId,
            agentId,
            sessionId: sessionIdRef.current,
            cols: xtermRef.current.cols,
            rows: xtermRef.current.rows,
          });
        }
      }
    };

    window.addEventListener('resize', handleResize);

    // Subscribe to terminal output
    const unsubscribe = wsService.on('terminal_output', handleTerminalOutput);

    // Start terminal session
    if (wsService.isConnected) {
      startTerminalSession();
    } else {
      wsService.connect();
      const connectUnsub = wsService.on('connected', () => {
        startTerminalSession();
        connectUnsub();
      });
    }

    xterm.write('\x1b[1;32mConnecting to terminal...\x1b[0m\r\n');

    return () => {
      window.removeEventListener('resize', handleResize);
      unsubscribe();
      closeTerminalSession();
      xterm.dispose();
    };
  }, [deviceId, agentId, handleTerminalOutput, startTerminalSession, closeTerminalSession, isConnected]);

  // Re-fit terminal when fullscreen changes
  useEffect(() => {
    setTimeout(() => {
      fitAddonRef.current?.fit();
    }, 100);
  }, [isFullscreen]);

  const handleClose = () => {
    closeTerminalSession();
    onClose();
  };

  return (
    <div
      className={`flex flex-col bg-[#1e1e1e] rounded-lg overflow-hidden ${
        isFullscreen
          ? 'fixed inset-0 z-50'
          : 'h-[400px]'
      }`}
    >
      {/* Terminal header */}
      <div className="flex items-center justify-between px-4 py-2 bg-[#323232] border-b border-[#464646]">
        <div className="flex items-center gap-2">
          <div className={`w-3 h-3 rounded-full ${isConnected ? 'bg-green-500' : 'bg-red-500'}`} />
          <span className="text-sm text-gray-300">
            Terminal - {isConnected ? 'Connected' : 'Disconnected'}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setIsFullscreen(!isFullscreen)}
            className="p-1 text-gray-400 hover:text-white transition-colors"
          >
            {isFullscreen ? (
              <Minimize2 className="w-4 h-4" />
            ) : (
              <Maximize2 className="w-4 h-4" />
            )}
          </button>
          <button
            onClick={handleClose}
            className="p-1 text-gray-400 hover:text-white transition-colors"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Terminal content */}
      <div ref={terminalRef} className="flex-1 p-2" />

      {/* Reconnect button if disconnected */}
      {!isConnected && (
        <div className="absolute inset-0 flex items-center justify-center bg-black/50">
          <Button onClick={startTerminalSession}>
            Reconnect
          </Button>
        </div>
      )}
    </div>
  );
}
