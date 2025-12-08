import { useEffect, useRef, useCallback, useState } from 'react';
import { wsService } from '@/services/websocket';
import { Button } from '@/components/ui';
import { X, Maximize2, Minimize2, Monitor, MousePointer, Keyboard } from 'lucide-react';

interface RemoteDesktopProps {
  deviceId: string;
  agentId: string;
  onClose: () => void;
}

interface RemoteFrame {
  sessionId: string;
  data: string; // base64 encoded image
  width: number;
  height: number;
  timestamp: number;
}

export function RemoteDesktop({ deviceId, agentId, onClose }: RemoteDesktopProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const sessionIdRef = useRef<string>('');
  const [isConnected, setIsConnected] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [fps, setFps] = useState(0);
  const [resolution, setResolution] = useState({ width: 0, height: 0 });
  const frameCountRef = useRef(0);
  const lastFpsUpdateRef = useRef(Date.now());

  const handleRemoteFrame = useCallback((data: unknown) => {
    const frame = data as RemoteFrame;
    if (frame.sessionId !== sessionIdRef.current) return;

    const canvas = canvasRef.current;
    if (!canvas) return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    // Create image from base64 data
    const img = new Image();
    img.onload = () => {
      // Resize canvas if needed
      if (canvas.width !== frame.width || canvas.height !== frame.height) {
        canvas.width = frame.width;
        canvas.height = frame.height;
        setResolution({ width: frame.width, height: frame.height });
      }

      ctx.drawImage(img, 0, 0);

      // Update FPS counter
      frameCountRef.current++;
      const now = Date.now();
      if (now - lastFpsUpdateRef.current >= 1000) {
        setFps(frameCountRef.current);
        frameCountRef.current = 0;
        lastFpsUpdateRef.current = now;
      }
    };
    img.src = `data:image/jpeg;base64,${frame.data}`;
  }, []);

  const startRemoteSession = useCallback(() => {
    sessionIdRef.current = `remote-${deviceId}-${Date.now()}`;

    wsService.send('start_remote', {
      deviceId,
      agentId,
      sessionId: sessionIdRef.current,
    });

    setIsConnected(true);
  }, [deviceId, agentId]);

  const stopRemoteSession = useCallback(() => {
    if (sessionIdRef.current) {
      wsService.send('stop_remote', {
        deviceId,
        agentId,
        sessionId: sessionIdRef.current,
      });
    }
    setIsConnected(false);
  }, [deviceId, agentId]);

  // Mouse event handlers
  const getCanvasCoordinates = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    const canvas = canvasRef.current;
    if (!canvas) return { x: 0, y: 0 };

    const rect = canvas.getBoundingClientRect();
    const scaleX = canvas.width / rect.width;
    const scaleY = canvas.height / rect.height;

    return {
      x: Math.round((e.clientX - rect.left) * scaleX),
      y: Math.round((e.clientY - rect.top) * scaleY),
    };
  }, []);

  const sendMouseEvent = useCallback((type: string, e: React.MouseEvent<HTMLCanvasElement>, button?: number) => {
    if (!isConnected || !sessionIdRef.current) return;

    const { x, y } = getCanvasCoordinates(e);

    wsService.send('remote_input', {
      deviceId,
      agentId,
      sessionId: sessionIdRef.current,
      inputType: 'mouse',
      data: {
        type,
        x,
        y,
        button: button ?? e.button,
      },
    });
  }, [deviceId, agentId, isConnected, getCanvasCoordinates]);

  const handleMouseMove = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    sendMouseEvent('move', e);
  }, [sendMouseEvent]);

  const handleMouseDown = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    e.preventDefault();
    sendMouseEvent('down', e);
  }, [sendMouseEvent]);

  const handleMouseUp = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    e.preventDefault();
    sendMouseEvent('up', e);
  }, [sendMouseEvent]);

  const handleContextMenu = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    e.preventDefault();
  }, []);

  const handleWheel = useCallback((e: React.WheelEvent<HTMLCanvasElement>) => {
    if (!isConnected || !sessionIdRef.current) return;

    const { x, y } = getCanvasCoordinates(e);

    wsService.send('remote_input', {
      deviceId,
      agentId,
      sessionId: sessionIdRef.current,
      inputType: 'mouse',
      data: {
        type: 'wheel',
        x,
        y,
        deltaX: e.deltaX,
        deltaY: e.deltaY,
      },
    });
  }, [deviceId, agentId, isConnected, getCanvasCoordinates]);

  // Keyboard event handlers
  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (!isConnected || !sessionIdRef.current) return;

    e.preventDefault();

    wsService.send('remote_input', {
      deviceId,
      agentId,
      sessionId: sessionIdRef.current,
      inputType: 'keyboard',
      data: {
        type: 'down',
        key: e.key,
        code: e.code,
        ctrlKey: e.ctrlKey,
        shiftKey: e.shiftKey,
        altKey: e.altKey,
        metaKey: e.metaKey,
      },
    });
  }, [deviceId, agentId, isConnected]);

  const handleKeyUp = useCallback((e: KeyboardEvent) => {
    if (!isConnected || !sessionIdRef.current) return;

    e.preventDefault();

    wsService.send('remote_input', {
      deviceId,
      agentId,
      sessionId: sessionIdRef.current,
      inputType: 'keyboard',
      data: {
        type: 'up',
        key: e.key,
        code: e.code,
      },
    });
  }, [deviceId, agentId, isConnected]);

  useEffect(() => {
    // Subscribe to remote frames
    const unsubscribe = wsService.on('remote_frame', handleRemoteFrame);

    // Start remote session
    if (wsService.isConnected) {
      startRemoteSession();
    } else {
      wsService.connect();
      const connectUnsub = wsService.on('connected', () => {
        startRemoteSession();
        connectUnsub();
      });
    }

    // Add keyboard listeners
    window.addEventListener('keydown', handleKeyDown);
    window.addEventListener('keyup', handleKeyUp);

    return () => {
      unsubscribe();
      stopRemoteSession();
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('keyup', handleKeyUp);
    };
  }, [handleRemoteFrame, startRemoteSession, stopRemoteSession, handleKeyDown, handleKeyUp]);

  const handleClose = () => {
    stopRemoteSession();
    onClose();
  };

  const toggleFullscreen = () => {
    if (!containerRef.current) return;

    if (!isFullscreen) {
      containerRef.current.requestFullscreen?.();
    } else {
      document.exitFullscreen?.();
    }
    setIsFullscreen(!isFullscreen);
  };

  return (
    <div
      ref={containerRef}
      className={`flex flex-col bg-gray-900 rounded-lg overflow-hidden ${
        isFullscreen ? 'fixed inset-0 z-50' : 'h-[500px]'
      }`}
    >
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2 bg-gray-800 border-b border-gray-700">
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <Monitor className="w-4 h-4 text-primary" />
            <span className="text-sm font-medium text-white">Remote Desktop</span>
          </div>
          <div className={`w-2 h-2 rounded-full ${isConnected ? 'bg-green-500' : 'bg-red-500'}`} />
          {isConnected && (
            <>
              <span className="text-xs text-gray-400">
                {resolution.width}×{resolution.height}
              </span>
              <span className="text-xs text-gray-400">{fps} FPS</span>
            </>
          )}
        </div>
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-1 text-xs text-gray-400">
            <MousePointer className="w-3 h-3" />
            <Keyboard className="w-3 h-3" />
          </div>
          <button
            onClick={toggleFullscreen}
            className="p-1 text-gray-400 hover:text-white transition-colors"
            title={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'}
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
            title="Close"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Canvas container */}
      <div className="flex-1 flex items-center justify-center bg-black overflow-hidden">
        {!isConnected ? (
          <div className="flex flex-col items-center gap-4 text-gray-400">
            <Monitor className="w-12 h-12" />
            <p>Connecting to remote desktop...</p>
            <Button onClick={startRemoteSession}>Reconnect</Button>
          </div>
        ) : (
          <canvas
            ref={canvasRef}
            className="max-w-full max-h-full object-contain cursor-crosshair"
            onMouseMove={handleMouseMove}
            onMouseDown={handleMouseDown}
            onMouseUp={handleMouseUp}
            onContextMenu={handleContextMenu}
            onWheel={handleWheel}
            tabIndex={0}
          />
        )}
      </div>

      {/* Status bar */}
      <div className="flex items-center justify-between px-4 py-1 bg-gray-800 border-t border-gray-700 text-xs text-gray-400">
        <span>
          {isConnected ? 'Connected - Click canvas to enable keyboard input' : 'Disconnected'}
        </span>
        <span>Session: {sessionIdRef.current || 'None'}</span>
      </div>
    </div>
  );
}
