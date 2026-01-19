/**
 * Auth Store - Unified authentication for Electron and Web modes
 *
 * In Electron mode: Authentication is handled by the main process
 * In Web mode: Uses HTTP API for auth, stores token in localStorage
 */
import { create } from 'zustand';
import { isElectron, isWeb } from '../services/env';
import { auth, connection } from '../services';

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
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;
  _authChecked: boolean; // Flag to prevent duplicate checkAuth calls
  login: (identifier: string, password: string) => Promise<void>;
  logout: () => void;
  checkAuth: () => Promise<void>;
  clearError: () => void;
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
function getInitialAuthState(): { token: string | null; isAuthenticated: boolean; isLoading: boolean } {
  if (isElectron) {
    return { token: null, isAuthenticated: true, isLoading: false };
  }

  const token = localStorage.getItem('token');
  if (token) {
    // Token exists - mark as loading until validated, but don't redirect yet
    console.log('[AuthStore] Found token in localStorage, will validate');
    return { token, isAuthenticated: false, isLoading: true };
  }

  console.log('[AuthStore] No token in localStorage');
  return { token: null, isAuthenticated: false, isLoading: false };
}

const initialState = getInitialAuthState();

export const useAuthStore = create<AuthState>()((set, get) => ({
  user: null,
  token: initialState.token,
  isAuthenticated: initialState.isAuthenticated,
  isLoading: initialState.isLoading, // True if token exists and needs validation
  error: null,
  _authChecked: false, // Prevents duplicate checkAuth calls

  login: async (identifier: string, password: string) => {
    if (isElectron) {
      return;
    }

    set({ isLoading: true, error: null });
    try {
      console.log('[AuthStore] Logging in...');
      const response = await auth.login(identifier, password);
      const { accessToken, user } = response as { accessToken: string; user: User };

      console.log('[AuthStore] Login successful');
      localStorage.setItem('token', accessToken);

      set({
        user,
        token: accessToken,
        isAuthenticated: true,
        isLoading: false,
        error: null,
      });

      // Connect WebSocket after successful login
      connection.connect();
    } catch (err: unknown) {
      console.error('[AuthStore] Login failed:', err);
      const error = err as Error;
      set({
        user: null,
        token: null,
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

    auth.logout().catch(() => {});

    // Clear all auth storage
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    localStorage.removeItem('auth-storage');

    set({
      user: null,
      token: null,
      isAuthenticated: false,
      isLoading: false,
      error: null,
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
    console.log('[AuthStore] Token in localStorage:', !!token);

    if (!token) {
      console.log('[AuthStore] No token, showing login');
      set({ isLoading: false, isAuthenticated: false, _authChecked: true });
      return;
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
      set({
        user: user as User,
        token,
        isAuthenticated: true,
        isLoading: false,
        _authChecked: true,
      });

      connection.connect();
    } catch (err) {
      console.log('[AuthStore] Token invalid:', err);
      localStorage.removeItem('token');
      localStorage.removeItem('auth-storage');
      set({
        user: null,
        token: null,
        isAuthenticated: false,
        isLoading: false,
        _authChecked: true,
      });
    }
  },

  clearError: () => set({ error: null }),
}));
