import { useState, useEffect } from 'react';
import { api } from '@/services/api';
import { format } from 'date-fns';

interface AgentLink {
  id: string;
  downloadToken: string;
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
  installationCode?: string;
}

interface InstallationCode {
  id: string;
  code: string;
  deviceName: string;
  status: string;
  createdAt: string;
  expiresAt: string;
  usedAt?: string;
  createdByName?: string;
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

interface CreateCodeForm {
  deviceName: string;
  userName: string;
  notes: string;
  expirationDays: number;
}

const statusConfig: Record<string, { label: string; color: string; icon: string }> = {
  pending: { label: 'Pending', color: 'bg-yellow-100 text-yellow-800', icon: 'clock' },
  downloaded: { label: 'Downloaded', color: 'bg-blue-100 text-blue-800', icon: 'download' },
  installing: { label: 'Installing', color: 'bg-indigo-100 text-indigo-800', icon: 'cog' },
  installed: { label: 'Installed', color: 'bg-green-100 text-green-800', icon: 'check' },
  expired: { label: 'Expired', color: 'bg-gray-100 text-gray-800', icon: 'clock' },
  revoked: { label: 'Revoked', color: 'bg-red-100 text-red-800', icon: 'ban' },
};

export default function AgentInstallations() {
  const [links, setLinks] = useState<AgentLink[]>([]);
  const [codes, setCodes] = useState<InstallationCode[]>([]);
  const [stats, setStats] = useState<LinkStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState('');
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showDetailModal, setShowDetailModal] = useState(false);
  const [selectedLink, setSelectedLink] = useState<AgentLink | null>(null);
  const [createMode, setCreateMode] = useState<'link' | 'code'>('code');
  const [viewMode, setViewMode] = useState<'links' | 'codes'>('links');

  const [formData, setFormData] = useState<CreateLinkForm>({
    deviceName: '',
    userEmail: '',
    userName: '',
    notes: '',
    expiresInHours: 24,
    sendEmail: true,
  });

  const [codeFormData, setCodeFormData] = useState<CreateCodeForm>({
    deviceName: '',
    userName: '',
    notes: '',
    expirationDays: 7,
  });

  const [creating, setCreating] = useState(false);
  const [createResult, setCreateResult] = useState<any>(null);
  const [codeResult, setCodeResult] = useState<any>(null);

  const fetchLinks = async () => {
    setLoading(true);
    try {
      const response = await api?.getAgentLinks({
        status: filter || undefined,
        search: search || undefined,
        page,
        pageSize: 20,
      });
      setLinks((response?.links || []) as AgentLink[]);
      setTotalPages(response?.totalPages || 1);
    } catch (err) {
      console.error('Failed to fetch links:', err);
    } finally {
      setLoading(false);
    }
  };

  const fetchCodes = async () => {
    try {
      const response = await api?.getInstallationCodes();
      setCodes(response?.codes || []);
    } catch (err) {
      console.error('Failed to fetch codes:', err);
    }
  };

  const fetchStats = async () => {
    try {
      const data = await api?.getAgentLinkStats();
      setStats(data as LinkStats);
    } catch (err) {
      console.error('Failed to fetch stats:', err);
    }
  };

  useEffect(() => {
    fetchLinks();
    fetchCodes();
  }, [filter, search, page]);

  useEffect(() => {
    fetchStats();
  }, []);

  const handleCreateLink = async () => {
    if (!formData.deviceName || !formData.userEmail) return;
    setCreating(true);
    try {
      const result = await api?.createAgentLink({
        deviceName: formData.deviceName,
        userEmail: formData.userEmail,
        userName: formData.userName || undefined,
        notes: formData.notes || undefined,
        expiresInHours: formData.expiresInHours,
        sendEmail: formData.sendEmail,
      });
      setCreateResult(result);
      fetchLinks();
      fetchStats();
    } catch (err: any) {
      alert(err.message || 'Failed to create link');
    } finally {
      setCreating(false);
    }
  };

  const handleCreateCode = async () => {
    if (!codeFormData.deviceName) return;
    setCreating(true);
    try {
      const result = await api?.createInstallationCode({
        deviceName: codeFormData.deviceName,
        userName: codeFormData.userName || undefined,
        notes: codeFormData.notes || undefined,
        expirationDays: codeFormData.expirationDays,
      });
      setCodeResult(result);
      fetchCodes();
      fetchStats();
    } catch (err: any) {
      alert(err.message || 'Failed to generate code');
    } finally {
      setCreating(false);
    }
  };

  const handleResendEmail = async (linkId: string) => {
    try {
      await api?.resendAgentLinkEmail(linkId);
      alert('Email resent successfully');
      fetchLinks();
    } catch (err) {
      alert('Failed to resend email');
    }
  };

  const handleRevokeLink = async (linkId: string) => {
    if (!confirm('Are you sure you want to revoke this installation link?')) return;
    try {
      await api?.revokeAgentLink(linkId);
      fetchLinks();
      fetchStats();
    } catch (err) {
      alert('Failed to revoke link');
    }
  };

  const handleViewDetails = async (link: AgentLink) => {
    try {
      const details = await api?.getAgentLink(link.id);
      setSelectedLink(details as AgentLink);
      setShowDetailModal(true);
    } catch (err) {
      console.error('Failed to fetch link details:', err);
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
  };

  const resetForm = () => {
    setFormData({ deviceName: '', userEmail: '', userName: '', notes: '', expiresInHours: 24, sendEmail: true });
    setCodeFormData({ deviceName: '', userName: '', notes: '', expirationDays: 7 });
    setCreateResult(null);
    setCodeResult(null);
  };

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Agent Installations</h1>
          <p className="text-gray-600">Manage installation links and codes</p>
        </div>
        <button onClick={() => { resetForm(); setShowCreateModal(true); }}
          className="inline-flex items-center gap-2 px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors">
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          New Installation
        </button>
      </div>

      {stats && (
        <div className="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-6 gap-4 mb-6">
          <StatCard label="Total Links" value={stats.total} />
          <StatCard label="Pending" value={stats.pending} color="yellow" />
          <StatCard label="Downloaded" value={stats.downloaded} color="blue" />
          <StatCard label="Installed" value={stats.installed} color="green" />
          <StatCard label="Expired" value={stats.expired} color="gray" />
          <StatCard label="Last 24h" value={stats.last24Hours} color="indigo" />
        </div>
      )}

      <div className="flex gap-2 mb-4">
        <button onClick={() => setViewMode('links')}
          className={`px-4 py-2 rounded-lg font-medium transition-colors ${viewMode === 'links' ? 'bg-indigo-600 text-white' : 'bg-gray-100 text-gray-700 hover:bg-gray-200'}`}>
          Email Links
        </button>
        <button onClick={() => setViewMode('codes')}
          className={`px-4 py-2 rounded-lg font-medium transition-colors ${viewMode === 'codes' ? 'bg-indigo-600 text-white' : 'bg-gray-100 text-gray-700 hover:bg-gray-200'}`}>
          Installation Codes
        </button>
      </div>

      <div className="flex flex-col sm:flex-row gap-4 mb-6">
        <div className="flex-1">
          <input type="text" placeholder="Search by device name..."
            value={search} onChange={(e) => setSearch(e.target.value)}
            className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500" />
        </div>
        <select value={filter} onChange={(e) => setFilter(e.target.value)}
          className="px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500">
          <option value="">All Status</option>
          <option value="pending">Pending</option>
          <option value="downloaded">Downloaded</option>
          <option value="installed">Installed</option>
          <option value="expired">Expired</option>
          <option value="revoked">Revoked</option>
        </select>
      </div>

      {viewMode === 'links' && (
        <div className="bg-white rounded-xl shadow-sm border border-gray-200 overflow-hidden flex flex-col max-h-[calc(100vh-320px)]">
          <div className="overflow-auto flex-1">
            <table className="w-full">
              <thead className="bg-gray-50 sticky top-0 z-10">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Device</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">User</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Status</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Created</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Downloads</th>
                  <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {loading ? (
                  <tr><td colSpan={6} className="px-6 py-12 text-center text-gray-500">
                    <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600 mx-auto"></div>
                  </td></tr>
                ) : links.length === 0 ? (
                  <tr><td colSpan={6} className="px-6 py-12 text-center text-gray-500">No installation links found</td></tr>
                ) : links.map((link) => (
                  <tr key={link.id} className="hover:bg-gray-50">
                    <td className="px-6 py-4">
                      <div className="font-medium text-gray-900">{link.deviceName}</div>
                      {link.notes && <div className="text-sm text-gray-500 truncate max-w-xs">{link.notes}</div>}
                    </td>
                    <td className="px-6 py-4">
                      <div className="text-gray-900">{link.userEmail || '-'}</div>
                      {link.userName && <div className="text-sm text-gray-500">{link.userName}</div>}
                    </td>
                    <td className="px-6 py-4"><StatusBadge status={link.status} /></td>
                    <td className="px-6 py-4 text-sm text-gray-500">{format(new Date(link.createdAt), 'MMM d, yyyy HH:mm')}</td>
                    <td className="px-6 py-4 text-sm text-gray-500">{link.downloadCount}</td>
                    <td className="px-6 py-4 text-right">
                      <div className="flex items-center justify-end gap-2">
                        <button onClick={() => handleViewDetails(link)} className="text-gray-400 hover:text-gray-600" title="View Details">
                          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                          </svg>
                        </button>
                        {link.status === 'pending' && link.userEmail && (
                          <button onClick={() => handleResendEmail(link.id)} className="text-gray-400 hover:text-indigo-600" title="Resend Email">
                            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
                            </svg>
                          </button>
                        )}
                        {!['installed', 'revoked', 'expired'].includes(link.status) && (
                          <button onClick={() => handleRevokeLink(link.id)} className="text-gray-400 hover:text-red-600" title="Revoke Link">
                            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636" />
                            </svg>
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          </div>
          {totalPages > 1 && (
            <div className="px-6 py-3 border-t border-gray-200 flex items-center justify-between">
              <button onClick={() => setPage((p) => Math.max(1, p - 1))} disabled={page === 1}
                className="px-3 py-1 border border-gray-300 rounded text-sm disabled:opacity-50">Previous</button>
              <span className="text-sm text-gray-600">Page {page} of {totalPages}</span>
              <button onClick={() => setPage((p) => Math.min(totalPages, p + 1))} disabled={page === totalPages}
                className="px-3 py-1 border border-gray-300 rounded text-sm disabled:opacity-50">Next</button>
            </div>
          )}
        </div>
      )}

      {viewMode === 'codes' && (
        <div className="bg-white rounded-xl shadow-sm border border-gray-200 overflow-hidden flex flex-col max-h-[calc(100vh-320px)]">
          <div className="overflow-auto flex-1">
            <table className="w-full">
              <thead className="bg-gray-50 sticky top-0 z-10">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Code</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Device Name</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Status</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Created</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Expires</th>
                  <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {codes.length === 0 ? (
                  <tr><td colSpan={6} className="px-6 py-12 text-center text-gray-500">No installation codes found. Generate one to get started.</td></tr>
                ) : codes.map((code) => (
                  <tr key={code.id} className="hover:bg-gray-50">
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-2">
                        <code className="font-mono text-lg font-bold text-indigo-600 bg-indigo-50 px-3 py-1 rounded">{code.code}</code>
                        <button onClick={() => copyToClipboard(code.code)} className="text-gray-400 hover:text-gray-600" title="Copy Code">
                          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                          </svg>
                        </button>
                      </div>
                    </td>
                    <td className="px-6 py-4"><div className="font-medium text-gray-900">{code.deviceName}</div></td>
                    <td className="px-6 py-4"><StatusBadge status={code.status} /></td>
                    <td className="px-6 py-4 text-sm text-gray-500">{format(new Date(code.createdAt), 'MMM d, yyyy HH:mm')}</td>
                    <td className="px-6 py-4 text-sm text-gray-500">{format(new Date(code.expiresAt), 'MMM d, yyyy')}</td>
                    <td className="px-6 py-4 text-right">
                      <button onClick={() => copyToClipboard(code.code)} className="text-indigo-600 hover:text-indigo-800 text-sm font-medium">Copy Code</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          </div>
        </div>
      )}

      {showCreateModal && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
          <div className="bg-white rounded-xl shadow-xl max-w-md w-full max-h-[90vh] overflow-y-auto">
            <div className="p-6">
              {!createResult && !codeResult ? (
                <>
                  <h2 className="text-xl font-bold text-gray-900 mb-4">New Installation</h2>
                  <div className="flex gap-2 mb-6">
                    <button onClick={() => setCreateMode('code')}
                      className={`flex-1 py-2 px-4 rounded-lg font-medium text-sm transition-colors ${createMode === 'code' ? 'bg-indigo-600 text-white' : 'bg-gray-100 text-gray-700 hover:bg-gray-200'}`}>
                      Installation Code
                    </button>
                    <button onClick={() => setCreateMode('link')}
                      className={`flex-1 py-2 px-4 rounded-lg font-medium text-sm transition-colors ${createMode === 'link' ? 'bg-indigo-600 text-white' : 'bg-gray-100 text-gray-700 hover:bg-gray-200'}`}>
                      Email Link
                    </button>
                  </div>

                  {createMode === 'code' ? (
                    <div className="space-y-4">
                      <div className="bg-indigo-50 border border-indigo-100 rounded-lg p-4 text-sm text-indigo-800">
                        Generate a code that users enter during installation. No email required.
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">Device Name *</label>
                        <input type="text" value={codeFormData.deviceName}
                          onChange={(e) => setCodeFormData({ ...codeFormData, deviceName: e.target.value })}
                          placeholder="Johns-Laptop"
                          className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500" />
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">User Name (optional)</label>
                        <input type="text" value={codeFormData.userName}
                          onChange={(e) => setCodeFormData({ ...codeFormData, userName: e.target.value })}
                          placeholder="John Smith"
                          className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500" />
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">Code Expiration</label>
                        <select value={codeFormData.expirationDays}
                          onChange={(e) => setCodeFormData({ ...codeFormData, expirationDays: Number(e.target.value) })}
                          className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500">
                          <option value={1}>1 day</option>
                          <option value={3}>3 days</option>
                          <option value={7}>7 days</option>
                          <option value={14}>14 days</option>
                          <option value={30}>30 days</option>
                        </select>
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">Notes (internal)</label>
                        <textarea value={codeFormData.notes}
                          onChange={(e) => setCodeFormData({ ...codeFormData, notes: e.target.value })}
                          placeholder="New hire - Marketing dept" rows={2}
                          className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500" />
                      </div>
                    </div>
                  ) : (
                    <div className="space-y-4">
                      <div className="bg-blue-50 border border-blue-100 rounded-lg p-4 text-sm text-blue-800">
                        Send an installation link via email with embedded configuration.
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">Device Name *</label>
                        <input type="text" value={formData.deviceName}
                          onChange={(e) => setFormData({ ...formData, deviceName: e.target.value })}
                          placeholder="Johns-Laptop"
                          className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500" />
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">User Email *</label>
                        <input type="email" value={formData.userEmail}
                          onChange={(e) => setFormData({ ...formData, userEmail: e.target.value })}
                          placeholder="john@company.com"
                          className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500" />
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">User Name</label>
                        <input type="text" value={formData.userName}
                          onChange={(e) => setFormData({ ...formData, userName: e.target.value })}
                          placeholder="John Smith"
                          className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500" />
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">Expiration</label>
                        <select value={formData.expiresInHours}
                          onChange={(e) => setFormData({ ...formData, expiresInHours: Number(e.target.value) })}
                          className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500">
                          <option value={12}>12 hours</option>
                          <option value={24}>24 hours</option>
                          <option value={48}>48 hours</option>
                          <option value={168}>7 days</option>
                          <option value={720}>30 days</option>
                        </select>
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">Notes (internal)</label>
                        <textarea value={formData.notes}
                          onChange={(e) => setFormData({ ...formData, notes: e.target.value })}
                          placeholder="New hire - Marketing dept" rows={2}
                          className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500" />
                      </div>
                      <div className="flex items-center gap-2">
                        <input type="checkbox" id="sendEmail" checked={formData.sendEmail}
                          onChange={(e) => setFormData({ ...formData, sendEmail: e.target.checked })}
                          className="w-4 h-4 text-indigo-600 rounded focus:ring-indigo-500" />
                        <label htmlFor="sendEmail" className="text-sm text-gray-700">Send email notification</label>
                      </div>
                    </div>
                  )}

                  <div className="flex justify-end gap-3 mt-6">
                    <button onClick={() => setShowCreateModal(false)} className="px-4 py-2 text-gray-700 hover:bg-gray-100 rounded-lg">Cancel</button>
                    <button onClick={createMode === 'code' ? handleCreateCode : handleCreateLink}
                      disabled={creating || (createMode === 'code' ? !codeFormData.deviceName : !formData.deviceName || !formData.userEmail)}
                      className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 disabled:bg-indigo-300">
                      {creating ? 'Creating...' : createMode === 'code' ? 'Generate Code' : 'Create Link'}
                    </button>
                  </div>
                </>
              ) : codeResult ? (
                <>
                  <div className="text-center mb-6">
                    <div className="w-12 h-12 bg-green-100 rounded-full flex items-center justify-center mx-auto mb-4">
                      <svg className="w-6 h-6 text-green-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                      </svg>
                    </div>
                    <h2 className="text-xl font-bold text-gray-900">Installation Code Generated</h2>
                  </div>
                  <div className="space-y-4">
                    <div className="bg-indigo-50 border border-indigo-200 rounded-xl p-6 text-center">
                      <div className="text-sm text-indigo-600 mb-2">Installation Code</div>
                      <div className="flex items-center justify-center gap-3">
                        <code className="font-mono text-3xl font-bold text-indigo-700 tracking-wider">{codeResult.code}</code>
                        <button onClick={() => copyToClipboard(codeResult.code)}
                          className="p-2 bg-indigo-100 hover:bg-indigo-200 rounded-lg text-indigo-600" title="Copy Code">
                          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                          </svg>
                        </button>
                      </div>
                    </div>
                    <div>
                      <label className="block text-sm font-medium text-gray-500 mb-1">Device Name</label>
                      <p className="text-gray-900">{codeResult.deviceName}</p>
                    </div>
                    <div>
                      <label className="block text-sm font-medium text-gray-500 mb-1">Code Expires</label>
                      <p className="text-gray-900">{format(new Date(codeResult.expiresAt), 'MMM d, yyyy HH:mm')}</p>
                    </div>
                    <div>
                      <label className="block text-sm font-medium text-gray-500 mb-1">Download URL</label>
                      <div className="flex gap-2">
                        <input type="text" value={codeResult.downloadUrl} readOnly
                          className="flex-1 px-3 py-2 bg-gray-50 border border-gray-300 rounded-lg text-sm" />
                        <button onClick={() => copyToClipboard(codeResult.downloadUrl)}
                          className="px-3 py-2 bg-gray-100 hover:bg-gray-200 rounded-lg" title="Copy URL">
                          <svg className="w-5 h-5 text-gray-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                          </svg>
                        </button>
                      </div>
                    </div>
                    <div className="bg-gray-50 rounded-lg p-4 text-sm text-gray-600">
                      <div className="font-medium text-gray-900 mb-2">Instructions:</div>
                      <ol className="list-decimal list-inside space-y-1">
                        <li>Download the installer from the URL above</li>
                        <li>Run the installer on the target device</li>
                        <li>Enter the code <code className="font-mono bg-white px-1 rounded">{codeResult.code}</code> when prompted</li>
                      </ol>
                    </div>
                  </div>
                  <div className="flex justify-end gap-3 mt-6">
                    <button onClick={() => { setShowCreateModal(false); resetForm(); }}
                      className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700">Done</button>
                  </div>
                </>
              ) : (
                <>
                  <div className="text-center mb-6">
                    <div className="w-12 h-12 bg-green-100 rounded-full flex items-center justify-center mx-auto mb-4">
                      <svg className="w-6 h-6 text-green-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                      </svg>
                    </div>
                    <h2 className="text-xl font-bold text-gray-900">Installation Link Created</h2>
                  </div>
                  <div className="space-y-4">
                    <div>
                      <label className="block text-sm font-medium text-gray-500 mb-1">Email sent to</label>
                      <p className="text-gray-900">{formData.userEmail}</p>
                    </div>
                    <div>
                      <label className="block text-sm font-medium text-gray-500 mb-1">Link expires</label>
                      <p className="text-gray-900">{format(new Date(createResult.expiresAt), 'MMM d, yyyy HH:mm')}</p>
                    </div>
                    <div>
                      <label className="block text-sm font-medium text-gray-500 mb-1">Installation URL</label>
                      <div className="flex gap-2">
                        <input type="text" value={createResult.downloadUrl} readOnly
                          className="flex-1 px-3 py-2 bg-gray-50 border border-gray-300 rounded-lg text-sm" />
                        <button onClick={() => copyToClipboard(createResult.downloadUrl)}
                          className="px-3 py-2 bg-gray-100 hover:bg-gray-200 rounded-lg">
                          <svg className="w-5 h-5 text-gray-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                          </svg>
                        </button>
                      </div>
                    </div>
                  </div>
                  <div className="flex justify-end gap-3 mt-6">
                    <button onClick={() => { setShowCreateModal(false); resetForm(); }}
                      className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700">Done</button>
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      )}

      {showDetailModal && selectedLink && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
          <div className="bg-white rounded-xl shadow-xl max-w-2xl w-full max-h-[90vh] overflow-y-auto">
            <div className="p-6">
              <div className="flex items-start justify-between mb-6">
                <h2 className="text-xl font-bold text-gray-900">{selectedLink.deviceName}</h2>
                <button onClick={() => setShowDetailModal(false)} className="text-gray-400 hover:text-gray-600">
                  <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
              <div className="grid grid-cols-2 gap-6">
                <div>
                  <h3 className="font-semibold text-gray-900 mb-3">Device Information</h3>
                  <dl className="space-y-2 text-sm">
                    <div><dt className="text-gray-500">Device Name</dt><dd className="text-gray-900">{selectedLink.deviceName}</dd></div>
                    {selectedLink.userEmail && <div><dt className="text-gray-500">User Email</dt><dd className="text-gray-900">{selectedLink.userEmail}</dd></div>}
                    {selectedLink.userName && <div><dt className="text-gray-500">User Name</dt><dd className="text-gray-900">{selectedLink.userName}</dd></div>}
                    <div><dt className="text-gray-500">Status</dt><dd><StatusBadge status={selectedLink.status} /></dd></div>
                  </dl>
                </div>
                <div>
                  <h3 className="font-semibold text-gray-900 mb-3">Link Information</h3>
                  <dl className="space-y-2 text-sm">
                    <div><dt className="text-gray-500">Created</dt><dd className="text-gray-900">{format(new Date(selectedLink.createdAt), 'MMM d, yyyy HH:mm')}</dd></div>
                    {selectedLink.createdByName && <div><dt className="text-gray-500">Created By</dt><dd className="text-gray-900">{selectedLink.createdByName}</dd></div>}
                    <div><dt className="text-gray-500">Expires</dt><dd className="text-gray-900">{format(new Date(selectedLink.expiresAt), 'MMM d, yyyy HH:mm')}</dd></div>
                    <div><dt className="text-gray-500">Download Count</dt><dd className="text-gray-900">{selectedLink.downloadCount}</dd></div>
                  </dl>
                </div>
              </div>
              {selectedLink.installationCode && (
                <div className="mt-6">
                  <h3 className="font-semibold text-gray-900 mb-2">Installation Code</h3>
                  <code className="font-mono text-lg font-bold text-indigo-600 bg-indigo-50 px-3 py-1 rounded">{selectedLink.installationCode}</code>
                </div>
              )}
              {selectedLink.downloadUrl && (
                <div className="mt-6">
                  <h3 className="font-semibold text-gray-900 mb-2">Download URL</h3>
                  <div className="flex gap-2">
                    <input type="text" value={selectedLink.downloadUrl} readOnly className="flex-1 px-3 py-2 bg-gray-50 border border-gray-300 rounded-lg text-sm" />
                    <button onClick={() => copyToClipboard(selectedLink.downloadUrl!)} className="px-3 py-2 bg-gray-100 hover:bg-gray-200 rounded-lg">Copy</button>
                  </div>
                </div>
              )}
              {selectedLink.notes && (
                <div className="mt-6">
                  <h3 className="font-semibold text-gray-900 mb-2">Notes</h3>
                  <p className="text-gray-600 text-sm">{selectedLink.notes}</p>
                </div>
              )}
              <div className="flex justify-end gap-3 mt-6 pt-6 border-t">
                {selectedLink.status === 'pending' && selectedLink.userEmail && (
                  <button onClick={() => { handleResendEmail(selectedLink.id); setShowDetailModal(false); }}
                    className="px-4 py-2 text-indigo-600 hover:bg-indigo-50 rounded-lg">Resend Email</button>
                )}
                {!['installed', 'revoked', 'expired'].includes(selectedLink.status) && (
                  <button onClick={() => { handleRevokeLink(selectedLink.id); setShowDetailModal(false); }}
                    className="px-4 py-2 text-red-600 hover:bg-red-50 rounded-lg">Revoke Link</button>
                )}
                <button onClick={() => setShowDetailModal(false)} className="px-4 py-2 bg-gray-100 hover:bg-gray-200 rounded-lg">Close</button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function StatCard({ label, value, color = 'indigo' }: { label: string; value: number; color?: string }) {
  const colors: Record<string, string> = {
    indigo: 'bg-indigo-50 text-indigo-600',
    green: 'bg-green-50 text-green-600',
    yellow: 'bg-yellow-50 text-yellow-600',
    blue: 'bg-blue-50 text-blue-600',
    gray: 'bg-gray-50 text-gray-600',
    red: 'bg-red-50 text-red-600',
  };
  return (
    <div className={`p-4 rounded-xl ${colors[color]}`}>
      <div className="text-2xl font-bold">{value}</div>
      <div className="text-sm opacity-80">{label}</div>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const config = statusConfig[status] || { label: status, color: 'bg-gray-100 text-gray-800' };
  return (
    <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${config.color}`}>
      {config.label}
    </span>
  );
}
