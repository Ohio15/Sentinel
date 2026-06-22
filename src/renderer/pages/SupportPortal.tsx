import { useState, useEffect, useCallback } from 'react';
import { useSearchParams } from 'react-router-dom';
import { getApiBaseUrl } from '@/services/env';

interface PortalTicket {
  id: string;
  ticketNumber: number;
  subject: string;
  description?: string;
  status: string;
  priority: string;
  createdAt: string;
  updatedAt: string;
}

interface PortalComment {
  id: string;
  content: string;
  isInternal: boolean;
  authorName: string;
  createdAt: string;
}

interface UserInfo {
  id: string;
  email: string;
  firstName?: string;
  lastName?: string;
  role: string;
}

/**
 * Portal API helper — uses the JWT token from the URL, not localStorage.
 */
function createPortalApi(token: string) {
  const baseUrl = getApiBaseUrl();

  async function request<T>(method: string, endpoint: string, data?: unknown): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
    };

    const response = await fetch(`${baseUrl}${endpoint}`, {
      method,
      headers,
      credentials: 'include',
      body: data ? JSON.stringify(data) : undefined,
    });

    if (!response.ok) {
      const err = await response.json().catch(() => ({ message: 'Request failed' }));
      throw new Error(err.message || `HTTP ${response.status}`);
    }

    const text = await response.text();
    return (text ? JSON.parse(text) : null) as T;
  }

  return {
    getMe: () => request<UserInfo>('GET', '/auth/me'),
    getTickets: () => request<{ tickets: PortalTicket[]; total: number } | PortalTicket[]>('GET', '/tickets'),
    getTicket: (id: string) => request<PortalTicket>('GET', `/tickets/${id}`),
    getComments: (id: string) => request<{ comments?: PortalComment[] } | PortalComment[]>('GET', `/tickets/${id}/comments`),
    createTicket: (data: { subject: string; description: string; priority: string }) =>
      request<PortalTicket>('POST', '/tickets', data),
    createComment: (ticketId: string, content: string) =>
      request<PortalComment>('POST', `/tickets/${ticketId}/comments`, { content, isInternal: false }),
  };
}

const statusColors: Record<string, string> = {
  open: 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300',
  in_progress: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-300',
  waiting: 'bg-orange-100 text-orange-800 dark:bg-orange-900/30 dark:text-orange-300',
  resolved: 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300',
  closed: 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300',
};

const priorityColors: Record<string, string> = {
  low: 'text-gray-600 dark:text-gray-400',
  medium: 'text-blue-600 dark:text-blue-400',
  high: 'text-orange-600 dark:text-orange-400',
  critical: 'text-red-600 dark:text-red-400',
};

export default function SupportPortal() {
  const [searchParams] = useSearchParams();
  const token = searchParams.get('token');

  const [user, setUser] = useState<UserInfo | null>(null);
  const [tickets, setTickets] = useState<PortalTicket[]>([]);
  const [selectedTicket, setSelectedTicket] = useState<PortalTicket | null>(null);
  const [comments, setComments] = useState<PortalComment[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreateForm, setShowCreateForm] = useState(false);
  const [newComment, setNewComment] = useState('');
  const [submitting, setSubmitting] = useState(false);

  // Create form state
  const [newSubject, setNewSubject] = useState('');
  const [newDescription, setNewDescription] = useState('');
  const [newPriority, setNewPriority] = useState('medium');

  const api = token ? createPortalApi(token) : null;

  const fetchTickets = useCallback(async () => {
    if (!api) return;
    try {
      const result = await api.getTickets();
      const list = Array.isArray(result) ? result : (result as { tickets: PortalTicket[] }).tickets || [];
      setTickets(list);
    } catch (err) {
      console.error('Failed to fetch tickets:', err);
    }
  }, [api]);

  const fetchComments = useCallback(async (ticketId: string) => {
    if (!api) return;
    try {
      const result = await api.getComments(ticketId);
      const list = Array.isArray(result) ? result : (result as { comments?: PortalComment[] }).comments || [];
      setComments(list.filter((c: PortalComment) => !c.isInternal));
    } catch (err) {
      console.error('Failed to fetch comments:', err);
    }
  }, [api]);

  useEffect(() => {
    if (!token || !api) {
      setError('Invalid or missing authentication token.');
      setLoading(false);
      return;
    }

    void (async () => {
      try {
        const me = await api.getMe();
        setUser(me);
        await fetchTickets();
      } catch {
        setError('Authentication failed. The token may be expired or invalid.');
      } finally {
        setLoading(false);
      }
    })();
  }, [token]);

  const handleSelectTicket = async (ticket: PortalTicket) => {
    setSelectedTicket(ticket);
    setShowCreateForm(false);
    await fetchComments(ticket.id);
  };

  const handleCreateTicket = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!api || !newSubject.trim() || !newDescription.trim()) return;
    setSubmitting(true);
    try {
      await api.createTicket({
        subject: newSubject.trim(),
        description: newDescription.trim(),
        priority: newPriority,
      });
      setNewSubject('');
      setNewDescription('');
      setNewPriority('medium');
      setShowCreateForm(false);
      await fetchTickets();
    } catch (err) {
      console.error('Failed to create ticket:', err);
    } finally {
      setSubmitting(false);
    }
  };

  const handleAddComment = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!api || !selectedTicket || !newComment.trim()) return;
    setSubmitting(true);
    try {
      await api.createComment(selectedTicket.id, newComment.trim());
      setNewComment('');
      await fetchComments(selectedTicket.id);
    } catch (err) {
      console.error('Failed to add comment:', err);
    } finally {
      setSubmitting(false);
    }
  };

  // Error / loading states
  if (loading) {
    return (
      <div className="min-h-screen bg-slate-50 dark:bg-slate-900 flex items-center justify-center">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600" />
      </div>
    );
  }

  if (error || !token) {
    return (
      <div className="min-h-screen bg-slate-50 dark:bg-slate-900 flex items-center justify-center p-4">
        <div className="bg-white dark:bg-slate-800 rounded-xl shadow-lg p-8 max-w-md w-full text-center">
          <div className="w-16 h-16 bg-red-100 dark:bg-red-900/30 rounded-full flex items-center justify-center mx-auto mb-4">
            <svg className="w-8 h-8 text-red-600 dark:text-red-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.082 16.5c-.77.833.192 2.5 1.732 2.5z" />
            </svg>
          </div>
          <h2 className="text-xl font-semibold text-slate-900 dark:text-white mb-2">Access Denied</h2>
          <p className="text-slate-600 dark:text-slate-400">{error || 'No authentication token provided.'}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-slate-50 dark:bg-slate-900">
      {/* Header */}
      <header className="bg-white dark:bg-slate-800 border-b border-slate-200 dark:border-slate-700 px-6 py-4">
        <div className="max-w-6xl mx-auto flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="w-8 h-8 bg-blue-600 rounded-lg flex items-center justify-center">
              <svg className="w-5 h-5 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M18.364 5.636l-3.536 3.536m0 5.656l3.536 3.536M9.172 9.172L5.636 5.636m3.536 9.192l-3.536 3.536M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-5 0a4 4 0 11-8 0 4 4 0 018 0z" />
              </svg>
            </div>
            <h1 className="text-lg font-semibold text-slate-900 dark:text-white">Sentinel Support Portal</h1>
          </div>
          {user && (
            <span className="text-sm text-slate-500 dark:text-slate-400">
              {user.firstName ? `${user.firstName} ${user.lastName || ''}`.trim() : user.email}
            </span>
          )}
        </div>
      </header>

      {/* Main Content */}
      <div className="max-w-6xl mx-auto px-6 py-8">
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-2xl font-bold text-slate-900 dark:text-white">
            {selectedTicket ? `Ticket #${selectedTicket.ticketNumber}` : 'Support Tickets'}
          </h2>
          <div className="flex gap-2">
            {selectedTicket && (
              <button
                onClick={() => { setSelectedTicket(null); setComments([]); }}
                className="px-4 py-2 text-sm font-medium text-slate-700 dark:text-slate-300 bg-white dark:bg-slate-800 border border-slate-300 dark:border-slate-600 rounded-lg hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors"
              >
                Back to Tickets
              </button>
            )}
            {!selectedTicket && !showCreateForm && (
              <button
                onClick={() => setShowCreateForm(true)}
                className="px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 transition-colors"
              >
                New Ticket
              </button>
            )}
          </div>
        </div>

        {/* Create Ticket Form */}
        {showCreateForm && !selectedTicket && (
          <div className="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-6 mb-6">
            <h3 className="text-lg font-semibold text-slate-900 dark:text-white mb-4">Create Support Ticket</h3>
            <form onSubmit={(e) => { void handleCreateTicket(e); }} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1">Subject</label>
                <input
                  type="text"
                  value={newSubject}
                  onChange={(e) => setNewSubject(e.target.value)}
                  placeholder="Brief description of your issue"
                  className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-900 text-slate-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                  required
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1">Priority</label>
                <select
                  value={newPriority}
                  onChange={(e) => setNewPriority(e.target.value)}
                  className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-900 text-slate-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                >
                  <option value="low">Low</option>
                  <option value="medium">Medium</option>
                  <option value="high">High</option>
                  <option value="critical">Critical</option>
                </select>
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1">Description</label>
                <textarea
                  value={newDescription}
                  onChange={(e) => setNewDescription(e.target.value)}
                  placeholder="Describe your issue in detail..."
                  rows={5}
                  className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-900 text-slate-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 resize-vertical"
                  required
                />
              </div>
              <div className="flex gap-2 justify-end">
                <button
                  type="button"
                  onClick={() => setShowCreateForm(false)}
                  className="px-4 py-2 text-sm font-medium text-slate-700 dark:text-slate-300 bg-white dark:bg-slate-800 border border-slate-300 dark:border-slate-600 rounded-lg hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={submitting}
                  className="px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition-colors"
                >
                  {submitting ? 'Creating...' : 'Create Ticket'}
                </button>
              </div>
            </form>
          </div>
        )}

        {/* Ticket Detail View */}
        {selectedTicket && (
          <div className="space-y-6">
            <div className="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-6">
              <div className="flex items-start justify-between mb-4">
                <div>
                  <h3 className="text-lg font-semibold text-slate-900 dark:text-white">{selectedTicket.subject}</h3>
                  <div className="flex items-center gap-3 mt-2">
                    <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${statusColors[selectedTicket.status] || statusColors.open}`}>
                      {selectedTicket.status.replace('_', ' ')}
                    </span>
                    <span className={`text-sm font-medium ${priorityColors[selectedTicket.priority] || ''}`}>
                      {selectedTicket.priority} priority
                    </span>
                    <span className="text-sm text-slate-500 dark:text-slate-400">
                      Created {new Date(selectedTicket.createdAt).toLocaleDateString()}
                    </span>
                  </div>
                </div>
              </div>
              {selectedTicket.description && (
                <div className="mt-4 text-slate-700 dark:text-slate-300 whitespace-pre-wrap">{selectedTicket.description}</div>
              )}
            </div>

            {/* Comments */}
            <div className="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-6">
              <h4 className="text-md font-semibold text-slate-900 dark:text-white mb-4">
                Comments ({comments.length})
              </h4>
              {comments.length === 0 ? (
                <p className="text-slate-500 dark:text-slate-400 text-sm">No comments yet.</p>
              ) : (
                <div className="space-y-4">
                  {comments.map((comment) => (
                    <div key={comment.id} className="border-l-2 border-blue-200 dark:border-blue-800 pl-4 py-2">
                      <div className="flex items-center gap-2 mb-1">
                        <span className="text-sm font-medium text-slate-900 dark:text-white">{comment.authorName || 'Support'}</span>
                        <span className="text-xs text-slate-500 dark:text-slate-400">
                          {new Date(comment.createdAt).toLocaleString()}
                        </span>
                      </div>
                      <p className="text-sm text-slate-700 dark:text-slate-300 whitespace-pre-wrap">{comment.content}</p>
                    </div>
                  ))}
                </div>
              )}

              {/* Add Comment */}
              <form onSubmit={(e) => { void handleAddComment(e); }} className="mt-6">
                <textarea
                  value={newComment}
                  onChange={(e) => setNewComment(e.target.value)}
                  placeholder="Add a comment..."
                  rows={3}
                  className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-900 text-slate-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 resize-vertical"
                />
                <div className="flex justify-end mt-2">
                  <button
                    type="submit"
                    disabled={submitting || !newComment.trim()}
                    className="px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition-colors"
                  >
                    {submitting ? 'Sending...' : 'Add Comment'}
                  </button>
                </div>
              </form>
            </div>
          </div>
        )}

        {/* Ticket List */}
        {!selectedTicket && !showCreateForm && (
          <div className="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 overflow-hidden">
            {tickets.length === 0 ? (
              <div className="p-12 text-center">
                <svg className="w-12 h-12 text-slate-300 dark:text-slate-600 mx-auto mb-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                </svg>
                <h3 className="text-lg font-medium text-slate-900 dark:text-white mb-1">No tickets yet</h3>
                <p className="text-slate-500 dark:text-slate-400 mb-4">Create your first support ticket to get help.</p>
                <button
                  onClick={() => setShowCreateForm(true)}
                  className="px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 transition-colors"
                >
                  Create Ticket
                </button>
              </div>
            ) : (
              <table className="w-full">
                <thead className="bg-slate-50 dark:bg-slate-700/50">
                  <tr>
                    <th className="text-left px-6 py-3 text-xs font-medium text-slate-500 dark:text-slate-400 uppercase">#</th>
                    <th className="text-left px-6 py-3 text-xs font-medium text-slate-500 dark:text-slate-400 uppercase">Subject</th>
                    <th className="text-left px-6 py-3 text-xs font-medium text-slate-500 dark:text-slate-400 uppercase">Status</th>
                    <th className="text-left px-6 py-3 text-xs font-medium text-slate-500 dark:text-slate-400 uppercase">Priority</th>
                    <th className="text-left px-6 py-3 text-xs font-medium text-slate-500 dark:text-slate-400 uppercase">Created</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-200 dark:divide-slate-700">
                  {tickets.map((ticket) => (
                    <tr
                      key={ticket.id}
                      onClick={() => { void handleSelectTicket(ticket); }}
                      className="hover:bg-slate-50 dark:hover:bg-slate-700/30 cursor-pointer transition-colors"
                    >
                      <td className="px-6 py-4 text-sm text-slate-500 dark:text-slate-400">
                        {ticket.ticketNumber}
                      </td>
                      <td className="px-6 py-4 text-sm font-medium text-slate-900 dark:text-white">
                        {ticket.subject}
                      </td>
                      <td className="px-6 py-4">
                        <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${statusColors[ticket.status] || statusColors.open}`}>
                          {ticket.status.replace('_', ' ')}
                        </span>
                      </td>
                      <td className="px-6 py-4">
                        <span className={`text-sm font-medium capitalize ${priorityColors[ticket.priority] || ''}`}>
                          {ticket.priority}
                        </span>
                      </td>
                      <td className="px-6 py-4 text-sm text-slate-500 dark:text-slate-400">
                        {new Date(ticket.createdAt).toLocaleDateString()}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
