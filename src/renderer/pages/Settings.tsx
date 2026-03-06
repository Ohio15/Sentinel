import React, { useState, useEffect } from 'react';
import { useThemeStore } from '../stores/themeStore';
import { useDeviceStore } from '../stores/deviceStore';
import { PasskeyManager } from '../components/PasskeyManager';
import { CredentialManager } from '../components/CredentialManager';
import { settings as settingsService, server as serverService, portal as portalService, backend as backendService, clients as clientsService } from '../services';

interface Settings {
  serverPort: number;
  agentCheckInterval: number;
  metricsRetentionDays: number;
  alertEmailEnabled: boolean;
  alertEmail?: string;
}

interface ServerInfo {
  port: number;
  version?: string;
  environment?: string;
  agentCount?: number;
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

type MainTab = 'general' | 'security' | 'monitoring' | 'integrations' | 'portal';
type PortalSubTab = 'azure' | 'email' | 'tenants';

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

function TabIcon({ name }: { name: MainTab }) {
  const icons: Record<MainTab, JSX.Element> = {
    general: (
      <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
      </svg>
    ),
    security: (
      <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
      </svg>
    ),
    monitoring: (
      <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" />
      </svg>
    ),
    integrations: (
      <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
      </svg>
    ),
    portal: (
      <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9" />
      </svg>
    ),
  };
  return icons[name];
}

export function Settings() {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [serverInfo, setServerInfo] = useState<ServerInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const { theme, setTheme } = useThemeStore();
  const { devices } = useDeviceStore();
  const onlineCount = devices.filter(d => d.status === 'online').length;

  // External backend state
  const [backendUrl, setBackendUrl] = useState('');
  const [backendApiKey, setBackendApiKey] = useState('');
  const [backendAuthMode, setBackendAuthMode] = useState<'apikey' | 'credentials'>('apikey');
  const [backendEmail, setBackendEmail] = useState('');
  const [backendPassword, setBackendPassword] = useState('');
  const [backendConnecting, setBackendConnecting] = useState(false);
  const [backendConnected, setBackendConnected] = useState(false);
  const [backendError, setBackendError] = useState('');

  // Tab state
  const [activeTab, setActiveTab] = useState<MainTab>('general');
  const [portalSubTab, setPortalSubTab] = useState<PortalSubTab>('azure');

  // Portal settings state
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
      const [settingsData, infoData, backendConfig] = await Promise.all([
        settingsService.get(),
        serverService.getInfo(),
        backendService.getConfig().catch(() => ({ url: '', isConfigured: false, isAuthenticated: false })),
      ]);
      setSettings(settingsData as Settings);
      setServerInfo(infoData as ServerInfo);
      if (backendConfig.url) {
        setBackendUrl(backendConfig.url);
        setBackendConnected(backendConfig.isAuthenticated || false);
      }
    } catch (error) {
      console.error('Failed to load settings:', error);
    } finally {
      setLoading(false);
    }
  };

  const loadPortalData = async () => {
    try {
      const [portalData, tenantsData, clientsData] = await Promise.all([
        portalService.getSettings().catch(() => null),
        portalService.getClientTenants().catch(() => []),
        clientsService.list().catch(() => []),
      ]);

      if (portalData) {
        const data = portalData as { azureAd?: { clientId: string; clientSecret: string; redirectUri: string }; email?: { enabled: boolean } };
        setPortalSettings({
          azureAd: data.azureAd || { clientId: '', clientSecret: '', redirectUri: '' },
          email: data.email || { enabled: false }
        });
      }
      setClientTenants((tenantsData || []) as ClientTenant[]);
      setClients(clientsData || []);
    } catch (error) {
      console.error('Failed to load portal settings:', error);
    }
  };

  const handleSavePortalSettings = async () => {
    setSavingPortal(true);
    try {
      await portalService.updateSettings(portalSettings);
      alert('Portal settings saved successfully');
    } catch (error: unknown) {
      alert(`Error saving portal settings: ${error instanceof Error ? error.message : 'Unknown error'}`);
    } finally {
      setSavingPortal(false);
    }
  };

  const handleAddTenant = async () => {
    if (!newTenant.tenantId) {
      alert('Please enter the Azure AD tenant ID');
      return;
    }

    const guidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
    if (!guidRegex.test(newTenant.tenantId)) {
      alert('Invalid Tenant ID format. Must be a valid GUID (e.g., xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx)');
      return;
    }

    try {
      await portalService.createClientTenant(newTenant);
      setNewTenant({ clientId: '', tenantId: '', tenantName: '' });
      setShowAddTenant(false);
      loadPortalData();
    } catch (error: unknown) {
      alert(`Error adding tenant mapping: ${error instanceof Error ? error.message : 'Unknown error'}`);
    }
  };

  const handleDeleteTenant = async (id: string) => {
    if (!confirm('Are you sure you want to remove this tenant mapping?')) return;

    try {
      await portalService.deleteClientTenant(id);
      loadPortalData();
    } catch (error: unknown) {
      alert(`Error deleting tenant mapping: ${error instanceof Error ? error.message : 'Unknown error'}`);
    }
  };

  const handleConnectBackend = async () => {
    if (!backendUrl) {
      setBackendError('Please enter a backend URL');
      return;
    }

    if (backendAuthMode === 'apikey') {
      if (!backendApiKey) {
        setBackendError('Please enter an API key');
        return;
      }
    } else {
      if (!backendEmail || !backendPassword) {
        setBackendError('Please enter credentials');
        return;
      }
    }

    setBackendConnecting(true);
    setBackendError('');

    try {
      await backendService.setUrl(backendUrl);

      if (backendAuthMode === 'apikey') {
        await backendService.setApiKey(backendApiKey);
        const result = await backendService.testConnection();
        if (result.success) {
          setBackendConnected(true);
          alert('Successfully connected with API key');
        } else {
          setBackendError('Connection failed - invalid API key');
          setBackendConnected(false);
        }
      } else {
        const result = await backendService.authenticate();
        if (result.success) {
          setBackendConnected(true);
          setBackendPassword('');
          alert('Successfully connected to external backend');
        } else {
          setBackendError('Authentication failed');
          setBackendConnected(false);
        }
      }
    } catch (error) {
      setBackendError(error instanceof Error ? error.message : 'Unknown error');
      setBackendConnected(false);
    } finally {
      setBackendConnecting(false);
    }
  };

  const handleSave = async () => {
    if (!settings) return;
    setSaving(true);
    try {
      await settingsService.update(settings);
      alert('Settings saved successfully');
    } catch (error: unknown) {
      alert(`Error saving settings: ${error instanceof Error ? error.message : 'Unknown error'}`);
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

  const tabs: { id: MainTab; label: string }[] = [
    { id: 'general', label: 'General' },
    { id: 'security', label: 'Security' },
    { id: 'monitoring', label: 'Monitoring' },
    { id: 'integrations', label: 'Integrations' },
    { id: 'portal', label: 'Support Portal' },
  ];

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-text-primary">Settings</h1>

      {/* Main Tab Navigation */}
      <div className="flex gap-1 border-b border-border overflow-x-auto">
        {tabs.map(tab => (
          <button
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            className={`flex items-center gap-2 px-4 py-2 font-medium transition-colors whitespace-nowrap ${
              activeTab === tab.id
                ? 'text-primary border-b-2 border-primary'
                : 'text-text-secondary hover:text-text-primary'
            }`}
          >
            <TabIcon name={tab.id} />
            {tab.label}
          </button>
        ))}
      </div>

      {/* General Tab */}
      {activeTab === 'general' && (
        <div className="space-y-6">
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
                <p className="text-lg font-semibold text-text-primary">{onlineCount}</p>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Security Tab */}
      {activeTab === 'security' && (
        <div className="space-y-6">
          <CredentialManager />
          <PasskeyManager />
        </div>
      )}

      {/* Monitoring Tab */}
      {activeTab === 'monitoring' && (
        <div className="space-y-6">
          {/* Server Settings */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-text-primary mb-4">Server Configuration</h2>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
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

          <div className="flex justify-end">
            <button onClick={handleSave} disabled={saving} className="btn btn-primary">
              {saving ? 'Saving...' : 'Save Settings'}
            </button>
          </div>
        </div>
      )}

      {/* Integrations Tab */}
      {activeTab === 'integrations' && (
        <div className="space-y-6">
          {/* External Backend */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-text-primary mb-4">External Backend</h2>
            <p className="text-sm text-text-secondary mb-4">
              Connect to a Docker or standalone Sentinel backend to manage agents connected to that server.
              This enables commands, ping, and other operations for remotely-connected agents.
            </p>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
              <div className="md:col-span-2">
                <label className="label">Backend URL</label>
                <input
                  type="url"
                  value={backendUrl}
                  onChange={e => setBackendUrl(e.target.value)}
                  className="input"
                  placeholder="http://192.168.1.2:8090"
                />
                <p className="text-xs text-text-secondary mt-1">
                  The URL of your Docker or standalone Sentinel server
                </p>
              </div>

              {/* Auth Mode Toggle */}
              <div className="md:col-span-2">
                <label className="label">Authentication Method</label>
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={() => setBackendAuthMode('apikey')}
                    className={`px-4 py-2 rounded-lg border transition-colors ${
                      backendAuthMode === 'apikey'
                        ? 'bg-primary text-white border-primary'
                        : 'bg-surface border-border text-text-secondary hover:border-primary'
                    }`}
                  >
                    API Key (Recommended)
                  </button>
                  <button
                    type="button"
                    onClick={() => setBackendAuthMode('credentials')}
                    className={`px-4 py-2 rounded-lg border transition-colors ${
                      backendAuthMode === 'credentials'
                        ? 'bg-primary text-white border-primary'
                        : 'bg-surface border-border text-text-secondary hover:border-primary'
                    }`}
                  >
                    Email/Password
                  </button>
                </div>
              </div>

              {backendAuthMode === 'apikey' ? (
                <div className="md:col-span-2">
                  <label className="label">API Key</label>
                  <input
                    type="password"
                    value={backendApiKey}
                    onChange={e => setBackendApiKey(e.target.value)}
                    className="input font-mono"
                    placeholder="Enter your API key"
                  />
                  <p className="text-xs text-text-secondary mt-1">
                    Generate an API key from your backend server's .env file (API_KEY setting)
                  </p>
                </div>
              ) : (
                <>
                  <div>
                    <label className="label">Email</label>
                    <input
                      type="email"
                      value={backendEmail}
                      onChange={e => setBackendEmail(e.target.value)}
                      className="input"
                      placeholder="admin@sentinel.local"
                    />
                  </div>
                  <div>
                    <label className="label">Password</label>
                    <input
                      type="password"
                      value={backendPassword}
                      onChange={e => setBackendPassword(e.target.value)}
                      className="input"
                      placeholder="Enter password"
                    />
                  </div>
                </>
              )}
            </div>
            {backendError && (
              <div className="mt-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
                <p className="text-sm text-danger">{backendError}</p>
              </div>
            )}
            <div className="flex items-center gap-4 mt-4">
              <button
                onClick={handleConnectBackend}
                disabled={backendConnecting}
                className="btn btn-primary"
              >
                {backendConnecting ? 'Connecting...' : 'Connect'}
              </button>
              {backendConnected && (
                <div className="flex items-center gap-2 text-success">
                  <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                  </svg>
                  <span className="text-sm font-medium">Connected</span>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Support Portal Tab */}
      {activeTab === 'portal' && (
        <div className="space-y-6">
          {/* Portal Sub-Tabs */}
          <div className="flex gap-2">
            <button
              onClick={() => setPortalSubTab('azure')}
              className={`px-4 py-2 text-sm font-medium rounded-lg transition-colors ${
                portalSubTab === 'azure'
                  ? 'bg-primary text-white'
                  : 'bg-gray-100 dark:bg-gray-800 text-text-secondary hover:text-text-primary'
              }`}
            >
              Azure AD / SSO
            </button>
            <button
              onClick={() => setPortalSubTab('email')}
              className={`px-4 py-2 text-sm font-medium rounded-lg transition-colors ${
                portalSubTab === 'email'
                  ? 'bg-primary text-white'
                  : 'bg-gray-100 dark:bg-gray-800 text-text-secondary hover:text-text-primary'
              }`}
            >
              Email Configuration
            </button>
            <button
              onClick={() => setPortalSubTab('tenants')}
              className={`px-4 py-2 text-sm font-medium rounded-lg transition-colors ${
                portalSubTab === 'tenants'
                  ? 'bg-primary text-white'
                  : 'bg-gray-100 dark:bg-gray-800 text-text-secondary hover:text-text-primary'
              }`}
            >
              Tenant Mapping
            </button>
          </div>

          {/* Azure AD Configuration */}
          {portalSubTab === 'azure' && (
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
              <div className="flex justify-end mt-6">
                <button onClick={handleSavePortalSettings} disabled={savingPortal} className="btn btn-primary">
                  {savingPortal ? 'Saving...' : 'Save Azure AD Settings'}
                </button>
              </div>
            </div>
          )}

          {/* Email Notification Settings */}
          {portalSubTab === 'email' && (
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
              <div className="flex justify-end mt-6">
                <button onClick={handleSavePortalSettings} disabled={savingPortal} className="btn btn-primary">
                  {savingPortal ? 'Saving...' : 'Save Email Settings'}
                </button>
              </div>
            </div>
          )}

          {/* Client Tenant Mapping */}
          {portalSubTab === 'tenants' && (
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
          )}
        </div>
      )}
    </div>
  );
}

export default Settings;
