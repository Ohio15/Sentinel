import { create } from 'zustand';

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
      const alerts = await window.api.alerts.list();
      set({ alerts, loading: false });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error', loading: false });
    }
  },

  acknowledgeAlert: async (id: string) => {
    try {
      await window.api.alerts.acknowledge(id);
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
      await window.api.alerts.resolve(id);
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
      const rules = await window.api.alerts.getRules();
      set({ rules });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
    }
  },

  createRule: async (rule) => {
    try {
      const newRule = await window.api.alerts.createRule(rule);
      const { rules } = get();
      set({ rules: [...rules, newRule] });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
    }
  },

  updateRule: async (id: string, rule) => {
    try {
      const updatedRule = await window.api.alerts.updateRule(id, rule);
      const { rules } = get();
      set({ rules: rules.map(r => r.id === id ? updatedRule : r) });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
    }
  },

  deleteRule: async (id: string) => {
    try {
      await window.api.alerts.deleteRule(id);
      const { rules } = get();
      set({ rules: rules.filter(r => r.id !== id) });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error' });
    }
  },

  subscribeToAlerts: () => {
    const unsub = window.api.alerts.onNew((alert: Alert) => {
      const { alerts } = get();
      set({ alerts: [alert, ...alerts] });
    });

    return unsub;
  },
}));
