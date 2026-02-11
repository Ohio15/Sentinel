/**
 * Auth Service for Sentinel Mobile
 * Handles authentication operations using SecureStore for token persistence
 */
import * as SecureStore from 'expo-secure-store';
import { api } from './api';

// Token storage keys
const TOKEN_KEY = 'accessToken';
const REFRESH_TOKEN_KEY = 'refreshToken';
const TOKEN_EXPIRES_KEY = 'tokenExpiresAt';
const USER_KEY = 'user';

export interface User {
  id: string;
  username: string;
  email: string;
  firstName?: string;
  lastName?: string;
  role: string;
}

export interface LoginResponse {
  accessToken: string;
  refreshToken: string;
  expiresIn: number;
  user: User;
}

export interface TokenData {
  accessToken: string | null;
  refreshToken: string | null;
  expiresAt: number | null;
}

class AuthService {
  /**
   * Login with username/email and password
   */
  async login(identifier: string, password: string): Promise<LoginResponse> {
    console.log('[Auth] Attempting login...');

    const response = await api.login(identifier, password);

    // Calculate expiration timestamp
    const expiresAt = Date.now() + (response.expiresIn * 1000);

    // Save tokens and user data
    await this.saveTokens(response.accessToken, response.refreshToken, expiresAt);
    await this.saveUser(response.user);

    console.log('[Auth] Login successful');
    return response;
  }

  /**
   * Logout - clear tokens locally and notify server
   */
  async logout(): Promise<void> {
    console.log('[Auth] Logging out...');

    try {
      // Notify server (best effort)
      await api.logout();
    } catch (error) {
      console.log('[Auth] Server logout failed, continuing with local logout');
    }

    // Clear all stored auth data
    await this.clearTokens();
    await this.clearUser();

    console.log('[Auth] Logout complete');
  }

  /**
   * Refresh the access token
   */
  async refreshToken(): Promise<string | null> {
    console.log('[Auth] Refreshing token...');
    return api.refreshAccessToken();
  }

  /**
   * Get the stored access token
   */
  async getStoredToken(): Promise<string | null> {
    try {
      return await SecureStore.getItemAsync(TOKEN_KEY);
    } catch (error) {
      console.error('[Auth] Failed to get stored token:', error);
      return null;
    }
  }

  /**
   * Get all stored token data
   */
  async getTokenData(): Promise<TokenData> {
    try {
      const [accessToken, refreshToken, expiresAtStr] = await Promise.all([
        SecureStore.getItemAsync(TOKEN_KEY),
        SecureStore.getItemAsync(REFRESH_TOKEN_KEY),
        SecureStore.getItemAsync(TOKEN_EXPIRES_KEY),
      ]);

      return {
        accessToken,
        refreshToken,
        expiresAt: expiresAtStr ? parseInt(expiresAtStr, 10) : null,
      };
    } catch (error) {
      console.error('[Auth] Failed to get token data:', error);
      return { accessToken: null, refreshToken: null, expiresAt: null };
    }
  }

  /**
   * Save tokens to secure storage
   */
  async saveTokens(accessToken: string, refreshToken: string, expiresAt: number): Promise<void> {
    try {
      await Promise.all([
        SecureStore.setItemAsync(TOKEN_KEY, accessToken),
        SecureStore.setItemAsync(REFRESH_TOKEN_KEY, refreshToken),
        SecureStore.setItemAsync(TOKEN_EXPIRES_KEY, expiresAt.toString()),
      ]);
      console.log('[Auth] Tokens saved to secure storage');
    } catch (error) {
      console.error('[Auth] Failed to save tokens:', error);
      throw new Error('Failed to save authentication tokens');
    }
  }

  /**
   * Clear all tokens from secure storage
   */
  async clearTokens(): Promise<void> {
    try {
      await Promise.all([
        SecureStore.deleteItemAsync(TOKEN_KEY),
        SecureStore.deleteItemAsync(REFRESH_TOKEN_KEY),
        SecureStore.deleteItemAsync(TOKEN_EXPIRES_KEY),
      ]);
      console.log('[Auth] Tokens cleared from secure storage');
    } catch (error) {
      console.error('[Auth] Failed to clear tokens:', error);
    }
  }

  /**
   * Save user data to secure storage
   */
  async saveUser(user: User): Promise<void> {
    try {
      await SecureStore.setItemAsync(USER_KEY, JSON.stringify(user));
    } catch (error) {
      console.error('[Auth] Failed to save user:', error);
    }
  }

  /**
   * Get stored user data
   */
  async getStoredUser(): Promise<User | null> {
    try {
      const userStr = await SecureStore.getItemAsync(USER_KEY);
      return userStr ? JSON.parse(userStr) : null;
    } catch (error) {
      console.error('[Auth] Failed to get stored user:', error);
      return null;
    }
  }

  /**
   * Clear user data from secure storage
   */
  async clearUser(): Promise<void> {
    try {
      await SecureStore.deleteItemAsync(USER_KEY);
    } catch (error) {
      console.error('[Auth] Failed to clear user:', error);
    }
  }

  /**
   * Check if token is expired
   */
  async isTokenExpired(): Promise<boolean> {
    try {
      const expiresAtStr = await SecureStore.getItemAsync(TOKEN_EXPIRES_KEY);
      if (!expiresAtStr) return true;

      const expiresAt = parseInt(expiresAtStr, 10);
      // Add 1 minute buffer
      return Date.now() > (expiresAt - 60000);
    } catch {
      return true;
    }
  }

  /**
   * Validate the current token by calling the /auth/me endpoint
   */
  async validateToken(): Promise<User | null> {
    try {
      const token = await this.getStoredToken();
      if (!token) {
        console.log('[Auth] No token to validate');
        return null;
      }

      console.log('[Auth] Validating token...');
      const user = await api.getCurrentUser();

      // Update stored user data
      await this.saveUser(user);

      console.log('[Auth] Token valid, user:', user.username);
      return user;
    } catch (error) {
      console.log('[Auth] Token validation failed:', error);

      // Try to refresh the token
      const refreshed = await this.refreshToken();
      if (refreshed) {
        try {
          const user = await api.getCurrentUser();
          await this.saveUser(user);
          return user;
        } catch {
          console.log('[Auth] Validation after refresh failed');
        }
      }

      // Clear invalid tokens
      await this.clearTokens();
      await this.clearUser();
      return null;
    }
  }
}

// Export singleton instance
export const auth = new AuthService();
export default auth;
