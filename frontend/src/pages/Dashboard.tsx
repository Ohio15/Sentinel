import { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import {
  Monitor,
  AlertTriangle,
  CheckCircle,
  XCircle,
  Activity,
  ArrowRight,
} from 'lucide-react';
import { Header } from '@/components/layout';
import { Card, CardContent, Badge } from '@/components/ui';
import api from '@/services/api';
import wsService from '@/services/websocket';
import { useDeviceStore } from '@/stores/deviceStore';
import { useAuthStore } from '@/stores/authStore';
import type { Device, Alert, DashboardStats } from '@/types';
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
} from 'recharts';

export function Dashboard() {
  const { devices, setDevices, updateDeviceStatus } = useDeviceStore();
  const { token, isAuthenticated } = useAuthStore();

  // Only enable queries when we have a valid token
  const queryEnabled = isAuthenticated && !!token;

  // Fetch dashboard stats - temporarily disabled until endpoint exists
  const { data: stats } = useQuery<DashboardStats>({
    queryKey: ['dashboard-stats'],
    queryFn: () => api.getDashboardStats(),
    refetchInterval: 30000,
    enabled: false, // Disabled - endpoint doesn't exist yet
  });

  // Fetch devices
  const { data: devicesData } = useQuery({
    queryKey: ['devices'],
    queryFn: () => api.getDevices({ pageSize: 100 }),
    enabled: queryEnabled,
    retry: false,
  });

  // Fetch recent alerts
  const { data: alertsData } = useQuery({
    queryKey: ['alerts', 'recent'],
    queryFn: () => api.getAlerts({ status: 'open', pageSize: 5 }),
    enabled: queryEnabled,
    retry: false,
  });

  useEffect(() => {
    // API returns array directly, not { devices: [...] }
    if (Array.isArray(devicesData)) {
      setDevices(devicesData);
    } else if (devicesData?.devices) {
      setDevices(devicesData.devices);
    }
  }, [devicesData, setDevices]);

  // Listen for real-time updates
  useEffect(() => {
    const unsubDeviceStatus = wsService.on('device_status', (data: unknown) => {
      const { deviceId, status, lastSeen } = data as { deviceId: string; status: Device['status']; lastSeen: string };
      updateDeviceStatus(deviceId, status, lastSeen);
    });

    return () => {
      unsubDeviceStatus();
    };
  }, [updateDeviceStatus]);

  const statCards = [
    {
      title: 'Total Devices',
      value: stats?.totalDevices || 0,
      icon: Monitor,
      color: 'text-blue-600',
      bgColor: 'bg-blue-100',
    },
    {
      title: 'Online',
      value: stats?.onlineDevices || 0,
      icon: CheckCircle,
      color: 'text-green-600',
      bgColor: 'bg-green-100',
    },
    {
      title: 'Offline',
      value: stats?.offlineDevices || 0,
      icon: XCircle,
      color: 'text-gray-600',
      bgColor: 'bg-gray-100',
    },
    {
      title: 'Open Alerts',
      value: stats?.openAlerts || 0,
      icon: AlertTriangle,
      color: 'text-amber-600',
      bgColor: 'bg-amber-100',
    },
  ];

  // Generate mock chart data (would come from real metrics in production)
  const chartData = Array.from({ length: 24 }, (_, i) => ({
    time: `${i}:00`,
    online: Math.floor(Math.random() * 20) + 80,
    alerts: Math.floor(Math.random() * 5),
  }));

  return (
    <div>
      <Header title="Dashboard" subtitle="Overview of your managed endpoints" />

      <div className="p-6 space-y-6">
        {/* Stats Grid */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          {statCards.map((stat) => (
            <Card key={stat.title}>
              <CardContent>
                <div className="flex items-center justify-between">
                  <div>
                    <p className="text-sm text-text-secondary">{stat.title}</p>
                    <p className="text-3xl font-bold text-text-primary mt-1">
                      {stat.value}
                    </p>
                  </div>
                  <div className={`p-3 rounded-xl ${stat.bgColor}`}>
                    <stat.icon className={`w-6 h-6 ${stat.color}`} />
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          {/* Device Status Chart */}
          <Card className="lg:col-span-2">
            <CardContent>
              <div className="flex items-center justify-between mb-4">
                <div>
                  <h3 className="text-lg font-semibold text-text-primary">Device Status</h3>
                  <p className="text-sm text-text-secondary">Last 24 hours</p>
                </div>
                <Activity className="w-5 h-5 text-text-secondary" />
              </div>
              <div className="h-64">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={chartData}>
                    <XAxis
                      dataKey="time"
                      tick={{ fontSize: 12 }}
                      tickLine={false}
                      axisLine={false}
                    />
                    <YAxis
                      tick={{ fontSize: 12 }}
                      tickLine={false}
                      axisLine={false}
                    />
                    <Tooltip />
                    <Line
                      type="monotone"
                      dataKey="online"
                      stroke="#22c55e"
                      strokeWidth={2}
                      dot={false}
                    />
                    <Line
                      type="monotone"
                      dataKey="alerts"
                      stroke="#f59e0b"
                      strokeWidth={2}
                      dot={false}
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            </CardContent>
          </Card>

          {/* Recent Alerts */}
          <Card>
            <CardContent>
              <div className="flex items-center justify-between mb-4">
                <h3 className="text-lg font-semibold text-text-primary">Recent Alerts</h3>
                <Link
                  to="/alerts"
                  className="text-sm text-primary hover:text-primary-hover flex items-center gap-1"
                >
                  View all
                  <ArrowRight className="w-4 h-4" />
                </Link>
              </div>
              <div className="space-y-3">
                {(Array.isArray(alertsData) ? alertsData : alertsData?.alerts)?.length > 0 ? (
                  (Array.isArray(alertsData) ? alertsData : alertsData?.alerts).map((alert: Alert) => (
                    <div
                      key={alert.id}
                      className="flex items-start gap-3 p-3 bg-gray-50 rounded-lg"
                    >
                      <AlertTriangle
                        className={`w-5 h-5 mt-0.5 ${
                          alert.severity === 'critical'
                            ? 'text-red-500'
                            : alert.severity === 'warning'
                            ? 'text-amber-500'
                            : 'text-blue-500'
                        }`}
                      />
                      <div className="flex-1 min-w-0">
                        <p className="text-sm font-medium text-text-primary truncate">
                          {alert.title}
                        </p>
                        <p className="text-xs text-text-secondary mt-0.5">
                          {new Date(alert.createdAt).toLocaleString()}
                        </p>
                      </div>
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
                    </div>
                  ))
                ) : (
                  <p className="text-sm text-text-secondary text-center py-4">
                    No open alerts
                  </p>
                )}
              </div>
            </CardContent>
          </Card>
        </div>

        {/* Device List */}
        <Card>
          <CardContent>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-text-primary">Devices</h3>
              <Link
                to="/devices"
                className="text-sm text-primary hover:text-primary-hover flex items-center gap-1"
              >
                View all
                <ArrowRight className="w-4 h-4" />
              </Link>
            </div>
            <div className="overflow-x-auto">
              <table className="w-full">
                <thead>
                  <tr className="border-b border-border">
                    <th className="text-left py-3 px-4 text-xs font-medium text-text-secondary uppercase">
                      Device
                    </th>
                    <th className="text-left py-3 px-4 text-xs font-medium text-text-secondary uppercase">
                      OS
                    </th>
                    <th className="text-left py-3 px-4 text-xs font-medium text-text-secondary uppercase">
                      Status
                    </th>
                    <th className="text-left py-3 px-4 text-xs font-medium text-text-secondary uppercase">
                      Last Seen
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {devices.slice(0, 5).map((device) => (
                    <tr
                      key={device.id}
                      className="hover:bg-gray-50 cursor-pointer"
                      onClick={() => window.location.href = `/devices/${device.id}`}
                    >
                      <td className="py-3 px-4">
                        <div className="flex items-center gap-3">
                          <Monitor className="w-5 h-5 text-text-secondary" />
                          <div>
                            <p className="text-sm font-medium text-text-primary">
                              {device.displayName || device.hostname}
                            </p>
                            <p className="text-xs text-text-secondary">
                              {device.ipAddress}
                            </p>
                          </div>
                        </div>
                      </td>
                      <td className="py-3 px-4">
                        <span className="text-sm text-text-primary capitalize">
                          {device.osType} {device.osVersion}
                        </span>
                      </td>
                      <td className="py-3 px-4">
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
                      </td>
                      <td className="py-3 px-4 text-sm text-text-secondary">
                        {new Date(device.lastSeen).toLocaleString()}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {devices.length === 0 && (
                <p className="text-sm text-text-secondary text-center py-8">
                  No devices enrolled yet. Install the agent on your endpoints to get started.
                </p>
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
