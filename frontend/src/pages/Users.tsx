import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Users as UsersIcon,
  Search,
  Plus,
  Edit,
  Trash2,
  Mail,
  Copy,
  Clock,
  CheckCircle,
} from 'lucide-react';
import { Header } from '@/components/layout';
import { Card, CardContent, Badge, Button, Modal } from '@/components/ui';
import api from '@/services/api';
import type { User } from '@/types';
import toast from 'react-hot-toast';

interface Invitation {
  id: string;
  token?: string;
  email?: string;
  role: string;
  expiresAt: string;
  usedAt?: string;
  createdAt: string;
}

interface UserFormData {
  email: string;
  firstName: string;
  lastName: string;
  password?: string;
  role: 'admin' | 'user' | 'readonly';
}

export function Users() {
  const queryClient = useQueryClient();
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedUser, setSelectedUser] = useState<User | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showEditModal, setShowEditModal] = useState(false);
  const [showDeleteModal, setShowDeleteModal] = useState(false);
  const [showInviteModal, setShowInviteModal] = useState(false);
  const [activeTab, setActiveTab] = useState<'users' | 'invitations'>('users');
  const [newInvitation, setNewInvitation] = useState<{ email: string; role: string }>({ email: '', role: 'viewer' });
  const [createdInvitation, setCreatedInvitation] = useState<Invitation | null>(null);
  const [formData, setFormData] = useState<UserFormData>({
    email: '',
    firstName: '',
    lastName: '',
    password: '',
    role: 'user',
  });

  const { data: users = [], isLoading } = useQuery({
    queryKey: ['users', searchQuery],
    queryFn: () => api.getUsers({ search: searchQuery || undefined }),
  });

  const { data: invitations = [], isLoading: invitationsLoading } = useQuery({
    queryKey: ['invitations'],
    queryFn: () => api.getInvitations(),
  });

  const createInvitationMutation = useMutation({
    mutationFn: (data: { email?: string; role: string }) => api.createInvitation(data),
    onSuccess: (data) => {
      setCreatedInvitation(data);
      queryClient.invalidateQueries({ queryKey: ['invitations'] });
    },
    onError: () => toast.error('Failed to create invitation'),
  });

  const deleteInvitationMutation = useMutation({
    mutationFn: (id: string) => api.deleteInvitation(id),
    onSuccess: () => {
      toast.success('Invitation deleted');
      queryClient.invalidateQueries({ queryKey: ['invitations'] });
    },
    onError: () => toast.error('Failed to delete invitation'),
  });

  const copyInviteLink = (token: string) => {
    const link = `${window.location.origin}/register?token=${token}`;
    navigator.clipboard.writeText(link);
    toast.success('Invitation link copied to clipboard');
  };

  const handleCreateInvitation = () => {
    createInvitationMutation.mutate({
      email: newInvitation.email || undefined,
      role: newInvitation.role,
    });
  };

  const createMutation = useMutation({
    mutationFn: (data: UserFormData) => api.createUser({ ...data, password: data.password || "" }),
    onSuccess: () => {
      toast.success('User created successfully');
      setShowCreateModal(false);
      resetForm();
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: () => toast.error('Failed to create user'),
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<UserFormData> }) =>
      api.updateUser(id, data),
    onSuccess: () => {
      toast.success('User updated successfully');
      setShowEditModal(false);
      setSelectedUser(null);
      resetForm();
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: () => toast.error('Failed to update user'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deleteUser(id),
    onSuccess: () => {
      toast.success('User deleted successfully');
      setShowDeleteModal(false);
      setSelectedUser(null);
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: () => toast.error('Failed to delete user'),
  });

  const resetForm = () => {
    setFormData({ email: '', firstName: '', lastName: '', password: '', role: 'user' });
  };

  const handleEdit = (user: User) => {
    setSelectedUser(user);
    setFormData({
      email: user.email,
      firstName: user.firstName,
      lastName: user.lastName,
      role: user.role as 'admin' | 'user' | 'readonly',
    });
    setShowEditModal(true);
  };

  const handleDelete = (user: User) => {
    setSelectedUser(user);
    setShowDeleteModal(true);
  };

  const getRoleBadgeVariant = (role: string) => {
    switch (role) {
      case 'admin': return 'danger';
      case 'user': return 'info';
      case 'readonly': return 'default';
      default: return 'default';
    }
  };

  const filteredUsers = users.filter(
    (user: User) =>
      user.email.toLowerCase().includes(searchQuery.toLowerCase()) ||
      user.firstName.toLowerCase().includes(searchQuery.toLowerCase()) ||
      user.lastName.toLowerCase().includes(searchQuery.toLowerCase())
  );

  return (
    <div>
      <Header title="Users" subtitle={`${filteredUsers.length} users in organization`} />
      <div className="p-6 space-y-6">
        {/* Tabs */}
        <div className="flex gap-4 border-b border-border">
          <button
            onClick={() => setActiveTab('users')}
            className={`pb-3 px-1 text-sm font-medium border-b-2 transition-colors ${
              activeTab === 'users'
                ? 'border-primary text-primary'
                : 'border-transparent text-text-secondary hover:text-text-primary'
            }`}
          >
            Users ({filteredUsers.length})
          </button>
          <button
            onClick={() => setActiveTab('invitations')}
            className={`pb-3 px-1 text-sm font-medium border-b-2 transition-colors ${
              activeTab === 'invitations'
                ? 'border-primary text-primary'
                : 'border-transparent text-text-secondary hover:text-text-primary'
            }`}
          >
            Invitations ({invitations.filter((i: Invitation) => !i.usedAt).length})
          </button>
        </div>

        {activeTab === 'users' ? (
          <>
            <div className="flex flex-col sm:flex-row gap-4 justify-between">
              <div className="relative flex-1 max-w-md">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-secondary" />
                <input
                  type="text"
                  placeholder="Search users..."
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className="w-full pl-10 pr-4 py-2 bg-surface border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary focus:border-transparent"
                />
              </div>
              <div className="flex gap-2">
                <Button variant="secondary" onClick={() => setShowInviteModal(true)}>
                  <Mail className="w-4 h-4 mr-2" />Invite User
                </Button>
                <Button onClick={() => setShowCreateModal(true)}>
                  <Plus className="w-4 h-4 mr-2" />Add User
                </Button>
              </div>
            </div>

            <Card>
          <CardContent padding="none">
            {isLoading ? (
              <div className="p-8 text-center">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary mx-auto" />
              </div>
            ) : filteredUsers.length > 0 ? (
              <table className="w-full">
                <thead className="bg-[var(--hover-bg)] border-b border-border">
                  <tr>
                    <th className="text-left px-6 py-3 text-xs font-medium text-text-secondary uppercase tracking-wider">User</th>
                    <th className="text-left px-6 py-3 text-xs font-medium text-text-secondary uppercase tracking-wider">Email</th>
                    <th className="text-left px-6 py-3 text-xs font-medium text-text-secondary uppercase tracking-wider">Role</th>
                    <th className="text-left px-6 py-3 text-xs font-medium text-text-secondary uppercase tracking-wider">Last Login</th>
                    <th className="text-right px-6 py-3 text-xs font-medium text-text-secondary uppercase tracking-wider">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {filteredUsers.map((user: User) => (
                    <tr key={user.id} className="hover:bg-[var(--hover-bg)]">
                      <td className="px-6 py-4 whitespace-nowrap">
                        <div className="flex items-center gap-3">
                          <div className="w-10 h-10 rounded-full bg-primary-light flex items-center justify-center">
                            <span className="text-primary font-medium">{user.firstName[0]}{user.lastName[0]}</span>
                          </div>
                          <div>
                            <p className="font-medium text-text-primary">{user.firstName} {user.lastName}</p>
                          </div>
                        </div>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-text-secondary">{user.email}</td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <Badge variant={getRoleBadgeVariant(user.role)}>{user.role}</Badge>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-text-secondary">
                        {user.lastLogin ? new Date(user.lastLogin).toLocaleString() : 'Never'}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-right">
                        <div className="flex items-center justify-end gap-2">
                          <button onClick={() => handleEdit(user)} className="p-1 text-text-secondary hover:text-primary rounded">
                            <Edit className="w-4 h-4" />
                          </button>
                          <button onClick={() => handleDelete(user)} className="p-1 text-text-secondary hover:text-red-600 rounded">
                            <Trash2 className="w-4 h-4" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <div className="text-center py-12">
                <UsersIcon className="w-12 h-12 text-text-secondary mx-auto mb-4" />
                <h3 className="text-lg font-medium text-text-primary mb-2">No users found</h3>
                <p className="text-text-secondary mb-4">
                  {searchQuery ? 'Try adjusting your search' : 'Add your first user to get started'}
                </p>
              </div>
            )}
          </CardContent>
        </Card>
          </>
        ) : (
          <>
            <div className="flex justify-end">
              <Button onClick={() => setShowInviteModal(true)}>
                <Mail className="w-4 h-4 mr-2" />Create Invitation
              </Button>
            </div>

            <Card>
              <CardContent padding="none">
                {invitationsLoading ? (
                  <div className="p-8 text-center">
                    <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary mx-auto" />
                  </div>
                ) : invitations.length > 0 ? (
                  <table className="w-full">
                    <thead className="bg-[var(--hover-bg)] border-b border-border">
                      <tr>
                        <th className="text-left px-6 py-3 text-xs font-medium text-text-secondary uppercase tracking-wider">Email</th>
                        <th className="text-left px-6 py-3 text-xs font-medium text-text-secondary uppercase tracking-wider">Role</th>
                        <th className="text-left px-6 py-3 text-xs font-medium text-text-secondary uppercase tracking-wider">Status</th>
                        <th className="text-left px-6 py-3 text-xs font-medium text-text-secondary uppercase tracking-wider">Expires</th>
                        <th className="text-right px-6 py-3 text-xs font-medium text-text-secondary uppercase tracking-wider">Actions</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border">
                      {invitations.map((invitation: Invitation) => (
                        <tr key={invitation.id} className="hover:bg-[var(--hover-bg)]">
                          <td className="px-6 py-4 whitespace-nowrap text-sm text-text-primary">
                            {invitation.email || <span className="text-text-secondary italic">Any email</span>}
                          </td>
                          <td className="px-6 py-4 whitespace-nowrap">
                            <Badge variant={invitation.role === 'admin' ? 'danger' : invitation.role === 'operator' ? 'warning' : 'default'}>
                              {invitation.role}
                            </Badge>
                          </td>
                          <td className="px-6 py-4 whitespace-nowrap">
                            {invitation.usedAt ? (
                              <span className="flex items-center gap-1 text-green-600">
                                <CheckCircle className="w-4 h-4" /> Used
                              </span>
                            ) : new Date(invitation.expiresAt) < new Date() ? (
                              <span className="text-text-secondary">Expired</span>
                            ) : (
                              <span className="flex items-center gap-1 text-amber-600">
                                <Clock className="w-4 h-4" /> Pending
                              </span>
                            )}
                          </td>
                          <td className="px-6 py-4 whitespace-nowrap text-sm text-text-secondary">
                            {new Date(invitation.expiresAt).toLocaleString()}
                          </td>
                          <td className="px-6 py-4 whitespace-nowrap text-right">
                            <div className="flex items-center justify-end gap-2">
                              {!invitation.usedAt && invitation.token && (
                                <button
                                  onClick={() => copyInviteLink(invitation.token!)}
                                  className="p-1 text-text-secondary hover:text-primary rounded"
                                  title="Copy invite link"
                                >
                                  <Copy className="w-4 h-4" />
                                </button>
                              )}
                              {!invitation.usedAt && (
                                <button
                                  onClick={() => deleteInvitationMutation.mutate(invitation.id)}
                                  className="p-1 text-text-secondary hover:text-red-600 rounded"
                                  title="Delete invitation"
                                >
                                  <Trash2 className="w-4 h-4" />
                                </button>
                              )}
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                ) : (
                  <div className="text-center py-12">
                    <Mail className="w-12 h-12 text-text-secondary mx-auto mb-4" />
                    <h3 className="text-lg font-medium text-text-primary mb-2">No invitations</h3>
                    <p className="text-text-secondary mb-4">Create an invitation to allow new users to register</p>
                  </div>
                )}
              </CardContent>
            </Card>
          </>
        )}
      </div>

      {/* Invite User Modal */}
      <Modal
        isOpen={showInviteModal}
        onClose={() => {
          setShowInviteModal(false);
          setCreatedInvitation(null);
          setNewInvitation({ email: '', role: 'viewer' });
        }}
        title={createdInvitation ? "Invitation Created" : "Invite User"}
      >
        {createdInvitation ? (
          <div className="space-y-4">
            <div className="p-4 bg-green-50 border border-green-200 rounded-lg">
              <p className="text-sm text-green-800 mb-2">Invitation created successfully! Share this link with the user:</p>
              <div className="flex items-center gap-2">
                <input
                  type="text"
                  readOnly
                  value={`${window.location.origin}/register?token=${createdInvitation.token}`}
                  className="flex-1 px-3 py-2 bg-white border border-green-300 rounded text-sm"
                />
                <Button
                  size="sm"
                  onClick={() => copyInviteLink(createdInvitation.token!)}
                >
                  <Copy className="w-4 h-4" />
                </Button>
              </div>
            </div>
            <p className="text-sm text-text-secondary">
              This invitation will expire in 48 hours.
              {createdInvitation.email && ` It can only be used by ${createdInvitation.email}.`}
            </p>
            <div className="flex justify-end">
              <Button onClick={() => {
                setShowInviteModal(false);
                setCreatedInvitation(null);
                setNewInvitation({ email: '', role: 'viewer' });
              }}>
                Done
              </Button>
            </div>
          </div>
        ) : (
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-text-primary mb-1">Email (optional)</label>
              <input
                type="email"
                value={newInvitation.email}
                onChange={(e) => setNewInvitation({ ...newInvitation, email: e.target.value })}
                placeholder="user@example.com"
                className="w-full px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary"
              />
              <p className="text-xs text-text-secondary mt-1">Leave blank to allow any email address</p>
            </div>
            <div>
              <label className="block text-sm font-medium text-text-primary mb-1">Role</label>
              <select
                value={newInvitation.role}
                onChange={(e) => setNewInvitation({ ...newInvitation, role: e.target.value })}
                className="w-full px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary"
              >
                <option value="viewer">Viewer</option>
                <option value="operator">Operator</option>
                <option value="admin">Admin</option>
              </select>
            </div>
            <div className="flex gap-3 justify-end pt-4">
              <Button
                type="button"
                variant="secondary"
                onClick={() => {
                  setShowInviteModal(false);
                  setNewInvitation({ email: '', role: 'viewer' });
                }}
              >
                Cancel
              </Button>
              <Button
                onClick={handleCreateInvitation}
                isLoading={createInvitationMutation.isPending}
              >
                Create Invitation
              </Button>
            </div>
          </div>
        )}
      </Modal>

      <Modal isOpen={showCreateModal} onClose={() => { setShowCreateModal(false); resetForm(); }} title="Add User">
        <form onSubmit={(e) => { e.preventDefault(); createMutation.mutate({...formData, password: formData.password || ""}); }} className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-text-primary mb-1">First Name</label>
              <input type="text" value={formData.firstName} onChange={(e) => setFormData({ ...formData, firstName: e.target.value })}
                className="w-full px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary" required />
            </div>
            <div>
              <label className="block text-sm font-medium text-text-primary mb-1">Last Name</label>
              <input type="text" value={formData.lastName} onChange={(e) => setFormData({ ...formData, lastName: e.target.value })}
                className="w-full px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary" required />
            </div>
          </div>
          <div>
            <label className="block text-sm font-medium text-text-primary mb-1">Email</label>
            <input type="email" value={formData.email} onChange={(e) => setFormData({ ...formData, email: e.target.value })}
              className="w-full px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary" required />
          </div>
          <div>
            <label className="block text-sm font-medium text-text-primary mb-1">Password</label>
            <input type="password" value={formData.password} onChange={(e) => setFormData({ ...formData, password: e.target.value })}
              className="w-full px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary" required />
          </div>
          <div>
            <label className="block text-sm font-medium text-text-primary mb-1">Role</label>
            <select value={formData.role} onChange={(e) => setFormData({ ...formData, role: e.target.value as 'admin' | 'user' | 'readonly' })}
              className="w-full px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary">
              <option value="user">User</option>
              <option value="admin">Admin</option>
              <option value="readonly">Read Only</option>
            </select>
          </div>
          <div className="flex gap-3 justify-end pt-4">
            <Button type="button" variant="secondary" onClick={() => { setShowCreateModal(false); resetForm(); }}>Cancel</Button>
            <Button type="submit" isLoading={createMutation.isPending}>Create User</Button>
          </div>
        </form>
      </Modal>

      <Modal isOpen={showEditModal} onClose={() => { setShowEditModal(false); setSelectedUser(null); resetForm(); }} title="Edit User">
        <form onSubmit={(e) => {
          e.preventDefault();
          if (selectedUser) {
            const updateData: Partial<UserFormData> = {
              firstName: formData.firstName,
              lastName: formData.lastName,
              email: formData.email,
              role: formData.role,
            };
            if (formData.password) updateData.password = formData.password;
            updateMutation.mutate({ id: selectedUser.id, data: updateData });
          }
        }} className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-text-primary mb-1">First Name</label>
              <input type="text" value={formData.firstName} onChange={(e) => setFormData({ ...formData, firstName: e.target.value })}
                className="w-full px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary" required />
            </div>
            <div>
              <label className="block text-sm font-medium text-text-primary mb-1">Last Name</label>
              <input type="text" value={formData.lastName} onChange={(e) => setFormData({ ...formData, lastName: e.target.value })}
                className="w-full px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary" required />
            </div>
          </div>
          <div>
            <label className="block text-sm font-medium text-text-primary mb-1">Email</label>
            <input type="email" value={formData.email} onChange={(e) => setFormData({ ...formData, email: e.target.value })}
              className="w-full px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary" required />
          </div>
          <div>
            <label className="block text-sm font-medium text-text-primary mb-1">New Password (leave blank to keep current)</label>
            <input type="password" value={formData.password || ''} onChange={(e) => setFormData({ ...formData, password: e.target.value })}
              className="w-full px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary" />
          </div>
          <div>
            <label className="block text-sm font-medium text-text-primary mb-1">Role</label>
            <select value={formData.role} onChange={(e) => setFormData({ ...formData, role: e.target.value as 'admin' | 'user' | 'readonly' })}
              className="w-full px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary">
              <option value="user">User</option>
              <option value="admin">Admin</option>
              <option value="readonly">Read Only</option>
            </select>
          </div>
          <div className="flex gap-3 justify-end pt-4">
            <Button type="button" variant="secondary" onClick={() => { setShowEditModal(false); setSelectedUser(null); resetForm(); }}>Cancel</Button>
            <Button type="submit" isLoading={updateMutation.isPending}>Save Changes</Button>
          </div>
        </form>
      </Modal>

      <Modal isOpen={showDeleteModal} onClose={() => { setShowDeleteModal(false); setSelectedUser(null); }} title="Delete User">
        <p className="text-text-secondary mb-6">
          Are you sure you want to delete <strong className="text-text-primary">{selectedUser?.firstName} {selectedUser?.lastName}</strong>? This action cannot be undone.
        </p>
        <div className="flex gap-3 justify-end">
          <Button variant="secondary" onClick={() => { setShowDeleteModal(false); setSelectedUser(null); }}>Cancel</Button>
          <Button variant="danger" onClick={() => selectedUser && deleteMutation.mutate(selectedUser.id)} isLoading={deleteMutation.isPending}>Delete User</Button>
        </div>
      </Modal>
    </div>
  );
}
