import React, { useState, useRef, useEffect, useCallback } from 'react';

interface RemoteDesktopProps {
  deviceId: string;
  isOnline: boolean;
}

interface WebRTCStats {
  fps: number;
  bitrate: number;
  resolution: string;
}

export function RemoteDesktop({ deviceId, isOnline }: RemoteDesktopProps) {
  const [connecting, setConnecting] = useState(false);
  const [connected, setConnected] = useState(false);
  const [quality, setQuality] = useState<'low' | 'medium' | 'high'>('medium');
  const [fullscreen, setFullscreen] = useState(false);
  const [stats, setStats] = useState<WebRTCStats | null>(null);

  const videoRef = useRef<HTMLVideoElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const peerConnectionRef = useRef<RTCPeerConnection | null>(null);
  const dataChannelRef = useRef<RTCDataChannel | null>(null);
  const pendingCandidatesRef = useRef<RTCIceCandidateInit[]>([]);
  const statsIntervalRef = useRef<NodeJS.Timeout | null>(null);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      cleanup();
    };
  }, []);

  // Listen for WebRTC signaling messages
  useEffect(() => {
    if (!connected && !connecting) return;

    const unsubscribe = window.api.webrtc.onSignal(async (signal) => {
      if (signal.deviceId !== deviceId) return;

      const pc = peerConnectionRef.current;
      if (!pc) return;

      try {
        if (signal.type === 'answer') {
          await pc.setRemoteDescription(new RTCSessionDescription({
            type: 'answer',
            sdp: signal.sdp
          }));

          // Process any pending ICE candidates
          for (const candidate of pendingCandidatesRef.current) {
            await pc.addIceCandidate(new RTCIceCandidate(candidate));
          }
          pendingCandidatesRef.current = [];

        } else if (signal.type === 'candidate' && signal.candidate) {
          if (pc.remoteDescription) {
            await pc.addIceCandidate(new RTCIceCandidate(signal.candidate));
          } else {
            pendingCandidatesRef.current.push(signal.candidate);
          }
        }
      } catch (err) {
        console.error('Error handling WebRTC signal:', err);
      }
    });

    return () => {
      unsubscribe();
    };
  }, [deviceId, connected, connecting]);

  const cleanup = () => {
    if (statsIntervalRef.current) {
      clearInterval(statsIntervalRef.current);
      statsIntervalRef.current = null;
    }

    if (dataChannelRef.current) {
      dataChannelRef.current.close();
      dataChannelRef.current = null;
    }

    if (peerConnectionRef.current) {
      peerConnectionRef.current.close();
      peerConnectionRef.current = null;
    }

    if (videoRef.current) {
      videoRef.current.srcObject = null;
    }

    pendingCandidatesRef.current = [];
    setStats(null);
  };

  const startStatsCollection = () => {
    if (statsIntervalRef.current) return;

    let lastBytes = 0;
    let lastTime = Date.now();

    statsIntervalRef.current = setInterval(async () => {
      const pc = peerConnectionRef.current;
      if (!pc) return;

      try {
        const stats = await pc.getStats();
        let currentBytes = 0;
        let framesDecoded = 0;
        let frameWidth = 0;
        let frameHeight = 0;

        stats.forEach((report) => {
          if (report.type === 'inbound-rtp' && report.kind === 'video') {
            currentBytes = report.bytesReceived || 0;
            framesDecoded = report.framesDecoded || 0;
            frameWidth = report.frameWidth || 0;
            frameHeight = report.frameHeight || 0;
          }
        });

        const now = Date.now();
        const timeDiff = (now - lastTime) / 1000;
        const bytesDiff = currentBytes - lastBytes;
        const bitrate = timeDiff > 0 ? Math.round((bytesDiff * 8) / timeDiff / 1000) : 0;

        lastBytes = currentBytes;
        lastTime = now;

        setStats({
          fps: framesDecoded > 0 ? Math.round(framesDecoded / (now / 1000)) : 0,
          bitrate,
          resolution: frameWidth > 0 ? frameWidth + 'x' + frameHeight : 'N/A'
        });
      } catch (err) {
        console.error('Error getting stats:', err);
      }
    }, 1000);
  };

  const handleConnect = async () => {
    if (!isOnline) return;

    setConnecting(true);
    cleanup();

    try {
      // Create peer connection with STUN servers
      const pc = new RTCPeerConnection({
        iceServers: [
          { urls: 'stun:stun.l.google.com:19302' },
          { urls: 'stun:stun1.l.google.com:19302' }
        ]
      });
      peerConnectionRef.current = pc;

      // Create data channel for input events
      const dc = pc.createDataChannel('input', { ordered: true });
      dataChannelRef.current = dc;

      dc.onopen = () => {
        console.log('Input data channel opened');
      };

      dc.onclose = () => {
        console.log('Input data channel closed');
      };

      // Handle incoming video track
      pc.ontrack = (event) => {
        console.log('Received track:', event.track.kind);
        if (event.track.kind === 'video' && videoRef.current) {
          videoRef.current.srcObject = event.streams[0];
          videoRef.current.play().catch(console.error);
          setConnected(true);
          setConnecting(false);
          startStatsCollection();
        }
      };

      // Handle ICE candidates
      pc.onicecandidate = (event) => {
        if (event.candidate) {
          window.api.webrtc.sendSignal(deviceId, {
            type: 'candidate',
            candidate: event.candidate.toJSON()
          });
        }
      };

      // Handle connection state changes
      pc.onconnectionstatechange = () => {
        console.log('Connection state:', pc.connectionState);
        if (pc.connectionState === 'failed' || pc.connectionState === 'disconnected') {
          handleDisconnect();
        }
      };

      // Handle ICE connection state
      pc.oniceconnectionstatechange = () => {
        console.log('ICE connection state:', pc.iceConnectionState);
      };

      // Add transceiver for receiving video
      pc.addTransceiver('video', { direction: 'recvonly' });

      // Create and send offer
      const offer = await pc.createOffer();
      await pc.setLocalDescription(offer);

      // Start session on agent via server
      await window.api.webrtc.start(deviceId, {
        type: 'offer',
        sdp: offer.sdp,
        quality
      });

    } catch (error: unknown) {
      console.error('Connection error:', error);
      cleanup();
      setConnecting(false);
      alert('Failed to connect: ' + (error instanceof Error ? error.message : 'Unknown error'));
    }
  };

  const handleDisconnect = async () => {
    try {
      await window.api.webrtc.stop(deviceId);
    } catch (err) {
      console.error('Error stopping session:', err);
    }
    cleanup();
    setConnected(false);
    setConnecting(false);
  };

  const handleQualityChange = async (newQuality: 'low' | 'medium' | 'high') => {
    setQuality(newQuality);
    if (connected) {
      try {
        await window.api.webrtc.setQuality(deviceId, newQuality);
      } catch (err) {
        console.error('Error setting quality:', err);
      }
    }
  };

  const sendInput = useCallback((data: object) => {
    const dc = dataChannelRef.current;
    if (dc && dc.readyState === 'open') {
      dc.send(JSON.stringify(data));
    }
  }, []);

  const handleMouseEvent = useCallback((e: React.MouseEvent, eventType: string) => {
    if (!connected || !videoRef.current) return;

    const video = videoRef.current;
    const rect = video.getBoundingClientRect();

    // Calculate position relative to actual video content (accounting for letterboxing)
    const videoAspect = video.videoWidth / video.videoHeight;
    const rectAspect = rect.width / rect.height;

    let videoDisplayWidth: number;
    let videoDisplayHeight: number;
    let offsetX: number;
    let offsetY: number;

    if (rectAspect > videoAspect) {
      // Letterboxed horizontally
      videoDisplayHeight = rect.height;
      videoDisplayWidth = rect.height * videoAspect;
      offsetX = (rect.width - videoDisplayWidth) / 2;
      offsetY = 0;
    } else {
      // Letterboxed vertically
      videoDisplayWidth = rect.width;
      videoDisplayHeight = rect.width / videoAspect;
      offsetX = 0;
      offsetY = (rect.height - videoDisplayHeight) / 2;
    }

    const relX = e.clientX - rect.left - offsetX;
    const relY = e.clientY - rect.top - offsetY;

    // Normalize to 0-1 range
    const x = Math.max(0, Math.min(1, relX / videoDisplayWidth));
    const y = Math.max(0, Math.min(1, relY / videoDisplayHeight));

    sendInput({
      type: 'mouse',
      event: eventType,
      x,
      y,
      button: e.button
    });
  }, [connected, sendInput]);

  const handleWheel = useCallback((e: React.WheelEvent) => {
    if (!connected) return;
    e.preventDefault();

    sendInput({
      type: 'wheel',
      deltaX: e.deltaX,
      deltaY: e.deltaY
    });
  }, [connected, sendInput]);

  const handleKeyEvent = useCallback((e: React.KeyboardEvent, eventType: string) => {
    if (!connected) return;
    e.preventDefault();

    sendInput({
      type: 'keyboard',
      event: eventType,
      key: e.key,
      code: e.code,
      ctrl: e.ctrlKey,
      alt: e.altKey,
      shift: e.shiftKey,
      meta: e.metaKey
    });
  }, [connected, sendInput]);

  const toggleFullscreen = () => {
    if (!containerRef.current) return;

    if (!fullscreen) {
      containerRef.current.requestFullscreen?.();
    } else {
      document.exitFullscreen?.();
    }
    setFullscreen(!fullscreen);
  };

  // Listen for fullscreen changes
  useEffect(() => {
    const handleFullscreenChange = () => {
      setFullscreen(!!document.fullscreenElement);
    };
    document.addEventListener('fullscreenchange', handleFullscreenChange);
    return () => {
      document.removeEventListener('fullscreenchange', handleFullscreenChange);
    };
  }, []);

  if (!isOnline) {
    return (
      <div className="h-96 flex items-center justify-center">
        <p className="text-text-secondary">Device is offline. Remote desktop is not available.</p>
      </div>
    );
  }

  const statusClass = connected ? 'bg-green-500' : connecting ? 'bg-yellow-500' : 'bg-red-500';
  const statusText = connected ? 'Connected' : connecting ? 'Connecting...' : 'Disconnected';

  return (
    <div ref={containerRef} className="flex flex-col h-96">
      {/* Toolbar */}
      <div className="flex items-center justify-between px-4 py-2 bg-gray-800">
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <div className={'w-3 h-3 rounded-full ' + statusClass} />
            <span className="text-sm text-gray-300">{statusText}</span>
          </div>

          {connected && (
            <>
              <div className="h-4 w-px bg-gray-600" />
              <select
                value={quality}
                onChange={e => handleQualityChange(e.target.value as 'low' | 'medium' | 'high')}
                className="px-2 py-1 text-sm bg-gray-700 text-gray-200 rounded border border-gray-600"
              >
                <option value="low">Low (720p)</option>
                <option value="medium">Medium (1080p)</option>
                <option value="high">High (1080p+)</option>
              </select>

              {stats && (
                <>
                  <div className="h-4 w-px bg-gray-600" />
                  <span className="text-xs text-gray-400">
                    {stats.resolution} | {stats.bitrate} kbps
                  </span>
                </>
              )}
            </>
          )}
        </div>

        <div className="flex items-center gap-2">
          {connected && (
            <button
              onClick={toggleFullscreen}
              className="p-1 text-gray-400 hover:text-gray-200 transition-colors"
              title="Toggle fullscreen"
            >
              <FullscreenIcon className="w-5 h-5" />
            </button>
          )}

          {connected ? (
            <button
              onClick={handleDisconnect}
              className="px-3 py-1 text-sm bg-red-600 text-white rounded hover:bg-red-700 transition-colors"
            >
              Disconnect
            </button>
          ) : (
            <button
              onClick={handleConnect}
              disabled={connecting}
              className="px-3 py-1 text-sm bg-green-600 text-white rounded hover:bg-green-700 transition-colors disabled:opacity-50"
            >
              {connecting ? 'Connecting...' : 'Connect'}
            </button>
          )}
        </div>
      </div>

      {/* Remote View */}
      <div className="flex-1 bg-black flex items-center justify-center overflow-hidden">
        {connected || connecting ? (
          <video
            ref={videoRef}
            className="max-w-full max-h-full object-contain"
            autoPlay
            playsInline
            muted
            tabIndex={0}
            onMouseDown={e => handleMouseEvent(e, 'mousedown')}
            onMouseUp={e => handleMouseEvent(e, 'mouseup')}
            onMouseMove={e => handleMouseEvent(e, 'mousemove')}
            onWheel={handleWheel}
            onKeyDown={e => handleKeyEvent(e, 'keydown')}
            onKeyUp={e => handleKeyEvent(e, 'keyup')}
            onContextMenu={e => e.preventDefault()}
            style={{ cursor: connected ? 'none' : 'default' }}
          />
        ) : (
          <div className="text-center">
            <RemoteIcon className="w-16 h-16 text-gray-600 mx-auto mb-4" />
            <p className="text-gray-400">Click Connect to start a remote desktop session</p>
            <p className="text-gray-500 text-sm mt-2">
              WebRTC-based streaming with H.264 encoding
            </p>
          </div>
        )}
      </div>

      {/* Status Bar */}
      {connected && (
        <div className="px-4 py-2 bg-gray-800 border-t border-gray-700 text-sm text-gray-400">
          <span>Remote Desktop Session Active</span>
          <span className="mx-2">|</span>
          <span>Quality: {quality}</span>
          <span className="mx-2">|</span>
          <span>Click in the view to enable keyboard input</span>
        </div>
      )}
    </div>
  );
}

// Icons
function FullscreenIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5l-5-5m5 5v-4m0 4h-4" />
    </svg>
  );
}

function RemoteIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
    </svg>
  );
}
