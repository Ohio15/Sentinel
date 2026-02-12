import { create } from 'zustand';
import { alerts as alertsService } from '../services';

export interface AlertMetadata {
  sessionId?: string;
  fileCount?: number;
  usbDeviceId?: string;
}

export interface Alert {
  id: string;
  deviceId: string;
  deviceName: string;
  ruleId?: string;
  severity: 'info' | 'warning' | 'critical';
  title: string;
  message: string;
  status: 'open' | 'acknowledged' | 'resolved';
  createdAt: string;
  acknowledgedAt?: string;
  resolvedAt?: string;
  metadata?: AlertMetadata;
}

export interface AlertRule {
  id: string;
  name: string;
  description?: string;
  enabled: boolean;
  metric: string;
  operator: 'gt' | 'lt' | 'eq' | 'gte' | 'lte';
  threshold: number;
  severity: 'info' | 'warning' | 'critical';
  cooldownMinutes: number;
  createdAt: string;
}

interface AlertState {
  alerts: Alert[];
  rules: AlertRule[];
  loading: boolean;
  error: string | null;

  fetchAlerts: () => Promise<void>;
  acknowledgeAlert: (id: string) => Promise<void>;
  resolveAlert: (id: string) => Promise<void>;
  fetchRules: () => Promise<void>;
  createRule: (rule: Omit<AlertRule, 'id' | 'createdAt'>) => Promise<void>;
  updateRule: (id: string, rule: Partial<AlertRule>) => Promise<void>;
  deleteRule: (id: string) => Promise<void>;
  subscribeToAlerts: () => () => void;
}

export const useAlertStore = create<AlertState>((set, get) => ({
  alerts: [],
  rules: [],
  loading: false,
  error: null,

  fetchAlerts: async () => {
    set({ loading: true, error: null });
    try {
      const alerts = await alertsService.list();
      set({ alerts: alerts as Alert[], loading: false });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error', loading: false });
    }
  },

  acknowledgeAlert: async (id: string) => {
    try {
      await alertsService.acknowledge(id);
      const { alerts } = get();
      set({
        alerts: alerts.map(a =>
          a.id === id ? { ...a, status: 'acknowledged' as const, acknowledgedAt: new Date().toISOString() } : a
        ),
      });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
    }
  },

  resolveAlert: async (id: string) => {
    try {
      await alertsService.resolve(id);
      const { alerts } = get();
      set({
        alerts: alerts.map(a =>
          a.id === id ? { ...a, status: 'resolved' as const, resolvedAt: new Date().toISOString() } : a
        ),
      });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
    }
  },

  fetchRules: async () => {
    try {
      const rules = await alertsService.getRules();
      set({ rules: rules as AlertRule[] });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
    }
  },

  createRule: async (rule) => {
    try {
      const newRule = await alertsService.createRule(rule as any);
      const { rules } = get();
      set({ rules: [...rules, newRule as AlertRule] });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
    }
  },

  updateRule: async (id: string, rule) => {
    try {
      const updatedRule = await alertsService.updateRule(id, rule);
      const { rules } = get();
      set({ rules: rules.map(r => r.id === id ? updatedRule as AlertRule : r) });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
    }
  },

  deleteRule: async (id: string) => {
    try {
      await alertsService.deleteRule(id);
      const { rules } = get();
      set({ rules: rules.filter(r => r.id !== id) });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
    }
  },

  subscribeToAlerts: () => {
    const unsub = alertsService.onNew((alert) => {
      const { alerts } = get();
      set({ alerts: [alert as Alert, ...alerts] });
    });

    return unsub;
  },
}));
