import { useEffect, useRef, useCallback, useState, memo } from 'react';
import { WebRTCService, InputEvent, WebRTCStats } from '../services/webrtc';
import { wsService } from '../services/websocket';
import { useDeviceStore } from '../stores/deviceStore';
import { Maximize2, Minimize2, Monitor, MousePointer, Keyboard, Wifi, WifiOff } from 'lucide-react';

interface RemoteDesktopProps {
  deviceId: string;
  isOnline: boolean;
  isActive?: boolean;
}

export const RemoteDesktop = memo(function RemoteDesktop({ deviceId, isOnline, isActive = true }: RemoteDesktopProps) {
  const device = useDeviceStore((state) => state.devices.find(d => d.id === deviceId));
  const agentId = device?.agentId || deviceId;
  const videoRef = useRef<HTMLVideoElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const webrtcRef = useRef<WebRTCService | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const [isConnecting, setIsConnecting] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [stats, setStats] = useState<WebRTCStats | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [connectionState, setConnectionState] = useState<string>('disconnected');

  // Calculate coordinates relative to video
  const getVideoCoordinates = useCallback((e: React.MouseEvent<HTMLVideoElement>): { x: number; y: number } | null => {
    const video = videoRef.current;
    if (!video || !video.videoWidth || !video.videoHeight) return null;

    const rect = video.getBoundingClientRect();

    // Video maintains aspect ratio, may have letterboxing
    const videoAspect = video.videoWidth / video.videoHeight;
    const containerAspect = rect.width / rect.height;

    let displayWidth: number, displayHeight: number, offsetX = 0, offsetY = 0;

    if (containerAspect > videoAspect) {
      // Letterbox on sides
      displayHeight = rect.height;
      displayWidth = rect.height * videoAspect;
      offsetX = (rect.width - displayWidth) / 2;
    } else {
      // Letterbox on top/bottom
      displayWidth = rect.width;
      displayHeight = rect.width / videoAspect;
      offsetY = (rect.height - displayHeight) / 2;
    }

    const x = ((e.clientX - rect.left - offsetX) / displayWidth) * video.videoWidth;
    const y = ((e.clientY - rect.top - offsetY) / displayHeight) * video.videoHeight;

    // Bounds check
    if (x < 0 || x > video.videoWidth || y < 0 || y > video.videoHeight) {
      return null;
    }

    return { x: Math.round(x), y: Math.round(y) };
  }, []);

  // Mouse event handlers
  const sendMouseEvent = useCallback((type: InputEvent['type'], e: React.MouseEvent<HTMLVideoElement>, button?: number) => {
    if (!isConnected || !webrtcRef.current) return;

    const coords = getVideoCoordinates(e);
    if (!coords) return;

    const input: InputEvent = {
      type,
      x: coords.x,
      y: coords.y,
    };

    if (button !== undefined) {
      input.button = button;
    }

    webrtcRef.current.sendInput(input);
  }, [isConnected, getVideoCoordinates]);

  const handleMouseMove = useCallback((e: React.MouseEvent<HTMLVideoElement>) => {
    sendMouseEvent('move', e);
  }, [sendMouseEvent]);

  const handleMouseDown = useCallback((e: React.MouseEvent<HTMLVideoElement>) => {
    e.preventDefault();
    sendMouseEvent('down', e, e.button);
  }, [sendMouseEvent]);

  const handleMouseUp = useCallback((e: React.MouseEvent<HTMLVideoElement>) => {
    e.preventDefault();
    sendMouseEvent('up', e, e.button);
  }, [sendMouseEvent]);

  const handleWheel = useCallback((e: React.WheelEvent<HTMLVideoElement>) => {
    if (!isConnected || !webrtcRef.current) return;

    const coords = getVideoCoordinates(e);
    if (!coords) return;

    webrtcRef.current.sendInput({
      type: 'wheel',
      x: coords.x,
      y: coords.y,
      deltaY: e.deltaY,
    });
  }, [isConnected, getVideoCoordinates]);

  const handleContextMenu = useCallback((e: React.MouseEvent<HTMLVideoElement>) => {
    e.preventDefault();
  }, []);

  // Keyboard event handlers
  useEffect(() => {
    if (!isConnected) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      // Only capture when container is focused
      if (!containerRef.current?.contains(document.activeElement) &&
          document.activeElement !== containerRef.current) {
        return;
      }

      e.preventDefault();
      webrtcRef.current?.sendInput({
        type: 'keydown',
        key: e.key,
        code: e.code,
        modifiers: {
          ctrl: e.ctrlKey,
          alt: e.altKey,
          shift: e.shiftKey,
          meta: e.metaKey,
        },
      });
    };

    const handleKeyUp = (e: KeyboardEvent) => {
      if (!containerRef.current?.contains(document.activeElement) &&
          document.activeElement !== containerRef.current) {
        return;
      }

      e.preventDefault();
      webrtcRef.current?.sendInput({
        type: 'keyup',
        key: e.key,
        code: e.code,
      });
    };

    window.addEventListener('keydown', handleKeyDown);
    window.addEventListener('keyup', handleKeyUp);

    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('keyup', handleKeyUp);
    };
  }, [isConnected]);

  // Connect to remote desktop
  const connect = useCallback(async () => {
    if (!wsService || isConnecting || isConnected) return;

    setError(null);
    setIsConnecting(true);
    setConnectionState('connecting');

    const sessionId = `webrtc-${deviceId}-${Date.now()}`;
    const webrtc = new WebRTCService(agentId);
    webrtcRef.current = webrtc;

    webrtc.setOnConnectionStateChange((state) => {
      setConnectionState(state);
      if (state === 'connected') {
        setIsConnected(true);
        setIsConnecting(false);
      } else if (state === 'failed' || state === 'disconnected' || state === 'closed') {
        setIsConnected(false);
        setIsConnecting(false);
      }
    });

    webrtc.setOnError((err) => {
      setError(err);
      setIsConnecting(false);
    });

    try {
      console.log('[RemoteDesktop] Connecting via WebRTC...');
      const stream = await webrtc.connect(sessionId);

      // Attach stream to video element
      if (videoRef.current) {
        videoRef.current.srcObject = stream;
      }
    } catch (err) {
      console.error('[RemoteDesktop] WebRTC connection failed:', err);
      setError(err instanceof Error ? err.message : 'Connection failed');
      setIsConnecting(false);
      setConnectionState('failed');
      webrtc.disconnect();
      webrtcRef.current = null;
    }
  }, [deviceId, agentId, isConnecting, isConnected]);

  // Disconnect
  const disconnect = useCallback(() => {
    if (webrtcRef.current) {
      webrtcRef.current.disconnect();
      webrtcRef.current = null;
    }

    if (videoRef.current) {
      videoRef.current.srcObject = null;
    }

    setIsConnected(false);
    setIsConnecting(false);
    setConnectionState('disconnected');
    setStats(null);
  }, []);

  // Stats polling
  useEffect(() => {
    if (!isConnected) return;

    const interval = setInterval(async () => {
      const newStats = await webrtcRef.current?.getStats();
      if (newStats) {
        setStats(newStats);
      }
    }, 1000);

    return () => clearInterval(interval);
  }, [isConnected]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      webrtcRef.current?.disconnect();
    };
  }, []);

  const toggleFullscreen = useCallback(() => {
    if (!containerRef.current) return;

    if (!isFullscreen) {
      containerRef.current.requestFullscreen?.();
    } else {
      document.exitFullscreen?.();
    }
    setIsFullscreen(!isFullscreen);
  }, [isFullscreen]);

  // Show offline state if device is not online
  if (!isOnline) {
    return (
      <div className="h-[500px] flex items-center justify-center bg-gray-900 rounded-lg">
        <div className="flex flex-col items-center gap-2 text-gray-400">
          <Monitor className="w-12 h-12" />
          <p>Device is offline. Remote Desktop is not available.</p>
        </div>
      </div>
    );
  }

  // Show error if WebSocket service is not available (Electron mode)
  if (!wsService) {
    return (
      <div className="h-[500px] flex items-center justify-center bg-gray-900 rounded-lg">
        <div className="flex flex-col items-center gap-2 text-gray-400">
          <Monitor className="w-12 h-12" />
          <p>Remote Desktop is only available in web mode.</p>
        </div>
      </div>
    );
  }

  const getStatusColor = () => {
    switch (connectionState) {
      case 'connected':
        return 'bg-green-500';
      case 'connecting':
      case 'new':
        return 'bg-yellow-500 animate-pulse';
      case 'failed':
        return 'bg-red-500';
      default:
        return 'bg-gray-500';
    }
  };

  const getStatusText = () => {
    switch (connectionState) {
      case 'connected':
        return 'Connected';
      case 'connecting':
      case 'new':
        return 'Connecting...';
      case 'failed':
        return 'Failed';
      case 'disconnected':
      case 'closed':
        return 'Disconnected';
      default:
        return connectionState;
    }
  };

  return (
    <div
      ref={containerRef}
      className={`flex flex-col bg-gray-900 rounded-lg overflow-hidden ${
        isFullscreen ? 'fixed inset-0 z-50' : 'h-[500px]'
      }`}
      tabIndex={0}
      style={{ outline: 'none' }}
    >
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2 bg-gray-800 border-b border-gray-700">
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <Monitor className="w-4 h-4 text-primary" />
            <span className="text-sm font-medium text-white">Remote Desktop</span>
            <span className="text-xs text-gray-500">(WebRTC)</span>
          </div>
          <div className={`w-2 h-2 rounded-full ${getStatusColor()}`} />
          <span className="text-sm text-gray-300">{getStatusText()}</span>
          {isConnected && stats && (
            <>
              <span className="text-xs text-gray-400">
                {stats.fps.toFixed(0)} FPS
              </span>
              <span className="text-xs text-gray-400">
                {stats.latency.toFixed(0)}ms
              </span>
              <span className="text-xs text-gray-400">
                {(stats.bitrate / 1_000_000).toFixed(1)} Mbps
              </span>
            </>
          )}
        </div>
        <div className="flex items-center gap-2">
          {isConnected && (
            <div className="flex items-center gap-1 text-xs text-gray-400">
              <MousePointer className="w-3 h-3" />
              <Keyboard className="w-3 h-3" />
              <Wifi className="w-3 h-3 text-green-400" />
            </div>
          )}
          {isConnected && (
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
          )}
          {isConnected ? (
            <button
              onClick={disconnect}
              className="px-3 py-1 text-sm bg-red-600 text-white rounded hover:bg-red-700 transition-colors"
            >
              Disconnect
            </button>
          ) : (
            <button
              onClick={connect}
              disabled={isConnecting}
              className="px-3 py-1 text-sm bg-green-600 text-white rounded hover:bg-green-700 transition-colors disabled:opacity-50"
            >
              {isConnecting ? 'Connecting...' : 'Connect'}
            </button>
          )}
        </div>
      </div>

      {/* Video container */}
      <div className="flex-1 flex items-center justify-center bg-black overflow-hidden relative">
        {/* Video element - browser handles all H.264 decoding */}
        <video
          ref={videoRef}
          autoPlay
          playsInline
          muted
          onMouseMove={handleMouseMove}
          onMouseDown={handleMouseDown}
          onMouseUp={handleMouseUp}
          onWheel={handleWheel}
          onContextMenu={handleContextMenu}
          className={`max-w-full max-h-full ${isConnected ? 'cursor-none' : 'cursor-default'}`}
          style={{
            objectFit: 'contain',
            backgroundColor: '#000',
            display: isConnected ? 'block' : 'none',
          }}
        />

        {/* Placeholder when not connected */}
        {!isConnected && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-4 text-gray-400">
            {error ? (
              <>
                <WifiOff className="w-16 h-16 text-red-400 opacity-50" />
                <p className="text-red-400">{error}</p>
                <button
                  onClick={() => { setError(null); connect(); }}
                  className="px-4 py-2 bg-green-600 text-white rounded hover:bg-green-700 transition-colors"
                >
                  Try Again
                </button>
              </>
            ) : isConnecting ? (
              <>
                <Monitor className="w-16 h-16 opacity-50 animate-pulse" />
                <p>Establishing WebRTC connection...</p>
                <p className="text-xs text-gray-500">This may take a few seconds</p>
              </>
            ) : (
              <>
                <Monitor className="w-16 h-16 opacity-50" />
                <p>Click "Connect" to start remote desktop session</p>
                <p className="text-xs text-gray-500">Uses WebRTC for low-latency streaming</p>
              </>
            )}
          </div>
        )}
      </div>

      {/* Status bar */}
      <div className="flex items-center justify-between px-4 py-1 bg-gray-800 border-t border-gray-700 text-xs text-gray-400">
        <span>
          {isConnected ? 'Click video to enable keyboard input' : isConnecting ? 'Establishing connection...' : 'Disconnected'}
        </span>
        <span>
          {isConnected && videoRef.current?.videoWidth && videoRef.current?.videoHeight
            ? `${videoRef.current.videoWidth}×${videoRef.current.videoHeight}`
            : 'WebRTC'}
        </span>
      </div>
    </div>
  );
});
