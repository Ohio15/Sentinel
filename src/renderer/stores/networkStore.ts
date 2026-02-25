import { create } from 'zustand';
import { api } from '../services/api';

export interface RouterScheduledAction {
  id: string;
  name: string;
  actionType: string;
  cronExpression: string;
  isActive: boolean;
  lastRunAt?: string;
  nextRunAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface AuditLogEntry {
  id: string;
  action: string;
  description: string;
  targetMac?: string;
  metadata: Record<string, unknown>;
  status: string;
  createdAt: string;
}

interface AuditLogsPagination {
  page: number;
  limit: number;
  total: number;
  totalPages: number;
}

interface NetworkState {
  // Scheduled actions
  scheduledActions: RouterScheduledAction[];
  scheduledActionsLoading: boolean;

  // Audit logs
  auditLogs: AuditLogEntry[];
  auditLogsPagination: AuditLogsPagination;
  auditLogsLoading: boolean;

  // Actions
  fetchScheduledActions: () => Promise<void>;
  createScheduledAction: (data: Omit<RouterScheduledAction, 'id' | 'createdAt' | 'updatedAt' | 'lastRunAt' | 'nextRunAt'>) => Promise<void>;
  updateScheduledAction: (id: string, data: Omit<RouterScheduledAction, 'id' | 'createdAt' | 'updatedAt' | 'lastRunAt' | 'nextRunAt'>) => Promise<void>;
  deleteScheduledAction: (id: string) => Promise<void>;
  toggleScheduledAction: (id: string, isActive: boolean) => Promise<void>;
  fetchAuditLogs: (params?: { page?: number; limit?: number; action?: string; search?: string }) => Promise<void>;
}

export const useNetworkStore = create<NetworkState>((set, get) => ({
  scheduledActions: [],
  scheduledActionsLoading: false,

  auditLogs: [],
  auditLogsPagination: { page: 1, limit: 25, total: 0, totalPages: 0 },
  auditLogsLoading: false,

  fetchScheduledActions: async () => {
    set({ scheduledActionsLoading: true });
    try {
      const result = await api.makeRequest<RouterScheduledAction[]>('GET', '/router/scheduled-actions');
      set({ scheduledActions: result || [] });
    } catch (err) {
      console.error('[NetworkStore] Failed to fetch scheduled actions:', err);
    } finally {
      set({ scheduledActionsLoading: false });
    }
  },

  createScheduledAction: async (data) => {
    await api.makeRequest('POST', '/router/scheduled-actions', data);
    await get().fetchScheduledActions();
  },

  updateScheduledAction: async (id, data) => {
    await api.makeRequest('PUT', `/router/scheduled-actions/${id}`, data);
    await get().fetchScheduledActions();
  },

  deleteScheduledAction: async (id) => {
    await api.makeRequest('DELETE', `/router/scheduled-actions/${id}`);
    await get().fetchScheduledActions();
  },

  toggleScheduledAction: async (id, isActive) => {
    await api.makeRequest('POST', `/router/scheduled-actions/${id}/toggle`, { isActive });
    await get().fetchScheduledActions();
  },

  fetchAuditLogs: async (params) => {
    set({ auditLogsLoading: true });
    try {
      const queryParams: Record<string, string> = {};
      if (params?.page) queryParams.page = String(params.page);
      if (params?.limit) queryParams.limit = String(params.limit);
      if (params?.action) queryParams.action = params.action;
      if (params?.search) queryParams.search = params.search;

      const result = await api.makeRequest<{
        data: AuditLogEntry[];
        page: number;
        limit: number;
        total: number;
        totalPages: number;
      }>('GET', '/router/audit-logs', undefined, queryParams);

      set({
        auditLogs: result.data || [],
        auditLogsPagination: {
          page: result.page,
          limit: result.limit,
          total: result.total,
          totalPages: result.totalPages,
        },
      });
    } catch (err) {
      console.error('[NetworkStore] Failed to fetch audit logs:', err);
    } finally {
      set({ auditLogsLoading: false });
    }
  },
}));
