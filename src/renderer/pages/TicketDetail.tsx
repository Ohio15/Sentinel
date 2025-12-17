import React, { useState, useEffect } from 'react';
import { useTicketStore, Ticket, TicketComment } from '../stores/ticketStore';

interface TicketDetailProps {
  ticketId: string;
  onBack: () => void;
}

export function TicketDetail({ ticketId, onBack }: TicketDetailProps) {
  const {
    selectedTicket,
    ticketComments,
    ticketActivity,
    templates,
    loading,
    fetchTicket,
    updateTicket,
    addComment,
    fetchTemplates,
    deleteTicket,
  } = useTicketStore();

  const [activeTab, setActiveTab] = useState<'comments' | 'activity'>('comments');
  const [newComment, setNewComment] = useState('');
  const [isInternal, setIsInternal] = useState(false);
  const [showEditModal, setShowEditModal] = useState(false);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);

  useEffect(() => {
    fetchTicket(ticketId);
    fetchTemplates();
  }, [ticketId]);

  const handleStatusChange = async (status: Ticket['status']) => {
    if (selectedTicket) {
      await updateTicket(ticketId, { status, actorName: 'Admin' });
    }
  };

  const handleAddComment = async () => {
    if (!newComment.trim() || !selectedTicket) return;

    await addComment({
      ticketId,
      content: newComment,
      isInternal,
      authorName: 'Admin',
      authorEmail: 'admin@sentinel.local',
      attachments: [],
    });
    setNewComment('');
  };

  const handleDelete = async () => {
    await deleteTicket(ticketId);
    onBack();
  };

  const useTemplate = (template: { content: string }) => {
    setNewComment(template.content);
  };

  const getStatusBadgeClass = (status: string) => {
    switch (status) {
      case 'open':
        return 'badge-info';
      case 'in_progress':
        return 'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200';
      case 'waiting':
        return 'badge-warning';
      case 'resolved':
        return 'badge-success';
      case 'closed':
        return 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300';
      default:
        return 'badge-info';
    }
  };

  const getPriorityBadgeClass = (priority: string) => {
    switch (priority) {
      case 'urgent':
        return 'badge-danger';
      case 'high':
        return 'bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200';
      case 'medium':
        return 'badge-warning';
      case 'low':
        return 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300';
      default:
        return 'badge-info';
    }
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleString();
  };

  const getActivityDescription = (action: string, fieldName?: string, oldValue?: string, newValue?: string) => {
    switch (action) {
      case 'created':
        return 'Created this ticket';
      case 'comment_added':
        return 'Added a comment';
      case 'internal_note_added':
        return 'Added an internal note';
      case 'field_changed':
        return `Changed ${fieldName} from "${oldValue}" to "${newValue}"`;
      default:
        return action;
    }
  };

  if (loading || !selectedTicket) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-text-secondary">Loading ticket...</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-4">
          <button
            onClick={onBack}
            className="p-2 hover:bg-gray-100 dark:hover:bg-slate-700 rounded-lg"
          >
            <BackIcon className="w-5 h-5 text-text-secondary" />
          </button>
          <div>
            <div className="flex items-center gap-3">
              <span className={`badge ${getStatusBadgeClass(selectedTicket.status)}`}>
                {selectedTicket.status.replace('_', ' ')}
              </span>
              <span className={`badge ${getPriorityBadgeClass(selectedTicket.priority)}`}>
                {selectedTicket.priority}
              </span>
            </div>
            <h2 className="text-xl text-text-primary mt-1">{selectedTicket.subject}</h2>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setShowEditModal(true)}
            className="btn btn-secondary"
          >
            Edit
          </button>
          <button
            onClick={() => setShowDeleteConfirm(true)}
            className="btn btn-danger"
          >
            Delete
          </button>
        </div>
      </div>

      <div className="grid grid-cols-3 gap-6">
        {/* Main Content */}
        <div className="col-span-2 space-y-6">
          {/* Description */}
          <div className="card p-6">
            <h3 className="text-lg font-semibold text-text-primary mb-4">Description</h3>
            <div className="text-text-primary whitespace-pre-wrap">
              {selectedTicket.description || 'No description provided.'}
            </div>
          </div>

          {/* Comments / Activity Tabs */}
          <div className="card">
            <div className="border-b border-border">
              <div className="flex">
                <button
                  onClick={() => setActiveTab('comments')}
                  className={`px-6 py-3 font-medium ${
                    activeTab === 'comments'
                      ? 'text-primary border-b-2 border-primary'
                      : 'text-text-secondary hover:text-text-primary'
                  }`}
                >
                  Comments ({ticketComments.length})
                </button>
                <button
                  onClick={() => setActiveTab('activity')}
                  className={`px-6 py-3 font-medium ${
                    activeTab === 'activity'
                      ? 'text-primary border-b-2 border-primary'
                      : 'text-text-secondary hover:text-text-primary'
                  }`}
                >
                  Activity ({ticketActivity.length})
                </button>
              </div>
            </div>

            <div className="p-6">
              {activeTab === 'comments' ? (
                <div className="space-y-4">
                  {/* Comment List */}
                  {ticketComments.length === 0 ? (
                    <p className="text-text-secondary text-center py-4">
                      No comments yet. Add the first comment below.
                    </p>
                  ) : (
                    ticketComments.map((comment) => (
                      <div
                        key={comment.id}
                        className={`p-4 rounded-lg ${
                          comment.isInternal
                            ? 'bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800'
                            : 'bg-gray-50 dark:bg-slate-800'
                        }`}
                      >
                        <div className="flex items-center justify-between mb-2">
                          <div className="flex items-center gap-2">
                            <span className="font-medium text-text-primary">
                              {comment.authorName}
                            </span>
                            {comment.isInternal && (
                              <span className="text-xs bg-yellow-200 dark:bg-yellow-800 text-yellow-800 dark:text-yellow-200 px-2 py-0.5 rounded">
                                Internal
                              </span>
                            )}
                          </div>
                          <span className="text-sm text-text-secondary">
                            {formatDate(comment.createdAt)}
                          </span>
                        </div>
                        <div className="text-text-primary whitespace-pre-wrap">
                          {comment.content}
                        </div>
                      </div>
                    ))
                  )}

                  {/* Add Comment */}
                  <div className="border-t border-border pt-4 mt-4">
                    <div className="flex items-center gap-4 mb-2">
                      <label className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          checked={isInternal}
                          onChange={(e) => setIsInternal(e.target.checked)}
                          className="w-4 h-4"
                        />
                        <span className="text-sm text-text-secondary">Internal note</span>
                      </label>
                      {templates.length > 0 && (
                        <div className="flex items-center gap-2">
                          <span className="text-sm text-text-secondary">Templates:</span>
                          {templates.map((t) => (
                            <button
                              key={t.id}
                              onClick={() => useTemplate(t)}
                              className="text-sm text-primary hover:underline"
                            >
                              {t.name}
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                    <textarea
                      value={newComment}
                      onChange={(e) => setNewComment(e.target.value)}
                      placeholder="Add a comment..."
                      className="input min-h-[100px] mb-2"
                    />
                    <div className="flex justify-end">
                      <button
                        onClick={handleAddComment}
                        disabled={!newComment.trim()}
                        className="btn btn-primary"
                      >
                        Add Comment
                      </button>
                    </div>
                  </div>
                </div>
              ) : (
                <div className="space-y-3">
                  {ticketActivity.length === 0 ? (
                    <p className="text-text-secondary text-center py-4">
                      No activity recorded.
                    </p>
                  ) : (
                    ticketActivity.map((activity) => (
                      <div
                        key={activity.id}
                        className="flex items-start gap-3 py-2 border-b border-border last:border-0"
                      >
                        <div className="w-8 h-8 rounded-full bg-primary-light flex items-center justify-center">
                          <ActivityIcon className="w-4 h-4 text-primary" />
                        </div>
                        <div className="flex-1">
                          <div className="text-text-primary">
                            <span className="font-medium">{activity.actorName}</span>{' '}
                            {getActivityDescription(
                              activity.action,
                              activity.fieldName,
                              activity.oldValue,
                              activity.newValue
                            )}
                          </div>
                          <div className="text-sm text-text-secondary">
                            {formatDate(activity.createdAt)}
                          </div>
                        </div>
                      </div>
                    ))
                  )}
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Sidebar */}
        <div className="space-y-6">
          {/* Quick Actions */}
          <div className="card p-4">
            <h3 className="font-semibold text-text-primary mb-3">Quick Actions</h3>
            <div className="space-y-2">
              {selectedTicket.status !== 'in_progress' && (
                <button
                  onClick={() => handleStatusChange('in_progress')}
                  className="w-full btn btn-secondary text-left"
                >
                  Mark In Progress
                </button>
              )}
              {selectedTicket.status !== 'waiting' && (
                <button
                  onClick={() => handleStatusChange('waiting')}
                  className="w-full btn btn-secondary text-left"
                >
                  Mark Waiting
                </button>
              )}
              {selectedTicket.status !== 'resolved' && (
                <button
                  onClick={() => handleStatusChange('resolved')}
                  className="w-full btn btn-secondary text-left"
                >
                  Resolve Ticket
                </button>
              )}
              {selectedTicket.status !== 'closed' && (
                <button
                  onClick={() => handleStatusChange('closed')}
                  className="w-full btn btn-secondary text-left"
                >
                  Close Ticket
                </button>
              )}
              {selectedTicket.status === 'closed' && (
                <button
                  onClick={() => handleStatusChange('open')}
                  className="w-full btn btn-secondary text-left"
                >
                  Reopen Ticket
                </button>
              )}
            </div>
          </div>

          {/* Details */}
          <div className="card p-4">
            <h3 className="font-semibold text-text-primary mb-3">Details</h3>
            <div className="space-y-3 text-sm">
              <div className="flex justify-between">
                <span className="text-text-secondary">Type</span>
                <span className="text-text-primary capitalize">{selectedTicket.type}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-text-secondary">Requester</span>
                <span className="text-text-primary">
                  {selectedTicket.requesterName || '-'}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-text-secondary">Email</span>
                <span className="text-text-primary">
                  {selectedTicket.requesterEmail || '-'}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-text-secondary">Assigned To</span>
                <span className="text-text-primary">
                  {selectedTicket.assignedTo || 'Unassigned'}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-text-secondary">Device</span>
                <span className="text-text-primary">
                  {selectedTicket.deviceDisplayName ||
                    selectedTicket.deviceName ||
                    'None'}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-text-secondary">Created</span>
                <span className="text-text-primary">
                  {formatDate(selectedTicket.createdAt)}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-text-secondary">Updated</span>
                <span className="text-text-primary">
                  {formatDate(selectedTicket.updatedAt)}
                </span>
              </div>
              {selectedTicket.resolvedAt && (
                <div className="flex justify-between">
                  <span className="text-text-secondary">Resolved</span>
                  <span className="text-text-primary">
                    {formatDate(selectedTicket.resolvedAt)}
                  </span>
                </div>
              )}
            </div>
          </div>

          {/* Tags */}
          {selectedTicket.tags && selectedTicket.tags.length > 0 && (
            <div className="card p-4">
              <h3 className="font-semibold text-text-primary mb-3">Tags</h3>
              <div className="flex flex-wrap gap-2">
                {selectedTicket.tags.map((tag, index) => (
                  <span
                    key={index}
                    className="px-2 py-1 bg-gray-100 dark:bg-slate-700 text-text-secondary rounded text-sm"
                  >
                    {tag}
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Edit Modal */}
      {showEditModal && (
        <EditTicketModal
          ticket={selectedTicket}
          onClose={() => setShowEditModal(false)}
          onSave={async (updates) => {
            await updateTicket(ticketId, { ...updates, actorName: 'Admin' });
            setShowEditModal(false);
          }}
        />
      )}

      {/* Delete Confirmation */}
      {showDeleteConfirm && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-surface rounded-lg shadow-xl p-6 max-w-md">
            <h3 className="text-lg font-semibold text-text-primary mb-2">
              Delete Ticket?
            </h3>
            <p className="text-text-secondary mb-4">
              Are you sure you want to delete this ticket? This
              action cannot be undone.
            </p>
            <div className="flex justify-end gap-3">
              <button
                onClick={() => setShowDeleteConfirm(false)}
                className="btn btn-secondary"
              >
                Cancel
              </button>
              <button onClick={handleDelete} className="btn btn-danger">
                Delete
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// Edit Ticket Modal
function EditTicketModal({
  ticket,
  onClose,
  onSave,
}: {
  ticket: Ticket;
  onClose: () => void;
  onSave: (updates: Partial<Ticket>) => Promise<void>;
}) {
  const [formData, setFormData] = useState({
    subject: ticket.subject,
    description: ticket.description || '',
    priority: ticket.priority,
    type: ticket.type,
    status: ticket.status,
    requesterName: ticket.requesterName || '',
    requesterEmail: ticket.requesterEmail || '',
    assignedTo: ticket.assignedTo || '',
  });
  const [saving, setSaving] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    try {
      await onSave(formData);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-surface rounded-lg shadow-xl w-full max-w-2xl max-h-[90vh] overflow-auto">
        <div className="p-6 border-b border-border">
          <h2 className="text-xl font-semibold text-text-primary">
            Edit Ticket
          </h2>
        </div>
        <form onSubmit={handleSubmit} className="p-6 space-y-4">
          <div>
            <label className="label">Subject</label>
            <input
              type="text"
              value={formData.subject}
              onChange={(e) => setFormData({ ...formData, subject: e.target.value })}
              className="input"
            />
          </div>
          <div>
            <label className="label">Description</label>
            <textarea
              value={formData.description}
              onChange={(e) => setFormData({ ...formData, description: e.target.value })}
              className="input min-h-[120px]"
            />
          </div>
          <div className="grid grid-cols-3 gap-4">
            <div>
              <label className="label">Status</label>
              <select
                value={formData.status}
                onChange={(e) =>
                  setFormData({ ...formData, status: e.target.value as any })
                }
                className="input"
              >
                <option value="open">Open</option>
                <option value="in_progress">In Progress</option>
                <option value="waiting">Waiting</option>
                <option value="resolved">Resolved</option>
                <option value="closed">Closed</option>
              </select>
            </div>
            <div>
              <label className="label">Priority</label>
              <select
                value={formData.priority}
                onChange={(e) =>
                  setFormData({ ...formData, priority: e.target.value as any })
                }
                className="input"
              >
                <option value="low">Low</option>
                <option value="medium">Medium</option>
                <option value="high">High</option>
                <option value="urgent">Urgent</option>
              </select>
            </div>
            <div>
              <label className="label">Type</label>
              <select
                value={formData.type}
                onChange={(e) =>
                  setFormData({ ...formData, type: e.target.value as any })
                }
                className="input"
              >
                <option value="incident">Incident</option>
                <option value="request">Request</option>
                <option value="problem">Problem</option>
                <option value="change">Change</option>
              </select>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">Requester Name</label>
              <input
                type="text"
                value={formData.requesterName}
                onChange={(e) =>
                  setFormData({ ...formData, requesterName: e.target.value })
                }
                className="input"
              />
            </div>
            <div>
              <label className="label">Requester Email</label>
              <input
                type="email"
                value={formData.requesterEmail}
                onChange={(e) =>
                  setFormData({ ...formData, requesterEmail: e.target.value })
                }
                className="input"
              />
            </div>
          </div>
          <div>
            <label className="label">Assigned To</label>
            <input
              type="text"
              value={formData.assignedTo}
              onChange={(e) => setFormData({ ...formData, assignedTo: e.target.value })}
              className="input"
            />
          </div>
          <div className="flex justify-end gap-3 pt-4 border-t border-border">
            <button type="button" onClick={onClose} className="btn btn-secondary">
              Cancel
            </button>
            <button type="submit" disabled={saving} className="btn btn-primary">
              {saving ? 'Saving...' : 'Save Changes'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

// Icons
function BackIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
    </svg>
  );
}

function ActivityIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={2}
        d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
      />
    </svg>
  );
}
