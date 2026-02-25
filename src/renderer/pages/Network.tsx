import React, { useEffect, useState, useCallback } from 'react';
import { useNetworkStore, type AuditLogEntry, type RouterScheduledAction } from '../stores/networkStore';

type TabId = 'automation' | 'audit';

const ACTION_BADGES: Record<string, { label: string; color: string }> = {
  device_blocked: { label: 'Device Blocked', color: 'bg-red-500/20 text-red-400' },
  device_allowed: { label: 'Device Allowed', color: 'bg-green-500/20 text-green-400' },
  device_marked_known: { label: 'Marked Known', color: 'bg-blue-500/20 text-blue-400' },
  wol_sent: { label: 'WOL Sent', color: 'bg-purple-500/20 text-purple-400' },
  speed_test_run: { label: 'Speed Test', color: 'bg-cyan-500/20 text-cyan-400' },
  schedule_created: { label: 'Schedule Created', color: 'bg-indigo-500/20 text-indigo-400' },
  schedule_deleted: { label: 'Schedule Deleted', color: 'bg-orange-500/20 text-orange-400' },
  schedule_executed: { label: 'Schedule Executed', color: 'bg-teal-500/20 text-teal-400' },
  anomaly_dismissed: { label: 'Anomaly Dismissed', color: 'bg-yellow-500/20 text-yellow-400' },
};

const STATUS_COLORS: Record<string, string> = {
  success: 'text-green-400',
  failure: 'text-red-400',
  error: 'text-red-400',
};

function formatDate(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

// ---- Automation Tab ----

function AutomationTab() {
  const {
    scheduledActions,
    scheduledActionsLoading,
    fetchScheduledActions,
    createScheduledAction,
    deleteScheduledAction,
    toggleScheduledAction,
  } = useNetworkStore();

  const [showCreate, setShowCreate] = useState(false);
  const [newAction, setNewAction] = useState({ name: '', actionType: 'speed_test', cronExpression: '', isActive: false });

  useEffect(() => {
    fetchScheduledActions();
  }, [fetchScheduledActions]);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await createScheduledAction(newAction);
      setShowCreate(false);
      setNewAction({ name: '', actionType: 'speed_test', cronExpression: '', isActive: false });
    } catch (err) {
      console.error('Failed to create scheduled action:', err);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this scheduled action?')) return;
    try {
      await deleteScheduledAction(id);
    } catch (err) {
      console.error('Failed to delete scheduled action:', err);
    }
  };

  const handleToggle = async (action: RouterScheduledAction) => {
    try {
      await toggleScheduledAction(action.id, !action.isActive);
    } catch (err) {
      console.error('Failed to toggle scheduled action:', err);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-text-primary">Scheduled Actions</h3>
        <button
          onClick={() => setShowCreate(!showCreate)}
          className="px-4 py-2 bg-primary text-white rounded-lg text-sm font-medium hover:bg-primary/90 transition-colors"
        >
          {showCreate ? 'Cancel' : '+ New Action'}
        </button>
      </div>

      {showCreate && (
        <form onSubmit={handleCreate} className="bg-surface border border-border rounded-lg p-4 space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-sm text-text-secondary mb-1">Name</label>
              <input
                type="text"
                value={newAction.name}
                onChange={(e) => setNewAction({ ...newAction, name: e.target.value })}
                className="w-full px-3 py-2 bg-background border border-border rounded-lg text-text-primary text-sm"
                placeholder="Daily Speed Test"
                required
              />
            </div>
            <div>
              <label className="block text-sm text-text-secondary mb-1">Type</label>
              <select
                value={newAction.actionType}
                onChange={(e) => setNewAction({ ...newAction, actionType: e.target.value })}
                className="w-full px-3 py-2 bg-background border border-border rounded-lg text-text-primary text-sm"
              >
                <option value="speed_test">Speed Test</option>
                <option value="guest_wifi_on">Guest WiFi On</option>
                <option value="guest_wifi_off">Guest WiFi Off</option>
                <option value="reboot_router">Reboot Router</option>
              </select>
            </div>
          </div>
          <div>
            <label className="block text-sm text-text-secondary mb-1">Cron Expression (5 or 6 field)</label>
            <input
              type="text"
              value={newAction.cronExpression}
              onChange={(e) => setNewAction({ ...newAction, cronExpression: e.target.value })}
              className="w-full px-3 py-2 bg-background border border-border rounded-lg text-text-primary text-sm font-mono"
              placeholder="0 3 * * *"
              required
            />
          </div>
          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="isActive"
              checked={newAction.isActive}
              onChange={(e) => setNewAction({ ...newAction, isActive: e.target.checked })}
              className="rounded border-border"
            />
            <label htmlFor="isActive" className="text-sm text-text-secondary">Active immediately</label>
          </div>
          <button type="submit" className="px-4 py-2 bg-primary text-white rounded-lg text-sm font-medium hover:bg-primary/90">
            Create
          </button>
        </form>
      )}

      {scheduledActionsLoading ? (
        <div className="text-center py-8 text-text-secondary">Loading scheduled actions...</div>
      ) : scheduledActions.length === 0 ? (
        <div className="text-center py-8 text-text-secondary">No scheduled actions yet.</div>
      ) : (
        <div className="bg-surface border border-border rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border">
                <th className="text-left px-4 py-3 text-text-secondary font-medium">Name</th>
                <th className="text-left px-4 py-3 text-text-secondary font-medium">Type</th>
                <th className="text-left px-4 py-3 text-text-secondary font-medium">Schedule</th>
                <th className="text-left px-4 py-3 text-text-secondary font-medium">Status</th>
                <th className="text-left px-4 py-3 text-text-secondary font-medium">Last Run</th>
                <th className="text-right px-4 py-3 text-text-secondary font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {scheduledActions.map((action) => (
                <tr key={action.id} className="border-b border-border last:border-0 hover:bg-background/50">
                  <td className="px-4 py-3 text-text-primary font-medium">{action.name}</td>
                  <td className="px-4 py-3">
                    <span className="px-2 py-1 rounded-full text-xs bg-blue-500/20 text-blue-400">
                      {action.actionType.replace(/_/g, ' ')}
                    </span>
                  </td>
                  <td className="px-4 py-3 font-mono text-text-secondary text-xs">{action.cronExpression}</td>
                  <td className="px-4 py-3">
                    <button
                      onClick={() => handleToggle(action)}
                      className={`px-2 py-1 rounded-full text-xs font-medium ${
                        action.isActive ? 'bg-green-500/20 text-green-400' : 'bg-gray-500/20 text-gray-400'
                      }`}
                    >
                      {action.isActive ? 'Active' : 'Inactive'}
                    </button>
                  </td>
                  <td className="px-4 py-3 text-text-secondary text-xs">
                    {action.lastRunAt ? formatDate(action.lastRunAt) : 'Never'}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <button
                      onClick={() => handleDelete(action.id)}
                      className="text-red-400 hover:text-red-300 text-xs"
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// ---- Audit Tab ----

function AuditTab() {
  const { auditLogs, auditLogsPagination, auditLogsLoading, fetchAuditLogs } = useNetworkStore();
  const [actionFilter, setActionFilter] = useState('');
  const [search, setSearch] = useState('');
  const [searchDebounced, setSearchDebounced] = useState('');

  // Debounce search
  useEffect(() => {
    const timer = setTimeout(() => setSearchDebounced(search), 300);
    return () => clearTimeout(timer);
  }, [search]);

  const loadPage = useCallback(
    (page: number) => {
      fetchAuditLogs({ page, action: actionFilter || undefined, search: searchDebounced || undefined });
    },
    [fetchAuditLogs, actionFilter, searchDebounced]
  );

  useEffect(() => {
    loadPage(1);
  }, [loadPage]);

  const { page, totalPages, total } = auditLogsPagination;

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3 flex-wrap">
        <input
          type="text"
          placeholder="Search descriptions..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="px-3 py-2 bg-background border border-border rounded-lg text-text-primary text-sm w-64"
        />
        <select
          value={actionFilter}
          onChange={(e) => setActionFilter(e.target.value)}
          className="px-3 py-2 bg-background border border-border rounded-lg text-text-primary text-sm"
        >
          <option value="">All Actions</option>
          {Object.entries(ACTION_BADGES).map(([key, { label }]) => (
            <option key={key} value={key}>
              {label}
            </option>
          ))}
        </select>
        <span className="text-text-secondary text-sm ml-auto">{total} entries</span>
      </div>

      {auditLogsLoading ? (
        <div className="text-center py-8 text-text-secondary">Loading audit logs...</div>
      ) : auditLogs.length === 0 ? (
        <div className="text-center py-8 text-text-secondary">No audit log entries found.</div>
      ) : (
        <div className="bg-surface border border-border rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border">
                <th className="text-left px-4 py-3 text-text-secondary font-medium">Timestamp</th>
                <th className="text-left px-4 py-3 text-text-secondary font-medium">Action</th>
                <th className="text-left px-4 py-3 text-text-secondary font-medium">Description</th>
                <th className="text-left px-4 py-3 text-text-secondary font-medium">Target</th>
                <th className="text-left px-4 py-3 text-text-secondary font-medium">Status</th>
              </tr>
            </thead>
            <tbody>
              {auditLogs.map((entry) => {
                const badge = ACTION_BADGES[entry.action] || { label: entry.action, color: 'bg-gray-500/20 text-gray-400' };
                return (
                  <tr key={entry.id} className="border-b border-border last:border-0 hover:bg-background/50">
                    <td className="px-4 py-3 text-text-secondary text-xs whitespace-nowrap">
                      {formatDate(entry.createdAt)}
                    </td>
                    <td className="px-4 py-3">
                      <span className={`px-2 py-1 rounded-full text-xs font-medium ${badge.color}`}>
                        {badge.label}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-text-primary max-w-md truncate">{entry.description}</td>
                    <td className="px-4 py-3 font-mono text-text-secondary text-xs">
                      {entry.targetMac || '—'}
                    </td>
                    <td className="px-4 py-3">
                      <span className={`text-xs font-medium ${STATUS_COLORS[entry.status] || 'text-text-secondary'}`}>
                        {entry.status}
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between">
          <span className="text-sm text-text-secondary">
            Page {page} of {totalPages}
          </span>
          <div className="flex gap-2">
            <button
              onClick={() => loadPage(page - 1)}
              disabled={page <= 1}
              className="px-3 py-1.5 text-sm bg-surface border border-border rounded-lg text-text-primary hover:bg-background disabled:opacity-40 disabled:cursor-not-allowed"
            >
              Previous
            </button>
            <button
              onClick={() => loadPage(page + 1)}
              disabled={page >= totalPages}
              className="px-3 py-1.5 text-sm bg-surface border border-border rounded-lg text-text-primary hover:bg-background disabled:opacity-40 disabled:cursor-not-allowed"
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

// ---- Network Page ----

export function Network() {
  const [activeTab, setActiveTab] = useState<TabId>('automation');

  const tabs: { id: TabId; label: string }[] = [
    { id: 'automation', label: 'Automation' },
    { id: 'audit', label: 'Audit' },
  ];

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-bold text-text-primary">Network</h2>
        <p className="text-text-secondary text-sm mt-1">Router automation, scheduled actions, and audit trail</p>
      </div>

      {/* Tab bar */}
      <div className="border-b border-border">
        <div className="flex gap-6">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={`pb-3 text-sm font-medium border-b-2 transition-colors ${
                activeTab === tab.id
                  ? 'border-primary text-primary'
                  : 'border-transparent text-text-secondary hover:text-text-primary'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>
      </div>

      {/* Tab content */}
      {activeTab === 'automation' && <AutomationTab />}
      {activeTab === 'audit' && <AuditTab />}
    </div>
  );
}
