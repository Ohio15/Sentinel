/**
 * Enhanced WebRTC Service with native-feel features
 *
 * Features:
 * - Capability negotiation
 * - Local cursor support
 * - Clipboard sync
 * - Pointer lock support
 * - TURN server integration
 * - Adaptive quality
 */

import { wsService } from './websocket';

// Types
export interface HostCapabilities {
  screens: ScreenInfo[];
  encoders: EncoderInfo[];
  inputCapabilities: InputCapabilities;
  platform: string;
  osVersion: string;
  cpuCores: number;
  gpuName: string;
  dxgiCapture: boolean;
  hardwareEncode: boolean;
  cursorCapture: boolean;
}

export interface ScreenInfo {
  index: number;
  width: number;
  height: number;
  x: number;
  y: number;
  refreshRate: number;
  dpiScale: number;
  isPrimary: boolean;
}

export interface EncoderInfo {
  type: string;
  maxWidth: number;
  maxHeight: number;
  maxFps: number;
  supportsHardware: boolean;
}

export interface InputCapabilities {
  absoluteMouse: boolean;
  relativeMouse: boolean;
  multiTouch: boolean;
  pen: boolean;
  clipboard: boolean;
}

export interface ClientPreferences {
  viewportWidth: number;
  viewportHeight: number;
  devicePixelRatio: number;
  localRefreshRate: number;
  preferredMonitor: number;
  preferredQuality: 'low' | 'medium' | 'high' | 'auto';
  maxBandwidth: number;
  preferLowLatency: boolean;
  pointerLockSupported: boolean;
  touchSupported: boolean;
  clipboardSupported: boolean;
  estimatedRtt: number;
  connectionType: 'lan' | 'wifi' | 'cellular' | 'unknown';
}

export interface NegotiatedSession {
  captureWidth: number;
  captureHeight: number;
  encodeWidth: number;
  encodeHeight: number;
  targetFps: number;
  maxLatencyMs: number;
  encoder: string;
  bitrate: number;
  adaptiveBitrate: boolean;
  coordinateSpace: CoordinateMapping;
  localCursor: boolean;
  clipboardSync: boolean;
  pointerLock: boolean;
}

export interface CoordinateMapping {
  hostVirtualLeft: number;
  hostVirtualTop: number;
  hostVirtualWidth: number;
  hostVirtualHeight: number;
  captureOffsetX: number;
  captureOffsetY: number;
}

export interface TurnCredentials {
  username: string;
  password: string;
  ttl: number;
  urls: string[];
}

export interface InputEvent {
  type: 'mouse' | 'mouse_relative' | 'keyboard' | 'clipboard';
  event?: string;
  x?: number;
  y?: number;
  button?: number;
  deltaX?: number;
  deltaY?: number;
  key?: string;
  code?: string;
  modifiers?: {
    ctrl?: boolean;
    alt?: boolean;
    shift?: boolean;
    meta?: boolean;
  };
  text?: string;
  timestamp?: number;
}

export interface WebRTCStats {
  fps: number;
  latency: number;
  bitrate: number;
  packetsLost: number;
  jitter: number;
  resolution?: { width: number; height: number };
}

// Event callbacks
type EventCallback = (data: unknown) => void;

export class WebRTCEnhancedService {
  private pc: RTCPeerConnection | null = null;
  private dataChannel: RTCDataChannel | null = null;
  private mediaStream: MediaStream | null = null;
  private agentId: string;
  private sessionId: string;

  private capabilities: HostCapabilities | null = null;
  private session: NegotiatedSession | null = null;
  private turnCredentials: TurnCredentials | null = null;

  private eventListeners: Map<string, Set<EventCallback>> = new Map();
  private statsInterval: number | null = null;
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 5;

  private lastBytesReceived = 0;
  private lastStatsTime = 0;

  constructor(agentId: string, sessionId: string) {
    this.agentId = agentId;
    this.sessionId = sessionId;
  }

  // Get client preferences based on current environment
  private getClientPreferences(): ClientPreferences {
    const connection = (navigator as any).connection;

    return {
      viewportWidth: window.innerWidth,
      viewportHeight: window.innerHeight,
      devicePixelRatio: window.devicePixelRatio || 1,
      localRefreshRate: 60, // Could detect via requestAnimationFrame
      preferredMonitor: -1, // -1 for all monitors
      preferredQuality: 'auto',
      maxBandwidth: 0, // 0 = unlimited
      preferLowLatency: true,
      pointerLockSupported: 'pointerLockElement' in document,
      touchSupported: 'ontouchstart' in window,
      clipboardSupported: 'clipboard' in navigator,
      estimatedRtt: 0, // Will be updated after connection
      connectionType: connection?.effectiveType || 'unknown',
    };
  }

  // Request TURN credentials from server
  private async requestTurnCredentials(): Promise<TurnCredentials | null> {
    return new Promise((resolve) => {
      const timeout = setTimeout(() => resolve(null), 5000);

      const handler = (data: any) => {
        clearTimeout(timeout);
        wsService?.off('turn_credentials', handler);
        resolve(data as TurnCredentials);
      };

      wsService?.on('turn_credentials', handler);
      wsService?.send('request_turn_credentials', { agentId: this.agentId });
    });
  }

  // Connect to remote desktop
  async connect(): Promise<MediaStream> {
    this.emit('connecting', {});

    // Get TURN credentials
    this.turnCredentials = await this.requestTurnCredentials();

    // Build ICE servers configuration
    const iceServers: RTCIceServer[] = [
      { urls: 'stun:stun.l.google.com:19302' },
      { urls: 'stun:stun1.l.google.com:19302' },
    ];

    if (this.turnCredentials) {
      iceServers.push({
        urls: this.turnCredentials.urls.filter(u => u.startsWith('turn:')),
        username: this.turnCredentials.username,
        credential: this.turnCredentials.password,
      });
    }

    // Create peer connection
    this.pc = new RTCPeerConnection({
      iceServers,
      iceCandidatePoolSize: 10,
    });

    // Create data channel for input
    this.dataChannel = this.pc.createDataChannel('input', {
      ordered: false,
      maxRetransmits: 0,
    });

    this.dataChannel.onopen = () => {
      console.log('[WebRTC] Data channel open');
      // Send client preferences for negotiation
      this.sendPreferences();
    };

    this.dataChannel.onmessage = (event) => {
      this.handleDataChannelMessage(event.data);
    };

    // Handle incoming video track
    return new Promise((resolve, reject) => {
      const connectionTimeout = setTimeout(() => {
        reject(new Error('Connection timeout'));
      }, 30000);

      this.pc!.ontrack = (event) => {
        console.log('[WebRTC] Track received:', event.track.kind);
        if (event.streams && event.streams[0]) {
          this.mediaStream = event.streams[0];
          clearTimeout(connectionTimeout);
          this.emit('connected', {});
          this.startStatsPolling();
          resolve(event.streams[0]);
        }
      };

      // ICE candidate handling
      this.pc!.onicecandidate = (event) => {
        if (event.candidate) {
          wsService?.send('webrtc_signal', {
            type: 'candidate',
            agentId: this.agentId,
            sessionId: this.sessionId,
            candidate: event.candidate.toJSON(),
          });
        }
      };

      // Connection state changes
      this.pc!.onconnectionstatechange = () => {
        const state = this.pc?.connectionState;
        console.log('[WebRTC] Connection state:', state);

        if (state === 'connected') {
          this.reconnectAttempts = 0;
        } else if (state === 'failed' || state === 'disconnected') {
          this.handleDisconnection();
        }
      };

      // ICE connection state changes
      this.pc!.oniceconnectionstatechange = () => {
        console.log('[WebRTC] ICE state:', this.pc?.iceConnectionState);
      };

      // Add transceiver for receiving video
      this.pc!.addTransceiver('video', { direction: 'recvonly' });

      // Create and send offer
      this.createAndSendOffer();

      // Listen for signaling responses
      this.setupSignalingHandlers();
    });
  }

  private async createAndSendOffer() {
    if (!this.pc) return;

    const offer = await this.pc.createOffer();
    await this.pc.setLocalDescription(offer);

    wsService?.send('webrtc_signal', {
      type: 'offer',
      agentId: this.agentId,
      sessionId: this.sessionId,
      sdp: offer.sdp,
    });
  }

  private setupSignalingHandlers() {
    // Handle SDP answer
    wsService?.on('webrtc_signal', async (data: any) => {
      if (data.sessionId !== this.sessionId) return;

      if (data.type === 'answer' && data.sdp) {
        await this.pc?.setRemoteDescription({
          type: 'answer',
          sdp: data.sdp,
        });
      } else if (data.type === 'candidate' && data.candidate) {
        try {
          await this.pc?.addIceCandidate(data.candidate);
        } catch (err) {
          console.warn('[WebRTC] Failed to add ICE candidate:', err);
        }
      }
    });

    // Handle host capabilities
    wsService?.on('host_capabilities', (data: any) => {
      if (data.sessionId !== this.sessionId) return;
      this.capabilities = data.capabilities;
      this.emit('capabilities', this.capabilities);
    });

    // Handle session negotiation result
    wsService?.on('session_negotiated', (data: any) => {
      if (data.sessionId !== this.sessionId) return;
      this.session = data.session;
      this.emit('session_negotiated', this.session);
    });
  }

  private sendPreferences() {
    const prefs = this.getClientPreferences();

    if (this.dataChannel?.readyState === 'open') {
      this.dataChannel.send(JSON.stringify({
        type: 'preferences',
        ...prefs,
      }));
    }
  }

  private handleDataChannelMessage(data: string) {
    try {
      const msg = JSON.parse(data);

      switch (msg.type) {
        case 'cursor_update':
          this.emit('cursor_update', msg);
          break;
        case 'clipboard':
          this.emit('clipboard', msg);
          break;
        case 'capabilities':
          this.capabilities = msg;
          this.emit('capabilities', msg);
          break;
        case 'session':
          this.session = msg;
          this.emit('session_negotiated', msg);
          break;
        default:
          console.log('[WebRTC] Unknown message type:', msg.type);
      }
    } catch (err) {
      console.warn('[WebRTC] Failed to parse data channel message:', err);
    }
  }

  private handleDisconnection() {
    this.emit('disconnected', {});
    this.stopStatsPolling();

    if (this.reconnectAttempts < this.maxReconnectAttempts) {
      this.reconnectAttempts++;
      const delay = Math.min(1000 * Math.pow(2, this.reconnectAttempts), 16000);
      console.log(`[WebRTC] Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`);

      setTimeout(() => {
        this.connect().catch(err => {
          console.error('[WebRTC] Reconnection failed:', err);
        });
      }, delay);
    } else {
      this.emit('error', 'Connection failed after multiple attempts');
    }
  }

  // Send input event
  sendInput(input: InputEvent) {
    if (this.dataChannel?.readyState !== 'open') return;

    this.dataChannel.send(JSON.stringify(input));
  }

  // Get current stats
  async getStats(): Promise<WebRTCStats> {
    if (!this.pc) {
      return { fps: 0, latency: 0, bitrate: 0, packetsLost: 0, jitter: 0 };
    }

    const stats: WebRTCStats = {
      fps: 0,
      latency: 0,
      bitrate: 0,
      packetsLost: 0,
      jitter: 0,
    };

    const report = await this.pc.getStats();
    const now = Date.now();

    report.forEach((stat) => {
      if (stat.type === 'inbound-rtp' && stat.kind === 'video') {
        stats.fps = stat.framesPerSecond || 0;
        stats.packetsLost = stat.packetsLost || 0;
        stats.jitter = (stat.jitter || 0) * 1000; // Convert to ms

        if (stat.frameWidth && stat.frameHeight) {
          stats.resolution = {
            width: stat.frameWidth,
            height: stat.frameHeight,
          };
        }

        // Calculate bitrate
        if (this.lastStatsTime > 0) {
          const bytesDelta = stat.bytesReceived - this.lastBytesReceived;
          const timeDelta = (now - this.lastStatsTime) / 1000;
          stats.bitrate = (bytesDelta * 8) / timeDelta;
        }
        this.lastBytesReceived = stat.bytesReceived;
      }

      if (stat.type === 'candidate-pair' && stat.state === 'succeeded') {
        stats.latency = (stat.currentRoundTripTime || 0) * 1000;
      }
    });

    this.lastStatsTime = now;
    return stats;
  }

  private startStatsPolling() {
    this.statsInterval = window.setInterval(async () => {
      const stats = await this.getStats();
      this.emit('stats', stats);
    }, 1000);
  }

  private stopStatsPolling() {
    if (this.statsInterval) {
      clearInterval(this.statsInterval);
      this.statsInterval = null;
    }
  }

  // Get current media stream
  getStream(): MediaStream | null {
    return this.mediaStream;
  }

  // Get capabilities
  getCapabilities(): HostCapabilities | null {
    return this.capabilities;
  }

  // Get session
  getSession(): NegotiatedSession | null {
    return this.session;
  }

  // Event handling
  on(event: string, callback: EventCallback): () => void {
    if (!this.eventListeners.has(event)) {
      this.eventListeners.set(event, new Set());
    }
    this.eventListeners.get(event)!.add(callback);

    return () => {
      this.eventListeners.get(event)?.delete(callback);
    };
  }

  off(event: string, callback: EventCallback) {
    this.eventListeners.get(event)?.delete(callback);
  }

  private emit(event: string, data: unknown) {
    this.eventListeners.get(event)?.forEach(cb => cb(data));
  }

  // Disconnect
  disconnect() {
    this.stopStatsPolling();

    if (this.dataChannel) {
      this.dataChannel.close();
      this.dataChannel = null;
    }

    if (this.pc) {
      this.pc.close();
      this.pc = null;
    }

    this.mediaStream = null;
    this.capabilities = null;
    this.session = null;
    this.emit('disconnected', {});
  }

  // Check if connected
  isConnected(): boolean {
    return this.pc?.connectionState === 'connected';
  }
}

export default WebRTCEnhancedService;
