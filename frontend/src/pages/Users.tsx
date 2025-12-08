import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Users as UsersIcon,
  Search,
  Plus,
  Edit,
  Trash2,
} from 'lucide-react';
import { Header } from '@/components/layout';
import { Card, CardContent, Badge, Button, Modal } from '@/components/ui';
import api from '@/services/api';
import type { User } from '@/types';
import toast from 'react-hot-toast';

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
          <Button onClick={() => setShowCreateModal(true)}>
            <Plus className="w-4 h-4 mr-2" />Add User
          </Button>
        </div>

        <Card>
          <CardContent padding="none">
            {isLoading ? (
              <div className="p-8 text-center">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary mx-auto" />
              </div>
            ) : filteredUsers.length > 0 ? (
              <table className="w-full">
                <thead className="bg-gray-50 border-b border-border">
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
                    <tr key={user.id} className="hover:bg-gray-50">
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
      </div>

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
