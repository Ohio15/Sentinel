import axios, { AxiosInstance } from 'axios';

// Public API service (no auth required - for installation portal)
class PublicApiService {
  private client: AxiosInstance;

  constructor() {
    const baseUrl = import.meta.env.VITE_API_URL || '/api';
    this.client = axios.create({
      baseURL: baseUrl,
      headers: {
        'Content-Type': 'application/json',
      },
    });
  }

  async validateInstallLink(downloadToken: string) {
    const response = await this.client.get(`/public/install/${downloadToken}`);
    return response.data;
  }

  async checkInstallStatus(downloadToken: string) {
    const response = await this.client.get(`/public/install/${downloadToken}/status`);
    return response.data;
  }

  getInstallerDownloadUrl(downloadToken: string) {
    const baseUrl = import.meta.env.VITE_API_URL?.replace('/api', '') || window.location.origin;
    return `${baseUrl}/api/public/install/${downloadToken}/download`;
  }
}

export const publicApi = new PublicApiService();
export default publicApi;
