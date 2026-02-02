import { useEffect, useRef, useState, RefObject } from 'react';

export interface FramePacingStats {
  avgJitter: number;      // Average jitter in ms
  targetBuffer: number;   // Target buffer depth in ms
  frameInterval: number;  // Average interval between frames in ms
  droppedFrames: number;  // Number of dropped frames
}

/**
 * useFramePacing - Adaptive jitter buffer for smoother video playback
 *
 * This hook monitors video frame timing and provides jitter statistics.
 * It uses requestVideoFrameCallback (if available) to get precise frame timing.
 *
 * The jitter buffer target adapts based on observed network conditions:
 * - Low jitter: smaller buffer (20ms) for lower latency
 * - High jitter: larger buffer (100ms) for smoother playback
 */
export function useFramePacing(videoRef: RefObject<HTMLVideoElement>) {
  const [stats, setStats] = useState<FramePacingStats>({
    avgJitter: 0,
    targetBuffer: 40,
    frameInterval: 33.33, // ~30fps default
    droppedFrames: 0,
  });

  // Jitter samples for calculating average
  const jitterSamplesRef = useRef<number[]>([]);
  const intervalSamplesRef = useRef<number[]>([]);
  const targetBufferRef = useRef(40); // ms
  const droppedFramesRef = useRef(0);
  const frameCallbackIdRef = useRef<number | null>(null);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    // Check if requestVideoFrameCallback is available (Chrome/Edge 83+)
    if (!('requestVideoFrameCallback' in video)) {
      console.log('[FramePacing] requestVideoFrameCallback not available');
      return;
    }

    let lastPresentationTime = 0;
    let expectedInterval = 1000 / 30; // 30fps expected

    // Frame callback for precise timing
    const onFrame = (now: DOMHighResTimeStamp, metadata: VideoFrameCallbackMetadata) => {
      if (lastPresentationTime > 0) {
        const interval = metadata.presentationTime - lastPresentationTime;
        const jitter = Math.abs(interval - expectedInterval);

        // Store samples (keep last 30 frames)
        jitterSamplesRef.current.push(jitter);
        intervalSamplesRef.current.push(interval);

        if (jitterSamplesRef.current.length > 30) {
          jitterSamplesRef.current.shift();
        }
        if (intervalSamplesRef.current.length > 30) {
          intervalSamplesRef.current.shift();
        }

        // Calculate average jitter
        const avgJitter = jitterSamplesRef.current.reduce((a, b) => a + b, 0) / jitterSamplesRef.current.length;

        // Calculate average interval and update expected interval
        const avgInterval = intervalSamplesRef.current.reduce((a, b) => a + b, 0) / intervalSamplesRef.current.length;
        if (avgInterval > 0) {
          expectedInterval = avgInterval;
        }

        // Adapt buffer target based on jitter (2x observed jitter, clamped 20-100ms)
        targetBufferRef.current = Math.min(100, Math.max(20, avgJitter * 2));

        // Detect dropped frames (interval > 1.5x expected)
        if (interval > expectedInterval * 1.5) {
          droppedFramesRef.current++;
        }

        // Update stats every 10 frames
        if (jitterSamplesRef.current.length % 10 === 0) {
          setStats({
            avgJitter,
            targetBuffer: targetBufferRef.current,
            frameInterval: avgInterval,
            droppedFrames: droppedFramesRef.current,
          });
        }
      }

      lastPresentationTime = metadata.presentationTime;

      // Schedule next frame callback
      frameCallbackIdRef.current = (video as any).requestVideoFrameCallback(onFrame);
    };

    // Start frame monitoring
    frameCallbackIdRef.current = (video as any).requestVideoFrameCallback(onFrame);

    return () => {
      // Cancel pending callback on cleanup
      if (frameCallbackIdRef.current !== null && 'cancelVideoFrameCallback' in video) {
        (video as any).cancelVideoFrameCallback(frameCallbackIdRef.current);
        frameCallbackIdRef.current = null;
      }
    };
  }, [videoRef]);

  return {
    stats,
    getTargetBuffer: () => targetBufferRef.current,
    resetDroppedFrames: () => {
      droppedFramesRef.current = 0;
      setStats(prev => ({ ...prev, droppedFrames: 0 }));
    },
  };
}

// VideoFrameCallbackMetadata type for TypeScript
interface VideoFrameCallbackMetadata {
  presentationTime: DOMHighResTimeStamp;
  expectedDisplayTime: DOMHighResTimeStamp;
  width: number;
  height: number;
  mediaTime: number;
  presentedFrames: number;
  processingDuration?: number;
  captureTime?: DOMHighResTimeStamp;
  receiveTime?: DOMHighResTimeStamp;
  rtpTimestamp?: number;
}
