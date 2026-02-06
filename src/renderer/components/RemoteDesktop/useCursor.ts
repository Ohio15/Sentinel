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

  // Use native system cursor for best performance (no React rendering lag)
  const cssCursor = useMemo(() => {
    return 'crosshair';
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

  // Handle mouse move - send position to remote (no React state updates for performance)
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

      // Only send to remote if within video bounds
      if (videoX >= 0 && videoX <= displayWidth && videoY >= 0 && videoY <= displayHeight) {
        // Scale to remote coordinates
        const remote = scaleToRemote(clampedX, clampedY);

        // Only send if position changed by at least 2 pixels (reduces noise, helps with PIN entry etc)
        const dx = Math.abs(remote.x - lastSentRef.current.x);
        const dy = Math.abs(remote.y - lastSentRef.current.y);
        if (dx < 2 && dy < 2) {
          return; // Skip tiny movements
        }

        // Throttle remote updates via RAF
        if (!rafRef.current) {
          rafRef.current = requestAnimationFrame(() => {
            rafRef.current = undefined;
            lastSentRef.current = remote;
            onMove(remote.x, remote.y);
          });
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
