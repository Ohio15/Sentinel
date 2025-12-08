import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  AlertTriangle,
  CheckCircle,
  Clock,
  Bell,
} from 'lucide-react';
import { Header } from '@/components/layout';
import { Card, CardContent, Badge, Button, Modal } from '@/components/ui';
import api from '@/services/api';
import type { Alert } from '@/types';
import toast from 'react-hot-toast';

export function Alerts() {
  const queryClient = useQueryClient();
  const [statusFilter, setStatusFilter] = useState<string>('open');
  const [severityFilter, setSeverityFilter] = useState<string>('all');
  const [selectedAlert, setSelectedAlert] = useState<Alert | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ['alerts', statusFilter, severityFilter],
    queryFn: () =>
      api.getAlerts({
        status: statusFilter !== 'all' ? statusFilter : undefined,
        severity: severityFilter !== 'all' ? severityFilter : undefined,
        pageSize: 100,
      }),
  });

  // API returns array directly, not { alerts: [...] }
  const alerts: Alert[] = Array.isArray(data) ? data : (data?.alerts || []);

  const acknowledgeMutation = useMutation({
    mutationFn: (id: string) => api.acknowledgeAlert(id),
    onSuccess: () => {
      toast.success('Alert acknowledged');
      queryClient.invalidateQueries({ queryKey: ['alerts'] });
      setSelectedAlert(null);
    },
    onError: () => {
      toast.error('Failed to acknowledge alert');
    },
  });

  const resolveMutation = useMutation({
    mutationFn: (id: string) => api.resolveAlert(id),
    onSuccess: () => {
      toast.success('Alert resolved');
      queryClient.invalidateQueries({ queryKey: ['alerts'] });
      setSelectedAlert(null);
    },
    onError: () => {
      toast.error('Failed to resolve alert');
    },
  });

  const getSeverityIcon = (severity: string) => {
    switch (severity) {
      case 'critical':
        return <AlertTriangle className="w-5 h-5 text-red-500" />;
      case 'warning':
        return <AlertTriangle className="w-5 h-5 text-amber-500" />;
      default:
        return <Bell className="w-5 h-5 text-blue-500" />;
    }
  };


  return (
    <div>
      <Header
        title="Alerts"
        subtitle={`${alerts.length} alerts`}
      />

      <div className="p-6 space-y-6">
        {/* Filters */}
        <div className="flex flex-wrap gap-4">
          <div className="flex gap-2">
            <span className="text-sm text-text-secondary self-center">Status:</span>
            {(['all', 'open', 'acknowledged', 'resolved'] as const).map((status) => (
              <button
                key={status}
                onClick={() => setStatusFilter(status)}
                className={`px-3 py-1.5 text-sm font-medium rounded-lg transition-colors ${
                  statusFilter === status
                    ? 'bg-primary text-white'
                    : 'bg-surface border border-border text-text-secondary hover:text-text-primary'
                }`}
              >
                {status.charAt(0).toUpperCase() + status.slice(1)}
              </button>
            ))}
          </div>

          <div className="flex gap-2">
            <span className="text-sm text-text-secondary self-center">Severity:</span>
            {(['all', 'critical', 'warning', 'info'] as const).map((severity) => (
              <button
                key={severity}
                onClick={() => setSeverityFilter(severity)}
                className={`px-3 py-1.5 text-sm font-medium rounded-lg transition-colors ${
                  severityFilter === severity
                    ? 'bg-primary text-white'
                    : 'bg-surface border border-border text-text-secondary hover:text-text-primary'
                }`}
              >
                {severity.charAt(0).toUpperCase() + severity.slice(1)}
              </button>
            ))}
          </div>
        </div>

        {/* Alerts List */}
        {isLoading ? (
          <Card>
            <CardContent>
              <div className="animate-pulse space-y-4">
                {[...Array(5)].map((_, i) => (
                  <div key={i} className="h-16 bg-gray-100 rounded-lg" />
                ))}
              </div>
            </CardContent>
          </Card>
        ) : alerts.length > 0 ? (
          <div className="space-y-3">
            {alerts.map((alert) => (
              <Card
                key={alert.id}
                className="hover:border-primary/30 transition-colors cursor-pointer"
                onClick={() => setSelectedAlert(alert)}
              >
                <CardContent>
                  <div className="flex items-start gap-4">
                    <div className="mt-0.5">{getSeverityIcon(alert.severity)}</div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-start justify-between gap-4">
                        <div>
                          <h3 className="font-medium text-text-primary">
                            {alert.title}
                          </h3>
                          {alert.message && (
                            <p className="text-sm text-text-secondary mt-1 line-clamp-2">
                              {alert.message}
                            </p>
                          )}
                        </div>
                        <div className="flex items-center gap-2 flex-shrink-0">
                          <Badge
                            variant={
                              alert.severity === 'critical'
                                ? 'danger'
                                : alert.severity === 'warning'
                                ? 'warning'
                                : 'info'
                            }
                          >
                            {alert.severity}
                          </Badge>
                          <Badge
                            variant={
                              alert.status === 'resolved'
                                ? 'success'
                                : alert.status === 'acknowledged'
                                ? 'warning'
                                : 'danger'
                            }
                          >
                            {alert.status}
                          </Badge>
                        </div>
                      </div>
                      <div className="flex items-center gap-4 mt-2 text-xs text-text-secondary">
                        <span className="flex items-center gap-1">
                          <Clock className="w-3.5 h-3.5" />
                          {new Date(alert.createdAt).toLocaleString()}
                        </span>
                        {alert.device && (
                          <span>Device: {alert.device.hostname}</span>
                        )}
                      </div>
                    </div>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        ) : (
          <Card>
            <CardContent>
              <div className="text-center py-12">
                <CheckCircle className="w-12 h-12 text-green-500 mx-auto mb-4" />
                <h3 className="text-lg font-medium text-text-primary mb-2">
                  No alerts
                </h3>
                <p className="text-text-secondary">
                  {statusFilter !== 'all' || severityFilter !== 'all'
                    ? 'Try adjusting your filters'
                    : 'All systems are operating normally'}
                </p>
              </div>
            </CardContent>
          </Card>
        )}
      </div>

      {/* Alert Detail Modal */}
      <Modal
        isOpen={!!selectedAlert}
        onClose={() => setSelectedAlert(null)}
        title="Alert Details"
        size="lg"
      >
        {selectedAlert && (
          <div className="space-y-6">
            <div className="flex items-start gap-4">
              {getSeverityIcon(selectedAlert.severity)}
              <div>
                <h3 className="text-lg font-medium text-text-primary">
                  {selectedAlert.title}
                </h3>
                <div className="flex items-center gap-2 mt-1">
                  <Badge
                    variant={
                      selectedAlert.severity === 'critical'
                        ? 'danger'
                        : selectedAlert.severity === 'warning'
                        ? 'warning'
                        : 'info'
                    }
                  >
                    {selectedAlert.severity}
                  </Badge>
                  <Badge
                    variant={
                      selectedAlert.status === 'resolved'
                        ? 'success'
                        : selectedAlert.status === 'acknowledged'
                        ? 'warning'
                        : 'danger'
                    }
                  >
                    {selectedAlert.status}
                  </Badge>
                </div>
              </div>
            </div>

            {selectedAlert.message && (
              <div>
                <h4 className="text-sm font-medium text-text-secondary mb-2">
                  Message
                </h4>
                <p className="text-text-primary bg-gray-50 p-3 rounded-lg">
                  {selectedAlert.message}
                </p>
              </div>
            )}

            <div className="grid grid-cols-2 gap-4">
              <div>
                <h4 className="text-sm font-medium text-text-secondary mb-1">
                  Created
                </h4>
                <p className="text-text-primary">
                  {new Date(selectedAlert.createdAt).toLocaleString()}
                </p>
              </div>
              {selectedAlert.acknowledgedAt && (
                <div>
                  <h4 className="text-sm font-medium text-text-secondary mb-1">
                    Acknowledged
                  </h4>
                  <p className="text-text-primary">
                    {new Date(selectedAlert.acknowledgedAt).toLocaleString()}
                  </p>
                </div>
              )}
              {selectedAlert.resolvedAt && (
                <div>
                  <h4 className="text-sm font-medium text-text-secondary mb-1">
                    Resolved
                  </h4>
                  <p className="text-text-primary">
                    {new Date(selectedAlert.resolvedAt).toLocaleString()}
                  </p>
                </div>
              )}
            </div>

            {selectedAlert.status !== 'resolved' && (
              <div className="flex gap-3 pt-4 border-t border-border">
                {selectedAlert.status === 'open' && (
                  <Button
                    variant="secondary"
                    onClick={() => acknowledgeMutation.mutate(selectedAlert.id)}
                    isLoading={acknowledgeMutation.isPending}
                  >
                    Acknowledge
                  </Button>
                )}
                <Button
                  onClick={() => resolveMutation.mutate(selectedAlert.id)}
                  isLoading={resolveMutation.isPending}
                >
                  Resolve
                </Button>
              </div>
            )}
          </div>
        )}
      </Modal>
    </div>
  );
}
