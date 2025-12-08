import { create } from 'zustand';
import type { Device, DeviceMetrics } from '@/types';

interface DeviceState {
  devices: Device[];
  selectedDevice: Device | null;
  deviceMetrics: Map<string, DeviceMetrics[]>;
  isLoading: boolean;
  error: string | null;

  setDevices: (devices: Device[]) => void;
  updateDevice: (device: Device) => void;
  removeDevice: (id: string) => void;
  setSelectedDevice: (device: Device | null) => void;
  updateDeviceStatus: (deviceId: string, status: Device['status'], lastSeen: string) => void;
  updateDeviceMetrics: (deviceId: string, metrics: DeviceMetrics) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
}

export const useDeviceStore = create<DeviceState>((set, get) => ({
  devices: [],
  selectedDevice: null,
  deviceMetrics: new Map(),
  isLoading: false,
  error: null,

  setDevices: (devices) => set({ devices }),

  updateDevice: (device) => {
    set((state) => ({
      devices: state.devices.map((d) => (d.id === device.id ? device : d)),
      selectedDevice: state.selectedDevice?.id === device.id ? device : state.selectedDevice,
    }));
  },

  removeDevice: (id) => {
    set((state) => ({
      devices: state.devices.filter((d) => d.id !== id),
      selectedDevice: state.selectedDevice?.id === id ? null : state.selectedDevice,
    }));
  },

  setSelectedDevice: (device) => set({ selectedDevice: device }),

  updateDeviceStatus: (deviceId, status, lastSeen) => {
    set((state) => ({
      devices: state.devices.map((d) =>
        d.id === deviceId ? { ...d, status, lastSeen } : d
      ),
      selectedDevice:
        state.selectedDevice?.id === deviceId
          ? { ...state.selectedDevice, status, lastSeen }
          : state.selectedDevice,
    }));
  },

  updateDeviceMetrics: (deviceId, metrics) => {
    const currentMetrics = get().deviceMetrics;
    const deviceMetricsList = currentMetrics.get(deviceId) || [];

    // Keep last 60 data points (1 hour at 1-minute intervals)
    const updatedMetrics = [...deviceMetricsList, metrics].slice(-60);

    const newMetricsMap = new Map(currentMetrics);
    newMetricsMap.set(deviceId, updatedMetrics);

    set({ deviceMetrics: newMetricsMap });
  },

  setLoading: (isLoading) => set({ isLoading }),

  setError: (error) => set({ error }),
}));
