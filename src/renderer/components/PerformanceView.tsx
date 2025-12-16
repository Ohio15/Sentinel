import React, { useMemo, useState } from 'react';
import { DeviceMetrics } from '../stores/deviceStore';

interface PerformanceViewProps {
  metrics: DeviceMetrics[];
  systemInfo?: {
    cpuModel?: string;
    cpuCores?: number;
    cpuThreads?: number;
    cpuSpeed?: number;
    totalMemory?: number;
    gpu?: Array<{ name: string; vendor: string; memory: number; driverVersion: string }>;
    storage?: Array<{ device: string; mountpoint: string; fstype: string; total: number; used: number; free: number; percent: number }>;
  };
}

type ResourceType = 'cpu' | 'memory' | 'disk' | 'network' | 'gpu';

interface ResourceItem {
  id: string;
  type: ResourceType;
  label: string;
  sublabel?: string;
  value: number;
  unit: string;
  color: string;
}

export function PerformanceView({ metrics, systemInfo }: PerformanceViewProps) {
  const [selectedResource, setSelectedResource] = useState<string>('cpu');

  // Get the latest metrics
  const latestMetrics = metrics.length > 0 ? metrics[0] : null;

  // Build resource list
  const resources = useMemo<ResourceItem[]>(() => {
    const items: ResourceItem[] = [
      {
        id: 'cpu',
        type: 'cpu',
        label: 'CPU',
        sublabel: systemInfo?.cpuModel ? systemInfo.cpuModel.split(' ').slice(0, 3).join(' ') : undefined,
        value: latestMetrics?.cpuPercent ?? 0,
        unit: '%',
        color: '#0078d4',
      },
      {
        id: 'memory',
        type: 'memory',
        label: 'Memory',
        sublabel: systemInfo?.totalMemory ? `${formatBytes(latestMetrics?.memoryUsedBytes ?? 0)}/${formatBytes(systemInfo.totalMemory)}` : undefined,
        value: latestMetrics?.memoryPercent ?? 0,
        unit: '%',
        color: '#8764b8',
      },
    ];

    // Add disks
    if (systemInfo?.storage) {
      systemInfo.storage.forEach((disk, idx) => {
        items.push({
          id: `disk-${idx}`,
          type: 'disk',
          label: `Disk ${idx}`,
          sublabel: disk.mountpoint,
          value: disk.percent ?? 0,
          unit: '%',
          color: '#00b294',
        });
      });
    } else if (latestMetrics?.diskPercent !== undefined) {
      items.push({
        id: 'disk-0',
        type: 'disk',
        label: 'Disk 0',
        sublabel: 'C:',
        value: latestMetrics.diskPercent,
        unit: '%',
        color: '#00b294',
      });
    }

    // Add network
    items.push({
      id: 'network',
      type: 'network',
      label: 'Ethernet',
      sublabel: latestMetrics ? `↓${formatBytesPerSec(latestMetrics.networkRxBytes ?? 0)} ↑${formatBytesPerSec(latestMetrics.networkTxBytes ?? 0)}` : undefined,
      value: 0,
      unit: '',
      color: '#d48c00',
    });

    // Add GPUs
    if (systemInfo?.gpu) {
      systemInfo.gpu.forEach((gpu, idx) => {
        items.push({
          id: `gpu-${idx}`,
          type: 'gpu',
          label: `GPU ${idx}`,
          sublabel: gpu.name,
          value: 0,
          unit: '%',
          color: '#4cc2ff',
        });
      });
    }

    return items;
  }, [latestMetrics, systemInfo]);

  const selectedItem = resources.find(r => r.id === selectedResource) ?? resources[0];

  // Get metrics for graph (last 60 seconds worth)
  const graphMetrics = useMemo(() => {
    const sorted = [...metrics].reverse();
    return sorted.slice(-60);
  }, [metrics]);

  return (
    <div className="flex h-full bg-background text-text-primary">
      {/* Left sidebar - resource list */}
      <div className="w-72 bg-surface border-r border-border overflow-y-auto">
        {resources.map((resource) => (
          <ResourceSidebarItem
            key={resource.id}
            resource={resource}
            isSelected={selectedResource === resource.id}
            onClick={() => setSelectedResource(resource.id)}
            metrics={graphMetrics}
          />
        ))}
      </div>

      {/* Main content area */}
      <div className="flex-1 p-6 overflow-y-auto">
        {selectedItem.type === 'cpu' && (
          <CPUDetailView metrics={graphMetrics} systemInfo={systemInfo} latestMetrics={latestMetrics} />
        )}
        {selectedItem.type === 'memory' && (
          <MemoryDetailView metrics={graphMetrics} systemInfo={systemInfo} latestMetrics={latestMetrics} />
        )}
        {selectedItem.type === 'disk' && (
          <DiskDetailView
            metrics={graphMetrics}
            systemInfo={systemInfo}
            latestMetrics={latestMetrics}
            diskIndex={parseInt(selectedItem.id.split('-')[1] || '0')}
          />
        )}
        {selectedItem.type === 'network' && (
          <NetworkDetailView metrics={graphMetrics} latestMetrics={latestMetrics} />
        )}
        {selectedItem.type === 'gpu' && (
          <GPUDetailView
            systemInfo={systemInfo}
            gpuIndex={parseInt(selectedItem.id.split('-')[1] || '0')}
          />
        )}
      </div>
    </div>
  );
}

// Sidebar item component
interface ResourceSidebarItemProps {
  resource: ResourceItem;
  isSelected: boolean;
  onClick: () => void;
  metrics: DeviceMetrics[];
}

function ResourceSidebarItem({ resource, isSelected, onClick, metrics }: ResourceSidebarItemProps) {
  const miniGraphPath = useMemo(() => {
    if (metrics.length < 2) return '';

    const width = 60;
    const height = 30;
    const values = metrics.slice(-30).map(m => {
      switch (resource.type) {
        case 'cpu': return m.cpuPercent ?? 0;
        case 'memory': return m.memoryPercent ?? 0;
        case 'disk': return m.diskPercent ?? 0;
        default: return 0;
      }
    });

    if (values.length < 2) return '';

    const step = width / (values.length - 1);
    const points = values.map((v, i) => `${i * step},${height - (v / 100) * height}`);
    return `M ${points.join(' L ')}`;
  }, [metrics, resource.type]);

  return (
    <div
      className={`p-3 cursor-pointer border-l-2 transition-colors ${
        isSelected
          ? 'bg-primary-light border-primary'
          : 'border-transparent hover:bg-gray-100'
      }`}
      onClick={onClick}
    >
      <div className="flex items-center justify-between">
        <div className="flex-1 min-w-0">
          <div className="font-medium text-sm">{resource.label}</div>
          {resource.sublabel && (
            <div className="text-xs text-text-secondary truncate">{resource.sublabel}</div>
          )}
          <div className="text-xs mt-1" style={{ color: resource.color }}>
            {resource.value.toFixed(0)}{resource.unit}
          </div>
        </div>
        <div className="w-16 h-8 ml-2">
          <svg viewBox="0 0 60 30" className="w-full h-full">
            <rect x="0" y="0" width="60" height="30" fill="#f8fafc" stroke="#e2e8f0" strokeWidth="1" />
            {miniGraphPath && (
              <path d={miniGraphPath} fill="none" stroke={resource.color} strokeWidth="1.5" />
            )}
          </svg>
        </div>
      </div>
    </div>
  );
}

// CPU Detail View
interface DetailViewProps {
  metrics: DeviceMetrics[];
  systemInfo?: PerformanceViewProps['systemInfo'];
  latestMetrics: DeviceMetrics | null;
}

function CPUDetailView({ metrics, systemInfo, latestMetrics }: DetailViewProps) {
  const cpuPercent = latestMetrics?.cpuPercent ?? 0;

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <div>
          <h2 className="text-2xl font-light">CPU</h2>
          <div className="text-sm text-text-secondary">{systemInfo?.cpuModel || 'Unknown CPU'}</div>
        </div>
        <div className="text-right">
          <div className="text-4xl font-light">{cpuPercent.toFixed(0)}%</div>
          <div className="text-sm text-text-secondary">Utilization</div>
        </div>
      </div>

      <PerformanceGraph
        metrics={metrics}
        dataKey="cpuPercent"
        color="#0078d4"
        label="% Utilization"
        maxValue={100}
      />

      <div className="grid grid-cols-2 gap-x-12 gap-y-2 mt-6 text-sm">
        <StatRow label="Utilization" value={`${cpuPercent.toFixed(0)}%`} />
        <StatRow label="Speed" value={systemInfo?.cpuSpeed ? `${(systemInfo.cpuSpeed / 1000).toFixed(2)} GHz` : 'N/A'} />
        <StatRow label="Processes" value={latestMetrics?.processCount?.toString() ?? 'N/A'} />
        <StatRow label="Threads" value="N/A" />
        <StatRow label="Handles" value="N/A" />
        <StatRow label="Up time" value={formatUptime(latestMetrics?.uptime ?? 0)} />
        <div className="col-span-2 border-t border-border my-2" />
        <StatRow label="Base speed" value={systemInfo?.cpuSpeed ? `${(systemInfo.cpuSpeed / 1000).toFixed(2)} GHz` : 'N/A'} />
        <StatRow label="Sockets" value="1" />
        <StatRow label="Cores" value={systemInfo?.cpuCores?.toString() ?? 'N/A'} />
        <StatRow label="Logical processors" value={systemInfo?.cpuThreads?.toString() ?? 'N/A'} />
      </div>
    </div>
  );
}

// Memory Detail View
function MemoryDetailView({ metrics, systemInfo, latestMetrics }: DetailViewProps) {
  const memPercent = latestMetrics?.memoryPercent ?? 0;
  const memUsed = latestMetrics?.memoryUsedBytes ?? 0;
  const memAvailable = memTotal - memUsed;
  const memTotal = systemInfo?.totalMemory ?? 0;

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <div>
          <h2 className="text-2xl font-light">Memory</h2>
          <div className="text-sm text-text-secondary">{formatBytes(memTotal)} Total</div>
        </div>
        <div className="text-right">
          <div className="text-4xl font-light">{memPercent.toFixed(0)}%</div>
          <div className="text-sm text-text-secondary">{formatBytes(memUsed)} in use</div>
        </div>
      </div>

      <PerformanceGraph
        metrics={metrics}
        dataKey="memoryPercent"
        color="#8764b8"
        label="Memory usage"
        maxValue={100}
      />

      {/* Memory composition bar */}
      <div className="mt-4 h-12 bg-gray-100 border border-border rounded flex overflow-hidden">
        <div
          className="bg-[#8764b8] flex items-center justify-center text-xs"
          style={{ width: `${memPercent}%` }}
        >
          In Use
        </div>
        <div
          className="bg-gray-200 flex items-center justify-center text-xs text-text-secondary"
          style={{ width: `${100 - memPercent}%` }}
        >
          Available
        </div>
      </div>

      <div className="grid grid-cols-2 gap-x-12 gap-y-2 mt-6 text-sm">
        <StatRow label="In use" value={formatBytes(memUsed)} />
        <StatRow label="Available" value={formatBytes(memAvailable)} />
        <StatRow label="Committed" value="N/A" />
        <StatRow label="Cached" value="N/A" />
        <StatRow label="Paged pool" value="N/A" />
        <StatRow label="Non-paged pool" value="N/A" />
      </div>
    </div>
  );
}

// Disk Detail View
interface DiskDetailViewProps extends DetailViewProps {
  diskIndex: number;
}

function DiskDetailView({ metrics, systemInfo, latestMetrics, diskIndex }: DiskDetailViewProps) {
  const disk = systemInfo?.storage?.[diskIndex];
  const diskPercent = disk?.percent ?? latestMetrics?.diskPercent ?? 0;

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <div>
          <h2 className="text-2xl font-light">Disk {diskIndex}</h2>
          <div className="text-sm text-text-secondary">{disk?.mountpoint ?? 'C:'} - {disk?.fstype ?? 'NTFS'}</div>
        </div>
        <div className="text-right">
          <div className="text-4xl font-light">{diskPercent.toFixed(0)}%</div>
          <div className="text-sm text-text-secondary">Active time</div>
        </div>
      </div>

      <PerformanceGraph
        metrics={metrics}
        dataKey="diskPercent"
        color="#00b294"
        label="Active time"
        maxValue={100}
      />

      <div className="grid grid-cols-2 gap-x-12 gap-y-2 mt-6 text-sm">
        <StatRow label="Active time" value={`${diskPercent.toFixed(0)}%`} />
        <StatRow label="Average response time" value="N/A" />
        <StatRow label="Read speed" value="N/A" />
        <StatRow label="Write speed" value="N/A" />
        <div className="col-span-2 border-t border-border my-2" />
        <StatRow label="Capacity" value={formatBytes(disk?.total ?? latestMetrics?.diskTotalBytes ?? 0)} />
        <StatRow label="Used" value={formatBytes(disk?.used ?? latestMetrics?.diskUsedBytes ?? 0)} />
        <StatRow label="Free" value={formatBytes(disk?.free ?? 0)} />
        <StatRow label="Type" value={disk?.fstype ?? 'NTFS'} />
      </div>
    </div>
  );
}

// Network Detail View
interface NetworkDetailViewProps {
  metrics: DeviceMetrics[];
  latestMetrics: DeviceMetrics | null;
}

function NetworkDetailView({ metrics, latestMetrics }: NetworkDetailViewProps) {
  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <div>
          <h2 className="text-2xl font-light">Ethernet</h2>
          <div className="text-sm text-text-secondary">Network Adapter</div>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-4 mb-6">
        <div className="bg-gray-50 border border-border p-4 rounded">
          <div className="text-sm text-text-secondary mb-2">Send</div>
          <div className="text-2xl font-light">{formatBytesPerSec(latestMetrics?.networkTxBytes ?? 0)}</div>
        </div>
        <div className="bg-gray-50 border border-border p-4 rounded">
          <div className="text-sm text-text-secondary mb-2">Receive</div>
          <div className="text-2xl font-light">{formatBytesPerSec(latestMetrics?.networkRxBytes ?? 0)}</div>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-x-12 gap-y-2 mt-6 text-sm">
        <StatRow label="Send" value={formatBytesPerSec(latestMetrics?.networkTxBytes ?? 0)} />
        <StatRow label="Receive" value={formatBytesPerSec(latestMetrics?.networkRxBytes ?? 0)} />
        <StatRow label="Total sent" value={formatBytes(latestMetrics?.networkTxBytes ?? 0)} />
        <StatRow label="Total received" value={formatBytes(latestMetrics?.networkRxBytes ?? 0)} />
      </div>
    </div>
  );
}

// GPU Detail View
interface GPUDetailViewProps {
  systemInfo?: PerformanceViewProps['systemInfo'];
  gpuIndex: number;
}

function GPUDetailView({ systemInfo, gpuIndex }: GPUDetailViewProps) {
  const gpu = systemInfo?.gpu?.[gpuIndex];

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <div>
          <h2 className="text-2xl font-light">GPU {gpuIndex}</h2>
          <div className="text-sm text-text-secondary">{gpu?.name ?? 'Unknown GPU'}</div>
        </div>
        <div className="text-right">
          <div className="text-4xl font-light">0%</div>
          <div className="text-sm text-text-secondary">Utilization</div>
        </div>
      </div>

      <div className="bg-gray-50 border border-border p-4 rounded mb-6">
        <div className="text-sm text-text-secondary mb-2">GPU utilization data not available</div>
        <div className="text-xs text-text-secondary">GPU monitoring requires additional drivers</div>
      </div>

      <div className="grid grid-cols-2 gap-x-12 gap-y-2 mt-6 text-sm">
        <StatRow label="GPU" value={gpu?.name ?? 'N/A'} />
        <StatRow label="Vendor" value={gpu?.vendor ?? 'N/A'} />
        <StatRow label="Dedicated GPU memory" value={gpu?.memory ? formatBytes(gpu.memory) : 'N/A'} />
        <StatRow label="Driver version" value={gpu?.driverVersion ?? 'N/A'} />
      </div>
    </div>
  );
}

// Performance graph component
interface PerformanceGraphProps {
  metrics: DeviceMetrics[];
  dataKey: keyof DeviceMetrics;
  color: string;
  label: string;
  maxValue: number;
}

function PerformanceGraph({ metrics, dataKey, color, label, maxValue }: PerformanceGraphProps) {
  const { path, areaPath } = useMemo(() => {
    if (metrics.length < 2) return { path: '', areaPath: '' };

    const width = 800;
    const height = 200;
    const padding = 2;

    const values = metrics.map(m => (m[dataKey] as number) ?? 0);
    const step = (width - padding * 2) / (values.length - 1);

    const points = values.map((v, i) => {
      const x = padding + i * step;
      const y = height - padding - (v / maxValue) * (height - padding * 2);
      return { x, y };
    });

    const pathD = points.map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x.toFixed(1)} ${p.y.toFixed(1)}`).join(' ');

    const firstX = points[0].x.toFixed(1);
    const lastX = points[points.length - 1].x.toFixed(1);
    const bottomY = (height - padding).toFixed(1);
    const areaD = `${pathD} L ${lastX} ${bottomY} L ${firstX} ${bottomY} Z`;

    return { path: pathD, areaPath: areaD };
  }, [metrics, dataKey, maxValue]);

  return (
    <div className="relative">
      <div className="absolute top-2 left-2 text-xs text-text-secondary">{label}</div>
      <div className="absolute top-2 right-2 text-xs text-text-secondary">60 seconds</div>
      <div className="bg-gray-50 border border-border rounded overflow-hidden" style={{ height: '200px' }}>
        <svg viewBox="0 0 800 200" className="w-full h-full" preserveAspectRatio="none">
          {/* Grid lines */}
          {[25, 50, 75].map(pct => (
            <line
              key={pct}
              x1="0"
              y1={200 - (pct / 100) * 196}
              x2="800"
              y2={200 - (pct / 100) * 196}
              stroke="#e2e8f0"
              strokeWidth="1"
            />
          ))}

          {/* Area fill */}
          {areaPath && (
            <path d={areaPath} fill={color} fillOpacity="0.2" />
          )}

          {/* Line */}
          {path && (
            <path d={path} fill="none" stroke={color} strokeWidth="2" />
          )}
        </svg>
      </div>

      {/* Y-axis labels */}
      <div className="absolute right-2 top-8 bottom-2 flex flex-col justify-between text-xs text-text-secondary">
        <span>100%</span>
        <span>50%</span>
        <span>0%</span>
      </div>
    </div>
  );
}

// Stat row component
function StatRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between">
      <span className="text-text-secondary">{label}</span>
      <span className="text-text-primary font-medium">{value}</span>
    </div>
  );
}

// Helper functions
function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`;
}

function formatBytesPerSec(bytes: number): string {
  return `${formatBytes(bytes)}/s`;
}

function formatUptime(seconds: number): string {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const mins = Math.floor((seconds % 3600) / 60);
  const secs = seconds % 60;

  if (days > 0) {
    return `${days}:${hours.toString().padStart(2, '0')}:${mins.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`;
  }
  return `${hours}:${mins.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`;
}
