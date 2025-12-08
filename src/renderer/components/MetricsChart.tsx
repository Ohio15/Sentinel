import React, { useMemo } from 'react';
import { DeviceMetrics } from '../stores/deviceStore';

interface MetricsChartProps {
  metrics: DeviceMetrics[];
}

export function MetricsChart({ metrics }: MetricsChartProps) {
  // Reverse metrics so oldest is first
  const sortedMetrics = useMemo(() => [...metrics].reverse(), [metrics]);

  // Sample data points for display (max 50 points)
  const sampledMetrics = useMemo(() => {
    if (sortedMetrics.length <= 50) return sortedMetrics;
    const step = Math.floor(sortedMetrics.length / 50);
    return sortedMetrics.filter((_, i) => i % step === 0);
  }, [sortedMetrics]);

  if (sampledMetrics.length < 2) {
    return (
      <div className="h-64 flex items-center justify-center text-text-secondary">
        Not enough data to display chart
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <MetricLine
        data={sampledMetrics}
        dataKey="cpuPercent"
        label="CPU Usage"
        color="#3b82f6"
        unit="%"
      />
      <MetricLine
        data={sampledMetrics}
        dataKey="memoryPercent"
        label="Memory Usage"
        color="#22c55e"
        unit="%"
      />
      <MetricLine
        data={sampledMetrics}
        dataKey="diskPercent"
        label="Disk Usage"
        color="#f59e0b"
        unit="%"
      />
    </div>
  );
}

interface MetricLineProps {
  data: DeviceMetrics[];
  dataKey: keyof DeviceMetrics;
  label: string;
  color: string;
  unit: string;
}

function MetricLine({ data, dataKey, label, color, unit }: MetricLineProps) {
  const { path, area, points, max, avg, current } = useMemo(() => {
    const values = data.map(d => d[dataKey] as number);
    const max = Math.max(...values);
    const min = Math.min(...values);
    const avg = values.reduce((a, b) => a + b, 0) / values.length;
    const current = values[values.length - 1];

    // Create SVG path
    const width = 800;
    const height = 60;
    const padding = 2;

    const xScale = (width - padding * 2) / (data.length - 1);
    const yScale = (height - padding * 2) / 100; // 0-100% scale

    const pathPoints = values.map((v, i) => ({
      x: padding + i * xScale,
      y: height - padding - v * yScale,
    }));

    const pathD = pathPoints
      .map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x} ${p.y}`)
      .join(' ');

    const areaD = `${pathD} L ${width - padding} ${height - padding} L ${padding} ${height - padding} Z`;

    return {
      path: pathD,
      area: areaD,
      points: pathPoints,
      max,
      avg,
      current,
    };
  }, [data, dataKey]);

  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          <div className="w-3 h-3 rounded-full" style={{ backgroundColor: color }} />
          <span className="text-sm font-medium text-text-primary">{label}</span>
        </div>
        <div className="flex items-center gap-4 text-sm">
          <span className="text-text-secondary">
            Current: <span className="font-medium text-text-primary">{current.toFixed(1)}{unit}</span>
          </span>
          <span className="text-text-secondary">
            Avg: <span className="font-medium text-text-primary">{avg.toFixed(1)}{unit}</span>
          </span>
          <span className="text-text-secondary">
            Max: <span className="font-medium text-text-primary">{max.toFixed(1)}{unit}</span>
          </span>
        </div>
      </div>
      <div className="relative h-16 bg-gray-50 rounded-lg overflow-hidden">
        <svg
          viewBox="0 0 800 60"
          className="w-full h-full"
          preserveAspectRatio="none"
        >
          {/* Grid lines */}
          <line x1="0" y1="15" x2="800" y2="15" stroke="#e2e8f0" strokeWidth="1" />
          <line x1="0" y1="30" x2="800" y2="30" stroke="#e2e8f0" strokeWidth="1" />
          <line x1="0" y1="45" x2="800" y2="45" stroke="#e2e8f0" strokeWidth="1" />

          {/* Area fill */}
          <path
            d={area}
            fill={color}
            fillOpacity="0.1"
          />

          {/* Line */}
          <path
            d={path}
            fill="none"
            stroke={color}
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>

        {/* Y-axis labels */}
        <div className="absolute top-0 right-0 h-full flex flex-col justify-between py-1 pr-2 text-xs text-text-secondary">
          <span>100%</span>
          <span>50%</span>
          <span>0%</span>
        </div>
      </div>
    </div>
  );
}
