import { useCallback, useRef, useState, useEffect } from 'react';
import { wsService } from '../../services/websocket';
import { InputEvent } from './useInput';
import { CursorShape } from './useCursor';

export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'failed';

interface RemoteInfo {
  width: number;
  height: number;
}

interface CursorUpdate {
  type: 'cursor';
  x: number;
  y: number;
  visible: boolean;
  shape?: string; // Cursor type or base64 image
  hotspot?: { x: number; y: number };
}

// Frame timing data from server for latency analysis
export interface FrameTiming {
  type: 'frameTiming';
  frameId: number;
  captureMs: number;  // Time to capture screen
  convertMs: number;  // Time to convert RGBA to YCbCr
  encodeMs: number;   // Time to encode to H.264
  totalMs: number;    // Total pipeline time
  timestamp: number;  // Unix microseconds when capture started
}

// RTT measurement
export interface LatencyStats {
  rtt: number;           // Round-trip time in ms
  serverCapture: number; // Server capture time in ms
  serverConvert: number; // Server convert time in ms
  serverEncode: number;  // Server encode time in ms
  serverTotal: number;   // Server total pipeline time in ms
}

interface UseWebRTCOptions {
  agentId: string;
  onVideoTrack: (track: MediaStreamTrack) => void;
  onRemoteInfo: (info: RemoteInfo) => void;
  onCursorUpdate?: (update: CursorUpdate) => void;
  onCursorShape?: (shape: CursorShape) => void;
  onFrameTiming?: (timing: FrameTiming) => void;
  onLatencyUpdate?: (stats: LatencyStats) => void;
}

export interface WebRTCStats {
  fps: number;
  latency: number;
  bitrate: number;
  packetsLost: number;
  jitter: number;
}

export function useWebRTC(options: UseWebRTCOptions) {
  const { agentId, onVideoTrack, onRemoteInfo, onCursorUpdate, onCursorShape, onFrameTiming, onLatencyUpdate } = options;

  const pcRef = useRef<RTCPeerConnection | null>(null);
  const dcRef = useRef<RTCDataChannel | null>(null);

  const [connectionState, setConnectionState] = useState<ConnectionState>('disconnected');
  const [sessionId, setSessionId] = useState<string>('');

  const unsubscribeSignalRef = useRef<(() => void) | null>(null);
  const unsubscribeResponseRef = useRef<(() => void) | null>(null);
  const pendingCandidatesRef = useRef<RTCIceCandidateInit[]>([]);
  const remoteDescriptionSetRef = useRef<boolean>(false);
  const sessionIdRef = useRef<string>(''); // Use ref to avoid stale closure issues
  const connectResolveRef = useRef<((stream: MediaStream) => void) | null>(null);
  const connectRejectRef = useRef<((err: Error) => void) | null>(null);
  const connectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pingIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const lastFrameTimingRef = useRef<FrameTiming | null>(null);

  // Initialize WebRTC peer connection
  const initPeerConnection = useCallback(() => {
    const pc = new RTCPeerConnection({
      iceServers: [
        { urls: 'stun:stun.l.google.com:19302' },
        { urls: 'stun:stun1.l.google.com:19302' },
        { urls: 'stun:stun2.l.google.com:19302' },
      ],
      iceCandidatePoolSize: 10,
    });

    pc.oniceconnectionstatechange = () => {
      console.log('[WebRTC] ICE connection state:', pc.iceConnectionState);
      if (pc.iceConnectionState === 'connected') {
        setConnectionState('connected');
      } else if (pc.iceConnectionState === 'failed') {
        setConnectionState('failed');
      } else if (pc.iceConnectionState === 'disconnected') {
        // Don't immediately set to disconnected - may reconnect
        console.log('[WebRTC] ICE disconnected, may reconnect...');
      }
    };

    pc.onconnectionstatechange = () => {
      console.log('[WebRTC] Connection state:', pc.connectionState);
      if (pc.connectionState === 'connected') {
        setConnectionState('connected');
      } else if (pc.connectionState === 'failed') {
        setConnectionState('failed');
      } else if (pc.connectionState === 'closed') {
        setConnectionState('disconnected');
      }
    };

    pc.ontrack = (event) => {
      console.log('[WebRTC] Received track:', event.track.kind);
      if (event.track.kind === 'video') {
        onVideoTrack(event.track);
        // Resolve the connect promise if waiting
        if (connectResolveRef.current && event.streams[0]) {
          if (connectTimeoutRef.current) {
            clearTimeout(connectTimeoutRef.current);
            connectTimeoutRef.current = null;
          }
          connectResolveRef.current(event.streams[0]);
          connectResolveRef.current = null;
          connectRejectRef.current = null;
        }
      }
    };

    // Create data channel for input (ordered, reliable for input events)
    const dc = pc.createDataChannel('input', {
      ordered: true,
    });

    dc.onopen = () => {
      console.log('[WebRTC] Data channel opened');
    };

    dc.onclose = () => {
      console.log('[WebRTC] Data channel closed');
    };

    dc.onerror = (event) => {
      console.error('[WebRTC] Data channel error:', event);
    };

    dc.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);

        // Handle cursor updates from agent
        if (data.type === 'cursor') {
          onCursorUpdate?.(data as CursorUpdate);
        }
        // Handle cursor shape changes
        else if (data.type === 'cursorShape') {
          onCursorShape?.(data.shape as CursorShape);
        }
        // Handle remote info (screen dimensions)
        else if (data.type === 'remoteInfo') {
          onRemoteInfo({ width: data.width, height: data.height });
        }
        // Handle frame timing data (latency instrumentation)
        else if (data.type === 'frameTiming') {
          const timing = data as FrameTiming;
          lastFrameTimingRef.current = timing;
          onFrameTiming?.(timing);
        }
        // Handle pong response (RTT measurement)
        else if (data.type === 'pong') {
          const now = performance.now() * 1000; // Convert to microseconds
          const clientT = data.clientT as number;
          const rtt = (now - clientT) / 1000; // Convert to ms

          // Combine with last frame timing if available
          const lastTiming = lastFrameTimingRef.current;
          if (lastTiming) {
            onLatencyUpdate?.({
              rtt,
              serverCapture: lastTiming.captureMs,
              serverConvert: lastTiming.convertMs,
              serverEncode: lastTiming.encodeMs,
              serverTotal: lastTiming.totalMs,
            });
          } else {
            onLatencyUpdate?.({
              rtt,
              serverCapture: 0,
              serverConvert: 0,
              serverEncode: 0,
              serverTotal: 0,
            });
          }
        }
      } catch (err) {
        console.warn('[WebRTC] Failed to parse data channel message:', err);
      }
    };

    // Start ping interval when data channel opens
    const originalOnOpen = dc.onopen;
    dc.onopen = () => {
      originalOnOpen?.call(dc);
      // Send ping every 2 seconds for RTT measurement
      pingIntervalRef.current = setInterval(() => {
        if (dc.readyState === 'open') {
          dc.send(JSON.stringify({
            type: 'ping',
            clientT: performance.now() * 1000, // Microseconds
          }));
        }
      }, 2000);
    };

    // Stop ping interval when data channel closes
    const originalOnClose = dc.onclose;
    dc.onclose = () => {
      originalOnClose?.call(dc);
      if (pingIntervalRef.current) {
        clearInterval(pingIntervalRef.current);
        pingIntervalRef.current = null;
      }
    };

    dcRef.current = dc;
    pcRef.current = pc;

    return pc;
  }, [onVideoTrack, onRemoteInfo, onCursorUpdate, onCursorShape, onFrameTiming, onLatencyUpdate]);

  // Handle remote ICE candidate
  const handleRemoteCandidate = useCallback(async (candidateStr: string) => {
    const pc = pcRef.current;
    if (!pc) return;

    try {
      let candidateInit: RTCIceCandidateInit;

      if (typeof candidateStr === 'string') {
        try {
          candidateInit = JSON.parse(candidateStr);
        } catch {
          candidateInit = { candidate: candidateStr, sdpMid: '0', sdpMLineIndex: 0 };
        }
      } else {
        candidateInit = candidateStr as unknown as RTCIceCandidateInit;
      }

      if (!remoteDescriptionSetRef.current) {
        console.log('[WebRTC] Queueing ICE candidate (remote description not set yet)');
        pendingCandidatesRef.current.push(candidateInit);
        return;
      }

      await pc.addIceCandidate(candidateInit);
      console.log('[WebRTC] Added remote ICE candidate');
    } catch (err) {
      console.warn('[WebRTC] Failed to add ICE candidate:', err);
    }
  }, []);

  // Set remote answer
  const setRemoteAnswer = useCallback(async (sdp: string) => {
    const pc = pcRef.current;
    if (!pc) {
      console.error('[WebRTC] setRemoteAnswer called but peer connection is null');
      return;
    }

    console.log('[WebRTC] Setting remote answer, pc state:', pc.connectionState, 'signaling:', pc.signalingState);

    try {
      await pc.setRemoteDescription({
        type: 'answer',
        sdp: sdp,
      });
      remoteDescriptionSetRef.current = true;
      console.log('[WebRTC] Remote description set successfully, new signaling state:', pc.signalingState);

      // Add any pending ICE candidates
      for (const candidate of pendingCandidatesRef.current) {
        try {
          await pc.addIceCandidate(candidate);
          console.log('[WebRTC] Added pending ICE candidate');
        } catch (err) {
          console.warn('[WebRTC] Failed to add pending ICE candidate:', err);
        }
      }
      pendingCandidatesRef.current = [];
    } catch (err) {
      console.error('[WebRTC] Failed to set remote description:', err);
      setConnectionState('failed');
    }
  }, []);

  // Connect to agent via WebRTC
  const connect = useCallback(async (): Promise<MediaStream> => {
    if (!wsService) {
      throw new Error('WebSocket service not available');
    }

    // Clean up any existing connection/subscriptions FIRST
    unsubscribeSignalRef.current?.();
    unsubscribeResponseRef.current?.();
    unsubscribeSignalRef.current = null;
    unsubscribeResponseRef.current = null;

    if (pcRef.current) {
      pcRef.current.close();
      pcRef.current = null;
    }
    if (dcRef.current) {
      dcRef.current.close();
      dcRef.current = null;
    }
    if (connectTimeoutRef.current) {
      clearTimeout(connectTimeoutRef.current);
      connectTimeoutRef.current = null;
    }
    connectResolveRef.current = null;
    connectRejectRef.current = null;

    setConnectionState('connecting');
    remoteDescriptionSetRef.current = false;
    pendingCandidatesRef.current = [];

    const newSessionId = `webrtc-${agentId}-${Date.now()}`;
    sessionIdRef.current = newSessionId; // Store in ref for closure safety
    setSessionId(newSessionId);

    // Ensure WebSocket is connected
    if (!wsService.isConnected) {
      console.log('[WebRTC] Waiting for WebSocket connection...');
      wsService.connect();

      // Wait for connection (up to 5 seconds)
      const connected = await new Promise<boolean>((resolve) => {
        const checkInterval = setInterval(() => {
          if (wsService.isConnected) {
            clearInterval(checkInterval);
            resolve(true);
          }
        }, 100);
        setTimeout(() => {
          clearInterval(checkInterval);
          resolve(false);
        }, 5000);
      });

      if (!connected) {
        setConnectionState('failed');
        throw new Error('WebSocket connection failed');
      }
    }

    const pc = initPeerConnection();

    // Subscribe to WebRTC signals from server
    unsubscribeSignalRef.current = wsService.on('webrtc_signal', (data: unknown) => {
      const signal = data as {
        sessionId?: string;
        signal?: {
          type?: string;
          candidate?: string;
        } | string;
      };

      if (signal.sessionId !== newSessionId) return;

      let signalData = signal.signal;
      if (typeof signalData === 'string') {
        try {
          signalData = JSON.parse(signalData);
        } catch {
          return;
        }
      }

      if (signalData && typeof signalData === 'object') {
        const signalObj = signalData as { type?: string; candidate?: string };
        if (signalObj.type === 'candidate' && signalObj.candidate) {
          handleRemoteCandidate(signalObj.candidate);
        }
      }
    });

    // Subscribe to response messages (for SDP answer)
    console.log('[WebRTC] Subscribing to response events for session:', newSessionId);
    unsubscribeResponseRef.current = wsService.on('response', (data: unknown) => {
      const response = data as {
        success?: boolean;
        data?: {
          sessionId?: string;
          answerSdp?: string;
        };
      };

      console.log('[WebRTC] Response received:', {
        hasData: !!response.data,
        hasAnswerSdp: !!response.data?.answerSdp,
        responseSessionId: response.data?.sessionId,
        expectedSessionId: newSessionId,
        matches: response.data?.sessionId === newSessionId,
      });

      if (response.data?.answerSdp && response.data?.sessionId === newSessionId) {
        console.log('[WebRTC] Received answer, SDP length:', response.data.answerSdp.length);
        setRemoteAnswer(response.data.answerSdp);
      }
    });

    // Handle local ICE candidates
    pc.onicecandidate = (event) => {
      if (event.candidate) {
        console.log('[WebRTC] Sending ICE candidate');
        wsService.send('webrtc_signal', {
          agentId: agentId,
          sessionId: newSessionId,
          signal: {
            type: 'candidate',
            sessionId: newSessionId,
            candidate: JSON.stringify(event.candidate.toJSON()),
          },
        });
      }
    };

    // Add transceiver for receiving video
    pc.addTransceiver('video', { direction: 'recvonly' });

    // Create and send offer
    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);

    console.log('[WebRTC] Sending offer, SDP length:', offer.sdp?.length);
    wsService.send('webrtc_start', {
      agentId: agentId,
      sessionId: newSessionId,
      offerSdp: offer.sdp,
    });

    // Return promise that resolves with video stream
    return new Promise((resolve, reject) => {
      connectResolveRef.current = resolve;
      connectRejectRef.current = reject;

      connectTimeoutRef.current = setTimeout(() => {
        console.error('[WebRTC] Connection timeout');
        connectResolveRef.current = null;
        connectRejectRef.current = null;
        connectTimeoutRef.current = null;
        reject(new Error('WebRTC connection timeout'));
        // Don't call disconnect() here - let caller handle it
      }, 30000);

      // ontrack handler in initPeerConnection will resolve the promise
    });
  }, [agentId, initPeerConnection, handleRemoteCandidate, setRemoteAnswer]);

  // Disconnect
  const disconnect = useCallback(() => {
    console.log('[WebRTC] Disconnecting, sessionId:', sessionIdRef.current);

    // Send stop message using ref (avoids stale closure)
    if (wsService && sessionIdRef.current) {
      wsService.send('webrtc_stop', {
        agentId: agentId,
        sessionId: sessionIdRef.current,
      });
    }

    // Clear any pending timeout
    if (connectTimeoutRef.current) {
      clearTimeout(connectTimeoutRef.current);
      connectTimeoutRef.current = null;
    }

    // Clear ping interval
    if (pingIntervalRef.current) {
      clearInterval(pingIntervalRef.current);
      pingIntervalRef.current = null;
    }

    // Reject any pending connect promise
    if (connectRejectRef.current) {
      connectRejectRef.current(new Error('Connection cancelled'));
      connectResolveRef.current = null;
      connectRejectRef.current = null;
    }

    // Unsubscribe from events
    unsubscribeSignalRef.current?.();
    unsubscribeResponseRef.current?.();
    unsubscribeSignalRef.current = null;
    unsubscribeResponseRef.current = null;

    // Close data channel
    if (dcRef.current) {
      dcRef.current.close();
      dcRef.current = null;
    }

    // Close peer connection
    if (pcRef.current) {
      pcRef.current.close();
      pcRef.current = null;
    }

    sessionIdRef.current = '';
    setSessionId('');
    setConnectionState('disconnected');
    remoteDescriptionSetRef.current = false;
    pendingCandidatesRef.current = [];
  }, [agentId]);

  // Send input event via data channel
  const sendInput = useCallback((event: InputEvent) => {
    const dc = dcRef.current;
    console.log('[WebRTC] sendInput called:', event.type, 'dc:', dc ? 'exists' : 'null', 'readyState:', dc?.readyState);
    if (dc && dc.readyState === 'open') {
      const jsonStr = JSON.stringify(event);
      console.log('[WebRTC] Sending via data channel:', jsonStr);
      dc.send(jsonStr);
      console.log('[WebRTC] Sent successfully');
    } else {
      console.warn('[WebRTC] sendInput DROPPED - data channel not open:', dc?.readyState, 'event:', event.type);
    }
  }, []);

  // Get connection stats
  const getStats = useCallback(async (): Promise<WebRTCStats | null> => {
    const pc = pcRef.current;
    if (!pc) return null;

    try {
      const stats = await pc.getStats();
      let fps = 0;
      let latency = 0;
      let bitrate = 0;
      let packetsLost = 0;
      let jitter = 0;

      stats.forEach((report) => {
        if (report.type === 'inbound-rtp' && report.kind === 'video') {
          fps = report.framesPerSecond || 0;
          packetsLost = report.packetsLost || 0;
          jitter = report.jitter || 0;

          if (report.bytesReceived && report.timestamp) {
            bitrate = (report.bytesReceived * 8) / (report.timestamp / 1000);
          }
        }
        if (report.type === 'candidate-pair' && report.state === 'succeeded') {
          latency = report.currentRoundTripTime ? report.currentRoundTripTime * 1000 : 0;
        }
      });

      return { fps, latency, bitrate, packetsLost, jitter };
    } catch {
      return null;
    }
  }, []);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      disconnect();
    };
  }, [disconnect]);

  return {
    connect,
    disconnect,
    sendInput,
    getStats,
    connectionState,
    isConnected: connectionState === 'connected',
    dataChannel: dcRef.current,
  };
}
