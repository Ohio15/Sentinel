import { useCallback, useEffect, useRef } from 'react';

export interface InputModifiers {
  ctrl?: boolean;
  alt?: boolean;
  shift?: boolean;
  meta?: boolean;
}

export interface InputEvent {
  type: 'mousemove' | 'mousedown' | 'mouseup' | 'keydown' | 'keyup' | 'wheel';
  x?: number;
  y?: number;
  button?: number;
  keyCode?: number;
  key?: string;
  code?: string;
  deltaX?: number;
  deltaY?: number;
  modifiers?: InputModifiers;
}


interface UseInputOptions {
  displayWidth: number;
  displayHeight: number;
  remoteWidth: number;
  remoteHeight: number;
  videoOffsetX?: number;
  videoOffsetY?: number;
  sendInput: (event: InputEvent) => void;
  enabled?: boolean;
}

// Map browser key codes to Windows virtual key codes
const KEY_MAP: Record<string, number> = {
  // Letters
  KeyA: 0x41, KeyB: 0x42, KeyC: 0x43, KeyD: 0x44, KeyE: 0x45,
  KeyF: 0x46, KeyG: 0x47, KeyH: 0x48, KeyI: 0x49, KeyJ: 0x4a,
  KeyK: 0x4b, KeyL: 0x4c, KeyM: 0x4d, KeyN: 0x4e, KeyO: 0x4f,
  KeyP: 0x50, KeyQ: 0x51, KeyR: 0x52, KeyS: 0x53, KeyT: 0x54,
  KeyU: 0x55, KeyV: 0x56, KeyW: 0x57, KeyX: 0x58, KeyY: 0x59,
  KeyZ: 0x5a,

  // Numbers
  Digit0: 0x30, Digit1: 0x31, Digit2: 0x32, Digit3: 0x33, Digit4: 0x34,
  Digit5: 0x35, Digit6: 0x36, Digit7: 0x37, Digit8: 0x38, Digit9: 0x39,

  // Numpad
  Numpad0: 0x60, Numpad1: 0x61, Numpad2: 0x62, Numpad3: 0x63, Numpad4: 0x64,
  Numpad5: 0x65, Numpad6: 0x66, Numpad7: 0x67, Numpad8: 0x68, Numpad9: 0x69,
  NumpadMultiply: 0x6a, NumpadAdd: 0x6b, NumpadSubtract: 0x6d,
  NumpadDecimal: 0x6e, NumpadDivide: 0x6f,

  // Function keys
  F1: 0x70, F2: 0x71, F3: 0x72, F4: 0x73, F5: 0x74, F6: 0x75,
  F7: 0x76, F8: 0x77, F9: 0x78, F10: 0x79, F11: 0x7a, F12: 0x7b,

  // Control keys
  Escape: 0x1b,
  Tab: 0x09,
  CapsLock: 0x14,
  ShiftLeft: 0x10, ShiftRight: 0x10,
  ControlLeft: 0x11, ControlRight: 0x11,
  AltLeft: 0x12, AltRight: 0x12,
  MetaLeft: 0x5b, MetaRight: 0x5c, // Windows keys
  Space: 0x20,
  Enter: 0x0d,
  Backspace: 0x08,
  Delete: 0x2e,
  Insert: 0x2d,
  Home: 0x24,
  End: 0x23,
  PageUp: 0x21,
  PageDown: 0x22,

  // Arrow keys
  ArrowUp: 0x26,
  ArrowDown: 0x28,
  ArrowLeft: 0x25,
  ArrowRight: 0x27,

  // Punctuation
  Semicolon: 0xba,
  Equal: 0xbb,
  Comma: 0xbc,
  Minus: 0xbd,
  Period: 0xbe,
  Slash: 0xbf,
  Backquote: 0xc0,
  BracketLeft: 0xdb,
  Backslash: 0xdc,
  BracketRight: 0xdd,
  Quote: 0xde,

  // Lock keys
  NumLock: 0x90,
  ScrollLock: 0x91,

  // Print/Pause
  PrintScreen: 0x2c,
  Pause: 0x13,

  // Context menu
  ContextMenu: 0x5d,
};

export function useInput(options: UseInputOptions) {
  const { displayWidth, displayHeight, remoteWidth, remoteHeight, videoOffsetX = 0, videoOffsetY = 0, sendInput, enabled = true } = options;

  const containerRef = useRef<HTMLElement | null>(null);

  // Scale local coordinates to remote coordinates (accounting for video offset)
  const scaleCoordinates = useCallback(
    (containerX: number, containerY: number): { x: number; y: number; inBounds: boolean } => {
      // If display size is 0, fall back to assuming 1:1 mapping with remote
      // This prevents clicks from being blocked during initialization
      const effectiveDisplayWidth = displayWidth > 0 ? displayWidth : remoteWidth;
      const effectiveDisplayHeight = displayHeight > 0 ? displayHeight : remoteHeight;

      console.log('[useInput] scaleCoordinates input:', {
        containerX, containerY,
        displayWidth, displayHeight,
        effectiveDisplayWidth, effectiveDisplayHeight,
        remoteWidth, remoteHeight,
        videoOffsetX, videoOffsetY,
      });

      if (effectiveDisplayWidth === 0 || effectiveDisplayHeight === 0) {
        console.warn('[useInput] scaleCoordinates: all sizes are 0!');
        return { x: Math.round(containerX), y: Math.round(containerY), inBounds: true };
      }

      // Adjust for video offset (letterboxing)
      const videoX = containerX - videoOffsetX;
      const videoY = containerY - videoOffsetY;

      // Clamp to video bounds
      const clampedX = Math.max(0, Math.min(videoX, effectiveDisplayWidth));
      const clampedY = Math.max(0, Math.min(videoY, effectiveDisplayHeight));

      const result = {
        x: Math.round((clampedX / effectiveDisplayWidth) * remoteWidth),
        y: Math.round((clampedY / effectiveDisplayHeight) * remoteHeight),
        inBounds: true,
      };

      console.log('[useInput] scaleCoordinates result:', {
        videoX, videoY,
        clampedX, clampedY,
        resultX: result.x, resultY: result.y,
      });

      return result;
    },
    [displayWidth, displayHeight, remoteWidth, remoteHeight, videoOffsetX, videoOffsetY]
  );

  // Get modifiers from keyboard/mouse event
  const getModifiers = useCallback((e: KeyboardEvent | MouseEvent): InputModifiers => {
    return {
      shift: e.shiftKey,
      ctrl: e.ctrlKey,
      alt: e.altKey,
      meta: e.metaKey,
    };
  }, []);

  // Get virtual key code from keyboard event
  const getKeyCode = useCallback((e: KeyboardEvent): number => {
    // First try the code (physical key)
    if (KEY_MAP[e.code]) {
      return KEY_MAP[e.code];
    }
    // Fall back to keyCode (deprecated but still works)
    return e.keyCode || 0;
  }, []);

  // Mouse down handler
  // Note: Handler attachment is now conditional in parent (isConnected && videoReady)
  // So we don't need strict `enabled` check here, but we keep it for safety
  const handleMouseDown = useCallback(
    (e: React.MouseEvent<HTMLElement>) => {
      e.preventDefault();
      const rect = e.currentTarget.getBoundingClientRect();
      const { x, y, inBounds } = scaleCoordinates(e.clientX - rect.left, e.clientY - rect.top);

      console.log('[useInput] mousedown:', { x, y, inBounds, button: e.button, enabled });

      // Skip if completely outside bounds (with margin)
      if (!inBounds) {
        console.log('[useInput] mousedown outside bounds, still sending (edge case)');
        // Still send - let the server decide
      }

      try {
        const mods = getModifiers(e.nativeEvent);
        const event = {
          type: 'mousedown' as const,
          x,
          y,
          button: e.button,
          modifiers: mods,
        };
        console.log('[useInput] Sending mousedown:', JSON.stringify(event));
        sendInput(event);
      } catch (err) {
        console.error('[useInput] Error in mousedown handler:', err);
      }
    },
    [enabled, scaleCoordinates, sendInput, getModifiers]
  );

  // Mouse up handler
  // Note: Handler attachment is now conditional in parent
  const handleMouseUp = useCallback(
    (e: React.MouseEvent<HTMLElement>) => {
      e.preventDefault();
      const rect = e.currentTarget.getBoundingClientRect();
      const { x, y } = scaleCoordinates(e.clientX - rect.left, e.clientY - rect.top);

      console.log('[useInput] mouseup:', { x, y, button: e.button });

      // Always send mouse up (even if outside bounds, to release any drag)
      sendInput({
        type: 'mouseup',
        x,
        y,
        button: e.button,
        modifiers: getModifiers(e.nativeEvent),
      });
    },
    [scaleCoordinates, sendInput, getModifiers]
  );

  // Wheel handler
  // Note: Handler attachment is now conditional in parent
  const handleWheel = useCallback(
    (e: React.WheelEvent<HTMLElement>) => {
      e.preventDefault();
      const rect = e.currentTarget.getBoundingClientRect();
      const { x, y, inBounds } = scaleCoordinates(e.clientX - rect.left, e.clientY - rect.top);

      // Still send wheel events even if slightly outside bounds
      if (!inBounds) {
        console.log('[useInput] wheel outside bounds, sending anyway');
      }

      // Normalize wheel delta (different browsers report different values)
      const deltaX = Math.sign(e.deltaX) * Math.min(Math.abs(e.deltaX), 120);
      const deltaY = Math.sign(e.deltaY) * Math.min(Math.abs(e.deltaY), 120);

      console.log('[useInput] wheel:', { x, y, deltaX, deltaY });

      sendInput({
        type: 'wheel',
        x,
        y,
        deltaX: Math.round(deltaX / 40), // Convert to "notches"
        deltaY: Math.round(deltaY / 40),
        modifiers: getModifiers(e.nativeEvent),
      });
    },
    [scaleCoordinates, sendInput, getModifiers]
  );

  // Context menu handler (prevent default)
  const handleContextMenu = useCallback((e: React.MouseEvent<HTMLElement>) => {
    e.preventDefault();
  }, []);

  // Set container ref for keyboard focus
  const setContainer = useCallback((element: HTMLElement | null) => {
    containerRef.current = element;
  }, []);

  // Keyboard event handlers (attached to window when container is focused)
  useEffect(() => {
    if (!enabled) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      // Only capture when container is focused
      if (!containerRef.current?.contains(document.activeElement) &&
          document.activeElement !== containerRef.current) {
        return;
      }

      // Prevent default for most keys to avoid browser shortcuts
      // Allow some keys like F11 (fullscreen)
      if (e.code !== 'F11') {
        e.preventDefault();
      }

      sendInput({
        type: 'keydown',
        keyCode: getKeyCode(e),
        key: e.key,
        code: e.code,
        modifiers: getModifiers(e),
      });
    };

    const handleKeyUp = (e: KeyboardEvent) => {
      if (!containerRef.current?.contains(document.activeElement) &&
          document.activeElement !== containerRef.current) {
        return;
      }

      if (e.code !== 'F11') {
        e.preventDefault();
      }

      sendInput({
        type: 'keyup',
        keyCode: getKeyCode(e),
        key: e.key,
        code: e.code,
        modifiers: getModifiers(e),
      });
    };

    window.addEventListener('keydown', handleKeyDown, true);
    window.addEventListener('keyup', handleKeyUp, true);

    return () => {
      window.removeEventListener('keydown', handleKeyDown, true);
      window.removeEventListener('keyup', handleKeyUp, true);
    };
  }, [enabled, sendInput, getKeyCode, getModifiers]);

  return {
    handleMouseDown,
    handleMouseUp,
    handleWheel,
    handleContextMenu,
    setContainer,
  };
}
