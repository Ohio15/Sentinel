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
export { useClipboard } from './useClipboard';
export { useFileTransfer } from './useFileTransfer';
export { FileTransferPanel } from './FileTransferPanel';

export type { CursorState, CursorShape, CursorPosition } from './useCursor';
export type { InputEvent } from './useInput';
export type { ConnectionState, WebRTCStats } from './useWebRTC';
export type { UseClipboardOptions, UseClipboardReturn } from './useClipboard';
export type { RemoteFile, FileTransfer, UseFileTransferOptions, UseFileTransferReturn } from './useFileTransfer';
