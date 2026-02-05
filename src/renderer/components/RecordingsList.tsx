import React, { useState, useEffect, useCallback } from 'react';
import { getApiBaseUrl } from '../services/env';

interface Recording {
  id: string;
  deviceId: string;
  name: string | null;
  description: string | null;
  status: string;
  startedAt: string;
  endedAt: string | null;
  durationSeconds: number | null;
  metricsCount: number;
  deviceHostname?: string;
  deviceDisplayName?: string;
  initiatedByEmail?: string;
}

interface RecordingsListProps {
  deviceId: string;
  onViewRecording: (recordingId: string) => void;
  onRefresh?: () => void;
}

// Helper function for HTTP requests
async function fetchApi(endpoint: string, options: RequestInit = {}): Promise<Response> {
  const token = localStorage.getItem('token');
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string> || {}),
  };

  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  if (options.method && options.method !== 'GET') {
    const match = document.cookie.match(/csrf_token=([^;]+)/);
    if (match) {
      headers['X-CSRF-Token'] = match[1];
    }
  }

  const baseUrl = getApiBaseUrl();
  return fetch(`${baseUrl}${endpoint}`, {
    ...options,
    headers,
    credentials: 'include',
  });
}

export function RecordingsList({ deviceId, onViewRecording, onRefresh }: RecordingsListProps) {
  const [recordings, setRecordings] = useState<Recording[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);

  const loadRecordings = useCallback(async () => {
    setIsLoading(true);
    setError(null);

    try {
      const response = await fetchApi(`/recordings?deviceId=${deviceId}`);
      if (!response.ok) {
        throw new Error('Failed to load recordings');
      }
      const data = await response.json();
      setRecordings(data.recordings || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load recordings');
    } finally {
      setIsLoading(false);
    }
  }, [deviceId]);

  useEffect(() => {
    loadRecordings();
  }, [loadRecordings]);

  // Expose refresh function
  useEffect(() => {
    if (onRefresh) {
      // Create a stable reference
      (window as any).__recordingsListRefresh = loadRecordings;
    }
    return () => {
      delete (window as any).__recordingsListRefresh;
    };
  }, [loadRecordings, onRefresh]);

  const handleDelete = async (id: string) => {
    try {
      const response = await fetchApi(`/recordings/${id}`, {
        method: 'DELETE',
      });

      if (!response.ok) {
        const data = await response.json();
        throw new Error(data.error || 'Failed to delete recording');
      }

      setRecordings(prev => prev.filter(r => r.id !== id));
      setDeleteConfirm(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete recording');
    }
  };

  const handleExport = (id: string, format: 'csv' | 'json') => {
    const baseUrl = getApiBaseUrl();
    const token = localStorage.getItem('token');
    window.open(`${baseUrl}/recordings/${id}/export/${format}?token=${token}`, '_blank');
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

  const formatDate = (dateStr: string): string => {
    const date = new Date(dateStr);
    return date.toLocaleString();
  };

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'recording':
        return (
          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium bg-error/10 text-error">
            <span className="w-1.5 h-1.5 bg-error rounded-full animate-pulse" />
            Recording
          </span>
        );
      case 'completed':
        return (
          <span className="px-2 py-0.5 rounded text-xs font-medium bg-success/10 text-success">
            Completed
          </span>
        );
      case 'failed':
        return (
          <span className="px-2 py-0.5 rounded text-xs font-medium bg-warning/10 text-warning">
            Failed
          </span>
        );
      default:
        return (
          <span className="px-2 py-0.5 rounded text-xs font-medium bg-text-secondary/10 text-text-secondary">
            {status}
          </span>
        );
    }
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-8">
        <div className="w-6 h-6 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center py-8 text-error">
        <span>{error}</span>
        <button
          onClick={loadRecordings}
          className="ml-4 text-sm underline hover:no-underline"
        >
          Retry
        </button>
      </div>
    );
  }

  if (recordings.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-8 text-text-secondary">
        <svg className="w-12 h-12 mb-3 opacity-50" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
          <circle cx="12" cy="12" r="10" />
          <circle cx="12" cy="12" r="3" />
          <path d="M12 2v3M12 19v3M2 12h3M19 12h3" />
        </svg>
        <p>No recordings yet</p>
        <p className="text-xs mt-1">Click the Record button to capture performance metrics</p>
      </div>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border">
            <th className="text-left px-3 py-2 font-medium text-text-secondary">Name</th>
            <th className="text-left px-3 py-2 font-medium text-text-secondary">Date</th>
            <th className="text-left px-3 py-2 font-medium text-text-secondary">Duration</th>
            <th className="text-left px-3 py-2 font-medium text-text-secondary">Samples</th>
            <th className="text-left px-3 py-2 font-medium text-text-secondary">Status</th>
            <th className="text-right px-3 py-2 font-medium text-text-secondary">Actions</th>
          </tr>
        </thead>
        <tbody>
          {recordings.map((recording) => (
            <tr
              key={recording.id}
              className="border-b border-border/50 hover:bg-hover/50 transition-colors"
            >
              <td className="px-3 py-2">
                <div className="font-medium text-text-primary">
                  {recording.name || `Recording ${recording.id.slice(0, 8)}`}
                </div>
                {recording.description && (
                  <div className="text-xs text-text-secondary truncate max-w-xs">
                    {recording.description}
                  </div>
                )}
              </td>
              <td className="px-3 py-2 text-text-secondary">
                {formatDate(recording.startedAt)}
              </td>
              <td className="px-3 py-2 text-text-secondary">
                {formatDuration(recording.durationSeconds)}
              </td>
              <td className="px-3 py-2 text-text-secondary">
                {recording.metricsCount.toLocaleString()}
              </td>
              <td className="px-3 py-2">
                {getStatusBadge(recording.status)}
              </td>
              <td className="px-3 py-2">
                <div className="flex items-center justify-end gap-1">
                  {/* View button */}
                  <button
                    onClick={() => onViewRecording(recording.id)}
                    className="p-1.5 hover:bg-hover rounded transition-colors"
                    title="View recording"
                  >
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
                      <circle cx="12" cy="12" r="3" />
                    </svg>
                  </button>

                  {/* Export dropdown */}
                  {recording.status === 'completed' && (
                    <div className="relative group">
                      <button
                        className="p-1.5 hover:bg-hover rounded transition-colors"
                        title="Export"
                      >
                        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                          <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                          <polyline points="7 10 12 15 17 10" />
                          <line x1="12" y1="15" x2="12" y2="3" />
                        </svg>
                      </button>
                      <div className="absolute right-0 mt-1 w-32 bg-surface border border-border rounded shadow-lg opacity-0 invisible group-hover:opacity-100 group-hover:visible transition-all z-10">
                        <button
                          onClick={() => handleExport(recording.id, 'csv')}
                          className="w-full px-3 py-2 text-left text-sm hover:bg-hover transition-colors"
                        >
                          Export CSV
                        </button>
                        <button
                          onClick={() => handleExport(recording.id, 'json')}
                          className="w-full px-3 py-2 text-left text-sm hover:bg-hover transition-colors"
                        >
                          Export JSON
                        </button>
                      </div>
                    </div>
                  )}

                  {/* Delete button */}
                  {recording.status !== 'recording' && (
                    deleteConfirm === recording.id ? (
                      <div className="flex items-center gap-1">
                        <button
                          onClick={() => handleDelete(recording.id)}
                          className="p-1.5 hover:bg-error/20 text-error rounded transition-colors"
                          title="Confirm delete"
                        >
                          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                            <polyline points="20 6 9 17 4 12" />
                          </svg>
                        </button>
                        <button
                          onClick={() => setDeleteConfirm(null)}
                          className="p-1.5 hover:bg-hover rounded transition-colors"
                          title="Cancel"
                        >
                          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                            <line x1="18" y1="6" x2="6" y2="18" />
                            <line x1="6" y1="6" x2="18" y2="18" />
                          </svg>
                        </button>
                      </div>
                    ) : (
                      <button
                        onClick={() => setDeleteConfirm(recording.id)}
                        className="p-1.5 hover:bg-error/10 text-text-secondary hover:text-error rounded transition-colors"
                        title="Delete recording"
                      >
                        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                          <polyline points="3 6 5 6 21 6" />
                          <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
                        </svg>
                      </button>
                    )
                  )}
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default RecordingsList;
