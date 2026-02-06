import React, { useRef, useEffect, useState, useCallback, memo } from 'react';
import { useWebRTC, WebRTCStats } from './useWebRTC';
import { useSimpleInput } from './useSimpleInput';
import { useDeviceStore } from '../../stores/deviceStore';
import { wsService } from '../../services/websocket';
import {
  Maximize2,
  Minimize2,
  Monitor,
  MousePointer,
  Pause,
  Keyboard,
  Wifi,
  WifiOff,
  Bug,
  Shield,
} from 'lucide-react';

interface HighPerformanceRemoteDesktopProps {
  deviceId: string;
  isOnline: boolean;
  isActive?: boolean;
}

export const HighPerformanceRemoteDesktop = memo(function HighPerformanceRemoteDesktop({
  deviceId,
  isOnline,
  isActive = true,
}: HighPerformanceRemoteDesktopProps) {
  const device = useDeviceStore((state) => state.devices.find((d) => d.id === deviceId));
  const agentId = device?.agentId || deviceId;

  const containerRef = useRef<HTMLDivElement>(null);
  const videoContainerRef = useRef<HTMLDivElement>(null);
  const wrapperRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);

  const [remoteSize, setRemoteSize] = useState({ width: 1920, height: 1080 });
  const [wrapperSize, setWrapperSize] = useState({ width: 0, height: 0 });
  const [videoReady, setVideoReady] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [stats, setStats] = useState<WebRTCStats | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [showDebug, setShowDebug] = useState(false);
  const [pauseMouse, setPauseMouse] = useState(false); // Pause mouse moves for PIN entry etc.

  // WebRTC connection
  const {
    connect,
    disconnect,
    sendInput,
    getStats,
    connectionState,
    isConnected,
  } = useWebRTC({
    agentId,
    onVideoTrack: (track) => {
      if (videoRef.current) {
        videoRef.current.srcObject = new MediaStream([track]);
      }
    },
    onRemoteInfo: (info) => {
      console.log('[RemoteDesktop] Remote info received:', info);
      setRemoteSize({ width: info.width, height: info.height });
    },
  });

  // Send Ctrl+Alt+Del sequence (for Windows login screens)
  const sendCtrlAltDel = useCallback(() => {
    if (!isConnected) return;

    // Send key down events
    sendInput({ type: 'keydown', key: 'Control', code: 'ControlLeft' });
    sendInput({ type: 'keydown', key: 'Alt', code: 'AltLeft' });
    sendInput({ type: 'keydown', key: 'Delete', code: 'Delete' });

    // Small delay then release
    setTimeout(() => {
      sendInput({ type: 'keyup', key: 'Delete', code: 'Delete' });
      sendInput({ type: 'keyup', key: 'Alt', code: 'AltLeft' });
      sendInput({ type: 'keyup', key: 'Control', code: 'ControlLeft' });
    }, 50);
  }, [isConnected, sendInput]);

  // Simple input handling (Neko-inspired approach)
  const inputEnabled = isConnected && videoReady;
  const {
    handleMouseMove,
    handleMouseDown,
    handleMouseUp,
    handleWheel,
    handleContextMenu,
    handleKeyDown,
    handleKeyUp,
  } = useSimpleInput({
    remoteWidth: remoteSize.width,
    remoteHeight: remoteSize.height,
    sendInput,
    enabled: inputEnabled,
  });

  // Calculate wrapper size to fit container while maintaining aspect ratio
  useEffect(() => {
    const container = videoContainerRef.current;
    if (!container) return;

    const updateSize = () => {
      const rect = container.getBoundingClientRect();
      if (rect.width === 0 || rect.height === 0) return;

      // Calculate wrapper size that maintains remote aspect ratio
      const remoteAspect = remoteSize.width / remoteSize.height;
      const containerAspect = rect.width / rect.height;

      let width: number;
      let height: number;

      if (remoteAspect > containerAspect) {
        // Remote is wider - fit to container width
        width = rect.width;
        height = rect.width / remoteAspect;
      } else {
        // Remote is taller - fit to container height
        height = rect.height;
        width = rect.height * remoteAspect;
      }

      console.log('[RemoteDesktop] Wrapper size:', { width, height, container: { w: rect.width, h: rect.height } });
      setWrapperSize({ width, height });
    };

    updateSize();
    const observer = new ResizeObserver(updateSize);
    observer.observe(container);

    return () => observer.disconnect();
  }, [remoteSize.width, remoteSize.height]);

  // Handle video ready state
  const handleVideoCanPlay = useCallback(() => {
    setVideoReady(true);
    // Update remote size from actual video dimensions
    const video = videoRef.current;
    if (video && video.videoWidth && video.videoHeight) {
      setRemoteSize({ width: video.videoWidth, height: video.videoHeight });
    }
  }, []);

  // Connect to remote desktop
  const handleConnect = useCallback(async () => {
    // Allow connect if disconnected OR failed (to retry)
    if (connectionState !== 'disconnected' && connectionState !== 'failed') {
      console.log('[RemoteDesktop] Ignoring connect - state is:', connectionState);
      return;
    }

    setError(null);
    setVideoReady(false);

    try {
      await connect();
    } catch (err) {
      console.error('[RemoteDesktop] Connection failed:', err);
      setError(err instanceof Error ? err.message : 'Connection failed');
      // Ensure we can retry by calling disconnect to reset state
      disconnect();
    }
  }, [connect, disconnect, connectionState]);

  // Disconnect
  const handleDisconnect = useCallback(() => {
    disconnect();
    setVideoReady(false);
    if (videoRef.current) {
      videoRef.current.srcObject = null;
    }
  }, [disconnect]);

  // Stats polling
  useEffect(() => {
    if (!isConnected) return;

    const interval = setInterval(async () => {
      const newStats = await getStats();
      if (newStats) {
        setStats(newStats);
      }
    }, 1000);

    return () => clearInterval(interval);
  }, [isConnected, getStats]);

  // Fullscreen toggle
  const toggleFullscreen = useCallback(() => {
    if (!containerRef.current) return;

    if (!isFullscreen) {
      containerRef.current.requestFullscreen?.();
    } else {
      document.exitFullscreen?.();
    }
    setIsFullscreen(!isFullscreen);
  }, [isFullscreen]);

  // Offline state
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

  // No WebSocket service
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
        return 'Connecting...';
      case 'failed':
        return 'Failed';
      default:
        return 'Disconnected';
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
            <span className="text-xs text-green-400">(High Performance)</span>
          </div>
          <div className={`w-2 h-2 rounded-full ${getStatusColor()}`} />
          <span className="text-sm text-gray-300">{getStatusText()}</span>
          {isConnected && stats && (
            <>
              <span className="text-xs text-gray-400">{stats.fps.toFixed(0)} FPS</span>
              <span className="text-xs text-gray-400">{stats.latency.toFixed(0)}ms</span>
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
            <>
              <button
                onClick={sendCtrlAltDel}
                className="p-1 text-gray-400 hover:text-white transition-colors"
                title="Send Ctrl+Alt+Del"
              >
                <Shield className="w-4 h-4" />
              </button>
              <button
                onClick={() => setPauseMouse(!pauseMouse)}
                className={`p-1 transition-colors ${
                  pauseMouse ? 'text-orange-400' : 'text-gray-400 hover:text-white'
                }`}
                title={pauseMouse ? 'Resume mouse (click to enable)' : 'Pause mouse (for PIN entry)'}
              >
                {pauseMouse ? <Pause className="w-4 h-4" /> : <MousePointer className="w-4 h-4" />}
              </button>
            </>
          )}
          <button
            onClick={() => setShowDebug(!showDebug)}
            className={`p-1 transition-colors ${
              showDebug ? 'text-yellow-400' : 'text-gray-400 hover:text-white'
            }`}
            title="Toggle debug overlay"
          >
            <Bug className="w-4 h-4" />
          </button>
          {isConnected && (
            <button
              onClick={toggleFullscreen}
              className="p-1 text-gray-400 hover:text-white transition-colors"
              title={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'}
            >
              {isFullscreen ? <Minimize2 className="w-4 h-4" /> : <Maximize2 className="w-4 h-4" />}
            </button>
          )}
          {isConnected ? (
            <button
              onClick={handleDisconnect}
              className="px-3 py-1 text-sm bg-red-600 text-white rounded hover:bg-red-700 transition-colors"
            >
              Disconnect
            </button>
          ) : (
            <button
              onClick={handleConnect}
              disabled={connectionState === 'connecting'}
              className="px-3 py-1 text-sm bg-green-600 text-white rounded hover:bg-green-700 transition-colors disabled:opacity-50"
            >
              {connectionState === 'connecting' ? 'Connecting...' : 'Connect'}
            </button>
          )}
        </div>
      </div>

      {/* Video container - centers the wrapper */}
      <div
        ref={videoContainerRef}
        className="flex-1 flex items-center justify-center bg-black overflow-hidden relative"
      >
        {/* Video wrapper - explicit size maintains aspect ratio, receives input events */}
        <div
          ref={wrapperRef}
          tabIndex={0}
          className="relative focus:outline-none"
          style={{
            width: wrapperSize.width > 0 ? `${wrapperSize.width}px` : '100%',
            height: wrapperSize.height > 0 ? `${wrapperSize.height}px` : '100%',
            cursor: inputEnabled ? 'crosshair' : 'default',
          }}
          onMouseMove={inputEnabled && !pauseMouse ? handleMouseMove : undefined}
          onMouseDown={inputEnabled ? handleMouseDown : undefined}
          onMouseUp={inputEnabled ? handleMouseUp : undefined}
          onWheel={inputEnabled ? handleWheel : undefined}
          onContextMenu={handleContextMenu}
          onKeyDown={inputEnabled ? handleKeyDown : undefined}
          onKeyUp={inputEnabled ? handleKeyUp : undefined}
        >
          {/* Video fills wrapper completely */}
          <video
            ref={videoRef}
            autoPlay
            playsInline
            muted
            onCanPlay={handleVideoCanPlay}
            style={{
              width: '100%',
              height: '100%',
              objectFit: 'fill',
              backgroundColor: '#000',
              display: isConnected ? 'block' : 'none',
              pointerEvents: 'none',
            }}
          />

          {/* Debug overlay */}
          {showDebug && isConnected && (
            <div className="absolute top-2 left-2 bg-black/80 text-xs text-white p-2 rounded font-mono z-10">
              <div>Remote: {remoteSize.width}x{remoteSize.height}</div>
              <div>Wrapper: {wrapperSize.width.toFixed(0)}x{wrapperSize.height.toFixed(0)}</div>
              {stats && (
                <>
                  <div className="mt-1 pt-1 border-t border-gray-600">WebRTC:</div>
                  <div>FPS: {stats.fps.toFixed(1)} | {stats.latency.toFixed(0)}ms</div>
                  <div>{(stats.bitrate / 1_000_000).toFixed(2)} Mbps</div>
                </>
              )}
            </div>
          )}
        </div>

        {/* Placeholder when not connected */}
        {!isConnected && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-4 text-gray-400">
            {error ? (
              <>
                <WifiOff className="w-16 h-16 text-red-400 opacity-50" />
                <p className="text-red-400">{error}</p>
                <button
                  onClick={() => {
                    setError(null);
                    handleConnect();
                  }}
                  className="px-4 py-2 bg-green-600 text-white rounded hover:bg-green-700 transition-colors"
                >
                  Try Again
                </button>
              </>
            ) : connectionState === 'connecting' ? (
              <>
                <Monitor className="w-16 h-16 opacity-50 animate-pulse" />
                <p>Establishing WebRTC connection...</p>
                <p className="text-xs text-gray-500">Local cursor rendering for instant feedback</p>
              </>
            ) : (
              <>
                <Monitor className="w-16 h-16 opacity-50" />
                <p>Click "Connect" to start high-performance remote desktop</p>
                <p className="text-xs text-gray-500">
                  Features: Local cursor, DXGI capture, H.264 hardware encoding
                </p>
              </>
            )}
          </div>
        )}

      </div>

      {/* Status bar */}
      <div className="flex items-center justify-between px-4 py-1 bg-gray-800 border-t border-gray-700 text-xs text-gray-400">
        <span>
          {isConnected
            ? 'Click video area to enable keyboard input'
            : connectionState === 'connecting'
            ? 'Establishing connection...'
            : 'Disconnected'}
        </span>
        <span>
          {isConnected && videoRef.current?.videoWidth && videoRef.current?.videoHeight
            ? `${videoRef.current.videoWidth}x${videoRef.current.videoHeight}`
            : 'WebRTC H.264'}
        </span>
      </div>
    </div>
  );
});

export default HighPerformanceRemoteDesktop;
