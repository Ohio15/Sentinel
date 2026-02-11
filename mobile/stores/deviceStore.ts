/**
 * Device Store for Sentinel Mobile
 * Manages device state with Zustand
 */
import { create } from 'zustand';
import { api } from '@/services/api';

export type DeviceStatus = 'online' | 'offline' | 'warning' | 'critical' | 'disabled' | 'uninstalling';

export interface Device {
  id: string;
  hostname: string;
  displayName?: string;
  status: DeviceStatus;
  osType: string;
  osVersion?: string;
  ipAddress?: string;
  agentVersion?: string;
  lastSeen?: string;
  cpuUsage?: number;
  memoryUsage?: number;
  diskUsage?: number;
  tags?: string[];
}

export interface DeviceMetrics {
  cpuUsage?: number;
  memoryUsage?: number;
  diskUsage?: number;
  networkIn?: number;
  networkOut?: number;
  processCount?: number;
  uptime?: number;
}

export interface Alert {
  id: string;
  title: string;
  message: string;
  severity: string;
  status: string;
  deviceId?: string;
  deviceName?: string;
  createdAt: string;
}

export type StatusFilter = 'all' | 'online' | 'offline' | 'warning' | 'critical';

interface DeviceState {
  // Data
  devices: Device[];
  selectedDevice: Device | null;
  deviceAlerts: Alert[];
  deviceMetrics: DeviceMetrics | null;

  // Filters
  searchQuery: string;
  statusFilter: StatusFilter;

  // UI State
  loading: boolean;
  refreshing: boolean;
  detailLoading: boolean;
  error: string | null;

  // Actions
  fetchDevices: () => Promise<void>;
  refreshDevices: () => Promise<void>;
  fetchDeviceDetail: (id: string) => Promise<void>;
  fetchDeviceAlerts: (deviceId: string) => Promise<void>;
  setSearchQuery: (query: string) => void;
  setStatusFilter: (filter: StatusFilter) => void;
  clearError: () => void;

  // Device Actions
  pingDevice: (id: string) => Promise<{ success: boolean; latency?: number }>;
  rebootDevice: (id: string) => Promise<void>;
  disableDevice: (id: string) => Promise<void>;
  enableDevice: (id: string) => Promise<void>;

  // Computed
  filteredDevices: () => Device[];
}

export const useDeviceStore = create<DeviceState>((set, get) => ({
  // Initial State
  devices: [],
  selectedDevice: null,
  deviceAlerts: [],
  deviceMetrics: null,
  searchQuery: '',
  statusFilter: 'all',
  loading: false,
  refreshing: false,
  detailLoading: false,
  error: null,

  // Actions
  fetchDevices: async () => {
    set({ loading: true, error: null });
    try {
      const response = await api.getDevices();
      set({ devices: response.devices, loading: false });
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to fetch devices';
      set({ error: message, loading: false });
      console.error('[DeviceStore] fetchDevices error:', error);
    }
  },

  refreshDevices: async () => {
    set({ refreshing: true, error: null });
    try {
      const response = await api.getDevices();
      set({ devices: response.devices, refreshing: false });
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to refresh devices';
      set({ error: message, refreshing: false });
      console.error('[DeviceStore] refreshDevices error:', error);
    }
  },

  fetchDeviceDetail: async (id: string) => {
    set({ detailLoading: true, error: null, selectedDevice: null });
    try {
      const device = await api.getDevice(id);
      set({
        selectedDevice: device,
        detailLoading: false,
        deviceMetrics: {
          cpuUsage: device.cpuUsage,
          memoryUsage: device.memoryUsage,
          diskUsage: device.diskUsage,
        }
      });
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to fetch device details';
      set({ error: message, detailLoading: false });
      console.error('[DeviceStore] fetchDeviceDetail error:', error);
    }
  },

  fetchDeviceAlerts: async (deviceId: string) => {
    try {
      const response = await api.getAlerts({ status: 'active' });
      // Filter alerts for this specific device
      const deviceAlerts = response.alerts.filter(
        (alert: Alert) => alert.deviceId === deviceId
      );
      set({ deviceAlerts });
    } catch (error) {
      console.error('[DeviceStore] fetchDeviceAlerts error:', error);
      set({ deviceAlerts: [] });
    }
  },

  setSearchQuery: (query: string) => {
    set({ searchQuery: query });
  },

  setStatusFilter: (filter: StatusFilter) => {
    set({ statusFilter: filter });
  },

  clearError: () => {
    set({ error: null });
  },

  // Device Actions
  pingDevice: async (id: string) => {
    try {
      const result = await api.pingAgent(id);
      return result;
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Ping failed';
      throw new Error(message);
    }
  },

  rebootDevice: async (id: string) => {
    try {
      await api.devicePowerAction(id, 'restart');
      // Update device status in store
      const { devices } = get();
      const updated = devices.map(d =>
        d.id === id ? { ...d, status: 'offline' as DeviceStatus } : d
      );
      set({ devices: updated });
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Reboot failed';
      throw new Error(message);
    }
  },

  disableDevice: async (id: string) => {
    try {
      await api.post(`/devices/${id}/disable`);
      // Update device status in store
      const { devices, selectedDevice } = get();
      const updated = devices.map(d =>
        d.id === id ? { ...d, status: 'disabled' as DeviceStatus } : d
      );
      set({
        devices: updated,
        selectedDevice: selectedDevice?.id === id
          ? { ...selectedDevice, status: 'disabled' as DeviceStatus }
          : selectedDevice
      });
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to disable device';
      throw new Error(message);
    }
  },

  enableDevice: async (id: string) => {
    try {
      await api.post(`/devices/${id}/enable`);
      // Update device status in store
      const { devices, selectedDevice } = get();
      const updated = devices.map(d =>
        d.id === id ? { ...d, status: 'offline' as DeviceStatus } : d
      );
      set({
        devices: updated,
        selectedDevice: selectedDevice?.id === id
          ? { ...selectedDevice, status: 'offline' as DeviceStatus }
          : selectedDevice
      });
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to enable device';
      throw new Error(message);
    }
  },

  // Computed
  filteredDevices: () => {
    const { devices, searchQuery, statusFilter } = get();

    return devices.filter(device => {
      // Search filter
      const searchLower = searchQuery.toLowerCase();
      const matchesSearch =
        !searchQuery ||
        device.hostname.toLowerCase().includes(searchLower) ||
        device.displayName?.toLowerCase().includes(searchLower) ||
        device.ipAddress?.includes(searchQuery);

      // Status filter
      const matchesStatus =
        statusFilter === 'all' ||
        device.status === statusFilter;

      return matchesSearch && matchesStatus;
    });
  },
}));
