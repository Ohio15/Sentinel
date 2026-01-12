/**
 * Auth Store - Unified authentication for Electron and Web modes
 *
 * In Electron mode: Authentication is handled by the main process
 * In Web mode: Uses HTTP API for auth, stores token in localStorage
 */
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
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
  login: (identifier: string, password: string) => Promise<void>;
  logout: () => void;
  checkAuth: () => Promise<void>;
  clearError: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      user: null,
      token: null,
      isAuthenticated: isElectron, // Electron is always "authenticated" (main process handles it)
      isLoading: !isElectron, // Only loading in web mode
      error: null,

      login: async (identifier: string, password: string) => {
        if (isElectron) {
          // Electron doesn't use web login
          return;
        }

        set({ isLoading: true, error: null });
        try {
          const response = await auth.login(identifier, password);
          const { token, user } = response as { token: string; user: User };

          set({
            user,
            token,
            isAuthenticated: true,
            isLoading: false,
            error: null,
          });

          // Connect WebSocket after successful login
          connection.connect();
        } catch (err: unknown) {
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

        auth.logout().catch(() => {
          // Ignore logout errors
        });

        set({
          user: null,
          token: null,
          isAuthenticated: false,
          isLoading: false,
          error: null,
        });
      },

      checkAuth: async () => {
        if (isElectron) {
          // Electron handles auth differently
          set({ isAuthenticated: true, isLoading: false });
          return;
        }

        const token = localStorage.getItem('token');
        if (!token) {
          set({ isLoading: false, isAuthenticated: false });
          return;
        }

        try {
          const user = await auth.getCurrentUser();
          set({
            user: user as User,
            token,
            isAuthenticated: true,
            isLoading: false,
          });

          // Connect WebSocket if authenticated
          connection.connect();
        } catch {
          localStorage.removeItem('token');
          set({
            user: null,
            token: null,
            isAuthenticated: false,
            isLoading: false,
          });
        }
      },

      clearError: () => set({ error: null }),
    }),
    {
      name: 'auth-storage',
      partialize: (state) => ({
        token: state.token,
        user: state.user,
      }),
      // Only persist in web mode
      skipHydration: isElectron,
    }
  )
);
