/**
 * Environment detection for dual Electron/Web builds
 */

// Check if running in Electron (window.api is provided by preload script)
export const isElectron = typeof window !== 'undefined' && typeof (window as any).api !== 'undefined';

// Check if running in a web browser
export const isWeb = !isElectron;

// Get API base URL for web mode
export const getApiBaseUrl = () => {
  if (isElectron) {
    // Electron mode - API calls go through IPC, this shouldn't be used
    return '';
  }
  // Web mode - use environment variable or default to same origin
  return import.meta.env.VITE_API_URL || '/api';
};

// Get WebSocket base URL for web mode
export const getWsBaseUrl = () => {
  if (isElectron) {
    return '';
  }
  const apiUrl = import.meta.env.VITE_API_URL?.replace('/api', '') || window.location.origin;
  const url = new URL(apiUrl);
  const protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${url.host}`;
};

console.log(`[Env] Running in ${isElectron ? 'Electron' : 'Web'} mode`);
