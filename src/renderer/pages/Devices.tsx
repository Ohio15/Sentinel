import React, { useState, useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import { useDeviceStore, Device } from '../stores/deviceStore';
import { useClientStore } from '../stores/clientStore';
import { api } from '../services/api';
import { server as serverService, agent as agentService } from '../services';
import { format } from 'date-fns';

interface DevicesProps {
  onDeviceSelect: (deviceId: string) => void;
}

interface ServerInfo {
  port: number;
  version?: string;
  environment?: string;
  enrollmentToken?: string;
}

interface AgentLink {
  id: string;
  downloadToken: string;
  installationCode?: string;
  deviceName: string;
  userEmail: string;
  userName?: string;
  createdAt: string;
  createdByName?: string;
  expiresAt: string;
  downloadedAt?: string;
  downloadCount: number;
  agentConnectedAt?: string;
  deviceId?: number;
  status: string;
  emailSentAt?: string;
  emailDeliveryStatus?: string;
  notes?: string;
  downloadUrl?: string;
}

interface LinkStats {
  total: number;
  pending: number;
  downloaded: number;
  installed: number;
  expired: number;
  revoked: number;
  last24Hours: number;
  last7Days: number;
}

interface CreateLinkForm {
  deviceName: string;
  userEmail: string;
  userName: string;
  notes: string;
  expiresInHours: number;
  sendEmail: boolean;
}

const linkStatusConfig: Record<string, { label: string; color: string }> = {
  pending: { label: 'Pending', color: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-300' },
  downloaded: { label: 'Downloaded', color: 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300' },
  installing: { label: 'Installing', color: 'bg-indigo-100 text-indigo-800 dark:bg-indigo-900/30 dark:text-indigo-300' },
  installed: { label: 'Installed', color: 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300' },
  expired: { label: 'Expired', color: 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300' },
  revoked: { label: 'Revoked', color: 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300' },
};

export function Devices({ onDeviceSelect }: DevicesProps) {
  const { devices, loading, deleteDevice, disableDevice, enableDevice, uninstallDevice, forceUpdateDevice, updateDevice } = useDeviceStore();
  const { clients, currentClientId } = useClientStore();
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<string>('all');
  const [typeFilter, setTypeFilter] = useState<string>('all');
  const [osFilter, setOsFilter] = useState<string>('all');

  const getClientName = (clientId?: string) => {
    if (!clientId) return null;
    return clients.find(c => c.id === clientId);
  };
  const [actionMenu, setActionMenu] = useState<string | null>(null);
  const [menuPosition, setMenuPosition] = useState<{ x: number; y: number } | null>(null);
  const [confirmAction, setConfirmAction] = useState<{ deviceId: string; action: 'disable' | 'uninstall' | 'delete' } | null>(null);
  const [forceUpdating, setForceUpdating] = useState<string | null>(null);
  const [actionResult, setActionResult] = useState<{ type: 'success' | 'error'; message: string } | null>(null);
  const [activeTab, setActiveTab] = useState<'devices' | 'installation'>('devices');
  const [installationSubTab, setInstallationSubTab] = useState<'download' | 'links'>('download');
  const [serverInfo, setServerInfo] = useState<ServerInfo | null>(null);
  const [downloadingPlatform, setDownloadingPlatform] = useState<string | null>(null);
  const [downloadResult, setDownloadResult] = useState<{ type: 'success' | 'error'; message: string } | null>(null);
  const [editingName, setEditingName] = useState<string | null>(null);
  const [editNameValue, setEditNameValue] = useState('');

  // Installation Links state
  const [links, setLinks] = useState<AgentLink[]>([]);
  const [linkStats, setLinkStats] = useState<LinkStats | null>(null);
  const [linksLoading, setLinksLoading] = useState(false);
  const [linkFilter, setLinkFilter] = useState('');
  const [linkSearch, setLinkSearch] = useState('');
  const [linkPage, setLinkPage] = useState(1);
  const [linkTotalPages, setLinkTotalPages] = useState(1);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showDetailModal, setShowDetailModal] = useState(false);
  const [selectedLink, setSelectedLink] = useState<AgentLink | null>(null);
  const [linkFormData, setLinkFormData] = useState<CreateLinkForm>({
    deviceName: '',
    userEmail: '',
    userName: '',
    notes: '',
    expiresInHours: 24,
    sendEmail: true,
  });
  const [creatingLink, setCreatingLink] = useState(false);
  const [createLinkResult, setCreateLinkResult] = useState<any>(null);
  const [copiedUrl, setCopiedUrl] = useState<string | null>(null);

  useEffect(() => {
    loadServerInfo();
  }, []);

  // Close action menu when clicking outside
  useEffect(() => {
    const handleClickOutside = () => {
      setActionMenu(null);
      setMenuPosition(null);
    };
    if (actionMenu) {
      document.addEventListener('click', handleClickOutside);
      return () => document.removeEventListener('click', handleClickOutside);
    }
    return undefined;
  }, [actionMenu]);

  // Fetch installation links when on links tab
  useEffect(() => {
    if (activeTab === 'installation' && installationSubTab === 'links') {
      fetchLinks();
      fetchLinkStats();
    }
  }, [activeTab, installationSubTab, linkFilter, linkSearch, linkPage]);

  const loadServerInfo = async () => {
    try {
      const info = await serverService.getInfo();
      setServerInfo(info);
    } catch (error) {
      console.error('Failed to load server info:', error);
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopiedUrl(text);
    setTimeout(() => setCopiedUrl(null), 2000);
  };

  const handleDownload = async (platform: string) => {
    setDownloadingPlatform(platform);
    setDownloadResult(null);

    try {
      const result = await agentService.download(platform);
      // In web mode, open the download URL in a new tab
      if (result.url) {
        window.open(result.url, '_blank');
        setDownloadResult({
          type: 'success',
          message: 'Download started in new tab'
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


  const handleDownloadConfigured = async (platform: string) => {
    setDownloadingPlatform(platform);
    setDownloadResult(null);

    try {
      const result = await agentService.downloadConfigured(platform);
      // In web mode, open the download URL in a new tab
      if (result.url) {
        window.open(result.url, '_blank');
        setDownloadResult({
          type: 'success',
          message: 'Pre-configured installer download started'
        });
      }
    } catch (error: any) {
      setDownloadResult({
        type: 'error',
        message: error.message || 'Failed to generate installer'
      });
    } finally {
      setDownloadingPlatform(null);
    }
  };

  // Installation Links functions
  const fetchLinks = async () => {
    setLinksLoading(true);
    try {
      const response = await api.getAgentLinks({
        status: linkFilter || undefined,
        search: linkSearch || undefined,
        page: linkPage,
        pageSize: 20,
      });
      setLinks((response?.links || []) as AgentLink[]);
      setLinkTotalPages(response?.totalPages || 1);
    } catch (err) {
      console.error('Failed to fetch links:', err);
    } finally {
      setLinksLoading(false);
    }
  };

  const fetchLinkStats = async () => {
    try {
      const data = await api.getAgentLinkStats() as LinkStats;
      setLinkStats(data);
    } catch (err) {
      console.error('Failed to fetch stats:', err);
    }
  };

  const handleCreateLink = async () => {
    if (!linkFormData.deviceName || !linkFormData.userEmail) return;
    setCreatingLink(true);
    try {
      const linkData = {
        deviceName: linkFormData.deviceName,
        userEmail: linkFormData.userEmail,
        userName: linkFormData.userName || undefined,
        notes: linkFormData.notes || undefined,
        expiresInHours: linkFormData.expiresInHours,
        sendEmail: linkFormData.sendEmail,
      };
      const result = await api.createAgentLink(linkData);
      setCreateLinkResult(result);
      fetchLinks();
      fetchLinkStats();
    } catch (err: any) {
      alert(err.response?.data?.error || err.message || 'Failed to create link');
    } finally {
      setCreatingLink(false);
    }
  };

  const handleResendEmail = async (linkId: string) => {
    try {
      await api.resendAgentLinkEmail(linkId);
      alert('Email resent successfully');
      fetchLinks();
    } catch (err) {
      alert('Failed to resend email');
    }
  };

  const handleRevokeLink = async (linkId: string) => {
    if (!confirm('Are you sure you want to revoke this installation link?')) return;
    try {
      await api.revokeAgentLink(linkId);
      fetchLinks();
      fetchLinkStats();
    } catch (err) {
      alert('Failed to revoke link');
    }
  };

  const handleDeleteLink = async (linkId: string) => {
    if (!confirm('Are you sure you want to permanently delete this installation link? This cannot be undone.')) return;
    try {
      await api.deleteAgentLink(linkId);
      fetchLinks();
      fetchLinkStats();
    } catch (err) {
      alert('Failed to delete link');
    }
  };

  const handleViewLinkDetails = async (link: AgentLink) => {
    try {
      const details = await api.getAgentLink(link.id) as AgentLink;
      setSelectedLink(details);
      setShowDetailModal(true);
    } catch (err) {
      console.error('Failed to fetch link details:', err);
    }
  };

  const resetLinkForm = () => {
    setLinkFormData({
      deviceName: '',
      userEmail: '',
      userName: '',
      notes: '',
      expiresInHours: 24,
      sendEmail: true,
    });
    setCreateLinkResult(null);
  };

  // Get unique OS types for the filter dropdown (respecting client filter)
  const uniqueOsTypes = React.useMemo(() => {
    const clientFilteredDevices = devices.filter(d => !currentClientId || d.clientId === currentClientId);
    const osSet = new Set(clientFilteredDevices.map(d => d.osType).filter(Boolean));
    return Array.from(osSet).sort();
  }, [devices, currentClientId]);

  // Get unique device types for filter (respecting client filter)
  const uniqueDeviceTypes = React.useMemo(() => {
    const clientFilteredDevices = devices.filter(d => !currentClientId || d.clientId === currentClientId);
    const typeSet = new Set(clientFilteredDevices.map(d => d.deviceType || 'desktop').filter(Boolean));
    return Array.from(typeSet).sort();
  }, [devices, currentClientId]);

  const filteredDevices = devices.filter(device => {
    const matchesSearch =
      device.hostname.toLowerCase().includes(search.toLowerCase()) ||
      device.displayName?.toLowerCase().includes(search.toLowerCase()) ||
      device.ipAddress.includes(search);

    const matchesStatus = statusFilter === 'all' || device.status === statusFilter;
    const matchesType = typeFilter === 'all' || (device.deviceType || 'desktop') === typeFilter;
    const matchesOs = osFilter === 'all' || device.osType === osFilter;

    // Filter by selected client from the header dropdown
    const matchesClient = !currentClientId || device.clientId === currentClientId;

    return matchesSearch && matchesStatus && matchesType && matchesOs && matchesClient;
  });

  const handleDisable = async (id: string) => {
    try {
      await disableDevice(id);
      setConfirmAction(null);
      setActionMenu(null);
    } catch (error) {
      console.error('Failed to disable device:', error);
    }
  };

  const handleEnable = async (id: string) => {
    try {
      await enableDevice(id);
      setActionMenu(null);
    } catch (error) {
      console.error('Failed to enable device:', error);
    }
  };

  const handleUninstall = async (id: string) => {
    try {
      await uninstallDevice(id);
      setConfirmAction(null);
      setActionMenu(null);
    } catch (error) {
      console.error('Failed to uninstall agent:', error);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deleteDevice(id);
      setConfirmAction(null);
      setActionMenu(null);
      setActionResult({ type: 'success', message: 'Device removed from list.' });
      setTimeout(() => setActionResult(null), 3000);
    } catch (error) {
      console.error('Failed to delete device:', error);
      setActionResult({ type: 'error', message: 'Failed to delete device.' });
      setTimeout(() => setActionResult(null), 5000);
    }
  };

  const handleForceUpdate = async (id: string) => {
    setForceUpdating(id);
    setActionMenu(null);
    setMenuPosition(null);
    try {
      await forceUpdateDevice(id);
      setActionResult({ type: 'success', message: 'Force update command sent. The agent will check for updates shortly.' });
    } catch (error) {
      console.error('Failed to trigger force update:', error);
      setActionResult({ type: 'error', message: 'Failed to send force update command.' });
    } finally {
      setForceUpdating(null);
      setTimeout(() => setActionResult(null), 5000);
    }
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
          {/* Action Result Toast */}
          {actionResult && (
            <div className={`p-4 rounded-lg flex items-center gap-3 ${
              actionResult.type === 'success'
                ? 'bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800'
                : 'bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800'
            }`}>
              {actionResult.type === 'success' ? (
                <CheckIcon className="w-5 h-5 text-green-600 dark:text-green-400" />
              ) : (
                <ErrorIcon className="w-5 h-5 text-red-600 dark:text-red-400" />
              )}
              <span className={`text-sm flex-1 ${
                actionResult.type === 'success' ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'
              }`}>
                {actionResult.message}
              </span>
              <button
                onClick={() => setActionResult(null)}
                className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
              >
                <CloseIcon className="w-4 h-4" />
              </button>
            </div>
          )}

          {/* Force Update Loading Indicator */}
          {forceUpdating && (
            <div className="p-4 rounded-lg flex items-center gap-3 bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800">
              <SpinnerIcon />
              <span className="text-sm text-blue-700 dark:text-blue-300">
                Sending force update command...
              </span>
            </div>
          )}

          {/* Filters */}
          <div className="flex flex-wrap gap-3">
            <div className="flex-1 min-w-[200px]">
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
              className="input w-36"
            >
              <option value="all">All Status</option>
              <option value="online">Online</option>
              <option value="offline">Offline</option>
              <option value="warning">Warning</option>
              <option value="critical">Critical</option>
              <option value="disabled">Disabled</option>
              <option value="uninstalling">Uninstalling</option>
            </select>
            <select
              value={typeFilter}
              onChange={e => setTypeFilter(e.target.value)}
              className="input w-32"
            >
              <option value="all">All Types</option>
              {uniqueDeviceTypes.map(type => (
                <option key={type} value={type}>
                  {type.charAt(0).toUpperCase() + type.slice(1)}
                </option>
              ))}
            </select>
            <select
              value={osFilter}
              onChange={e => setOsFilter(e.target.value)}
              className="input w-32"
            >
              <option value="all">All OS</option>
              {uniqueOsTypes.map(os => (
                <option key={os} value={os}>{os}</option>
              ))}
            </select>
            {(statusFilter !== 'all' || typeFilter !== 'all' || osFilter !== 'all') && (
              <button
                onClick={() => {
                  setStatusFilter('all');
                  setTypeFilter('all');
                  setOsFilter('all');
                }}
                className="btn btn-secondary text-sm"
              >
                Clear Filters
              </button>
            )}
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
            <div className="card flex flex-col max-h-[calc(100vh-280px)]">
              <div className="overflow-auto flex-1 relative">
              <table>
                <thead className="sticky top-0 bg-surface z-10">
                  <tr>
                    <th>Status</th>
                    <th>Custom Name</th>
                    <th>Hostname</th>
                    <th>Type</th>
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
                      <td onClick={e => e.stopPropagation()}>
                        {editingName === device.id ? (
                          <div className="flex items-center gap-1">
                            <input
                              type="text"
                              value={editNameValue}
                              onChange={(e) => setEditNameValue(e.target.value)}
                              className="input py-1 px-2 text-sm w-32"
                              autoFocus
                              onKeyDown={(e) => {
                                if (e.key === 'Enter') {
                                  updateDevice(device.id, { displayName: editNameValue || undefined });
                                  setEditingName(null);
                                } else if (e.key === 'Escape') {
                                  setEditingName(null);
                                }
                              }}
                            />
                            <button
                              onClick={() => {
                                updateDevice(device.id, { displayName: editNameValue || undefined });
                                setEditingName(null);
                              }}
                              className="p-1 text-green-600 hover:text-green-700"
                              title="Save"
                            >
                              <CheckIcon className="w-4 h-4" />
                            </button>
                            <button
                              onClick={() => setEditingName(null)}
                              className="p-1 text-gray-400 hover:text-gray-600"
                              title="Cancel"
                            >
                              <CloseIcon className="w-4 h-4" />
                            </button>
                          </div>
                        ) : (
                          <div
                            className="flex items-center gap-1 group cursor-pointer"
                            onClick={() => {
                              setEditingName(device.id);
                              setEditNameValue(device.displayName || '');
                            }}
                          >
                            <span className="text-sm text-text-primary">
                              {device.displayName || <span className="text-text-secondary italic">Click to set</span>}
                            </span>
                            <EditIcon className="w-3 h-3 text-text-secondary opacity-0 group-hover:opacity-100 transition-opacity" />
                          </div>
                        )}
                      </td>
                      <td>
                        <p className="font-medium text-text-primary">{device.hostname}</p>
                      </td>
                      <td>
                        <DeviceTypeCell device={device} onUpdate={updateDevice} />
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
                      <td onClick={e => e.stopPropagation()} className="relative overflow-visible">
                        {confirmAction?.deviceId === device.id ? (
                          <div className="flex gap-2">
                            <button
                              onClick={() => {
                                if (confirmAction.action === 'disable') handleDisable(device.id);
                                else if (confirmAction.action === 'uninstall') handleUninstall(device.id);
                                else if (confirmAction.action === 'delete') handleDelete(device.id);
                              }}
                              className="btn btn-danger text-xs py-1"
                            >
                              {confirmAction.action === 'delete' ? 'Delete' : 'Confirm'}
                            </button>
                            <button
                              onClick={() => setConfirmAction(null)}
                              className="btn btn-secondary text-xs py-1"
                            >
                              Cancel
                            </button>
                          </div>
                        ) : (
                          <div className="relative">
                            <button
                              onClick={(e) => {
                                e.stopPropagation();
                                if (actionMenu === device.id) {
                                  setActionMenu(null);
                                  setMenuPosition(null);
                                } else {
                                  const rect = e.currentTarget.getBoundingClientRect();
                                  setMenuPosition({ x: rect.right, y: rect.bottom });
                                  setActionMenu(device.id);
                                }
                              }}
                              className="text-text-secondary hover:text-text-primary transition-colors p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-700"
                              title="Device actions"
                            >
                              <MoreIcon className="w-4 h-4" />
                            </button>
                            {actionMenu === device.id && menuPosition && createPortal(
                              <div
                                onClick={(e) => e.stopPropagation()}
                                className="fixed bg-surface border border-border rounded-lg shadow-lg z-[9999] min-w-[160px]"
                                style={{ top: menuPosition.y + 4, left: menuPosition.x - 160 }}
                              >
                                {device.status === 'disabled' ? (
                                  <button
                                    onClick={() => handleEnable(device.id)}
                                    className="w-full text-left px-4 py-2 text-sm hover:bg-gray-100 dark:hover:bg-gray-700 text-success flex items-center gap-2 rounded-t-lg"
                                  >
                                    <EnableIcon className="w-4 h-4" />
                                    Enable Device
                                  </button>
                                ) : (
                                  <button
                                    onClick={() => {
                                      setConfirmAction({ deviceId: device.id, action: 'disable' });
                                      setActionMenu(null);
                                    }}
                                    className="w-full text-left px-4 py-2 text-sm hover:bg-gray-100 dark:hover:bg-gray-700 text-warning flex items-center gap-2 rounded-t-lg"
                                  >
                                    <DisableIcon className="w-4 h-4" />
                                    Disable Device
                                  </button>
                                )}
                                <button
                                  onClick={() => handleForceUpdate(device.id)}
                                  className="w-full text-left px-4 py-2 text-sm hover:bg-gray-100 dark:hover:bg-gray-700 text-primary flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed"
                                  disabled={device.status !== 'online'}
                                  title={device.status !== 'online' ? 'Device must be online to force update' : 'Force the agent to check for updates'}
                                >
                                  <UpdateIcon className="w-4 h-4" />
                                  Force Update
                                </button>
                                <div className="border-t border-border"></div>
                                <button
                                  onClick={() => {
                                    setConfirmAction({ deviceId: device.id, action: 'uninstall' });
                                    setActionMenu(null);
                                  }}
                                  className="w-full text-left px-4 py-2 text-sm hover:bg-gray-100 dark:hover:bg-gray-700 text-danger flex items-center gap-2"
                                  disabled={device.status !== 'online'}
                                  title={device.status !== 'online' ? 'Device must be online to uninstall' : 'Uninstall agent from device'}
                                >
                                  <TrashIcon className="w-4 h-4" />
                                  Uninstall Agent
                                </button>
                                <button
                                  onClick={() => {
                                    setConfirmAction({ deviceId: device.id, action: 'delete' });
                                    setActionMenu(null);
                                  }}
                                  className="w-full text-left px-4 py-2 text-sm hover:bg-gray-100 dark:hover:bg-gray-700 text-danger flex items-center gap-2 rounded-b-lg"
                                  title="Remove device from list (does not uninstall agent)"
                                >
                                  <DeleteIcon className="w-4 h-4" />
                                  Delete Device
                                </button>
                              </div>,
                              document.body
                            )}
                          </div>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              </div>
            </div>
          )}
        </>
      )}

      {activeTab === 'installation' && serverInfo && (
        <div className="space-y-6">
          {/* Installation Sub-Tabs */}
          <div className="flex gap-2">
            <button
              onClick={() => setInstallationSubTab('download')}
              className={`px-4 py-2 text-sm font-medium rounded-lg transition-colors ${
                installationSubTab === 'download'
                  ? 'bg-primary text-white'
                  : 'bg-gray-100 dark:bg-gray-800 text-text-secondary hover:text-text-primary'
              }`}
            >
              Direct Download
            </button>
            <button
              onClick={() => setInstallationSubTab('links')}
              className={`px-4 py-2 text-sm font-medium rounded-lg transition-colors ${
                installationSubTab === 'links'
                  ? 'bg-primary text-white'
                  : 'bg-gray-100 dark:bg-gray-800 text-text-secondary hover:text-text-primary'
              }`}
            >
              Installation Links
            </button>
          </div>

          {installationSubTab === 'download' && (
            <>
          {/* Agent Downloads */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-text-primary mb-4">Download Agent Installer</h2>
            <p className="text-sm text-text-secondary mb-4">
              Download a pre-configured installer with server URL and enrollment token embedded. Just run it!
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
                onClick={() => handleDownloadConfigured('windows')}
                disabled={downloadingPlatform !== null}
                className="flex items-center gap-3 p-4 bg-gray-50 dark:bg-gray-800 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors border border-border disabled:opacity-50 disabled:cursor-not-allowed text-left"
              >
                <WindowsIcon className="w-5 h-5 text-blue-500" />
                <div className="flex-1">
                  <p className="font-medium text-text-primary">Windows</p>
                  <p className="text-xs text-text-secondary">
                    {downloadingPlatform === 'windows' ? 'Downloading...' : 'SentinelAgent-Setup.exe'}
                  </p>
                </div>
                {downloadingPlatform === 'windows' ? <SpinnerIcon /> : <DownloadIcon />}
              </button>
              <button
                onClick={() => handleDownloadConfigured('macos')}
                disabled={downloadingPlatform !== null}
                className="flex items-center gap-3 p-4 bg-gray-50 dark:bg-gray-800 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors border border-border disabled:opacity-50 disabled:cursor-not-allowed text-left"
              >
                <AppleIcon className="w-5 h-5 text-gray-600 dark:text-gray-400" />
                <div className="flex-1">
                  <p className="font-medium text-text-primary">macOS</p>
                  <p className="text-xs text-text-secondary">
                    {downloadingPlatform === 'macos' ? 'Saving...' : 'sentinel-install.sh'}
                  </p>
                </div>
                {downloadingPlatform === 'macos' ? <SpinnerIcon /> : <DownloadIcon />}
              </button>
              <button
                onClick={() => handleDownloadConfigured('linux')}
                disabled={downloadingPlatform !== null}
                className="flex items-center gap-3 p-4 bg-gray-50 dark:bg-gray-800 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors border border-border disabled:opacity-50 disabled:cursor-not-allowed text-left"
              >
                <LinuxIcon className="w-5 h-5 text-orange-500" />
                <div className="flex-1">
                  <p className="font-medium text-text-primary">Linux</p>
                  <p className="text-xs text-text-secondary">
                    {downloadingPlatform === 'linux' ? 'Saving...' : 'sentinel-install.sh'}
                  </p>
                </div>
                {downloadingPlatform === 'linux' ? <SpinnerIcon /> : <DownloadIcon />}
              </button>
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
            </>
          )}

          {installationSubTab === 'links' && (
            <>
              <div className="flex items-center justify-between">
                <div>
                  <h2 className="text-lg font-semibold text-text-primary">Installation Links</h2>
                  <p className="text-sm text-text-secondary">Send installation links to users via email</p>
                </div>
                <button onClick={() => { resetLinkForm(); setShowCreateModal(true); }} className="btn btn-primary flex items-center gap-2">
                  <PlusIcon className="w-4 h-4" />Create Link
                </button>
              </div>

              {linkStats && (
                <div className="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-6 gap-4">
                  <LinkStatCard label="Total Links" value={linkStats.total} />
                  <LinkStatCard label="Pending" value={linkStats.pending} color="yellow" />
                  <LinkStatCard label="Downloaded" value={linkStats.downloaded} color="blue" />
                  <LinkStatCard label="Installed" value={linkStats.installed} color="green" />
                  <LinkStatCard label="Expired" value={linkStats.expired} color="gray" />
                  <LinkStatCard label="Last 24h" value={linkStats.last24Hours} color="indigo" />
                </div>
              )}

              <div className="flex flex-col sm:flex-row gap-4">
                <div className="flex-1">
                  <input type="text" placeholder="Search by device name or email..." value={linkSearch} onChange={(e) => setLinkSearch(e.target.value)} className="input w-full" />
                </div>
                <select value={linkFilter} onChange={(e) => setLinkFilter(e.target.value)} className="input w-40">
                  <option value="">All Status</option>
                  <option value="pending">Pending</option>
                  <option value="downloaded">Downloaded</option>
                  <option value="installed">Installed</option>
                  <option value="expired">Expired</option>
                  <option value="revoked">Revoked</option>
                </select>
              </div>

              <div className="card overflow-hidden flex flex-col max-h-[calc(100vh-400px)]">
                <div className="overflow-auto flex-1">
                  <table>
                    <thead className="sticky top-0 bg-surface z-10"><tr><th>Device</th><th>User</th><th>Status</th><th>Created</th><th>Downloads</th><th className="text-right">Actions</th></tr></thead>
                    <tbody>
                      {linksLoading ? (
                        <tr><td colSpan={6} className="text-center py-12"><SpinnerIcon /></td></tr>
                      ) : links.length === 0 ? (
                        <tr><td colSpan={6} className="text-center py-12 text-text-secondary">No installation links found</td></tr>
                      ) : (
                        links.map((link) => (
                          <tr key={link.id}>
                            <td><div className="font-medium text-text-primary">{link.deviceName}</div>{link.notes && <div className="text-sm text-text-secondary truncate max-w-xs">{link.notes}</div>}</td>
                            <td><div className="text-text-primary">{link.userEmail}</div>{link.userName && <div className="text-sm text-text-secondary">{link.userName}</div>}</td>
                            <td><LinkStatusBadge status={link.status} /></td>
                            <td className="text-sm text-text-secondary">{format(new Date(link.createdAt), 'MMM d, yyyy HH:mm')}</td>
                            <td className="text-sm text-text-secondary">{link.downloadCount}</td>
                            <td>
                              <div className="flex items-center justify-end gap-2">
                                <button onClick={() => handleViewLinkDetails(link)} className="text-text-secondary hover:text-text-primary p-1" title="View Details"><EyeIcon className="w-4 h-4" /></button>
                                {link.status === 'pending' && <button onClick={() => handleResendEmail(link.id)} className="text-text-secondary hover:text-primary p-1" title="Resend Email"><MailIcon className="w-4 h-4" /></button>}
                                {!['installed', 'revoked', 'expired'].includes(link.status) && <button onClick={() => handleRevokeLink(link.id)} className="text-text-secondary hover:text-danger p-1" title="Revoke Link"><BanIcon className="w-4 h-4" /></button>}
                                {['revoked', 'expired'].includes(link.status) && <button onClick={() => handleDeleteLink(link.id)} className="text-text-secondary hover:text-danger p-1" title="Delete Link"><TrashIcon className="w-4 h-4" /></button>}
                              </div>
                            </td>
                          </tr>
                        ))
                      )}
                    </tbody>
                  </table>
                </div>
                {linkTotalPages > 1 && (
                  <div className="px-6 py-3 border-t border-border flex items-center justify-between">
                    <button onClick={() => setLinkPage((p) => Math.max(1, p - 1))} disabled={linkPage === 1} className="btn btn-secondary text-sm">Previous</button>
                    <span className="text-sm text-text-secondary">Page {linkPage} of {linkTotalPages}</span>
                    <button onClick={() => setLinkPage((p) => Math.min(linkTotalPages, p + 1))} disabled={linkPage === linkTotalPages} className="btn btn-secondary text-sm">Next</button>
                  </div>
                )}
              </div>
            </>
          )}
        </div>
      )}

      {/* Create Link Modal */}
      {showCreateModal && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
          <div className="bg-surface rounded-xl shadow-xl max-w-md w-full max-h-[90vh] overflow-y-auto border border-border">
            <div className="p-6">
              {!createLinkResult ? (
                <>
                  <h2 className="text-xl font-bold text-text-primary mb-4">Create Installation Link</h2>
                  <div className="space-y-4">
                    <div><label className="block text-sm font-medium text-text-primary mb-1">Device Name *</label><input type="text" value={linkFormData.deviceName} onChange={(e) => setLinkFormData({ ...linkFormData, deviceName: e.target.value })} placeholder="Johns-Laptop" className="input w-full" /></div>
                    <div><label className="block text-sm font-medium text-text-primary mb-1">User Email *</label><input type="email" value={linkFormData.userEmail} onChange={(e) => setLinkFormData({ ...linkFormData, userEmail: e.target.value })} placeholder="john@company.com" className="input w-full" /></div>
                    <div><label className="block text-sm font-medium text-text-primary mb-1">User Name</label><input type="text" value={linkFormData.userName} onChange={(e) => setLinkFormData({ ...linkFormData, userName: e.target.value })} placeholder="John Smith" className="input w-full" /></div>
                    <div><label className="block text-sm font-medium text-text-primary mb-1">Expiration</label>
                      <select value={linkFormData.expiresInHours} onChange={(e) => setLinkFormData({ ...linkFormData, expiresInHours: Number(e.target.value) })} className="input w-full">
                        <option value={12}>12 hours</option><option value={24}>24 hours</option><option value={48}>48 hours</option><option value={168}>7 days</option><option value={720}>30 days</option>
                      </select>
                    </div>
                    <div><label className="block text-sm font-medium text-text-primary mb-1">Notes (internal)</label><textarea value={linkFormData.notes} onChange={(e) => setLinkFormData({ ...linkFormData, notes: e.target.value })} placeholder="New hire - Marketing dept" rows={2} className="input w-full" /></div>
                    <div className="flex items-center gap-2"><input type="checkbox" id="sendEmail" checked={linkFormData.sendEmail} onChange={(e) => setLinkFormData({ ...linkFormData, sendEmail: e.target.checked })} className="w-4 h-4 text-primary rounded focus:ring-primary" /><label htmlFor="sendEmail" className="text-sm text-text-secondary">Send email notification</label></div>
                  </div>
                  <div className="flex justify-end gap-3 mt-6">
                    <button onClick={() => setShowCreateModal(false)} className="btn btn-secondary">Cancel</button>
                    <button onClick={handleCreateLink} disabled={creatingLink || !linkFormData.deviceName || !linkFormData.userEmail} className="btn btn-primary">{creatingLink ? 'Creating...' : 'Create Link'}</button>
                  </div>
                </>
              ) : (
                <>
                  <div className="text-center mb-6">
                    <div className="w-12 h-12 bg-green-100 dark:bg-green-900/30 rounded-full flex items-center justify-center mx-auto mb-4"><CheckIcon className="w-6 h-6 text-green-600 dark:text-green-400" /></div>
                    <h2 className="text-xl font-bold text-text-primary">Installation Link Created</h2>
                  </div>
                  <div className="space-y-4">
                    <div><label className="block text-sm font-medium text-text-secondary mb-1">Email sent to</label><p className="text-text-primary">{linkFormData.userEmail}</p></div>
                    <div><label className="block text-sm font-medium text-text-secondary mb-1">Link expires</label><p className="text-text-primary">{format(new Date(createLinkResult.expiresAt), 'MMM d, yyyy HH:mm')}</p></div>
                    <div><label className="block text-sm font-medium text-text-secondary mb-1">Installation URL</label><div className="flex gap-2"><input type="text" value={createLinkResult.downloadUrl} readOnly className="input flex-1 text-sm" /><button onClick={() => copyToClipboard(createLinkResult.downloadUrl)} className="btn btn-secondary">{copiedUrl === createLinkResult.downloadUrl ? 'Copied!' : 'Copy'}</button></div></div>
                  </div>
                  <div className="flex justify-end gap-3 mt-6"><button onClick={() => { setShowCreateModal(false); resetLinkForm(); }} className="btn btn-primary">Done</button></div>
                </>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Link Detail Modal */}
      {showDetailModal && selectedLink && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
          <div className="bg-surface rounded-xl shadow-xl max-w-2xl w-full max-h-[90vh] overflow-y-auto border border-border">
            <div className="p-6">
              <div className="flex items-start justify-between mb-6"><h2 className="text-xl font-bold text-text-primary">{selectedLink.deviceName}</h2><button onClick={() => setShowDetailModal(false)} className="text-text-secondary hover:text-text-primary"><CloseIcon className="w-6 h-6" /></button></div>
              <div className="grid grid-cols-2 gap-6">
                <div>
                  <h3 className="font-semibold text-text-primary mb-3">Device Information</h3>
                  <dl className="space-y-2 text-sm">
                    <div><dt className="text-text-secondary">Device Name</dt><dd className="text-text-primary">{selectedLink.deviceName}</dd></div>
                    <div><dt className="text-text-secondary">User Email</dt><dd className="text-text-primary">{selectedLink.userEmail}</dd></div>
                    {selectedLink.userName && <div><dt className="text-text-secondary">User Name</dt><dd className="text-text-primary">{selectedLink.userName}</dd></div>}
                    <div><dt className="text-text-secondary">Status</dt><dd><LinkStatusBadge status={selectedLink.status} /></dd></div>
                  </dl>
                </div>
                <div>
                  <h3 className="font-semibold text-text-primary mb-3">Link Information</h3>
                  <dl className="space-y-2 text-sm">
                    <div><dt className="text-text-secondary">Created</dt><dd className="text-text-primary">{format(new Date(selectedLink.createdAt), 'MMM d, yyyy HH:mm')}</dd></div>
                    {selectedLink.createdByName && <div><dt className="text-text-secondary">Created By</dt><dd className="text-text-primary">{selectedLink.createdByName}</dd></div>}
                    <div><dt className="text-text-secondary">Expires</dt><dd className="text-text-primary">{format(new Date(selectedLink.expiresAt), 'MMM d, yyyy HH:mm')}</dd></div>
                    <div><dt className="text-text-secondary">Download Count</dt><dd className="text-text-primary">{selectedLink.downloadCount}</dd></div>
                    {selectedLink.installationCode && <div><dt className="text-text-secondary">Installation Code</dt><dd><code className="font-mono text-lg font-bold text-indigo-600 bg-indigo-50 dark:bg-indigo-900/30 dark:text-indigo-300 px-3 py-1 rounded">{selectedLink.installationCode}</code></dd></div>}
                  </dl>
                </div>
              </div>
              {selectedLink.downloadUrl && (
                <div className="mt-6"><h3 className="font-semibold text-text-primary mb-2">Download URL</h3><div className="flex gap-2"><input type="text" value={selectedLink.downloadUrl} readOnly className="input flex-1 text-sm" /><button onClick={() => copyToClipboard(selectedLink.downloadUrl!)} className="btn btn-secondary">{copiedUrl === selectedLink.downloadUrl ? 'Copied!' : 'Copy'}</button></div></div>
              )}
              {selectedLink.notes && <div className="mt-6"><h3 className="font-semibold text-text-primary mb-2">Notes</h3><p className="text-text-secondary text-sm">{selectedLink.notes}</p></div>}
              <div className="flex justify-end gap-3 mt-6 pt-6 border-t border-border">
                {selectedLink.status === 'pending' && <button onClick={() => { handleResendEmail(selectedLink.id); setShowDetailModal(false); }} className="btn btn-secondary">Resend Email</button>}
                {!['installed', 'revoked', 'expired'].includes(selectedLink.status) && <button onClick={() => { handleRevokeLink(selectedLink.id); setShowDetailModal(false); }} className="btn btn-danger">Revoke Link</button>}
                <button onClick={() => setShowDetailModal(false)} className="btn btn-secondary">Close</button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function StatusBadge({ status }: { status: Device['status'] }) {
  const styles: Record<string, string> = {
    online: 'badge-success',
    offline: 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300',
    warning: 'badge-warning',
    critical: 'badge-danger',
    disabled: 'bg-gray-200 dark:bg-gray-600 text-gray-500 dark:text-gray-400',
    uninstalling: 'bg-orange-100 dark:bg-orange-900 text-orange-600 dark:text-orange-300',
  };

  return (
    <span className={`badge ${styles[status] || styles.offline}`}>
      {status.charAt(0).toUpperCase() + status.slice(1)}
    </span>
  );
}

function LinkStatusBadge({ status }: { status: string }) {
  const config = linkStatusConfig[status] || { label: status, color: 'bg-gray-100 text-gray-800' };
  return <span className={`px-2 py-1 rounded-full text-xs font-medium ${config.color}`}>{config.label}</span>;
}

function LinkStatCard({ label, value, color = 'primary' }: { label: string; value: number; color?: string }) {
  const colors: Record<string, string> = {
    primary: 'bg-primary-light text-primary',
    yellow: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-300',
    blue: 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300',
    green: 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300',
    gray: 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300',
    indigo: 'bg-indigo-100 text-indigo-800 dark:bg-indigo-900/30 dark:text-indigo-300',
  };
  return (
    <div className="card p-4">
      <div className="text-sm text-text-secondary mb-1">{label}</div>
      <div className={`text-2xl font-bold ${colors[color] || colors.primary} rounded px-2 py-1 inline-block`}>{value}</div>
    </div>
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

// Device type icons and selector
const deviceTypeConfig: Record<string, { icon: string; label: string; color: string }> = {
  desktop: { icon: '🖥️', label: 'Desktop', color: 'text-blue-600' },
  laptop: { icon: '💻', label: 'Laptop', color: 'text-green-600' },
  server: { icon: '🖧', label: 'Server', color: 'text-purple-600' },
  tablet: { icon: '📱', label: 'Tablet', color: 'text-orange-600' },
  virtual: { icon: '☁️', label: 'Virtual', color: 'text-cyan-600' },
};

function DeviceTypeCell({ device, onUpdate }: { device: Device; onUpdate: (id: string, data: { deviceType?: string }) => void }) {
  const [isEditing, setIsEditing] = useState(false);
  const [selectedType, setSelectedType] = useState(device.deviceType || 'desktop');
  const dropdownRef = useRef<HTMLDivElement>(null);

  const currentType = device.deviceType || 'desktop';
  const config = deviceTypeConfig[currentType] || deviceTypeConfig.desktop;

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setIsEditing(false);
      }
    };
    if (isEditing) {
      document.addEventListener('mousedown', handleClickOutside);
    }
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [isEditing]);

  const handleSelect = (type: string) => {
    setSelectedType(type);
    onUpdate(device.id, { deviceType: type });
    setIsEditing(false);
  };

  return (
    <div className="relative" ref={dropdownRef} onClick={e => e.stopPropagation()}>
      <button
        onClick={() => setIsEditing(!isEditing)}
        className="flex items-center gap-1.5 px-2 py-1 rounded hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
        title="Click to change device type"
      >
        <span className="text-sm">{config.icon}</span>
        <span className={`text-xs font-medium ${config.color}`}>{config.label}</span>
      </button>
      {isEditing && (
        <div className="absolute z-50 top-full left-0 mt-1 bg-white dark:bg-gray-800 border border-border rounded-lg shadow-lg py-1 min-w-[120px]">
          {Object.entries(deviceTypeConfig).map(([type, cfg]) => (
            <button
              key={type}
              onClick={() => handleSelect(type)}
              className={`w-full flex items-center gap-2 px-3 py-1.5 text-left hover:bg-gray-100 dark:hover:bg-gray-700 ${
                currentType === type ? 'bg-primary-light dark:bg-primary/20' : ''
              }`}
            >
              <span>{cfg.icon}</span>
              <span className={`text-sm ${cfg.color}`}>{cfg.label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
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

function DeleteIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z" />
    </svg>
  );
}

function MoreIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 5v.01M12 12v.01M12 19v.01M12 6a1 1 0 110-2 1 1 0 010 2zm0 7a1 1 0 110-2 1 1 0 010 2zm0 7a1 1 0 110-2 1 1 0 010 2z" />
    </svg>
  );
}

function DisableIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636" />
    </svg>
  );
}

function EnableIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
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

function PlusIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M12 4v16m8-8H4" />
    </svg>
  );
}

function EyeIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
      <path strokeLinecap="round" strokeLinejoin="round" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
    </svg>
  );
}

function MailIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
    </svg>
  );
}

function BanIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636" />
    </svg>
  );
}

function EditIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" />
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

function UpdateIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
    </svg>
  );
}
