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
    bootTime?: number;
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
          value: latestMetrics?.gpuMetrics?.[idx]?.utilization ?? 0,
          unit: '%',
          color: '#4cc2ff',
        });
      });
    }

    return items;
  }, [latestMetrics, systemInfo]);

  const selectedItem = resources.find(r => r.id === selectedResource) ?? resources[0];

  // Get metrics for graph - show all available (store handles the sliding window)
  const graphMetrics = useMemo(() => {
    return [...metrics].reverse();
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
        {/* Debug info - remove after testing */}
        {metrics.length === 0 && (
          <div className="mb-4 p-3 bg-yellow-100 border border-yellow-300 rounded text-yellow-800 text-sm">
            No metrics data received. Make sure the agent is connected and sending heartbeats.
          </div>
        )}
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
            latestMetrics={latestMetrics}
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
  const cpuPerCore = latestMetrics?.cpuPerCore ?? [];
  const topProcesses = latestMetrics?.topProcesses ?? [];

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

      {/* Per-core CPU usage grid (Task Manager style) */}
      {cpuPerCore.length > 0 && (
        <div className="mt-4">
          <div className="text-sm text-text-secondary mb-2">Logical processors</div>
          <div className="grid gap-1" style={{ gridTemplateColumns: `repeat(${Math.min(cpuPerCore.length, 8)}, 1fr)` }}>
            {cpuPerCore.map((corePercent, idx) => (
              <div key={idx} className="relative h-10 bg-gray-100 border border-border rounded overflow-hidden" title={`Core ${idx}: ${corePercent.toFixed(0)}%`}>
                <div
                  className="absolute bottom-0 left-0 right-0 bg-[#0078d4] transition-all duration-300"
                  style={{ height: `${Math.min(corePercent, 100)}%` }}
                />
                <div className="absolute inset-0 flex items-center justify-center text-xs font-medium">
                  {corePercent.toFixed(0)}%
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

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

      {/* Top processes by CPU (Task Manager style) */}
      {topProcesses.length > 0 && (
        <div className="mt-6">
          <div className="text-sm text-text-secondary mb-2">Top processes by CPU</div>
          <div className="bg-gray-50 border border-border rounded overflow-hidden">
            <table className="w-full text-sm">
              <thead className="bg-gray-100 border-b border-border">
                <tr>
                  <th className="text-left px-3 py-2 font-medium">Name</th>
                  <th className="text-right px-3 py-2 font-medium w-20">PID</th>
                  <th className="text-right px-3 py-2 font-medium w-20">CPU</th>
                  <th className="text-right px-3 py-2 font-medium w-24">Memory</th>
                </tr>
              </thead>
              <tbody>
                {topProcesses.slice(0, 10).map((proc, idx) => (
                  <tr key={proc.pid} className={idx % 2 === 0 ? 'bg-white' : 'bg-gray-50'}>
                    <td className="px-3 py-1.5 truncate max-w-[200px]" title={proc.name}>{proc.name}</td>
                    <td className="px-3 py-1.5 text-right text-text-secondary">{proc.pid}</td>
                    <td className="px-3 py-1.5 text-right">
                      <span className={proc.cpuPercent > 50 ? 'text-orange-600 font-medium' : ''}>
                        {proc.cpuPercent.toFixed(1)}%
                      </span>
                    </td>
                    <td className="px-3 py-1.5 text-right text-text-secondary">
                      {formatBytes(proc.memoryRss)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
}

// Memory Detail View
function MemoryDetailView({ metrics, systemInfo, latestMetrics }: DetailViewProps) {
  const memPercent = latestMetrics?.memoryPercent ?? 0;
  const memUsed = latestMetrics?.memoryUsedBytes ?? 0;
  const memTotal = systemInfo?.totalMemory ?? 0;
  const memAvailable = memTotal - memUsed;

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
        <StatRow label="Committed" value={latestMetrics?.memoryCommitted ? formatBytes(latestMetrics.memoryCommitted) : 'N/A'} />
        <StatRow label="Cached" value={latestMetrics?.memoryCached ? formatBytes(latestMetrics.memoryCached) : 'N/A'} />
        <StatRow label="Paged pool" value={latestMetrics?.memoryPagedPool ? formatBytes(latestMetrics.memoryPagedPool) : 'N/A'} />
        <StatRow label="Non-paged pool" value={latestMetrics?.memoryNonPagedPool ? formatBytes(latestMetrics.memoryNonPagedPool) : 'N/A'} />
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
          <div className="text-sm text-text-secondary">Used</div>
        </div>
      </div>

      <PerformanceGraph
        metrics={metrics}
        dataKey="diskPercent"
        color="#00b294"
        label="Disk usage"
        maxValue={100}
      />

      <div className="grid grid-cols-2 gap-x-12 gap-y-2 mt-6 text-sm">
        <StatRow label="Used" value={`${diskPercent.toFixed(0)}%`} />
        <StatRow label="Average response time" value="N/A" />
        <StatRow label="Read speed" value={latestMetrics?.diskReadBytesPerSec ? formatBytesPerSec(latestMetrics.diskReadBytesPerSec) : 'N/A'} />
        <StatRow label="Write speed" value={latestMetrics?.diskWriteBytesPerSec ? formatBytesPerSec(latestMetrics.diskWriteBytesPerSec) : 'N/A'} />
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
  latestMetrics?: DeviceMetrics | null;
}

function GPUDetailView({ systemInfo, gpuIndex, latestMetrics }: GPUDetailViewProps) {
  const gpu = systemInfo?.gpu?.[gpuIndex];
  const gpuMetric = latestMetrics?.gpuMetrics?.[gpuIndex];
  const utilization = gpuMetric?.utilization ?? 0;
  const hasUtilization = gpuMetric && gpuMetric.utilization > 0;

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <div>
          <h2 className="text-2xl font-light">GPU {gpuIndex}</h2>
          <div className="text-sm text-text-secondary">{gpuMetric?.name ?? gpu?.name ?? 'Unknown GPU'}</div>
        </div>
        <div className="text-right">
          <div className="text-4xl font-light">{utilization.toFixed(0)}%</div>
          <div className="text-sm text-text-secondary">Utilization</div>
        </div>
      </div>

      {!hasUtilization ? (
        <div className="bg-gray-50 border border-border p-4 rounded mb-6">
          <div className="text-sm text-text-secondary mb-2">Real-time GPU utilization not available</div>
          <div className="text-xs text-text-secondary">NVIDIA GPUs with nvidia-smi support real-time monitoring</div>
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-4 mb-6">
          <div className="bg-gray-50 border border-border p-4 rounded">
            <div className="text-sm text-text-secondary mb-2">GPU Memory</div>
            <div className="text-2xl font-light">
              {gpuMetric.memoryUsed && gpuMetric.memoryTotal
                ? `${formatBytes(gpuMetric.memoryUsed)} / ${formatBytes(gpuMetric.memoryTotal)}`
                : 'N/A'}
            </div>
          </div>
          <div className="bg-gray-50 border border-border p-4 rounded">
            <div className="text-sm text-text-secondary mb-2">Temperature</div>
            <div className="text-2xl font-light">
              {gpuMetric.temperature ? `${gpuMetric.temperature.toFixed(0)}°C` : 'N/A'}
            </div>
          </div>
        </div>
      )}

      <div className="grid grid-cols-2 gap-x-12 gap-y-2 mt-6 text-sm">
        <StatRow label="GPU" value={gpuMetric?.name ?? gpu?.name ?? 'N/A'} />
        <StatRow label="Vendor" value={gpu?.vendor ?? 'N/A'} />
        <StatRow label="Utilization" value={`${utilization.toFixed(0)}%`} />
        <StatRow label="Temperature" value={gpuMetric?.temperature ? `${gpuMetric.temperature.toFixed(0)}°C` : 'N/A'} />
        <StatRow label="GPU Memory Used" value={gpuMetric?.memoryUsed ? formatBytes(gpuMetric.memoryUsed) : 'N/A'} />
        <StatRow label="GPU Memory Total" value={gpuMetric?.memoryTotal ? formatBytes(gpuMetric.memoryTotal) : (gpu?.memory ? formatBytes(gpu.memory) : 'N/A')} />
        <StatRow label="Power Draw" value={gpuMetric?.powerDraw ? `${gpuMetric.powerDraw.toFixed(0)} W` : 'N/A'} />
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
  const canvasRef = React.useRef<HTMLCanvasElement>(null);
  const animatedValuesRef = React.useRef<number[]>([]);
  const animationRef = React.useRef<number>(0);
  const TOTAL_POINTS = 60;

  // Get target values (padded to 60 points)
  const targetValues = useMemo(() => {
    const vals = metrics.map(m => (m[dataKey] as number) ?? 0);
    while (vals.length < TOTAL_POINTS) {
      vals.unshift(0);
    }
    return vals.slice(-TOTAL_POINTS);
  }, [metrics, dataKey]);

  // Animation loop for smooth flowing effect
  React.useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    // Initialize animated values if empty
    if (animatedValuesRef.current.length !== TOTAL_POINTS) {
      animatedValuesRef.current = new Array(TOTAL_POINTS).fill(0);
    }

    const width = canvas.width;
    const height = canvas.height;
    const padding = 4;
    const lerp = 0.15; // Interpolation speed (higher = faster)

    const draw = () => {
      // Interpolate animated values toward target values
      let needsAnimation = false;
      for (let i = 0; i < TOTAL_POINTS; i++) {
        const target = targetValues[i] ?? 0;
        const current = animatedValuesRef.current[i];
        const diff = target - current;
        if (Math.abs(diff) > 0.1) {
          animatedValuesRef.current[i] = current + diff * lerp;
          needsAnimation = true;
        } else {
          animatedValuesRef.current[i] = target;
        }
      }

      // Clear and draw background
      ctx.fillStyle = '#f8fafc';
      ctx.fillRect(0, 0, width, height);

      // Draw grid lines
      ctx.strokeStyle = '#e2e8f0';
      ctx.lineWidth = 1;
      [25, 50, 75].forEach(pct => {
        const y = height - (pct / 100) * (height - padding * 2) - padding;
        ctx.beginPath();
        ctx.moveTo(0, y);
        ctx.lineTo(width, y);
        ctx.stroke();
      });

      const values = animatedValuesRef.current;
      const step = (width - padding * 2) / (TOTAL_POINTS - 1);
      const points = values.map((v, i) => ({
        x: padding + i * step,
        y: height - padding - (v / maxValue) * (height - padding * 2),
      }));

      // Draw filled area
      ctx.beginPath();
      ctx.moveTo(points[0].x, height - padding);
      points.forEach(p => ctx.lineTo(p.x, p.y));
      ctx.lineTo(points[points.length - 1].x, height - padding);
      ctx.closePath();
      ctx.fillStyle = color + '33';
      ctx.fill();

      // Draw line
      ctx.beginPath();
      ctx.moveTo(points[0].x, points[0].y);
      points.forEach((p, i) => {
        if (i > 0) ctx.lineTo(p.x, p.y);
      });
      ctx.strokeStyle = color;
      ctx.lineWidth = 2;
      ctx.stroke();

      // Continue animation if needed
      if (needsAnimation) {
        animationRef.current = requestAnimationFrame(draw);
      }
    };

    // Start animation
    animationRef.current = requestAnimationFrame(draw);

    return () => {
      if (animationRef.current) {
        cancelAnimationFrame(animationRef.current);
      }
    };
  }, [targetValues, color, maxValue]);

  return (
    <div className="relative">
      <div className="absolute top-2 left-2 text-xs text-text-secondary z-10">{label}</div>
      <div className="absolute top-2 right-2 text-xs text-text-secondary z-10">60 seconds</div>
      <div className="bg-gray-50 border border-border rounded overflow-hidden" style={{ height: '200px' }}>
        <canvas
          ref={canvasRef}
          width={800}
          height={200}
          className="w-full h-full"
        />
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
