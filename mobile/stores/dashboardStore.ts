/**
 * Dashboard Store - Sentinel Mobile
 * Manages dashboard state including stats and recent alerts
 */
import { create } from 'zustand';
import { api } from '@/services/api';

export interface Alert {
  id: string;
  title: string;
  message: string;
  severity: 'info' | 'warning' | 'critical';
  status: 'open' | 'acknowledged' | 'resolved';
  deviceId?: string;
  deviceName?: string;
  createdAt: string;
  acknowledgedAt?: string;
  resolvedAt?: string;
}

export interface DashboardStats {
  deviceCount: number;
  onlineCount: number;
  offlineCount: number;
  alertCounts: {
    total: number;
    open: number;
    critical: number;
    warning: number;
    info: number;
  };
  ticketCount: number;
  openTicketCount: number;
}

interface DashboardState {
  stats: DashboardStats;
  recentAlerts: Alert[];
  loading: boolean;
  refreshing: boolean;
  error: string | null;
  lastUpdated: Date | null;

  fetchDashboardData: () => Promise<void>;
  refresh: () => Promise<void>;
}

const initialStats: DashboardStats = {
  deviceCount: 0,
  onlineCount: 0,
  offlineCount: 0,
  alertCounts: {
    total: 0,
    open: 0,
    critical: 0,
    warning: 0,
    info: 0,
  },
  ticketCount: 0,
  openTicketCount: 0,
};

export const useDashboardStore = create<DashboardState>((set, get) => ({
  stats: initialStats,
  recentAlerts: [],
  loading: false,
  refreshing: false,
  error: null,
  lastUpdated: null,

  fetchDashboardData: async () => {
    const { loading } = get();
    if (loading) return;

    set({ loading: true, error: null });
    try {
      // Fetch dashboard stats and alerts in parallel
      const [statsResponse, alertsResponse] = await Promise.all([
        api.getDashboardStats(),
        api.getAlerts({ status: 'open' }),
      ]);

      // Map API response to our stats structure
      const stats: DashboardStats = {
        deviceCount: statsResponse.totalDevices || 0,
        onlineCount: statsResponse.onlineDevices || 0,
        offlineCount: statsResponse.offlineDevices || 0,
        alertCounts: {
          total: alertsResponse.total || 0,
          open: statsResponse.activeAlerts || 0,
          critical: statsResponse.criticalAlerts || 0,
          warning: 0, // Will be calculated from alerts
          info: 0,
        },
        ticketCount: 0, // TODO: Add tickets API
        openTicketCount: 0,
      };

      // Count severity breakdown from alerts
      const alerts = alertsResponse.alerts || [];
      stats.alertCounts.warning = alerts.filter(a => a.severity === 'warning').length;
      stats.alertCounts.info = alerts.filter(a => a.severity === 'info').length;

      // Get recent alerts (last 5 open alerts)
      const recentAlerts: Alert[] = alerts
        .slice(0, 5)
        .map(a => ({
          id: a.id,
          title: a.title,
          message: a.message,
          severity: a.severity as 'info' | 'warning' | 'critical',
          status: a.status as 'open' | 'acknowledged' | 'resolved',
          deviceId: a.deviceId,
          deviceName: a.deviceName,
          createdAt: a.createdAt,
          acknowledgedAt: a.acknowledgedAt,
          resolvedAt: a.resolvedAt,
        }));

      set({
        stats,
        recentAlerts,
        loading: false,
        lastUpdated: new Date(),
      });
    } catch (error) {
      set({
        error: error instanceof Error ? error.message : 'Failed to fetch dashboard data',
        loading: false,
      });
    }
  },

  refresh: async () => {
    set({ refreshing: true });
    try {
      await get().fetchDashboardData();
    } finally {
      set({ refreshing: false });
    }
  },
}));
