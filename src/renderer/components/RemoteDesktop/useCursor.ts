import { useState, useCallback, useRef, useEffect, useMemo } from 'react';

export interface CursorPosition {
  x: number;
  y: number;
}

export interface CursorShape {
  type: 'default' | 'pointer' | 'text' | 'wait' | 'crosshair' | 'move' | 'not-allowed' | 'custom';
  hotspot: { x: number; y: number };
  image?: string; // Base64 PNG for custom cursors
}

export interface CursorState {
  local: CursorPosition;      // Where cursor appears on screen (instant, with correction applied)
  remote: CursorPosition;     // Last known remote position (delayed - from server)
  shape: CursorShape;         // Current cursor icon
  visible: boolean;
}

// Correction constants for smooth cursor drift correction
const CORRECTION_THRESHOLD = 5; // pixels - only correct if drift exceeds this
const CORRECTION_SPEED = 0.15; // 15% correction per update (smooth, not jarring)

interface UseCursorOptions {
  displayWidth: number;
  displayHeight: number;
  remoteWidth: number;
  remoteHeight: number;
  videoOffsetX?: number; // Offset from container to video (for letterboxing)
  videoOffsetY?: number;
  onMove: (remoteX: number, remoteY: number) => void;
}

// Standard cursor CSS values
const CURSOR_CSS: Record<string, string> = {
  default: 'default',
  pointer: 'pointer',
  text: 'text',
  wait: 'wait',
  crosshair: 'crosshair',
  move: 'move',
  'not-allowed': 'not-allowed',
};

// Built-in cursor SVGs for CSS cursor (used when custom cursor image is needed)
const CURSOR_SVGS: Record<string, string> = {
  default: `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">
    <path fill="white" stroke="black" stroke-width="1.5" d="M5.5 3.21V20.8l4.86-4.86h6.36L5.5 3.21z"/>
  </svg>`,
};

export function useCursor(options: UseCursorOptions) {
  const { displayWidth, displayHeight, remoteWidth, remoteHeight, videoOffsetX = 0, videoOffsetY = 0, onMove } = options;

  const [cursor, setCursor] = useState<CursorState>({
    local: { x: 0, y: 0 },
    remote: { x: 0, y: 0 },
    shape: { type: 'default', hotspot: { x: 0, y: 0 } },
    visible: true,
  });

  // Throttle remote updates using requestAnimationFrame
  const pendingRef = useRef<CursorPosition | null>(null);
  const rafRef = useRef<number>();
  const lastSentRef = useRef<CursorPosition>({ x: -1, y: -1 });

  // Correction offset: accumulated drift correction applied to local cursor
  const correctionRef = useRef<CursorPosition>({ x: 0, y: 0 });

  // Generate CSS cursor value - hide system cursor since we render our own via CursorOverlay
  // The CursorOverlay provides instant local cursor movement with smooth server-correction
  const cssCursor = useMemo(() => {
    // Hide system cursor - CursorOverlay renders the cursor at native polling rate
    return 'none';
  }, []);

  // Scale local coordinates to remote coordinates
  const scaleToRemote = useCallback(
    (localX: number, localY: number): CursorPosition => {
      if (displayWidth === 0 || displayHeight === 0) {
        return { x: 0, y: 0 };
      }
      return {
        x: Math.round((localX / displayWidth) * remoteWidth),
        y: Math.round((localY / displayHeight) * remoteHeight),
      };
    },
    [displayWidth, displayHeight, remoteWidth, remoteHeight]
  );

  // Scale remote coordinates to local coordinates (for showing remote cursor position)
  const scaleToLocal = useCallback(
    (remoteX: number, remoteY: number): CursorPosition => {
      if (remoteWidth === 0 || remoteHeight === 0) {
        return { x: 0, y: 0 };
      }
      return {
        x: Math.round((remoteX / remoteWidth) * displayWidth) + videoOffsetX,
        y: Math.round((remoteY / remoteHeight) * displayHeight) + videoOffsetY,
      };
    },
    [displayWidth, displayHeight, remoteWidth, remoteHeight, videoOffsetX, videoOffsetY]
  );

  // Handle mouse move - CursorOverlay renders cursor, we track position and send to remote
  const handleMouseMove = useCallback(
    (e: React.MouseEvent<HTMLElement>) => {
      const rect = e.currentTarget.getBoundingClientRect();
      const containerX = e.clientX - rect.left;
      const containerY = e.clientY - rect.top;

      // Calculate position relative to video (accounting for letterbox offset)
      const videoX = containerX - videoOffsetX;
      const videoY = containerY - videoOffsetY;

      // Clamp to video bounds
      const clampedX = Math.max(0, Math.min(videoX, displayWidth));
      const clampedY = Math.max(0, Math.min(videoY, displayHeight));

      // Apply correction offset to local cursor position
      // This smoothly corrects for any drift between local and server cursor positions
      const correctedX = videoOffsetX + clampedX + correctionRef.current.x;
      const correctedY = videoOffsetY + clampedY + correctionRef.current.y;

      // Decay correction offset over time (prevents accumulation)
      correctionRef.current.x *= 0.95;
      correctionRef.current.y *= 0.95;

      // Zero out very small corrections (avoids micro-jitter)
      if (Math.abs(correctionRef.current.x) < 0.5) correctionRef.current.x = 0;
      if (Math.abs(correctionRef.current.y) < 0.5) correctionRef.current.y = 0;

      // Update local cursor position (used by CursorOverlay for rendering)
      setCursor((prev) => ({
        ...prev,
        local: { x: correctedX, y: correctedY },
      }));

      // Only send to remote if within video bounds
      if (videoX >= 0 && videoX <= displayWidth && videoY >= 0 && videoY <= displayHeight) {
        // Scale to remote coordinates
        const remote = scaleToRemote(clampedX, clampedY);

        // Throttle remote updates via RAF (60fps for network, cursor rendered locally at native rate)
        if (!rafRef.current) {
          rafRef.current = requestAnimationFrame(() => {
            rafRef.current = undefined;

            // Only send if position changed
            if (remote.x !== lastSentRef.current.x || remote.y !== lastSentRef.current.y) {
              lastSentRef.current = remote;
              onMove(remote.x, remote.y);
            }
          });
        } else {
          // Update pending position for next RAF
          pendingRef.current = remote;
        }
      }
    },
    [scaleToRemote, onMove, videoOffsetX, videoOffsetY, displayWidth, displayHeight]
  );

  // Update cursor shape when remote sends new shape
  const updateCursorShape = useCallback((shape: CursorShape) => {
    setCursor((prev) => ({ ...prev, shape }));
  }, []);

  // Update remote position and apply correction to local cursor
  // This is called when server sends cursor position (where cursor actually is on remote)
  const updateRemotePosition = useCallback(
    (x: number, y: number) => {
      // Scale remote coordinates to display coordinates
      const remoteLocal = scaleToLocal(x, y);

      setCursor((prev) => {
        // Calculate drift between where local cursor IS and where server says it should be
        const dx = remoteLocal.x - prev.local.x;
        const dy = remoteLocal.y - prev.local.y;
        const drift = Math.sqrt(dx * dx + dy * dy);

        // Only apply correction if drift exceeds threshold (avoids micro-jitter)
        if (drift > CORRECTION_THRESHOLD) {
          // Accumulate correction offset (smooth over multiple frames)
          correctionRef.current.x += dx * CORRECTION_SPEED;
          correctionRef.current.y += dy * CORRECTION_SPEED;
        }

        return {
          ...prev,
          remote: { x: remoteLocal.x, y: remoteLocal.y },
        };
      });
    },
    [scaleToLocal]
  );

  // Hide/show cursor
  const setCursorVisible = useCallback((visible: boolean) => {
    setCursor((prev) => ({ ...prev, visible }));
  }, []);

  // Reset cursor to default state
  const resetCursor = useCallback(() => {
    setCursor({
      local: { x: 0, y: 0 },
      remote: { x: 0, y: 0 },
      shape: { type: 'default', hotspot: { x: 0, y: 0 } },
      visible: true,
    });
    lastSentRef.current = { x: -1, y: -1 };
  }, []);

  // Cleanup
  useEffect(() => {
    return () => {
      if (rafRef.current) {
        cancelAnimationFrame(rafRef.current);
      }
    };
  }, []);

  return {
    cursor,
    cssCursor,  // CSS cursor value for native system cursor
    handleMouseMove,
    updateCursorShape,
    updateRemotePosition,
    setCursorVisible,
    resetCursor,
    scaleToRemote,
    scaleToLocal,
  };
}
