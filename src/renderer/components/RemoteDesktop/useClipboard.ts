import { useCallback, useEffect, useRef, useState } from 'react';

// Direction controls which way clipboard data flows
type ClipboardDirection = 'bidirectional' | 'host-to-viewer' | 'viewer-to-host' | 'disabled';

interface ClipboardFormat {
  type: string;
  size: number;
  data?: string;
  truncated?: boolean;
  mimeType?: string;
}

interface ClipboardContent {
  id: string;
  timestamp: number;
  formats: ClipboardFormat[];
  source: 'host' | 'viewer';
}

interface ClipboardUpdateMessage {
  type: 'clipboard.update';
  content: ClipboardContent;
}

interface ClipboardConfigMessage {
  type: 'clipboard.config';
  direction: ClipboardDirection;
}

interface ClipboardRequestMessage {
  type: 'clipboard.request';
  formats: string[];
}

export interface UseClipboardOptions {
  dataChannel: RTCDataChannel | null;
  enabled: boolean;
}

export interface UseClipboardReturn {
  /** Whether clipboard sync is currently active */
  clipboardEnabled: boolean;
  /** Current sync direction */
  direction: ClipboardDirection;
  /** Toggle clipboard between disabled and bidirectional */
  toggleClipboard: () => void;
  /** Set a specific clipboard direction */
  setDirection: (dir: ClipboardDirection) => void;
  /** Manually send current local clipboard to remote */
  sendClipboard: () => Promise<void>;
  /** Request the remote host's clipboard content */
  requestClipboard: () => void;
  /** The last clipboard content received from the remote host */
  lastRemoteClipboard: ClipboardContent | null;
  /** Whether clipboard API permission has been granted */
  permissionGranted: boolean;
}

/**
 * useClipboard manages clipboard synchronization over a WebRTC data channel.
 *
 * Security model:
 * - Disabled by default, requires explicit opt-in via toggleClipboard()
 * - Direction control limits data flow (host-to-viewer, viewer-to-host, bidirectional)
 * - Requires browser Clipboard API permissions (user must grant access)
 * - All clipboard operations are logged to console for audit
 *
 * Usage:
 * ```tsx
 * const { clipboardEnabled, toggleClipboard, sendClipboard, lastRemoteClipboard } = useClipboard({
 *   dataChannel: dcRef.current,
 *   enabled: isConnected,
 * });
 * ```
 */
export function useClipboard(options: UseClipboardOptions): UseClipboardReturn {
  const { dataChannel, enabled } = options;

  const [clipboardEnabled, setClipboardEnabled] = useState(false);
  const [direction, setDirectionState] = useState<ClipboardDirection>('disabled');
  const [lastRemoteClipboard, setLastRemoteClipboard] = useState<ClipboardContent | null>(null);
  const [permissionGranted, setPermissionGranted] = useState(false);

  // Refs to avoid stale closures in event handlers
  const directionRef = useRef<ClipboardDirection>('disabled');
  const dcRef = useRef<RTCDataChannel | null>(null);
  const enabledRef = useRef(false);
  const clipboardEnabledRef = useRef(false);

  // Keep refs in sync with state
  useEffect(() => {
    dcRef.current = dataChannel;
  }, [dataChannel]);

  useEffect(() => {
    enabledRef.current = enabled;
  }, [enabled]);

  useEffect(() => {
    clipboardEnabledRef.current = clipboardEnabled;
    directionRef.current = direction;
  }, [clipboardEnabled, direction]);

  // Check clipboard API permissions on mount
  useEffect(() => {
    async function checkPermission() {
      try {
        // clipboard-read permission check is supported in Chromium-based browsers
        const result = await navigator.permissions.query({
          name: 'clipboard-read' as PermissionName,
        });
        setPermissionGranted(result.state === 'granted');

        result.onchange = () => {
          setPermissionGranted(result.state === 'granted');
        };
      } catch {
        // Firefox and some browsers don't support clipboard-read permission query.
        // We'll try the operation and handle the error there.
        setPermissionGranted(true);
      }
    }

    void checkPermission();
  }, []);

  // Listen for clipboard messages on the data channel
  useEffect(() => {
    if (!dataChannel) return;

    function handleMessage(event: MessageEvent) {
      try {
        const data = JSON.parse(event.data);

        if (!data.type || !data.type.startsWith('clipboard.')) {
          return; // Not a clipboard message, ignore
        }

        switch (data.type) {
          case 'clipboard.update':
            void handleRemoteClipboardUpdate(data as ClipboardUpdateMessage);
            break;

          case 'clipboard.ack':
            console.log('[useClipboard] Received ack for content:', data.id);
            break;

          case 'clipboard.error':
            console.warn('[useClipboard] Remote clipboard error:', data.error, data.details);
            break;

          default:
            console.log('[useClipboard] Unknown clipboard message type:', data.type);
        }
      } catch {
        // Not JSON or not a clipboard message - other handlers will deal with it
      }
    }

    // The data channel may already have an onmessage handler (for input, cursor, etc).
    // We need to chain our handler without clobbering the existing one.
    const existingHandler = dataChannel.onmessage;

    dataChannel.onmessage = (event: MessageEvent) => {
      // Call existing handler first
      if (existingHandler) {
        existingHandler.call(dataChannel, event);
      }
      // Then process clipboard messages
      handleMessage(event);
    };

    return () => {
      // Restore the original handler on cleanup
      if (dataChannel.onmessage) {
        dataChannel.onmessage = existingHandler;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dataChannel]);

  // Handle incoming clipboard update from the remote host
  async function handleRemoteClipboardUpdate(msg: ClipboardUpdateMessage) {
    const currentDirection = directionRef.current;

    if (!clipboardEnabledRef.current) {
      console.log('[useClipboard] Ignoring remote clipboard update: clipboard disabled');
      return;
    }

    // Check if direction allows incoming data from host
    const allowsIncoming =
      currentDirection === 'bidirectional' || currentDirection === 'host-to-viewer';

    if (!allowsIncoming) {
      console.log(
        '[useClipboard] Ignoring remote clipboard update: direction',
        currentDirection,
        'does not allow incoming'
      );
      return;
    }

    const content = msg.content;
    if (!content || !content.formats || content.formats.length === 0) {
      return;
    }

    setLastRemoteClipboard(content);

    // Write text content to local clipboard if available
    const textFormat = content.formats.find(
      (f) => f.type === 'text/plain' && f.data
    );

    if (textFormat && textFormat.data) {
      try {
        await navigator.clipboard.writeText(textFormat.data);
        console.log(
          '[useClipboard] Wrote remote clipboard text to local clipboard:',
          textFormat.data.length,
          'chars'
        );
      } catch (err) {
        console.warn('[useClipboard] Failed to write to local clipboard:', err);
      }
    }
  }

  // Automatic sync: listen for copy/paste events when bidirectional
  useEffect(() => {
    if (!clipboardEnabled || direction !== 'bidirectional') return;

    function handleCopy() {
      // Small delay to let the browser update the clipboard
      setTimeout(() => {
        void (async () => {
          if (!clipboardEnabledRef.current || directionRef.current === 'disabled') return;
          if (
            directionRef.current !== 'bidirectional' &&
            directionRef.current !== 'viewer-to-host'
          )
            return;

          try {
            const text = await navigator.clipboard.readText();
            if (text) {
              sendClipboardText(text);
            }
          } catch (err) {
            console.warn('[useClipboard] Failed to read clipboard after copy:', err);
          }
        })();
      }, 50);
    }

    function handlePaste() {
      // When pasting in bidirectional mode, request the latest remote clipboard
      // so the user gets the most recent content from the host
      if (clipboardEnabledRef.current && directionRef.current === 'bidirectional') {
        requestClipboard();
      }
    }

    document.addEventListener('copy', handleCopy);
    document.addEventListener('paste', handlePaste);

    return () => {
      document.removeEventListener('copy', handleCopy);
      document.removeEventListener('paste', handlePaste);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clipboardEnabled, direction]);

  // Send a text string as a clipboard update to the remote host
  function sendClipboardText(text: string) {
    const dc = dcRef.current;
    if (!dc || dc.readyState !== 'open') return;

    const content: ClipboardContent = {
      id: `viewer-${Date.now()}`,
      timestamp: Date.now(),
      formats: [
        {
          type: 'text/plain',
          size: text.length,
          data: text,
          mimeType: 'text/plain; charset=utf-8',
        },
      ],
      source: 'viewer',
    };

    const msg: ClipboardUpdateMessage = {
      type: 'clipboard.update',
      content,
    };

    try {
      dc.send(JSON.stringify(msg));
      console.log('[useClipboard] Sent clipboard text to remote:', text.length, 'chars');
    } catch (err) {
      console.warn('[useClipboard] Failed to send clipboard update:', err);
    }
  }

  // Send a direction config message to the remote agent
  function sendDirectionConfig(dir: ClipboardDirection) {
    const dc = dcRef.current;
    if (!dc || dc.readyState !== 'open') {
      console.warn('[useClipboard] Cannot send config: data channel not open');
      return;
    }

    const msg: ClipboardConfigMessage = {
      type: 'clipboard.config',
      direction: dir,
    };

    try {
      dc.send(JSON.stringify(msg));
      console.log('[useClipboard] Sent clipboard config: direction =', dir);
    } catch (err) {
      console.warn('[useClipboard] Failed to send clipboard config:', err);
    }
  }

  // Toggle clipboard between disabled and bidirectional
  const toggleClipboard = useCallback(() => {
    const newEnabled = !clipboardEnabledRef.current;
    const newDirection: ClipboardDirection = newEnabled ? 'bidirectional' : 'disabled';

    setClipboardEnabled(newEnabled);
    setDirectionState(newDirection);
    clipboardEnabledRef.current = newEnabled;
    directionRef.current = newDirection;

    sendDirectionConfig(newDirection);

    console.log('[useClipboard] Toggled clipboard:', newEnabled ? 'enabled (bidirectional)' : 'disabled');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Set a specific direction
  const setDirection = useCallback((dir: ClipboardDirection) => {
    const isEnabled = dir !== 'disabled';

    setClipboardEnabled(isEnabled);
    setDirectionState(dir);
    clipboardEnabledRef.current = isEnabled;
    directionRef.current = dir;

    sendDirectionConfig(dir);

    console.log('[useClipboard] Set direction:', dir);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Manually send the current local clipboard to the remote host
  const sendClipboard = useCallback(async () => {
    const currentDirection = directionRef.current;
    if (!clipboardEnabledRef.current) {
      console.warn('[useClipboard] Cannot send clipboard: disabled');
      return;
    }

    const allowsOutgoing =
      currentDirection === 'bidirectional' || currentDirection === 'viewer-to-host';

    if (!allowsOutgoing) {
      console.warn('[useClipboard] Cannot send clipboard: direction', currentDirection, 'does not allow outgoing');
      return;
    }

    try {
      const text = await navigator.clipboard.readText();
      if (text) {
        sendClipboardText(text);
      } else {
        console.log('[useClipboard] Local clipboard is empty or has no text');
      }
    } catch (err) {
      console.warn('[useClipboard] Failed to read local clipboard:', err);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Request the remote host's clipboard content
  const requestClipboard = useCallback(() => {
    const dc = dcRef.current;
    if (!dc || dc.readyState !== 'open') {
      console.warn('[useClipboard] Cannot request clipboard: data channel not open');
      return;
    }

    if (!clipboardEnabledRef.current) {
      console.warn('[useClipboard] Cannot request clipboard: disabled');
      return;
    }

    const msg: ClipboardRequestMessage = {
      type: 'clipboard.request',
      formats: ['text/plain', 'text/html'],
    };

    try {
      dc.send(JSON.stringify(msg));
      console.log('[useClipboard] Requested remote clipboard');
    } catch (err) {
      console.warn('[useClipboard] Failed to send clipboard request:', err);
    }
  }, []);

  // Disable clipboard sync when the connection drops
  useEffect(() => {
    if (!enabled && clipboardEnabled) {
      setClipboardEnabled(false);
      setDirectionState('disabled');
      clipboardEnabledRef.current = false;
      directionRef.current = 'disabled';
      setLastRemoteClipboard(null);
      console.log('[useClipboard] Connection lost, clipboard disabled');
    }
  }, [enabled, clipboardEnabled]);

  return {
    clipboardEnabled,
    direction,
    toggleClipboard,
    setDirection,
    sendClipboard,
    requestClipboard,
    lastRemoteClipboard,
    permissionGranted,
  };
}
