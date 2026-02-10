/**
 * Environment configuration for web-only builds
 */

// Always web mode (no Electron)
export const isElectron = false;
export const isWeb = true;

// Get API base URL
export const getApiBaseUrl = () => {
  // Check localStorage for persisted backend URL, then env var, then default
  const persistedUrl = typeof localStorage !== 'undefined' ? localStorage.getItem('backend_url') : null;
  if (persistedUrl) {
    return persistedUrl.endsWith('/api') ? persistedUrl : `${persistedUrl}/api`;
  }
  return import.meta.env.VITE_API_URL || '/api';
};

// Get WebSocket base URL
export const getWsBaseUrl = () => {
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

console.log('[Env] Running in Web mode');
