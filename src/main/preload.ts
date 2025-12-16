import { contextBridge, ipcRenderer } from 'electron';

// Expose protected methods to renderer
contextBridge.exposeInMainWorld('api', {
  // Devices
  devices: {
    list: (clientId?: string) => ipcRenderer.invoke('devices:list', clientId),
    get: (id: string) => ipcRenderer.invoke('devices:get', id),
    ping: (deviceId: string) => ipcRenderer.invoke('devices:ping', deviceId),
    delete: (id: string) => ipcRenderer.invoke('devices:delete', id),
    update: (id: string, updates: { displayName?: string; tags?: string[] }) =>
      ipcRenderer.invoke('devices:update', id, updates),
    getMetrics: (deviceId: string, hours: number) =>
      ipcRenderer.invoke('devices:getMetrics', deviceId, hours),
    setMetricsInterval: (deviceId: string, intervalMs: number) =>
      ipcRenderer.invoke('devices:setMetricsInterval', deviceId, intervalMs),
  },

  // Commands
  commands: {
    execute: (deviceId: string, command: string, type: string) =>
      ipcRenderer.invoke('commands:execute', deviceId, command, type),
    getHistory: (deviceId: string) =>
      ipcRenderer.invoke('commands:getHistory', deviceId),
  },

  // Terminal
  terminal: {
    start: (deviceId: string) => ipcRenderer.invoke('terminal:start', deviceId),
    send: (sessionId: string, data: string) =>
      ipcRenderer.invoke('terminal:send', sessionId, data),
    resize: (sessionId: string, cols: number, rows: number) =>
      ipcRenderer.invoke('terminal:resize', sessionId, cols, rows),
    close: (sessionId: string) => ipcRenderer.invoke('terminal:close', sessionId),
    onData: (callback: (data: string) => void) => {
      const handler = (_: any, payload: { sessionId: string; data: string }) => callback(payload.data);
      ipcRenderer.on('terminal:data', handler);
      return () => ipcRenderer.removeListener('terminal:data', handler);
    },
  },

  // Files
  files: {
    drives: (deviceId: string) =>
      ipcRenderer.invoke('files:drives', deviceId),
    list: (deviceId: string, path: string) =>
      ipcRenderer.invoke('files:list', deviceId, path),
    download: (deviceId: string, remotePath: string, localPath: string) =>
      ipcRenderer.invoke('files:download', deviceId, remotePath, localPath),
    upload: (deviceId: string, localPath: string, remotePath: string) =>
      ipcRenderer.invoke('files:upload', deviceId, localPath, remotePath),
    scan: (deviceId: string, path: string, maxDepth: number) =>
      ipcRenderer.invoke('files:scan', deviceId, path, maxDepth),
    downloadToSandbox: (deviceId: string, remotePath: string) =>
      ipcRenderer.invoke('files:downloadToSandbox', deviceId, remotePath),
    onProgress: (callback: (progress: any) => void) => {
      const handler = (_: any, progress: any) => callback(progress);
      ipcRenderer.on('files:progress', handler);
      return () => ipcRenderer.removeListener('files:progress', handler);
    },
  },

  // Remote Desktop (legacy)
  remote: {
    startSession: (deviceId: string) =>
      ipcRenderer.invoke('remote:startSession', deviceId),
    stopSession: (sessionId: string) =>
      ipcRenderer.invoke('remote:stopSession', sessionId),
    sendInput: (sessionId: string, input: any) =>
      ipcRenderer.invoke('remote:sendInput', sessionId, input),
    onFrame: (callback: (frame: any) => void) => {
      const handler = (_: any, frame: any) => callback(frame);
      ipcRenderer.on('remote:frame', handler);
      return () => ipcRenderer.removeListener('remote:frame', handler);
    },
  },


  // WebRTC Remote Desktop
  webrtc: {
    start: (deviceId: string, offer: { type: string; sdp?: string; quality: string }) =>
      ipcRenderer.invoke('webrtc:start', deviceId, offer),
    stop: (deviceId: string) =>
      ipcRenderer.invoke('webrtc:stop', deviceId),
    sendSignal: (deviceId: string, signal: any) =>
      ipcRenderer.invoke('webrtc:signal', deviceId, signal),
    setQuality: (deviceId: string, quality: string) =>
      ipcRenderer.invoke('webrtc:setQuality', deviceId, quality),
    onSignal: (callback: (signal: any) => void) => {
      const handler = (_: any, signal: any) => callback(signal);
      ipcRenderer.on('webrtc:signal', handler);
      return () => ipcRenderer.removeListener('webrtc:signal', handler);
    },
  },
  // Alerts
  alerts: {
    list: () => ipcRenderer.invoke('alerts:list'),
    acknowledge: (id: string) => ipcRenderer.invoke('alerts:acknowledge', id),
    resolve: (id: string) => ipcRenderer.invoke('alerts:resolve', id),
    getRules: () => ipcRenderer.invoke('alerts:getRules'),
    createRule: (rule: any) => ipcRenderer.invoke('alerts:createRule', rule),
    updateRule: (id: string, rule: any) =>
      ipcRenderer.invoke('alerts:updateRule', id, rule),
    deleteRule: (id: string) => ipcRenderer.invoke('alerts:deleteRule', id),
    onNew: (callback: (alert: any) => void) => {
      const handler = (_: any, alert: any) => callback(alert);
      ipcRenderer.on('alerts:new', handler);
      return () => ipcRenderer.removeListener('alerts:new', handler);
    },
  },

  // Scripts
  scripts: {
    list: () => ipcRenderer.invoke('scripts:list'),
    create: (script: any) => ipcRenderer.invoke('scripts:create', script),
    update: (id: string, script: any) =>
      ipcRenderer.invoke('scripts:update', id, script),
    delete: (id: string) => ipcRenderer.invoke('scripts:delete', id),
    execute: (scriptId: string, deviceIds: string[]) =>
      ipcRenderer.invoke('scripts:execute', scriptId, deviceIds),
  },

  // Tickets
  tickets: {
    list: (filters?: { status?: string; priority?: string; assignedTo?: string; deviceId?: string }) =>
      ipcRenderer.invoke('tickets:list', filters),
    get: (id: string) => ipcRenderer.invoke('tickets:get', id),
    create: (ticket: any) => ipcRenderer.invoke('tickets:create', ticket),
    update: (id: string, updates: any) =>
      ipcRenderer.invoke('tickets:update', id, updates),
    delete: (id: string) => ipcRenderer.invoke('tickets:delete', id),
    getComments: (ticketId: string) =>
      ipcRenderer.invoke('tickets:getComments', ticketId),
    addComment: (comment: any) =>
      ipcRenderer.invoke('tickets:addComment', comment),
    getActivity: (ticketId: string) =>
      ipcRenderer.invoke('tickets:getActivity', ticketId),
    getStats: () => ipcRenderer.invoke('tickets:getStats'),
    getTemplates: () => ipcRenderer.invoke('tickets:getTemplates'),
    createTemplate: (template: any) =>
      ipcRenderer.invoke('tickets:createTemplate', template),
    updateTemplate: (id: string, template: any) =>
      ipcRenderer.invoke('tickets:updateTemplate', id, template),
    deleteTemplate: (id: string) =>
      ipcRenderer.invoke('tickets:deleteTemplate', id),
  },

  // Settings
  settings: {
    get: () => ipcRenderer.invoke('settings:get'),
    update: (settings: any) => ipcRenderer.invoke('settings:update', settings),
  },


  // Clients
  clients: {
    list: () => ipcRenderer.invoke('clients:list'),
    get: (id: string) => ipcRenderer.invoke('clients:get', id),
    create: (client: { name: string; description?: string; color?: string; logoUrl?: string }) =>
      ipcRenderer.invoke('clients:create', client),
    update: (id: string, client: { name?: string; description?: string; color?: string; logoUrl?: string }) =>
      ipcRenderer.invoke('clients:update', id, client),
    delete: (id: string) => ipcRenderer.invoke('clients:delete', id),
    assignDevice: (deviceId: string, clientId: string | null) =>
      ipcRenderer.invoke('devices:assignToClient', deviceId, clientId),
    bulkAssignDevices: (deviceIds: string[], clientId: string | null) =>
      ipcRenderer.invoke('devices:bulkAssignToClient', deviceIds, clientId),
  },

  // Server
  server: {
    getInfo: () => ipcRenderer.invoke('server:getInfo'),
    regenerateToken: () => ipcRenderer.invoke('server:regenerateToken'),
    getAgentInstallerCommand: (platform: string) =>
      ipcRenderer.invoke('agent:getInstallerCommand', platform),
  },

  // Agent
  agent: {
    download: (platform: string) => ipcRenderer.invoke('agent:download', platform),
  },

  // Updater
  updater: {
    checkForUpdates: () => ipcRenderer.invoke('check-for-updates'),
    downloadUpdate: () => ipcRenderer.invoke('download-update'),
    installUpdate: () => ipcRenderer.invoke('install-update'),
    getVersion: () => ipcRenderer.invoke('get-app-version'),
    onUpdateAvailable: (callback: (info: any) => void) => {
      const handler = (_: any, info: any) => callback(info);
      ipcRenderer.on('update-available', handler);
      return () => ipcRenderer.removeListener('update-available', handler);
    },
    onUpdateNotAvailable: (callback: (info: any) => void) => {
      const handler = (_: any, info: any) => callback(info);
      ipcRenderer.on('update-not-available', handler);
      return () => ipcRenderer.removeListener('update-not-available', handler);
    },
    onDownloadProgress: (callback: (progress: any) => void) => {
      const handler = (_: any, progress: any) => callback(progress);
      ipcRenderer.on('update-download-progress', handler);
      return () => ipcRenderer.removeListener('update-download-progress', handler);
    },
    onUpdateDownloaded: (callback: (info: any) => void) => {
      const handler = (_: any, info: any) => callback(info);
      ipcRenderer.on('update-downloaded', handler);
      return () => ipcRenderer.removeListener('update-downloaded', handler);
    },
    onError: (callback: (error: any) => void) => {
      const handler = (_: any, error: any) => callback(error);
      ipcRenderer.on('update-error', handler);
      return () => ipcRenderer.removeListener('update-error', handler);
    },
  },


  // Event subscriptions for real-time updates
  on: (channel: string, callback: (...args: any[]) => void) => {
    const validChannels = [
      'devices:updated',
      'devices:online',
      'devices:offline',
      'metrics:updated',
      'alerts:new',
      'terminal:data',
      'files:progress',
      'remote:frame',
      'webrtc:signal',
      'command:output',
      'tickets:updated',
    ];
    if (validChannels.includes(channel)) {
      const handler = (_: any, ...args: any[]) => callback(...args);
      ipcRenderer.on(channel, handler);
      return () => ipcRenderer.removeListener(channel, handler);
    }
    return () => {};
  },
});

// Type definitions for the exposed API
export interface ElectronAPI {
  devices: {
    list: (clientId?: string) => Promise<Device[]>;
    get: (id: string) => Promise<Device | null>;
    ping: (deviceId: string) => Promise<{ online: boolean; status: string; message: string }>;
    delete: (id: string) => Promise<void>;
    update: (id: string, updates: { displayName?: string; tags?: string[] }) => Promise<Device | null>;
    getMetrics: (deviceId: string, hours: number) => Promise<DeviceMetrics[]>;
    setMetricsInterval: (deviceId: string, intervalMs: number) => Promise<void>;
  };
  commands: {
    execute: (deviceId: string, command: string, type: string) => Promise<CommandResult>;
    getHistory: (deviceId: string) => Promise<Command[]>;
  };
  terminal: {
    start: (deviceId: string) => Promise<{ sessionId: string }>;
    send: (sessionId: string, data: string) => Promise<void>;
    resize: (sessionId: string, cols: number, rows: number) => Promise<void>;
    close: (sessionId: string) => Promise<void>;
    onData: (callback: (data: string) => void) => () => void;
  };
  files: {
    drives: (deviceId: string) => Promise<DriveInfo[]>;
    list: (deviceId: string, path: string) => Promise<FileEntry[]>;
    download: (deviceId: string, remotePath: string, localPath: string) => Promise<void>;
    upload: (deviceId: string, localPath: string, remotePath: string) => Promise<void>;
    scan: (deviceId: string, path: string, maxDepth: number) => Promise<any>;
    downloadToSandbox: (deviceId: string, remotePath: string) => Promise<SandboxDownloadResult>;
    onProgress: (callback: (progress: FileProgress) => void) => () => void;
  };
  remote: {
    startSession: (deviceId: string) => Promise<{ sessionId: string }>;
    stopSession: (sessionId: string) => Promise<void>;
    sendInput: (sessionId: string, input: RemoteInput) => Promise<void>;
    onFrame: (callback: (frame: RemoteFrame) => void) => () => void;
  };
  webrtc: {
    start: (deviceId: string, offer: WebRTCOffer) => Promise<void>;
    stop: (deviceId: string) => Promise<void>;
    sendSignal: (deviceId: string, signal: WebRTCSignal) => Promise<void>;
    setQuality: (deviceId: string, quality: string) => Promise<void>;
    onSignal: (callback: (signal: WebRTCSignal) => void) => () => void;
  };
  alerts: {
    list: () => Promise<Alert[]>;
    acknowledge: (id: string) => Promise<void>;
    resolve: (id: string) => Promise<void>;
    getRules: () => Promise<AlertRule[]>;
    createRule: (rule: Omit<AlertRule, 'id'>) => Promise<AlertRule>;
    updateRule: (id: string, rule: Partial<AlertRule>) => Promise<AlertRule>;
    deleteRule: (id: string) => Promise<void>;
    onNew: (callback: (alert: Alert) => void) => () => void;
  };
  scripts: {
    list: () => Promise<Script[]>;
    create: (script: Omit<Script, 'id'>) => Promise<Script>;
    update: (id: string, script: Partial<Script>) => Promise<Script>;
    delete: (id: string) => Promise<void>;
    execute: (scriptId: string, deviceIds: string[]) => Promise<void>;
  };
  tickets: {
    list: (filters?: TicketFilters) => Promise<Ticket[]>;
    get: (id: string) => Promise<Ticket | null>;
    create: (ticket: Omit<Ticket, 'id' | 'ticketNumber' | 'createdAt' | 'updatedAt'>) => Promise<Ticket>;
    update: (id: string, updates: Partial<Ticket> & { actorName?: string }) => Promise<Ticket>;
    delete: (id: string) => Promise<void>;
    getComments: (ticketId: string) => Promise<TicketComment[]>;
    addComment: (comment: Omit<TicketComment, 'id' | 'createdAt'>) => Promise<TicketComment>;
    getActivity: (ticketId: string) => Promise<TicketActivity[]>;
    getStats: () => Promise<TicketStats>;
    getTemplates: () => Promise<TicketTemplate[]>;
    createTemplate: (template: Omit<TicketTemplate, 'id' | 'createdAt' | 'updatedAt'>) => Promise<TicketTemplate>;
    updateTemplate: (id: string, template: Partial<TicketTemplate>) => Promise<TicketTemplate>;
    deleteTemplate: (id: string) => Promise<void>;
  };
  settings: {
    get: () => Promise<Settings>;
    update: (settings: Partial<Settings>) => Promise<Settings>;
  };
  clients: {
    list: () => Promise<Client[]>;
    get: (id: string) => Promise<Client | null>;
    create: (client: Omit<Client, 'id' | 'createdAt' | 'updatedAt' | 'deviceCount' | 'openTicketCount'>) => Promise<Client>;
    update: (id: string, client: Partial<Client>) => Promise<Client | null>;
    delete: (id: string) => Promise<void>;
    assignDevice: (deviceId: string, clientId: string | null) => Promise<Device | null>;
    bulkAssignDevices: (deviceIds: string[], clientId: string | null) => Promise<{ success: boolean; count: number }>;
  };
  server: {
    getInfo: () => Promise<ServerInfo>;
    regenerateToken: () => Promise<string>;
    getAgentInstallerCommand: (platform: string) => Promise<string>;
  };
  agent: {
    download: (platform: string) => Promise<{
      success: boolean;
      filePath?: string;
      size?: number;
      canceled?: boolean;
      error?: string;
    }>;
  };
  updater: {
    checkForUpdates: () => Promise<{ success: boolean; updateInfo?: any; error?: string }>;
    downloadUpdate: () => Promise<{ success: boolean; error?: string }>;
    installUpdate: () => void;
    getVersion: () => Promise<string>;
    onUpdateAvailable: (callback: (info: UpdateInfo) => void) => () => void;
    onUpdateNotAvailable: (callback: (info: { version: string }) => void) => () => void;
    onDownloadProgress: (callback: (progress: DownloadProgress) => void) => () => void;
    onUpdateDownloaded: (callback: (info: UpdateInfo) => void) => () => void;
    onError: (callback: (error: { message: string }) => void) => () => void;
  };
  on: (channel: string, callback: (...args: any[]) => void) => () => void;
}

// Type definitions
interface Device {
  id: string;
  hostname: string;
  displayName?: string;
  osType: string;
  osVersion: string;
  architecture: string;
  agentVersion: string;
  lastSeen: string;
  status: 'online' | 'offline' | 'warning' | 'critical';
  ipAddress: string;
  macAddress: string;
  tags: string[];
}

interface DeviceMetrics {
  timestamp: string;
  cpuPercent: number;
  memoryPercent: number;
  memoryUsedBytes: number;
  diskPercent: number;
  diskUsedBytes: number;
  networkRxBytes: number;
  networkTxBytes: number;
}

interface Command {
  id: string;
  deviceId: string;
  command: string;
  type: string;
  status: 'pending' | 'running' | 'completed' | 'failed';
  output?: string;
  error?: string;
  createdAt: string;
  completedAt?: string;
}

interface CommandResult {
  success: boolean;
  output?: string;
  error?: string;
}

interface DriveInfo {
  name: string;
  path: string;
  label: string;
  drive_type: string;
  file_system: string;
  total_size: number;
  free_space: number;
  used_space: number;
}

interface FileEntry {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  modified_time: string;
  mode: string;
  is_hidden?: boolean;
}

interface SandboxDownloadResult {
  localPath: string;
  sandboxDir: string;
  filename: string;
  originalName: string;
  sha256: string;
  size: number;
}

interface FileProgress {
  deviceId: string;
  filename: string;
  bytesTransferred: number;
  totalBytes: number;
  percentage: number;
}

interface WebRTCOffer {
  type: string;
  sdp?: string;
  quality: string;
}

interface WebRTCSignal {
  deviceId: string;
  type: string;
  sdp?: string;
  candidate?: any;
}

interface RemoteInput {
  type: 'mouse' | 'keyboard';
  event: string;
  x?: number;
  y?: number;
  button?: number;
  key?: string;
  modifiers?: string[];
}

interface RemoteFrame {
  sessionId: string;
  data: ArrayBuffer;
  width: number;
  height: number;
}

interface Alert {
  id: string;
  deviceId: string;
  deviceName: string;
  severity: 'info' | 'warning' | 'critical';
  title: string;
  message: string;
  status: 'open' | 'acknowledged' | 'resolved';
  createdAt: string;
  acknowledgedAt?: string;
  resolvedAt?: string;
}

interface AlertRule {
  id: string;
  name: string;
  description?: string;
  enabled: boolean;
  metric: string;
  operator: 'gt' | 'lt' | 'eq' | 'gte' | 'lte';
  threshold: number;
  severity: 'info' | 'warning' | 'critical';
  cooldownMinutes: number;
}

interface Script {
  id: string;
  name: string;
  description?: string;
  language: 'powershell' | 'bash' | 'python';
  content: string;
  osTypes: string[];
  createdAt: string;
  updatedAt: string;
}


interface UpdateInfo {
  version: string;
  releaseNotes?: string;
}

interface DownloadProgress {
  percent: number;
  bytesPerSecond: number;
  transferred: number;
  total: number;
}

interface Settings {
  serverPort: number;
  agentCheckInterval: number;
  metricsRetentionDays: number;
  alertEmailEnabled: boolean;
  alertEmail?: string;
  theme: 'light' | 'dark' | 'system';
}

interface ServerInfo {
  port: number;
  agentCount: number;
  enrollmentToken: string;
}

// Ticket types
interface Ticket {
  id: string;
  ticketNumber: number;
  subject: string;
  description?: string;
  status: 'open' | 'in_progress' | 'waiting' | 'resolved' | 'closed';
  priority: 'low' | 'medium' | 'high' | 'urgent';
  type: 'incident' | 'request' | 'problem' | 'change';
  deviceId?: string;
  deviceName?: string;
  deviceDisplayName?: string;
  requesterName?: string;
  requesterEmail?: string;
  assignedTo?: string;
  tags: string[];
  dueDate?: string;
  resolvedAt?: string;
  closedAt?: string;
  createdAt: string;
  updatedAt: string;
}

interface TicketFilters {
  status?: string;
  priority?: string;
  assignedTo?: string;
  deviceId?: string;
}

interface TicketComment {
  id: string;
  ticketId: string;
  content: string;
  isInternal: boolean;
  authorName: string;
  authorEmail?: string;
  attachments: string[];
  createdAt: string;
}

interface TicketActivity {
  id: string;
  ticketId: string;
  action: string;
  fieldName?: string;
  oldValue?: string;
  newValue?: string;
  actorName: string;
  createdAt: string;
}

interface TicketTemplate {
  id: string;
  name: string;
  subject?: string;
  content: string;
  isActive: boolean;
  createdAt: string;
  updatedAt: string;
}

interface TicketStats {
  openCount: number;
  inProgressCount: number;
  waitingCount: number;
  resolvedCount: number;
  closedCount: number;
  totalCount: number;
}



interface Client {
  id: string;
  name: string;
  description?: string;
  color?: string;
  logoUrl?: string;
  deviceCount?: number;
  openTicketCount?: number;
  createdAt: string;
  updatedAt: string;
}

declare global {
  interface Window {
    api: ElectronAPI;
  }
}
