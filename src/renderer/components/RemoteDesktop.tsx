import React, { useState, useRef, useEffect, useCallback } from 'react';

interface RemoteDesktopProps {
  deviceId: string;
  isOnline: boolean;
}

export function RemoteDesktop({ deviceId, isOnline }: RemoteDesktopProps) {
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [connecting, setConnecting] = useState(false);
  const [connected, setConnected] = useState(false);
  const [quality, setQuality] = useState<'low' | 'medium' | 'high'>('medium');
  const [fullscreen, setFullscreen] = useState(false);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const unsub = window.api.remote.onFrame((frame) => {
      if (frame.sessionId === sessionId && canvasRef.current) {
        drawFrame(frame.data, frame.width, frame.height);
      }
    });

    return () => {
      unsub();
      if (sessionId) {
        window.api.remote.stopSession(sessionId);
      }
    };
  }, [sessionId]);

  const drawFrame = (data: ArrayBuffer, width: number, height: number) => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    // Set canvas size to match frame
    if (canvas.width !== width || canvas.height !== height) {
      canvas.width = width;
      canvas.height = height;
    }

    // Create image from frame data
    const imageData = new ImageData(new Uint8ClampedArray(data), width, height);
    ctx.putImageData(imageData, 0, 0);
  };

  const handleConnect = async () => {
    if (!isOnline) return;

    setConnecting(true);
    try {
      const result = await window.api.remote.startSession(deviceId);
      setSessionId(result.sessionId);
      setConnected(true);
    } catch (error: any) {
      alert(`Failed to connect: ${error.message}`);
    } finally {
      setConnecting(false);
    }
  };

  const handleDisconnect = async () => {
    if (sessionId) {
      await window.api.remote.stopSession(sessionId);
      setSessionId(null);
      setConnected(false);
    }
  };

  const handleMouseEvent = useCallback((e: React.MouseEvent, eventType: string) => {
    if (!sessionId || !canvasRef.current) return;

    const canvas = canvasRef.current;
    const rect = canvas.getBoundingClientRect();
    const scaleX = canvas.width / rect.width;
    const scaleY = canvas.height / rect.height;

    const x = Math.round((e.clientX - rect.left) * scaleX);
    const y = Math.round((e.clientY - rect.top) * scaleY);

    window.api.remote.sendInput(sessionId, {
      type: 'mouse',
      event: eventType,
      x,
      y,
      button: e.button,
    });
  }, [sessionId]);

  const handleKeyEvent = useCallback((e: React.KeyboardEvent, eventType: string) => {
    if (!sessionId) return;

    e.preventDefault();

    const modifiers: string[] = [];
    if (e.ctrlKey) modifiers.push('ctrl');
    if (e.altKey) modifiers.push('alt');
    if (e.shiftKey) modifiers.push('shift');
    if (e.metaKey) modifiers.push('meta');

    window.api.remote.sendInput(sessionId, {
      type: 'keyboard',
      event: eventType,
      key: e.key,
      modifiers,
    });
  }, [sessionId]);

  const toggleFullscreen = () => {
    if (!containerRef.current) return;

    if (!fullscreen) {
      containerRef.current.requestFullscreen?.();
    } else {
      document.exitFullscreen?.();
    }
    setFullscreen(!fullscreen);
  };

  if (!isOnline) {
    return (
      <div className="h-96 flex items-center justify-center">
        <p className="text-text-secondary">Device is offline. Remote desktop is not available.</p>
      </div>
    );
  }

  return (
    <div ref={containerRef} className="flex flex-col h-96">
      {/* Toolbar */}
      <div className="flex items-center justify-between px-4 py-2 bg-gray-800">
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <div className={`w-3 h-3 rounded-full ${connected ? 'bg-green-500' : 'bg-red-500'}`} />
            <span className="text-sm text-gray-300">
              {connected ? 'Connected' : 'Disconnected'}
            </span>
          </div>

          {connected && (
            <>
              <div className="h-4 w-px bg-gray-600" />
              <select
                value={quality}
                onChange={e => setQuality(e.target.value as any)}
                className="px-2 py-1 text-sm bg-gray-700 text-gray-200 rounded border border-gray-600"
              >
                <option value="low">Low Quality</option>
                <option value="medium">Medium Quality</option>
                <option value="high">High Quality</option>
              </select>
            </>
          )}
        </div>

        <div className="flex items-center gap-2">
          {connected && (
            <button
              onClick={toggleFullscreen}
              className="p-1 text-gray-400 hover:text-gray-200 transition-colors"
              title="Toggle fullscreen"
            >
              <FullscreenIcon className="w-5 h-5" />
            </button>
          )}

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
      </div>

      {/* Remote View */}
      <div className="flex-1 bg-black flex items-center justify-center overflow-hidden">
        {connected ? (
          <canvas
            ref={canvasRef}
            className="max-w-full max-h-full object-contain cursor-none"
            tabIndex={0}
            onMouseDown={e => handleMouseEvent(e, 'mousedown')}
            onMouseUp={e => handleMouseEvent(e, 'mouseup')}
            onMouseMove={e => handleMouseEvent(e, 'mousemove')}
            onKeyDown={e => handleKeyEvent(e, 'keydown')}
            onKeyUp={e => handleKeyEvent(e, 'keyup')}
            onContextMenu={e => e.preventDefault()}
          />
        ) : (
          <div className="text-center">
            <RemoteIcon className="w-16 h-16 text-gray-600 mx-auto mb-4" />
            <p className="text-gray-400">Click Connect to start a remote desktop session</p>
            <p className="text-gray-500 text-sm mt-2">
              You will be able to see and control the remote device
            </p>
          </div>
        )}
      </div>

      {/* Status Bar */}
      {connected && (
        <div className="px-4 py-2 bg-gray-800 border-t border-gray-700 text-sm text-gray-400">
          <span>Remote Desktop Session Active</span>
          <span className="mx-2">|</span>
          <span>Quality: {quality}</span>
          <span className="mx-2">|</span>
          <span>Click in the view to enable keyboard input</span>
        </div>
      )}
    </div>
  );
}

// Icons
function FullscreenIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5l-5-5m5 5v-4m0 4h-4" />
    </svg>
  );
}

function RemoteIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
    </svg>
  );
}
