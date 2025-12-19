import React, { useState, useEffect } from 'react';
import { useTicketStore, Ticket, TicketFilters } from '../stores/ticketStore';
import { useDeviceStore, Device } from '../stores/deviceStore';
import { SLABadge, CategoryBadge, TicketViewSwitcher } from '../components/tickets';

interface Category {
  id: string;
  name: string;
  parentId: string | null;
  color: string | null;
  icon: string | null;
  isActive: boolean;
}

interface TicketsProps {
  onTicketSelect: (ticketId: string) => void;
}

export function Tickets({ onTicketSelect }: TicketsProps) {
  const {
    tickets,
    stats,
    filters,
    loading,
    fetchTickets,
    fetchStats,
    setFilters,
    createTicket,
  } = useTicketStore();

  const [showCreateModal, setShowCreateModal] = useState(false);
  const [searchTerm, setSearchTerm] = useState('');
  const [categories, setCategories] = useState<Category[]>([]);
  const [selectedCategoryId, setSelectedCategoryId] = useState<string>('');

  useEffect(() => {
    fetchTickets();
    fetchStats();
    loadCategories();
  }, []);

  const loadCategories = async () => {
    try {
      const cats = await window.api.categories.list();
      setCategories(cats);
    } catch (error) {
      console.error('Failed to load categories:', error);
    }
  };

  const handleFilterChange = (key: keyof TicketFilters, value: string) => {
    setFilters({ ...filters, [key]: value || undefined });
  };

  const filteredTickets = tickets.filter((ticket) => {
    // Search filter
    const matchesSearch = searchTerm
      ? ticket.subject.toLowerCase().includes(searchTerm.toLowerCase()) ||
        ticket.ticketNumber.toString().includes(searchTerm) ||
        ticket.requesterName?.toLowerCase().includes(searchTerm.toLowerCase())
      : true;

    // Category filter
    const matchesCategory = selectedCategoryId
      ? ticket.categoryId === selectedCategoryId
      : true;

    return matchesSearch && matchesCategory;
  });

  // Get category by ID for display
  const getCategoryById = (categoryId: string | null): Category | null => {
    if (!categoryId) return null;
    return categories.find((c) => c.id === categoryId) || null;
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
    const date = new Date(dateString);
    const now = new Date();
    const diff = now.getTime() - date.getTime();
    const hours = Math.floor(diff / (1000 * 60 * 60));
    const days = Math.floor(hours / 24);

    if (hours < 1) return 'Just now';
    if (hours < 24) return `${hours}h ago`;
    if (days < 7) return `${days}d ago`;
    return date.toLocaleDateString();
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold text-text-primary">Tickets</h1>
          <TicketViewSwitcher />
        </div>
        <button
          onClick={() => setShowCreateModal(true)}
          className="btn btn-primary flex items-center gap-2"
        >
          <PlusIcon className="w-5 h-5" />
          New Ticket
        </button>
      </div>

      {/* Stats Cards */}
      {stats && (
        <div className="grid grid-cols-2 md:grid-cols-6 gap-4">
          <StatCard
            label="Open"
            count={stats.openCount}
            color="blue"
            onClick={() => handleFilterChange('status', 'open')}
            active={filters.status === 'open'}
          />
          <StatCard
            label="In Progress"
            count={stats.inProgressCount}
            color="purple"
            onClick={() => handleFilterChange('status', 'in_progress')}
            active={filters.status === 'in_progress'}
          />
          <StatCard
            label="Waiting"
            count={stats.waitingCount}
            color="yellow"
            onClick={() => handleFilterChange('status', 'waiting')}
            active={filters.status === 'waiting'}
          />
          <StatCard
            label="Resolved"
            count={stats.resolvedCount}
            color="green"
            onClick={() => handleFilterChange('status', 'resolved')}
            active={filters.status === 'resolved'}
          />
          <StatCard
            label="Closed"
            count={stats.closedCount}
            color="gray"
            onClick={() => handleFilterChange('status', 'closed')}
            active={filters.status === 'closed'}
          />
          <StatCard
            label="Total"
            count={stats.totalCount}
            color="slate"
            onClick={() => handleFilterChange('status', '')}
            active={!filters.status}
          />
        </div>
      )}

      {/* Filters */}
      <div className="card p-4">
        <div className="flex flex-wrap items-center gap-4">
          <div className="flex-1 min-w-[200px]">
            <div className="relative">
              <SearchIcon className="absolute left-3 top-1/2 -translate-y-1/2 w-5 h-5 text-text-secondary" />
              <input
                type="text"
                placeholder="Search tickets..."
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
                className="input pl-10"
              />
            </div>
          </div>
          <select
            value={filters.priority || ''}
            onChange={(e) => handleFilterChange('priority', e.target.value)}
            className="input w-auto"
          >
            <option value="">All Priorities</option>
            <option value="urgent">Urgent</option>
            <option value="high">High</option>
            <option value="medium">Medium</option>
            <option value="low">Low</option>
          </select>
          <select
            value={filters.assignedTo || ''}
            onChange={(e) => handleFilterChange('assignedTo', e.target.value)}
            className="input w-auto"
          >
            <option value="">All Assignees</option>
            <option value="unassigned">Unassigned</option>
          </select>
          <select
            value={selectedCategoryId}
            onChange={(e) => setSelectedCategoryId(e.target.value)}
            className="input w-auto"
          >
            <option value="">All Categories</option>
            {categories.filter((c) => c.isActive).map((cat) => (
              <option key={cat.id} value={cat.id}>
                {cat.name}
              </option>
            ))}
          </select>
          {(filters.status || filters.priority || filters.assignedTo || selectedCategoryId) && (
            <button
              onClick={() => {
                setFilters({});
                setSelectedCategoryId('');
              }}
              className="text-sm text-primary hover:underline"
            >
              Clear filters
            </button>
          )}
        </div>
      </div>

      {/* Tickets Table */}
      <div className="card overflow-hidden">
        {loading ? (
          <div className="p-8 text-center text-text-secondary">Loading tickets...</div>
        ) : filteredTickets.length === 0 ? (
          <div className="p-8 text-center text-text-secondary">
            {tickets.length === 0
              ? 'No tickets yet. Create your first ticket!'
              : 'No tickets match your filters.'}
          </div>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="bg-gray-50 dark:bg-slate-800">
                <th className="text-left">Ticket</th>
                <th className="text-left">Status</th>
                <th className="text-left">Priority</th>
                <th className="text-left">SLA</th>
                <th className="text-left">Category</th>
                <th className="text-left">Requester</th>
                <th className="text-left">Assigned To</th>
                <th className="text-left">Created</th>
              </tr>
            </thead>
            <tbody>
              {filteredTickets.map((ticket) => (
                <tr
                  key={ticket.id}
                  onClick={() => onTicketSelect(ticket.id)}
                  className="cursor-pointer hover:bg-gray-50 dark:hover:bg-slate-800"
                >
                  <td>
                    <div>
                      <div className="font-medium text-text-primary">
                        {ticket.subject}
                      </div>
                      {ticket.description && (
                        <div className="text-sm text-text-secondary truncate max-w-md">
                          {ticket.description}
                        </div>
                      )}
                    </div>
                  </td>
                  <td>
                    <span className={`badge ${getStatusBadgeClass(ticket.status)}`}>
                      {ticket.status.replace('_', ' ')}
                    </span>
                  </td>
                  <td>
                    <span className={`badge ${getPriorityBadgeClass(ticket.priority)}`}>
                      {ticket.priority}
                    </span>
                  </td>
                  <td>
                    <SLABadge
                      firstResponseDueAt={ticket.firstResponseDueAt}
                      resolutionDueAt={ticket.resolutionDueAt}
                      firstResponseAt={ticket.firstResponseAt}
                      resolvedAt={ticket.resolvedAt}
                      slaResponseBreached={ticket.slaResponseBreached}
                      slaResolutionBreached={ticket.slaResolutionBreached}
                      slaPausedAt={ticket.slaPausedAt}
                      status={ticket.status}
                    />
                  </td>
                  <td>
                    <CategoryBadge
                      category={ticket.categoryId ? getCategoryById(ticket.categoryId) : null}
                      size="sm"
                    />
                  </td>
                  <td className="text-text-primary">
                    {ticket.requesterName || '-'}
                  </td>
                  <td className="text-text-primary">
                    {ticket.assignedTo || (
                      <span className="text-text-secondary">Unassigned</span>
                    )}
                  </td>
                  <td className="text-text-secondary">{formatDate(ticket.createdAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Create Ticket Modal */}
      {showCreateModal && (
        <CreateTicketModal
          onClose={() => setShowCreateModal(false)}
          onCreate={async (ticket) => {
            await createTicket(ticket);
            setShowCreateModal(false);
            fetchStats();
          }}
        />
      )}
    </div>
  );
}

// Stat Card Component
function StatCard({
  label,
  count,
  color,
  onClick,
  active,
}: {
  label: string;
  count: number;
  color: string;
  onClick: () => void;
  active: boolean;
}) {
  const colorClasses: Record<string, string> = {
    blue: 'border-blue-500',
    purple: 'border-purple-500',
    yellow: 'border-yellow-500',
    green: 'border-green-500',
    gray: 'border-gray-500',
    slate: 'border-slate-500',
  };

  return (
    <button
      onClick={onClick}
      className={`card p-4 text-left transition-all ${
        active ? `border-2 ${colorClasses[color]} bg-opacity-50` : 'hover:shadow-md'
      }`}
    >
      <div className="text-2xl font-bold text-text-primary">{count}</div>
      <div className="text-sm text-text-secondary">{label}</div>
    </button>
  );
}

// Create Ticket Modal
function CreateTicketModal({
  onClose,
  onCreate,
}: {
  onClose: () => void;
  onCreate: (ticket: Partial<Ticket>) => Promise<void>;
}) {
  const { devices, fetchDevices } = useDeviceStore();
  const [formData, setFormData] = useState({
    subject: '',
    description: '',
    priority: 'medium' as const,
    type: 'incident' as const,
    requesterName: '',
    requesterEmail: '',
    assignedTo: '',
    deviceId: '',
  });
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    fetchDevices();
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formData.subject.trim()) return;

    setSaving(true);
    try {
      const ticketData = {
        ...formData,
        deviceId: formData.deviceId || undefined,
      };
      await onCreate(ticketData);
    } catch (error) {
      console.error('Failed to create ticket:', error);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-surface rounded-lg shadow-xl w-full max-w-2xl max-h-[90vh] overflow-auto">
        <div className="p-6 border-b border-border">
          <div className="flex items-center justify-between">
            <h2 className="text-xl font-semibold text-text-primary">Create New Ticket</h2>
            <button onClick={onClose} className="text-text-secondary hover:text-text-primary">
              <CloseIcon className="w-6 h-6" />
            </button>
          </div>
        </div>
        <form onSubmit={handleSubmit} className="p-6 space-y-4">
          <div>
            <label className="label">Subject *</label>
            <input
              type="text"
              value={formData.subject}
              onChange={(e) => setFormData({ ...formData, subject: e.target.value })}
              className="input"
              placeholder="Brief description of the issue"
              required
            />
          </div>
          <div>
            <label className="label">Description</label>
            <textarea
              value={formData.description}
              onChange={(e) => setFormData({ ...formData, description: e.target.value })}
              className="input min-h-[120px]"
              placeholder="Detailed description of the issue..."
            />
          </div>
          <div className="grid grid-cols-2 gap-4">
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
                placeholder="John Doe"
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
                placeholder="john@example.com"
              />
            </div>
          </div>
          <div>
            <label className="label">Device</label>
            <select
              value={formData.deviceId}
              onChange={(e) => setFormData({ ...formData, deviceId: e.target.value })}
              className="input"
            >
              <option value="">No device selected</option>
              {devices.map((device) => (
                <option key={device.id} value={device.id}>
                  {device.displayName || device.hostname} ({device.osType})
                </option>
              ))}
            </select>
            <p className="text-xs text-text-secondary mt-1">
              Selecting a device will automatically collect diagnostics when the ticket is created.
            </p>
          </div>
          <div>
            <label className="label">Assigned To</label>
            <input
              type="text"
              value={formData.assignedTo}
              onChange={(e) => setFormData({ ...formData, assignedTo: e.target.value })}
              className="input"
              placeholder="Technician name"
            />
          </div>
          <div className="flex justify-end gap-3 pt-4 border-t border-border">
            <button type="button" onClick={onClose} className="btn btn-secondary">
              Cancel
            </button>
            <button type="submit" disabled={saving} className="btn btn-primary">
              {saving ? 'Creating...' : 'Create Ticket'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

// Icons
function PlusIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
    </svg>
  );
}

function SearchIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={2}
        d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
      />
    </svg>
  );
}

function CloseIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
    </svg>
  );
}
