import { create } from 'zustand';
import { devices as devicesService, events, isElectron } from '../services';

export interface GPUInfo {
  name: string;
  vendor: string;
  memory: number;
  driverVersion: string;
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
  status: 'online' | 'offline' | 'warning' | 'critical' | 'disabled' | 'uninstalling';
  ipAddress: string;
  publicIp?: string;
  macAddress: string;
  tags: string[];
  metadata: Record<string, unknown>;
  clientId?: string;
  isDisabled?: boolean;
  disabledAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface GPUMetric {
  name: string;
  utilization: number;
  memoryUsed: number;
  memoryTotal: number;
  temperature?: number;
  powerDraw?: number;
}

export interface NetworkInterfaceMetric {
  name: string;
  rxBytesPerSec: number;
  txBytesPerSec: number;
  rxBytes: number;
  txBytes: number;
  rxPackets: number;
  txPackets: number;
  errorsIn: number;
  errorsOut: number;
}

export interface ProcessMetric {
  pid: number;
  name: string;
  cpuPercent: number;
  memPercent: number;
  memoryRss: number;
  status: string;
  username?: string;
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
  // Extended metrics
  diskReadBytesPerSec?: number;
  diskWriteBytesPerSec?: number;
  memoryCommitted?: number;
  memoryCached?: number;
  memoryPagedPool?: number;
  memoryNonPagedPool?: number;
  gpuMetrics?: GPUMetric[];
  networkInterfaces?: NetworkInterfaceMetric[];
  // Task Manager style per-core CPU and top processes
  cpuPerCore?: number[];
  topProcesses?: ProcessMetric[];
}

interface DeviceState {
  devices: Device[];
  selectedDevice: Device | null;
  metrics: DeviceMetrics[];
  loading: boolean;
  error: string | null;

  fetchDevices: (clientId?: string | null, showLoading?: boolean) => Promise<void>;
  fetchDevice: (id: string) => Promise<void>;
  fetchMetrics: (deviceId: string, hours?: number) => Promise<void>;
  deleteDevice: (id: string) => Promise<void>;
  disableDevice: (id: string) => Promise<void>;
  enableDevice: (id: string) => Promise<void>;
  uninstallDevice: (id: string) => Promise<void>;
  forceUpdateDevice: (id: string) => Promise<void>;
  updateDevice: (id: string, data: { displayName?: string; tags?: string[]; clientId?: string | null }) => Promise<void>;
  subscribeToUpdates: () => () => void;
}

export const useDeviceStore = create<DeviceState>((set, get) => ({
  devices: [],
  selectedDevice: null,
  metrics: [],
  loading: false,
  error: null,

  fetchDevices: async (clientId?: string | null, showLoading: boolean = true) => {
    if (showLoading) set({ loading: true, error: null });
    try {
      // Pass undefined instead of null to get all devices
      const devices = await devicesService.list(clientId || undefined) as unknown as Device[];
      set({ devices, ...(showLoading && { loading: false }) });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error', loading: false });
    }
  },

  fetchDevice: async (id: string) => {
    console.log('[DeviceStore] fetchDevice called with id:', id);
    set({ loading: true, error: null });
    try {
      const device = await devicesService.get(id) as unknown as Device;
      console.log('[DeviceStore] fetchDevice result:', device);
      set({ selectedDevice: device, loading: false });
    } catch (error: unknown) {
      console.error('[DeviceStore] fetchDevice error:', error);
      set({ error: error instanceof Error ? error.message : 'Unknown error', loading: false });
    }
  },

  fetchMetrics: async (deviceId: string, hours: number = 24) => {
    console.log('[DeviceStore] fetchMetrics called for device:', deviceId, 'hours:', hours);
    try {
      const metrics = await devicesService.getMetrics(deviceId, hours) as unknown as DeviceMetrics[];
      console.log('[DeviceStore] fetchMetrics returned', metrics.length, 'metrics');
      set({ metrics });
    } catch (error: unknown) {
      console.error('Failed to fetch metrics:', error);
    }
  },

  deleteDevice: async (id: string) => {
    try {
      await devicesService.delete(id);
      const { devices } = get();
      set({ devices: devices.filter(d => d.id !== id) });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
    }
  },

  disableDevice: async (id: string) => {
    try {
      await devicesService.disable(id);
      const { devices, selectedDevice } = get();
      const updated = devices.map(d =>
        d.id === id ? { ...d, status: 'disabled' as const, isDisabled: true, disabledAt: new Date().toISOString() } : d
      );
      set({
        devices: updated,
        selectedDevice: selectedDevice?.id === id
          ? { ...selectedDevice, status: 'disabled' as const, isDisabled: true, disabledAt: new Date().toISOString() }
          : selectedDevice
      });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
      throw error;
    }
  },

  enableDevice: async (id: string) => {
    try {
      await devicesService.enable(id);
      const { devices, selectedDevice } = get();
      const updated = devices.map(d =>
        d.id === id ? { ...d, status: 'offline' as const, isDisabled: false, disabledAt: undefined } : d
      );
      set({
        devices: updated,
        selectedDevice: selectedDevice?.id === id
          ? { ...selectedDevice, status: 'offline' as const, isDisabled: false, disabledAt: undefined }
          : selectedDevice
      });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
      throw error;
    }
  },

  uninstallDevice: async (id: string) => {
    try {
      await devicesService.uninstall(id);
      const { devices, selectedDevice } = get();
      const updated = devices.map(d =>
        d.id === id ? { ...d, status: 'uninstalling' as const } : d
      );
      set({
        devices: updated,
        selectedDevice: selectedDevice?.id === id
          ? { ...selectedDevice, status: 'uninstalling' as const }
          : selectedDevice
      });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
      throw error;
    }
  },

  forceUpdateDevice: async (id: string) => {
    try {
      await devicesService.forceUpdate(id);
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
      throw error;
    }
  },

  updateDevice: async (id: string, data: { displayName?: string; tags?: string[]; clientId?: string | null }) => {
    try {
      await devicesService.update(id, data);
      const { devices, selectedDevice } = get();
      const updated = devices.map(d =>
        d.id === id ? { ...d, ...data } : d
      );
      set({
        devices: updated,
        selectedDevice: selectedDevice?.id === id
          ? { ...selectedDevice, ...data }
          : selectedDevice
      });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
      throw error;
    }
  },

  subscribeToUpdates: () => {
    // Periodic refresh every 10 seconds for faster agent detection
    const refreshInterval = setInterval(() => {
      get().fetchDevices(undefined, false); // Background refresh, no loading indicator
    }, 10000);

    // Refresh on window focus for immediate updates after tab switch
    const handleFocus = () => {
      console.log('[DeviceStore] Window focused, refreshing devices...');
      get().fetchDevices(undefined, false); // Background refresh, no loading indicator
    };
    window.addEventListener('focus', handleFocus);

    const unsubOnline = events.on('devices:online', async (rawData) => {
      const data = rawData as { agentId: string; deviceId?: string };
      const { devices } = get();
      const existingDevice = devices.find(d => d.agentId === data.agentId);

      if (existingDevice) {
        // Device exists in store, just update status
        const updated = devices.map(d =>
          d.agentId === data.agentId ? { ...d, status: 'online' as const } : d
        );
        set({ devices: updated });
      } else if (data.deviceId) {
        // New device - fetch it and add to store
        console.log('[DeviceStore] New device online, fetching:', data.deviceId);
        try {
          const newDevice = await devicesService.get(data.deviceId) as unknown as Device;
          if (newDevice) {
            set({ devices: [...devices, { ...newDevice, status: 'online' as const }] });
            console.log('[DeviceStore] Added new device to store:', newDevice.hostname);
          }
        } catch (error) {
          console.error('[DeviceStore] Failed to fetch new device:', error);
        }
      }
    });

    const unsubOffline = events.on('devices:offline', (rawData) => {
      const data = rawData as { agentId: string };
      const { devices } = get();
      const updated = devices.map(d =>
        d.agentId === data.agentId ? { ...d, status: 'offline' as const } : d
      );
      set({ devices: updated });
    });

    const unsubUpdated = events.on('devices:updated', (rawData) => {
      const data = rawData as { deviceId: string };
      console.log('[DeviceStore] devices:updated received', data);
      // Refetch devices to get the latest list (no loading indicator)
      get().fetchDevices(undefined, false);
    });

    let metricsReceivedCount = 0;
    const unsubMetrics = events.on('metrics:updated', (rawData) => {
      const data = rawData as { deviceId: string; source?: string; metrics: Record<string, unknown> };
      metricsReceivedCount++;
      const { selectedDevice, metrics } = get();

      // Log every metric with a counter
      console.log(`[DeviceStore] metrics:updated #${metricsReceivedCount}`, {
        dataDeviceId: data.deviceId,
        selectedDeviceId: selectedDevice?.id,
        match: selectedDevice?.id === data.deviceId,
        cpu: (data.metrics?.cpuPercent as number)?.toFixed(1),
        source: data.source,
      });

      if (selectedDevice && selectedDevice.id === data.deviceId) {
        // Validate all required metrics fields
        const m = data.metrics as Record<string, unknown>;
        if (!m ||
            typeof m.cpuPercent !== 'number' ||
            typeof m.memoryPercent !== 'number' ||
            typeof m.diskPercent !== 'number') {
          console.warn('[DeviceStore] Invalid metrics data received:', m);
          return;
        }

        // Sliding window - 120 points = 60 seconds at 500ms intervals (smoother than Task Manager)
        const newMetric: DeviceMetrics = {
          timestamp: new Date().toISOString(),
          cpuPercent: m.cpuPercent as number,
          memoryPercent: m.memoryPercent as number,
          memoryUsedBytes: (m.memoryUsedBytes || m.memory_used || 0) as number,
          memoryTotalBytes: m.memoryTotalBytes as number | undefined,
          diskPercent: m.diskPercent as number,
          diskUsedBytes: (m.diskUsedBytes || m.disk_used || 0) as number,
          diskTotalBytes: m.diskTotalBytes as number | undefined,
          networkRxBytes: (m.networkRxBytes || m.network_rx_bytes || 0) as number,
          networkTxBytes: (m.networkTxBytes || m.network_tx_bytes || 0) as number,
          processCount: (m.processCount || m.process_count || 0) as number,
          uptime: (m.uptime || 0) as number,
          // Extended metrics
          diskReadBytesPerSec: (m.diskReadBytesPerSec || m.disk_read_bytes_sec || 0) as number,
          diskWriteBytesPerSec: (m.diskWriteBytesPerSec || m.disk_write_bytes_sec || 0) as number,
          memoryCommitted: (m.memoryCommitted || m.memory_committed || 0) as number,
          memoryCached: (m.memoryCached || m.memory_cached || 0) as number,
          memoryPagedPool: (m.memoryPagedPool || m.memory_paged_pool || 0) as number,
          memoryNonPagedPool: (m.memoryNonPagedPool || m.memory_non_paged_pool || 0) as number,
          gpuMetrics: (m.gpuMetrics || m.gpu_metrics || []) as GPUMetric[],
          networkInterfaces: (m.networkInterfaces || m.network_interfaces || []) as NetworkInterfaceMetric[],
        };
        const newMetrics = [newMetric, ...metrics.slice(0, 119)];
        set({ metrics: newMetrics });
        console.log('[DeviceStore] metrics updated, count:', newMetrics.length,
          'CPU:', m.cpuPercent?.toFixed(1), 'MEM:', m.memoryPercent?.toFixed(1));
      } else {
        console.log('[DeviceStore] metrics:updated skipped - device mismatch or no selection');
      }
    });

    return () => {
      clearInterval(refreshInterval);
      window.removeEventListener('focus', handleFocus);
      unsubOnline();
      unsubOffline();
      unsubUpdated();
      unsubMetrics();
    };
  },
}));
