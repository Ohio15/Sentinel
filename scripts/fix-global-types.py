import os

filepath = 'D:/Projects/Sentinel/src/renderer/global.d.ts'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# Add missing APIs before the closing brace of ElectronAPI
old_portal = """  portal?: {
    getPortal: (subdomain: string) => Promise<any>;
    updateBranding: (subdomain: string, branding: any) => Promise<any>;
    getDevices: (subdomain: string) => Promise<any>;
    getDevice: (subdomain: string, deviceId: string) => Promise<any>;
  };
  installers?: {
    downloadAgent: (platform: string) => Promise<any>;
  };"""

new_portal = """  portal?: {
    getPortal: (subdomain: string) => Promise<any>;
    updateBranding: (subdomain: string, branding: any) => Promise<any>;
    getDevices: (subdomain: string) => Promise<any>;
    getDevice: (subdomain: string, deviceId: string) => Promise<any>;
    getSettings: () => Promise<any>;
    updateSettings: (settings: any) => Promise<any>;
    getClientTenants: () => Promise<any>;
    createClientTenant: (clientId: string, tenantId: string) => Promise<any>;
    deleteClientTenant: (clientId: string, tenantId: string) => Promise<any>;
  };
  installers?: {
    downloadAgent: (platform: string) => Promise<any>;
  };
  // Alias for updates (used by some components)
  updater: {
    checkForUpdates: () => Promise<any>;
    downloadUpdate: () => Promise<any>;
    installUpdate: () => void;
    getVersion: () => Promise<string>;
    onUpdateAvailable: (callback: (info: any) => void) => () => void;
    onDownloadProgress: (callback: (progress: any) => void) => () => void;
    onUpdateDownloaded: (callback: (info: any) => void) => () => void;
    onError: (callback: (error: any) => void) => () => void;
    getDevice: (deviceId: string) => Promise<any>;
    onStatus: (callback: (status: any) => void) => () => void;
  };
  // Server API for enrollment and settings
  server?: {
    getEnrollmentLink: () => Promise<string>;
    getSettings: () => Promise<any>;
  };
  // Agent download API
  agent?: {
    download: (platform: string) => Promise<string>;
  };
  // Knowledge base alias
  kb?: {
    list: (filters?: any) => Promise<any>;
    get: (id: string) => Promise<any>;
    create: (article: any) => Promise<any>;
    update: (id: string, article: any) => Promise<any>;
    delete: (id: string) => Promise<any>;
    search: (query: string) => Promise<any>;
    getCategories: () => Promise<any>;
    createCategory: (category: any) => Promise<any>;
    updateCategory: (id: string, category: any) => Promise<any>;
    deleteCategory: (id: string) => Promise<any>;
  };
  // Backend connection API
  backend?: {
    connect: (url: string) => Promise<any>;
    getStatus: () => Promise<any>;
  };"""

content = content.replace(old_portal, new_portal)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('Updated global.d.ts with missing API types')
