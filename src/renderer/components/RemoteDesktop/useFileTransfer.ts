import { useCallback, useEffect, useRef, useState } from 'react';

// ─── Types ───────────────────────────────────────────────────────────────────

export interface RemoteFile {
  name: string;
  path: string;
  size: number;
  is_dir: boolean;
  modified_time: string;
  mode?: string;
}

export interface FileTransfer {
  id: string;
  name: string;
  size: number;
  transferred: number;
  progress: number;
  state: 'queued' | 'in_progress' | 'paused' | 'completed' | 'failed' | 'canceled';
  direction: 'upload' | 'download';
  speed: number; // bytes per second
  error?: string;
}

interface DirectoryListing {
  path: string;
  files: RemoteFile[];
  hasMore?: boolean;
}

interface TransferResponse {
  type: string;
  transfer?: {
    id: string;
    direction: string;
    sourcePath: string;
    destPath: string;
    fileSize: number;
    transferred: number;
    state: string;
    speed: number;
    progress: number;
    chunksTotal: number;
    chunksSent: number;
  };
  error?: string;
}

interface ChunkAckMsg {
  type: string;
  ack: {
    transferId: string;
    index: number;
    success: boolean;
    error?: string;
  };
}

interface ProgressMsg {
  type: string;
  transfer: {
    id: string;
    state: string;
    transferred: number;
    speed: number;
    progress: number;
    fileSize: number;
  };
}

interface ErrorMsg {
  type: string;
  transferId?: string;
  error: string;
  details?: string;
}

export interface UseFileTransferOptions {
  peerConnection: RTCPeerConnection | null;
  isConnected: boolean;
}

export interface UseFileTransferReturn {
  files: RemoteFile[];
  currentPath: string;
  transfers: FileTransfer[];
  listDirectory: (path: string) => void;
  uploadFile: (file: File) => void;
  downloadFile: (path: string) => void;
  pauseTransfer: (id: string) => void;
  resumeTransfer: (id: string) => void;
  cancelTransfer: (id: string) => void;
  navigateUp: () => void;
  isOpen: boolean;
  setIsOpen: (open: boolean) => void;
}

// ─── Constants ───────────────────────────────────────────────────────────────

const CHUNK_SIZE = 64 * 1024; // 64KB, matching agent-side DefaultChunkSize

// ─── Hook ────────────────────────────────────────────────────────────────────

export function useFileTransfer(options: UseFileTransferOptions): UseFileTransferReturn {
  const { peerConnection, isConnected } = options;

  const [files, setFiles] = useState<RemoteFile[]>([]);
  const [currentPath, setCurrentPath] = useState<string>('');
  const [transfers, setTransfers] = useState<FileTransfer[]>([]);
  const [isOpen, setIsOpen] = useState(false);

  const dcRef = useRef<RTCDataChannel | null>(null);
  const transfersRef = useRef<FileTransfer[]>([]);

  // Keep ref in sync for use inside callbacks without stale closures
  const syncTransfers = useCallback((updater: (prev: FileTransfer[]) => FileTransfer[]) => {
    setTransfers((prev) => {
      const next = updater(prev);
      transfersRef.current = next;
      return next;
    });
  }, []);

  // Pending response handlers keyed by message type for request/response patterns
  const pendingListDir = useRef<((listing: DirectoryListing | null, error?: string) => void) | null>(null);

  // Track upload chunk ACK wait state per transfer
  const uploadState = useRef<Map<string, { resolve: () => void; reject: (err: Error) => void }>>(new Map());

  // Track download chunk accumulation per transfer
  const downloadState = useRef<Map<string, { chunks: Uint8Array[]; totalSize: number; fileName: string }>>(new Map());

  // ─── Data Channel Setup ──────────────────────────────────────────────────

  useEffect(() => {
    if (!peerConnection || !isConnected) {
      return;
    }

    // Create a dedicated data channel for file transfer (ordered, reliable)
    const dc = peerConnection.createDataChannel('filetransfer', {
      ordered: true,
    });

    dc.binaryType = 'arraybuffer';

    dc.onopen = () => {
      console.log('[FileTransfer] Data channel opened');
      dcRef.current = dc;
    };

    dc.onclose = () => {
      console.log('[FileTransfer] Data channel closed');
      dcRef.current = null;
    };

    dc.onerror = (event) => {
      console.error('[FileTransfer] Data channel error:', event);
    };

    dc.onmessage = (event) => {
      handleMessage(event.data);
    };

    return () => {
      if (dc.readyState === 'open' || dc.readyState === 'connecting') {
        dc.close();
      }
      dcRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [peerConnection, isConnected]);

  // ─── Message Handler ─────────────────────────────────────────────────────

  const handleMessage = useCallback((rawData: string | ArrayBuffer) => {
    let data: string;
    if (rawData instanceof ArrayBuffer) {
      data = new TextDecoder().decode(rawData);
    } else {
      data = rawData;
    }

    let msg: { type: string; [key: string]: unknown };
    try {
      msg = JSON.parse(data);
    } catch (err) {
      console.warn('[FileTransfer] Failed to parse message:', err);
      return;
    }

    switch (msg.type) {
      case 'file.listDirResp':
        handleListDirResp(msg as unknown as { type: string; listing?: DirectoryListing; error?: string });
        break;

      case 'file.transferResp':
        handleTransferResp(msg as unknown as TransferResponse);
        break;

      case 'file.chunk':
        handleIncomingChunk(msg as unknown as { type: string; chunk: { transferId: string; index: number; offset: number; size: number; data: number[]; hash?: string; isLast: boolean } });
        break;

      case 'file.chunkAck':
        handleChunkAck(msg as unknown as ChunkAckMsg);
        break;

      case 'file.progress':
        handleProgress(msg as unknown as ProgressMsg);
        break;

      case 'file.complete':
        handleComplete(msg as unknown as ProgressMsg);
        break;

      case 'file.error':
        handleError(msg as unknown as ErrorMsg);
        break;

      default:
        console.log('[FileTransfer] Unknown message type:', msg.type);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ─── Response Handlers ───────────────────────────────────────────────────

  const handleListDirResp = useCallback((msg: { type: string; listing?: DirectoryListing; error?: string }) => {
    if (msg.error) {
      console.error('[FileTransfer] ListDir error:', msg.error);
      if (pendingListDir.current) {
        pendingListDir.current(null, msg.error);
        pendingListDir.current = null;
      }
      return;
    }

    if (msg.listing) {
      setCurrentPath(msg.listing.path);
      setFiles(msg.listing.files || []);
    }

    if (pendingListDir.current) {
      pendingListDir.current(msg.listing || null);
      pendingListDir.current = null;
    }
  }, []);

  const handleTransferResp = useCallback((msg: TransferResponse) => {
    if (msg.error) {
      console.error('[FileTransfer] Transfer start error:', msg.error);
      return;
    }

    if (msg.transfer) {
      const t = msg.transfer;
      syncTransfers((prev) => {
        const existing = prev.find((x) => x.id === t.id);
        if (existing) {
          return prev.map((x) =>
            x.id === t.id
              ? { ...x, state: t.state as FileTransfer['state'], transferred: t.transferred, progress: t.progress, speed: t.speed }
              : x
          );
        }

        return [
          ...prev,
          {
            id: t.id,
            name: t.direction === 'upload' ? t.destPath.split(/[\\/]/).pop() || t.destPath : t.sourcePath.split(/[\\/]/).pop() || t.sourcePath,
            size: t.fileSize,
            transferred: t.transferred,
            progress: t.progress,
            state: t.state as FileTransfer['state'],
            direction: t.direction as FileTransfer['direction'],
            speed: t.speed,
          },
        ];
      });
    }
  }, [syncTransfers]);

  const handleIncomingChunk = useCallback((msg: { type: string; chunk: { transferId: string; index: number; offset: number; size: number; data: number[]; hash?: string; isLast: boolean } }) => {
    const chunk = msg.chunk;
    const state = downloadState.current.get(chunk.transferId);
    if (!state) {
      console.warn('[FileTransfer] Received chunk for unknown download:', chunk.transferId);
      return;
    }

    // chunk.data is a byte array from JSON (Go encodes []byte as base64 in JSON)
    // Decode base64 string or handle raw byte array
    let bytes: Uint8Array;
    if (typeof (chunk.data as unknown) === 'string') {
      // Base64 encoded
      const binary = atob(chunk.data as unknown as string);
      bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i++) {
        bytes[i] = binary.charCodeAt(i);
      }
    } else if (Array.isArray(chunk.data)) {
      bytes = new Uint8Array(chunk.data);
    } else {
      console.warn('[FileTransfer] Unexpected chunk data format');
      return;
    }

    state.chunks.push(bytes);
    state.totalSize += bytes.length;

    // Update transfer progress
    syncTransfers((prev) =>
      prev.map((t) =>
        t.id === chunk.transferId
          ? { ...t, transferred: state.totalSize, progress: t.size > 0 ? (state.totalSize / t.size) * 100 : 0 }
          : t
      )
    );

    if (chunk.isLast) {
      // Assemble all chunks into a single Blob and trigger browser download
      const blob = new Blob(state.chunks);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = state.fileName;
      document.body.appendChild(a);
      a.click();

      // Cleanup
      setTimeout(() => {
        URL.revokeObjectURL(url);
        document.body.removeChild(a);
      }, 100);

      downloadState.current.delete(chunk.transferId);

      syncTransfers((prev) =>
        prev.map((t) =>
          t.id === chunk.transferId ? { ...t, state: 'completed', progress: 100, transferred: t.size } : t
        )
      );
    }
  }, [syncTransfers]);

  const handleChunkAck = useCallback((msg: ChunkAckMsg) => {
    const ack = msg.ack;
    const pending = uploadState.current.get(`${ack.transferId}:${ack.index}`);
    if (pending) {
      if (ack.success) {
        pending.resolve();
      } else {
        pending.reject(new Error(ack.error || 'Chunk write failed'));
      }
      uploadState.current.delete(`${ack.transferId}:${ack.index}`);
    }
  }, []);

  const handleProgress = useCallback((msg: ProgressMsg) => {
    const t = msg.transfer;
    syncTransfers((prev) =>
      prev.map((x) =>
        x.id === t.id
          ? {
              ...x,
              state: t.state as FileTransfer['state'],
              transferred: t.transferred,
              progress: t.progress,
              speed: t.speed,
            }
          : x
      )
    );
  }, [syncTransfers]);

  const handleComplete = useCallback((msg: ProgressMsg) => {
    const t = msg.transfer;
    syncTransfers((prev) =>
      prev.map((x) =>
        x.id === t.id
          ? {
              ...x,
              state: 'completed' as const,
              transferred: t.transferred || x.size,
              progress: 100,
              speed: t.speed,
            }
          : x
      )
    );
  }, [syncTransfers]);

  const handleError = useCallback((msg: ErrorMsg) => {
    console.error('[FileTransfer] Error:', msg.error, msg.details);
    if (msg.transferId) {
      syncTransfers((prev) =>
        prev.map((x) =>
          x.id === msg.transferId
            ? { ...x, state: 'failed' as const, error: msg.error }
            : x
        )
      );
    }
  }, [syncTransfers]);

  // ─── Sending Helpers ─────────────────────────────────────────────────────

  const sendMessage = useCallback((msg: object) => {
    const dc = dcRef.current;
    if (!dc || dc.readyState !== 'open') {
      console.warn('[FileTransfer] Data channel not open, cannot send message');
      return false;
    }

    try {
      dc.send(JSON.stringify(msg));
      return true;
    } catch (err) {
      console.error('[FileTransfer] Failed to send message:', err);
      return false;
    }
  }, []);

  // ─── Public API ──────────────────────────────────────────────────────────

  const listDirectory = useCallback((path: string) => {
    sendMessage({ type: 'file.listDir', path });
  }, [sendMessage]);

  const navigateUp = useCallback(() => {
    if (!currentPath) return;

    // Handle both Windows and Unix paths
    let parent: string;
    const parts = currentPath.replace(/[\\/]+$/, '').split(/[\\/]/);

    if (parts.length <= 1) {
      // Already at root
      return;
    }

    // On Windows, if we're at "C:\", don't go further
    if (parts.length === 2 && /^[A-Z]:$/i.test(parts[0])) {
      parent = parts[0] + '\\';
    } else {
      parts.pop();
      parent = parts.join('\\');
    }

    listDirectory(parent);
  }, [currentPath, listDirectory]);

  const uploadFile = useCallback((file: File) => {
    const transferId = `upload-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    const destPath = currentPath ? `${currentPath.replace(/[\\/]+$/, '')}\\${file.name}` : file.name;

    // Add to transfers list immediately as queued
    syncTransfers((prev) => [
      ...prev,
      {
        id: transferId,
        name: file.name,
        size: file.size,
        transferred: 0,
        progress: 0,
        state: 'queued' as const,
        direction: 'upload' as const,
        speed: 0,
      },
    ]);

    // Send upload start request
    const sent = sendMessage({
      type: 'file.uploadStart',
      request: {
        transferId,
        direction: 'upload',
        sourcePath: file.name,
        destPath,
        fileSize: file.size,
        overwrite: true,
      },
    });

    if (!sent) {
      syncTransfers((prev) =>
        prev.map((t) => (t.id === transferId ? { ...t, state: 'failed' as const, error: 'Data channel not available' } : t))
      );
      return;
    }

    // Start sending chunks after a brief delay to let the agent process the start message
    setTimeout(() => {
      void sendFileChunks(transferId, file);
    }, 100);
  }, [currentPath, sendMessage, syncTransfers]);

  const sendFileChunks = useCallback(async (transferId: string, file: File) => {
    const totalChunks = Math.ceil(file.size / CHUNK_SIZE);
    let offset = 0;

    syncTransfers((prev) =>
      prev.map((t) => (t.id === transferId ? { ...t, state: 'in_progress' as const } : t))
    );

    for (let index = 0; index < totalChunks; index++) {
      // Check if transfer was canceled
      const currentTransfer = transfersRef.current.find((t) => t.id === transferId);
      if (currentTransfer && (currentTransfer.state === 'canceled' || currentTransfer.state === 'failed')) {
        return;
      }

      // Handle pause: spin-wait until resumed
      while (currentTransfer && currentTransfer.state === 'paused') {
        await new Promise((r) => setTimeout(r, 200));
        const refreshed = transfersRef.current.find((t) => t.id === transferId);
        if (!refreshed || refreshed.state === 'canceled' || refreshed.state === 'failed') return;
        if (refreshed.state !== 'paused') break;
      }

      const end = Math.min(offset + CHUNK_SIZE, file.size);
      const slice = file.slice(offset, end);
      const arrayBuffer = await slice.arrayBuffer();
      const bytes = new Uint8Array(arrayBuffer);

      // Convert to base64 for JSON transport (matching Go's []byte JSON encoding)
      const base64Data = btoa(String.fromCharCode(...bytes));

      const isLast = index === totalChunks - 1;

      // Create a promise that resolves when we get the chunk ACK
      const ackPromise = new Promise<void>((resolve, reject) => {
        uploadState.current.set(`${transferId}:${index}`, { resolve, reject });

        // Timeout after 30s
        setTimeout(() => {
          if (uploadState.current.has(`${transferId}:${index}`)) {
            uploadState.current.delete(`${transferId}:${index}`);
            reject(new Error('Chunk ACK timeout'));
          }
        }, 30000);
      });

      const sent = sendMessage({
        type: 'file.chunk',
        chunk: {
          transferId,
          index,
          offset,
          size: bytes.length,
          data: base64Data,
          isLast,
        },
      });

      if (!sent) {
        syncTransfers((prev) =>
          prev.map((t) => (t.id === transferId ? { ...t, state: 'failed' as const, error: 'Data channel closed' } : t))
        );
        return;
      }

      // Update local progress
      syncTransfers((prev) =>
        prev.map((t) =>
          t.id === transferId
            ? { ...t, transferred: end, progress: file.size > 0 ? (end / file.size) * 100 : 0 }
            : t
        )
      );

      // Wait for ACK before sending next chunk
      try {
        await ackPromise;
      } catch (err) {
        console.error('[FileTransfer] Chunk ACK error:', err);
        syncTransfers((prev) =>
          prev.map((t) =>
            t.id === transferId
              ? { ...t, state: 'failed' as const, error: err instanceof Error ? err.message : 'Unknown error' }
              : t
          )
        );
        return;
      }

      offset = end;
    }

    // Upload completed (server-side completion callback will also fire)
    syncTransfers((prev) =>
      prev.map((t) =>
        t.id === transferId ? { ...t, state: 'completed' as const, progress: 100, transferred: file.size } : t
      )
    );

    // Refresh directory listing to show the new file
    if (currentPath) {
      listDirectory(currentPath);
    }
  }, [sendMessage, syncTransfers, currentPath, listDirectory]);

  const downloadFile = useCallback((path: string) => {
    const transferId = `download-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    const fileName = path.split(/[\\/]/).pop() || 'download';

    // Initialize download accumulation state
    downloadState.current.set(transferId, {
      chunks: [],
      totalSize: 0,
      fileName,
    });

    // Add to transfers list
    syncTransfers((prev) => [
      ...prev,
      {
        id: transferId,
        name: fileName,
        size: 0, // Will be updated when transfer response arrives
        transferred: 0,
        progress: 0,
        state: 'queued' as const,
        direction: 'download' as const,
        speed: 0,
      },
    ]);

    sendMessage({
      type: 'file.downloadStart',
      request: {
        transferId,
        direction: 'download',
        sourcePath: path,
        destPath: fileName,
        fileSize: 0,
        overwrite: false,
      },
    });
  }, [sendMessage, syncTransfers]);

  const pauseTransfer = useCallback((id: string) => {
    sendMessage({ type: 'file.pause', transferId: id });
    syncTransfers((prev) =>
      prev.map((t) => (t.id === id ? { ...t, state: 'paused' as const } : t))
    );
  }, [sendMessage, syncTransfers]);

  const resumeTransfer = useCallback((id: string) => {
    sendMessage({ type: 'file.resume', transferId: id });
    syncTransfers((prev) =>
      prev.map((t) => (t.id === id ? { ...t, state: 'in_progress' as const } : t))
    );
  }, [sendMessage, syncTransfers]);

  const cancelTransfer = useCallback((id: string) => {
    sendMessage({ type: 'file.cancel', transferId: id });
    syncTransfers((prev) =>
      prev.map((t) => (t.id === id ? { ...t, state: 'canceled' as const } : t))
    );

    // Clean up download state if present
    downloadState.current.delete(id);

    // Clean up any pending upload ACKs
    for (const key of uploadState.current.keys()) {
      if (key.startsWith(id + ':')) {
        const pending = uploadState.current.get(key);
        if (pending) {
          pending.reject(new Error('Transfer canceled'));
        }
        uploadState.current.delete(key);
      }
    }
  }, [sendMessage, syncTransfers]);

  // ─── Cleanup on Unmount ──────────────────────────────────────────────────

  useEffect(() => {
    return () => {
      downloadState.current.clear();
      uploadState.current.clear();
    };
  }, []);

  return {
    files,
    currentPath,
    transfers,
    listDirectory,
    uploadFile,
    downloadFile,
    pauseTransfer,
    resumeTransfer,
    cancelTransfer,
    navigateUp,
    isOpen,
    setIsOpen,
  };
}
