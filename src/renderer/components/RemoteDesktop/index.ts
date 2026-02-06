// High-performance remote desktop implementation
// Features:
// - Local cursor rendering for instant feedback (perceived 0ms latency)
// - DXGI Desktop Duplication for fast capture
// - H.264 hardware encoding (NVENC/QSV/AMF)
// - WebRTC P2P for direct connection when possible
// - Dirty rectangle optimization

export { HighPerformanceRemoteDesktop } from './HighPerformanceRemoteDesktop';
// Re-export as RemoteDesktop for backward compatibility
export { HighPerformanceRemoteDesktop as RemoteDesktop } from './HighPerformanceRemoteDesktop';
export { CursorOverlay } from './CursorOverlay';
export { useCursor } from './useCursor';
export { useWebRTC } from './useWebRTC';
export { useInput } from './useInput';

export type { CursorState, CursorShape, CursorPosition } from './useCursor';
export type { InputEvent } from './useInput';
export type { ConnectionState, WebRTCStats } from './useWebRTC';
