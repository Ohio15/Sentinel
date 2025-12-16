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
  const { paths, areas, max, avg, current } = useMemo(() => {
    // Filter out null/undefined values and get valid data points
    const validPoints: { index: number; value: number; timestamp: number }[] = [];

    for (let i = 0; i < data.length; i++) {
      const value = data[i][dataKey] as number;
      const timestamp = data[i].timestamp ? new Date(data[i].timestamp).getTime() : i * 60000;

      if (value !== null && value !== undefined && !isNaN(value)) {
        validPoints.push({ index: i, value, timestamp });
      }
    }

    if (validPoints.length === 0) {
      return { paths: [], areas: [], max: 0, avg: 0, current: 0 };
    }

    const values = validPoints.map(p => p.value);
    const max = Math.max(...values);
    const avg = values.reduce((a, b) => a + b, 0) / values.length;
    const current = values[values.length - 1] ?? 0;

    // Create SVG paths - break into segments when there's a time gap > 5 minutes
    const width = 800;
    const height = 60;
    const padding = 2;
    const maxGapMs = 5 * 60 * 1000; // 5 minutes

    // Calculate time range for proper x-axis scaling
    const timeRange = validPoints.length > 1
      ? validPoints[validPoints.length - 1].timestamp - validPoints[0].timestamp
      : 1;

    const xScale = (width - padding * 2) / (timeRange || 1);
    const yScale = (height - padding * 2) / 100; // 0-100% scale
    const startTime = validPoints[0].timestamp;

    // Break data into segments based on time gaps
    const segments: { x: number; y: number }[][] = [];
    let currentSegment: { x: number; y: number }[] = [];

    for (let i = 0; i < validPoints.length; i++) {
      const point = validPoints[i];
      const x = padding + (point.timestamp - startTime) * xScale;
      const y = height - padding - point.value * yScale;

      // Check for time gap
      if (i > 0) {
        const gap = point.timestamp - validPoints[i - 1].timestamp;
        if (gap > maxGapMs && currentSegment.length > 0) {
          // Start new segment
          segments.push(currentSegment);
          currentSegment = [];
        }
      }

      currentSegment.push({ x, y });
    }

    if (currentSegment.length > 0) {
      segments.push(currentSegment);
    }

    // Generate paths and areas for each segment
    const paths: string[] = [];
    const areas: string[] = [];

    for (const segment of segments) {
      if (segment.length < 1) continue;

      const pathD = segment
        .map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x.toFixed(1)} ${p.y.toFixed(1)}`)
        .join(' ');
      paths.push(pathD);

      if (segment.length >= 2) {
        const firstX = segment[0].x.toFixed(1);
        const lastX = segment[segment.length - 1].x.toFixed(1);
        const bottomY = (height - padding).toFixed(1);
        const areaD = `${pathD} L ${lastX} ${bottomY} L ${firstX} ${bottomY} Z`;
        areas.push(areaD);
      }
    }

    return { paths, areas, max, avg, current };
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
            Current: <span className="font-medium text-text-primary">{(current ?? 0).toFixed(1)}{unit}</span>
          </span>
          <span className="text-text-secondary">
            Avg: <span className="font-medium text-text-primary">{(avg ?? 0).toFixed(1)}{unit}</span>
          </span>
          <span className="text-text-secondary">
            Max: <span className="font-medium text-text-primary">{(max ?? 0).toFixed(1)}{unit}</span>
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

          {/* Area fills */}
          {areas.map((area, i) => (
            <path
              key={`area-${i}`}
              d={area}
              fill={color}
              fillOpacity="0.1"
            />
          ))}

          {/* Lines */}
          {paths.map((path, i) => (
            <path
              key={`line-${i}`}
              d={path}
              fill="none"
              stroke={color}
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          ))}
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
