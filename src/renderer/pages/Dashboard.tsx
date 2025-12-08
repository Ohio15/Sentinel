import React, { useMemo } from 'react';
import { useDeviceStore } from '../stores/deviceStore';
import { useAlertStore } from '../stores/alertStore';

interface DashboardProps {
  onDeviceSelect: (deviceId: string) => void;
}

export function Dashboard({ onDeviceSelect }: DashboardProps) {
  const { devices } = useDeviceStore();
  const { alerts } = useAlertStore();

  const stats = useMemo(() => {
    const online = devices.filter(d => d.status === 'online').length;
    const offline = devices.filter(d => d.status === 'offline').length;
    const warning = devices.filter(d => d.status === 'warning').length;
    const critical = devices.filter(d => d.status === 'critical').length;
    const openAlerts = alerts.filter(a => a.status === 'open').length;
    const criticalAlerts = alerts.filter(a => a.status === 'open' && a.severity === 'critical').length;

    return { online, offline, warning, critical, total: devices.length, openAlerts, criticalAlerts };
  }, [devices, alerts]);

  const recentAlerts = useMemo(() => {
    return alerts.filter(a => a.status === 'open').slice(0, 5);
  }, [alerts]);

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-text-primary">Dashboard</h1>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard
          title="Total Devices"
          value={stats.total}
          icon={<DeviceIcon />}
          color="blue"
        />
        <StatCard
          title="Online"
          value={stats.online}
          icon={<OnlineIcon />}
          color="green"
        />
        <StatCard
          title="Offline"
          value={stats.offline}
          icon={<OfflineIcon />}
          color="gray"
        />
        <StatCard
          title="Open Alerts"
          value={stats.openAlerts}
          subtitle={stats.criticalAlerts > 0 ? `${stats.criticalAlerts} critical` : undefined}
          icon={<AlertIcon />}
          color={stats.criticalAlerts > 0 ? 'red' : 'yellow'}
        />
      </div>

      {/* Main Content */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Recent Alerts */}
        <div className="card">
          <div className="p-4 border-b border-border">
            <h2 className="text-lg font-semibold text-text-primary">Recent Alerts</h2>
          </div>
          <div className="p-4">
            {recentAlerts.length === 0 ? (
              <p className="text-text-secondary text-center py-8">No open alerts</p>
            ) : (
              <div className="space-y-3">
                {recentAlerts.map(alert => (
                  <div
                    key={alert.id}
                    className="flex items-start gap-3 p-3 bg-gray-50 rounded-lg"
                  >
                    <div className={`w-2 h-2 mt-2 rounded-full ${
                      alert.severity === 'critical' ? 'bg-danger' :
                      alert.severity === 'warning' ? 'bg-warning' : 'bg-blue-500'
                    }`} />
                    <div className="flex-1 min-w-0">
                      <p className="font-medium text-text-primary truncate">{alert.title}</p>
                      <p className="text-sm text-text-secondary truncate">{alert.deviceName}</p>
                      <p className="text-xs text-text-secondary mt-1">
                        {new Date(alert.createdAt).toLocaleString()}
                      </p>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* Device Overview */}
        <div className="card">
          <div className="p-4 border-b border-border">
            <h2 className="text-lg font-semibold text-text-primary">Device Overview</h2>
          </div>
          <div className="p-4">
            {devices.length === 0 ? (
              <div className="text-center py-8">
                <p className="text-text-secondary mb-4">No devices registered yet</p>
                <p className="text-sm text-text-secondary">
                  Go to Settings to get the agent installation command
                </p>
              </div>
            ) : (
              <div className="space-y-2">
                {devices.slice(0, 8).map(device => (
                  <button
                    key={device.id}
                    onClick={() => onDeviceSelect(device.id)}
                    className="w-full flex items-center gap-3 p-3 hover:bg-gray-50 rounded-lg transition-colors text-left"
                  >
                    <div className={`status-indicator ${
                      device.status === 'online' ? 'status-online' :
                      device.status === 'warning' ? 'status-warning' :
                      device.status === 'critical' ? 'status-critical' : 'status-offline'
                    }`} />
                    <div className="flex-1 min-w-0">
                      <p className="font-medium text-text-primary truncate">
                        {device.displayName || device.hostname}
                      </p>
                      <p className="text-sm text-text-secondary truncate">
                        {device.osType} {device.osVersion} - {device.ipAddress}
                      </p>
                    </div>
                    <ChevronRightIcon className="w-4 h-4 text-text-secondary" />
                  </button>
                ))}
                {devices.length > 8 && (
                  <p className="text-sm text-text-secondary text-center pt-2">
                    +{devices.length - 8} more devices
                  </p>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

interface StatCardProps {
  title: string;
  value: number;
  subtitle?: string;
  icon: React.ReactNode;
  color: 'blue' | 'green' | 'red' | 'yellow' | 'gray';
}

function StatCard({ title, value, subtitle, icon, color }: StatCardProps) {
  const colors = {
    blue: 'bg-blue-100 text-blue-600',
    green: 'bg-green-100 text-green-600',
    red: 'bg-red-100 text-red-600',
    yellow: 'bg-yellow-100 text-yellow-600',
    gray: 'bg-gray-100 text-gray-600',
  };

  return (
    <div className="card p-4">
      <div className="flex items-start justify-between">
        <div>
          <p className="text-sm text-text-secondary">{title}</p>
          <p className="text-3xl font-bold text-text-primary mt-1">{value}</p>
          {subtitle && (
            <p className="text-sm text-danger mt-1">{subtitle}</p>
          )}
        </div>
        <div className={`p-3 rounded-lg ${colors[color]}`}>
          {icon}
        </div>
      </div>
    </div>
  );
}

// Icons
function DeviceIcon() {
  return (
    <svg className="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
    </svg>
  );
}

function OnlineIcon() {
  return (
    <svg className="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
    </svg>
  );
}

function OfflineIcon() {
  return (
    <svg className="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636" />
    </svg>
  );
}

function AlertIcon() {
  return (
    <svg className="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
    </svg>
  );
}

function ChevronRightIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
    </svg>
  );
}
