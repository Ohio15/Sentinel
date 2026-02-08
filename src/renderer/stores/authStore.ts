/**
 * Auth Store - Unified authentication for Electron and Web modes
 *
 * In Electron mode: Authentication is handled by the main process
 * In Web mode: Uses HTTP API for auth, stores token in localStorage
 */
import { create } from 'zustand';
import { isElectron, isWeb } from '../services/env';
import { auth, connection } from '../services';
import { api } from '../services/api';

export interface User {
  id: string;
  username: string;
  email: string;
  firstName?: string;
  lastName?: string;
  role: string;
}

interface AuthState {
  user: User | null;
  token: string | null;
  refreshToken: string | null;
  tokenExpiresAt: number | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;
  _authChecked: boolean; // Flag to prevent duplicate checkAuth calls
  _refreshTimer: ReturnType<typeof setTimeout> | null;
  _refreshAttempts: number; // Counter to prevent infinite refresh loops
  _isRefreshing: boolean; // Flag to prevent concurrent refresh attempts
  login: (identifier: string, password: string) => Promise<void>;
  logout: () => void;
  checkAuth: () => Promise<void>;
  clearError: () => void;
  refreshAccessToken: () => Promise<boolean>;
  _scheduleTokenRefresh: (expiresIn: number) => void;
}

// Check for force clear parameter in web mode
if (isWeb && window.location.search.includes('clear')) {
  console.log('[AuthStore] Force clear requested');
  localStorage.removeItem('token');
  localStorage.removeItem('user');
  localStorage.removeItem('auth-storage');
  window.history.replaceState({}, '', window.location.pathname);
}

// Initialize auth state from localStorage synchronously to prevent flash redirect
function getInitialAuthState(): { token: string | null; refreshToken: string | null; tokenExpiresAt: number | null; isAuthenticated: boolean; isLoading: boolean } {
  if (isElectron) {
    return { token: null, refreshToken: null, tokenExpiresAt: null, isAuthenticated: true, isLoading: false };
  }

  const token = localStorage.getItem('token');
  const refreshToken = localStorage.getItem('refreshToken');
  const tokenExpiresAt = localStorage.getItem('tokenExpiresAt');
  if (token) {
    // Token exists - mark as loading until validated, but don't redirect yet
    console.log('[AuthStore] Found token in localStorage, will validate');
    return {
      token,
      refreshToken,
      tokenExpiresAt: tokenExpiresAt ? parseInt(tokenExpiresAt, 10) : null,
      isAuthenticated: false,
      isLoading: true
    };
  }

  console.log('[AuthStore] No token in localStorage');
  return { token: null, refreshToken: null, tokenExpiresAt: null, isAuthenticated: false, isLoading: false };
}

const initialState = getInitialAuthState();

export const useAuthStore = create<AuthState>()((set, get) => ({
  user: null,
  token: initialState.token,
  refreshToken: initialState.refreshToken,
  tokenExpiresAt: initialState.tokenExpiresAt,
  isAuthenticated: initialState.isAuthenticated,
  isLoading: initialState.isLoading, // True if token exists and needs validation
  error: null,
  _authChecked: false, // Prevents duplicate checkAuth calls
  _refreshTimer: null,
  _refreshAttempts: 0, // Prevents infinite refresh loops (max 2 attempts)
  _isRefreshing: false, // Prevents concurrent refresh attempts

  _scheduleTokenRefresh: (expiresIn: number) => {
    const state = get();
    // Clear any existing timer
    if (state._refreshTimer) {
      clearTimeout(state._refreshTimer);
    }

    // Refresh 5 minutes before expiry (or at 80% of lifetime for shorter tokens)
    const refreshTime = Math.min(expiresIn - 300, expiresIn * 0.8) * 1000;
    if (refreshTime <= 0) {
      console.log('[AuthStore] Token expiring too soon, refreshing immediately');
      get().refreshAccessToken();
      return;
    }

    console.log(`[AuthStore] Scheduling token refresh in ${Math.round(refreshTime / 1000 / 60)} minutes`);
    const timer = setTimeout(() => {
      console.log('[AuthStore] Auto-refreshing token...');
      get().refreshAccessToken();
    }, refreshTime);

    set({ _refreshTimer: timer });
  },

  refreshAccessToken: async () => {
    const state = get();

    // Prevent concurrent refresh attempts
    if (state._isRefreshing) {
      console.log('[AuthStore] Refresh already in progress, skipping');
      return false;
    }

    // Prevent infinite refresh loops (max 2 attempts per session)
    if (state._refreshAttempts >= 2) {
      console.log('[AuthStore] Max refresh attempts reached, clearing tokens');
      get().logout();
      return false;
    }

    if (!state.refreshToken) {
      console.log('[AuthStore] No refresh token available');
      return false;
    }

    set({ _isRefreshing: true, _refreshAttempts: state._refreshAttempts + 1 });

    try {
      console.log(`[AuthStore] Refreshing access token (attempt ${state._refreshAttempts + 1}/2)...`);
      const response = await api!.refreshToken(state.refreshToken);
      const { token: newToken, expiresIn } = response;

      const expiresAt = Date.now() + (expiresIn * 1000);
      localStorage.setItem('token', newToken);
      localStorage.setItem('tokenExpiresAt', expiresAt.toString());

      set({
        token: newToken,
        tokenExpiresAt: expiresAt,
        _isRefreshing: false,
        // Reset attempts on successful refresh
        _refreshAttempts: 0,
      });

      // Schedule next refresh
      get()._scheduleTokenRefresh(expiresIn);

      console.log('[AuthStore] Token refreshed successfully');
      return true;
    } catch (err) {
      console.error('[AuthStore] Token refresh failed:', err);
      set({ _isRefreshing: false });
      // If refresh fails, logout
      get().logout();
      return false;
    }
  },

  login: async (identifier: string, password: string) => {
    if (isElectron) {
      return;
    }

    set({ isLoading: true, error: null });
    try {
      console.log('[AuthStore] Logging in...');
      const response = await auth.login(identifier, password);
      const { accessToken, refreshToken, expiresIn, user } = response as { accessToken: string; refreshToken: string; expiresIn: number; user: User };

      console.log('[AuthStore] Login successful');
      const expiresAt = Date.now() + (expiresIn * 1000);
      localStorage.setItem('token', accessToken);
      localStorage.setItem('refreshToken', refreshToken);
      localStorage.setItem('tokenExpiresAt', expiresAt.toString());

      set({
        user,
        token: accessToken,
        refreshToken,
        tokenExpiresAt: expiresAt,
        isAuthenticated: true,
        isLoading: false,
        error: null,
        _refreshAttempts: 0,
        _isRefreshing: false,
      });

      // Schedule token refresh
      get()._scheduleTokenRefresh(expiresIn);

      // Connect WebSocket after successful login
      connection.connect();
    } catch (err: unknown) {
      console.error('[AuthStore] Login failed:', err);
      const error = err as Error;
      set({
        user: null,
        token: null,
        refreshToken: null,
        tokenExpiresAt: null,
        isAuthenticated: false,
        isLoading: false,
        error: error.message || 'Login failed',
      });
      throw err;
    }
  },

  logout: () => {
    if (isElectron) {
      return;
    }

    // Clear refresh timer
    const state = get();
    if (state._refreshTimer) {
      clearTimeout(state._refreshTimer);
    }

    auth.logout().catch(() => {});

    // Clear all auth storage
    localStorage.removeItem('token');
    localStorage.removeItem('refreshToken');
    localStorage.removeItem('tokenExpiresAt');
    localStorage.removeItem('user');
    localStorage.removeItem('auth-storage');

    set({
      user: null,
      token: null,
      refreshToken: null,
      tokenExpiresAt: null,
      isAuthenticated: false,
      isLoading: false,
      error: null,
      _refreshTimer: null,
      _refreshAttempts: 0,
      _isRefreshing: false,
    });
  },

  checkAuth: async () => {
    const state = get();

    // Prevent duplicate calls
    if (state._authChecked) {
      console.log('[AuthStore] checkAuth already completed, skipping');
      return;
    }

    console.log('[AuthStore] checkAuth called');

    if (isElectron) {
      set({ isAuthenticated: true, isLoading: false, _authChecked: true });
      return;
    }

    const token = localStorage.getItem('token');
    const refreshToken = localStorage.getItem('refreshToken');
    const tokenExpiresAt = localStorage.getItem('tokenExpiresAt');
    console.log('[AuthStore] Token in localStorage:', !!token);

    if (!token) {
      console.log('[AuthStore] No token, showing login');
      set({ isLoading: false, isAuthenticated: false, _authChecked: true });
      return;
    }

    // Check if token is expired and we have a refresh token
    const expiresAt = tokenExpiresAt ? parseInt(tokenExpiresAt, 10) : null;
    if (expiresAt && Date.now() > expiresAt && refreshToken) {
      console.log('[AuthStore] Token expired, attempting refresh...');
      set({ refreshToken });
      const refreshed = await get().refreshAccessToken();
      if (!refreshed) {
        console.log('[AuthStore] Refresh failed, showing login');
        set({ isLoading: false, isAuthenticated: false, _authChecked: true });
        return;
      }
    }

    // Only set isLoading if not already true (from initial state)
    if (!state.isLoading) {
      set({ isLoading: true });
    }

    try {
      console.log('[AuthStore] Validating token...');
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), 5000);

      const user = await auth.getCurrentUser();
      clearTimeout(timeoutId);

      console.log('[AuthStore] Token valid');

      // Calculate remaining time and schedule refresh
      const currentExpiresAt = tokenExpiresAt ? parseInt(tokenExpiresAt, 10) : null;
      if (currentExpiresAt) {
        const remainingSeconds = Math.max(0, Math.floor((currentExpiresAt - Date.now()) / 1000));
        if (remainingSeconds > 0) {
          get()._scheduleTokenRefresh(remainingSeconds);
        }
      }

      set({
        user: user as User,
        token,
        refreshToken,
        tokenExpiresAt: currentExpiresAt,
        isAuthenticated: true,
        isLoading: false,
        _authChecked: true,
      });

      connection.connect();
    } catch (err) {
      console.log('[AuthStore] Token invalid:', err);

      // Try to refresh if we have a refresh token AND haven't already tried
      const currentState = get();
      if (refreshToken && currentState._refreshAttempts === 0) {
        console.log('[AuthStore] Attempting token refresh...');
        set({ refreshToken });
        const refreshed = await get().refreshAccessToken();
        if (refreshed) {
          // Retry validation ONE time with the new token
          try {
            console.log('[AuthStore] Validating new token...');
            const user = await auth.getCurrentUser();
            const newToken = localStorage.getItem('token');
            const newExpiresAt = localStorage.getItem('tokenExpiresAt');

            set({
              user: user as User,
              token: newToken,
              isAuthenticated: true,
              isLoading: false,
              _authChecked: true,
            });

            connection.connect();
            return;
          } catch (retryErr) {
            console.log('[AuthStore] New token also invalid, giving up:', retryErr);
            // Fall through to logout
          }
        }
      }

      // Clear everything and show login
      console.log('[AuthStore] Clearing tokens and showing login');
      localStorage.removeItem('token');
      localStorage.removeItem('refreshToken');
      localStorage.removeItem('tokenExpiresAt');
      localStorage.removeItem('auth-storage');
      set({
        user: null,
        token: null,
        refreshToken: null,
        tokenExpiresAt: null,
        isAuthenticated: false,
        isLoading: false,
        _authChecked: true,
        _refreshAttempts: 0,
        _isRefreshing: false,
      });
    }
  },

  clearError: () => set({ error: null }),
}));
