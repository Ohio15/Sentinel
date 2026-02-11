/**
 * Auth Store - Zustand store for authentication state
 * Mirrors the web authStore pattern but uses SecureStore for persistence
 */
import { create } from 'zustand';
import { auth, User } from '../services/auth';

interface AuthState {
  // State
  user: User | null;
  token: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;
  _authChecked: boolean;

  // Actions
  login: (identifier: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  checkAuth: () => Promise<void>;
  clearError: () => void;
  setUser: (user: User | null) => void;
}

export const useAuthStore = create<AuthState>()((set, get) => ({
  // Initial state
  user: null,
  token: null,
  isAuthenticated: false,
  isLoading: true, // Start as loading to prevent flash
  error: null,
  _authChecked: false,

  /**
   * Login with username/email and password
   */
  login: async (identifier: string, password: string) => {
    set({ isLoading: true, error: null });

    try {
      console.log('[AuthStore] Logging in...');
      const response = await auth.login(identifier, password);

      console.log('[AuthStore] Login successful');
      set({
        user: response.user,
        token: response.accessToken,
        isAuthenticated: true,
        isLoading: false,
        error: null,
      });
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

  /**
   * Logout - clear tokens and state
   */
  logout: async () => {
    console.log('[AuthStore] Logging out...');

    try {
      await auth.logout();
    } catch (error) {
      console.log('[AuthStore] Logout error (non-fatal):', error);
    }

    set({
      user: null,
      token: null,
      isAuthenticated: false,
      isLoading: false,
      error: null,
      _authChecked: true,
    });

    console.log('[AuthStore] Logout complete');
  },

  /**
   * Check for existing auth on app start
   * Validates stored token and restores session if valid
   */
  checkAuth: async () => {
    const state = get();

    // Prevent duplicate calls
    if (state._authChecked) {
      console.log('[AuthStore] checkAuth already completed, skipping');
      return;
    }

    console.log('[AuthStore] Checking authentication...');
    set({ isLoading: true });

    try {
      // Check for stored token
      const token = await auth.getStoredToken();

      if (!token) {
        console.log('[AuthStore] No stored token found');
        set({
          user: null,
          token: null,
          isAuthenticated: false,
          isLoading: false,
          _authChecked: true,
        });
        return;
      }

      console.log('[AuthStore] Found stored token, validating...');

      // Validate the token
      const user = await auth.validateToken();

      if (user) {
        console.log('[AuthStore] Token valid, user:', user.username);
        set({
          user,
          token,
          isAuthenticated: true,
          isLoading: false,
          _authChecked: true,
        });
      } else {
        console.log('[AuthStore] Token invalid or expired');
        set({
          user: null,
          token: null,
          isAuthenticated: false,
          isLoading: false,
          _authChecked: true,
        });
      }
    } catch (error) {
      console.error('[AuthStore] checkAuth error:', error);
      set({
        user: null,
        token: null,
        isAuthenticated: false,
        isLoading: false,
        _authChecked: true,
      });
    }
  },

  /**
   * Clear any error message
   */
  clearError: () => set({ error: null }),

  /**
   * Set user (for external updates)
   */
  setUser: (user: User | null) => set({ user }),
}));

// Re-export User type for convenience
export type { User } from '../services/auth';
