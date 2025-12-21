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
  };
  installers?: {
    downloadAgent: (platform: string) => Promise<any>;
  };
  logError?: (error: { message: string; stack?: string; componentStack?: string }) => void;
  getAppVersion: () => Promise<string>;
  onDeviceUpdate: (callback: (device: any) => void) => () => void;
  onAlertUpdate: (callback: (alert: any) => void) => () => void;
}

declare global {
  interface Window {
    api: ElectronAPI;
  }
}

export {};
