import { create } from 'zustand';

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
  osType: string;
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
  lastSeen: string;
  status: 'online' | 'offline' | 'warning' | 'critical';
  ipAddress: string;
  publicIp?: string;
  macAddress: string;
  tags: string[];
  metadata: Record<string, any>;
  createdAt: string;
  updatedAt: string;
}

export interface DeviceMetrics {
  timestamp: string;
  cpuPercent: number;
  memoryPercent: number;
  memoryUsedBytes: number;
  memoryTotalBytes?: number;
  diskPercent: number;
  diskUsedBytes: number;
  diskTotalBytes?: number;
  networkRxBytes: number;
  networkTxBytes: number;
  processCount: number;
  uptime: number; // System uptime in seconds
}

interface DeviceState {
  devices: Device[];
  selectedDevice: Device | null;
  metrics: DeviceMetrics[];
  loading: boolean;
  error: string | null;

  fetchDevices: () => Promise<void>;
  fetchDevice: (id: string) => Promise<void>;
  fetchMetrics: (deviceId: string, hours?: number) => Promise<void>;
  deleteDevice: (id: string) => Promise<void>;
  subscribeToUpdates: () => () => void;
}

export const useDeviceStore = create<DeviceState>((set, get) => ({
  devices: [],
  selectedDevice: null,
  metrics: [],
  loading: false,
  error: null,

  fetchDevices: async () => {
    set({ loading: true, error: null });
    try {
      const devices = await window.api.devices.list();
      set({ devices, loading: false });
    } catch (error: any) {
      set({ error: error.message, loading: false });
    }
  },

  fetchDevice: async (id: string) => {
    console.log('[DeviceStore] fetchDevice called with id:', id);
    set({ loading: true, error: null });
    try {
      const device = await window.api.devices.get(id);
      console.log('[DeviceStore] fetchDevice result:', device);
      set({ selectedDevice: device, loading: false });
    } catch (error: any) {
      console.error('[DeviceStore] fetchDevice error:', error);
      set({ error: error.message, loading: false });
    }
  },

  fetchMetrics: async (deviceId: string, hours: number = 24) => {
    try {
      const metrics = await window.api.devices.getMetrics(deviceId, hours);
      set({ metrics });
    } catch (error: any) {
      console.error('Failed to fetch metrics:', error);
    }
  },

  deleteDevice: async (id: string) => {
    try {
      await window.api.devices.delete(id);
      const { devices } = get();
      set({ devices: devices.filter(d => d.id !== id) });
    } catch (error: any) {
      set({ error: error.message });
    }
  },

  subscribeToUpdates: () => {
    const unsubOnline = window.api.on('devices:online', (data: any) => {
      const { devices } = get();
      const updated = devices.map(d =>
        d.agentId === data.agentId ? { ...d, status: 'online' as const } : d
      );
      set({ devices: updated });
    });

    const unsubOffline = window.api.on('devices:offline', (data: any) => {
      const { devices } = get();
      const updated = devices.map(d =>
        d.agentId === data.agentId ? { ...d, status: 'offline' as const } : d
      );
      set({ devices: updated });
    });

    const unsubUpdated = window.api.on('devices:updated', () => {
      get().fetchDevices();
    });

    const unsubMetrics = window.api.on('metrics:updated', (data: any) => {
      const { selectedDevice, metrics } = get();
      if (selectedDevice && selectedDevice.id === data.deviceId) {
        const newMetrics = [{
          timestamp: new Date().toISOString(),
          ...data.metrics,
        }, ...metrics.slice(0, 99)];
        set({ metrics: newMetrics });
      }
    });

    return () => {
      unsubOnline();
      unsubOffline();
      unsubUpdated();
      unsubMetrics();
    };
  },
}));
