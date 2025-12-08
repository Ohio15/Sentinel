import axios, { AxiosError, AxiosInstance } from 'axios';
import type { ApiError } from '@/types';

const API_BASE_URL = import.meta.env.VITE_API_URL || '/api';

class ApiService {
  private client: AxiosInstance;

  constructor() {
    this.client = axios.create({
      baseURL: API_BASE_URL,
      headers: {
        'Content-Type': 'application/json',
      },
    });

    // Request interceptor to add auth token
    this.client.interceptors.request.use(
      (config) => {
        const token = localStorage.getItem('token');
        if (token) {
          config.headers.Authorization = `Bearer ${token}`;
        }
        return config;
      },
      (error) => Promise.reject(error)
    );

    // Response interceptor for error handling
    this.client.interceptors.response.use(
      (response) => response,
      (error: AxiosError<ApiError>) => {
        if (error.response?.status === 401) {
          // Don't redirect if we're already on the login page or if this is the login request
          const isLoginRequest = error.config?.url?.includes('/auth/login');
          const isOnLoginPage = window.location.pathname === '/login';

          if (!isLoginRequest && !isOnLoginPage) {
            localStorage.removeItem('token');
            localStorage.removeItem('user');
            localStorage.removeItem('auth-storage');
            window.location.href = '/login';
          }
        }
        return Promise.reject(error);
      }
    );
  }

  // Auth endpoints
  async login(email: string, password: string) {
    const response = await this.client.post('/auth/login', { email, password });
    return response.data;
  }

  async logout() {
    await this.client.post('/auth/logout');
  }

  async refreshToken() {
    const response = await this.client.post('/auth/refresh');
    return response.data;
  }

  async getCurrentUser() {
    const response = await this.client.get('/auth/me');
    return response.data;
  }

  // Device endpoints
  async getDevices(params?: { status?: string; search?: string; page?: number; pageSize?: number }) {
    const response = await this.client.get('/devices', { params });
    return response.data;
  }

  async getDevice(id: string) {
    const response = await this.client.get(`/devices/${id}`);
    return response.data;
  }

  async updateDevice(id: string, data: { displayName?: string; tags?: string[] }) {
    const response = await this.client.put(`/devices/${id}`, data);
    return response.data;
  }

  async deleteDevice(id: string) {
    await this.client.delete(`/devices/${id}`);
  }

  async getDeviceMetrics(id: string, params?: { from?: string; to?: string; limit?: number }) {
    const response = await this.client.get(`/devices/${id}/metrics`, { params });
    return response.data;
  }

  async executeCommand(deviceId: string, commandType: string, payload: Record<string, unknown>) {
    const response = await this.client.post(`/devices/${deviceId}/commands`, {
      commandType,
      payload,
    });
    return response.data;
  }

  async getDeviceCommands(deviceId: string, params?: { status?: string; page?: number; pageSize?: number }) {
    const response = await this.client.get(`/devices/${deviceId}/commands`, { params });
    return response.data;
  }

  // Script endpoints
  async getScripts(params?: { language?: string; search?: string; page?: number; pageSize?: number }) {
    const response = await this.client.get('/scripts', { params });
    return response.data;
  }

  async getScript(id: string) {
    const response = await this.client.get(`/scripts/${id}`);
    return response.data;
  }

  async createScript(data: {
    name: string;
    description?: string;
    language: string;
    content: string;
    osTypes: string[];
    parameters?: unknown[];
  }) {
    const response = await this.client.post('/scripts', data);
    return response.data;
  }

  async updateScript(id: string, data: Partial<{
    name: string;
    description: string;
    content: string;
    osTypes: string[];
    parameters: unknown[];
  }>) {
    const response = await this.client.put(`/scripts/${id}`, data);
    return response.data;
  }

  async deleteScript(id: string) {
    await this.client.delete(`/scripts/${id}`);
  }

  async runScript(scriptId: string, deviceIds: string[], parameters?: Record<string, unknown>) {
    const response = await this.client.post(`/scripts/${scriptId}/run`, {
      deviceIds,
      parameters,
    });
    return response.data;
  }

  // Alert endpoints
  async getAlerts(params?: { status?: string; severity?: string; page?: number; pageSize?: number }) {
    const response = await this.client.get('/alerts', { params });
    return response.data;
  }

  async getAlert(id: string) {
    const response = await this.client.get(`/alerts/${id}`);
    return response.data;
  }

  async acknowledgeAlert(id: string) {
    const response = await this.client.post(`/alerts/${id}/acknowledge`);
    return response.data;
  }

  async resolveAlert(id: string) {
    const response = await this.client.post(`/alerts/${id}/resolve`);
    return response.data;
  }

  // Alert rule endpoints
  async getAlertRules() {
    const response = await this.client.get('/alert-rules');
    return response.data;
  }

  async getAlertRule(id: string) {
    const response = await this.client.get(`/alert-rules/${id}`);
    return response.data;
  }

  async createAlertRule(data: {
    name: string;
    description?: string;
    conditions: unknown[];
    severity: string;
    cooldownMinutes?: number;
  }) {
    const response = await this.client.post('/alert-rules', data);
    return response.data;
  }

  async updateAlertRule(id: string, data: Partial<{
    name: string;
    description: string;
    enabled: boolean;
    conditions: unknown[];
    severity: string;
    cooldownMinutes: number;
  }>) {
    const response = await this.client.put(`/alert-rules/${id}`, data);
    return response.data;
  }

  async deleteAlertRule(id: string) {
    await this.client.delete(`/alert-rules/${id}`);
  }

  // Dashboard stats
  async getDashboardStats() {
    const response = await this.client.get('/dashboard/stats');
    return response.data;
  }

  // Settings endpoints
  async getSettings() {
    const response = await this.client.get('/settings');
    return response.data;
  }

  async updateSettings(data: Record<string, unknown>) {
    const response = await this.client.put('/settings', data);
    return response.data;
  }

  // User management endpoints
  async getUsers(params?: { search?: string; page?: number; pageSize?: number }) {
    const response = await this.client.get('/users', { params });
    return response.data;
  }

  async createUser(data: {
    email: string;
    password: string;
    firstName: string;
    lastName: string;
    role: string;
  }) {
    const response = await this.client.post('/users', data);
    return response.data;
  }

  async updateUser(id: string, data: Partial<{
    firstName: string;
    lastName: string;
    role: string;
    password: string;
  }>) {
    const response = await this.client.put(`/users/${id}`, data);
    return response.data;
  }

  async deleteUser(id: string) {
    await this.client.delete(`/users/${id}`);
  }

  // Enrollment Token endpoints
  async getEnrollmentTokens() {
    const response = await this.client.get('/enrollment-tokens');
    return response.data;
  }

  async getEnrollmentToken(id: string) {
    const response = await this.client.get(`/enrollment-tokens/${id}`);
    return response.data;
  }

  async createEnrollmentToken(data: {
    name: string;
    description?: string;
    expiresAt?: string;
    maxUses?: number;
    tags?: string[];
    metadata?: Record<string, string>;
  }) {
    const response = await this.client.post('/enrollment-tokens', data);
    return response.data;
  }

  async updateEnrollmentToken(id: string, data: Partial<{
    name: string;
    description: string;
    isActive: boolean;
    expiresAt: string;
    maxUses: number;
    tags: string[];
    metadata: Record<string, string>;
  }>) {
    const response = await this.client.put(`/enrollment-tokens/${id}`, data);
    return response.data;
  }

  async deleteEnrollmentToken(id: string) {
    await this.client.delete(`/enrollment-tokens/${id}`);
  }

  async regenerateEnrollmentToken(id: string) {
    const response = await this.client.post(`/enrollment-tokens/${id}/regenerate`);
    return response.data;
  }

  // Agent Installers
  async getAgentInstallers() {
    const response = await this.client.get('/agents/installers');
    return response.data;
  }

  getAgentDownloadUrl(platform: string, arch: string, token: string) {
    const baseUrl = import.meta.env.VITE_API_URL?.replace('/api', '') || window.location.origin;
    return `${baseUrl}/api/agents/download/${platform}/${arch}?token=${encodeURIComponent(token)}`;
  }

  getAgentScriptUrl(platform: string, token: string) {
    const baseUrl = import.meta.env.VITE_API_URL?.replace('/api', '') || window.location.origin;
    return `${baseUrl}/api/agents/script/${platform}?token=${encodeURIComponent(token)}`;
  }
}

export const api = new ApiService();
export default api;
