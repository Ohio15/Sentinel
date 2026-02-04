import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Settings as SettingsIcon,
  Shield,
  Bell,
  Globe,
  Save,
  Copy,
  RefreshCw,
  Plus,
  Trash2,
  Building2,
} from 'lucide-react';
import { Header } from '@/components/layout';
import { Card, CardContent, Button, Input } from '@/components/ui';
import { PasskeyManager } from '@/components/PasskeyManager';
import api from '@/services/api';
import toast from 'react-hot-toast';

interface SettingsData {
  organizationName: string;
  enrollmentToken: string;
  alertEmailEnabled: boolean;
  alertEmailRecipients: string;
  retentionDays: number;
  agentHeartbeatInterval: number;
  agentMetricsInterval: number;
}

interface ClientTenant {
  id: string;
  clientId?: string;
  clientName?: string;
  tenantId: string;
  tenantName?: string;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}

interface Client {
  id: string;
  name: string;
}

export function Settings() {
  const queryClient = useQueryClient();
  const [settings, setSettings] = useState<SettingsData | null>(null);
  const [activeTab, setActiveTab] = useState<'general' | 'portal'>('general');

  // Portal state
  const [showAddTenant, setShowAddTenant] = useState(false);
  const [newTenant, setNewTenant] = useState({ clientId: '', tenantId: '', tenantName: '' });

  const { isLoading } = useQuery({
    queryKey: ['settings'],
    queryFn: async () => {
      const data = await api.getSettings();
      // Transform backend key-value format to frontend structure
      const transformed: SettingsData = {
        organizationName: data.organization_name || 'Sentinel',
        enrollmentToken: data.enrollment_token || 'sentinel-enrollment-token',
        alertEmailEnabled: data.alert_email_enabled === 'true',
        alertEmailRecipients: data.alert_email_recipients || '',
        retentionDays: parseInt(data.metrics_retention_days) || 30,
        agentHeartbeatInterval: parseInt(data.agent_heartbeat_interval) || 30,
        agentMetricsInterval: parseInt(data.agent_metrics_interval) || 60,
      };
      setSettings(transformed);
      return transformed;
    },
  });

  // Portal queries
  const { data: clientTenants = [], isLoading: tenantsLoading } = useQuery<ClientTenant[]>({
    queryKey: ['clientTenants'],
    queryFn: () => api.getClientTenants(),
    enabled: activeTab === 'portal',
  });

  const { data: clients = [] } = useQuery<Client[]>({
    queryKey: ['clients'],
    queryFn: () => api.getClients(),
    enabled: activeTab === 'portal',
  });

  const updateMutation = useMutation({
    mutationFn: (data: Partial<SettingsData>) => api.updateSettings(data),
    onSuccess: () => {
      toast.success('Settings saved successfully');
      queryClient.invalidateQueries({ queryKey: ['settings'] });
    },
    onError: () => {
      toast.error('Failed to save settings');
    },
  });

  const createTenantMutation = useMutation({
    mutationFn: (data: { clientId?: string; tenantId: string; tenantName?: string }) =>
      api.createClientTenant(data),
    onSuccess: () => {
      toast.success('Tenant mapping created');
      queryClient.invalidateQueries({ queryKey: ['clientTenants'] });
      setShowAddTenant(false);
      setNewTenant({ clientId: '', tenantId: '', tenantName: '' });
    },
    onError: (err: { response?: { data?: { error?: string } } }) => {
      toast.error(err.response?.data?.error || 'Failed to create tenant mapping');
    },
  });

  const deleteTenantMutation = useMutation({
    mutationFn: (id: string) => api.deleteClientTenant(id),
    onSuccess: () => {
      toast.success('Tenant mapping deleted');
      queryClient.invalidateQueries({ queryKey: ['clientTenants'] });
    },
    onError: () => {
      toast.error('Failed to delete tenant mapping');
    },
  });

  const handleSave = () => {
    if (settings) {
      updateMutation.mutate(settings);
    }
  };

  const copyEnrollmentToken = () => {
    if (settings?.enrollmentToken) {
      navigator.clipboard.writeText(settings.enrollmentToken);
      toast.success('Enrollment token copied to clipboard');
    }
  };

  const handleAddTenant = () => {
    if (!newTenant.tenantId) {
      toast.error('Please enter the Azure AD tenant ID');
      return;
    }

    // Validate GUID format
    const guidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
    if (!guidRegex.test(newTenant.tenantId)) {
      toast.error('Invalid Tenant ID format. Must be a valid GUID.');
      return;
    }

    createTenantMutation.mutate({
      clientId: newTenant.clientId || undefined,
      tenantId: newTenant.tenantId,
      tenantName: newTenant.tenantName || undefined,
    });
  };

  const handleDeleteTenant = (id: string) => {
    if (window.confirm('Are you sure you want to remove this tenant mapping?')) {
      deleteTenantMutation.mutate(id);
    }
  };

  // Get the server URL from environment or use current origin
  const serverUrl = import.meta.env.VITE_API_URL?.replace('/api', '') || window.location.origin;
  const wsUrl = import.meta.env.VITE_WS_URL || serverUrl.replace('http', 'ws') + '/ws';

  // Generate installation scripts
  const windowsInstallScript = `# Sentinel Agent Installation Script for Windows
# Run this in PowerShell as Administrator

$SERVER_URL = "${serverUrl}"
$WS_URL = "${wsUrl}"
$TOKEN = "${settings?.enrollmentToken || 'YOUR_TOKEN_HERE'}"
$INSTALL_DIR = "C:\\Program Files\\Sentinel"
$AGENT_NAME = "sentinel-agent.exe"

# Create installation directory
New-Item -ItemType Directory -Force -Path $INSTALL_DIR | Out-Null

# Download the agent (update URL when agent is hosted)
Write-Host "Downloading Sentinel agent..." -ForegroundColor Cyan
# Invoke-WebRequest -Uri "$SERVER_URL/downloads/agent/windows/amd64/$AGENT_NAME" -OutFile "$INSTALL_DIR\\$AGENT_NAME"

# For now, create a placeholder config
$config = @"
server_url: $SERVER_URL
websocket_url: $WS_URL
enrollment_token: $TOKEN
agent_id: $env:COMPUTERNAME-$(Get-Random)
"@
$config | Out-File -FilePath "$INSTALL_DIR\\config.yaml" -Encoding UTF8

Write-Host "Configuration saved to $INSTALL_DIR\\config.yaml" -ForegroundColor Green
Write-Host ""
Write-Host "Next steps:" -ForegroundColor Yellow
Write-Host "1. Build the agent: cd agent && cargo build --release"
Write-Host "2. Copy sentinel-agent.exe to $INSTALL_DIR"
Write-Host "3. Run: & '$INSTALL_DIR\\$AGENT_NAME' install"
Write-Host "4. Start service: Start-Service SentinelAgent"`;

  const linuxInstallScript = `#!/bin/bash
# Sentinel Agent Installation Script for Linux
# Run with: sudo bash install.sh

set -e

SERVER_URL="${serverUrl}"
WS_URL="${wsUrl}"
TOKEN="${settings?.enrollmentToken || 'YOUR_TOKEN_HERE'}"
INSTALL_DIR="/opt/sentinel"
AGENT_NAME="sentinel-agent"

echo "Installing Sentinel Agent..."

# Create installation directory
mkdir -p $INSTALL_DIR
mkdir -p /etc/sentinel

# Create configuration file
cat > /etc/sentinel/config.yaml << EOF
server_url: $SERVER_URL
websocket_url: $WS_URL
enrollment_token: $TOKEN
agent_id: $(hostname)-$(cat /proc/sys/kernel/random/uuid | cut -d'-' -f1)
EOF

# Create systemd service
cat > /etc/systemd/system/sentinel-agent.service << EOF
[Unit]
Description=Sentinel Monitoring Agent
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/$AGENT_NAME
Restart=always
RestartSec=10
User=root
WorkingDirectory=$INSTALL_DIR

[Install]
WantedBy=multi-user.target
EOF

echo "Configuration saved to /etc/sentinel/config.yaml"
echo ""
echo "Next steps:"
echo "1. Build the agent: cd agent && cargo build --release --target x86_64-unknown-linux-gnu"
echo "2. Copy the binary: sudo cp target/release/sentinel-agent $INSTALL_DIR/"
echo "3. Enable service: sudo systemctl enable sentinel-agent"
echo "4. Start service: sudo systemctl start sentinel-agent"`;

  const macosInstallScript = `#!/bin/bash
# Sentinel Agent Installation Script for macOS
# Run with: sudo bash install.sh

set -e

SERVER_URL="${serverUrl}"
WS_URL="${wsUrl}"
TOKEN="${settings?.enrollmentToken || 'YOUR_TOKEN_HERE'}"
INSTALL_DIR="/usr/local/sentinel"
AGENT_NAME="sentinel-agent"

echo "Installing Sentinel Agent..."

# Create installation directory
mkdir -p $INSTALL_DIR
mkdir -p /etc/sentinel

# Create configuration file
cat > /etc/sentinel/config.yaml << EOF
server_url: $SERVER_URL
websocket_url: $WS_URL
enrollment_token: $TOKEN
agent_id: $(hostname)-$(uuidgen | cut -d'-' -f1)
EOF

# Create launchd plist
cat > /Library/LaunchDaemons/com.sentinel.agent.plist << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.sentinel.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>$INSTALL_DIR/$AGENT_NAME</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>WorkingDirectory</key>
    <string>$INSTALL_DIR</string>
</dict>
</plist>
EOF

echo "Configuration saved to /etc/sentinel/config.yaml"
echo ""
echo "Next steps:"
echo "1. Build the agent: cd agent && cargo build --release --target x86_64-apple-darwin"
echo "2. Copy the binary: sudo cp target/release/sentinel-agent $INSTALL_DIR/"
echo "3. Load service: sudo launchctl load /Library/LaunchDaemons/com.sentinel.agent.plist"
echo "4. Check status: sudo launchctl list | grep sentinel"`;

  if (isLoading || !settings) {
    return (
      <div>
        <Header title="Settings" subtitle="Configure your Sentinel instance" />
        <div className="p-6">
          <div className="animate-pulse space-y-6">
            {[...Array(3)].map((_, i) => (
              <div key={i} className="h-48 bg-gray-100 dark:bg-gray-800 rounded-xl" />
            ))}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div>
      <Header title="Settings" subtitle="Configure your Sentinel instance" />

      {/* Tab Navigation */}
      <div className="px-6 pt-4">
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
      </div>

      <div className="p-6 space-y-6 max-w-4xl">
        {activeTab === 'general' && (
          <>
            {/* General Settings */}
            <Card>
              <CardContent>
                <div className="flex items-center gap-3 mb-6">
                  <div className="p-2 bg-blue-100 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400 rounded-lg">
                    <SettingsIcon className="w-5 h-5" />
                  </div>
                  <div>
                    <h3 className="text-lg font-semibold text-text-primary">
                      General Settings
                    </h3>
                    <p className="text-sm text-text-secondary">
                      Basic configuration for your organization
                    </p>
                  </div>
                </div>

                <div className="space-y-4">
                  <Input
                    label="Organization Name"
                    value={settings.organizationName}
                    onChange={(e) =>
                      setSettings({ ...settings, organizationName: e.target.value })
                    }
                    placeholder="My Organization"
                  />

                  <div>
                    <label className="block text-sm font-medium text-text-primary mb-1.5">
                      Data Retention (days)
                    </label>
                    <input
                      type="number"
                      value={settings.retentionDays}
                      onChange={(e) =>
                        setSettings({
                          ...settings,
                          retentionDays: parseInt(e.target.value) || 30,
                        })
                      }
                      min={7}
                      max={365}
                      className="w-32 px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary bg-surface text-text-primary"
                    />
                    <p className="text-xs text-text-secondary mt-1">
                      How long to keep metrics and logs
                    </p>
                  </div>
                </div>
              </CardContent>
            </Card>

            {/* Agent Settings */}
            <Card>
              <CardContent>
                <div className="flex items-center gap-3 mb-6">
                  <div className="p-2 bg-green-100 text-green-600 dark:bg-green-900/30 dark:text-green-400 rounded-lg">
                    <Shield className="w-5 h-5" />
                  </div>
                  <div>
                    <h3 className="text-lg font-semibold text-text-primary">
                      Agent Settings
                    </h3>
                    <p className="text-sm text-text-secondary">
                      Configure agent behavior and enrollment
                    </p>
                  </div>
                </div>

                <div className="space-y-4">
                  <div>
                    <label className="block text-sm font-medium text-text-primary mb-1.5">
                      Enrollment Token
                    </label>
                    <div className="flex gap-2">
                      <input
                        type="text"
                        value={settings.enrollmentToken}
                        readOnly
                        className="flex-1 px-3 py-2 border border-border rounded-lg text-sm bg-gray-50 dark:bg-gray-800 font-mono text-text-primary"
                      />
                      <Button variant="secondary" onClick={copyEnrollmentToken}>
                        <Copy className="w-4 h-4" />
                      </Button>
                      <Button
                        variant="secondary"
                        onClick={() =>
                          toast.error('Token regeneration requires admin approval')
                        }
                      >
                        <RefreshCw className="w-4 h-4" />
                      </Button>
                    </div>
                    <p className="text-xs text-text-secondary mt-1">
                      Use this token when installing agents on new devices
                    </p>
                  </div>

                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <label className="block text-sm font-medium text-text-primary mb-1.5">
                        Heartbeat Interval (seconds)
                      </label>
                      <input
                        type="number"
                        value={settings.agentHeartbeatInterval}
                        onChange={(e) =>
                          setSettings({
                            ...settings,
                            agentHeartbeatInterval: parseInt(e.target.value) || 30,
                          })
                        }
                        min={10}
                        max={300}
                        className="w-32 px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary bg-surface text-text-primary"
                      />
                    </div>

                    <div>
                      <label className="block text-sm font-medium text-text-primary mb-1.5">
                        Metrics Interval (seconds)
                      </label>
                      <input
                        type="number"
                        value={settings.agentMetricsInterval}
                        onChange={(e) =>
                          setSettings({
                            ...settings,
                            agentMetricsInterval: parseInt(e.target.value) || 60,
                          })
                        }
                        min={30}
                        max={600}
                        className="w-32 px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary bg-surface text-text-primary"
                      />
                    </div>
                  </div>
                </div>
              </CardContent>
            </Card>

            {/* Security / Passkeys */}
            <PasskeyManager />

            {/* Notification Settings */}
            <Card>
              <CardContent>
                <div className="flex items-center gap-3 mb-6">
                  <div className="p-2 bg-amber-100 text-amber-600 dark:bg-amber-900/30 dark:text-amber-400 rounded-lg">
                    <Bell className="w-5 h-5" />
                  </div>
                  <div>
                    <h3 className="text-lg font-semibold text-text-primary">
                      Notifications
                    </h3>
                    <p className="text-sm text-text-secondary">
                      Configure how you receive alerts
                    </p>
                  </div>
                </div>

                <div className="space-y-4">
                  <label className="flex items-center gap-3">
                    <input
                      type="checkbox"
                      checked={settings.alertEmailEnabled}
                      onChange={(e) =>
                        setSettings({
                          ...settings,
                          alertEmailEnabled: e.target.checked,
                        })
                      }
                      className="rounded border-border w-4 h-4"
                    />
                    <span className="text-sm text-text-primary">
                      Enable email notifications for alerts
                    </span>
                  </label>

                  {settings.alertEmailEnabled && (
                    <Input
                      label="Email Recipients"
                      value={settings.alertEmailRecipients}
                      onChange={(e) =>
                        setSettings({
                          ...settings,
                          alertEmailRecipients: e.target.value,
                        })
                      }
                      placeholder="admin@example.com, team@example.com"
                      helperText="Comma-separated list of email addresses"
                    />
                  )}
                </div>
              </CardContent>
            </Card>

            {/* Agent Installation Instructions */}
            <Card>
              <CardContent>
                <div className="flex items-center gap-3 mb-6">
                  <div className="p-2 bg-purple-100 text-purple-600 dark:bg-purple-900/30 dark:text-purple-400 rounded-lg">
                    <Globe className="w-5 h-5" />
                  </div>
                  <div>
                    <h3 className="text-lg font-semibold text-text-primary">
                      Agent Installation
                    </h3>
                    <p className="text-sm text-text-secondary">
                      Install the Sentinel agent on your endpoints
                    </p>
                  </div>
                </div>

                <div className="space-y-4">
                  <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-4 mb-4">
                    <p className="text-sm text-blue-800 dark:text-blue-200">
                      <strong>Server URL:</strong> {serverUrl}
                    </p>
                    <p className="text-xs text-blue-600 dark:text-blue-300 mt-1">
                      Make sure the target machine can reach this address. For local network installs, use your machine's IP address.
                    </p>
                  </div>

                  <div>
                    <div className="flex items-center justify-between mb-2">
                      <h4 className="text-sm font-medium text-text-primary">
                        Windows (PowerShell - Run as Administrator)
                      </h4>
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => {
                          navigator.clipboard.writeText(windowsInstallScript);
                          toast.success('Windows install script copied!');
                        }}
                      >
                        <Copy className="w-3.5 h-3.5 mr-1" />
                        Copy
                      </Button>
                    </div>
                    <div className="bg-gray-900 text-gray-100 p-4 rounded-lg font-mono text-xs overflow-x-auto whitespace-pre-wrap">
                      <code>{windowsInstallScript}</code>
                    </div>
                  </div>

                  <div>
                    <div className="flex items-center justify-between mb-2">
                      <h4 className="text-sm font-medium text-text-primary">
                        Linux (Bash - Run as root or with sudo)
                      </h4>
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => {
                          navigator.clipboard.writeText(linuxInstallScript);
                          toast.success('Linux install script copied!');
                        }}
                      >
                        <Copy className="w-3.5 h-3.5 mr-1" />
                        Copy
                      </Button>
                    </div>
                    <div className="bg-gray-900 text-gray-100 p-4 rounded-lg font-mono text-xs overflow-x-auto whitespace-pre-wrap">
                      <code>{linuxInstallScript}</code>
                    </div>
                  </div>

                  <div>
                    <div className="flex items-center justify-between mb-2">
                      <h4 className="text-sm font-medium text-text-primary">
                        macOS (Bash - Run in Terminal)
                      </h4>
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => {
                          navigator.clipboard.writeText(macosInstallScript);
                          toast.success('macOS install script copied!');
                        }}
                      >
                        <Copy className="w-3.5 h-3.5 mr-1" />
                        Copy
                      </Button>
                    </div>
                    <div className="bg-gray-900 text-gray-100 p-4 rounded-lg font-mono text-xs overflow-x-auto whitespace-pre-wrap">
                      <code>{macosInstallScript}</code>
                    </div>
                  </div>

                  <div className="bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg p-4 mt-4">
                    <p className="text-sm text-amber-800 dark:text-amber-200">
                      <strong>Note:</strong> The agent binary must be built and hosted before these scripts will work.
                      See the <code className="bg-amber-100 dark:bg-amber-800 px-1 rounded">agent/</code> directory for build instructions.
                    </p>
                  </div>
                </div>
              </CardContent>
            </Card>

            {/* Save Button */}
            <div className="flex justify-end">
              <Button
                onClick={handleSave}
                isLoading={updateMutation.isPending}
              >
                <Save className="w-4 h-4 mr-2" />
                Save Settings
              </Button>
            </div>
          </>
        )}

        {activeTab === 'portal' && (
          <>
            {/* Client Tenant Mapping */}
            <Card>
              <CardContent>
                <div className="flex items-center justify-between mb-6">
                  <div className="flex items-center gap-3">
                    <div className="p-2 bg-indigo-100 text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-400 rounded-lg">
                      <Building2 className="w-5 h-5" />
                    </div>
                    <div>
                      <h3 className="text-lg font-semibold text-text-primary">
                        Client Tenant Mapping
                      </h3>
                      <p className="text-sm text-text-secondary">
                        Map Azure AD tenant IDs to Sentinel clients for SSO access
                      </p>
                    </div>
                  </div>
                  <Button onClick={() => setShowAddTenant(true)}>
                    <Plus className="w-4 h-4 mr-2" />
                    Add Tenant
                  </Button>
                </div>

                {/* Add Tenant Form */}
                {showAddTenant && (
                  <div className="p-4 bg-gray-50 dark:bg-gray-800 rounded-lg mb-6">
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                      <div>
                        <label className="block text-sm font-medium text-text-primary mb-1.5">
                          Azure AD Tenant ID *
                        </label>
                        <input
                          type="text"
                          value={newTenant.tenantId}
                          onChange={(e) =>
                            setNewTenant({ ...newTenant, tenantId: e.target.value })
                          }
                          className="w-full px-3 py-2 border border-border rounded-lg text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
                          placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
                        />
                        <p className="text-xs text-text-secondary mt-1">
                          Found in Azure Portal → Microsoft Entra ID → Overview → Tenant ID
                        </p>
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-text-primary mb-1.5">
                          Organization Name
                        </label>
                        <input
                          type="text"
                          value={newTenant.tenantName}
                          onChange={(e) =>
                            setNewTenant({ ...newTenant, tenantName: e.target.value })
                          }
                          className="w-full px-3 py-2 border border-border rounded-lg text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
                          placeholder="Contoso Corp"
                        />
                        <p className="text-xs text-text-secondary mt-1">
                          This will be used as the client name if auto-creating
                        </p>
                      </div>
                    </div>
                    {clients.length > 0 && (
                      <div className="mt-4">
                        <label className="block text-sm font-medium text-text-primary mb-1.5">
                          Link to Existing Client (optional)
                        </label>
                        <select
                          value={newTenant.clientId}
                          onChange={(e) =>
                            setNewTenant({ ...newTenant, clientId: e.target.value })
                          }
                          className="w-full px-3 py-2 border border-border rounded-lg text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
                        >
                          <option value="">Auto-create new client</option>
                          {clients.map((client) => (
                            <option key={client.id} value={client.id}>
                              {client.name}
                            </option>
                          ))}
                        </select>
                      </div>
                    )}
                    <div className="flex gap-2 mt-4">
                      <Button
                        onClick={handleAddTenant}
                        isLoading={createTenantMutation.isPending}
                      >
                        Add Tenant
                      </Button>
                      <Button
                        variant="secondary"
                        onClick={() => {
                          setShowAddTenant(false);
                          setNewTenant({ clientId: '', tenantId: '', tenantName: '' });
                        }}
                      >
                        Cancel
                      </Button>
                    </div>
                  </div>
                )}

                {/* Tenant Table */}
                <div className="overflow-x-auto">
                  <table className="w-full">
                    <thead>
                      <tr className="border-b border-border">
                        <th className="text-left py-3 px-4 text-sm font-medium text-text-secondary">
                          Client
                        </th>
                        <th className="text-left py-3 px-4 text-sm font-medium text-text-secondary">
                          Tenant ID
                        </th>
                        <th className="text-left py-3 px-4 text-sm font-medium text-text-secondary">
                          Tenant Name
                        </th>
                        <th className="text-right py-3 px-4 text-sm font-medium text-text-secondary">
                          Actions
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {tenantsLoading ? (
                        <tr>
                          <td colSpan={4} className="py-8 text-center text-text-secondary">
                            Loading...
                          </td>
                        </tr>
                      ) : clientTenants.length === 0 ? (
                        <tr>
                          <td colSpan={4} className="py-8 text-center text-text-secondary">
                            No tenant mappings configured. Add a tenant to allow Azure AD users to sign in.
                          </td>
                        </tr>
                      ) : (
                        clientTenants.map((tenant) => (
                          <tr key={tenant.id} className="border-b border-border">
                            <td className="py-3 px-4 text-text-primary">
                              {tenant.clientName || 'Unknown'}
                            </td>
                            <td className="py-3 px-4 text-text-secondary font-mono text-sm">
                              {tenant.tenantId}
                            </td>
                            <td className="py-3 px-4 text-text-secondary">
                              {tenant.tenantName || '-'}
                            </td>
                            <td className="py-3 px-4 text-right">
                              <button
                                onClick={() => handleDeleteTenant(tenant.id)}
                                className="p-2 text-red-500 hover:bg-red-100 dark:hover:bg-red-900/30 rounded-lg transition-colors"
                                title="Remove"
                              >
                                <Trash2 className="w-4 h-4" />
                              </button>
                            </td>
                          </tr>
                        ))
                      )}
                    </tbody>
                  </table>
                </div>

                <div className="mt-4 p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                  <p className="text-xs text-blue-800 dark:text-blue-200">
                    <strong>Note:</strong> Users from mapped Azure AD tenants can sign in to the Support Portal
                    using their Microsoft work accounts. A Sentinel client will be auto-created for each tenant
                    if not linked to an existing one.
                  </p>
                </div>
              </CardContent>
            </Card>
          </>
        )}
      </div>
    </div>
  );
}
