import React, { useMemo, useState, useEffect, useRef, useCallback } from 'react';
import { DeviceMetrics } from '../stores/deviceStore';

// Smooth animated value hook with exponential ease-out
function useAnimatedValue(targetValue: number, duration: number = 250): number {
  const [displayValue, setDisplayValue] = useState(targetValue);
  const animationRef = useRef<number>(0);
  const startTimeRef = useRef<number>(0);
  const startValueRef = useRef<number>(targetValue);
  const targetRef = useRef<number>(targetValue);

  useEffect(() => {
    targetRef.current = targetValue;
    if (animationRef.current) cancelAnimationFrame(animationRef.current);
    startValueRef.current = displayValue;
    startTimeRef.current = performance.now();

    const animate = (currentTime: number) => {
      const elapsed = currentTime - startTimeRef.current;
      const progress = Math.min(elapsed / duration, 1);
      const easeOut = 1 - Math.pow(2, -10 * progress);
      const newValue = startValueRef.current + (targetRef.current - startValueRef.current) * easeOut;
      setDisplayValue(newValue);
      if (progress < 1) animationRef.current = requestAnimationFrame(animate);
    };
    animationRef.current = requestAnimationFrame(animate);
    return () => { if (animationRef.current) cancelAnimationFrame(animationRef.current); };
  }, [targetValue, duration]);

  return displayValue;
}

// Animated byte display component
function AnimatedBytes({ value, perSecond = false, className = '' }: { value: number; perSecond?: boolean; className?: string }) {
  const animatedValue = useAnimatedValue(value, 300);
  const formatted = perSecond ? formatBytesPerSec(animatedValue) : formatBytes(animatedValue);
  return <span className={className}>{formatted}</span>;
}

// Animated number display component
function AnimatedNumber({ value, decimals = 0, suffix = '', className = '' }: { value: number; decimals?: number; suffix?: string; className?: string }) {
  const animatedValue = useAnimatedValue(value, 300);
  return <span className={className}>{animatedValue.toFixed(decimals)}{suffix}</span>;
}

// Helper functions
function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(Math.abs(bytes) || 1) / Math.log(k));
  return (bytes / Math.pow(k, i)).toFixed(1) + ' ' + sizes[Math.min(i, sizes.length - 1)];
}

function formatBytesPerSec(bytes: number): string {
  return formatBytes(bytes) + '/s';
}

function formatUptime(seconds: number): string {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const mins = Math.floor((seconds % 3600) / 60);
  if (days > 0) return days + 'd ' + hours + 'h ' + mins + 'm';
  if (hours > 0) return hours + 'h ' + mins + 'm';
  return mins + 'm';
}

interface PerformanceViewProps {
  metrics: DeviceMetrics | null;
  metricsHistory: DeviceMetrics[];
}

type ResourceType = 'cpu' | 'memory' | 'disk' | 'network' | 'gpu';

// Smooth performance graph with continuous animation
function SmoothPerformanceGraph({ 
  data, 
  color, 
  height = 120, 
  maxValue = 100,
  label,
  showGrid = true
}: { 
  data: number[]; 
  color: string; 
  height?: number; 
  maxValue?: number;
  label?: string;
  showGrid?: boolean;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const animatedDataRef = useRef<number[]>([]);
  const animationRef = useRef<number>(0);
  const targetDataRef = useRef<number[]>([]);

  useEffect(() => {
    targetDataRef.current = data;
    if (animatedDataRef.current.length === 0) {
      animatedDataRef.current = [...data];
    }
  }, [data]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();
    canvas.width = rect.width * dpr;
    canvas.height = rect.height * dpr;
    ctx.scale(dpr, dpr);
    const width = rect.width;
    const drawHeight = rect.height;

    const animate = () => {
      // Smooth lerp towards target data
      const target = targetDataRef.current;
      const animated = animatedDataRef.current;
      
      while (animated.length < target.length) animated.push(0);
      while (animated.length > target.length) animated.pop();
      
      for (let i = 0; i < animated.length; i++) {
        animated[i] += (target[i] - animated[i]) * 0.08;
      }

      // Clear canvas
      ctx.clearRect(0, 0, width, drawHeight);

      // Draw grid if enabled
      if (showGrid) {
        ctx.strokeStyle = 'rgba(255,255,255,0.05)';
        ctx.lineWidth = 1;
        for (let i = 0; i <= 4; i++) {
          const y = (drawHeight / 4) * i;
          ctx.beginPath();
          ctx.moveTo(0, y);
          ctx.lineTo(width, y);
          ctx.stroke();
        }
      }

      // Draw smooth curve
      if (animated.length > 1) {
        const gradient = ctx.createLinearGradient(0, 0, 0, drawHeight);
        gradient.addColorStop(0, color.replace(')', ', 0.3)').replace('rgb', 'rgba'));
        gradient.addColorStop(1, color.replace(')', ', 0.0)').replace('rgb', 'rgba'));

        ctx.beginPath();
        ctx.moveTo(0, drawHeight);

        const points: { x: number; y: number }[] = [];
        for (let i = 0; i < animated.length; i++) {
          const x = (i / (animated.length - 1)) * width;
          const y = drawHeight - (animated[i] / maxValue) * drawHeight;
          points.push({ x, y });
        }

        // Draw filled area with smooth curve
        ctx.moveTo(0, drawHeight);
        ctx.lineTo(points[0].x, points[0].y);
        
        for (let i = 0; i < points.length - 1; i++) {
          const xc = (points[i].x + points[i + 1].x) / 2;
          const yc = (points[i].y + points[i + 1].y) / 2;
          ctx.quadraticCurveTo(points[i].x, points[i].y, xc, yc);
        }
        ctx.lineTo(points[points.length - 1].x, points[points.length - 1].y);
        ctx.lineTo(width, drawHeight);
        ctx.closePath();
        ctx.fillStyle = gradient;
        ctx.fill();

        // Draw line on top
        ctx.beginPath();
        ctx.moveTo(points[0].x, points[0].y);
        for (let i = 0; i < points.length - 1; i++) {
          const xc = (points[i].x + points[i + 1].x) / 2;
          const yc = (points[i].y + points[i + 1].y) / 2;
          ctx.quadraticCurveTo(points[i].x, points[i].y, xc, yc);
        }
        ctx.lineTo(points[points.length - 1].x, points[points.length - 1].y);
        ctx.strokeStyle = color;
        ctx.lineWidth = 2;
        ctx.stroke();
      }

      animationRef.current = requestAnimationFrame(animate);
    };

    animationRef.current = requestAnimationFrame(animate);
    return () => cancelAnimationFrame(animationRef.current);
  }, [color, height, maxValue, showGrid]);

  return (
    <div className="relative" style={{ height }}>
      {label && (
        <div className="absolute top-2 left-2 text-xs text-text-secondary z-10">{label}</div>
      )}
      <canvas 
        ref={canvasRef} 
        className="w-full h-full"
        style={{ width: '100%', height: '100%' }}
      />
    </div>
  );
}


// Animated resource sidebar item with mini canvas graph
function AnimatedResourceSidebarItem({ 
  label, 
  value, 
  suffix, 
  history, 
  maxValue, 
  color, 
  isActive, 
  onClick 
}: {
  label: string;
  value: number;
  suffix: string;
  history: number[];
  maxValue: number;
  color: string;
  isActive: boolean;
  onClick: () => void;
}) {
  const animatedValue = useAnimatedValue(value, 300);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const animatedHistoryRef = useRef<number[]>([]);
  const animationRef = useRef<number>(0);
  const targetHistoryRef = useRef<number[]>([]);

  useEffect(() => {
    targetHistoryRef.current = history;
    if (animatedHistoryRef.current.length === 0) {
      animatedHistoryRef.current = [...history];
    }
  }, [history]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const dpr = window.devicePixelRatio || 1;
    canvas.width = 60 * dpr;
    canvas.height = 24 * dpr;
    ctx.scale(dpr, dpr);

    const animate = () => {
      const target = targetHistoryRef.current;
      const animated = animatedHistoryRef.current;
      
      while (animated.length < target.length) animated.push(0);
      while (animated.length > target.length) animated.pop();
      
      for (let i = 0; i < animated.length; i++) {
        animated[i] += (target[i] - animated[i]) * 0.1;
      }

      ctx.clearRect(0, 0, 60, 24);
      
      if (animated.length > 1) {
        ctx.beginPath();
        const sliceCount = Math.min(animated.length, 30);
        const slice = animated.slice(-sliceCount);
        
        for (let i = 0; i < slice.length; i++) {
          const x = (i / (sliceCount - 1)) * 60;
          const y = 24 - (slice[i] / maxValue) * 20;
          if (i === 0) ctx.moveTo(x, y);
          else {
            const prevX = ((i - 1) / (sliceCount - 1)) * 60;
            const prevY = 24 - (slice[i - 1] / maxValue) * 20;
            const cpX = (prevX + x) / 2;
            ctx.quadraticCurveTo(prevX, prevY, cpX, (prevY + y) / 2);
          }
        }
        ctx.strokeStyle = color;
        ctx.lineWidth = 1.5;
        ctx.stroke();
      }

      animationRef.current = requestAnimationFrame(animate);
    };

    animationRef.current = requestAnimationFrame(animate);
    return () => cancelAnimationFrame(animationRef.current);
  }, [color, maxValue]);

  return (
    <button
      onClick={onClick}
      className={"w-full p-3 rounded-lg text-left transition-all duration-200 " + 
        (isActive ? 'bg-accent/20 border border-accent' : 'bg-bg-secondary hover:bg-bg-tertiary border border-transparent')}
    >
      <div className="flex items-center justify-between mb-1">
        <span className="text-sm font-medium">{label}</span>
        <span className="text-sm font-mono" style={{ color }}>
          {animatedValue.toFixed(1)}{suffix}
        </span>
      </div>
      <canvas ref={canvasRef} className="w-full" style={{ width: 60, height: 24 }} />
    </button>
  );
}

// Animated CPU core bar
function AnimatedCoreBar({ usage, index }: { usage: number; index: number }) {
  const animatedUsage = useAnimatedValue(usage, 300);
  const getColor = (val: number) => {
    if (val > 80) return 'rgb(239, 68, 68)';
    if (val > 60) return 'rgb(245, 158, 11)';
    return 'rgb(34, 197, 94)';
  };
  
  return (
    <div className="flex items-center gap-2">
      <span className="text-xs text-text-secondary w-8">C{index}</span>
      <div className="flex-1 h-2 bg-bg-tertiary rounded-full overflow-hidden">
        <div 
          className="h-full rounded-full transition-colors duration-300"
          style={{ 
            width: animatedUsage + '%',
            backgroundColor: getColor(animatedUsage)
          }}
        />
      </div>
      <span className="text-xs font-mono w-10 text-right">{animatedUsage.toFixed(0)}%</span>
    </div>
  );
}

// Stat row component
function StatRow({ label, value, subValue }: { label: string; value: React.ReactNode; subValue?: string }) {
  return (
    <div className="flex justify-between items-baseline py-1">
      <span className="text-text-secondary text-sm">{label}</span>
      <div className="text-right">
        <span className="font-mono">{value}</span>
        {subValue && <span className="text-text-secondary text-xs ml-1">{subValue}</span>}
      </div>
    </div>
  );
}


// CPU Detail View
function CPUDetailView({ metrics, history }: { metrics: DeviceMetrics; history: DeviceMetrics[] }) {
  const cpuHistory = useMemo(() => (history || []).map(m => m.cpu?.usage || 0).slice(-120), [history]);
  const coreUsages = metrics.cpu?.perCore || [];
  
  return (
    <div className="space-y-4">
      <div className="bg-bg-secondary rounded-lg p-4">
        <div className="flex justify-between items-center mb-2">
          <h3 className="font-medium">CPU Usage</h3>
          <AnimatedNumber value={metrics.cpu?.usage || 0} decimals={1} suffix="%" className="text-2xl font-mono text-accent" />
        </div>
        <SmoothPerformanceGraph data={cpuHistory} color="rgb(59, 130, 246)" height={150} />
      </div>
      
      {coreUsages.length > 0 && (
        <div className="bg-bg-secondary rounded-lg p-4">
          <h3 className="font-medium mb-3">Per-Core Usage</h3>
          <div className="grid gap-2" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))' }}>
            {coreUsages.map((usage, i) => (
              <AnimatedCoreBar key={i} usage={usage} index={i} />
            ))}
          </div>
        </div>
      )}
      
      <div className="bg-bg-secondary rounded-lg p-4">
        <h3 className="font-medium mb-2">CPU Information</h3>
        <div className="space-y-1">
          <StatRow label="Model" value={metrics.cpu?.model || 'Unknown'} />
          <StatRow label="Cores" value={metrics.cpu?.cores || 0} />
          <StatRow label="Threads" value={metrics.cpu?.threads || metrics.cpu?.cores || 0} />
          {metrics.cpu?.speed && <StatRow label="Speed" value={<AnimatedNumber value={metrics.cpu.speed} decimals={2} suffix=" GHz" />} />}
          {metrics.cpu?.temperature && <StatRow label="Temperature" value={<AnimatedNumber value={metrics.cpu.temperature} decimals={1} suffix="deg C" />} />}
        </div>
      </div>
    </div>
  );
}

// Memory Detail View
function MemoryDetailView({ metrics, history }: { metrics: DeviceMetrics; history: DeviceMetrics[] }) {
  const memHistory = useMemo(() => (history || []).map(m => {
    const used = m.memory?.used || 0;
    const total = m.memory?.total || 1;
    return (used / total) * 100;
  }).slice(-120), [history]);
  
  const usedPercent = ((metrics.memory?.used || 0) / (metrics.memory?.total || 1)) * 100;
  
  return (
    <div className="space-y-4">
      <div className="bg-bg-secondary rounded-lg p-4">
        <div className="flex justify-between items-center mb-2">
          <h3 className="font-medium">Memory Usage</h3>
          <AnimatedNumber value={usedPercent} decimals={1} suffix="%" className="text-2xl font-mono text-purple-400" />
        </div>
        <SmoothPerformanceGraph data={memHistory} color="rgb(168, 85, 247)" height={150} />
      </div>
      
      <div className="bg-bg-secondary rounded-lg p-4">
        <h3 className="font-medium mb-2">Memory Details</h3>
        <div className="space-y-1">
          <StatRow label="Used" value={<AnimatedBytes value={metrics.memory?.used || 0} />} />
          <StatRow label="Available" value={<AnimatedBytes value={metrics.memory?.available || 0} />} />
          <StatRow label="Total" value={formatBytes(metrics.memory?.total || 0)} />
          {metrics.memory?.cached && <StatRow label="Cached" value={<AnimatedBytes value={metrics.memory.cached} />} />}
        </div>
      </div>
      
      {metrics.memory?.swap && (
        <div className="bg-bg-secondary rounded-lg p-4">
          <h3 className="font-medium mb-2">Swap / Page File</h3>
          <div className="space-y-1">
            <StatRow label="Used" value={<AnimatedBytes value={metrics.memory.swap.used || 0} />} />
            <StatRow label="Total" value={formatBytes(metrics.memory.swap.total || 0)} />
          </div>
        </div>
      )}
    </div>
  );
}


// Disk Detail View
function DiskDetailView({ metrics, history }: { metrics: DeviceMetrics; history: DeviceMetrics[] }) {
  const readHistory = useMemo(() => (history || []).map(m => m.disk?.readSpeed || 0).slice(-120), [history]);
  const writeHistory = useMemo(() => (history || []).map(m => m.disk?.writeSpeed || 0).slice(-120), [history]);
  const maxIO = Math.max(...readHistory, ...writeHistory, 1024 * 1024);
  
  const disks = metrics.disk?.disks || [];
  
  return (
    <div className="space-y-4">
      <div className="bg-bg-secondary rounded-lg p-4">
        <div className="flex justify-between items-center mb-2">
          <h3 className="font-medium">Disk I/O</h3>
          <div className="text-right">
            <div className="text-sm">
              <span className="text-green-400">R:</span> <AnimatedBytes value={metrics.disk?.readSpeed || 0} perSecond className="font-mono" />
            </div>
            <div className="text-sm">
              <span className="text-blue-400">W:</span> <AnimatedBytes value={metrics.disk?.writeSpeed || 0} perSecond className="font-mono" />
            </div>
          </div>
        </div>
        <div className="relative" style={{ height: 150 }}>
          <SmoothPerformanceGraph data={readHistory} color="rgb(34, 197, 94)" height={150} maxValue={maxIO} showGrid={false} />
          <div className="absolute inset-0" style={{ opacity: 0.7 }}>
            <SmoothPerformanceGraph data={writeHistory} color="rgb(59, 130, 246)" height={150} maxValue={maxIO} showGrid={false} />
          </div>
        </div>
      </div>
      
      {disks.length > 0 && (
        <div className="bg-bg-secondary rounded-lg p-4">
          <h3 className="font-medium mb-3">Storage Devices</h3>
          <div className="space-y-3">
            {disks.map((disk, i) => {
              const usedPercent = ((disk.used || 0) / (disk.total || 1)) * 100;
              return (
                <div key={i} className="space-y-1">
                  <div className="flex justify-between text-sm">
                    <span>{disk.name || disk.mount || 'Disk ' + i}</span>
                    <span className="font-mono"><AnimatedBytes value={disk.used || 0} /> / {formatBytes(disk.total || 0)}</span>
                  </div>
                  <div className="h-2 bg-bg-tertiary rounded-full overflow-hidden">
                    <div 
                      className="h-full bg-amber-500 rounded-full transition-all duration-300"
                      style={{ width: usedPercent + '%' }}
                    />
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}

// Network Detail View  
function NetworkDetailView({ metrics, history }: { metrics: DeviceMetrics; history: DeviceMetrics[] }) {
  const downloadHistory = useMemo(() => (history || []).map(m => m.network?.downloadSpeed || 0).slice(-120), [history]);
  const uploadHistory = useMemo(() => (history || []).map(m => m.network?.uploadSpeed || 0).slice(-120), [history]);
  const maxNet = Math.max(...downloadHistory, ...uploadHistory, 1024 * 1024);
  
  const interfaces = metrics.network?.interfaces || [];
  
  return (
    <div className="space-y-4">
      <div className="bg-bg-secondary rounded-lg p-4">
        <div className="flex justify-between items-center mb-2">
          <h3 className="font-medium">Network Traffic</h3>
          <div className="text-right">
            <div className="text-sm">
              <span className="text-green-400">Down:</span> <AnimatedBytes value={metrics.network?.downloadSpeed || 0} perSecond className="font-mono" />
            </div>
            <div className="text-sm">
              <span className="text-red-400">Up:</span> <AnimatedBytes value={metrics.network?.uploadSpeed || 0} perSecond className="font-mono" />
            </div>
          </div>
        </div>
        <div className="relative" style={{ height: 150 }}>
          <SmoothPerformanceGraph data={downloadHistory} color="rgb(34, 197, 94)" height={150} maxValue={maxNet} showGrid={false} />
          <div className="absolute inset-0" style={{ opacity: 0.7 }}>
            <SmoothPerformanceGraph data={uploadHistory} color="rgb(239, 68, 68)" height={150} maxValue={maxNet} showGrid={false} />
          </div>
        </div>
      </div>
      
      <div className="bg-bg-secondary rounded-lg p-4">
        <h3 className="font-medium mb-2">Transfer Totals</h3>
        <div className="space-y-1">
          <StatRow label="Downloaded" value={<AnimatedBytes value={metrics.network?.totalDownload || 0} />} />
          <StatRow label="Uploaded" value={<AnimatedBytes value={metrics.network?.totalUpload || 0} />} />
        </div>
      </div>
      
      {interfaces.length > 0 && (
        <div className="bg-bg-secondary rounded-lg p-4">
          <h3 className="font-medium mb-3">Network Interfaces</h3>
          <div className="space-y-2">
            {interfaces.map((iface, i) => (
              <div key={i} className="p-2 bg-bg-tertiary rounded">
                <div className="font-medium text-sm">{iface.name}</div>
                {iface.ip && <div className="text-xs text-text-secondary">IP: {iface.ip}</div>}
                {iface.mac && <div className="text-xs text-text-secondary">MAC: {iface.mac}</div>}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}


// GPU Detail View
function GPUDetailView({ metrics, history }: { metrics: DeviceMetrics; history: DeviceMetrics[] }) {
  const gpuHistory = useMemo(() => (history || []).map(m => m.gpu?.usage || 0).slice(-120), [history]);
  const memHistory = useMemo(() => (history || []).map(m => {
    const used = m.gpu?.memoryUsed || 0;
    const total = m.gpu?.memoryTotal || 1;
    return (used / total) * 100;
  }).slice(-120), [history]);
  
  const memUsedPercent = ((metrics.gpu?.memoryUsed || 0) / (metrics.gpu?.memoryTotal || 1)) * 100;
  
  return (
    <div className="space-y-4">
      <div className="bg-bg-secondary rounded-lg p-4">
        <div className="flex justify-between items-center mb-2">
          <h3 className="font-medium">GPU Usage</h3>
          <AnimatedNumber value={metrics.gpu?.usage || 0} decimals={1} suffix="%" className="text-2xl font-mono text-emerald-400" />
        </div>
        <SmoothPerformanceGraph data={gpuHistory} color="rgb(52, 211, 153)" height={150} />
      </div>
      
      <div className="bg-bg-secondary rounded-lg p-4">
        <div className="flex justify-between items-center mb-2">
          <h3 className="font-medium">GPU Memory</h3>
          <AnimatedNumber value={memUsedPercent} decimals={1} suffix="%" className="text-xl font-mono text-teal-400" />
        </div>
        <SmoothPerformanceGraph data={memHistory} color="rgb(45, 212, 191)" height={100} />
        <div className="mt-2 space-y-1">
          <StatRow label="Used" value={<AnimatedBytes value={metrics.gpu?.memoryUsed || 0} />} />
          <StatRow label="Total" value={formatBytes(metrics.gpu?.memoryTotal || 0)} />
        </div>
      </div>
      
      <div className="bg-bg-secondary rounded-lg p-4">
        <h3 className="font-medium mb-2">GPU Information</h3>
        <div className="space-y-1">
          <StatRow label="Model" value={metrics.gpu?.model || 'Unknown'} />
          {metrics.gpu?.driver && <StatRow label="Driver" value={metrics.gpu.driver} />}
          {metrics.gpu?.temperature && <StatRow label="Temperature" value={<AnimatedNumber value={metrics.gpu.temperature} decimals={1} suffix=" C" />} />}
          {metrics.gpu?.fanSpeed && <StatRow label="Fan Speed" value={<AnimatedNumber value={metrics.gpu.fanSpeed} decimals={0} suffix="%" />} />}
          {metrics.gpu?.powerDraw && <StatRow label="Power" value={<AnimatedNumber value={metrics.gpu.powerDraw} decimals={1} suffix=" W" />} />}
        </div>
      </div>
    </div>
  );
}


// Main Performance View Component
export function PerformanceView({ metrics, metricsHistory }: PerformanceViewProps) {
  const [selectedResource, setSelectedResource] = useState<ResourceType>('cpu');
  
  // Prepare history data for sidebar mini-graphs
  const cpuHistory = useMemo(() => (metricsHistory || []).map(m => m.cpu?.usage || 0).slice(-30), [metricsHistory]);
  const memHistory = useMemo(() => (metricsHistory || []).map(m => ((m.memory?.used || 0) / (m.memory?.total || 1)) * 100).slice(-30), [metricsHistory]);
  const diskHistory = useMemo(() => (metricsHistory || []).map(m => (m.disk?.readSpeed || 0) + (m.disk?.writeSpeed || 0)).slice(-30), [metricsHistory]);
  const netHistory = useMemo(() => (metricsHistory || []).map(m => (m.network?.downloadSpeed || 0) + (m.network?.uploadSpeed || 0)).slice(-30), [metricsHistory]);
  const gpuHistory = useMemo(() => (metricsHistory || []).map(m => m.gpu?.usage || 0).slice(-30), [metricsHistory]);
  
  const maxDiskIO = Math.max(...diskHistory, 1024 * 1024);
  const maxNetIO = Math.max(...netHistory, 1024 * 1024);
  
  const hasGpu = metrics?.gpu && (metrics.gpu.model || metrics.gpu.usage !== undefined);
  
  if (!metrics) {
    return (
      <div className="flex items-center justify-center h-64 text-text-secondary">
        Waiting for metrics data...
      </div>
    );
  }
  
  return (
    <div className="flex gap-4 h-full">
      {/* Resource Sidebar */}
      <div className="w-48 flex-shrink-0 space-y-2">
        <AnimatedResourceSidebarItem
          label="CPU"
          value={metrics.cpu?.usage || 0}
          suffix="%"
          history={cpuHistory}
          maxValue={100}
          color="rgb(59, 130, 246)"
          isActive={selectedResource === 'cpu'}
          onClick={() => setSelectedResource('cpu')}
        />
        <AnimatedResourceSidebarItem
          label="Memory"
          value={((metrics.memory?.used || 0) / (metrics.memory?.total || 1)) * 100}
          suffix="%"
          history={memHistory}
          maxValue={100}
          color="rgb(168, 85, 247)"
          isActive={selectedResource === 'memory'}
          onClick={() => setSelectedResource('memory')}
        />
        <AnimatedResourceSidebarItem
          label="Disk"
          value={(metrics.disk?.readSpeed || 0) + (metrics.disk?.writeSpeed || 0)}
          suffix=""
          history={diskHistory}
          maxValue={maxDiskIO}
          color="rgb(245, 158, 11)"
          isActive={selectedResource === 'disk'}
          onClick={() => setSelectedResource('disk')}
        />
        <AnimatedResourceSidebarItem
          label="Network"
          value={(metrics.network?.downloadSpeed || 0) + (metrics.network?.uploadSpeed || 0)}
          suffix=""
          history={netHistory}
          maxValue={maxNetIO}
          color="rgb(34, 197, 94)"
          isActive={selectedResource === 'network'}
          onClick={() => setSelectedResource('network')}
        />
        {hasGpu && (
          <AnimatedResourceSidebarItem
            label="GPU"
            value={metrics.gpu?.usage || 0}
            suffix="%"
            history={gpuHistory}
            maxValue={100}
            color="rgb(52, 211, 153)"
            isActive={selectedResource === 'gpu'}
            onClick={() => setSelectedResource('gpu')}
          />
        )}
      </div>
      
      {/* Detail View */}
      <div className="flex-1 overflow-y-auto">
        {selectedResource === 'cpu' && <CPUDetailView metrics={metrics} history={metricsHistory} />}
        {selectedResource === 'memory' && <MemoryDetailView metrics={metrics} history={metricsHistory} />}
        {selectedResource === 'disk' && <DiskDetailView metrics={metrics} history={metricsHistory} />}
        {selectedResource === 'network' && <NetworkDetailView metrics={metrics} history={metricsHistory} />}
        {selectedResource === 'gpu' && hasGpu && <GPUDetailView metrics={metrics} history={metricsHistory} />}
      </div>
    </div>
  );
}

export default PerformanceView;
