// Global type declarations for Electron IPC API

interface ElectronAPI {
  devices: {
    list: (clientId?: string) => Promise<any>;
    get: (id: string) => Promise<any>;
    ping: (deviceId: string) => Promise<any>;
    delete: (id: string) => Promise<any>;
    disable: (id: string) => Promise<any>;
    enable: (id: string) => Promise<any>;
    uninstall: (id: string) => Promise<any>;
    update: (id: string, updates: { displayName?: string; tags?: string[] }) => Promise<any>;
    getMetrics: (deviceId: string, hours: number) => Promise<any>;
    setMetricsInterval: (deviceId: string, intervalMs: number) => Promise<any>;
  };
  commands: {
    execute: (deviceId: string, command: string, type: string) => Promise<any>;
    getHistory: (deviceId: string) => Promise<any>;
  };
  terminal: {
    start: (deviceId: string) => Promise<any>;
    send: (sessionId: string, data: string) => Promise<any>;
    resize: (sessionId: string, cols: number, rows: number) => Promise<any>;
    close: (sessionId: string) => Promise<any>;
    onData: (callback: (data: string) => void) => () => void;
  };
  files: {
    drives: (deviceId: string) => Promise<any>;
    list: (deviceId: string, path: string) => Promise<any>;
    download: (deviceId: string, remotePath: string, localPath: string) => Promise<any>;
    upload: (deviceId: string, localPath: string, remotePath: string) => Promise<any>;
    scan: (deviceId: string, path: string, maxDepth: number) => Promise<any>;
    downloadToSandbox: (deviceId: string, remotePath: string) => Promise<any>;
    onProgress: (callback: (progress: any) => void) => () => void;
  };
  remote: {
    startSession: (deviceId: string) => Promise<any>;
    stopSession: (sessionId: string) => Promise<any>;
    sendInput: (sessionId: string, input: any) => Promise<any>;
    onFrame: (callback: (frame: any) => void) => () => void;
  };
  webrtc: {
    start: (deviceId: string, offer: { type: string; sdp?: string; quality: string }) => Promise<any>;
    stop: (deviceId: string) => Promise<any>;
    sendSignal: (deviceId: string, signal: any) => Promise<any>;
    setQuality: (deviceId: string, quality: string) => Promise<any>;
    onSignal: (callback: (signal: any) => void) => () => void;
  };
  alerts: {
    list: () => Promise<any>;
    acknowledge: (id: string) => Promise<any>;
    dismiss: (id: string) => Promise<any>;
  };
  settings: {
    get: () => Promise<any>;
    update: (settings: any) => Promise<any>;
  };
  scripts: {
    list: () => Promise<any>;
    get: (id: string) => Promise<any>;
    create: (script: any) => Promise<any>;
    update: (id: string, script: any) => Promise<any>;
    delete: (id: string) => Promise<any>;
    execute: (deviceId: string, scriptId: string, parameters?: any) => Promise<any>;
  };
  clients: {
    list: () => Promise<any>;
    get: (id: string) => Promise<any>;
    create: (client: any) => Promise<any>;
    update: (id: string, client: any) => Promise<any>;
    delete: (id: string) => Promise<any>;
    getDevices: (clientId: string) => Promise<any>;
    assignDevice: (deviceId: string, clientId: string) => Promise<any>;
  };
  certificates: {
    list: () => Promise<any>;
    get: (id: string) => Promise<any>;
    download: (id: string) => Promise<any>;
    verify: (deviceId: string) => Promise<any>;
  };
  tickets: {
    list: (filters?: any) => Promise<any>;
    get: (id: string) => Promise<any>;
    create: (ticket: any) => Promise<any>;
    update: (id: string, ticket: any) => Promise<any>;
    delete: (id: string) => Promise<any>;
    addComment: (ticketId: string, comment: any) => Promise<any>;
  };
  knowledge: {
    list: (filters?: any) => Promise<any>;
    get: (id: string) => Promise<any>;
    create: (article: any) => Promise<any>;
    update: (id: string, article: any) => Promise<any>;
    delete: (id: string) => Promise<any>;
    search: (query: string) => Promise<any>;
  };
  updates: {
    checkForUpdates: () => Promise<any>;
    downloadUpdate: () => Promise<any>;
    installUpdate: () => void;
    onUpdateAvailable: (callback: (info: any) => void) => () => void;
    onDownloadProgress: (callback: (progress: any) => void) => () => void;
    onUpdateDownloaded: (callback: (info: any) => void) => () => void;
    onError: (callback: (error: any) => void) => () => void;
  };
  portal?: {
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
  };
  logError?: (error: { message: string; stack?: string; componentStack?: string }) => void;
  getAppVersion: () => Promise<string>;
  onDeviceUpdate: (callback: (device: any) => void) => () => void;
  onAlertUpdate: (callback: (alert: any) => void) => () => void;

  // Generic event subscription for real-time updates
  on: <T extends keyof IPCEventPayloads>(
    channel: T,
    callback: (data: IPCEventPayloads[T]) => void
  ) => () => void;
}

// IPC Event payload types for type-safe event handling
interface IPCEventPayloads {
  'devices:online': { agentId: string; deviceId?: string };
  'devices:offline': { agentId: string };
  'devices:updated': { deviceId: string };
  'metrics:updated': {
    deviceId: string;
    source?: string;
    metrics: {
      // Core metrics (required)
      cpuPercent: number;
      memoryPercent: number;
      diskPercent: number;
      // Memory metrics (camelCase)
      memoryUsedBytes?: number;
      memoryTotalBytes?: number;
      // Memory metrics (snake_case from agent)
      memory_used?: number;
      // Disk metrics (camelCase)
      diskUsedBytes?: number;
      diskTotalBytes?: number;
      // Disk metrics (snake_case from agent)
      disk_used?: number;
      // Network metrics (camelCase)
      networkRxBytes?: number;
      networkTxBytes?: number;
      // Network metrics (snake_case from agent)
      network_rx_bytes?: number;
      network_tx_bytes?: number;
      // Process metrics
      processCount?: number;
      process_count?: number;
      uptime?: number;
      // Extended disk metrics (camelCase)
      diskReadBytesPerSec?: number;
      diskWriteBytesPerSec?: number;
      // Extended disk metrics (snake_case from agent)
      disk_read_bytes_sec?: number;
      disk_write_bytes_sec?: number;
      // Extended memory metrics (camelCase)
      memoryCommitted?: number;
      memoryCached?: number;
      memoryPagedPool?: number;
      memoryNonPagedPool?: number;
      // Extended memory metrics (snake_case from agent)
      memory_committed?: number;
      memory_cached?: number;
      memory_paged_pool?: number;
      memory_non_paged_pool?: number;
      // GPU metrics (both variants)
      gpuMetrics?: Array<{
        name: string;
        utilization: number;
        memoryUsed: number;
        memoryTotal: number;
        temperature?: number;
        powerDraw?: number;
      }>;
      gpu_metrics?: Array<{
        name: string;
        utilization: number;
        memoryUsed: number;
        memoryTotal: number;
        temperature?: number;
        powerDraw?: number;
      }>;
      // Network interface metrics (both variants)
      networkInterfaces?: Array<{
        name: string;
        rxBytesPerSec: number;
        txBytesPerSec: number;
        rxBytes: number;
        txBytes: number;
        rxPackets: number;
        txPackets: number;
        errorsIn: number;
        errorsOut: number;
      }>;
      network_interfaces?: Array<{
        name: string;
        rxBytesPerSec: number;
        txBytesPerSec: number;
        rxBytes: number;
        txBytes: number;
        rxPackets: number;
        txPackets: number;
        errorsIn: number;
        errorsOut: number;
      }>;
    };
  };
  'alerts:new': { id: string; type: string; severity: string; message: string; deviceId?: string };
  'terminal:data': { sessionId: string; data: string };
  'files:progress': { deviceId: string; path: string; percent: number; bytesTransferred: number; totalBytes: number };
  'remote:frame': { sessionId: string; data: ArrayBuffer };
  'webrtc:signal': { type: string; sdp?: string; candidate?: string };
  'command:output': { commandId: string; output: string; error?: string };
  'tickets:updated': { ticketId: string; action: string };
  'certs:distributed': { deviceId: string; success: boolean };
  'certs:agentConfirmed': { deviceId: string; fingerprint: string };
  'updates:status': { status: string; version?: string; progress?: number };
}

declare global {
  interface Window {
    api: ElectronAPI;
  }
}

export {};
