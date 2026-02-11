/**
 * API Service for Web mode
 */
import { getApiBaseUrl } from './env';

interface ApiError {
  message: string;
  code?: string;
}

class ApiService {
  private baseUrl: string;

  constructor() {
    this.baseUrl = getApiBaseUrl();
  }

  private getCsrfToken(): string | null {
    const match = document.cookie.match(/csrf_token=([^;]+)/);
    return match ? match[1] : null;
  }

  private async request<T>(
    method: string,
    endpoint: string,
    data?: unknown,
    params?: Record<string, string>
  ): Promise<T> {
    const token = localStorage.getItem('token');
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }

    // Add CSRF token for state-changing requests
    if (method !== 'GET' && method !== 'HEAD' && method !== 'OPTIONS') {
      const csrfToken = this.getCsrfToken();
      if (csrfToken) {
        headers['X-CSRF-Token'] = csrfToken;
      }
    }

    let url = `${this.baseUrl}${endpoint}`;
    if (params) {
      const searchParams = new URLSearchParams(params);
      url += `?${searchParams.toString()}`;
    }

    const response = await fetch(url, {
      method,
      headers,
      credentials: 'include',
      body: data ? JSON.stringify(data) : undefined,
    });

    if (!response.ok) {
      if (response.status === 401) {
        const isAuthValidation = endpoint.includes('/auth/me') || endpoint.includes('/auth/login');
        const isOnLoginPage = window.location.pathname === '/login';
        if (!isAuthValidation && !isOnLoginPage) {
          localStorage.removeItem('token');
          localStorage.removeItem('user');
          localStorage.removeItem('auth-storage');
          window.location.href = '/login';
        }
      }
      const error: ApiError = await response.json().catch(() => ({ message: 'Request failed' }));
      throw new Error(error.message || 'Request failed');
    }

    // Handle empty responses
    const text = await response.text();
    return (text ? JSON.parse(text) : null) as T;
  }

  private get<T>(endpoint: string, params?: Record<string, string>): Promise<T> {
    return this.request<T>('GET', endpoint, undefined, params);
  }

  private post<T>(endpoint: string, data?: unknown): Promise<T> {
    return this.request<T>('POST', endpoint, data);
  }

  private put<T>(endpoint: string, data?: unknown): Promise<T> {
    return this.request<T>('PUT', endpoint, data);
  }

  private delete<T>(endpoint: string): Promise<T> {
    return this.request<T>('DELETE', endpoint);
  }

  // Public request method for service adapters
  public async makeRequest<T>(
    method: string,
    endpoint: string,
    data?: unknown,
    params?: Record<string, string>
  ): Promise<T> {
    return this.request<T>(method, endpoint, data, params);
  }

  // Auth endpoints
  async login(identifier: string, password: string) {
    return this.post<{ accessToken: string; refreshToken: string; expiresIn: number; user: unknown }>('/auth/login', { identifier, password });
  }

  async logout() {
    return this.post('/auth/logout');
  }

  async refreshToken(refreshToken: string) {
    return this.post<{ token: string; expiresIn: number }>('/auth/refresh', { refreshToken });
  }

  async getCurrentUser() {
    return this.get<unknown>('/auth/me');
  }

  // Invitation/Registration endpoints
  async validateInvitation(token: string) {
    return this.get<unknown>('/auth/invitations/validate', { token });
  }

  async register(data: {
    token: string;
    username: string;
    email: string;
    password: string;
    firstName?: string;
    lastName?: string;
  }) {
    return this.post('/auth/register', data);
  }

  // Invitation management
  async getInvitations() {
    return this.get<unknown[]>('/invitations');
  }

  async createInvitation(data: { email?: string; role: string }) {
    return this.post('/invitations', data);
  }

  async deleteInvitation(id: string) {
    return this.delete(`/invitations/${id}`);
  }

  // Device endpoints
  async getDevices(params?: { status?: string; search?: string; page?: number; pageSize?: number }) {
    const stringParams: Record<string, string> = {};
    if (params?.status) stringParams.status = params.status;
    if (params?.search) stringParams.search = params.search;
    if (params?.page) stringParams.page = String(params.page);
    if (params?.pageSize) stringParams.pageSize = String(params.pageSize);
    return this.get<unknown>('/devices', Object.keys(stringParams).length ? stringParams : undefined);
  }

  async getDevice(id: string) {
    return this.get<unknown>(`/devices/${id}`);
  }

  async updateDevice(id: string, data: { displayName?: string; tags?: string[]; clientId?: string | null }) {
    return this.put(`/devices/${id}`, data);
  }

  async deleteDevice(id: string) {
    return this.delete(`/devices/${id}`);
  }

  async getDeviceMetrics(id: string, params?: { from?: string; to?: string; limit?: number }) {
    const stringParams: Record<string, string> = {};
    if (params?.from) stringParams.from = params.from;
    if (params?.to) stringParams.to = params.to;
    if (params?.limit) stringParams.limit = String(params.limit);
    return this.get<unknown>(`/devices/${id}/metrics`, Object.keys(stringParams).length ? stringParams : undefined);
  }

  async executeCommand(deviceId: string, command: string, commandType: string = 'shell') {
    return this.post(`/devices/${deviceId}/commands`, { command, commandType });
  }

  async getDeviceCommands(deviceId: string, params?: { status?: string; page?: number; pageSize?: number }) {
    const stringParams: Record<string, string> = {};
    if (params?.status) stringParams.status = params.status;
    if (params?.page) stringParams.page = String(params.page);
    if (params?.pageSize) stringParams.pageSize = String(params.pageSize);
    return this.get<unknown>(`/devices/${deviceId}/commands`, Object.keys(stringParams).length ? stringParams : undefined);
  }

  async pingAgent(deviceId: string) {
    return this.post<unknown>(`/devices/${deviceId}/ping`);
  }

  async uninstallAgent(deviceId: string) {
    return this.post<unknown>(`/devices/${deviceId}/uninstall`);
  }

  async forceUpdate(deviceId: string) {
    return this.post<unknown>(`/devices/${deviceId}/force-update`);
  }

  async devicePowerAction(deviceId: string, action: 'shutdown' | 'restart' | 'wake') {
    return this.post<unknown>(`/devices/${deviceId}/power`, { action });
  }

  // Certificate endpoints
  async getDeviceCertStatuses() {
    return this.get<unknown>('/devices/cert-status');
  }

  async getCertificateInfo() {
    return this.get<{
      certificates: Array<{
        name: string;
        type: string;
        path: string;
        exists: boolean;
        subject?: string;
        issuer?: string;
        validFrom?: string;
        validTo?: string;
        fingerprint?: string;
        serialNumber?: string;
        daysUntilExpiry?: number;
        status: string;
      }>;
      certsDir: string;
      caCertHash?: string;
    }>('/certificates/info');
  }

  // Script endpoints
  async getScripts(params?: { language?: string; search?: string }) {
    const stringParams: Record<string, string> = {};
    if (params?.language) stringParams.language = params.language;
    if (params?.search) stringParams.search = params.search;
    return this.get<unknown>('/scripts', Object.keys(stringParams).length ? stringParams : undefined);
  }

  async getScript(id: string) {
    return this.get<unknown>(`/scripts/${id}`);
  }

  async createScript(data: {
    name: string;
    description?: string;
    language: string;
    content: string;
    osTypes: string[];
    parameters?: unknown[];
  }) {
    return this.post('/scripts', data);
  }

  async updateScript(id: string, data: Partial<{
    name: string;
    description: string;
    content: string;
    osTypes: string[];
    parameters: unknown[];
  }>) {
    return this.put(`/scripts/${id}`, data);
  }

  async deleteScript(id: string) {
    return this.delete(`/scripts/${id}`);
  }

  async runScript(scriptId: string, deviceIds: string[], parameters?: Record<string, unknown>) {
    return this.post(`/scripts/${scriptId}/execute`, { deviceIds, parameters });
  }

  // Alert endpoints
  async getAlerts(params?: { status?: string; severity?: string }) {
    const stringParams: Record<string, string> = {};
    if (params?.status) stringParams.status = params.status;
    if (params?.severity) stringParams.severity = params.severity;
    return this.get<unknown>('/alerts', Object.keys(stringParams).length ? stringParams : undefined);
  }

  async acknowledgeAlert(id: string) {
    return this.post(`/alerts/${id}/acknowledge`);
  }

  async resolveAlert(id: string) {
    return this.post(`/alerts/${id}/resolve`);
  }

  // Alert rule endpoints
  async getAlertRules() {
    return this.get<unknown[]>('/alert-rules');
  }

  async createAlertRule(data: {
    name: string;
    description?: string;
    conditions: unknown[];
    severity: string;
    cooldownMinutes?: number;
  }) {
    return this.post('/alert-rules', data);
  }

  async updateAlertRule(id: string, data: Partial<{
    name: string;
    description: string;
    enabled: boolean;
    conditions: unknown[];
    severity: string;
    cooldownMinutes: number;
  }>) {
    return this.put(`/alert-rules/${id}`, data);
  }

  async deleteAlertRule(id: string) {
    return this.delete(`/alert-rules/${id}`);
  }

  // Dashboard stats
  async getDashboardStats() {
    return this.get<unknown>('/dashboard/stats');
  }

  // Settings endpoints
  async getSettings() {
    return this.get<unknown>('/settings');
  }

  async updateSettings(data: Record<string, unknown>) {
    return this.put('/settings', data);
  }

  // User management
  async getUsers(params?: { search?: string }) {
    const stringParams: Record<string, string> = {};
    if (params?.search) stringParams.search = params.search;
    return this.get<unknown>('/users', Object.keys(stringParams).length ? stringParams : undefined);
  }

  async createUser(data: {
    email: string;
    password: string;
    firstName: string;
    lastName: string;
    role: string;
  }) {
    return this.post('/users', data);
  }

  async updateUser(id: string, data: Partial<{
    firstName: string;
    lastName: string;
    role: string;
    password: string;
  }>) {
    return this.put(`/users/${id}`, data);
  }

  async deleteUser(id: string) {
    return this.delete(`/users/${id}`);
  }

  // Enrollment Token endpoints
  async getEnrollmentTokens() {
    return this.get<unknown[]>('/enrollment-tokens');
  }

  async createEnrollmentToken(data: {
    name: string;
    description?: string;
    expiresAt?: string;
    maxUses?: number;
    tags?: string[];
  }) {
    return this.post('/enrollment-tokens', data);
  }

  async updateEnrollmentToken(id: string, data: Partial<{
    name: string;
    description: string;
    isActive: boolean;
  }>) {
    return this.put(`/enrollment-tokens/${id}`, data);
  }

  async deleteEnrollmentToken(id: string) {
    return this.delete(`/enrollment-tokens/${id}`);
  }

  async regenerateEnrollmentToken(id: string) {
    return this.post(`/enrollment-tokens/${id}/regenerate`);
  }

  // Agent installers
  async getAgentInstallers() {
    return this.get<unknown>('/agents/installers');
  }

  getAgentDownloadUrl(platform: string, arch: string, token: string) {
    const baseUrl = getApiBaseUrl().replace('/api', '') || window.location.origin;
    return `${baseUrl}/api/agents/download/${platform}/${arch}?token=${encodeURIComponent(token)}`;
  }

  // Client management
  async getClients() {
    return this.get<unknown[]>('/clients');
  }

  async getClient(id: string) {
    return this.get<unknown>(`/clients/${id}`);
  }

  async createClient(data: { name: string; description?: string; contactEmail?: string }) {
    return this.post('/clients', data);
  }

  async updateClient(id: string, data: Partial<{ name: string; description: string; contactEmail: string }>) {
    return this.put(`/clients/${id}`, data);
  }

  async deleteClient(id: string) {
    return this.delete(`/clients/${id}`);
  }
  // Agent Installation Links (Admin)
  async getAgentLinks(params?: { status?: string; search?: string; page?: number; pageSize?: number }) {
    const stringParams: Record<string, string> = {};
    if (params?.status) stringParams.status = params.status;
    if (params?.search) stringParams.search = params.search;
    if (params?.page) stringParams.page = String(params.page);
    if (params?.pageSize) stringParams.pageSize = String(params.pageSize);
    return this.get<{ links: unknown[]; total: number; page: number; pageSize: number; totalPages: number }>('/admin/agent-links', Object.keys(stringParams).length ? stringParams : undefined);
  }

  async getAgentLink(linkId: string) {
    return this.get<unknown>(`/admin/agent-links/${linkId}`);
  }

  async createAgentLink(data: {
    deviceName: string;
    userEmail: string;
    userName?: string;
    expiresInHours?: number;
    sendEmail?: boolean;
    notes?: string;
  }) {
    return this.post<unknown>('/admin/agent-links', data);
  }

  async resendAgentLinkEmail(linkId: string) {
    return this.post<unknown>(`/admin/agent-links/${linkId}/resend`);
  }

  async revokeAgentLink(linkId: string) {
    return this.post<unknown>(`/admin/agent-links/${linkId}/revoke`);
  }

  async deleteAgentLink(linkId: string) {
    return this.delete<unknown>(`/admin/agent-links/${linkId}`);
  }

  async getAgentLinkStats() {
    return this.get<unknown>('/admin/agent-links/stats');
  }

  // Installation Codes (code-based installation without email)
  async createInstallationCode(data: {
    deviceName: string;
    userName?: string;
    notes?: string;
    expirationDays?: number;
  }) {
    return this.post<{
      success: boolean;
      code: string;
      deviceName: string;
      downloadUrl: string;
      expiresAt: string;
      instructions: string;
    }>('/admin/installation-codes', data);
  }

  async getInstallationCodes() {
    return this.get<{
      codes: Array<{
        id: string;
        code: string;
        deviceName: string;
        status: string;
        createdAt: string;
        expiresAt: string;
        usedAt?: string;
        createdByName?: string;
      }>;
    }>('/admin/installation-codes');
  }

  // Passkey / WebAuthn endpoints
  async beginPasskeyRegistration() {
    return this.post<{ sessionId: string; options: unknown }>('/webauthn/register/begin');
  }

  async finishPasskeyRegistration(data: { sessionId: string; response: unknown; name?: string }) {
    return this.post('/webauthn/register/finish', data);
  }

  async beginPasskeyAuthentication() {
    return this.post<{ sessionId: string; options: unknown }>('/webauthn/authenticate/begin');
  }

  async finishPasskeyAuthentication(data: { sessionId: string; response: unknown }) {
    return this.post<{ accessToken: string; refreshToken: string; expiresIn: number; user: unknown }>('/webauthn/authenticate/finish', data);
  }

  async getPasskeys() {
    return this.get<Array<{ id: string; name: string; createdAt: string; lastUsedAt?: string }>>('/passkeys');
  }

  async deletePasskey(id: string) {
    return this.delete(`/passkeys/${id}`);
  }

  async renamePasskey(id: string, name: string) {
    return this.put(`/passkeys/${id}`, { name });
  }

  // Portal / Client Tenant endpoints
  async getClientTenants() {
    return this.get<Array<{
      id: string;
      clientId?: string;
      clientName?: string;
      tenantId: string;
      tenantName?: string;
      enabled: boolean;
      createdAt: string;
      updatedAt: string;
    }>>('/portal/client-tenants');
  }

  async createClientTenant(data: { clientId?: string; tenantId: string; tenantName?: string }) {
    return this.post('/portal/client-tenants', data);
  }

  async deleteClientTenant(id: string) {
    return this.delete(`/portal/client-tenants/${id}`);
  }
}

// Create API service instance
export const api = new ApiService();
export default api;
