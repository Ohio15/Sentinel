import React, { useState, useEffect } from 'react';

interface Settings {
  serverPort: number;
  agentCheckInterval: number;
  metricsRetentionDays: number;
  alertEmailEnabled: boolean;
  alertEmail?: string;
  theme: 'light' | 'dark' | 'system';
}

interface ServerInfo {
  port: number;
  agentCount: number;
}

export function Settings() {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [serverInfo, setServerInfo] = useState<ServerInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    loadData();
  }, []);

  const loadData = async () => {
    setLoading(true);
    try {
      const [settingsData, infoData] = await Promise.all([
        window.api.settings.get(),
        window.api.server.getInfo(),
      ]);
      setSettings(settingsData);
      setServerInfo(infoData);
    } catch (error) {
      console.error('Failed to load settings:', error);
    } finally {
      setLoading(false);
    }
  };

  const handleSave = async () => {
    if (!settings) return;
    setSaving(true);
    try {
      await window.api.settings.update(settings);
      alert('Settings saved successfully');
    } catch (error: any) {
      alert(`Error saving settings: ${error.message}`);
    } finally {
      setSaving(false);
    }
  };

  if (loading || !settings || !serverInfo) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-text-secondary">Loading settings...</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-text-primary">Settings</h1>

      {/* Server Settings */}
      <div className="card p-6">
        <h2 className="text-lg font-semibold text-text-primary mb-4">Server Settings</h2>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div>
            <label className="label">Server Port</label>
            <input
              type="number"
              value={settings.serverPort}
              onChange={e => setSettings({ ...settings, serverPort: Number(e.target.value) })}
              className="input"
              min="1"
              max="65535"
            />
            <p className="text-xs text-text-secondary mt-1">
              Requires restart to take effect
            </p>
          </div>
          <div>
            <label className="label">Agent Check Interval (seconds)</label>
            <input
              type="number"
              value={settings.agentCheckInterval}
              onChange={e => setSettings({ ...settings, agentCheckInterval: Number(e.target.value) })}
              className="input"
              min="10"
            />
          </div>
          <div>
            <label className="label">Metrics Retention (days)</label>
            <input
              type="number"
              value={settings.metricsRetentionDays}
              onChange={e => setSettings({ ...settings, metricsRetentionDays: Number(e.target.value) })}
              className="input"
              min="1"
            />
          </div>
        </div>
      </div>

      {/* Alert Settings */}
      <div className="card p-6">
        <h2 className="text-lg font-semibold text-text-primary mb-4">Alert Notifications</h2>
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="alertEmail"
              checked={settings.alertEmailEnabled}
              onChange={e => setSettings({ ...settings, alertEmailEnabled: e.target.checked })}
              className="w-4 h-4"
            />
            <label htmlFor="alertEmail" className="text-sm text-text-primary">
              Enable email notifications
            </label>
          </div>
          {settings.alertEmailEnabled && (
            <div>
              <label className="label">Email Address</label>
              <input
                type="email"
                value={settings.alertEmail || ''}
                onChange={e => setSettings({ ...settings, alertEmail: e.target.value })}
                className="input"
                placeholder="alerts@example.com"
              />
            </div>
          )}
        </div>
      </div>

      {/* Server Status */}
      <div className="card p-6">
        <h2 className="text-lg font-semibold text-text-primary mb-4">Server Status</h2>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div className="p-4 bg-gray-50 rounded-lg">
            <p className="text-sm text-text-secondary">Status</p>
            <p className="text-lg font-semibold text-success">Running</p>
          </div>
          <div className="p-4 bg-gray-50 rounded-lg">
            <p className="text-sm text-text-secondary">Port</p>
            <p className="text-lg font-semibold text-text-primary">{serverInfo.port}</p>
          </div>
          <div className="p-4 bg-gray-50 rounded-lg">
            <p className="text-sm text-text-secondary">Connected Agents</p>
            <p className="text-lg font-semibold text-text-primary">{serverInfo.agentCount}</p>
          </div>
        </div>
      </div>

      <div className="flex justify-end">
        <button onClick={handleSave} disabled={saving} className="btn btn-primary">
          {saving ? 'Saving...' : 'Save Settings'}
        </button>
      </div>
    </div>
  );
}
