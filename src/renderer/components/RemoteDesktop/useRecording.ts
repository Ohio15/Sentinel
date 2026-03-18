import { useCallback, useEffect, useRef, useState } from 'react';

interface UseRecordingOptions {
  dataChannel: RTCDataChannel | null;
  isConnected: boolean;
}

interface RecordingStatus {
  active: boolean;
  frameCount: number;
  bytesWritten: number;
  duration: number; // seconds
}

interface RecordingReturn {
  isRecording: boolean;
  startRecording: () => void;
  stopRecording: () => void;
  recordingDuration: string;
  recordingSize: string;
}

/**
 * useRecording manages session recording state via the WebRTC data channel.
 * It sends start/stop commands to the agent and tracks recording status updates.
 *
 * The agent writes raw H.264 Annex B bitstream files on disk. This hook
 * provides the UI controls and status display.
 */
export function useRecording(options: UseRecordingOptions): RecordingReturn {
  const { dataChannel, isConnected } = options;

  const [isRecording, setIsRecording] = useState(false);
  const [bytesWritten, setBytesWritten] = useState(0);
  const [elapsedSeconds, setElapsedSeconds] = useState(0);

  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const startTimeRef = useRef<number>(0);
  const messageHandlerRef = useRef<((event: MessageEvent) => void) | null>(null);

  // Clean up timer
  const stopTimer = useCallback(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  // Start local duration timer
  const startTimer = useCallback(() => {
    stopTimer();
    startTimeRef.current = Date.now();
    setElapsedSeconds(0);
    timerRef.current = setInterval(() => {
      const elapsed = Math.floor((Date.now() - startTimeRef.current) / 1000);
      setElapsedSeconds(elapsed);
    }, 1000);
  }, [stopTimer]);

  // Send a message over the data channel
  const sendMessage = useCallback((msg: Record<string, unknown>) => {
    if (dataChannel && dataChannel.readyState === 'open') {
      dataChannel.send(JSON.stringify(msg));
    }
  }, [dataChannel]);

  // Start recording
  const startRecording = useCallback(() => {
    if (!dataChannel || dataChannel.readyState !== 'open') {
      console.warn('[Recording] Cannot start: data channel not open');
      return;
    }
    if (isRecording) {
      console.warn('[Recording] Already recording');
      return;
    }

    sendMessage({ type: 'recording.start' });
    setIsRecording(true);
    setBytesWritten(0);
    startTimer();
    console.log('[Recording] Start command sent');
  }, [dataChannel, isRecording, sendMessage, startTimer]);

  // Stop recording
  const stopRecording = useCallback(() => {
    if (!isRecording) {
      return;
    }

    sendMessage({ type: 'recording.stop' });
    setIsRecording(false);
    stopTimer();
    console.log('[Recording] Stop command sent');
  }, [isRecording, sendMessage, stopTimer]);

  // Listen for recording.status messages from the agent
  useEffect(() => {
    if (!dataChannel) {
      return;
    }

    // Remove previous handler if any
    if (messageHandlerRef.current) {
      dataChannel.removeEventListener('message', messageHandlerRef.current);
    }

    const handler = (event: MessageEvent) => {
      try {
        const data = JSON.parse(event.data);
        if (data.type === 'recording.status') {
          const status = data as RecordingStatus;
          setIsRecording(status.active);
          setBytesWritten(status.bytesWritten);

          // If the agent reports not active but we think we are, sync state
          if (!status.active && timerRef.current) {
            stopTimer();
          }
          // If the agent reports active but we have no timer, start tracking
          if (status.active && !timerRef.current) {
            startTimer();
          }
        } else if (data.type === 'recording.stopped') {
          // Agent-initiated stop (e.g. limit reached)
          setIsRecording(false);
          stopTimer();
          console.log('[Recording] Agent stopped recording:', data.reason || 'unknown');
        }
      } catch {
        // Not a JSON message or not recording-related, ignore
      }
    };

    messageHandlerRef.current = handler;
    dataChannel.addEventListener('message', handler);

    return () => {
      dataChannel.removeEventListener('message', handler);
      messageHandlerRef.current = null;
    };
  }, [dataChannel, stopTimer, startTimer]);

  // Auto-stop recording if connection drops
  useEffect(() => {
    if (!isConnected && isRecording) {
      setIsRecording(false);
      stopTimer();
      console.log('[Recording] Connection lost, recording state reset');
    }
  }, [isConnected, isRecording, stopTimer]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      stopTimer();
    };
  }, [stopTimer]);

  // Format duration as MM:SS
  const recordingDuration = formatDuration(elapsedSeconds);

  // Format size as human-readable
  const recordingSize = formatBytes(bytesWritten);

  return {
    isRecording,
    startRecording,
    stopRecording,
    recordingDuration,
    recordingSize,
  };
}

/**
 * Format seconds as MM:SS string.
 */
function formatDuration(totalSeconds: number): string {
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes.toString().padStart(2, '0')}:${seconds.toString().padStart(2, '0')}`;
}

/**
 * Format bytes as a human-readable size string.
 */
function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}
