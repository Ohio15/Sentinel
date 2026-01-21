import React, { useRef, useEffect, useState, useCallback } from 'react';
import { events } from '../services';

// Types
interface HostCapabilities {
  screens: ScreenInfo[];
  encoders: EncoderInfo[];
  inputCapabilities: InputCapabilities;
  platform: string;
  osVersion: string;
  cpuCores: number;
  gpuName: string;
  dxgiCapture: boolean;
  hardwareEncode: boolean;
  cursorCapture: boolean;
}

interface ScreenInfo {
  index: number;
  width: number;
  height: number;
  x: number;
  y: number;
  refreshRate: number;
  dpiScale: number;
  isPrimary: boolean;
}

interface EncoderInfo {
  type: string;
  maxWidth: number;
  maxHeight: number;
  maxFps: number;
  supportsHardware: boolean;
}

interface InputCapabilities {
  absoluteMouse: boolean;
  relativeMouse: boolean;
  multiTouch: boolean;
  pen: boolean;
  clipboard: boolean;
}

interface NegotiatedSession {
  captureWidth: number;
  captureHeight: number;
  encodeWidth: number;
  encodeHeight: number;
  targetFps: number;
  maxLatencyMs: number;
  encoder: string;
  bitrate: number;
  adaptiveBitrate: boolean;
  coordinateSpace: CoordinateMapping;
  localCursor: boolean;
  clipboardSync: boolean;
  pointerLock: boolean;
}

interface CoordinateMapping {
  hostVirtualLeft: number;
  hostVirtualTop: number;
  hostVirtualWidth: number;
  hostVirtualHeight: number;
  captureOffsetX: number;
  captureOffsetY: number;
}

interface CursorUpdate {
  x: number;
  y: number;
  visible: boolean;
  shape: string;
  imageData?: string;
  hotspotX: number;
  hotspotY: number;
  width: number;
  height: number;
}

interface ClipboardData {
  text?: string;
  timestamp: number;
}

interface Stats {
  fps: number;
  latency: number;
  bitrate: number;
  packetsLost: number;
  jitter: number;
}

interface RemoteDesktopEnhancedProps {
  agentId: string;
  sessionId: string;
  webrtcService: any; // WebRTC service instance
  onDisconnect?: () => void;
  onCapabilities?: (caps: HostCapabilities) => void;
}

// Cursor shapes mapping
const CURSOR_SHAPES: Record<string, string> = {
  default: 'default',
  pointer: 'pointer',
  text: 'text',
  wait: 'wait',
  crosshair: 'crosshair',
  'not-allowed': 'not-allowed',
  'ew-resize': 'ew-resize',
  'ns-resize': 'ns-resize',
  'nesw-resize': 'nesw-resize',
  'nwse-resize': 'nwse-resize',
  move: 'move',
  grab: 'grab',
  grabbing: 'grabbing',
};

export function RemoteDesktopEnhanced({
  agentId,
  sessionId,
  webrtcService,
  onDisconnect,
  onCapabilities,
}: RemoteDesktopEnhancedProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const localCursorRef = useRef<HTMLDivElement>(null);

  // Connection state
  const [connected, setConnected] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Session state
  const [capabilities, setCapabilities] = useState<HostCapabilities | null>(null);
  const [session, setSession] = useState<NegotiatedSession | null>(null);
  const [stats, setStats] = useState<Stats>({ fps: 0, latency: 0, bitrate: 0, packetsLost: 0, jitter: 0 });

  // Local cursor state
  const [localCursor, setLocalCursor] = useState<CursorUpdate>({
    x: 0, y: 0, visible: true, shape: 'default', hotspotX: 0, hotspotY: 0, width: 0, height: 0
  });
  const [showLocalCursor, setShowLocalCursor] = useState(true);
  const [customCursorUrl, setCustomCursorUrl] = useState<string | null>(null);

  // Pointer lock state
  const [pointerLocked, setPointerLocked] = useState(false);
  const [pointerLockSupported] = useState(() => 'pointerLockElement' in document);

  // Clipboard state
  const [clipboardEnabled, setClipboardEnabled] = useState(true);
  const lastClipboardRef = useRef<string>('');

  // Calculate video coordinates from mouse event
  const getVideoCoordinates = useCallback((e: React.MouseEvent | MouseEvent): { x: number; y: number } | null => {
    const video = videoRef.current;
    if (!video || !video.videoWidth) return null;

    const rect = video.getBoundingClientRect();
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

  // Mouse handlers
  const handleMouseMove = useCallback((e: React.MouseEvent) => {
    if (!connected) return;

    // Update local cursor position immediately
    if (showLocalCursor && localCursorRef.current) {
      const rect = containerRef.current?.getBoundingClientRect();
      if (rect) {
        setLocalCursor(prev => ({
          ...prev,
          x: e.clientX - rect.left,
          y: e.clientY - rect.top,
        }));
      }
    }

    const coords = getVideoCoordinates(e);
    if (coords) {
      webrtcService?.sendInput({
        type: 'mouse',
        event: 'move',
        x: coords.x,
        y: coords.y,
      });
    }
  }, [connected, getVideoCoordinates, webrtcService, showLocalCursor]);

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    if (!connected) return;

    const coords = getVideoCoordinates(e);
    if (coords) {
      webrtcService?.sendInput({
        type: 'mouse',
        event: 'down',
        x: coords.x,
        y: coords.y,
        button: e.button,
      });
    }
  }, [connected, getVideoCoordinates, webrtcService]);

  const handleMouseUp = useCallback((e: React.MouseEvent) => {
    if (!connected) return;

    const coords = getVideoCoordinates(e);
    if (coords) {
      webrtcService?.sendInput({
        type: 'mouse',
        event: 'up',
        x: coords.x,
        y: coords.y,
        button: e.button,
      });
    }
  }, [connected, getVideoCoordinates, webrtcService]);

  const handleWheel = useCallback((e: React.WheelEvent) => {
    e.preventDefault();
    if (!connected) return;

    const coords = getVideoCoordinates(e);
    if (coords) {
      webrtcService?.sendInput({
        type: 'mouse',
        event: 'wheel',
        x: coords.x,
        y: coords.y,
        deltaY: e.deltaY,
      });
    }
  }, [connected, getVideoCoordinates, webrtcService]);

  // Pointer lock handlers
  const requestPointerLock = useCallback(async () => {
    if (!pointerLockSupported || !session?.pointerLock) return;

    try {
      await containerRef.current?.requestPointerLock();
    } catch (err) {
      console.warn('Pointer lock request failed:', err);
    }
  }, [pointerLockSupported, session?.pointerLock]);

  const exitPointerLock = useCallback(() => {
    document.exitPointerLock();
  }, []);

  // Handle pointer lock relative movement
  useEffect(() => {
    if (!pointerLocked || !connected) return;

    const handleLockedMouseMove = (e: MouseEvent) => {
      webrtcService?.sendInput({
        type: 'mouse_relative',
        deltaX: e.movementX,
        deltaY: e.movementY,
      });
    };

    document.addEventListener('mousemove', handleLockedMouseMove);
    return () => document.removeEventListener('mousemove', handleLockedMouseMove);
  }, [pointerLocked, connected, webrtcService]);

  // Pointer lock change detection
  useEffect(() => {
    const handleLockChange = () => {
      setPointerLocked(document.pointerLockElement === containerRef.current);
    };

    const handleLockError = () => {
      console.warn('Pointer lock error');
      setPointerLocked(false);
    };

    document.addEventListener('pointerlockchange', handleLockChange);
    document.addEventListener('pointerlockerror', handleLockError);

    return () => {
      document.removeEventListener('pointerlockchange', handleLockChange);
      document.removeEventListener('pointerlockerror', handleLockError);
    };
  }, []);

  // Keyboard handlers
  useEffect(() => {
    if (!connected) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (document.activeElement !== containerRef.current &&
          !containerRef.current?.contains(document.activeElement)) {
        return;
      }

      e.preventDefault();
      webrtcService?.sendInput({
        type: 'keyboard',
        event: 'down',
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
      if (document.activeElement !== containerRef.current &&
          !containerRef.current?.contains(document.activeElement)) {
        return;
      }

      e.preventDefault();
      webrtcService?.sendInput({
        type: 'keyboard',
        event: 'up',
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
  }, [connected, webrtcService]);

  // Clipboard sync
  useEffect(() => {
    if (!connected || !clipboardEnabled || !session?.clipboardSync) return;

    const checkClipboard = async () => {
      try {
        const text = await navigator.clipboard.readText();
        if (text !== lastClipboardRef.current) {
          lastClipboardRef.current = text;
          webrtcService?.sendInput({
            type: 'clipboard',
            text,
            timestamp: Date.now(),
          });
        }
      } catch (err) {
        // Clipboard access denied
      }
    };

    const interval = setInterval(checkClipboard, 500);
    return () => clearInterval(interval);
  }, [connected, clipboardEnabled, session?.clipboardSync, webrtcService]);

  // Handle incoming clipboard data
  useEffect(() => {
    if (!session?.clipboardSync) return;

    const unsubscribe = events.on('clipboard', async (data: unknown) => {
      const clipData = data as ClipboardData;
      if (clipData.text) {
        lastClipboardRef.current = clipData.text;
        try {
          await navigator.clipboard.writeText(clipData.text);
        } catch (err) {
          console.warn('Failed to write to clipboard:', err);
        }
      }
    });

    return unsubscribe;
  }, [session?.clipboardSync]);

  // Handle cursor updates from host
  useEffect(() => {
    if (!showLocalCursor) return;

    const unsubscribe = events.on('cursor_update', (data: unknown) => {
      const cursor = data as CursorUpdate;
      setLocalCursor(prev => ({
        ...prev,
        shape: cursor.shape,
        hotspotX: cursor.hotspotX,
        hotspotY: cursor.hotspotY,
        width: cursor.width,
        height: cursor.height,
      }));

      // Update custom cursor image if provided
      if (cursor.imageData) {
        setCustomCursorUrl(`data:image/png;base64,${cursor.imageData}`);
      } else {
        setCustomCursorUrl(null);
      }
    });

    return unsubscribe;
  }, [showLocalCursor]);

  // Handle capabilities from host
  useEffect(() => {
    const unsubscribe = events.on('host_capabilities', (data: unknown) => {
      const caps = data as HostCapabilities;
      setCapabilities(caps);
      onCapabilities?.(caps);
    });

    return unsubscribe;
  }, [onCapabilities]);

  // Handle session negotiation response
  useEffect(() => {
    const unsubscribe = events.on('session_negotiated', (data: unknown) => {
      const sess = data as NegotiatedSession;
      setSession(sess);
      setShowLocalCursor(sess.localCursor);
    });

    return unsubscribe;
  }, []);

  // Stats polling
  useEffect(() => {
    if (!connected) return;

    const interval = setInterval(async () => {
      const rtcStats = await webrtcService?.getStats();
      if (rtcStats) {
        setStats(rtcStats);
      }
    }, 1000);

    return () => clearInterval(interval);
  }, [connected, webrtcService]);

  // Connection state handling
  useEffect(() => {
    if (!webrtcService) return;

    const handleConnected = () => {
      setConnected(true);
      setConnecting(false);
      setError(null);
    };

    const handleDisconnected = () => {
      setConnected(false);
      setPointerLocked(false);
      onDisconnect?.();
    };

    const handleError = (err: string) => {
      setError(err);
      setConnecting(false);
    };

    // Subscribe to WebRTC events
    const unsub1 = events.on('webrtc_connected', handleConnected);
    const unsub2 = events.on('webrtc_disconnected', handleDisconnected);
    const unsub3 = events.on('webrtc_error', handleError);

    return () => {
      unsub1();
      unsub2();
      unsub3();
    };
  }, [webrtcService, onDisconnect]);

  // Attach stream to video element
  useEffect(() => {
    const video = videoRef.current;
    const stream = webrtcService?.getStream();

    if (video && stream) {
      video.srcObject = stream;
    }

    return () => {
      if (video) {
        video.srcObject = null;
      }
    };
  }, [webrtcService, connected]);

  // Get cursor style
  const getCursorStyle = () => {
    if (customCursorUrl) {
      return `url(${customCursorUrl}) ${localCursor.hotspotX} ${localCursor.hotspotY}, auto`;
    }
    return CURSOR_SHAPES[localCursor.shape] || 'default';
  };

  return (
    <div
      ref={containerRef}
      className="remote-desktop-enhanced"
      tabIndex={0}
      onDoubleClick={requestPointerLock}
      style={{
        position: 'relative',
        width: '100%',
        height: '100%',
        outline: 'none',
        cursor: showLocalCursor ? 'none' : getCursorStyle(),
        backgroundColor: '#000',
      }}
    >
      {/* Stats overlay */}
      {connected && (
        <div
          style={{
            position: 'absolute',
            top: 8,
            left: 8,
            padding: '4px 8px',
            backgroundColor: 'rgba(0,0,0,0.7)',
            color: '#fff',
            fontSize: 12,
            fontFamily: 'monospace',
            borderRadius: 4,
            zIndex: 100,
            pointerEvents: 'none',
          }}
        >
          {stats.fps.toFixed(0)} FPS | {stats.latency.toFixed(0)}ms | {(stats.bitrate / 1_000_000).toFixed(1)} Mbps
          {session?.encoder && ` | ${session.encoder}`}
          {pointerLocked && ' | LOCKED'}
        </div>
      )}

      {/* Connection status */}
      {!connected && (
        <div
          style={{
            position: 'absolute',
            top: '50%',
            left: '50%',
            transform: 'translate(-50%, -50%)',
            textAlign: 'center',
            color: '#fff',
          }}
        >
          {connecting ? (
            <div>
              <div className="spinner" style={{ marginBottom: 8 }} />
              Connecting...
            </div>
          ) : error ? (
            <div style={{ color: '#f44336' }}>{error}</div>
          ) : (
            <div>Disconnected</div>
          )}
        </div>
      )}

      {/* Pointer lock indicator */}
      {pointerLocked && (
        <div
          style={{
            position: 'absolute',
            bottom: 8,
            left: '50%',
            transform: 'translateX(-50%)',
            padding: '4px 12px',
            backgroundColor: 'rgba(0,0,0,0.7)',
            color: '#fff',
            fontSize: 12,
            borderRadius: 4,
            zIndex: 100,
          }}
        >
          Press Escape to release mouse
        </div>
      )}

      {/* Local cursor overlay */}
      {showLocalCursor && connected && localCursor.visible && !pointerLocked && (
        <div
          ref={localCursorRef}
          style={{
            position: 'absolute',
            left: localCursor.x - localCursor.hotspotX,
            top: localCursor.y - localCursor.hotspotY,
            pointerEvents: 'none',
            zIndex: 1000,
          }}
        >
          {customCursorUrl ? (
            <img
              src={customCursorUrl}
              alt=""
              width={localCursor.width || 32}
              height={localCursor.height || 32}
              style={{ display: 'block' }}
            />
          ) : (
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
              <path
                d="M5 3L19 12L12 13L9 20L5 3Z"
                fill="white"
                stroke="black"
                strokeWidth="1.5"
              />
            </svg>
          )}
        </div>
      )}

      {/* Video element */}
      <video
        ref={videoRef}
        autoPlay
        playsInline
        muted
        onMouseMove={!pointerLocked ? handleMouseMove : undefined}
        onMouseDown={!pointerLocked ? handleMouseDown : undefined}
        onMouseUp={!pointerLocked ? handleMouseUp : undefined}
        onWheel={handleWheel}
        onContextMenu={(e) => e.preventDefault()}
        style={{
          width: '100%',
          height: '100%',
          objectFit: 'contain',
          display: 'block',
        }}
      />
    </div>
  );
}

export default RemoteDesktopEnhanced;
