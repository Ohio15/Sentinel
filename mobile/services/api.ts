/**
 * API Service for Sentinel Mobile
 * Handles all HTTP requests with auth token injection and error handling
 */
import * as SecureStore from 'expo-secure-store';
import Constants from 'expo-constants';

const API_BASE_URL =
  Constants.expoConfig?.extra?.apiUrl ||
  process.env.EXPO_PUBLIC_API_URL ||
  'https://sentinelrmm.us/api';

interface ApiError {
  message: string;
  code?: string;
  status?: number;
}

interface ApiResponse<T> {
  data?: T;
  error?: ApiError;
}

type HttpMethod = 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH';

class ApiService {
  private baseUrl: string;
  private refreshPromise: Promise<string | null> | null = null;

  constructor() {
    this.baseUrl = API_BASE_URL;
    console.log('[API] Initialized with base URL:', this.baseUrl);
  }

  /**
   * Get stored access token
   */
  private async getToken(): Promise<string | null> {
    try {
      return await SecureStore.getItemAsync('accessToken');
    } catch (error) {
      console.error('[API] Failed to get token:', error);
      return null;
    }
  }

  /**
   * Get stored refresh token
   */
  private async getRefreshToken(): Promise<string | null> {
    try {
      return await SecureStore.getItemAsync('refreshToken');
    } catch (error) {
      console.error('[API] Failed to get refresh token:', error);
      return null;
    }
  }

  /**
   * Save tokens to secure storage
   */
  async saveTokens(accessToken: string, refreshToken: string, expiresAt: number): Promise<void> {
    try {
      await SecureStore.setItemAsync('accessToken', accessToken);
      await SecureStore.setItemAsync('refreshToken', refreshToken);
      await SecureStore.setItemAsync('tokenExpiresAt', expiresAt.toString());
    } catch (error) {
      console.error('[API] Failed to save tokens:', error);
      throw error;
    }
  }

  /**
   * Clear all stored tokens
   */
  async clearTokens(): Promise<void> {
    try {
      await SecureStore.deleteItemAsync('accessToken');
      await SecureStore.deleteItemAsync('refreshToken');
      await SecureStore.deleteItemAsync('tokenExpiresAt');
    } catch (error) {
      console.error('[API] Failed to clear tokens:', error);
    }
  }

  /**
   * Refresh the access token using the refresh token
   * Implements single-flight pattern to prevent multiple concurrent refreshes
   */
  async refreshAccessToken(): Promise<string | null> {
    // If refresh is already in progress, wait for it
    if (this.refreshPromise) {
      return this.refreshPromise;
    }

    this.refreshPromise = this._doRefresh();

    try {
      return await this.refreshPromise;
    } finally {
      this.refreshPromise = null;
    }
  }

  private async _doRefresh(): Promise<string | null> {
    const refreshToken = await this.getRefreshToken();
    if (!refreshToken) {
      console.log('[API] No refresh token available');
      return null;
    }

    try {
      console.log('[API] Refreshing access token...');
      const response = await fetch(`${this.baseUrl}/auth/refresh`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ refreshToken }),
      });

      if (!response.ok) {
        console.log('[API] Token refresh failed:', response.status);
        await this.clearTokens();
        return null;
      }

      const data = await response.json();
      const { token: newToken, expiresIn } = data;
      const expiresAt = Date.now() + (expiresIn * 1000);

      // Save new token (refresh token stays the same)
      await SecureStore.setItemAsync('accessToken', newToken);
      await SecureStore.setItemAsync('tokenExpiresAt', expiresAt.toString());

      console.log('[API] Token refreshed successfully');
      return newToken;
    } catch (error) {
      console.error('[API] Token refresh error:', error);
      await this.clearTokens();
      return null;
    }
  }

  /**
   * Check if token is expired or about to expire
   */
  private async isTokenExpired(): Promise<boolean> {
    try {
      const expiresAtStr = await SecureStore.getItemAsync('tokenExpiresAt');
      if (!expiresAtStr) return true;

      const expiresAt = parseInt(expiresAtStr, 10);
      // Consider token expired if it has less than 1 minute of life
      return Date.now() > (expiresAt - 60000);
    } catch {
      return true;
    }
  }

  /**
   * Make an authenticated HTTP request
   */
  private async request<T>(
    method: HttpMethod,
    endpoint: string,
    data?: unknown,
    params?: Record<string, string>,
    skipAuth: boolean = false
  ): Promise<T> {
    let token = skipAuth ? null : await this.getToken();

    // Check if token needs refresh (except for auth endpoints)
    if (token && !skipAuth && !endpoint.includes('/auth/')) {
      const expired = await this.isTokenExpired();
      if (expired) {
        console.log('[API] Token expired, attempting refresh...');
        token = await this.refreshAccessToken();
        if (!token) {
          throw new Error('Session expired. Please log in again.');
        }
      }
    }

    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };

    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }

    let url = `${this.baseUrl}${endpoint}`;
    if (params) {
      const searchParams = new URLSearchParams(params);
      url += `?${searchParams.toString()}`;
    }

    console.log(`[API] ${method} ${endpoint}`);

    const response = await fetch(url, {
      method,
      headers,
      body: data ? JSON.stringify(data) : undefined,
    });

    // Handle 401 - try to refresh token once
    if (response.status === 401 && !skipAuth && !endpoint.includes('/auth/')) {
      console.log('[API] Got 401, attempting token refresh...');
      token = await this.refreshAccessToken();

      if (token) {
        // Retry the request with new token
        headers['Authorization'] = `Bearer ${token}`;
        const retryResponse = await fetch(url, {
          method,
          headers,
          body: data ? JSON.stringify(data) : undefined,
        });

        if (!retryResponse.ok) {
          const error = await retryResponse.json().catch(() => ({ message: 'Request failed' }));
          throw new Error(error.message || 'Request failed');
        }

        const text = await retryResponse.text();
        return (text ? JSON.parse(text) : null) as T;
      } else {
        throw new Error('Session expired. Please log in again.');
      }
    }

    if (!response.ok) {
      const error: ApiError = await response.json().catch(() => ({
        message: 'Request failed',
        status: response.status
      }));
      throw new Error(error.message || `Request failed with status ${response.status}`);
    }

    // Handle empty responses
    const text = await response.text();
    return (text ? JSON.parse(text) : null) as T;
  }

  /**
   * HTTP GET request
   */
  async get<T>(endpoint: string, params?: Record<string, string>): Promise<T> {
    return this.request<T>('GET', endpoint, undefined, params);
  }

  /**
   * HTTP POST request
   */
  async post<T>(endpoint: string, data?: unknown): Promise<T> {
    return this.request<T>('POST', endpoint, data);
  }

  /**
   * HTTP PUT request
   */
  async put<T>(endpoint: string, data?: unknown): Promise<T> {
    return this.request<T>('PUT', endpoint, data);
  }

  /**
   * HTTP DELETE request
   */
  async delete<T>(endpoint: string): Promise<T> {
    return this.request<T>('DELETE', endpoint);
  }

  /**
   * HTTP PATCH request
   */
  async patch<T>(endpoint: string, data?: unknown): Promise<T> {
    return this.request<T>('PATCH', endpoint, data);
  }

  // ============================================
  // Auth endpoints (skip automatic auth handling)
  // ============================================

  async login(identifier: string, password: string) {
    return this.request<{
      accessToken: string;
      refreshToken: string;
      expiresIn: number;
      user: {
        id: string;
        username: string;
        email: string;
        firstName?: string;
        lastName?: string;
        role: string;
      };
    }>('POST', '/auth/login', { identifier, password }, undefined, true);
  }

  async logout() {
    try {
      await this.request('POST', '/auth/logout');
    } catch {
      // Ignore logout errors - we'll clear tokens locally anyway
    }
    await this.clearTokens();
  }

  async getCurrentUser() {
    return this.get<{
      id: string;
      username: string;
      email: string;
      firstName?: string;
      lastName?: string;
      role: string;
    }>('/auth/me');
  }

  // ============================================
  // Device endpoints
  // ============================================

  async getDevices(params?: { status?: string; search?: string; page?: number; pageSize?: number }) {
    const stringParams: Record<string, string> = {};
    if (params?.status) stringParams.status = params.status;
    if (params?.search) stringParams.search = params.search;
    if (params?.page) stringParams.page = String(params.page);
    if (params?.pageSize) stringParams.pageSize = String(params.pageSize);
    return this.get<{
      devices: Array<{
        id: string;
        hostname: string;
        displayName?: string;
        status: string;
        osType: string;
        osVersion?: string;
        lastSeen?: string;
        ipAddress?: string;
        cpuUsage?: number;
        memoryUsage?: number;
        diskUsage?: number;
      }>;
      total: number;
      page: number;
      pageSize: number;
    }>('/devices', Object.keys(stringParams).length ? stringParams : undefined);
  }

  async getDevice(id: string) {
    return this.get<{
      id: string;
      hostname: string;
      displayName?: string;
      status: string;
      osType: string;
      osVersion?: string;
      lastSeen?: string;
      ipAddress?: string;
      cpuUsage?: number;
      memoryUsage?: number;
      diskUsage?: number;
      tags?: string[];
    }>(`/devices/${id}`);
  }

  async pingAgent(deviceId: string) {
    return this.post<{ success: boolean; latency?: number }>(`/devices/${deviceId}/ping`);
  }

  async devicePowerAction(deviceId: string, action: 'shutdown' | 'restart' | 'wake') {
    return this.post<{ success: boolean }>(`/devices/${deviceId}/power`, { action });
  }

  // ============================================
  // Alert endpoints
  // ============================================

  async getAlerts(params?: { status?: string; severity?: string }) {
    const stringParams: Record<string, string> = {};
    if (params?.status) stringParams.status = params.status;
    if (params?.severity) stringParams.severity = params.severity;
    return this.get<{
      alerts: Array<{
        id: string;
        title: string;
        message: string;
        severity: string;
        status: string;
        deviceId?: string;
        deviceName?: string;
        ruleId?: string;
        createdAt: string;
        acknowledgedAt?: string;
        resolvedAt?: string;
        acknowledgedBy?: string;
        resolvedBy?: string;
      }>;
      total: number;
    }>('/alerts', Object.keys(stringParams).length ? stringParams : undefined);
  }

  async getAlert(id: string) {
    return this.get<{
      id: string;
      title: string;
      message: string;
      severity: string;
      status: string;
      deviceId?: string;
      deviceName?: string;
      ruleId?: string;
      createdAt: string;
      acknowledgedAt?: string;
      resolvedAt?: string;
      acknowledgedBy?: string;
      resolvedBy?: string;
    }>(`/alerts/${id}`);
  }

  async acknowledgeAlert(id: string) {
    return this.post(`/alerts/${id}/acknowledge`);
  }

  async resolveAlert(id: string) {
    return this.post(`/alerts/${id}/resolve`);
  }

  // ============================================
  // Dashboard endpoints
  // ============================================

  async getDashboardStats() {
    return this.get<{
      totalDevices: number;
      onlineDevices: number;
      offlineDevices: number;
      activeAlerts: number;
      criticalAlerts: number;
    }>('/dashboard/stats');
  }
}

// Export singleton instance
export const api = new ApiService();
export default api;
