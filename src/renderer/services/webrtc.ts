/**
 * WebRTC Service for Remote Desktop
 * Handles peer connection, video streaming, and input via DataChannel
 */

import { wsService } from './websocket';

export interface WebRTCStats {
  fps: number;
  latency: number;
  bitrate: number;
  packetsLost: number;
  jitter: number;
}

export interface InputEvent {
  type: 'move' | 'down' | 'up' | 'wheel' | 'keydown' | 'keyup';
  x?: number;
  y?: number;
  button?: number;
  deltaY?: number;
  key?: string;
  code?: string;
  modifiers?: {
    ctrl?: boolean;
    alt?: boolean;
    shift?: boolean;
    meta?: boolean;
  };
}

type ConnectionStateHandler = (state: RTCPeerConnectionState) => void;
type ErrorHandler = (error: string) => void;
type RemoteInfoHandler = (width: number, height: number) => void;

export class WebRTCService {
  private pc: RTCPeerConnection | null = null;
  private dataChannel: RTCDataChannel | null = null;
  private agentId: string;
  private sessionId: string = '';
  private onConnectionStateChange: ConnectionStateHandler | null = null;
  private onError: ErrorHandler | null = null;
  private onRemoteInfo: RemoteInfoHandler | null = null;

  // Store remote screen dimensions for coordinate mapping
  public remoteWidth: number = 0;
  public remoteHeight: number = 0;
  private unsubscribeSignal: (() => void) | null = null;
  private unsubscribeResponse: (() => void) | null = null;
  private pendingCandidates: RTCIceCandidateInit[] = [];
  private remoteDescriptionSet = false;

  constructor(agentId: string) {
    this.agentId = agentId;
  }

  setOnConnectionStateChange(handler: ConnectionStateHandler): void {
    this.onConnectionStateChange = handler;
  }

  setOnError(handler: ErrorHandler): void {
    this.onError = handler;
  }

  setOnRemoteInfo(handler: RemoteInfoHandler): void {
    this.onRemoteInfo = handler;
  }

  async connect(sessionId: string): Promise<MediaStream> {
    if (!wsService) {
      throw new Error('WebSocket service not available');
    }

    // Clean up any existing connection/subscriptions FIRST
    this.unsubscribeSignal?.();
    this.unsubscribeResponse?.();
    this.unsubscribeSignal = null;
    this.unsubscribeResponse = null;

    if (this.pc) {
      this.pc.close();
      this.pc = null;
    }
    if (this.dataChannel) {
      this.dataChannel.close();
      this.dataChannel = null;
    }

    this.sessionId = sessionId;
    this.remoteDescriptionSet = false;
    this.pendingCandidates = [];

    console.log('[WebRTC] Starting connection for session:', sessionId);

    // Create peer connection with ICE servers (STUN + TURN for NAT traversal)
    // Use STUN only — TURN credentials are requested dynamically via signaling server
    this.pc = new RTCPeerConnection({
      iceServers: [
        { urls: 'stun:stun.l.google.com:19302' },
        { urls: 'stun:stun1.l.google.com:19302' },
      ],
      iceCandidatePoolSize: 10,
    });

    // Create data channel for input (must be created before offer)
    this.dataChannel = this.pc.createDataChannel('input', {
      ordered: true,
    });

    this.dataChannel.onopen = () => {
      console.log('[WebRTC] Data channel opened');
    };

    this.dataChannel.onclose = () => {
      console.log('[WebRTC] Data channel closed');
    };

    this.dataChannel.onerror = (event) => {
      console.error('[WebRTC] Data channel error:', event);
    };

    this.dataChannel.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        console.log('[WebRTC] Data channel message:', data.type);

        if (data.type === 'remoteInfo') {
          this.remoteWidth = data.width;
          this.remoteHeight = data.height;
          console.log('[WebRTC] Remote screen dimensions:', data.width, 'x', data.height);
          this.onRemoteInfo?.(data.width, data.height);
        }
      } catch (err) {
        console.warn('[WebRTC] Failed to parse data channel message:', err);
      }
    };

    // Subscribe to WebRTC signals from server (ICE candidates from agent)
    this.unsubscribeSignal = wsService.on('webrtc_signal', (data: unknown) => {
      this.handleSignal(data);
    });

    // Subscribe to response messages (for SDP answer)
    this.unsubscribeResponse = wsService.on('response', (data: unknown) => {
      this.handleResponse(data);
    });

    // Handle ICE candidates
    this.pc.onicecandidate = (event) => {
      if (event.candidate && wsService) {
        console.log('[WebRTC] Sending ICE candidate');
        wsService.send('webrtc_signal', {
          agentId: this.agentId,
          sessionId: this.sessionId,
          signal: {
            type: 'candidate',
            sessionId: this.sessionId,
            candidate: JSON.stringify(event.candidate.toJSON()),
          },
        });
      }
    };

    this.pc.oniceconnectionstatechange = () => {
      console.log('[WebRTC] ICE connection state:', this.pc?.iceConnectionState);
    };

    this.pc.onicegatheringstatechange = () => {
      console.log('[WebRTC] ICE gathering state:', this.pc?.iceGatheringState);
    };

    // Handle connection state changes
    this.pc.onconnectionstatechange = () => {
      const state = this.pc?.connectionState;
      console.log('[WebRTC] Connection state:', state);
      if (state && this.onConnectionStateChange) {
        this.onConnectionStateChange(state);
      }
      if (state === 'failed') {
        this.onError?.('WebRTC connection failed');
      }
    };

    // Return promise that resolves with the video stream
    return new Promise((resolve, reject) => {
      if (!this.pc) {
        reject(new Error('Peer connection not created'));
        return;
      }

      // Set timeout for connection
      const timeout = setTimeout(() => {
        reject(new Error('WebRTC connection timeout'));
        this.disconnect();
      }, 30000);

      // Handle incoming video track
      this.pc.ontrack = (event) => {
        console.log('[WebRTC] Received track:', event.track.kind);
        if (event.streams && event.streams[0]) {
          clearTimeout(timeout);
          resolve(event.streams[0]);
        }
      };

      // Add transceiver for receiving video
      this.pc.addTransceiver('video', { direction: 'recvonly' });

      // Create and send offer
      this.createAndSendOffer().catch((err) => {
        clearTimeout(timeout);
        reject(err);
      });
    });
  }

  private async createAndSendOffer(): Promise<void> {
    if (!this.pc || !wsService) {
      throw new Error('Not initialized');
    }

    const offer = await this.pc.createOffer();
    await this.pc.setLocalDescription(offer);

    console.log('[WebRTC] Sending offer, SDP length:', offer.sdp?.length);

    // Send offer to server
    wsService.send('webrtc_start', {
      agentId: this.agentId,
      sessionId: this.sessionId,
      offerSdp: offer.sdp,
    });
  }

  private handleResponse(data: unknown): void {
    const response = data as {
      success?: boolean;
      data?: {
        sessionId?: string;
        answerSdp?: string;
      };
      error?: string;
    };

    console.log('[WebRTC] Response received:', {
      hasData: !!response.data,
      hasAnswerSdp: !!response.data?.answerSdp,
      responseSessionId: response.data?.sessionId,
      mySessionId: this.sessionId,
      matches: response.data?.sessionId === this.sessionId,
      success: response.success,
      error: response.error,
    });

    // Check if this is a response for our session
    if (response.data?.sessionId === this.sessionId) {
      // Handle error response
      if (response.success === false || response.error) {
        const errorMsg = response.error || 'Remote desktop connection failed';
        console.error('[WebRTC] Connection error from agent:', errorMsg);
        this.onError?.(errorMsg);
        return;
      }

      // Check if this is a WebRTC answer response
      if (response.data?.answerSdp) {
        console.log('[WebRTC] Received answer, SDP length:', response.data.answerSdp.length);
        void this.setRemoteAnswer(response.data.answerSdp);
      }
    }
  }

  private async setRemoteAnswer(sdp: string): Promise<void> {
    if (!this.pc) return;

    try {
      await this.pc.setRemoteDescription({
        type: 'answer',
        sdp: sdp,
      });
      this.remoteDescriptionSet = true;
      console.log('[WebRTC] Remote description set');

      // Add any pending ICE candidates
      for (const candidate of this.pendingCandidates) {
        try {
          await this.pc.addIceCandidate(candidate);
          console.log('[WebRTC] Added pending ICE candidate');
        } catch (err) {
          console.warn('[WebRTC] Failed to add pending ICE candidate:', err);
        }
      }
      this.pendingCandidates = [];
    } catch (err) {
      console.error('[WebRTC] Failed to set remote description:', err);
      this.onError?.('Failed to set remote description');
    }
  }

  private handleSignal(data: unknown): void {
    const signal = data as {
      sessionId?: string;
      signal?: {
        type?: string;
        candidate?: string;
        sdpMid?: string;
        sdpMLineIndex?: number;
      } | string;
    };

    if (signal.sessionId !== this.sessionId) {
      return;
    }

    let signalData = signal.signal;
    if (typeof signalData === 'string') {
      try {
        signalData = JSON.parse(signalData);
      } catch {
        console.warn('[WebRTC] Failed to parse signal:', signalData);
        return;
      }
    }

    if (!signalData || typeof signalData !== 'object') {
      return;
    }

    const signalObj = signalData as {
      type?: string;
      candidate?: string;
      sdpMid?: string;
      sdpMLineIndex?: number;
    };

    if (signalObj.type === 'candidate' && signalObj.candidate) {
      void this.handleRemoteCandidate(signalObj.candidate);
    }
  }

  private async handleRemoteCandidate(candidateStr: string): Promise<void> {
    if (!this.pc) return;

    try {
      let candidateInit: RTCIceCandidateInit;

      // Parse the candidate - it might be a JSON string or already an object
      if (typeof candidateStr === 'string') {
        try {
          candidateInit = JSON.parse(candidateStr);
        } catch {
          // If parsing fails, treat it as a raw candidate string
          candidateInit = { candidate: candidateStr, sdpMid: '0', sdpMLineIndex: 0 };
        }
      } else {
        candidateInit = candidateStr as unknown as RTCIceCandidateInit;
      }

      if (!this.remoteDescriptionSet) {
        // Queue the candidate for later
        console.log('[WebRTC] Queueing ICE candidate (remote description not set yet)');
        this.pendingCandidates.push(candidateInit);
        return;
      }

      await this.pc.addIceCandidate(candidateInit);
      console.log('[WebRTC] Added remote ICE candidate');
    } catch (err) {
      console.warn('[WebRTC] Failed to add ICE candidate:', err);
    }
  }

  sendInput(input: InputEvent): void {
    if (this.dataChannel?.readyState === 'open') {
      console.log('[WebRTC] sendInput:', input.type, 'x:', input.x, 'y:', input.y, 'button:', input.button);
      this.dataChannel.send(JSON.stringify(input));
    } else {
      console.warn('[WebRTC] sendInput FAILED - dataChannel not open. State:', this.dataChannel?.readyState);
    }
  }

  async getStats(): Promise<WebRTCStats | null> {
    if (!this.pc) return null;

    try {
      const stats = await this.pc.getStats();
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

          // Calculate bitrate from bytes received
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
  }

  disconnect(): void {
    console.log('[WebRTC] Disconnecting');

    // Send stop message
    if (wsService && this.sessionId) {
      wsService.send('webrtc_stop', {
        agentId: this.agentId,
        sessionId: this.sessionId,
      });
    }

    // Unsubscribe from events
    this.unsubscribeSignal?.();
    this.unsubscribeResponse?.();
    this.unsubscribeSignal = null;
    this.unsubscribeResponse = null;

    // Close data channel
    if (this.dataChannel) {
      this.dataChannel.close();
      this.dataChannel = null;
    }

    // Close peer connection
    if (this.pc) {
      this.pc.close();
      this.pc = null;
    }

    this.sessionId = '';
    this.remoteDescriptionSet = false;
    this.pendingCandidates = [];
  }

  get isConnected(): boolean {
    return this.pc?.connectionState === 'connected';
  }

  get connectionState(): RTCPeerConnectionState | null {
    return this.pc?.connectionState || null;
  }
}
