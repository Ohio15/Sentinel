import React, { useState, useEffect } from 'react';
import { useDeviceStore, Device } from '../stores/deviceStore';
import { useClientStore } from '../stores/clientStore';

interface DevicesProps {
  onDeviceSelect: (deviceId: string) => void;
}

interface ServerInfo {
  port: number;
  agentCount: number;
  enrollmentToken: string;
}

export function Devices({ onDeviceSelect }: DevicesProps) {
  const { devices, loading, deleteDevice } = useDeviceStore();
  const { clients, currentClientId } = useClientStore();
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<string>('all');

  const getClientName = (clientId?: string) => {
    if (!clientId) return null;
    return clients.find(c => c.id === clientId);
  };
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<'devices' | 'installation'>('devices');
  const [serverInfo, setServerInfo] = useState<ServerInfo | null>(null);
  const [copied, setCopied] = useState(false);
  const [downloadingPlatform, setDownloadingPlatform] = useState<string | null>(null);
  const [downloadResult, setDownloadResult] = useState<{ type: 'success' | 'error'; message: string } | null>(null);
  const [psRunning, setPsRunning] = useState(false);

  useEffect(() => {
    loadServerInfo();
  }, []);

  const loadServerInfo = async () => {
    try {
      const info = await window.api.server.getInfo();
      setServerInfo(info);
    } catch (error) {
      console.error('Failed to load server info:', error);
    }
  };

  const handleRegenerateToken = async () => {
    if (confirm('Are you sure? Existing agents will need to be re-enrolled.')) {
      const newToken = await window.api.server.regenerateToken();
      setServerInfo(prev => prev ? { ...prev, enrollmentToken: newToken } : null);
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleDownload = async (platform: string) => {
    setDownloadingPlatform(platform);
    setDownloadResult(null);

    try {
      const result = await window.api.agent.download(platform);

      if (result.canceled) {
        setDownloadResult(null);
      } else if (result.success) {
        const sizeMB = result.size ? (result.size / 1024 / 1024).toFixed(1) : '?';
        setDownloadResult({
          type: 'success',
          message: result.installCommand
            ? `Installer saved (${sizeMB} MB). Install command: ${result.installCommand}`
            : `Installer saved (${sizeMB} MB). Double-click to install.`
        });
      } else {
        setDownloadResult({
          type: 'error',
          message: result.error || 'Download failed'
        });
      }
    } catch (error) {
      setDownloadResult({
        type: 'error',
        message: error instanceof Error ? error.message : 'Download failed'
      });
    } finally {
      setDownloadingPlatform(null);
      setTimeout(() => setDownloadResult(null), 5000);
    }
  };

  const handlePowerShellInstall = async () => {
    setPsRunning(true);
    try {
      const result = await window.api.agent.runPowerShellInstall();
      if (!result.success) {
        setDownloadResult({
          type: 'error',
          message: result.error || 'Failed to launch PowerShell',
        });
      }
    } catch (error) {
      setDownloadResult({
        type: 'error',
        message: error instanceof Error ? error.message : 'Failed to launch PowerShell',
      });
    } finally {
      setPsRunning(false);
    }
  };

  const filteredDevices = devices.filter(device => {
    const matchesSearch =
      device.hostname.toLowerCase().includes(search.toLowerCase()) ||
      device.displayName?.toLowerCase().includes(search.toLowerCase()) ||
      device.ipAddress.includes(search);

    const matchesStatus = statusFilter === 'all' || device.status === statusFilter;

    return matchesSearch && matchesStatus;
  });

  const handleDelete = async (id: string) => {
    await deleteDevice(id);
    setDeleteConfirm(null);
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-text-primary">Devices</h1>
        <span className="text-sm text-text-secondary">
          {filteredDevices.length} of {devices.length} devices
        </span>
      </div>

      {/* Tabs */}
      <div className="border-b border-border">
        <div className="flex gap-4">
          <button
            onClick={() => setActiveTab('devices')}
            className={`pb-2 px-1 text-sm font-medium border-b-2 transition-colors ${
              activeTab === 'devices'
                ? 'border-primary text-primary'
                : 'border-transparent text-text-secondary hover:text-text-primary'
            }`}
          >
            Device List
          </button>
          <button
            onClick={() => setActiveTab('installation')}
            className={`pb-2 px-1 text-sm font-medium border-b-2 transition-colors ${
              activeTab === 'installation'
                ? 'border-primary text-primary'
                : 'border-transparent text-text-secondary hover:text-text-primary'
            }`}
          >
            Agent Installation
          </button>
        </div>
      </div>

      {activeTab === 'devices' && (
        <>
          {/* Filters */}
          <div className="flex gap-4">
            <div className="flex-1">
              <input
                type="text"
                placeholder="Search devices..."
                value={search}
                onChange={e => setSearch(e.target.value)}
                className="input"
              />
            </div>
            <select
              value={statusFilter}
              onChange={e => setStatusFilter(e.target.value)}
              className="input w-40"
            >
              <option value="all">All Status</option>
              <option value="online">Online</option>
              <option value="offline">Offline</option>
              <option value="warning">Warning</option>
              <option value="critical">Critical</option>
            </select>
          </div>

          {/* Device Table */}
          {loading ? (
            <div className="card p-8 text-center">
              <p className="text-text-secondary">Loading devices...</p>
            </div>
          ) : filteredDevices.length === 0 ? (
            <div className="card p-8 text-center">
              <p className="text-text-secondary">
                {devices.length === 0 ? (
                  <>
                    No devices registered yet.{' '}
                    <button
                      onClick={() => setActiveTab('installation')}
                      className="text-primary hover:underline"
                    >
                      Install an agent
                    </button>{' '}
                    to get started.
                  </>
                ) : (
                  'No devices match your search criteria.'
                )}
              </p>
            </div>
          ) : (
            <div className="card overflow-hidden">
              <table>
                <thead>
                  <tr>
                    <th>Status</th>
                    <th>Hostname</th>
                    {!currentClientId && <th>Client</th>}
                    <th>OS</th>
                    <th>IP Address</th>
                    <th>Last Seen</th>
                    <th>Agent Version</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {filteredDevices.map(device => (
                    <tr key={device.id} className="cursor-pointer" onClick={() => onDeviceSelect(device.id)}>
                      <td>
                        <StatusBadge status={device.status} />
                      </td>
                      <td>
                        <div>
                          <p className="font-medium text-text-primary">
                            {device.displayName || device.hostname}
                          </p>
                          {device.displayName && device.hostname !== device.displayName && (
                            <p className="text-sm text-text-secondary">{device.hostname}</p>
                          )}
                        </div>
                      </td>
                      {!currentClientId && (
                        <td>
                          {(() => {
                            const client = getClientName(device.clientId);
                            if (client) {
                              return (
                                <div className="flex items-center gap-2">
                                  <div
                                    className="w-2.5 h-2.5 rounded-full"
                                    style={{ backgroundColor: client.color || '#6366f1' }}
                                  />
                                  <span className="text-sm">{client.name}</span>
                                </div>
                              );
                            }
                            return <span className="text-sm text-text-secondary">-</span>;
                          })()}
                        </td>
                      )}
                      <td>
                        <div className="flex items-center gap-2">
                          <OsIcon osType={device.osType} />
                          <span className="text-sm">
                            {device.osType} {device.osVersion}
                          </span>
                        </div>
                      </td>
                      <td className="font-mono text-sm">{device.ipAddress}</td>
                      <td className="text-sm text-text-secondary">
                        {formatLastSeen(device.lastSeen)}
                      </td>
                      <td className="text-sm">{device.agentVersion}</td>
                      <td onClick={e => e.stopPropagation()}>
                        {deleteConfirm === device.id ? (
                          <div className="flex gap-2">
                            <button
                              onClick={() => handleDelete(device.id)}
                              className="btn btn-danger text-xs py-1"
                            >
                              Confirm
                            </button>
                            <button
                              onClick={() => setDeleteConfirm(null)}
                              className="btn btn-secondary text-xs py-1"
                            >
                              Cancel
                            </button>
                          </div>
                        ) : (
                          <button
                            onClick={() => setDeleteConfirm(device.id)}
                            className="text-text-secondary hover:text-danger transition-colors"
                            title="Delete device"
                          >
                            <TrashIcon className="w-4 h-4" />
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}

      {activeTab === 'installation' && serverInfo && (
        <div className="space-y-6">
          {/* Enrollment Token */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-text-primary mb-4">Enrollment Token</h2>
            <p className="text-sm text-text-secondary mb-4">
              Use this token to enroll new agents. Keep it secure!
            </p>
            <div className="flex gap-2">
              <input
                type="text"
                value={serverInfo.enrollmentToken}
                readOnly
                className="input flex-1 font-mono text-sm"
              />
              <button
                onClick={() => copyToClipboard(serverInfo.enrollmentToken)}
                className="btn btn-secondary"
              >
                {copied ? 'Copied!' : 'Copy'}
              </button>
              <button onClick={handleRegenerateToken} className="btn btn-danger">
                Regenerate
              </button>
            </div>
          </div>

          {/* Agent Downloads */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-text-primary mb-4">Download Agent Installer</h2>
            <p className="text-sm text-text-secondary mb-4">
              Download the platform-specific installer. Installation is automatic - just run the downloaded file.
            </p>

            {/* Download Result Toast */}
            {downloadResult && (
              <div className={`mb-4 p-4 rounded-lg flex items-center gap-3 ${
                downloadResult.type === 'success'
                  ? 'bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800'
                  : 'bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800'
              }`}>
                {downloadResult.type === 'success' ? (
                  <CheckIcon className="w-5 h-5 text-green-600 dark:text-green-400" />
                ) : (
                  <ErrorIcon className="w-5 h-5 text-red-600 dark:text-red-400" />
                )}
                <span className={`text-sm ${
                  downloadResult.type === 'success' ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'
                }`}>
                  {downloadResult.message}
                </span>
                <button
                  onClick={() => setDownloadResult(null)}
                  className="ml-auto text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                >
                  <CloseIcon className="w-4 h-4" />
                </button>
              </div>
            )}

            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <button
                onClick={() => handleDownload('windows')}
                disabled={downloadingPlatform !== null}
                className="flex items-center gap-3 p-4 bg-gray-50 dark:bg-gray-800 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors border border-border disabled:opacity-50 disabled:cursor-not-allowed text-left"
              >
                <WindowsIcon className="w-5 h-5 text-blue-500" />
                <div className="flex-1">
                  <p className="font-medium text-text-primary">Windows</p>
                  <p className="text-xs text-text-secondary">
                    {downloadingPlatform === 'windows' ? 'Saving...' : 'sentinel-agent.msi'}
                  </p>
                </div>
                {downloadingPlatform === 'windows' ? <SpinnerIcon /> : <DownloadIcon />}
              </button>
              <button
                onClick={() => handleDownload('macos')}
                disabled={downloadingPlatform !== null}
                className="flex items-center gap-3 p-4 bg-gray-50 dark:bg-gray-800 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors border border-border disabled:opacity-50 disabled:cursor-not-allowed text-left"
              >
                <AppleIcon className="w-5 h-5 text-gray-600 dark:text-gray-400" />
                <div className="flex-1">
                  <p className="font-medium text-text-primary">macOS</p>
                  <p className="text-xs text-text-secondary">
                    {downloadingPlatform === 'macos' ? 'Saving...' : 'sentinel-agent.pkg'}
                  </p>
                </div>
                {downloadingPlatform === 'macos' ? <SpinnerIcon /> : <DownloadIcon />}
              </button>
              <button
                onClick={() => handleDownload('linux')}
                disabled={downloadingPlatform !== null}
                className="flex items-center gap-3 p-4 bg-gray-50 dark:bg-gray-800 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors border border-border disabled:opacity-50 disabled:cursor-not-allowed text-left"
              >
                <LinuxIcon className="w-5 h-5 text-orange-500" />
                <div className="flex-1">
                  <p className="font-medium text-text-primary">Linux</p>
                  <p className="text-xs text-text-secondary">
                    {downloadingPlatform === 'linux' ? 'Saving...' : 'sentinel-agent.deb'}
                  </p>
                </div>
                {downloadingPlatform === 'linux' ? <SpinnerIcon /> : <DownloadIcon />}
              </button>
            </div>
          </div>

          
          {/* Quick Install - PowerShell */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-text-primary mb-4">Quick Install (PowerShell)</h2>
            <p className="text-sm text-text-secondary mb-4">
              One-click installation using PowerShell. Opens an elevated PowerShell window and automatically runs the install script.
            </p>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <button
                onClick={handlePowerShellInstall}
                disabled={psRunning}
                className="flex items-center gap-3 p-4 bg-blue-50 dark:bg-blue-900/30 rounded-lg hover:bg-blue-100 dark:hover:bg-blue-900/50 transition-colors border border-blue-200 dark:border-blue-800 disabled:opacity-50 disabled:cursor-not-allowed text-left"
              >
                <PowerShellIcon className="w-5 h-5 text-blue-700 dark:text-blue-400" />
                <div className="flex-1">
                  <p className="font-medium text-blue-900 dark:text-blue-100">Run PowerShell Install</p>
                  <p className="text-xs text-blue-700 dark:text-blue-300">
                    {psRunning ? 'Launching...' : 'Opens elevated PowerShell window'}
                  </p>
                </div>
                {psRunning ? <SpinnerIcon /> : <PlayIcon className="w-5 h-5 text-blue-600 dark:text-blue-400" />}
              </button>

              <div className="p-4 bg-gray-50 rounded-lg border border-border">
                <div className="flex items-center gap-2 mb-2">
                  <InfoIcon className="w-5 h-5 text-gray-500 dark:text-gray-400" />
                  <span className="font-medium text-text-primary">What happens</span>
                </div>
                <ul className="text-xs text-text-secondary space-y-1">
                  <li>1. UAC prompt requests admin rights</li>
                  <li>2. Downloads agent from this server</li>
                  <li>3. Installs and starts the service</li>
                </ul>
              </div>
            </div>
          </div>

          {/* Installation Notes */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-text-primary mb-4">Installation Notes</h2>
            <ul className="list-disc list-inside space-y-2 text-sm text-text-secondary">
              <li>The agent will automatically connect to this server once installed</li>
              <li>Agents run as a system service and start automatically on boot</li>
              <li>Make sure port {serverInfo.port} is accessible from the target machine</li>
              <li>For Windows, run the command in Command Prompt or PowerShell as Administrator</li>
              <li>For Linux/macOS, make the binary executable first: <code className="bg-gray-100 dark:bg-gray-700 px-1 rounded">chmod +x sentinel-agent</code></li>
              <li>Linux/macOS require sudo privileges for installation</li>
            </ul>
          </div>
        </div>
      )}
    </div>
  );
}

function StatusBadge({ status }: { status: Device['status'] }) {
  const styles = {
    online: 'badge-success',
    offline: 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300',
    warning: 'badge-warning',
    critical: 'badge-danger',
  };

  return (
    <span className={`badge ${styles[status]}`}>
      {status.charAt(0).toUpperCase() + status.slice(1)}
    </span>
  );
}

function OsIcon({ osType }: { osType: string }) {
  const type = osType.toLowerCase();

  if (type.includes('windows')) {
    return <WindowsIcon className="w-4 h-4 text-blue-500" />;
  } else if (type.includes('mac') || type.includes('darwin')) {
    return <AppleIcon className="w-4 h-4 text-gray-600 dark:text-gray-400" />;
  } else {
    return <LinuxIcon className="w-4 h-4 text-orange-500" />;
  }
}

function formatLastSeen(lastSeen: string): string {
  const date = new Date(lastSeen);
  const now = new Date();
  const diff = now.getTime() - date.getTime();

  const minutes = Math.floor(diff / 60000);
  const hours = Math.floor(diff / 3600000);
  const days = Math.floor(diff / 86400000);

  if (minutes < 1) return 'Just now';
  if (minutes < 60) return `${minutes}m ago`;
  if (hours < 24) return `${hours}h ago`;
  if (days < 7) return `${days}d ago`;

  return date.toLocaleDateString();
}

// Icons
function WindowsIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor">
      <path d="M3 5.5L10.5 4.5V11.5H3V5.5ZM3 12.5H10.5V19.5L3 18.5V12.5ZM11.5 4.25L21 3V11.5H11.5V4.25ZM11.5 12.5H21V21L11.5 19.75V12.5Z" />
    </svg>
  );
}

function AppleIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor">
      <path d="M18.71 19.5c-.83 1.24-1.71 2.45-3.05 2.47-1.34.03-1.77-.79-3.29-.79-1.53 0-2 .77-3.27.82-1.31.05-2.3-1.32-3.14-2.53C4.25 17 2.94 12.45 4.7 9.39c.87-1.52 2.43-2.48 4.12-2.51 1.28-.02 2.5.87 3.29.87.78 0 2.26-1.07 3.81-.91.65.03 2.47.26 3.64 1.98-.09.06-2.17 1.28-2.15 3.81.03 3.02 2.65 4.03 2.68 4.04-.03.07-.42 1.44-1.38 2.83M13 3.5c.73-.83 1.94-1.46 2.94-1.5.13 1.17-.34 2.35-1.04 3.19-.69.85-1.83 1.51-2.95 1.42-.15-1.15.41-2.35 1.05-3.11z" />
    </svg>
  );
}

function LinuxIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor">
      <path d="M12.504 0c-.155 0-.315.008-.48.021-4.226.333-3.105 4.807-3.17 6.298-.076 1.092-.3 1.953-1.05 3.02-.885 1.051-2.127 2.75-2.716 4.521-.278.832-.41 1.684-.287 2.489a.424.424 0 00-.11.135c-.26.268-.45.6-.663.839-.199.199-.485.267-.797.4-.313.136-.658.269-.864.68-.09.189-.136.394-.132.602 0 .199.027.4.055.536.058.399.116.728.04.97-.249.68-.28 1.145-.106 1.484.174.334.535.47.94.601.81.2 1.91.135 2.774.6.926.466 1.866.67 2.616.47.526-.116.97-.464 1.208-.946.587-.003 1.23-.269 2.26-.334.699-.058 1.574.267 2.577.2.025.134.063.198.114.333l.003.003c.391.778 1.113 1.132 1.884 1.071.771-.06 1.592-.536 2.257-1.306.631-.765 1.683-1.084 2.378-1.503.348-.199.629-.469.649-.853.023-.4-.2-.811-.714-1.376v-.097l-.003-.003c-.17-.2-.25-.535-.338-.926-.085-.401-.182-.786-.492-1.046h-.003c-.059-.054-.123-.067-.188-.135a.357.357 0 00-.19-.064c.431-1.278.264-2.55-.173-3.694-.533-1.41-1.465-2.638-2.175-3.483-.796-1.005-1.576-1.957-1.56-3.368.026-2.152.236-6.133-3.544-6.139z" />
    </svg>
  );
}

function DownloadIcon() {
  return (
    <svg className="w-5 h-5 text-primary" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
      <polyline points="7 10 12 15 17 10" />
      <line x1="12" y1="15" x2="12" y2="3" />
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

function SpinnerIcon() {
  return (
    <svg className="w-5 h-5 text-primary animate-spin" viewBox="0 0 24 24" fill="none">
      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
      <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
    </svg>
  );
}

function CheckIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
    </svg>
  );
}

function ErrorIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
    </svg>
  );
}

function CloseIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
    </svg>
  );
}

function MsiIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor">
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6zm-1 9h-2v2H9v-2H7v-2h2V7h2v2h2v2zm3 8H8v-2h8v2zm0-4H8v-2h8v2zm-2-8V3.5L18.5 9H14z"/>
    </svg>
  );
}

function TerminalIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
    </svg>
  );
}

function PowerShellIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor">
      <path d="M23.181 2.974c.568 0 .923.463.792 1.035l-3.659 15.982c-.13.572-.697 1.035-1.265 1.035H.819c-.568 0-.923-.463-.792-1.035L3.686 4.009c.13-.572.697-1.035 1.265-1.035h18.23zM6.669 16.108l1.06-.952 4.163-3.742-4.58-4.127L6.1 6.5l-.188.955 3.643 3.282-3.856 3.47-.189.955.188.946h.971zm5.781.946h5.469l.188-.946h-5.469l-.188.946z"/>
    </svg>
  );
}

function PlayIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
      <path strokeLinecap="round" strokeLinejoin="round" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
    </svg>
  );
}

function InfoIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
    </svg>
  );
}
