/**
 * Public API Service - for unauthenticated endpoints
 * Used by the installation portal and other public-facing pages
 */
import { getApiBaseUrl } from './env';

interface LinkInfo {
  valid: boolean;
  deviceName?: string;
  userName?: string;
  companyName?: string;
  expiresAt?: string;
  status?: string;
  downloadAvailable?: boolean;
  alreadyDownloaded?: boolean;
  alreadyInstalled?: boolean;
  downloadCount?: number;
  error?: string;
  message?: string;
  installInstructions?: string;
}

interface InstallStatus {
  status: string;
  agentConnected: boolean;
  connectedAt?: string;
  agentVersion?: string;
  deviceId?: number;
}

class PublicApiService {
  private baseUrl: string;

  constructor() {
    this.baseUrl = getApiBaseUrl();
  }

  private async request<T>(
    method: string,
    endpoint: string
  ): Promise<T> {
    const response = await fetch(`${this.baseUrl}${endpoint}`, {
      method,
      headers: {
        'Content-Type': 'application/json',
      },
    });

    if (!response.ok) {
      const error = await response.json().catch(() => ({ message: 'Request failed' }));
      const err = new Error(error.message || 'Request failed') as any;
      err.response = { data: error };
      throw err;
    }

    const text = await response.text();
    return (text ? JSON.parse(text) : null) as T;
  }

  /**
   * Validate an installation link by download token
   */
  async validateInstallLink(downloadToken: string): Promise<LinkInfo> {
    return this.request<LinkInfo>('GET', `/public/install/${downloadToken}`);
  }

  /**
   * Check installation status
   */
  async checkInstallStatus(downloadToken: string): Promise<InstallStatus> {
    return this.request<InstallStatus>('GET', `/public/install/${downloadToken}/status`);
  }

  /**
   * Get the download URL for the installer
   */
  getInstallerDownloadUrl(downloadToken: string): string {
    return `${this.baseUrl}/public/install/${downloadToken}/download`;
  }
}

export const publicApi = new PublicApiService();
export default publicApi;
