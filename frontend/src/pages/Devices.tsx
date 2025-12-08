import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import {
  Monitor,
  Search,
  MoreVertical,
  Trash2,
  Edit,
  Terminal,
} from 'lucide-react';
import { Header } from '@/components/layout';
import { Card, CardContent, Badge, Button, Modal } from '@/components/ui';
import api from '@/services/api';
import type { Device } from '@/types';
import toast from 'react-hot-toast';

export function Devices() {
  const navigate = useNavigate();
  const [searchQuery, setSearchQuery] = useState('');
  const [statusFilter, setStatusFilter] = useState<string>('all');
  const [selectedDevice, setSelectedDevice] = useState<Device | null>(null);
  const [showDeleteModal, setShowDeleteModal] = useState(false);

  const { data, isLoading, refetch } = useQuery({
    queryKey: ['devices', statusFilter, searchQuery],
    queryFn: () =>
      api.getDevices({
        status: statusFilter !== 'all' ? statusFilter : undefined,
        search: searchQuery || undefined,
        pageSize: 100,
      }),
  });

  // API returns array directly, not { devices: [...] }
  const devices: Device[] = Array.isArray(data) ? data : (data?.devices || []);

  const handleDelete = async () => {
    if (!selectedDevice) return;

    try {
      await api.deleteDevice(selectedDevice.id);
      toast.success('Device deleted successfully');
      setShowDeleteModal(false);
      setSelectedDevice(null);
      refetch();
    } catch {
      toast.error('Failed to delete device');
    }
  };

  const getOsIcon = (_osType: string) => {
    // In a real app, you'd use specific OS icons
    return <Monitor className="w-5 h-5" />;
  };

  const statusCounts = {
    all: devices.length,
    online: devices.filter((d) => d.status === 'online').length,
    offline: devices.filter((d) => d.status === 'offline').length,
    warning: devices.filter((d) => d.status === 'warning').length,
    critical: devices.filter((d) => d.status === 'critical').length,
  };

  return (
    <div>
      <Header
        title="Devices"
        subtitle={`${devices.length} devices enrolled`}
      />

      <div className="p-6 space-y-6">
        {/* Filters */}
        <div className="flex flex-col sm:flex-row gap-4">
          <div className="relative flex-1">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-secondary" />
            <input
              type="text"
              placeholder="Search devices..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-full pl-10 pr-4 py-2 bg-surface border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary focus:border-transparent"
            />
          </div>

          <div className="flex gap-2">
            {(['all', 'online', 'offline', 'warning', 'critical'] as const).map(
              (status) => (
                <button
                  key={status}
                  onClick={() => setStatusFilter(status)}
                  className={`px-3 py-2 text-sm font-medium rounded-lg transition-colors ${
                    statusFilter === status
                      ? 'bg-primary text-white'
                      : 'bg-surface border border-border text-text-secondary hover:text-text-primary'
                  }`}
                >
                  {status.charAt(0).toUpperCase() + status.slice(1)} (
                  {statusCounts[status]})
                </button>
              )
            )}
          </div>
        </div>

        {/* Device Grid */}
        {isLoading ? (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {[...Array(6)].map((_, i) => (
              <Card key={i}>
                <CardContent>
                  <div className="animate-pulse space-y-4">
                    <div className="flex items-center gap-3">
                      <div className="w-10 h-10 bg-gray-200 rounded-lg" />
                      <div className="flex-1">
                        <div className="h-4 bg-gray-200 rounded w-3/4" />
                        <div className="h-3 bg-gray-200 rounded w-1/2 mt-2" />
                      </div>
                    </div>
                    <div className="h-4 bg-gray-200 rounded" />
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        ) : devices.length > 0 ? (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {devices.map((device) => (
              <Card
                key={device.id}
                className="hover:border-primary/30 transition-colors cursor-pointer"
                onClick={() => navigate(`/devices/${device.id}`)}
              >
                <CardContent>
                  <div className="flex items-start justify-between">
                    <div className="flex items-center gap-3">
                      <div
                        className={`p-2 rounded-lg ${
                          device.status === 'online'
                            ? 'bg-green-100 text-green-600'
                            : device.status === 'warning'
                            ? 'bg-amber-100 text-amber-600'
                            : device.status === 'critical'
                            ? 'bg-red-100 text-red-600'
                            : 'bg-gray-100 text-gray-600'
                        }`}
                      >
                        {getOsIcon(device.osType)}
                      </div>
                      <div>
                        <h3 className="font-medium text-text-primary">
                          {device.displayName || device.hostname}
                        </h3>
                        <p className="text-sm text-text-secondary">
                          {device.ipAddress}
                        </p>
                      </div>
                    </div>

                    <div className="relative group">
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          setSelectedDevice(device);
                        }}
                        className="p-1 text-text-secondary hover:text-text-primary rounded"
                      >
                        <MoreVertical className="w-5 h-5" />
                      </button>

                      {selectedDevice?.id === device.id && (
                        <div className="absolute right-0 top-8 w-48 bg-surface border border-border rounded-lg shadow-lg z-10">
                          <button
                            onClick={(e) => {
                              e.stopPropagation();
                              navigate(`/devices/${device.id}`);
                            }}
                            className="w-full flex items-center gap-2 px-3 py-2 text-sm text-text-primary hover:bg-gray-50"
                          >
                            <Edit className="w-4 h-4" />
                            View Details
                          </button>
                          <button
                            onClick={(e) => {
                              e.stopPropagation();
                              // Open terminal
                            }}
                            className="w-full flex items-center gap-2 px-3 py-2 text-sm text-text-primary hover:bg-gray-50"
                          >
                            <Terminal className="w-4 h-4" />
                            Remote Terminal
                          </button>
                          <button
                            onClick={(e) => {
                              e.stopPropagation();
                              setShowDeleteModal(true);
                            }}
                            className="w-full flex items-center gap-2 px-3 py-2 text-sm text-red-600 hover:bg-red-50"
                          >
                            <Trash2 className="w-4 h-4" />
                            Delete
                          </button>
                        </div>
                      )}
                    </div>
                  </div>

                  <div className="mt-4 space-y-2">
                    <div className="flex items-center justify-between text-sm">
                      <span className="text-text-secondary">OS</span>
                      <span className="text-text-primary capitalize">
                        {device.osType} {device.osVersion}
                      </span>
                    </div>
                    <div className="flex items-center justify-between text-sm">
                      <span className="text-text-secondary">Agent</span>
                      <span className="text-text-primary">
                        v{device.agentVersion}
                      </span>
                    </div>
                    <div className="flex items-center justify-between text-sm">
                      <span className="text-text-secondary">Last Seen</span>
                      <span className="text-text-primary">
                        {new Date(device.lastSeen).toLocaleString()}
                      </span>
                    </div>
                  </div>

                  <div className="mt-4 flex items-center justify-between">
                    <Badge
                      variant={
                        device.status === 'online'
                          ? 'success'
                          : device.status === 'warning'
                          ? 'warning'
                          : device.status === 'critical'
                          ? 'danger'
                          : 'default'
                      }
                    >
                      {device.status}
                    </Badge>

                    {device.tags.length > 0 && (
                      <div className="flex gap-1">
                        {device.tags.slice(0, 2).map((tag) => (
                          <Badge key={tag} variant="info" size="sm">
                            {tag}
                          </Badge>
                        ))}
                        {device.tags.length > 2 && (
                          <Badge variant="default" size="sm">
                            +{device.tags.length - 2}
                          </Badge>
                        )}
                      </div>
                    )}
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        ) : (
          <Card>
            <CardContent>
              <div className="text-center py-12">
                <Monitor className="w-12 h-12 text-text-secondary mx-auto mb-4" />
                <h3 className="text-lg font-medium text-text-primary mb-2">
                  No devices found
                </h3>
                <p className="text-text-secondary mb-4">
                  {searchQuery || statusFilter !== 'all'
                    ? 'Try adjusting your filters'
                    : 'Install the Sentinel agent on your endpoints to get started'}
                </p>
              </div>
            </CardContent>
          </Card>
        )}
      </div>

      {/* Delete Confirmation Modal */}
      <Modal
        isOpen={showDeleteModal}
        onClose={() => {
          setShowDeleteModal(false);
          setSelectedDevice(null);
        }}
        title="Delete Device"
      >
        <p className="text-text-secondary mb-6">
          Are you sure you want to delete{' '}
          <strong className="text-text-primary">
            {selectedDevice?.displayName || selectedDevice?.hostname}
          </strong>
          ? This action cannot be undone.
        </p>
        <div className="flex gap-3 justify-end">
          <Button
            variant="secondary"
            onClick={() => {
              setShowDeleteModal(false);
              setSelectedDevice(null);
            }}
          >
            Cancel
          </Button>
          <Button variant="danger" onClick={handleDelete}>
            Delete Device
          </Button>
        </div>
      </Modal>
    </div>
  );
}
