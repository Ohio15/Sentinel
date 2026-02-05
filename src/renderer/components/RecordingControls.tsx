import React, { useState, useEffect, useCallback, useRef } from 'react';
import { isWeb, getApiBaseUrl } from '../services/env';

interface RecordingControlsProps {
  deviceId: string;
  onRecordingChange?: (isRecording: boolean, recordingId?: string) => void;
  onRecordingStopped?: () => void; // Called after recording stops to refresh list
}

interface ActiveRecording {
  id: string;
  deviceId: string;
  name: string | null;
  status: string;
  startedAt: string;
  metricsCount: number;
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

  // Add CSRF token for state-changing requests
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

export function RecordingControls({ deviceId, onRecordingChange, onRecordingStopped }: RecordingControlsProps) {
  const [isRecording, setIsRecording] = useState(false);
  const [activeRecording, setActiveRecording] = useState<ActiveRecording | null>(null);
  const [elapsedTime, setElapsedTime] = useState(0);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const timerRef = useRef<NodeJS.Timeout | null>(null);

  // Check for active recording on mount
  useEffect(() => {
    checkActiveRecording();
  }, [deviceId]);

  // Timer for elapsed time
  useEffect(() => {
    if (isRecording && activeRecording) {
      const startTime = new Date(activeRecording.startedAt).getTime();

      const updateElapsed = () => {
        const now = Date.now();
        setElapsedTime(Math.floor((now - startTime) / 1000));
      };

      updateElapsed();
      timerRef.current = setInterval(updateElapsed, 1000);

      return () => {
        if (timerRef.current) {
          clearInterval(timerRef.current);
        }
      };
    } else {
      setElapsedTime(0);
    }
  }, [isRecording, activeRecording]);

  const checkActiveRecording = useCallback(async () => {
    try {
      const response = await fetchApi(`/devices/${deviceId}/recording/active`);
      if (!response.ok) {
        // No active recording or endpoint not available
        setActiveRecording(null);
        setIsRecording(false);
        onRecordingChange?.(false);
        return;
      }

      const text = await response.text();
      if (!text) {
        setActiveRecording(null);
        setIsRecording(false);
        onRecordingChange?.(false);
        return;
      }

      const data = JSON.parse(text);
      if (data.recording) {
        setActiveRecording(data.recording);
        setIsRecording(true);
        onRecordingChange?.(true, data.recording.id);
      } else {
        setActiveRecording(null);
        setIsRecording(false);
        onRecordingChange?.(false);
      }
    } catch (err) {
      console.error('Failed to check active recording:', err);
      setActiveRecording(null);
      setIsRecording(false);
    }
  }, [deviceId, onRecordingChange]);

  const startRecording = useCallback(async () => {
    setIsLoading(true);
    setError(null);

    try {
      const response = await fetchApi('/recordings', {
        method: 'POST',
        body: JSON.stringify({
          deviceId,
          name: `Recording ${new Date().toLocaleString()}`,
        }),
      });

      const text = await response.text();
      let data;
      try {
        data = JSON.parse(text);
      } catch {
        throw new Error(`Server error: ${text.substring(0, 100)}`);
      }

      if (!response.ok) {
        throw new Error(data.error || 'Failed to start recording');
      }

      setActiveRecording({
        id: data.id,
        deviceId: data.deviceId,
        name: null,
        status: 'recording',
        startedAt: data.startedAt,
        metricsCount: 0,
      });
      setIsRecording(true);
      onRecordingChange?.(true, data.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to start recording');
    } finally {
      setIsLoading(false);
    }
  }, [deviceId, onRecordingChange]);

  const stopRecording = useCallback(async () => {
    if (!activeRecording) return;

    setIsLoading(true);
    setError(null);

    try {
      const response = await fetchApi(`/recordings/${activeRecording.id}/stop`, {
        method: 'POST',
      });

      if (!response.ok) {
        const data = await response.json();
        throw new Error(data.error || 'Failed to stop recording');
      }

      setActiveRecording(null);
      setIsRecording(false);
      setElapsedTime(0);
      onRecordingChange?.(false);
      // Notify parent to refresh recordings list
      onRecordingStopped?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to stop recording');
    } finally {
      setIsLoading(false);
    }
  }, [activeRecording, onRecordingChange, onRecordingStopped]);

  const formatTime = (seconds: number): string => {
    const hrs = Math.floor(seconds / 3600);
    const mins = Math.floor((seconds % 3600) / 60);
    const secs = seconds % 60;

    if (hrs > 0) {
      return `${hrs}:${mins.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`;
    }
    return `${mins}:${secs.toString().padStart(2, '0')}`;
  };

  return (
    <div className="flex items-center gap-3">
      {isRecording ? (
        <>
          {/* Recording button - turns red with outline when recording */}
          <button
            onClick={stopRecording}
            disabled={isLoading}
            className="flex items-center gap-2 px-4 py-1.5 bg-error/10 hover:bg-error/20 border-2 border-error text-error rounded-lg transition-colors disabled:opacity-50"
          >
            <div className="relative">
              <div className="w-3 h-3 bg-error rounded-full" />
              <div className="absolute inset-0 w-3 h-3 bg-error rounded-full animate-ping opacity-75" />
            </div>
            <span className="text-sm font-semibold">REC</span>
            <span className="text-sm font-mono">{formatTime(elapsedTime)}</span>
            <svg className="w-4 h-4 ml-1" viewBox="0 0 24 24" fill="currentColor">
              <rect x="6" y="6" width="12" height="12" rx="1" />
            </svg>
          </button>
        </>
      ) : (
        /* Start recording button - outlined style */
        <button
          onClick={startRecording}
          disabled={isLoading}
          className="flex items-center gap-2 px-4 py-1.5 bg-transparent hover:bg-hover border-2 border-border hover:border-text-secondary rounded-lg transition-colors disabled:opacity-50"
        >
          <svg className="w-4 h-4 text-error" viewBox="0 0 24 24" fill="currentColor">
            <circle cx="12" cy="12" r="6" />
          </svg>
          <span className="text-sm font-medium text-text-primary">Record</span>
        </button>
      )}

      {/* Metrics count indicator */}
      {isRecording && activeRecording && (
        <span className="text-xs text-text-secondary">
          {activeRecording.metricsCount} samples
        </span>
      )}

      {/* Error message */}
      {error && (
        <span className="text-xs text-error">{error}</span>
      )}
    </div>
  );
}

export default RecordingControls;
