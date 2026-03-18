/**
 * Simple input handling based on Neko's proven approach.
 *
 * Key insight: With a wrapper div sized to maintain aspect ratio and video using
 * objectFit: fill, coordinate mapping is just a simple ratio:
 *
 *   remoteX = (localX / wrapperWidth) * remoteWidth
 *   remoteY = (localY / wrapperHeight) * remoteHeight
 *
 * No letterboxing calculations needed.
 */

import { useCallback, useRef } from 'react';

export interface SimpleInputEvent {
  type: 'mousemove' | 'mousedown' | 'mouseup' | 'keydown' | 'keyup' | 'wheel';
  x?: number;
  y?: number;
  button?: number;
  key?: string;
  code?: string;
  deltaY?: number;
}

interface UseSimpleInputOptions {
  remoteWidth: number;
  remoteHeight: number;
  dpiScale?: number;
  sendInput: (event: SimpleInputEvent) => void;
  enabled?: boolean;
}

// Minimum movement threshold to reduce noise (helps with PIN entry, etc.)
const MOVE_THRESHOLD = 2;

export function useSimpleInput(options: UseSimpleInputOptions) {
  const { remoteWidth, remoteHeight, dpiScale, sendInput, enabled = true } = options;

  // Track last sent position to filter tiny movements
  const lastSentRef = useRef<{ x: number; y: number }>({ x: -1, y: -1 });

  // Throttle mousemove with RAF
  const rafRef = useRef<number | null>(null);
  const pendingMoveRef = useRef<{ x: number; y: number } | null>(null);

  /**
   * Convert local coordinates (relative to video wrapper) to remote coordinates.
   * This is Neko's simple, proven formula.
   */
  const toRemote = useCallback(
    (localX: number, localY: number, wrapperWidth: number, wrapperHeight: number) => {
      if (wrapperWidth === 0 || wrapperHeight === 0) {
        return { x: 0, y: 0 };
      }
      const scale = dpiScale || 1.0;
      return {
        x: Math.round((remoteWidth / wrapperWidth) * localX * scale),
        y: Math.round((remoteHeight / wrapperHeight) * localY * scale),
      };
    },
    [remoteWidth, remoteHeight, dpiScale]
  );

  /**
   * Mouse move handler - sends throttled position updates
   */
  const handleMouseMove = useCallback(
    (e: React.MouseEvent<HTMLElement>) => {
      if (!enabled) return;

      const rect = e.currentTarget.getBoundingClientRect();
      const localX = e.clientX - rect.left;
      const localY = e.clientY - rect.top;
      const remote = toRemote(localX, localY, rect.width, rect.height);

      // Skip tiny movements
      const dx = Math.abs(remote.x - lastSentRef.current.x);
      const dy = Math.abs(remote.y - lastSentRef.current.y);
      if (dx < MOVE_THRESHOLD && dy < MOVE_THRESHOLD) {
        return;
      }

      // Throttle via RAF
      pendingMoveRef.current = remote;
      if (!rafRef.current) {
        rafRef.current = requestAnimationFrame(() => {
          rafRef.current = null;
          if (pendingMoveRef.current) {
            lastSentRef.current = pendingMoveRef.current;
            sendInput({
              type: 'mousemove',
              x: pendingMoveRef.current.x,
              y: pendingMoveRef.current.y,
            });
            pendingMoveRef.current = null;
          }
        });
      }
    },
    [enabled, toRemote, sendInput]
  );

  /**
   * Mouse down handler
   */
  const handleMouseDown = useCallback(
    (e: React.MouseEvent<HTMLElement>) => {
      if (!enabled) return;
      e.preventDefault();

      const rect = e.currentTarget.getBoundingClientRect();
      const localX = e.clientX - rect.left;
      const localY = e.clientY - rect.top;
      const remote = toRemote(localX, localY, rect.width, rect.height);

      console.log('[SimpleInput] mousedown:', { local: { x: localX, y: localY }, remote, button: e.button });

      sendInput({
        type: 'mousedown',
        x: remote.x,
        y: remote.y,
        button: e.button,
      });
    },
    [enabled, toRemote, sendInput]
  );

  /**
   * Mouse up handler
   */
  const handleMouseUp = useCallback(
    (e: React.MouseEvent<HTMLElement>) => {
      if (!enabled) return;
      e.preventDefault();

      const rect = e.currentTarget.getBoundingClientRect();
      const localX = e.clientX - rect.left;
      const localY = e.clientY - rect.top;
      const remote = toRemote(localX, localY, rect.width, rect.height);

      sendInput({
        type: 'mouseup',
        x: remote.x,
        y: remote.y,
        button: e.button,
      });
    },
    [enabled, toRemote, sendInput]
  );

  /**
   * Wheel handler
   */
  const handleWheel = useCallback(
    (e: React.WheelEvent<HTMLElement>) => {
      if (!enabled) return;
      e.preventDefault();

      const rect = e.currentTarget.getBoundingClientRect();
      const localX = e.clientX - rect.left;
      const localY = e.clientY - rect.top;
      const remote = toRemote(localX, localY, rect.width, rect.height);

      // Normalize wheel delta
      let deltaY = e.deltaY;
      if (e.deltaMode === WheelEvent.DOM_DELTA_LINE) {
        deltaY *= 19; // Line height in pixels
      }
      deltaY = Math.min(Math.max(deltaY, -120), 120);

      sendInput({
        type: 'wheel',
        x: remote.x,
        y: remote.y,
        deltaY: Math.round(deltaY),
      });
    },
    [enabled, toRemote, sendInput]
  );

  /**
   * Context menu handler (prevent default)
   */
  const handleContextMenu = useCallback((e: React.MouseEvent<HTMLElement>) => {
    e.preventDefault();
  }, []);

  /**
   * Key down handler
   */
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLElement>) => {
      if (!enabled) return;

      // Allow F11 for fullscreen
      if (e.code === 'F11') return;

      e.preventDefault();
      sendInput({
        type: 'keydown',
        key: e.key,
        code: e.code,
      });
    },
    [enabled, sendInput]
  );

  /**
   * Key up handler
   */
  const handleKeyUp = useCallback(
    (e: React.KeyboardEvent<HTMLElement>) => {
      if (!enabled) return;

      if (e.code === 'F11') return;

      e.preventDefault();
      sendInput({
        type: 'keyup',
        key: e.key,
        code: e.code,
      });
    },
    [enabled, sendInput]
  );

  return {
    handleMouseMove,
    handleMouseDown,
    handleMouseUp,
    handleWheel,
    handleContextMenu,
    handleKeyDown,
    handleKeyUp,
  };
}
