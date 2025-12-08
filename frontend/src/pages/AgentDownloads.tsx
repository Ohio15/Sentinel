import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Download,
  Key,
  Plus,
  Trash2,
  RefreshCw,
  Copy,
  Check,
  Terminal,
  Monitor,
  Server,
  Apple,
  Clock,
  Hash,
  ToggleLeft,
  ToggleRight
} from 'lucide-react';
import { api } from '@/services/api';
import { useAuthStore } from '@/stores/authStore';
import toast from 'react-hot-toast';

interface EnrollmentToken {
  id: string;
  token: string;
  name: string;
  description?: string;
  createdBy?: string;
  expiresAt?: string;
  maxUses?: number;
  useCount: number;
  isActive: boolean;
  tags?: string[];
  metadata?: Record<string, string>;
  createdAt: string;
  updatedAt: string;
}

interface AgentInstaller {
  platform: string;
  architecture: string;
  filename: string;
  size: number;
  version: string;
  downloadUrl: string;
}

interface CreateTokenForm {
  name: string;
  description: string;
  expiresAt: string;
  maxUses: string;
}

export function AgentDownloads() {
  const { user } = useAuthStore();
  const queryClient = useQueryClient();
  const isAdmin = user?.role === 'admin';

  const [selectedToken, setSelectedToken] = useState<string>('');
  const [copiedField, setCopiedField] = useState<string>('');
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [createForm, setCreateForm] = useState<CreateTokenForm>({
    name: '',
    description: '',
    expiresAt: '',
    maxUses: ''
  });

  // Fetch enrollment tokens
  const { data: tokens = [], isLoading: tokensLoading } = useQuery({
    queryKey: ['enrollment-tokens'],
    queryFn: () => api.getEnrollmentTokens(),
    enabled: isAdmin
  });

  // Fetch available installers
  const { data: installers = [], isLoading: installersLoading } = useQuery({
    queryKey: ['agent-installers'],
    queryFn: () => api.getAgentInstallers()
  });

  // Mutations
  const createTokenMutation = useMutation({
    mutationFn: (data: { name: string; description?: string; expiresAt?: string; maxUses?: number }) =>
      api.createEnrollmentToken(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['enrollment-tokens'] });
      setShowCreateModal(false);
      setCreateForm({ name: '', description: '', expiresAt: '', maxUses: '' });
      toast.success('Enrollment token created successfully');
    },
    onError: (error: Error) => {
      console.error('Failed to create token:', error);
      toast.error('Failed to create token: ' + (error.message || 'Unknown error'));
    }
  });

  const deleteTokenMutation = useMutation({
    mutationFn: (id: string) => api.deleteEnrollmentToken(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['enrollment-tokens'] });
      if (selectedToken) setSelectedToken('');
      toast.success('Token deleted successfully');
    },
    onError: (error: Error) => {
      toast.error('Failed to delete token: ' + (error.message || 'Unknown error'));
    }
  });

  const regenerateTokenMutation = useMutation({
    mutationFn: (id: string) => api.regenerateEnrollmentToken(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['enrollment-tokens'] });
      toast.success('Token regenerated successfully');
    },
    onError: (error: Error) => {
      toast.error('Failed to regenerate token: ' + (error.message || 'Unknown error'));
    }
  });

  const toggleTokenMutation = useMutation({
    mutationFn: ({ id, isActive }: { id: string; isActive: boolean }) =>
      api.updateEnrollmentToken(id, { isActive }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['enrollment-tokens'] });
      toast.success('Token updated successfully');
    },
    onError: (error: Error) => {
      toast.error('Failed to update token: ' + (error.message || 'Unknown error'));
    }
  });

  const copyToClipboard = async (text: string, fieldId: string) => {
    await navigator.clipboard.writeText(text);
    setCopiedField(fieldId);
    toast.success('Copied to clipboard');
    setTimeout(() => setCopiedField(''), 2000);
  };

  const handleCreateToken = () => {
    const data: { name: string; description?: string; expiresAt?: string; maxUses?: number } = {
      name: createForm.name
    };
    if (createForm.description) data.description = createForm.description;
    if (createForm.expiresAt) data.expiresAt = new Date(createForm.expiresAt).toISOString();
    if (createForm.maxUses) data.maxUses = parseInt(createForm.maxUses, 10);

    createTokenMutation.mutate(data);
  };

  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  const formatDate = (dateString: string): string => {
    return new Date(dateString).toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  };

  const getPlatformIcon = (platform: string) => {
    switch (platform.toLowerCase()) {
      case 'windows':
        return <Monitor className="w-8 h-8" />;
      case 'linux':
        return <Server className="w-8 h-8" />;
      case 'darwin':
      case 'macos':
        return <Apple className="w-8 h-8" />;
      default:
        return <Monitor className="w-8 h-8" />;
    }
  };

  const getPlatformName = (platform: string): string => {
    switch (platform.toLowerCase()) {
      case 'darwin':
        return 'macOS';
      case 'windows':
        return 'Windows';
      case 'linux':
        return 'Linux';
      default:
        return platform;
    }
  };

  const getArchName = (arch: string): string => {
    switch (arch) {
      case 'amd64':
        return 'x64';
      case 'arm64':
        return 'ARM64';
      case '386':
        return 'x86';
      default:
        return arch;
    }
  };

  // Group installers by platform
  const installersByPlatform = installers.reduce((acc: Record<string, AgentInstaller[]>, installer: AgentInstaller) => {
    const platform = getPlatformName(installer.platform);
    if (!acc[platform]) acc[platform] = [];
    acc[platform].push(installer);
    return acc;
  }, {});

  const activeToken = tokens.find((t: EnrollmentToken) => t.id === selectedToken);

  // Generate install scripts
  const getWindowsScript = (token: string): string => {
    const url = api.getAgentScriptUrl('windows', token);
    return "powershell -ExecutionPolicy Bypass -Command \"& { iwr -useb '" + url + "' | iex }\"";
  };

  const getLinuxScript = (token: string): string => {
    const url = api.getAgentScriptUrl('linux', token);
    return "curl -sSL '" + url + "' | sudo bash";
  };

  const getMacScript = (token: string): string => {
    const url = api.getAgentScriptUrl('darwin', token);
    return "curl -sSL '" + url + "' | sudo bash";
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Agent Downloads</h1>
          <p className="text-gray-600 mt-1">
            Download and install the Sentinel agent on your endpoints
          </p>
        </div>
      </div>

      {/* Enrollment Tokens Section - Admin Only */}
      {isAdmin && (
        <div className="bg-white rounded-lg shadow-sm border border-gray-200">
          <div className="px-6 py-4 border-b border-gray-200 flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Key className="w-5 h-5 text-blue-600" />
              <h2 className="text-lg font-semibold text-gray-900">Enrollment Tokens</h2>
            </div>
            <button
              onClick={() => setShowCreateModal(true)}
              className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors"
            >
              <Plus className="w-4 h-4" />
              Create Token
            </button>
          </div>

          <div className="p-6">
            {tokensLoading ? (
              <div className="text-center py-8 text-gray-500">Loading tokens...</div>
            ) : tokens.length === 0 ? (
              <div className="text-center py-8 text-gray-500">
                <Key className="w-12 h-12 mx-auto mb-3 opacity-50" />
                <p>No enrollment tokens yet</p>
                <p className="text-sm mt-1">Create a token to start enrolling agents</p>
              </div>
            ) : (
              <div className="space-y-3">
                {tokens.map((token: EnrollmentToken) => (
                  <div
                    key={token.id}
                    className={"p-4 border rounded-lg cursor-pointer transition-colors " +
                      (selectedToken === token.id
                        ? 'border-blue-500 bg-blue-50'
                        : 'border-gray-200 hover:border-gray-300')
                    }
                    onClick={() => setSelectedToken(selectedToken === token.id ? '' : token.id)}
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-3">
                        <div className={"w-3 h-3 rounded-full " + (token.isActive ? 'bg-green-500' : 'bg-gray-400')} />
                        <div>
                          <h3 className="font-medium text-gray-900">{token.name}</h3>
                          {token.description && (
                            <p className="text-sm text-gray-500">{token.description}</p>
                          )}
                        </div>
                      </div>
                      <div className="flex items-center gap-4">
                        <div className="text-right text-sm">
                          <div className="flex items-center gap-1 text-gray-600">
                            <Hash className="w-4 h-4" />
                            <span>{token.useCount}{token.maxUses ? '/' + token.maxUses : ''} uses</span>
                          </div>
                          {token.expiresAt && (
                            <div className="flex items-center gap-1 text-gray-500">
                              <Clock className="w-4 h-4" />
                              <span>Expires {formatDate(token.expiresAt)}</span>
                            </div>
                          )}
                        </div>
                        <div className="flex items-center gap-2">
                          <button
                            onClick={(e) => {
                              e.stopPropagation();
                              toggleTokenMutation.mutate({ id: token.id, isActive: !token.isActive });
                            }}
                            className="p-2 hover:bg-gray-100 rounded-lg transition-colors"
                            title={token.isActive ? 'Disable token' : 'Enable token'}
                          >
                            {token.isActive ? (
                              <ToggleRight className="w-5 h-5 text-green-600" />
                            ) : (
                              <ToggleLeft className="w-5 h-5 text-gray-400" />
                            )}
                          </button>
                          <button
                            onClick={(e) => {
                              e.stopPropagation();
                              regenerateTokenMutation.mutate(token.id);
                            }}
                            className="p-2 hover:bg-gray-100 rounded-lg transition-colors"
                            title="Regenerate token"
                          >
                            <RefreshCw className="w-5 h-5 text-gray-600" />
                          </button>
                          <button
                            onClick={(e) => {
                              e.stopPropagation();
                              if (confirm('Are you sure you want to delete this token?')) {
                                deleteTokenMutation.mutate(token.id);
                              }
                            }}
                            className="p-2 hover:bg-red-50 rounded-lg transition-colors"
                            title="Delete token"
                          >
                            <Trash2 className="w-5 h-5 text-red-600" />
                          </button>
                        </div>
                      </div>
                    </div>

                    {selectedToken === token.id && (
                      <div className="mt-4 pt-4 border-t border-gray-200">
                        <div className="mb-3">
                          <label className="block text-sm font-medium text-gray-700 mb-1">
                            Token Value
                          </label>
                          <div className="flex items-center gap-2">
                            <code className="flex-1 px-3 py-2 bg-gray-100 rounded-lg text-sm font-mono break-all">
                              {token.token}
                            </code>
                            <button
                              onClick={() => copyToClipboard(token.token, 'token-' + token.id)}
                              className="p-2 hover:bg-gray-100 rounded-lg transition-colors"
                            >
                              {copiedField === 'token-' + token.id ? (
                                <Check className="w-5 h-5 text-green-600" />
                              ) : (
                                <Copy className="w-5 h-5 text-gray-600" />
                              )}
                            </button>
                          </div>
                        </div>
                        <p className="text-sm text-gray-500">
                          Created {formatDate(token.createdAt)}
                        </p>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Quick Install Scripts */}
      {activeToken && (
        <div className="bg-white rounded-lg shadow-sm border border-gray-200">
          <div className="px-6 py-4 border-b border-gray-200">
            <div className="flex items-center gap-2">
              <Terminal className="w-5 h-5 text-blue-600" />
              <h2 className="text-lg font-semibold text-gray-900">One-Line Install Scripts</h2>
            </div>
            <p className="text-sm text-gray-500 mt-1">
              Using token: <strong>{activeToken.name}</strong>
            </p>
          </div>

          <div className="p-6 space-y-4">
            {/* Windows */}
            <div>
              <label className="flex items-center gap-2 text-sm font-medium text-gray-700 mb-2">
                <Monitor className="w-4 h-4" />
                Windows (PowerShell as Administrator)
              </label>
              <div className="flex items-center gap-2">
                <code className="flex-1 px-3 py-2 bg-gray-900 text-green-400 rounded-lg text-sm font-mono overflow-x-auto">
                  {getWindowsScript(activeToken.token)}
                </code>
                <button
                  onClick={() => copyToClipboard(getWindowsScript(activeToken.token), 'script-windows')}
                  className="p-2 bg-gray-100 hover:bg-gray-200 rounded-lg transition-colors"
                >
                  {copiedField === 'script-windows' ? (
                    <Check className="w-5 h-5 text-green-600" />
                  ) : (
                    <Copy className="w-5 h-5 text-gray-600" />
                  )}
                </button>
              </div>
            </div>

            {/* Linux */}
            <div>
              <label className="flex items-center gap-2 text-sm font-medium text-gray-700 mb-2">
                <Server className="w-4 h-4" />
                Linux (Terminal)
              </label>
              <div className="flex items-center gap-2">
                <code className="flex-1 px-3 py-2 bg-gray-900 text-green-400 rounded-lg text-sm font-mono overflow-x-auto">
                  {getLinuxScript(activeToken.token)}
                </code>
                <button
                  onClick={() => copyToClipboard(getLinuxScript(activeToken.token), 'script-linux')}
                  className="p-2 bg-gray-100 hover:bg-gray-200 rounded-lg transition-colors"
                >
                  {copiedField === 'script-linux' ? (
                    <Check className="w-5 h-5 text-green-600" />
                  ) : (
                    <Copy className="w-5 h-5 text-gray-600" />
                  )}
                </button>
              </div>
            </div>

            {/* macOS */}
            <div>
              <label className="flex items-center gap-2 text-sm font-medium text-gray-700 mb-2">
                <Apple className="w-4 h-4" />
                macOS (Terminal)
              </label>
              <div className="flex items-center gap-2">
                <code className="flex-1 px-3 py-2 bg-gray-900 text-green-400 rounded-lg text-sm font-mono overflow-x-auto">
                  {getMacScript(activeToken.token)}
                </code>
                <button
                  onClick={() => copyToClipboard(getMacScript(activeToken.token), 'script-macos')}
                  className="p-2 bg-gray-100 hover:bg-gray-200 rounded-lg transition-colors"
                >
                  {copiedField === 'script-macos' ? (
                    <Check className="w-5 h-5 text-green-600" />
                  ) : (
                    <Copy className="w-5 h-5 text-gray-600" />
                  )}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Agent Installers */}
      <div className="bg-white rounded-lg shadow-sm border border-gray-200">
        <div className="px-6 py-4 border-b border-gray-200">
          <div className="flex items-center gap-2">
            <Download className="w-5 h-5 text-blue-600" />
            <h2 className="text-lg font-semibold text-gray-900">Agent Installers</h2>
          </div>
          <p className="text-sm text-gray-500 mt-1">
            Download the appropriate installer for your platform
          </p>
        </div>

        <div className="p-6">
          {installersLoading ? (
            <div className="text-center py-8 text-gray-500">Loading installers...</div>
          ) : Object.keys(installersByPlatform).length === 0 ? (
            <div className="text-center py-8 text-gray-500">
              <Download className="w-12 h-12 mx-auto mb-3 opacity-50" />
              <p>No installers available</p>
              <p className="text-sm mt-1">Contact your administrator</p>
            </div>
          ) : (
            <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
              {Object.entries(installersByPlatform).map(([platform, platformInstallers]) => (
                <div key={platform} className="border border-gray-200 rounded-lg p-4">
                  <div className="flex items-center gap-3 mb-4">
                    <div className="p-2 bg-gray-100 rounded-lg">
                      {getPlatformIcon(platform)}
                    </div>
                    <h3 className="text-lg font-semibold text-gray-900">{platform}</h3>
                  </div>

                  <div className="space-y-3">
                    {(platformInstallers as AgentInstaller[]).map((installer) => (
                      <div
                        key={installer.platform + '-' + installer.architecture}
                        className="flex items-center justify-between p-3 bg-gray-50 rounded-lg"
                      >
                        <div>
                          <div className="font-medium text-gray-900">
                            {getArchName(installer.architecture)}
                          </div>
                          <div className="text-sm text-gray-500">
                            {'v' + installer.version + ' \u2022 ' + formatBytes(installer.size)}
                          </div>
                        </div>
                        <a
                          href={activeToken
                            ? api.getAgentDownloadUrl(installer.platform, installer.architecture, activeToken.token)
                            : '#'
                          }
                          className={"flex items-center gap-2 px-3 py-2 rounded-lg transition-colors " +
                            (activeToken
                              ? 'bg-blue-600 text-white hover:bg-blue-700'
                              : 'bg-gray-300 text-gray-500 cursor-not-allowed')
                          }
                          onClick={(e) => {
                            if (!activeToken) {
                              e.preventDefault();
                              toast.error('Please select an enrollment token first');
                            }
                          }}
                        >
                          <Download className="w-4 h-4" />
                          Download
                        </a>
                      </div>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}

          {!activeToken && isAdmin && tokens.length > 0 && (
            <div className="mt-4 p-4 bg-yellow-50 border border-yellow-200 rounded-lg">
              <p className="text-yellow-800 text-sm">
                <strong>Note:</strong> Select an enrollment token above to enable downloads and generate install scripts.
              </p>
            </div>
          )}

          {!isAdmin && (
            <div className="mt-4 p-4 bg-blue-50 border border-blue-200 rounded-lg">
              <p className="text-blue-800 text-sm">
                <strong>Note:</strong> Contact your administrator to get an enrollment token for installing agents.
              </p>
            </div>
          )}
        </div>
      </div>

      {/* Create Token Modal */}
      {showCreateModal && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg shadow-xl w-full max-w-md mx-4">
            <div className="px-6 py-4 border-b border-gray-200">
              <h3 className="text-lg font-semibold text-gray-900">Create Enrollment Token</h3>
            </div>
            <div className="p-6 space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Name <span className="text-red-500">*</span>
                </label>
                <input
                  type="text"
                  value={createForm.name}
                  onChange={(e) => setCreateForm({ ...createForm, name: e.target.value })}
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  placeholder="e.g., Production Servers"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Description
                </label>
                <textarea
                  value={createForm.description}
                  onChange={(e) => setCreateForm({ ...createForm, description: e.target.value })}
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  rows={2}
                  placeholder="Optional description"
                />
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">
                    Expires At
                  </label>
                  <input
                    type="datetime-local"
                    value={createForm.expiresAt}
                    onChange={(e) => setCreateForm({ ...createForm, expiresAt: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">
                    Max Uses
                  </label>
                  <input
                    type="number"
                    value={createForm.maxUses}
                    onChange={(e) => setCreateForm({ ...createForm, maxUses: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                    placeholder="Unlimited"
                    min={1}
                  />
                </div>
              </div>
            </div>
            <div className="px-6 py-4 border-t border-gray-200 flex justify-end gap-3">
              <button
                onClick={() => {
                  setShowCreateModal(false);
                  setCreateForm({ name: '', description: '', expiresAt: '', maxUses: '' });
                }}
                className="px-4 py-2 text-gray-700 hover:bg-gray-100 rounded-lg transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleCreateToken}
                disabled={!createForm.name || createTokenMutation.isPending}
                className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {createTokenMutation.isPending ? 'Creating...' : 'Create Token'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
