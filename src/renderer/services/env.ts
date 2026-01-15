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
  // Web mode - check localStorage for persisted backend URL, then env var, then default
  const persistedUrl = typeof localStorage !== 'undefined' ? localStorage.getItem('backend_url') : null;
  if (persistedUrl) {
    return persistedUrl.endsWith('/api') ? persistedUrl : `${persistedUrl}/api`;
  }
  return import.meta.env.VITE_API_URL || '/api';
};

// Get WebSocket base URL for web mode
export const getWsBaseUrl = () => {
  if (isElectron) {
    return '';
  }
  // Check localStorage for persisted backend URL
  const persistedUrl = typeof localStorage !== 'undefined' ? localStorage.getItem('backend_url') : null;
  const baseUrl = persistedUrl || import.meta.env.VITE_API_URL?.replace('/api', '') || window.location.origin;
  try {
    const url = new URL(baseUrl);
    const protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${protocol}//${url.host}`;
  } catch {
    return window.location.origin.replace(/^http/, 'ws');
  }
};

console.log(`[Env] Running in ${isElectron ? 'Electron' : 'Web'} mode`);
