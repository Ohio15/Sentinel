import React from 'react';
import { useDeviceStore, GPUInfo, StorageInfo } from '../stores/deviceStore';

interface OverviewMetricsProps {
  deviceId: string;
  totalMemory?: number;
  gpu?: GPUInfo[];
  storage?: StorageInfo[];
}

// Helper function to format bytes
function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

// Helper function to format uptime in seconds to days, hours, minutes
function formatUptime(seconds: number): string {
  if (!seconds || seconds <= 0) return 'N/A';
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);

  const parts: string[] = [];
  if (days > 0) parts.push(`${days}d`);
  if (hours > 0) parts.push(`${hours}h`);
  if (minutes > 0 || parts.length === 0) parts.push(`${minutes}m`);

  return parts.join(' ');
}

export function OverviewMetrics({ deviceId, totalMemory, gpu, storage }: OverviewMetricsProps) {
  // Read metrics from store - component re-renders automatically when metrics update
  // Note: App.tsx handles the subscription to updates, child components just read from store
  const metrics = useDeviceStore((state) => state.metrics);

  const latestMetrics = metrics.length > 0 ? metrics[0] : null;

  // Use live metrics for storage when available, fall back to enrollment data
  const enrollmentStorage = storage?.reduce((sum, s) => sum + (s.total || 0), 0) || 0;
  const enrollmentUsed = storage?.reduce((sum, s) => sum + (s.used || 0), 0) || 0;
  const totalStorage = latestMetrics?.diskTotalBytes || enrollmentStorage;
  const usedStorage = latestMetrics?.diskUsedBytes || enrollmentUsed;

  // Get GPU info
  const gpuName = gpu?.[0]?.name || 'Unknown GPU';
  const gpuMemory = gpu?.[0]?.memory;

  // Memory usage
  const memoryUsed = latestMetrics?.memoryUsedBytes || 0;

  return (
    <div className="grid grid-cols-5 gap-4">
      {/* Storage Card */}
      <div className="bg-gradient-to-br from-blue-500 to-blue-700 rounded-xl p-4 text-white shadow-lg">
        <div className="flex items-center gap-3 mb-3">
          <HardDriveIcon className="w-8 h-8" />
          <span className="text-lg font-medium">Storage</span>
        </div>
        <div className="text-2xl font-bold">
          {totalStorage ? `${formatBytes(usedStorage)} / ${formatBytes(totalStorage)}` : 'N/A'}
        </div>
        <div className="text-sm opacity-80 mt-1">
          {totalStorage && latestMetrics?.diskPercent != null
            ? `${(100 - latestMetrics.diskPercent).toFixed(0)}% free`
            : ''}
        </div>
      </div>

      {/* GPU Card */}
      <div className="bg-gradient-to-br from-purple-500 to-purple-700 rounded-xl p-4 text-white shadow-lg">
        <div className="flex items-center gap-3 mb-3">
          <GpuIcon className="w-8 h-8" />
          <span className="text-lg font-medium">Graphics</span>
        </div>
        <div className="text-lg font-bold truncate" title={gpuName}>
          {gpuName}
        </div>
        <div className="text-sm opacity-80 mt-1">
          {gpuMemory ? formatBytes(gpuMemory) : ''}
        </div>
      </div>

      {/* RAM Card */}
      <div className="bg-gradient-to-br from-teal-500 to-teal-700 rounded-xl p-4 text-white shadow-lg">
        <div className="flex items-center gap-3 mb-3">
          <MemoryIcon className="w-8 h-8" />
          <span className="text-lg font-medium">RAM</span>
        </div>
        <div className="text-2xl font-bold">
          {totalMemory ? formatBytes(totalMemory) : 'N/A'}
        </div>
        <div className="text-sm opacity-80 mt-1">
          {latestMetrics?.memoryPercent != null
            ? `${latestMetrics.memoryPercent.toFixed(0)}% in use`
            : ''}
        </div>
      </div>

      {/* CPU Card */}
      <div className="bg-gradient-to-br from-orange-500 to-orange-700 rounded-xl p-4 text-white shadow-lg">
        <div className="flex items-center gap-3 mb-3">
          <CpuIcon className="w-8 h-8" />
          <span className="text-lg font-medium">Processor</span>
        </div>
        <div className="text-2xl font-bold">
          {latestMetrics?.cpuPercent != null
            ? `${latestMetrics.cpuPercent.toFixed(0)}%`
            : 'N/A'}
        </div>
        <div className="text-sm opacity-80 mt-1">
          {latestMetrics?.cpuPercent != null ? 'utilization' : ''}
        </div>
      </div>

      {/* Uptime Card */}
      <div className="bg-gradient-to-br from-green-500 to-green-700 rounded-xl p-4 text-white shadow-lg">
        <div className="flex items-center gap-3 mb-3">
          <ClockIcon className="w-8 h-8" />
          <span className="text-lg font-medium">Uptime</span>
        </div>
        <div className="text-2xl font-bold">
          {latestMetrics?.uptime ? formatUptime(latestMetrics.uptime) : 'N/A'}
        </div>
        <div className="text-sm opacity-80 mt-1">
          Since last boot
        </div>
      </div>
    </div>
  );
}

// Icons
function HardDriveIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4m0 5c0 2.21-3.582 4-8 4s-8-1.79-8-4" />
    </svg>
  );
}

function GpuIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z" />
    </svg>
  );
}

function MemoryIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
    </svg>
  );
}

function CpuIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z" />
    </svg>
  );
}

function ClockIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
    </svg>
  );
}
