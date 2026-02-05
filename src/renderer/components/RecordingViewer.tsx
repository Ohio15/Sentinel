import React, { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { getApiBaseUrl } from '../services/env';

interface RecordingMetric {
  id: number;
  timestamp: string;
  cpuPercent: number | null;
  memoryPercent: number | null;
  memoryUsedBytes: number | null;
  diskPercent: number | null;
  networkRxBytes: number | null;
  networkTxBytes: number | null;
  processCount: number | null;
}

interface RecordingSummary {
  id: string;
  deviceId: string;
  name: string | null;
  description: string | null;
  status: string;
  startedAt: string;
  endedAt: string | null;
  durationSeconds: number | null;
  metricsCount: number;
  deviceHostname: string;
  deviceDisplayName: string;
  avgCpuPercent: number | null;
  maxCpuPercent: number | null;
  avgMemoryPercent: number | null;
  maxMemoryPercent: number | null;
  totalNetworkRx: number | null;
  totalNetworkTx: number | null;
}

interface RecordingViewerProps {
  recordingId: string;
  onClose: () => void;
}

// Helper function for HTTP requests
async function fetchApi(endpoint: string): Promise<Response> {
  const token = localStorage.getItem('token');
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };

  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const baseUrl = getApiBaseUrl();
  return fetch(`${baseUrl}${endpoint}`, {
    headers,
    credentials: 'include',
  });
}

export function RecordingViewer({ recordingId, onClose }: RecordingViewerProps) {
  const [recording, setRecording] = useState<RecordingSummary | null>(null);
  const [metrics, setMetrics] = useState<RecordingMetric[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Playback state
  const [isPlaying, setIsPlaying] = useState(false);
  const [playbackSpeed, setPlaybackSpeed] = useState(1);
  const [currentIndex, setCurrentIndex] = useState(0);
  const playbackRef = useRef<NodeJS.Timeout | null>(null);

  // Selected metric type
  const [selectedMetric, setSelectedMetric] = useState<'cpu' | 'memory' | 'disk' | 'network'>('cpu');

  // Load recording data
  useEffect(() => {
    const loadData = async () => {
      setIsLoading(true);
      setError(null);

      try {
        // Load recording summary
        const recordingRes = await fetchApi(`/recordings/${recordingId}`);
        if (!recordingRes.ok) throw new Error('Failed to load recording');
        const recordingData = await recordingRes.json();
        setRecording(recordingData);

        // Load metrics
        const metricsRes = await fetchApi(`/recordings/${recordingId}/metrics`);
        if (!metricsRes.ok) throw new Error('Failed to load metrics');
        const metricsData = await metricsRes.json();
        setMetrics(metricsData.metrics || []);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load recording');
      } finally {
        setIsLoading(false);
      }
    };

    loadData();
  }, [recordingId]);

  // Playback control
  useEffect(() => {
    if (isPlaying && metrics.length > 0) {
      const intervalMs = 1000 / playbackSpeed;
      playbackRef.current = setInterval(() => {
        setCurrentIndex(prev => {
          if (prev >= metrics.length - 1) {
            setIsPlaying(false);
            return prev;
          }
          return prev + 1;
        });
      }, intervalMs);

      return () => {
        if (playbackRef.current) {
          clearInterval(playbackRef.current);
        }
      };
    }
  }, [isPlaying, playbackSpeed, metrics.length]);

  const handlePlay = () => {
    if (currentIndex >= metrics.length - 1) {
      setCurrentIndex(0);
    }
    setIsPlaying(true);
  };

  const handlePause = () => {
    setIsPlaying(false);
  };

  const handleSeek = (index: number) => {
    setCurrentIndex(index);
    setIsPlaying(false);
  };

  const currentMetric = metrics[currentIndex] || null;

  const formatBytes = (bytes: number | null): string => {
    if (bytes === null) return 'N/A';
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`;
  };

  const formatDuration = (seconds: number | null): string => {
    if (seconds === null) return '--';
    const hrs = Math.floor(seconds / 3600);
    const mins = Math.floor((seconds % 3600) / 60);
    const secs = seconds % 60;

    if (hrs > 0) {
      return `${hrs}h ${mins}m ${secs}s`;
    }
    if (mins > 0) {
      return `${mins}m ${secs}s`;
    }
    return `${secs}s`;
  };

  const formatTimestamp = (timestamp: string): string => {
    return new Date(timestamp).toLocaleTimeString();
  };

  // Get values for the selected metric type
  const getMetricValue = useCallback((metric: RecordingMetric, type: typeof selectedMetric): number => {
    switch (type) {
      case 'cpu': return metric.cpuPercent ?? 0;
      case 'memory': return metric.memoryPercent ?? 0;
      case 'disk': return metric.diskPercent ?? 0;
      case 'network': return ((metric.networkRxBytes ?? 0) + (metric.networkTxBytes ?? 0)) / 1000000; // MB
      default: return 0;
    }
  }, []);

  // Graph data
  const graphPath = useMemo(() => {
    if (metrics.length < 2) return '';

    const width = 800;
    const height = 200;
    const padding = 4;

    const values = metrics.map(m => getMetricValue(m, selectedMetric));
    const maxValue = selectedMetric === 'network'
      ? Math.max(...values, 1)
      : 100;

    const step = (width - padding * 2) / (values.length - 1);
    const points = values.map((v, i) => ({
      x: padding + i * step,
      y: height - padding - (v / maxValue) * (height - padding * 2),
    }));

    return `M ${points.map(p => `${p.x},${p.y}`).join(' L ')}`;
  }, [metrics, selectedMetric, getMetricValue]);

  // Playhead position
  const playheadX = useMemo(() => {
    if (metrics.length === 0) return 0;
    const width = 800;
    const padding = 4;
    const step = (width - padding * 2) / Math.max(metrics.length - 1, 1);
    return padding + currentIndex * step;
  }, [metrics.length, currentIndex]);

  const metricColor = useMemo(() => {
    switch (selectedMetric) {
      case 'cpu': return '#0078d4';
      case 'memory': return '#8764b8';
      case 'disk': return '#00b294';
      case 'network': return '#d48c00';
      default: return '#0078d4';
    }
  }, [selectedMetric]);

  if (isLoading) {
    return (
      <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
        <div className="bg-surface rounded-lg p-8">
          <div className="w-8 h-8 border-2 border-primary border-t-transparent rounded-full animate-spin mx-auto" />
          <p className="mt-4 text-text-secondary">Loading recording...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
        <div className="bg-surface rounded-lg p-8 max-w-md">
          <p className="text-error mb-4">{error}</p>
          <button
            onClick={onClose}
            className="px-4 py-2 bg-primary text-white rounded hover:bg-primary/90"
          >
            Close
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="bg-surface rounded-lg shadow-xl max-w-5xl w-full max-h-[90vh] overflow-hidden flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-border">
          <div>
            <h2 className="text-lg font-medium text-text-primary">
              {recording?.name || `Recording ${recordingId.slice(0, 8)}`}
            </h2>
            <p className="text-sm text-text-secondary">
              {recording?.deviceHostname || recording?.deviceDisplayName} &bull; {formatDuration(recording?.durationSeconds ?? null)} &bull; {recording?.metricsCount.toLocaleString()} samples
            </p>
          </div>
          <button
            onClick={onClose}
            className="p-2 hover:bg-hover rounded transition-colors"
          >
            <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-6">
          {/* Metric selector */}
          <div className="flex gap-2 mb-4">
            {(['cpu', 'memory', 'disk', 'network'] as const).map(type => (
              <button
                key={type}
                onClick={() => setSelectedMetric(type)}
                className={`px-3 py-1.5 text-sm rounded transition-colors ${
                  selectedMetric === type
                    ? 'bg-primary text-white'
                    : 'bg-surface-alt hover:bg-hover text-text-primary'
                }`}
              >
                {type.charAt(0).toUpperCase() + type.slice(1)}
              </button>
            ))}
          </div>

          {/* Graph */}
          <div className="relative bg-surface-alt border border-border rounded-lg overflow-hidden mb-4">
            <svg viewBox="0 0 800 200" className="w-full" style={{ height: '200px' }}>
              {/* Grid lines */}
              <line x1="0" y1="50" x2="800" y2="50" stroke="var(--border-color)" strokeDasharray="4" />
              <line x1="0" y1="100" x2="800" y2="100" stroke="var(--border-color)" strokeDasharray="4" />
              <line x1="0" y1="150" x2="800" y2="150" stroke="var(--border-color)" strokeDasharray="4" />

              {/* Area fill */}
              {graphPath && (
                <path
                  d={`${graphPath} L ${800 - 4},${200 - 4} L 4,${200 - 4} Z`}
                  fill={`${metricColor}22`}
                />
              )}

              {/* Line */}
              {graphPath && (
                <path
                  d={graphPath}
                  fill="none"
                  stroke={metricColor}
                  strokeWidth="2"
                />
              )}

              {/* Playhead */}
              <line
                x1={playheadX}
                y1="0"
                x2={playheadX}
                y2="200"
                stroke="var(--error-color)"
                strokeWidth="2"
              />
              <circle
                cx={playheadX}
                cy={200 - 4 - (getMetricValue(currentMetric || { cpuPercent: 0, memoryPercent: 0, diskPercent: 0, networkRxBytes: 0, networkTxBytes: 0 } as RecordingMetric, selectedMetric) / (selectedMetric === 'network' ? Math.max(...metrics.map(m => ((m.networkRxBytes ?? 0) + (m.networkTxBytes ?? 0)) / 1000000), 1) : 100)) * (200 - 8)}
                r="6"
                fill="var(--error-color)"
              />
            </svg>

            {/* Click to seek */}
            <div
              className="absolute inset-0 cursor-pointer"
              onClick={(e) => {
                const rect = e.currentTarget.getBoundingClientRect();
                const x = e.clientX - rect.left;
                const percent = x / rect.width;
                const index = Math.round(percent * (metrics.length - 1));
                handleSeek(Math.max(0, Math.min(metrics.length - 1, index)));
              }}
            />
          </div>

          {/* Playback controls */}
          <div className="flex items-center gap-4 mb-6">
            <button
              onClick={isPlaying ? handlePause : handlePlay}
              className="p-2 bg-primary text-white rounded-full hover:bg-primary/90 transition-colors"
            >
              {isPlaying ? (
                <svg className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor">
                  <rect x="6" y="4" width="4" height="16" />
                  <rect x="14" y="4" width="4" height="16" />
                </svg>
              ) : (
                <svg className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M8 5v14l11-7z" />
                </svg>
              )}
            </button>

            {/* Timeline slider */}
            <input
              type="range"
              min="0"
              max={Math.max(metrics.length - 1, 0)}
              value={currentIndex}
              onChange={(e) => handleSeek(parseInt(e.target.value))}
              className="flex-1 h-2 bg-surface-alt rounded-lg appearance-none cursor-pointer"
              style={{
                background: `linear-gradient(to right, ${metricColor} 0%, ${metricColor} ${(currentIndex / Math.max(metrics.length - 1, 1)) * 100}%, var(--surface-alt-color) ${(currentIndex / Math.max(metrics.length - 1, 1)) * 100}%, var(--surface-alt-color) 100%)`,
              }}
            />

            {/* Current time */}
            <span className="text-sm text-text-secondary font-mono min-w-[80px]">
              {currentMetric ? formatTimestamp(currentMetric.timestamp) : '--:--:--'}
            </span>

            {/* Speed selector */}
            <select
              value={playbackSpeed}
              onChange={(e) => setPlaybackSpeed(parseFloat(e.target.value))}
              className="px-2 py-1 bg-surface-alt border border-border rounded text-sm"
            >
              <option value="0.5">0.5x</option>
              <option value="1">1x</option>
              <option value="2">2x</option>
              <option value="4">4x</option>
            </select>
          </div>

          {/* Current values */}
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            <div className="bg-surface-alt border border-border rounded-lg p-4">
              <div className="text-xs text-text-secondary mb-1">CPU</div>
              <div className="text-2xl font-light" style={{ color: '#0078d4' }}>
                {currentMetric?.cpuPercent?.toFixed(1) ?? '--'}%
              </div>
            </div>
            <div className="bg-surface-alt border border-border rounded-lg p-4">
              <div className="text-xs text-text-secondary mb-1">Memory</div>
              <div className="text-2xl font-light" style={{ color: '#8764b8' }}>
                {currentMetric?.memoryPercent?.toFixed(1) ?? '--'}%
              </div>
              <div className="text-xs text-text-secondary">
                {formatBytes(currentMetric?.memoryUsedBytes ?? null)}
              </div>
            </div>
            <div className="bg-surface-alt border border-border rounded-lg p-4">
              <div className="text-xs text-text-secondary mb-1">Disk</div>
              <div className="text-2xl font-light" style={{ color: '#00b294' }}>
                {currentMetric?.diskPercent?.toFixed(1) ?? '--'}%
              </div>
            </div>
            <div className="bg-surface-alt border border-border rounded-lg p-4">
              <div className="text-xs text-text-secondary mb-1">Network</div>
              <div className="text-lg font-light" style={{ color: '#d48c00' }}>
                <div>↓ {formatBytes(currentMetric?.networkRxBytes ?? null)}</div>
                <div>↑ {formatBytes(currentMetric?.networkTxBytes ?? null)}</div>
              </div>
            </div>
          </div>

          {/* Summary stats */}
          {recording && (
            <div className="mt-6 p-4 bg-surface-alt border border-border rounded-lg">
              <h3 className="text-sm font-medium text-text-primary mb-3">Recording Summary</h3>
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
                <div>
                  <div className="text-text-secondary">Avg CPU</div>
                  <div className="font-medium">{recording.avgCpuPercent?.toFixed(1) ?? '--'}%</div>
                </div>
                <div>
                  <div className="text-text-secondary">Max CPU</div>
                  <div className="font-medium">{recording.maxCpuPercent?.toFixed(1) ?? '--'}%</div>
                </div>
                <div>
                  <div className="text-text-secondary">Avg Memory</div>
                  <div className="font-medium">{recording.avgMemoryPercent?.toFixed(1) ?? '--'}%</div>
                </div>
                <div>
                  <div className="text-text-secondary">Max Memory</div>
                  <div className="font-medium">{recording.maxMemoryPercent?.toFixed(1) ?? '--'}%</div>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export default RecordingViewer;
