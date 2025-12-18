import React, { useState, useEffect } from 'react';
import { useThemeStore } from '../stores/themeStore';

interface Settings {
  serverPort: number;
  agentCheckInterval: number;
  metricsRetentionDays: number;
  alertEmailEnabled: boolean;
  alertEmail?: string;
}

interface ServerInfo {
  port: number;
  agentCount: number;
}

interface PortalSettings {
  azureAd: {
    clientId: string;
    clientSecret: string;
    redirectUri: string;
  };
  email: {
    enabled: boolean;
    smtp?: {
      host: string;
      port: number;
      secure: boolean;
      user: string;
      password: string;
      fromAddress: string;
      fromName?: string;
    };
    portalUrl?: string;
  };
}

interface ClientTenant {
  id: string;
  clientId: string;
  tenantId: string;
  tenantName?: string;
  clientName?: string;
  createdAt: string;
}

interface Client {
  id: string;
  name: string;
}

function SunIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" />
    </svg>
  );
}

function MoonIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
    </svg>
  );
}

function MonitorIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
    </svg>
  );
}

export function Settings() {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [serverInfo, setServerInfo] = useState<ServerInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const { theme, setTheme } = useThemeStore();

  // Portal settings state
  const [activeTab, setActiveTab] = useState<'general' | 'portal'>('general');
  const [portalSettings, setPortalSettings] = useState<PortalSettings>({
    azureAd: { clientId: '', clientSecret: '', redirectUri: '' },
    email: { enabled: false }
  });
  const [clientTenants, setClientTenants] = useState<ClientTenant[]>([]);
  const [clients, setClients] = useState<Client[]>([]);
  const [savingPortal, setSavingPortal] = useState(false);
  const [newTenant, setNewTenant] = useState({ clientId: '', tenantId: '', tenantName: '' });
  const [showAddTenant, setShowAddTenant] = useState(false);

  useEffect(() => {
    loadData();
  }, []);

  useEffect(() => {
    if (activeTab === 'portal') {
      loadPortalData();
    }
  }, [activeTab]);

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

  const loadPortalData = async () => {
    try {
      const [portalData, tenantsData, clientsData] = await Promise.all([
        window.api.portal.getSettings().catch(() => null),
        window.api.portal.getClientTenants().catch(() => []),
        window.api.clients.list().catch(() => []),
      ]);

      if (portalData) {
        setPortalSettings({
          azureAd: portalData.azureAd || { clientId: '', clientSecret: '', redirectUri: '' },
          email: portalData.email || { enabled: false }
        });
      }
      setClientTenants(tenantsData || []);
      setClients(clientsData || []);
    } catch (error) {
      console.error('Failed to load portal settings:', error);
    }
  };

  const handleSavePortalSettings = async () => {
    setSavingPortal(true);
    try {
      await window.api.portal.updateSettings(portalSettings);
      alert('Portal settings saved successfully');
    } catch (error: any) {
      alert(`Error saving portal settings: ${error.message}`);
    } finally {
      setSavingPortal(false);
    }
  };

  const handleAddTenant = async () => {
    if (!newTenant.tenantId) {
      alert('Please enter the Azure AD tenant ID');
      return;
    }

    // Validate GUID format
    const guidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
    if (!guidRegex.test(newTenant.tenantId)) {
      alert('Invalid Tenant ID format. Must be a valid GUID (e.g., xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx)');
      return;
    }

    try {
      await window.api.portal.createClientTenant(newTenant);
      setNewTenant({ clientId: '', tenantId: '', tenantName: '' });
      setShowAddTenant(false);
      loadPortalData();
    } catch (error: any) {
      alert(`Error adding tenant mapping: ${error.message}`);
    }
  };

  const handleDeleteTenant = async (id: string) => {
    if (!confirm('Are you sure you want to remove this tenant mapping?')) return;

    try {
      await window.api.portal.deleteClientTenant(id);
      loadPortalData();
    } catch (error: any) {
      alert(`Error deleting tenant mapping: ${error.message}`);
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

      {/* Tab Navigation */}
      <div className="flex gap-1 border-b border-border">
        <button
          onClick={() => setActiveTab('general')}
          className={`px-4 py-2 font-medium transition-colors ${
            activeTab === 'general'
              ? 'text-primary border-b-2 border-primary'
              : 'text-text-secondary hover:text-text-primary'
          }`}
        >
          General
        </button>
        <button
          onClick={() => setActiveTab('portal')}
          className={`px-4 py-2 font-medium transition-colors ${
            activeTab === 'portal'
              ? 'text-primary border-b-2 border-primary'
              : 'text-text-secondary hover:text-text-primary'
          }`}
        >
          Support Portal
        </button>
      </div>

      {activeTab === 'general' && (
        <>
          {/* Appearance Settings */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-text-primary mb-4">Appearance</h2>
            <div>
              <label className="label">Theme</label>
              <div className="flex gap-2">
                <button
                  onClick={() => setTheme('light')}
                  className={`flex items-center gap-2 px-4 py-2 rounded-lg border transition-colors ${
                    theme === 'light'
                      ? 'border-primary bg-primary-light text-primary'
                      : 'border-border text-text-secondary hover:bg-gray-50 dark:hover:bg-slate-700'
                  }`}
                >
                  <SunIcon className="w-5 h-5" />
                  Light
                </button>
                <button
                  onClick={() => setTheme('dark')}
                  className={`flex items-center gap-2 px-4 py-2 rounded-lg border transition-colors ${
                    theme === 'dark'
                      ? 'border-primary bg-primary-light text-primary'
                      : 'border-border text-text-secondary hover:bg-gray-50 dark:hover:bg-slate-700'
                  }`}
                >
                  <MoonIcon className="w-5 h-5" />
                  Dark
                </button>
                <button
                  onClick={() => setTheme('system')}
                  className={`flex items-center gap-2 px-4 py-2 rounded-lg border transition-colors ${
                    theme === 'system'
                      ? 'border-primary bg-primary-light text-primary'
                      : 'border-border text-text-secondary hover:bg-gray-50 dark:hover:bg-slate-700'
                  }`}
                >
                  <MonitorIcon className="w-5 h-5" />
                  System
                </button>
              </div>
            </div>
          </div>

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
              <div className="p-4 bg-gray-50 dark:bg-slate-800 rounded-lg">
                <p className="text-sm text-text-secondary">Status</p>
                <p className="text-lg font-semibold text-success">Running</p>
              </div>
              <div className="p-4 bg-gray-50 dark:bg-slate-800 rounded-lg">
                <p className="text-sm text-text-secondary">Port</p>
                <p className="text-lg font-semibold text-text-primary">{serverInfo.port}</p>
              </div>
              <div className="p-4 bg-gray-50 dark:bg-slate-800 rounded-lg">
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
        </>
      )}

      {activeTab === 'portal' && (
        <>
          {/* Azure AD Configuration */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-text-primary mb-4">Microsoft Azure AD (Entra ID)</h2>
            <p className="text-sm text-text-secondary mb-4">
              Configure Azure AD for M365 single sign-on. Users can sign in with their Microsoft work accounts.
            </p>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
              <div>
                <label className="label">Application (Client) ID</label>
                <input
                  type="text"
                  value={portalSettings.azureAd.clientId}
                  onChange={e => setPortalSettings({
                    ...portalSettings,
                    azureAd: { ...portalSettings.azureAd, clientId: e.target.value }
                  })}
                  className="input"
                  placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
                />
              </div>
              <div>
                <label className="label">Client Secret</label>
                <input
                  type="password"
                  value={portalSettings.azureAd.clientSecret}
                  onChange={e => setPortalSettings({
                    ...portalSettings,
                    azureAd: { ...portalSettings.azureAd, clientSecret: e.target.value }
                  })}
                  className="input"
                  placeholder="Enter client secret"
                />
              </div>
              <div className="md:col-span-2">
                <label className="label">Redirect URI</label>
                <input
                  type="url"
                  value={portalSettings.azureAd.redirectUri}
                  onChange={e => setPortalSettings({
                    ...portalSettings,
                    azureAd: { ...portalSettings.azureAd, redirectUri: e.target.value }
                  })}
                  className="input"
                  placeholder="https://your-domain.com/portal/auth/callback"
                />
                <p className="text-xs text-text-secondary mt-1">
                  This must match the redirect URI configured in Azure AD
                </p>
              </div>
            </div>
          </div>

          {/* Email Notification Settings */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-text-primary mb-4">Email Notifications</h2>
            <div className="space-y-4">
              <div className="flex items-center gap-2">
                <input
                  type="checkbox"
                  id="emailEnabled"
                  checked={portalSettings.email.enabled}
                  onChange={e => setPortalSettings({
                    ...portalSettings,
                    email: { ...portalSettings.email, enabled: e.target.checked }
                  })}
                  className="w-4 h-4"
                />
                <label htmlFor="emailEnabled" className="text-sm text-text-primary">
                  Enable email notifications for ticket events
                </label>
              </div>

              {portalSettings.email.enabled && (
                <>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-6 pt-4 border-t border-border">
                    <div>
                      <label className="label">SMTP Host</label>
                      <input
                        type="text"
                        value={portalSettings.email.smtp?.host || ''}
                        onChange={e => setPortalSettings({
                          ...portalSettings,
                          email: {
                            ...portalSettings.email,
                            smtp: { ...portalSettings.email.smtp!, host: e.target.value }
                          }
                        })}
                        className="input"
                        placeholder="smtp.office365.com"
                      />
                    </div>
                    <div>
                      <label className="label">SMTP Port</label>
                      <input
                        type="number"
                        value={portalSettings.email.smtp?.port || 587}
                        onChange={e => setPortalSettings({
                          ...portalSettings,
                          email: {
                            ...portalSettings.email,
                            smtp: { ...portalSettings.email.smtp!, port: Number(e.target.value) }
                          }
                        })}
                        className="input"
                      />
                    </div>
                    <div>
                      <label className="label">SMTP Username</label>
                      <input
                        type="text"
                        value={portalSettings.email.smtp?.user || ''}
                        onChange={e => setPortalSettings({
                          ...portalSettings,
                          email: {
                            ...portalSettings.email,
                            smtp: { ...portalSettings.email.smtp!, user: e.target.value }
                          }
                        })}
                        className="input"
                        placeholder="notifications@yourdomain.com"
                      />
                    </div>
                    <div>
                      <label className="label">SMTP Password</label>
                      <input
                        type="password"
                        value={portalSettings.email.smtp?.password || ''}
                        onChange={e => setPortalSettings({
                          ...portalSettings,
                          email: {
                            ...portalSettings.email,
                            smtp: { ...portalSettings.email.smtp!, password: e.target.value }
                          }
                        })}
                        className="input"
                        placeholder="Enter password"
                      />
                    </div>
                    <div>
                      <label className="label">From Email Address</label>
                      <input
                        type="email"
                        value={portalSettings.email.smtp?.fromAddress || ''}
                        onChange={e => setPortalSettings({
                          ...portalSettings,
                          email: {
                            ...portalSettings.email,
                            smtp: { ...portalSettings.email.smtp!, fromAddress: e.target.value }
                          }
                        })}
                        className="input"
                        placeholder="support@yourdomain.com"
                      />
                    </div>
                    <div>
                      <label className="label">From Name (optional)</label>
                      <input
                        type="text"
                        value={portalSettings.email.smtp?.fromName || ''}
                        onChange={e => setPortalSettings({
                          ...portalSettings,
                          email: {
                            ...portalSettings.email,
                            smtp: { ...portalSettings.email.smtp!, fromName: e.target.value }
                          }
                        })}
                        className="input"
                        placeholder="IT Support"
                      />
                    </div>
                    <div className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        id="smtpSecure"
                        checked={portalSettings.email.smtp?.secure || false}
                        onChange={e => setPortalSettings({
                          ...portalSettings,
                          email: {
                            ...portalSettings.email,
                            smtp: { ...portalSettings.email.smtp!, secure: e.target.checked }
                          }
                        })}
                        className="w-4 h-4"
                      />
                      <label htmlFor="smtpSecure" className="text-sm text-text-primary">
                        Use SSL/TLS (port 465)
                      </label>
                    </div>
                  </div>
                  <div className="pt-4 border-t border-border">
                    <label className="label">Portal URL</label>
                    <input
                      type="url"
                      value={portalSettings.email.portalUrl || ''}
                      onChange={e => setPortalSettings({
                        ...portalSettings,
                        email: { ...portalSettings.email, portalUrl: e.target.value }
                      })}
                      className="input"
                      placeholder="https://your-domain.com"
                    />
                    <p className="text-xs text-text-secondary mt-1">
                      Base URL used for ticket links in email notifications
                    </p>
                  </div>
                </>
              )}
            </div>
          </div>

          {/* Client Tenant Mapping */}
          <div className="card p-6">
            <div className="flex justify-between items-center mb-4">
              <div>
                <h2 className="text-lg font-semibold text-text-primary">Client Tenant Mapping</h2>
                <p className="text-sm text-text-secondary">
                  Add Azure AD tenant IDs to allow users from those organizations to access the portal.
                  A Sentinel client will be auto-created for each tenant.
                </p>
              </div>
              <button
                onClick={() => setShowAddTenant(true)}
                className="btn btn-primary"
              >
                Add Tenant
              </button>
            </div>

            {showAddTenant && (
              <div className="p-4 bg-gray-50 dark:bg-slate-800 rounded-lg mb-4">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <div>
                    <label className="label">Azure AD Tenant ID *</label>
                    <input
                      type="text"
                      value={newTenant.tenantId}
                      onChange={e => setNewTenant({ ...newTenant, tenantId: e.target.value })}
                      className="input"
                      placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
                    />
                    <p className="text-xs text-text-secondary mt-1">
                      Found in Azure Portal → Microsoft Entra ID → Overview → Tenant ID
                    </p>
                  </div>
                  <div>
                    <label className="label">Organization Name *</label>
                    <input
                      type="text"
                      value={newTenant.tenantName}
                      onChange={e => setNewTenant({ ...newTenant, tenantName: e.target.value })}
                      className="input"
                      placeholder="Contoso Corp"
                    />
                    <p className="text-xs text-text-secondary mt-1">
                      This will be used as the client name in Sentinel
                    </p>
                  </div>
                </div>
                {clients.length > 0 && (
                  <div className="mt-4">
                    <label className="label">Link to Existing Client (optional)</label>
                    <select
                      value={newTenant.clientId}
                      onChange={e => setNewTenant({ ...newTenant, clientId: e.target.value })}
                      className="input"
                    >
                      <option value="">Auto-create new client</option>
                      {clients.map(client => (
                        <option key={client.id} value={client.id}>{client.name}</option>
                      ))}
                    </select>
                  </div>
                )}
                <div className="flex gap-2 mt-4">
                  <button onClick={handleAddTenant} className="btn btn-primary">
                    Add Tenant
                  </button>
                  <button
                    onClick={() => {
                      setShowAddTenant(false);
                      setNewTenant({ clientId: '', tenantId: '', tenantName: '' });
                    }}
                    className="btn btn-secondary"
                  >
                    Cancel
                  </button>
                </div>
              </div>
            )}

            <div className="overflow-x-auto">
              <table className="w-full">
                <thead>
                  <tr className="border-b border-border">
                    <th className="text-left py-2 px-4 text-sm font-medium text-text-secondary">Client</th>
                    <th className="text-left py-2 px-4 text-sm font-medium text-text-secondary">Tenant ID</th>
                    <th className="text-left py-2 px-4 text-sm font-medium text-text-secondary">Tenant Name</th>
                    <th className="text-right py-2 px-4 text-sm font-medium text-text-secondary">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {clientTenants.length === 0 ? (
                    <tr>
                      <td colSpan={4} className="py-8 text-center text-text-secondary">
                        No tenant mappings configured
                      </td>
                    </tr>
                  ) : (
                    clientTenants.map(tenant => (
                      <tr key={tenant.id} className="border-b border-border">
                        <td className="py-2 px-4 text-text-primary">{tenant.clientName || 'Unknown'}</td>
                        <td className="py-2 px-4 text-text-secondary font-mono text-sm">{tenant.tenantId}</td>
                        <td className="py-2 px-4 text-text-secondary">{tenant.tenantName || '-'}</td>
                        <td className="py-2 px-4 text-right">
                          <button
                            onClick={() => handleDeleteTenant(tenant.id)}
                            className="text-danger hover:text-red-700"
                          >
                            Remove
                          </button>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>

          <div className="flex justify-end">
            <button onClick={handleSavePortalSettings} disabled={savingPortal} className="btn btn-primary">
              {savingPortal ? 'Saving...' : 'Save Portal Settings'}
            </button>
          </div>
        </>
      )}
    </div>
  );
}
