import React, { useState, useEffect } from 'react';
import { useAlertStore, Alert, AlertRule } from '../stores/alertStore';

export function Alerts() {
  const {
    alerts,
    rules,
    loading,
    fetchAlerts,
    acknowledgeAlert,
    resolveAlert,
    fetchRules,
    createRule,
    updateRule,
    deleteRule,
  } = useAlertStore();

  const [activeTab, setActiveTab] = useState<'alerts' | 'rules'>('alerts');
  const [statusFilter, setStatusFilter] = useState<string>('open');
  const [showRuleModal, setShowRuleModal] = useState(false);
  const [editingRule, setEditingRule] = useState<AlertRule | null>(null);

  useEffect(() => {
    fetchAlerts();
    fetchRules();
  }, []);

  const filteredAlerts = alerts.filter(alert => {
    if (statusFilter === 'all') return true;
    return alert.status === statusFilter;
  });

  const handleEditRule = (rule: AlertRule) => {
    setEditingRule(rule);
    setShowRuleModal(true);
  };

  const handleCreateRule = () => {
    setEditingRule(null);
    setShowRuleModal(true);
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-text-primary">Alerts</h1>
      </div>

      {/* Tabs */}
      <div className="border-b border-border">
        <div className="flex gap-4">
          <button
            onClick={() => setActiveTab('alerts')}
            className={`pb-2 px-1 text-sm font-medium border-b-2 transition-colors ${
              activeTab === 'alerts'
                ? 'border-primary text-primary'
                : 'border-transparent text-text-secondary hover:text-text-primary'
            }`}
          >
            Alerts ({alerts.filter(a => a.status === 'open').length} open)
          </button>
          <button
            onClick={() => setActiveTab('rules')}
            className={`pb-2 px-1 text-sm font-medium border-b-2 transition-colors ${
              activeTab === 'rules'
                ? 'border-primary text-primary'
                : 'border-transparent text-text-secondary hover:text-text-primary'
            }`}
          >
            Alert Rules ({rules.length})
          </button>
        </div>
      </div>

      {activeTab === 'alerts' && (
        <>
          {/* Filter */}
          <div className="flex gap-4">
            <select
              value={statusFilter}
              onChange={e => setStatusFilter(e.target.value)}
              className="input w-40"
            >
              <option value="all">All Status</option>
              <option value="open">Open</option>
              <option value="acknowledged">Acknowledged</option>
              <option value="resolved">Resolved</option>
            </select>
          </div>

          {/* Alert List */}
          {loading ? (
            <div className="card p-8 text-center">
              <p className="text-text-secondary">Loading alerts...</p>
            </div>
          ) : filteredAlerts.length === 0 ? (
            <div className="card p-8 text-center">
              <p className="text-text-secondary">No alerts found</p>
            </div>
          ) : (
            <div className="space-y-3">
              {filteredAlerts.map(alert => (
                <AlertCard
                  key={alert.id}
                  alert={alert}
                  onAcknowledge={() => acknowledgeAlert(alert.id)}
                  onResolve={() => resolveAlert(alert.id)}
                />
              ))}
            </div>
          )}
        </>
      )}

      {activeTab === 'rules' && (
        <>
          <div className="flex justify-end">
            <button onClick={handleCreateRule} className="btn btn-primary">
              + Add Rule
            </button>
          </div>

          {rules.length === 0 ? (
            <div className="card p-8 text-center">
              <p className="text-text-secondary">No alert rules configured</p>
            </div>
          ) : (
            <div className="card overflow-hidden">
              <table>
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Metric</th>
                    <th>Condition</th>
                    <th>Severity</th>
                    <th>Status</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {rules.map(rule => (
                    <tr key={rule.id}>
                      <td>
                        <div>
                          <p className="font-medium text-text-primary">{rule.name}</p>
                          {rule.description && (
                            <p className="text-sm text-text-secondary">{rule.description}</p>
                          )}
                        </div>
                      </td>
                      <td>{formatMetric(rule.metric)}</td>
                      <td>
                        {formatOperator(rule.operator)} {rule.threshold}%
                      </td>
                      <td>
                        <SeverityBadge severity={rule.severity} />
                      </td>
                      <td>
                        <button
                          onClick={() => updateRule(rule.id, { enabled: !rule.enabled })}
                          className={`text-sm font-medium ${
                            rule.enabled ? 'text-success' : 'text-text-secondary'
                          }`}
                        >
                          {rule.enabled ? 'Enabled' : 'Disabled'}
                        </button>
                      </td>
                      <td>
                        <div className="flex gap-2">
                          <button
                            onClick={() => handleEditRule(rule)}
                            className="text-text-secondary hover:text-primary transition-colors"
                          >
                            <EditIcon className="w-4 h-4" />
                          </button>
                          <button
                            onClick={() => deleteRule(rule.id)}
                            className="text-text-secondary hover:text-danger transition-colors"
                          >
                            <TrashIcon className="w-4 h-4" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}

      {/* Rule Modal */}
      {showRuleModal && (
        <RuleModal
          rule={editingRule}
          onClose={() => setShowRuleModal(false)}
          onSave={async (rule) => {
            if (editingRule) {
              await updateRule(editingRule.id, rule);
            } else {
              await createRule(rule as any);
            }
            setShowRuleModal(false);
          }}
        />
      )}
    </div>
  );
}

function AlertCard({
  alert,
  onAcknowledge,
  onResolve,
}: {
  alert: Alert;
  onAcknowledge: () => void;
  onResolve: () => void;
}) {
  const severityColors = {
    info: 'border-l-blue-500',
    warning: 'border-l-warning',
    critical: 'border-l-danger',
  };

  return (
    <div className={`card p-4 border-l-4 ${severityColors[alert.severity]}`}>
      <div className="flex items-start justify-between">
        <div className="flex-1">
          <div className="flex items-center gap-2 mb-1">
            <SeverityBadge severity={alert.severity} />
            <StatusBadge status={alert.status} />
          </div>
          <h3 className="font-medium text-text-primary">{alert.title}</h3>
          <p className="text-sm text-text-secondary mt-1">{alert.message}</p>
          <div className="flex items-center gap-4 mt-2 text-xs text-text-secondary">
            <span>Device: {alert.deviceName}</span>
            <span>Created: {new Date(alert.createdAt).toLocaleString()}</span>
          </div>
        </div>
        {alert.status === 'open' && (
          <div className="flex gap-2">
            <button onClick={onAcknowledge} className="btn btn-secondary text-sm">
              Acknowledge
            </button>
            <button onClick={onResolve} className="btn btn-primary text-sm">
              Resolve
            </button>
          </div>
        )}
        {alert.status === 'acknowledged' && (
          <button onClick={onResolve} className="btn btn-primary text-sm">
            Resolve
          </button>
        )}
      </div>
    </div>
  );
}

function SeverityBadge({ severity }: { severity: Alert['severity'] }) {
  const styles = {
    info: 'badge-info',
    warning: 'badge-warning',
    critical: 'badge-danger',
  };

  return (
    <span className={`badge ${styles[severity]}`}>
      {severity.charAt(0).toUpperCase() + severity.slice(1)}
    </span>
  );
}

function StatusBadge({ status }: { status: Alert['status'] }) {
  const styles = {
    open: 'bg-red-100 text-red-600',
    acknowledged: 'bg-yellow-100 text-yellow-600',
    resolved: 'bg-green-100 text-green-600',
  };

  return (
    <span className={`badge ${styles[status]}`}>
      {status.charAt(0).toUpperCase() + status.slice(1)}
    </span>
  );
}

function RuleModal({
  rule,
  onClose,
  onSave,
}: {
  rule: AlertRule | null;
  onClose: () => void;
  onSave: (rule: Partial<AlertRule>) => void;
}) {
  const [formData, setFormData] = useState({
    name: rule?.name || '',
    description: rule?.description || '',
    metric: rule?.metric || 'cpu_percent',
    operator: rule?.operator || 'gt',
    threshold: rule?.threshold || 90,
    severity: rule?.severity || 'warning',
    cooldownMinutes: rule?.cooldownMinutes || 15,
    enabled: rule?.enabled ?? true,
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSave(formData);
  };

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-surface rounded-lg shadow-xl w-full max-w-md p-6">
        <h2 className="text-lg font-semibold text-text-primary mb-4">
          {rule ? 'Edit Alert Rule' : 'Create Alert Rule'}
        </h2>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="label">Name</label>
            <input
              type="text"
              value={formData.name}
              onChange={e => setFormData({ ...formData, name: e.target.value })}
              className="input"
              required
            />
          </div>
          <div>
            <label className="label">Description</label>
            <input
              type="text"
              value={formData.description}
              onChange={e => setFormData({ ...formData, description: e.target.value })}
              className="input"
            />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">Metric</label>
              <select
                value={formData.metric}
                onChange={e => setFormData({ ...formData, metric: e.target.value })}
                className="input"
              >
                <option value="cpu_percent">CPU Usage</option>
                <option value="memory_percent">Memory Usage</option>
                <option value="disk_percent">Disk Usage</option>
              </select>
            </div>
            <div>
              <label className="label">Operator</label>
              <select
                value={formData.operator}
                onChange={e => setFormData({ ...formData, operator: e.target.value as any })}
                className="input"
              >
                <option value="gt">Greater than</option>
                <option value="lt">Less than</option>
                <option value="gte">Greater or equal</option>
                <option value="lte">Less or equal</option>
                <option value="eq">Equal to</option>
              </select>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">Threshold (%)</label>
              <input
                type="number"
                value={formData.threshold}
                onChange={e => setFormData({ ...formData, threshold: Number(e.target.value) })}
                className="input"
                min="0"
                max="100"
              />
            </div>
            <div>
              <label className="label">Severity</label>
              <select
                value={formData.severity}
                onChange={e => setFormData({ ...formData, severity: e.target.value as any })}
                className="input"
              >
                <option value="info">Info</option>
                <option value="warning">Warning</option>
                <option value="critical">Critical</option>
              </select>
            </div>
          </div>
          <div>
            <label className="label">Cooldown (minutes)</label>
            <input
              type="number"
              value={formData.cooldownMinutes}
              onChange={e => setFormData({ ...formData, cooldownMinutes: Number(e.target.value) })}
              className="input"
              min="1"
            />
          </div>
          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="enabled"
              checked={formData.enabled}
              onChange={e => setFormData({ ...formData, enabled: e.target.checked })}
              className="w-4 h-4"
            />
            <label htmlFor="enabled" className="text-sm text-text-primary">
              Enabled
            </label>
          </div>
          <div className="flex justify-end gap-2 pt-4">
            <button type="button" onClick={onClose} className="btn btn-secondary">
              Cancel
            </button>
            <button type="submit" className="btn btn-primary">
              {rule ? 'Update' : 'Create'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function formatMetric(metric: string): string {
  const metrics: Record<string, string> = {
    cpu_percent: 'CPU Usage',
    memory_percent: 'Memory Usage',
    disk_percent: 'Disk Usage',
  };
  return metrics[metric] || metric;
}

function formatOperator(operator: string): string {
  const operators: Record<string, string> = {
    gt: '>',
    lt: '<',
    gte: '>=',
    lte: '<=',
    eq: '=',
  };
  return operators[operator] || operator;
}

function EditIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
    </svg>
  );
}

function TrashIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
    </svg>
  );
}
