import React, { useMemo } from 'react';
import { CursorState } from './useCursor';

interface CursorOverlayProps {
  cursor: CursorState;
  showRemoteCursor?: boolean; // Debug option to show actual remote position
}

// Built-in cursor SVGs with proper scaling
const CURSORS: Record<string, string> = {
  default: `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">
    <path fill="white" stroke="black" stroke-width="1.5" d="M5.5 3.21V20.8l4.86-4.86h6.36L5.5 3.21z"/>
  </svg>`,
  pointer: `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">
    <path fill="white" stroke="black" stroke-width="1.5" d="M10 6v10.5l2.5-2.5 2 4.5 1.5-.5-2-4.5h3.5L10 6z"/>
  </svg>`,
  text: `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">
    <path fill="none" stroke="black" stroke-width="2" d="M12 4v16M8 4h8M8 20h8"/>
    <path fill="none" stroke="white" stroke-width="1" d="M12 4v16M8 4h8M8 20h8"/>
  </svg>`,
  wait: `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">
    <circle fill="white" stroke="black" stroke-width="1.5" cx="12" cy="12" r="8"/>
    <path stroke="black" stroke-width="2" stroke-linecap="round" d="M12 8v4l3 3"/>
  </svg>`,
  crosshair: `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">
    <circle fill="none" stroke="black" stroke-width="2" cx="12" cy="12" r="6"/>
    <path stroke="black" stroke-width="2" d="M12 2v6M12 16v6M2 12h6M16 12h6"/>
    <circle fill="none" stroke="white" stroke-width="1" cx="12" cy="12" r="6"/>
    <path stroke="white" stroke-width="1" d="M12 2v6M12 16v6M2 12h6M16 12h6"/>
  </svg>`,
  move: `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">
    <path fill="white" stroke="black" stroke-width="1.5" d="M12 2l3 3h-2v4h4v-2l3 3-3 3v-2h-4v4h2l-3 3-3-3h2v-4H6v2l-3-3 3-3v2h4V5H8l4-3z"/>
  </svg>`,
  'not-allowed': `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">
    <circle fill="white" stroke="red" stroke-width="2" cx="12" cy="12" r="8"/>
    <path stroke="red" stroke-width="2" d="M6 18L18 6"/>
  </svg>`,
};

// Default hotspots for built-in cursors
const HOTSPOTS: Record<string, { x: number; y: number }> = {
  default: { x: 5, y: 3 },
  pointer: { x: 10, y: 6 },
  text: { x: 12, y: 12 },
  wait: { x: 12, y: 12 },
  crosshair: { x: 12, y: 12 },
  move: { x: 12, y: 12 },
  'not-allowed': { x: 12, y: 12 },
};

export const CursorOverlay: React.FC<CursorOverlayProps> = ({
  cursor,
  showRemoteCursor = false,
}) => {
  // Generate cursor image URL
  const cursorUrl = useMemo(() => {
    if (cursor.shape.type === 'custom' && cursor.shape.image) {
      return cursor.shape.image;
    }
    const svg = CURSORS[cursor.shape.type] || CURSORS.default;
    return `data:image/svg+xml;base64,${btoa(svg)}`;
  }, [cursor.shape]);

  // Get hotspot for current cursor type
  const hotspot = useMemo(() => {
    if (cursor.shape.type === 'custom') {
      return cursor.shape.hotspot;
    }
    return HOTSPOTS[cursor.shape.type] || HOTSPOTS.default;
  }, [cursor.shape]);

  if (!cursor.visible) return null;

  return (
    <>
      {/* Local cursor (instant feedback) - uses transform for GPU-accelerated smooth movement */}
      <div
        style={{
          position: 'absolute',
          left: 0,
          top: 0,
          width: 24,
          height: 24,
          backgroundImage: `url("${cursorUrl}")`,
          backgroundSize: 'contain',
          backgroundRepeat: 'no-repeat',
          pointerEvents: 'none',
          zIndex: 1000,
          // GPU acceleration for smooth movement - using translate instead of left/top
          willChange: 'transform',
          transform: `translate3d(${cursor.local.x - hotspot.x}px, ${cursor.local.y - hotspot.y}px, 0)`,
        }}
      />

      {/* Remote cursor position indicator (optional, for debugging latency) */}
      {showRemoteCursor && (
        <div
          style={{
            position: 'absolute',
            left: cursor.remote.x - 4,
            top: cursor.remote.y - 4,
            width: 8,
            height: 8,
            borderRadius: '50%',
            backgroundColor: 'rgba(255, 0, 0, 0.5)',
            border: '1px solid rgba(255, 0, 0, 0.8)',
            pointerEvents: 'none',
            zIndex: 999,
            // Show the delay between local cursor (instant) and remote cursor (network latency)
            transition: 'left 0.05s, top 0.05s',
          }}
          title="Remote cursor position (network latency indicator)"
        />
      )}
    </>
  );
};

export default CursorOverlay;
