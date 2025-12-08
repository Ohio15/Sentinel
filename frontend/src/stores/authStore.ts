import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import api from '@/services/api';
import wsService from '@/services/websocket';
import type { User } from '@/types';

interface AuthState {
  user: User | null;
  token: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;
  login: (email: string, password: string) => Promise<void>;
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

      login: async (email: string, password: string) => {
        set({ isLoading: true, error: null });
        try {
          const response = await api.login(email, password);
          const { accessToken, user } = response;

          localStorage.setItem('token', accessToken);
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

      logout: () => {
        api.logout().catch(() => {
          // Ignore logout errors
        });
        localStorage.removeItem('token');
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
      }),
    }
  )
);
