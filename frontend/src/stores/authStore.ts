import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { startAuthentication } from '@simplewebauthn/browser';
import api from '@/services/api';
import wsService from '@/services/websocket';
import type { User } from '@/types';

interface AuthState {
  user: User | null;
  token: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;
  login: (identifier: string, password: string) => Promise<void>;
  loginWithPasskey: () => Promise<void>;
  logout: () => void;
  checkAuth: () => Promise<void>;
  clearError: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      user: null,
      token: null,
      isAuthenticated: false,
      isLoading: true,
      error: null,

      login: async (identifier: string, password: string) => {
        set({ isLoading: true, error: null });
        try {
          const response = await api.login(identifier, password);
          const { accessToken, csrfToken, user } = response;

          localStorage.setItem('token', accessToken);
          if (csrfToken) {
            localStorage.setItem('csrfToken', csrfToken);
          }
          set({
            user,
            token: accessToken,
            isAuthenticated: true,
            isLoading: false,
            error: null,
          });

          // Connect WebSocket after successful login
          wsService.connect();
        } catch (err: unknown) {
          const error = err as { response?: { data?: { error?: string } } };
          set({
            user: null,
            token: null,
            isAuthenticated: false,
            isLoading: false,
            error: error.response?.data?.error || 'Login failed',
          });
          throw err;
        }
      },

      loginWithPasskey: async () => {
        set({ isLoading: true, error: null });
        try {
          // Step 1: Begin authentication - get challenge from server
          const beginResponse = await api.beginPasskeyAuthentication();
          const { sessionId, options } = beginResponse;

          // Step 2: Prompt user for passkey via browser API
          const authResponse = await startAuthentication(options);

          // Step 3: Send response to server for verification
          const finishResponse = await api.finishPasskeyAuthentication({
            sessionId,
            response: authResponse,
          });

          const { accessToken, csrfToken, user } = finishResponse;

          localStorage.setItem('token', accessToken);
          if (csrfToken) {
            localStorage.setItem('csrfToken', csrfToken);
          }
          set({
            user,
            token: accessToken,
            isAuthenticated: true,
            isLoading: false,
            error: null,
          });

          // Connect WebSocket after successful login
          wsService.connect();
        } catch (err: unknown) {
          const error = err as { response?: { data?: { error?: string } }; name?: string; message?: string };

          // Handle user cancellation gracefully
          if (error.name === 'NotAllowedError') {
            set({
              isLoading: false,
              error: 'Passkey authentication was cancelled',
            });
            throw err;
          }

          set({
            user: null,
            token: null,
            isAuthenticated: false,
            isLoading: false,
            error: error.response?.data?.error || error.message || 'Passkey authentication failed',
          });
          throw err;
        }
      },

      logout: () => {
        api.logout().catch(() => {
          // Ignore logout errors
        });
        localStorage.removeItem('token');
        localStorage.removeItem('csrfToken');
        wsService.disconnect();
        set({
          user: null,
          token: null,
          isAuthenticated: false,
          isLoading: false,
          error: null,
        });
      },

      checkAuth: async () => {
        const token = localStorage.getItem('token');
        if (!token) {
          set({ isLoading: false, isAuthenticated: false });
          return;
        }

        try {
          const user = await api.getCurrentUser();
          set({
            user,
            token,
            isAuthenticated: true,
            isLoading: false,
          });

          // Connect WebSocket if authenticated
          wsService.connect();
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
        isAuthenticated: state.isAuthenticated,
      }),
      onRehydrateStorage: () => (state) => {
        // After rehydration, validate the session if we think we're authenticated
        if (state?.isAuthenticated && state?.token) {
          // Token exists and was authenticated - validate in background
          state.checkAuth();
        } else {
          // No valid session - ensure loading is false
          if (state) {
            state.isLoading = false;
          }
        }
      },
    }
  )
);
