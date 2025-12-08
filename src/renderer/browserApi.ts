// Browser API shim for development without Electron
// Provides mock data when window.api is not available

const mockDevices = [
  {
    id: '1',
    agentId: 'agent-001',
    hostname: 'DESKTOP-DEV01',
    displayName: 'Development Workstation',
    osType: 'windows',
    osVersion: 'Windows 11 Pro',
    architecture: 'x86_64',
    agentVersion: '1.1.0',
    lastSeen: new Date().toISOString(),
    status: 'online' as const,
    ipAddress: '192.168.1.100',
    macAddress: 'AA:BB:CC:DD:EE:FF',
    tags: ['dev', 'workstation'],
    metadata: {},
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  },
  {
    id: '2',
    agentId: 'agent-002',
    hostname: 'SERVER-PROD01',
    displayName: 'Production Server',
    osType: 'linux',
    osVersion: 'Ubuntu 22.04 LTS',
    architecture: 'x86_64',
    agentVersion: '1.1.0',
    lastSeen: new Date(Date.now() - 300000).toISOString(),
    status: 'online' as const,
    ipAddress: '192.168.1.50',
    macAddress: '11:22:33:44:55:66',
    tags: ['prod', 'server'],
    metadata: {},
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  },
  {
    id: '3',
    agentId: 'agent-003',
    hostname: 'LAPTOP-USER01',
    displayName: 'User Laptop',
    osType: 'windows',
    osVersion: 'Windows 10 Pro',
    architecture: 'x86_64',
    agentVersion: '1.0.5',
    lastSeen: new Date(Date.now() - 3600000).toISOString(),
    status: 'offline' as const,
    ipAddress: '192.168.1.150',
    macAddress: 'FF:EE:DD:CC:BB:AA',
    tags: ['user'],
    metadata: {},
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  },
];

const mockAlerts = [
  {
    id: '1',
    deviceId: '1',
    deviceName: 'DESKTOP-DEV01',
    type: 'cpu_high',
    severity: 'warning' as const,
    title: 'High CPU Usage',
    message: 'CPU usage above 80%',
    status: 'open' as const,
    createdAt: new Date(Date.now() - 1800000).toISOString(),
  },
  {
    id: '2',
    deviceId: '2',
    deviceName: 'SERVER-PROD01',
    type: 'disk_space',
    severity: 'critical' as const,
    title: 'Low Disk Space',
    message: 'Disk space below 10%',
    status: 'open' as const,
    createdAt: new Date(Date.now() - 7200000).toISOString(),
  },
];

const mockRules = [
  {
    id: '1',
    name: 'High CPU Alert',
    description: 'Alert when CPU exceeds 80%',
    enabled: true,
    metric: 'cpu_percent',
    operator: 'gt' as const,
    threshold: 80,
    severity: 'warning' as const,
    cooldownMinutes: 15,
    createdAt: new Date().toISOString(),
  },
];

const mockSettings = {
  serverPort: 3000,
  agentCheckInterval: 30,
  metricsRetentionDays: 30,
  alertEmailEnabled: false,
  alertEmail: '',
  theme: 'system' as const,
  enrollmentToken: 'mock-enrollment-token-abc123',
};

const enrollmentToken = 'SENT-XXXX-YYYY-ZZZZ-1234';
const serverUrl = 'wss://your-server.com:3000';

export const browserApi = {
  server: {
    getInfo: async () => ({
      port: 3000,
      agentCount: mockDevices.filter(d => d.status === 'online').length,
      enrollmentToken: enrollmentToken,
    }),
    regenerateToken: async () => 'SENT-NEW-TOKEN-' + Date.now(),
    getAgentInstallerCommand: async (platform: string) => {
      switch (platform) {
        case 'windows':
          return `sentinel-agent.exe install --server ${serverUrl} --token ${enrollmentToken}`;
        case 'macos':
          return `sudo ./sentinel-agent install --server ${serverUrl} --token ${enrollmentToken}`;
        case 'linux':
          return `sudo ./sentinel-agent install --server ${serverUrl} --token ${enrollmentToken}`;
        default:
          return 'Unsupported platform';
      }
    },
  },
  devices: {
    list: async () => mockDevices,
    get: async (id: string) => mockDevices.find(d => d.id === id),
    getMetrics: async (deviceId: string, hours: number = 24) => {
      const now = Date.now();
      return Array.from({ length: 24 }, (_, i) => ({
        timestamp: new Date(now - i * 3600000).toISOString(),
        cpuPercent: Math.random() * 60 + 20,
        memoryPercent: Math.random() * 40 + 40,
        memoryUsedBytes: Math.floor(Math.random() * 8e9 + 4e9),
        diskPercent: Math.random() * 30 + 50,
        diskUsedBytes: Math.floor(Math.random() * 200e9 + 100e9),
        networkRxBytes: Math.floor(Math.random() * 1e8),
        networkTxBytes: Math.floor(Math.random() * 5e7),
        processCount: Math.floor(Math.random() * 100 + 100),
      }));
    },
    delete: async (id: string) => {},
  },
  alerts: {
    list: async () => mockAlerts,
    acknowledge: async (id: string) => {},
    resolve: async (id: string) => {},
    getRules: async () => mockRules,
    createRule: async (rule: any) => ({ ...rule, id: Date.now().toString(), createdAt: new Date().toISOString() }),
    updateRule: async (id: string, rule: any) => ({ ...mockRules[0], ...rule }),
    deleteRule: async (id: string) => {},
    onNew: (callback: Function) => () => {},
  },
  settings: {
    get: async () => mockSettings,
    update: async (settings: any) => ({ ...mockSettings, ...settings }),
  },
  scripts: {
    list: async () => [],
    execute: async (deviceId: string, scriptId: string) => ({ success: true, output: 'Mock output' }),
  },
  on: (event: string, callback: Function) => {
    return () => {};
  },
};

// Install browser API if not in Electron
export function installBrowserApi() {
  if (typeof window !== 'undefined' && !(window as any).api) {
    (window as any).api = browserApi;
    console.log('[Browser Mode] Using mock API for development');
  }
}
