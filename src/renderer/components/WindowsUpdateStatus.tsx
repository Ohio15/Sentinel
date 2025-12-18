import React, { useEffect, useState } from 'react';

interface PendingUpdateInfo {
  title: string;
  kb?: string;
  severity?: string;
  sizeMB?: number;
  isSecurityUpdate: boolean;
}

interface DeviceUpdateStatus {
  deviceId: string;
  hostname?: string;
  displayName?: string;
  deviceStatus?: string;
  pendingCount: number;
  securityUpdateCount: number;
  rebootRequired: boolean;
  lastChecked: string;
  lastUpdateInstalled?: string;
  pendingUpdates?: PendingUpdateInfo[];
}

interface WindowsUpdateStatusProps {
  deviceId: string;
  osType?: string;
}

export function WindowsUpdateStatus({ deviceId, osType }: WindowsUpdateStatusProps) {
  const [updateStatus, setUpdateStatus] = useState<DeviceUpdateStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [expanded, setExpanded] = useState(false);

  useEffect(() => {
    async function fetchUpdateStatus() {
      try {
        setLoading(true);
        const status = await window.api.updates.getDevice(deviceId);
        setUpdateStatus(status);
      } catch (error) {
        console.error('Failed to fetch update status:', error);
      } finally {
        setLoading(false);
      }
    }

    fetchUpdateStatus();

    // Subscribe to real-time update status changes
    const unsubscribe = window.api.updates.onStatus((data: DeviceUpdateStatus) => {
      if (data.deviceId === deviceId) {
        setUpdateStatus(data);
      }
    });

    return () => {
      unsubscribe();
    };
  }, [deviceId]);

  // Only show for Windows devices
  if (osType && !osType.toLowerCase().includes('windows')) {
    return null;
  }

  if (loading) {
    return (
      <div className="bg-surface rounded-lg border border-border p-4">
        <h3 className="text-base font-semibold text-text-primary mb-3 flex items-center gap-2">
          <WindowsIcon className="w-5 h-5" />
          Windows Update
        </h3>
        <div className="animate-pulse">
          <div className="h-4 bg-gray-200 rounded w-1/2 mb-2"></div>
          <div className="h-4 bg-gray-200 rounded w-1/3"></div>
        </div>
      </div>
    );
  }

  if (!updateStatus) {
    return (
      <div className="bg-surface rounded-lg border border-border p-4">
        <h3 className="text-base font-semibold text-text-primary mb-3 flex items-center gap-2">
          <WindowsIcon className="w-5 h-5" />
          Windows Update
        </h3>
        <p className="text-sm text-text-secondary">Update status not available</p>
      </div>
    );
  }

  const hasUpdates = updateStatus.pendingCount > 0;
  const hasSecurityUpdates = updateStatus.securityUpdateCount > 0;
  const needsReboot = updateStatus.rebootRequired;

  const getStatusColor = () => {
    if (needsReboot) return 'text-orange-600 bg-orange-50';
    if (hasSecurityUpdates) return 'text-red-600 bg-red-50';
    if (hasUpdates) return 'text-yellow-600 bg-yellow-50';
    return 'text-green-600 bg-green-50';
  };

  const getStatusText = () => {
    if (needsReboot) return 'Restart Required';
    if (hasSecurityUpdates) return 'Security Updates Pending';
    if (hasUpdates) return 'Updates Available';
    return 'Up to Date';
  };

  const formatDate = (dateStr: string | undefined) => {
    if (!dateStr) return 'Never';
    const date = new Date(dateStr);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / (1000 * 60));
    const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));

    if (diffMins < 1) return 'Just now';
    if (diffMins < 60) return `${diffMins} min ago`;
    if (diffHours < 24) return `${diffHours} hours ago`;
    if (diffDays < 7) return `${diffDays} days ago`;
    return date.toLocaleDateString();
  };

  return (
    <div className="bg-surface rounded-lg border border-border overflow-hidden">
      {/* Header - Always visible */}
      <div
        className="p-4 cursor-pointer hover:bg-gray-50 transition-colors"
        onClick={() => setExpanded(!expanded)}
      >
        <div className="flex items-center justify-between">
          <h3 className="text-base font-semibold text-text-primary flex items-center gap-2">
            <WindowsIcon className="w-5 h-5" />
            Windows Update
          </h3>
          <div className="flex items-center gap-3">
            <span className={`text-xs px-2 py-1 rounded-full font-medium ${getStatusColor()}`}>
              {getStatusText()}
            </span>
            <ChevronIcon className={`w-4 h-4 text-text-secondary transition-transform ${expanded ? 'rotate-180' : ''}`} />
          </div>
        </div>

        {/* Summary row */}
        <div className="mt-2 flex items-center gap-4 text-sm">
          <div className="flex items-center gap-1">
            <span className="text-text-secondary">Pending:</span>
            <span className={`font-medium ${hasUpdates ? 'text-yellow-600' : 'text-text-primary'}`}>
              {updateStatus.pendingCount}
            </span>
          </div>
          <div className="flex items-center gap-1">
            <span className="text-text-secondary">Security:</span>
            <span className={`font-medium ${hasSecurityUpdates ? 'text-red-600' : 'text-text-primary'}`}>
              {updateStatus.securityUpdateCount}
            </span>
          </div>
          {needsReboot && (
            <div className="flex items-center gap-1 text-orange-600">
              <RebootIcon className="w-4 h-4" />
              <span className="font-medium">Restart required</span>
            </div>
          )}
        </div>
      </div>

      {/* Expanded details */}
      {expanded && (
        <div className="border-t border-border">
          {/* Metadata */}
          <div className="p-4 bg-gray-50 grid grid-cols-2 gap-4 text-sm">
            <div>
              <span className="text-text-secondary">Last Checked:</span>
              <span className="ml-2 text-text-primary">{formatDate(updateStatus.lastChecked)}</span>
            </div>
            <div>
              <span className="text-text-secondary">Last Update Installed:</span>
              <span className="ml-2 text-text-primary">{formatDate(updateStatus.lastUpdateInstalled)}</span>
            </div>
          </div>

          {/* Pending updates list */}
          {updateStatus.pendingUpdates && updateStatus.pendingUpdates.length > 0 && (
            <div className="p-4">
              <h4 className="text-sm font-medium text-text-primary mb-3">Pending Updates</h4>
              <div className="space-y-2 max-h-64 overflow-y-auto">
                {updateStatus.pendingUpdates.map((update, index) => (
                  <div
                    key={index}
                    className={`p-3 rounded-lg border ${
                      update.isSecurityUpdate
                        ? 'border-red-200 bg-red-50'
                        : 'border-gray-200 bg-gray-50'
                    }`}
                  >
                    <div className="flex items-start justify-between">
                      <div className="flex-1 min-w-0">
                        <p className="text-sm font-medium text-text-primary truncate">
                          {update.title}
                        </p>
                        <div className="flex items-center gap-2 mt-1 text-xs text-text-secondary">
                          {update.kb && <span className="font-mono">{update.kb}</span>}
                          {update.severity && (
                            <span className={`px-1.5 py-0.5 rounded ${
                              update.severity === 'Critical' ? 'bg-red-100 text-red-700' :
                              update.severity === 'Important' ? 'bg-orange-100 text-orange-700' :
                              'bg-gray-100 text-gray-700'
                            }`}>
                              {update.severity}
                            </span>
                          )}
                          {update.sizeMB && update.sizeMB > 0 && (
                            <span>{update.sizeMB} MB</span>
                          )}
                        </div>
                      </div>
                      {update.isSecurityUpdate && (
                        <span className="ml-2 px-2 py-0.5 text-xs bg-red-100 text-red-700 rounded-full">
                          Security
                        </span>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* No updates message */}
          {(!updateStatus.pendingUpdates || updateStatus.pendingUpdates.length === 0) && !hasUpdates && (
            <div className="p-4 text-center text-sm text-text-secondary">
              <CheckIcon className="w-8 h-8 mx-auto mb-2 text-green-500" />
              <p>This device is up to date</p>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// Icon components
function WindowsIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor">
      <path d="M0 3.449L9.75 2.1v9.451H0m10.949-9.602L24 0v11.4H10.949M0 12.6h9.75v9.451L0 20.699M10.949 12.6H24V24l-12.9-1.801"/>
    </svg>
  );
}

function ChevronIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
    </svg>
  );
}

function RebootIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
    </svg>
  );
}

function CheckIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
    </svg>
  );
}
