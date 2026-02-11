/**
 * Alert Store - Zustand store for alert state management
 * Handles alerts list, filtering, and actions
 */
import { create } from 'zustand';
import { api } from '../services/api';

export type AlertSeverity = 'info' | 'warning' | 'critical';
export type AlertStatus = 'open' | 'acknowledged' | 'resolved';

export interface Alert {
  id: string;
  title: string;
  message: string;
  severity: AlertSeverity;
  status: AlertStatus;
  deviceId?: string;
  deviceName?: string;
  ruleId?: string;
  createdAt: string;
  acknowledgedAt?: string;
  resolvedAt?: string;
  acknowledgedBy?: string;
  resolvedBy?: string;
}

export interface AlertFilters {
  status: AlertStatus | 'all';
  severity: AlertSeverity | 'all';
}

interface AlertState {
  // State
  alerts: Alert[];
  loading: boolean;
  refreshing: boolean;
  error: string | null;
  filters: AlertFilters;

  // Computed counts (updated on alert changes)
  openCount: number;
  criticalCount: number;
  warningCount: number;

  // Actions
  fetchAlerts: () => Promise<void>;
  refreshAlerts: () => Promise<void>;
  acknowledgeAlert: (id: string) => Promise<void>;
  resolveAlert: (id: string) => Promise<void>;
  setFilters: (filters: Partial<AlertFilters>) => void;
  clearError: () => void;
  getFilteredAlerts: () => Alert[];
  getAlertById: (id: string) => Alert | undefined;
}

function calculateCounts(alerts: Alert[]) {
  return {
    openCount: alerts.filter(a => a.status === 'open').length,
    criticalCount: alerts.filter(a => a.status === 'open' && a.severity === 'critical').length,
    warningCount: alerts.filter(a => a.status === 'open' && a.severity === 'warning').length,
  };
}

export const useAlertStore = create<AlertState>()((set, get) => ({
  // Initial state
  alerts: [],
  loading: false,
  refreshing: false,
  error: null,
  filters: {
    status: 'all',
    severity: 'all',
  },
  openCount: 0,
  criticalCount: 0,
  warningCount: 0,

  /**
   * Fetch alerts from the API
   */
  fetchAlerts: async () => {
    const state = get();
    if (state.loading) return;

    set({ loading: true, error: null });

    try {
      console.log('[AlertStore] Fetching alerts...');
      const response = await api.getAlerts();
      const alerts = response.alerts as Alert[];
      const counts = calculateCounts(alerts);

      set({
        alerts,
        loading: false,
        ...counts,
      });

      console.log(`[AlertStore] Fetched ${alerts.length} alerts, ${counts.openCount} open`);
    } catch (err: unknown) {
      console.error('[AlertStore] Failed to fetch alerts:', err);
      const error = err as Error;
      set({
        loading: false,
        error: error.message || 'Failed to fetch alerts',
      });
    }
  },

  /**
   * Refresh alerts (pull-to-refresh)
   */
  refreshAlerts: async () => {
    const state = get();
    if (state.refreshing) return;

    set({ refreshing: true, error: null });

    try {
      console.log('[AlertStore] Refreshing alerts...');
      const response = await api.getAlerts();
      const alerts = response.alerts as Alert[];
      const counts = calculateCounts(alerts);

      set({
        alerts,
        refreshing: false,
        ...counts,
      });

      console.log(`[AlertStore] Refreshed ${alerts.length} alerts`);
    } catch (err: unknown) {
      console.error('[AlertStore] Failed to refresh alerts:', err);
      const error = err as Error;
      set({
        refreshing: false,
        error: error.message || 'Failed to refresh alerts',
      });
    }
  },

  /**
   * Acknowledge an alert
   */
  acknowledgeAlert: async (id: string) => {
    try {
      console.log(`[AlertStore] Acknowledging alert ${id}...`);
      await api.acknowledgeAlert(id);

      // Optimistically update local state
      const alerts = get().alerts.map(alert =>
        alert.id === id
          ? {
              ...alert,
              status: 'acknowledged' as AlertStatus,
              acknowledgedAt: new Date().toISOString(),
            }
          : alert
      );

      const counts = calculateCounts(alerts);
      set({ alerts, ...counts });

      console.log(`[AlertStore] Alert ${id} acknowledged`);
    } catch (err: unknown) {
      console.error('[AlertStore] Failed to acknowledge alert:', err);
      const error = err as Error;
      set({ error: error.message || 'Failed to acknowledge alert' });
      throw err;
    }
  },

  /**
   * Resolve an alert
   */
  resolveAlert: async (id: string) => {
    try {
      console.log(`[AlertStore] Resolving alert ${id}...`);
      await api.resolveAlert(id);

      // Optimistically update local state
      const alerts = get().alerts.map(alert =>
        alert.id === id
          ? {
              ...alert,
              status: 'resolved' as AlertStatus,
              resolvedAt: new Date().toISOString(),
            }
          : alert
      );

      const counts = calculateCounts(alerts);
      set({ alerts, ...counts });

      console.log(`[AlertStore] Alert ${id} resolved`);
    } catch (err: unknown) {
      console.error('[AlertStore] Failed to resolve alert:', err);
      const error = err as Error;
      set({ error: error.message || 'Failed to resolve alert' });
      throw err;
    }
  },

  /**
   * Set filter values
   */
  setFilters: (filters: Partial<AlertFilters>) => {
    const current = get().filters;
    set({ filters: { ...current, ...filters } });
  },

  /**
   * Clear any error message
   */
  clearError: () => set({ error: null }),

  /**
   * Get filtered alerts based on current filters
   */
  getFilteredAlerts: () => {
    const { alerts, filters } = get();

    return alerts.filter(alert => {
      // Filter by status
      if (filters.status !== 'all' && alert.status !== filters.status) {
        return false;
      }

      // Filter by severity
      if (filters.severity !== 'all' && alert.severity !== filters.severity) {
        return false;
      }

      return true;
    });
  },

  /**
   * Get a single alert by ID
   */
  getAlertById: (id: string) => {
    return get().alerts.find(alert => alert.id === id);
  },
}));
