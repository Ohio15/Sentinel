export interface User {
  id: string;
  email: string;
  firstName: string;
  lastName: string;
  role: 'admin' | 'user' | 'readonly';
  createdAt: string;
  lastLogin?: string;
}

export interface AuthState {
  user: User | null;
  token: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  login: (email: string, password: string) => Promise<void>;
  logout: () => void;
  checkAuth: () => Promise<void>;
}

export interface GPUInfo {
  name: string;
  vendor: string;
  memory: number;
  driver_version: string;
}

export interface StorageInfo {
  device: string;
  mountpoint: string;
  fstype: string;
  total: number;
  used: number;
  free: number;
  percent: number;
}

export interface Device {
  id: string;
  agentId: string;
  hostname: string;
  displayName?: string;
  osType: 'windows' | 'linux' | 'macos';
  osVersion: string;
  osBuild?: string;
  platform?: string;
  platformFamily?: string;
  architecture: string;
  cpuModel?: string;
  cpuCores?: number;
  cpuThreads?: number;
  cpuSpeed?: number;
  totalMemory?: number;
  bootTime?: number;
  gpu?: GPUInfo[];
  storage?: StorageInfo[];
  serialNumber?: string;
  manufacturer?: string;
  model?: string;
  domain?: string;
  agentVersion: string;
  previousAgentVersion?: string;
  lastUpdateCheck?: string;
  lastSeen: string;
  status: 'online' | 'offline' | 'warning' | 'critical';
  ipAddress?: string;
  publicIp?: string;
  macAddress?: string;
  tags: string[];
  createdAt: string;
  updatedAt: string;
}

export interface DeviceMetrics {
  id: string;
  deviceId: string;
  timestamp: string;
  cpuPercent: number;
  memoryPercent: number;
  memoryUsedBytes: number;
  diskPercent: number;
  diskUsedBytes: number;
  networkRxBytes: number;
  networkTxBytes: number;
  processCount: number;
}

export interface Command {
  id: string;
  deviceId: string;
  commandType: string;
  payload: Record<string, unknown>;
  status: 'pending' | 'running' | 'completed' | 'failed';
  result?: Record<string, unknown>;
  errorMessage?: string;
  createdBy: string;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
}

export interface Script {
  id: string;
  name: string;
  description?: string;
  language: 'powershell' | 'bash' | 'python';
  content: string;
  parameters?: ScriptParameter[];
  osTypes: string[];
  isSystem: boolean;
  version: number;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
}

export interface ScriptParameter {
  name: string;
  type: 'string' | 'number' | 'boolean';
  required: boolean;
  default?: string | number | boolean;
  description?: string;
}

export interface Alert {
  id: string;
  deviceId: string;
  ruleId?: string;
  severity: 'info' | 'warning' | 'critical';
  title: string;
  message?: string;
  status: 'open' | 'acknowledged' | 'resolved';
  acknowledgedBy?: string;
  acknowledgedAt?: string;
  resolvedAt?: string;
  createdAt: string;
  device?: Device;
}

export interface AlertRule {
  id: string;
  name: string;
  description?: string;
  enabled: boolean;
  conditions: AlertCondition[];
  severity: 'info' | 'warning' | 'critical';
  cooldownMinutes: number;
  createdAt: string;
}

export interface AlertCondition {
  metric: string;
  operator: 'gt' | 'lt' | 'eq' | 'gte' | 'lte';
  value: number;
}

export interface DashboardStats {
  totalDevices: number;
  onlineDevices: number;
  offlineDevices: number;
  warningDevices: number;
  criticalDevices: number;
  openAlerts: number;
  criticalAlerts: number;
}

export interface PaginatedResponse<T> {
  data: T[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface ApiError {
  error: string;
  message?: string;
  statusCode?: number;
}
