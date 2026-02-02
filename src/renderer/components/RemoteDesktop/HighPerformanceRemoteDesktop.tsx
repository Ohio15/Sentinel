import React, { useRef, useEffect, useState, useCallback, memo } from 'react';
import { useCursor, CursorShape } from './useCursor';
import { useWebRTC, WebRTCStats } from './useWebRTC';
import { useInput } from './useInput';
import { CursorOverlay } from './CursorOverlay';
import { useFramePacing } from './useFramePacing';
import { useDeviceStore } from '../../stores/deviceStore';
import { wsService } from '../../services/websocket';
import {
  Maximize2,
  Minimize2,
  Monitor,
  MousePointer,
  Keyboard,
  Wifi,
  WifiOff,
  Settings,
  Bug,
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
  const videoRef = useRef<HTMLVideoElement>(null);

  const [displaySize, setDisplaySize] = useState({ width: 0, height: 0 });
  const [videoOffset, setVideoOffset] = useState({ x: 0, y: 0 }); // Offset from container to video
  const [remoteSize, setRemoteSize] = useState({ width: 1920, height: 1080 });
  const [videoReady, setVideoReady] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [stats, setStats] = useState<WebRTCStats | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [showDebug, setShowDebug] = useState(false);
  const [latencyStats, setLatencyStats] = useState<{
    rtt: number;
    serverCapture: number;
    serverConvert: number;
    serverEncode: number;
    serverTotal: number;
  } | null>(null);

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
    onCursorUpdate: (update) => {
      // Update remote cursor position (for debugging)
      updateRemotePosition(update.x, update.y);
      if (update.visible !== undefined) {
        setCursorVisible(update.visible);
      }
    },
    onCursorShape: (shape) => {
      updateCursorShape(shape);
    },
    onLatencyUpdate: (latency) => {
      setLatencyStats(latency);
    },
  });

  // CSS custom cursor (native system cursor - instant feedback at mouse polling rate)
  const {
    cursor,
    cssCursor,
    handleMouseMove,
    updateCursorShape,
    updateRemotePosition,
    setCursorVisible,
  } = useCursor({
    displayWidth: displaySize.width,
    displayHeight: displaySize.height,
    remoteWidth: remoteSize.width,
    remoteHeight: remoteSize.height,
    videoOffsetX: videoOffset.x,
    videoOffsetY: videoOffset.y,
    onMove: (x, y) => {
      sendInput({ type: 'mousemove', x, y });
    },
  });

  // Frame pacing for jitter measurement
  const { stats: framePacingStats } = useFramePacing(videoRef);

  // Input handling (clicks, keyboard)
  const { handleMouseDown, handleMouseUp, handleWheel, handleContextMenu, setContainer } = useInput({
    displayWidth: displaySize.width,
    displayHeight: displaySize.height,
    remoteWidth: remoteSize.width,
    remoteHeight: remoteSize.height,
    videoOffsetX: videoOffset.x,
    videoOffsetY: videoOffset.y,
    sendInput,
    enabled: isConnected && videoReady,
  });

  // Set container ref for keyboard focus
  useEffect(() => {
    setContainer(containerRef.current);
  }, [setContainer]);

  // Track container size for coordinate mapping
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const updateSize = () => {
      const video = videoRef.current;
      if (!video) return;

      // Get the actual rendered video dimensions (accounting for letterboxing)
      const containerRect = container.getBoundingClientRect();
      const videoRect = video.getBoundingClientRect();
      const videoAspect = remoteSize.width / remoteSize.height;
      const containerAspect = containerRect.width / containerRect.height;

      let displayWidth: number, displayHeight: number;
      let offsetX: number, offsetY: number;

      if (containerAspect > videoAspect) {
        // Letterbox on sides (black bars left/right)
        displayHeight = containerRect.height;
        displayWidth = containerRect.height * videoAspect;
        offsetX = (containerRect.width - displayWidth) / 2;
        offsetY = 0;
      } else {
        // Letterbox on top/bottom (black bars top/bottom)
        displayWidth = containerRect.width;
        displayHeight = containerRect.width / videoAspect;
        offsetX = 0;
        offsetY = (containerRect.height - displayHeight) / 2;
      }

      setDisplaySize({ width: displayWidth, height: displayHeight });
      setVideoOffset({ x: offsetX, y: offsetY });
    };

    const observer = new ResizeObserver(updateSize);
    observer.observe(container);

    // Also update when video metadata loads
    const video = videoRef.current;
    if (video) {
      video.addEventListener('loadedmetadata', () => {
        if (video.videoWidth && video.videoHeight) {
          setRemoteSize({ width: video.videoWidth, height: video.videoHeight });
          updateSize();
        }
      });
      video.addEventListener('resize', updateSize);
    }

    return () => {
      observer.disconnect();
      if (video) {
        video.removeEventListener('resize', updateSize);
      }
    };
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

      {/* Video container with CSS custom cursor (native system cursor for instant feedback) */}
      <div
        className="flex-1 flex items-center justify-center bg-black overflow-hidden relative"
        style={{ cursor: isConnected && videoReady ? cssCursor : 'default' }}
        onMouseMove={isConnected && videoReady ? handleMouseMove : undefined}
        onMouseDown={handleMouseDown}
        onMouseUp={handleMouseUp}
        onWheel={handleWheel}
        onContextMenu={handleContextMenu}
      >
        {/* Video element - browser handles all H.264 decoding */}
        <video
          ref={videoRef}
          autoPlay
          playsInline
          muted
          onCanPlay={handleVideoCanPlay}
          className="max-w-full max-h-full"
          style={{
            objectFit: 'contain',
            backgroundColor: '#000',
            display: isConnected ? 'block' : 'none',
          }}
        />

        {/* Local cursor overlay - moves instantly with mouse, server corrections applied invisibly */}
        {isConnected && videoReady && (
          <CursorOverlay
            cursor={cursor}
            showRemoteCursor={showDebug}
          />
        )}

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

        {/* Debug info overlay */}
        {showDebug && isConnected && (
          <div className="absolute top-2 left-2 bg-black/75 text-xs text-white p-2 rounded font-mono">
            <div>Remote: {remoteSize.width}x{remoteSize.height}</div>
            <div>Display: {displaySize.width.toFixed(0)}x{displaySize.height.toFixed(0)}</div>
            <div>
              Local cursor: ({cursor.local.x.toFixed(0)}, {cursor.local.y.toFixed(0)})
            </div>
            <div>
              Remote cursor: ({cursor.remote.x.toFixed(0)}, {cursor.remote.y.toFixed(0)})
            </div>
            {stats && (
              <>
                <div className="mt-1 pt-1 border-t border-gray-600">WebRTC Stats:</div>
                <div>FPS: {stats.fps.toFixed(1)}</div>
                <div>Latency: {stats.latency.toFixed(1)}ms</div>
                <div>Bitrate: {(stats.bitrate / 1_000_000).toFixed(2)} Mbps</div>
                <div>Packets lost: {stats.packetsLost}</div>
                <div>Jitter: {(stats.jitter * 1000).toFixed(2)}ms</div>
              </>
            )}
            {latencyStats && (
              <>
                <div className="mt-1 pt-1 border-t border-gray-600">Pipeline Timing:</div>
                <div>RTT: {latencyStats.rtt.toFixed(1)}ms</div>
                <div>Capture: {latencyStats.serverCapture.toFixed(1)}ms</div>
                <div>Convert: {latencyStats.serverConvert.toFixed(1)}ms</div>
                <div>Encode: {latencyStats.serverEncode.toFixed(1)}ms</div>
                <div>Server Total: {latencyStats.serverTotal.toFixed(1)}ms</div>
              </>
            )}
            {framePacingStats && (
              <>
                <div className="mt-1 pt-1 border-t border-gray-600">Frame Pacing:</div>
                <div>Jitter: {framePacingStats.avgJitter.toFixed(1)}ms</div>
                <div>Buffer Target: {framePacingStats.targetBuffer.toFixed(0)}ms</div>
                <div>Frame Interval: {framePacingStats.frameInterval.toFixed(1)}ms</div>
                <div>Dropped: {framePacingStats.droppedFrames}</div>
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
