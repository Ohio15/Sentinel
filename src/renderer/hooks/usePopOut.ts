import { useState, useEffect, useCallback } from 'react';
import { isElectron, isWeb } from '../services/env';

interface PopOutConfig {
  deviceId: string;
  tab: string;
  width?: number;
  height?: number;
}

interface PopOutWindow {
  id: string;
  deviceId: string;
  tab: string;
  createdAt: number;
  windowRef?: Window;
}

interface UsePopOutReturn {
  popOutWindows: PopOutWindow[];
  createPopOut: (config: PopOutConfig) => Promise<string | null>;
  closePopOut: (id: string) => Promise<boolean>;
  reattachPopOut: (id: string) => Promise<boolean>;
  focusPopOut: (id: string) => Promise<boolean>;
  isPopOutOpen: (deviceId: string, tab: string) => boolean;
  onReattachRequest: (callback: (data: { deviceId: string; tab: string }) => void) => () => void;
}

// Track web mode pop-out windows
const webPopOutWindows = new Map<string, Window>();

export function usePopOut(): UsePopOutReturn {
  const [popOutWindows, setPopOutWindows] = useState<PopOutWindow[]>([]);

  // Fetch existing pop-out windows on mount
  useEffect(() => {
    if (isElectron && window.api?.popOut?.list) {
      window.api.popOut.list()
        .then((windows: PopOutWindow[]) => setPopOutWindows(windows))
        .catch(console.error);
    }
  }, []);

  // Listen for web mode reattach requests via localStorage
  useEffect(() => {
    if (!isWeb) return;

    const handleStorage = (e: StorageEvent) => {
      if (e.key === 'popout:reattach' && e.newValue) {
        try {
          const data = JSON.parse(e.newValue);
          // Clear the storage item
          localStorage.removeItem('popout:reattach');
          // Dispatch custom event for listeners
          window.dispatchEvent(new CustomEvent('popout:reattach', { detail: data }));
        } catch (err) {
          console.error('Failed to parse reattach data:', err);
        }
      }
    };

    // Listen for postMessage from pop-out windows
    const handleMessage = (e: MessageEvent) => {
      if (e.origin !== window.location.origin) return;
      if (e.data?.type === 'popout:reattach') {
        window.dispatchEvent(new CustomEvent('popout:reattach', { detail: e.data }));
        // Clean up the closed window from tracking
        const windowId = `web-popout-${e.data.deviceId}-${e.data.tab}`;
        webPopOutWindows.delete(windowId);
        setPopOutWindows(prev => prev.filter(w => w.id !== windowId));
      }
    };

    window.addEventListener('storage', handleStorage);
    window.addEventListener('message', handleMessage);

    return () => {
      window.removeEventListener('storage', handleStorage);
      window.removeEventListener('message', handleMessage);
    };
  }, []);

  // Check for closed web pop-out windows periodically
  useEffect(() => {
    if (!isWeb) return;

    const checkClosedWindows = () => {
      for (const [id, windowRef] of webPopOutWindows.entries()) {
        if (windowRef.closed) {
          webPopOutWindows.delete(id);
          setPopOutWindows(prev => prev.filter(w => w.id !== id));
        }
      }
    };

    const interval = setInterval(checkClosedWindows, 1000);
    return () => clearInterval(interval);
  }, []);

  const createPopOut = useCallback(async (config: PopOutConfig): Promise<string | null> => {
    try {
      if (isElectron && window.api?.popOut?.create) {
        // Electron mode: use IPC
        const result = await window.api.popOut.create(config);
        if (result.success) {
          // Refresh windows list
          const windows = await window.api.popOut.list();
          setPopOutWindows(windows);
          return result.id;
        }
        console.error('Failed to create pop-out:', result.error);
        return null;
      } else if (isWeb) {
        // Web mode: use window.open()
        const id = `web-popout-${config.deviceId}-${config.tab}`;

        // Check if already open
        const existingWindow = webPopOutWindows.get(id);
        if (existingWindow && !existingWindow.closed) {
          existingWindow.focus();
          return id;
        }

        // Calculate window dimensions
        const width = config.width || 1200;
        const height = config.height || 800;
        const left = window.screenX + 50;
        const top = window.screenY + 50;

        // Open new window
        const url = `/popout/performance/${config.deviceId}`;
        const features = `width=${width},height=${height},left=${left},top=${top},menubar=no,toolbar=no,location=no,status=no,resizable=yes,scrollbars=yes`;

        const newWindow = window.open(url, id, features);

        if (newWindow) {
          webPopOutWindows.set(id, newWindow);
          setPopOutWindows(prev => [
            ...prev.filter(w => w.id !== id),
            {
              id,
              deviceId: config.deviceId,
              tab: config.tab,
              createdAt: Date.now(),
              windowRef: newWindow,
            },
          ]);
          return id;
        }

        console.error('Failed to open pop-out window (popup blocked?)');
        return null;
      }

      return null;
    } catch (error) {
      console.error('Error creating pop-out:', error);
      return null;
    }
  }, []);

  const closePopOut = useCallback(async (id: string): Promise<boolean> => {
    try {
      if (isElectron && window.api?.popOut?.close) {
        const result = await window.api.popOut.close(id);
        if (result) {
          setPopOutWindows(prev => prev.filter(w => w.id !== id));
        }
        return result;
      } else if (isWeb) {
        const windowRef = webPopOutWindows.get(id);
        if (windowRef && !windowRef.closed) {
          windowRef.close();
          webPopOutWindows.delete(id);
          setPopOutWindows(prev => prev.filter(w => w.id !== id));
          return true;
        }
        return false;
      }

      return false;
    } catch (error) {
      console.error('Error closing pop-out:', error);
      return false;
    }
  }, []);

  const reattachPopOut = useCallback(async (id: string): Promise<boolean> => {
    try {
      if (isElectron && window.api?.popOut?.reattach) {
        return await window.api.popOut.reattach(id);
      } else if (isWeb) {
        return await closePopOut(id);
      }

      return false;
    } catch (error) {
      console.error('Error reattaching pop-out:', error);
      return false;
    }
  }, [closePopOut]);

  const focusPopOut = useCallback(async (id: string): Promise<boolean> => {
    try {
      if (isElectron && window.api?.popOut?.focus) {
        return await window.api.popOut.focus(id);
      } else if (isWeb) {
        const windowRef = webPopOutWindows.get(id);
        if (windowRef && !windowRef.closed) {
          windowRef.focus();
          return true;
        }
        return false;
      }

      return false;
    } catch (error) {
      console.error('Error focusing pop-out:', error);
      return false;
    }
  }, []);

  const isPopOutOpen = useCallback((deviceId: string, tab: string): boolean => {
    return popOutWindows.some(w => w.deviceId === deviceId && w.tab === tab);
  }, [popOutWindows]);

  const onReattachRequest = useCallback((callback: (data: { deviceId: string; tab: string }) => void): () => void => {
    if (isElectron && window.api?.popOut?.onReattachRequest) {
      // Electron mode: use IPC listener
      return window.api.popOut.onReattachRequest(callback);
    } else if (isWeb) {
      // Web mode: use custom event listener
      const handler = (e: Event) => {
        const customEvent = e as CustomEvent<{ deviceId: string; tab: string }>;
        callback(customEvent.detail);
      };
      window.addEventListener('popout:reattach', handler);
      return () => window.removeEventListener('popout:reattach', handler);
    }

    return () => {};
  }, []);

  return {
    popOutWindows,
    createPopOut,
    closePopOut,
    reattachPopOut,
    focusPopOut,
    isPopOutOpen,
    onReattachRequest,
  };
}

export default usePopOut;
