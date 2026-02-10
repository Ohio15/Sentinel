import { useState, useEffect } from 'react';
import { api } from '../services/api';

// Types
interface JWTStatus {
  currentVersion: number;
  status: string;
  createdAt: string;
  lastRotation?: string;
  nextScheduledRotation?: string;
  gracePeriodActive: boolean;
  gracePeriodEnds?: string;
  activeKeyCount: number;
  healthStatus: 'healthy' | 'warning' | 'overdue' | 'grace_period' | 'critical';
}

interface APIKey {
  id: string;
  keyPrefix: string;
  name: string;
  description?: string;
  permissions: string[];
  ipAllowlist?: string[];
  createdAt: string;
  lastUsedAt?: string;
  expiresAt?: string;
  revokedAt?: string;
  useCount: number;
}

interface APIKeyWithSecret extends APIKey {
  fullKey: string;
}

interface APIKeyStatus {
  totalKeys: number;
  activeKeys: number;
  expiringSoon: number;
  recentlyUsed: number;
}

interface RotationLog {
  id: string;
  credentialType: string;
  action: string;
  oldVersion?: number;
  newVersion: number;
  status: string;
  initiatedAt: string;
  completedAt?: string;
  failureReason?: string;
  affectedSessions: number;
  initiatedBy?: string;
  gracePeriodHours?: number;
}

interface RotationSchedule {
  credentialType: string;
  rotationIntervalDays: number;
  gracePeriodHours: number;
  warningThresholdDays: number;
  lastRotationAt?: string;
  nextScheduledRotation?: string;
  autoRotate: boolean;
  enabled: boolean;
}

// Permission options for API keys
const PERMISSION_OPTIONS = [
  { value: 'devices:read', label: 'Devices - Read', description: 'List and view devices' },
  { value: 'devices:write', label: 'Devices - Write', description: 'Update device settings' },
  { value: 'devices:delete', label: 'Devices - Delete', description: 'Delete devices' },
  { value: 'scripts:read', label: 'Scripts - Read', description: 'List and view scripts' },
  { value: 'scripts:execute', label: 'Scripts - Execute', description: 'Execute scripts on devices' },
  { value: 'alerts:read', label: 'Alerts - Read', description: 'View alerts' },
  { value: 'alerts:write', label: 'Alerts - Manage', description: 'Acknowledge and resolve alerts' },
  { value: 'admin:*', label: 'Full Admin', description: 'Full administrative access' },
];

export function CredentialManager() {
  const [activeSection, setActiveSection] = useState<'jwt' | 'apikeys' | 'history'>('jwt');
  const [jwtStatus, setJwtStatus] = useState<JWTStatus | null>(null);
  const [apiKeys, setApiKeys] = useState<APIKey[]>([]);
  const [apiKeyStatus, setApiKeyStatus] = useState<APIKeyStatus | null>(null);
  const [rotationHistory, setRotationHistory] = useState<RotationLog[]>([]);
  const [schedules, setSchedules] = useState<RotationSchedule[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Rotation states
  const [rotating, setRotating] = useState(false);
  const [rotationResult, setRotationResult] = useState<{ success: boolean; message: string } | null>(null);

  // API Key creation states
  const [showCreateKey, setShowCreateKey] = useState(false);
  const [newKeyName, setNewKeyName] = useState('');
  const [newKeyDescription, setNewKeyDescription] = useState('');
  const [newKeyPermissions, setNewKeyPermissions] = useState<string[]>([]);
  const [creatingKey, setCreatingKey] = useState(false);
  const [newKeyResult, setNewKeyResult] = useState<APIKeyWithSecret | null>(null);

  useEffect(() => {
    loadCredentialStatus();
  }, []);

  const loadCredentialStatus = async () => {
    try {
      setLoading(true);
      setError(null);

      const [statusRes, keysRes, historyRes] = await Promise.all([
        api.makeRequest<{ jwt_secret?: JWTStatus; api_key?: APIKeyStatus; schedules?: RotationSchedule[] }>('GET', '/credentials/status'),
        api.makeRequest<APIKey[]>('GET', '/credentials/api-keys').catch(() => []),
        api.makeRequest<RotationLog[]>('GET', '/credentials/rotation-history?limit=20').catch(() => []),
      ]);

      if (statusRes?.jwt_secret) {
        setJwtStatus(statusRes.jwt_secret);
      }
      if (statusRes?.api_key) {
        setApiKeyStatus(statusRes.api_key);
      }
      if (statusRes?.schedules) {
        setSchedules(statusRes.schedules || []);
      }

      setApiKeys(keysRes || []);
      setRotationHistory(historyRes || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load credential status');
    } finally {
      setLoading(false);
    }
  };

  const handleRotateJWT = async () => {
    if (!confirm('Are you sure you want to rotate the JWT signing key?\n\nExisting sessions will remain valid during the 24-hour grace period.')) {
      return;
    }

    try {
      setRotating(true);
      setRotationResult(null);

      const response = await api.makeRequest<{ newVersion: number; gracePeriodEnds: string }>('POST', '/credentials/jwt/rotate');

      setRotationResult({
        success: true,
        message: `Rotated to version ${response.newVersion}. Grace period ends ${new Date(response.gracePeriodEnds).toLocaleString()}`,
      });

      // Reload status
      await loadCredentialStatus();
    } catch (err) {
      setRotationResult({
        success: false,
        message: err instanceof Error ? err.message : 'Rotation failed',
      });
    } finally {
      setRotating(false);
    }
  };

  const handleCreateAPIKey = async () => {
    if (!newKeyName.trim()) {
      alert('Please enter a name for the API key');
      return;
    }
    if (newKeyPermissions.length === 0) {
      alert('Please select at least one permission');
      return;
    }

    try {
      setCreatingKey(true);

      const response = await api.makeRequest<APIKeyWithSecret>('POST', '/credentials/api-keys', {
        name: newKeyName.trim(),
        description: newKeyDescription.trim(),
        permissions: newKeyPermissions,
      });

      setNewKeyResult(response);

      // Reload keys
      const keysRes = await api.makeRequest<APIKey[]>('GET', '/credentials/api-keys');
      setApiKeys(keysRes || []);

      // Reset form
      setNewKeyName('');
      setNewKeyDescription('');
      setNewKeyPermissions([]);
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to create API key');
    } finally {
      setCreatingKey(false);
    }
  };

  const handleRevokeKey = async (keyId: string, keyName: string) => {
    if (!confirm(`Are you sure you want to revoke the API key "${keyName}"?\n\nThis action cannot be undone.`)) {
      return;
    }

    try {
      await api.makeRequest('DELETE', `/credentials/api-keys/${keyId}`);

      // Reload keys
      const keysRes = await api.makeRequest<APIKey[]>('GET', '/credentials/api-keys');
      setApiKeys(keysRes || []);
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to revoke API key');
    }
  };

  const getHealthStatusColor = (status: string) => {
    switch (status) {
      case 'healthy': return 'text-green-500';
      case 'warning': return 'text-yellow-500';
      case 'overdue': return 'text-red-500';
      case 'grace_period': return 'text-blue-500';
      case 'critical': return 'text-red-600';
      default: return 'text-gray-500';
    }
  };

  const getHealthStatusIcon = (status: string) => {
    switch (status) {
      case 'healthy': return '✓';
      case 'warning': return '⚠';
      case 'overdue': return '⚠';
      case 'grace_period': return '⏳';
      case 'critical': return '✕';
      default: return '?';
    }
  };

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleString();
  };

  const formatRelativeTime = (dateStr: string) => {
    const date = new Date(dateStr);
    const now = new Date();
    const diffMs = date.getTime() - now.getTime();
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));

    if (diffDays < 0) {
      return `${Math.abs(diffDays)} days ago`;
    } else if (diffDays === 0) {
      const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
      if (diffHours < 0) return `${Math.abs(diffHours)} hours ago`;
      return `in ${diffHours} hours`;
    }
    return `in ${diffDays} days`;
  };

  if (loading) {
    return (
      <div className="card p-6">
        <div className="flex items-center justify-center py-8">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500"></div>
          <span className="ml-3 text-text-secondary">Loading credential status...</span>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="card p-6">
        <div className="text-red-500 text-center py-4">
          <p>{error}</p>
          <button
            onClick={loadCredentialStatus}
            className="mt-2 text-blue-500 hover:underline"
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="card p-6">
      <h2 className="text-lg font-semibold text-text-primary mb-4">Credential Management</h2>

      {/* Section Tabs */}
      <div className="flex gap-2 mb-6 border-b border-border">
        <button
          onClick={() => setActiveSection('jwt')}
          className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
            activeSection === 'jwt'
              ? 'border-blue-500 text-blue-500'
              : 'border-transparent text-text-secondary hover:text-text-primary'
          }`}
        >
          JWT Signing Key
        </button>
        <button
          onClick={() => setActiveSection('apikeys')}
          className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
            activeSection === 'apikeys'
              ? 'border-blue-500 text-blue-500'
              : 'border-transparent text-text-secondary hover:text-text-primary'
          }`}
        >
          API Keys
          {apiKeyStatus && apiKeyStatus.activeKeys > 0 && (
            <span className="ml-2 px-2 py-0.5 text-xs bg-gray-200 dark:bg-gray-700 rounded-full">
              {apiKeyStatus.activeKeys}
            </span>
          )}
        </button>
        <button
          onClick={() => setActiveSection('history')}
          className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
            activeSection === 'history'
              ? 'border-blue-500 text-blue-500'
              : 'border-transparent text-text-secondary hover:text-text-primary'
          }`}
        >
          Rotation History
        </button>
      </div>

      {/* JWT Section */}
      {activeSection === 'jwt' && jwtStatus && (
        <div className="space-y-4">
          {/* Status Card */}
          <div className="bg-gray-50 dark:bg-gray-800 rounded-lg p-4">
            <div className="flex items-center justify-between mb-4">
              <div className="flex items-center gap-3">
                <span className={`text-2xl ${getHealthStatusColor(jwtStatus.healthStatus)}`}>
                  {getHealthStatusIcon(jwtStatus.healthStatus)}
                </span>
                <div>
                  <h3 className="font-medium text-text-primary">JWT Signing Key</h3>
                  <p className="text-sm text-text-secondary">
                    Version {jwtStatus.currentVersion} • {jwtStatus.activeKeyCount} active key{jwtStatus.activeKeyCount !== 1 ? 's' : ''}
                  </p>
                </div>
              </div>
              <span className={`px-3 py-1 rounded-full text-sm font-medium ${
                jwtStatus.healthStatus === 'healthy' ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200' :
                jwtStatus.healthStatus === 'warning' ? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200' :
                jwtStatus.healthStatus === 'grace_period' ? 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200' :
                'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
              }`}>
                {jwtStatus.healthStatus.replace('_', ' ').toUpperCase()}
              </span>
            </div>

            <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
              <div>
                <p className="text-text-secondary">Created</p>
                <p className="text-text-primary">{formatDate(jwtStatus.createdAt)}</p>
              </div>
              {jwtStatus.lastRotation && (
                <div>
                  <p className="text-text-secondary">Last Rotation</p>
                  <p className="text-text-primary">{formatDate(jwtStatus.lastRotation)}</p>
                </div>
              )}
              {jwtStatus.nextScheduledRotation && (
                <div>
                  <p className="text-text-secondary">Next Scheduled</p>
                  <p className="text-text-primary">{formatRelativeTime(jwtStatus.nextScheduledRotation)}</p>
                </div>
              )}
              {jwtStatus.gracePeriodActive && jwtStatus.gracePeriodEnds && (
                <div>
                  <p className="text-text-secondary">Grace Period Ends</p>
                  <p className="text-blue-500">{formatRelativeTime(jwtStatus.gracePeriodEnds)}</p>
                </div>
              )}
            </div>
          </div>

          {/* Grace Period Notice */}
          {jwtStatus.gracePeriodActive && (
            <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-4">
              <div className="flex items-start gap-3">
                <span className="text-blue-500 text-xl">⏳</span>
                <div>
                  <h4 className="font-medium text-blue-800 dark:text-blue-200">Grace Period Active</h4>
                  <p className="text-sm text-blue-700 dark:text-blue-300">
                    Both old and new keys are currently valid. Existing sessions will continue to work.
                    The old key will be retired automatically when the grace period ends.
                  </p>
                </div>
              </div>
            </div>
          )}

          {/* Rotation Result */}
          {rotationResult && (
            <div className={`rounded-lg p-4 ${
              rotationResult.success
                ? 'bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800'
                : 'bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800'
            }`}>
              <p className={rotationResult.success ? 'text-green-800 dark:text-green-200' : 'text-red-800 dark:text-red-200'}>
                {rotationResult.success ? '✓' : '✕'} {rotationResult.message}
              </p>
            </div>
          )}

          {/* Actions */}
          <div className="flex gap-3">
            <button
              onClick={handleRotateJWT}
              disabled={rotating}
              className="px-4 py-2 bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
            >
              {rotating ? (
                <>
                  <span className="animate-spin">↻</span>
                  Rotating...
                </>
              ) : (
                <>
                  <span>↻</span>
                  Rotate Now
                </>
              )}
            </button>
            <button
              onClick={loadCredentialStatus}
              className="px-4 py-2 border border-border rounded-lg hover:bg-gray-50 dark:hover:bg-gray-800 text-text-primary"
            >
              Refresh
            </button>
          </div>

          {/* Info Box */}
          <div className="bg-gray-100 dark:bg-gray-800 rounded-lg p-4 text-sm text-text-secondary">
            <h4 className="font-medium text-text-primary mb-2">About JWT Rotation</h4>
            <ul className="list-disc list-inside space-y-1">
              <li>Rotation creates a new signing key while keeping the old key valid</li>
              <li>During the 24-hour grace period, both keys can validate tokens</li>
              <li>No users will be logged out during rotation</li>
              <li>After the grace period, only the new key will be valid</li>
            </ul>
          </div>
        </div>
      )}

      {/* API Keys Section */}
      {activeSection === 'apikeys' && (
        <div className="space-y-4">
          {/* Create New Key Button */}
          {!showCreateKey && !newKeyResult && (
            <button
              onClick={() => setShowCreateKey(true)}
              className="px-4 py-2 bg-blue-500 text-white rounded-lg hover:bg-blue-600 flex items-center gap-2"
            >
              <span>+</span>
              Create API Key
            </button>
          )}

          {/* New Key Created Result */}
          {newKeyResult && (
            <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg p-4">
              <div className="flex items-start justify-between">
                <div>
                  <h4 className="font-medium text-green-800 dark:text-green-200 mb-2">
                    API Key Created Successfully
                  </h4>
                  <p className="text-sm text-green-700 dark:text-green-300 mb-3">
                    Copy this key now. You won't be able to see it again.
                  </p>
                  <div className="bg-white dark:bg-gray-900 rounded p-3 font-mono text-sm break-all">
                    {newKeyResult.fullKey}
                  </div>
                </div>
                <button
                  onClick={() => setNewKeyResult(null)}
                  className="text-green-600 hover:text-green-800"
                >
                  ✕
                </button>
              </div>
              <div className="mt-3 flex gap-2">
                <button
                  onClick={() => {
                    navigator.clipboard.writeText(newKeyResult.fullKey);
                    alert('API key copied to clipboard');
                  }}
                  className="px-3 py-1.5 bg-green-600 text-white rounded text-sm hover:bg-green-700"
                >
                  Copy to Clipboard
                </button>
                <button
                  onClick={() => setNewKeyResult(null)}
                  className="px-3 py-1.5 border border-green-600 text-green-600 rounded text-sm hover:bg-green-50 dark:hover:bg-green-900/30"
                >
                  Done
                </button>
              </div>
            </div>
          )}

          {/* Create Key Form */}
          {showCreateKey && !newKeyResult && (
            <div className="bg-gray-50 dark:bg-gray-800 rounded-lg p-4">
              <h4 className="font-medium text-text-primary mb-4">Create New API Key</h4>

              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-text-primary mb-1">Name *</label>
                  <input
                    type="text"
                    value={newKeyName}
                    onChange={(e) => setNewKeyName(e.target.value)}
                    placeholder="e.g., CI/CD Integration"
                    className="w-full px-3 py-2 border border-border rounded-lg bg-white dark:bg-gray-900 text-text-primary"
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-text-primary mb-1">Description</label>
                  <input
                    type="text"
                    value={newKeyDescription}
                    onChange={(e) => setNewKeyDescription(e.target.value)}
                    placeholder="Optional description"
                    className="w-full px-3 py-2 border border-border rounded-lg bg-white dark:bg-gray-900 text-text-primary"
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-text-primary mb-2">Permissions *</label>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
                    {PERMISSION_OPTIONS.map((perm) => (
                      <label
                        key={perm.value}
                        className="flex items-start gap-2 p-2 border border-border rounded hover:bg-gray-100 dark:hover:bg-gray-700 cursor-pointer"
                      >
                        <input
                          type="checkbox"
                          checked={newKeyPermissions.includes(perm.value)}
                          onChange={(e) => {
                            if (e.target.checked) {
                              setNewKeyPermissions([...newKeyPermissions, perm.value]);
                            } else {
                              setNewKeyPermissions(newKeyPermissions.filter(p => p !== perm.value));
                            }
                          }}
                          className="mt-1"
                        />
                        <div>
                          <div className="text-sm font-medium text-text-primary">{perm.label}</div>
                          <div className="text-xs text-text-secondary">{perm.description}</div>
                        </div>
                      </label>
                    ))}
                  </div>
                </div>
              </div>

              <div className="flex gap-2 mt-4">
                <button
                  onClick={handleCreateAPIKey}
                  disabled={creatingKey}
                  className="px-4 py-2 bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50"
                >
                  {creatingKey ? 'Creating...' : 'Create Key'}
                </button>
                <button
                  onClick={() => {
                    setShowCreateKey(false);
                    setNewKeyName('');
                    setNewKeyDescription('');
                    setNewKeyPermissions([]);
                  }}
                  className="px-4 py-2 border border-border rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 text-text-primary"
                >
                  Cancel
                </button>
              </div>
            </div>
          )}

          {/* API Keys List */}
          {apiKeys.length > 0 ? (
            <div className="space-y-3">
              {apiKeys.map((key) => (
                <div
                  key={key.id}
                  className="bg-gray-50 dark:bg-gray-800 rounded-lg p-4"
                >
                  <div className="flex items-start justify-between">
                    <div>
                      <div className="flex items-center gap-2">
                        <h4 className="font-medium text-text-primary">{key.name}</h4>
                        {key.revokedAt && (
                          <span className="px-2 py-0.5 text-xs bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200 rounded">
                            Revoked
                          </span>
                        )}
                      </div>
                      <p className="text-sm font-mono text-text-secondary">{key.keyPrefix}...</p>
                      {key.description && (
                        <p className="text-sm text-text-secondary mt-1">{key.description}</p>
                      )}
                    </div>
                    {!key.revokedAt && (
                      <button
                        onClick={() => handleRevokeKey(key.id, key.name)}
                        className="text-red-500 hover:text-red-700 text-sm"
                      >
                        Revoke
                      </button>
                    )}
                  </div>

                  <div className="mt-3 flex flex-wrap gap-1">
                    {key.permissions.map((perm) => (
                      <span
                        key={perm}
                        className="px-2 py-0.5 text-xs bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200 rounded"
                      >
                        {perm}
                      </span>
                    ))}
                  </div>

                  <div className="mt-3 flex gap-4 text-xs text-text-secondary">
                    <span>Created {formatDate(key.createdAt)}</span>
                    {key.lastUsedAt && <span>Last used {formatDate(key.lastUsedAt)}</span>}
                    <span>{key.useCount} requests</span>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="text-center py-8 text-text-secondary">
              <p>No API keys created yet</p>
              <p className="text-sm mt-1">Create an API key to integrate with external systems</p>
            </div>
          )}
        </div>
      )}

      {/* Rotation History Section */}
      {activeSection === 'history' && (
        <div className="space-y-4">
          {rotationHistory.length > 0 ? (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border">
                    <th className="text-left py-2 px-3 text-text-secondary font-medium">Type</th>
                    <th className="text-left py-2 px-3 text-text-secondary font-medium">Action</th>
                    <th className="text-left py-2 px-3 text-text-secondary font-medium">Version</th>
                    <th className="text-left py-2 px-3 text-text-secondary font-medium">Status</th>
                    <th className="text-left py-2 px-3 text-text-secondary font-medium">Time</th>
                    <th className="text-left py-2 px-3 text-text-secondary font-medium">By</th>
                  </tr>
                </thead>
                <tbody>
                  {rotationHistory.map((log) => (
                    <tr key={log.id} className="border-b border-border hover:bg-gray-50 dark:hover:bg-gray-800">
                      <td className="py-2 px-3 text-text-primary capitalize">{log.credentialType.replace('_', ' ')}</td>
                      <td className="py-2 px-3 text-text-primary capitalize">{log.action}</td>
                      <td className="py-2 px-3 text-text-primary">
                        {log.oldVersion ? `v${log.oldVersion} → v${log.newVersion}` : `v${log.newVersion}`}
                      </td>
                      <td className="py-2 px-3">
                        <span className={`px-2 py-0.5 rounded text-xs ${
                          log.status === 'success' ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200' :
                          log.status === 'failed' ? 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200' :
                          'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200'
                        }`}>
                          {log.status}
                        </span>
                      </td>
                      <td className="py-2 px-3 text-text-secondary">{formatDate(log.initiatedAt)}</td>
                      <td className="py-2 px-3 text-text-secondary">{log.initiatedBy || 'System'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="text-center py-8 text-text-secondary">
              <p>No rotation history yet</p>
              <p className="text-sm mt-1">Rotation events will appear here</p>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
