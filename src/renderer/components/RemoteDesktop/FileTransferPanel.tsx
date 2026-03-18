import React, { useCallback, useRef, useState } from 'react';
import {
  X,
  Upload,
  Download,
  FolderOpen,
  FileIcon,
  ChevronRight,
  Pause,
  Play,
  XCircle,
  ArrowUp,
  HardDrive,
} from 'lucide-react';
import type { RemoteFile, FileTransfer } from './useFileTransfer';

// ─── Props ───────────────────────────────────────────────────────────────────

interface FileTransferPanelProps {
  isOpen: boolean;
  onClose: () => void;
  files: RemoteFile[];
  currentPath: string;
  transfers: FileTransfer[];
  onNavigate: (path: string) => void;
  onNavigateUp: () => void;
  onUpload: (file: File) => void;
  onDownload: (path: string) => void;
  onPause: (id: string) => void;
  onResume: (id: string) => void;
  onCancel: (id: string) => void;
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function formatFileSize(bytes: number): string {
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const val = bytes / Math.pow(1024, i);
  return `${val.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

function formatSpeed(bytesPerSecond: number): string {
  if (bytesPerSecond === 0) return '0 B/s';
  if (bytesPerSecond < 1024) return `${Math.round(bytesPerSecond)} B/s`;
  if (bytesPerSecond < 1024 * 1024) return `${(bytesPerSecond / 1024).toFixed(1)} KB/s`;
  return `${(bytesPerSecond / (1024 * 1024)).toFixed(1)} MB/s`;
}

function formatDate(dateStr: string): string {
  if (!dateStr) return '';
  try {
    const d = new Date(dateStr);
    return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' }) +
      ' ' +
      d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
  } catch {
    return dateStr;
  }
}

function parseBreadcrumbs(path: string): { label: string; path: string }[] {
  if (!path) return [];

  const parts = path.replace(/[\\/]+$/, '').split(/[\\/]/);
  const crumbs: { label: string; path: string }[] = [];

  for (let i = 0; i < parts.length; i++) {
    const label = parts[i];
    // Reconstruct the path up to this segment
    const crumbPath = parts.slice(0, i + 1).join('\\');
    crumbs.push({ label: label || '\\', path: crumbPath.endsWith('\\') ? crumbPath : crumbPath + '\\' });
  }

  return crumbs;
}

function getTransferStateColor(state: FileTransfer['state']): string {
  switch (state) {
    case 'in_progress': return 'bg-blue-500';
    case 'paused': return 'bg-yellow-500';
    case 'completed': return 'bg-green-500';
    case 'failed': return 'bg-red-500';
    case 'canceled': return 'bg-gray-500';
    case 'queued': return 'bg-gray-400';
    default: return 'bg-gray-400';
  }
}

function getTransferStateLabel(state: FileTransfer['state']): string {
  switch (state) {
    case 'in_progress': return 'Transferring';
    case 'paused': return 'Paused';
    case 'completed': return 'Completed';
    case 'failed': return 'Failed';
    case 'canceled': return 'Canceled';
    case 'queued': return 'Queued';
    default: return state;
  }
}

// ─── Component ───────────────────────────────────────────────────────────────

export function FileTransferPanel({
  isOpen,
  onClose,
  files,
  currentPath,
  transfers,
  onNavigate,
  onNavigateUp,
  onUpload,
  onDownload,
  onPause,
  onResume,
  onCancel,
}: FileTransferPanelProps) {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [isDragOver, setIsDragOver] = useState(false);

  // ─── Drag & Drop ────────────────────────────────────────────────────────

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragOver(true);
  }, []);

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragOver(false);
  }, []);

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragOver(false);

    const droppedFiles = e.dataTransfer.files;
    for (let i = 0; i < droppedFiles.length; i++) {
      const file = droppedFiles[i];
      if (file) {
        onUpload(file);
      }
    }
  }, [onUpload]);

  // ─── File Input ─────────────────────────────────────────────────────────

  const handleFileSelect = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const selectedFiles = e.target.files;
    if (selectedFiles) {
      for (let i = 0; i < selectedFiles.length; i++) {
        const file = selectedFiles[i];
        if (file) {
          onUpload(file);
        }
      }
    }
    // Reset input so the same file can be selected again
    if (fileInputRef.current) {
      fileInputRef.current.value = '';
    }
  }, [onUpload]);

  // ─── File Row Click ─────────────────────────────────────────────────────

  const handleFileClick = useCallback((file: RemoteFile) => {
    if (file.is_dir) {
      onNavigate(file.path);
    }
  }, [onNavigate]);

  const handleDownloadClick = useCallback((e: React.MouseEvent, file: RemoteFile) => {
    e.stopPropagation();
    if (!file.is_dir) {
      onDownload(file.path);
    }
  }, [onDownload]);

  // ─── Breadcrumbs ────────────────────────────────────────────────────────

  const breadcrumbs = parseBreadcrumbs(currentPath);

  // ─── Active / Completed Transfer Counts ─────────────────────────────────

  const activeCount = transfers.filter((t) => t.state === 'in_progress' || t.state === 'queued' || t.state === 'paused').length;
  const completedCount = transfers.filter((t) => t.state === 'completed').length;

  // ─── Render ─────────────────────────────────────────────────────────────

  return (
    <>
      {/* Backdrop */}
      {isOpen && (
        <div
          className="fixed inset-0 bg-black/30 z-40"
          onClick={onClose}
        />
      )}

      {/* Slide-in Panel */}
      <div
        className={`fixed top-0 right-0 h-full w-[480px] max-w-[90vw] bg-gray-900 border-l border-gray-700 shadow-2xl z-50 flex flex-col transition-transform duration-300 ease-in-out ${
          isOpen ? 'translate-x-0' : 'translate-x-full'
        }`}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-700 bg-gray-800 shrink-0">
          <div className="flex items-center gap-2">
            <HardDrive className="w-5 h-5 text-blue-400" />
            <h2 className="text-base font-semibold text-gray-100">File Transfer</h2>
            {activeCount > 0 && (
              <span className="px-2 py-0.5 text-xs font-medium bg-blue-500/20 text-blue-300 rounded-full">
                {activeCount} active
              </span>
            )}
          </div>
          <button
            onClick={onClose}
            className="p-1 rounded hover:bg-gray-700 text-gray-400 hover:text-gray-200 transition-colors"
            title="Close"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Breadcrumbs / Path */}
        <div className="flex items-center gap-1 px-4 py-2 border-b border-gray-700/50 bg-gray-850 overflow-x-auto shrink-0" style={{ backgroundColor: 'rgb(26, 31, 41)' }}>
          <button
            onClick={onNavigateUp}
            className="p-1 rounded hover:bg-gray-700 text-gray-400 hover:text-gray-200 transition-colors shrink-0"
            title="Go up"
          >
            <ArrowUp className="w-4 h-4" />
          </button>

          <div className="flex items-center gap-0.5 text-sm text-gray-400 overflow-x-auto whitespace-nowrap">
            {breadcrumbs.length === 0 ? (
              <span className="text-gray-500 italic">No directory loaded</span>
            ) : (
              breadcrumbs.map((crumb, idx) => (
                <React.Fragment key={idx}>
                  {idx > 0 && <ChevronRight className="w-3 h-3 text-gray-600 shrink-0" />}
                  <button
                    onClick={() => onNavigate(crumb.path)}
                    className="px-1 py-0.5 rounded hover:bg-gray-700 hover:text-gray-200 transition-colors truncate max-w-[120px]"
                    title={crumb.path}
                  >
                    {crumb.label}
                  </button>
                </React.Fragment>
              ))
            )}
          </div>
        </div>

        {/* Upload Zone + Button */}
        <div className="px-4 py-2 border-b border-gray-700/50 shrink-0">
          <div
            onDragOver={handleDragOver}
            onDragLeave={handleDragLeave}
            onDrop={handleDrop}
            className={`flex items-center justify-center gap-2 px-4 py-3 border-2 border-dashed rounded-lg transition-colors cursor-pointer ${
              isDragOver
                ? 'border-blue-400 bg-blue-400/10 text-blue-300'
                : 'border-gray-600 bg-gray-800/50 text-gray-400 hover:border-gray-500 hover:text-gray-300'
            }`}
            onClick={() => fileInputRef.current?.click()}
          >
            <Upload className="w-5 h-5" />
            <span className="text-sm">
              {isDragOver ? 'Drop files here' : 'Drag files here or click to upload'}
            </span>
          </div>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            className="hidden"
            onChange={handleFileSelect}
          />
        </div>

        {/* File Browser */}
        <div className="flex-1 overflow-y-auto min-h-0">
          {files.length === 0 ? (
            <div className="flex items-center justify-center h-32 text-gray-500 text-sm">
              {currentPath ? 'Empty directory' : 'Navigate to a directory to browse files'}
            </div>
          ) : (
            <div className="divide-y divide-gray-800">
              {files.map((file) => (
                <div
                  key={file.path || file.name}
                  onClick={() => handleFileClick(file)}
                  className={`flex items-center gap-3 px-4 py-2 hover:bg-gray-800 transition-colors ${
                    file.is_dir ? 'cursor-pointer' : 'cursor-default'
                  }`}
                >
                  {/* Icon */}
                  <div className="shrink-0">
                    {file.is_dir ? (
                      <FolderOpen className="w-5 h-5 text-yellow-400" />
                    ) : (
                      <FileIcon className="w-5 h-5 text-gray-400" />
                    )}
                  </div>

                  {/* Name + Meta */}
                  <div className="flex-1 min-w-0">
                    <div className="text-sm text-gray-200 truncate">{file.name}</div>
                    <div className="text-xs text-gray-500">
                      {file.is_dir ? 'Folder' : formatFileSize(file.size)}
                      {file.modified_time && ` \u00B7 ${formatDate(file.modified_time)}`}
                    </div>
                  </div>

                  {/* Download button for files */}
                  {!file.is_dir && (
                    <button
                      onClick={(e) => handleDownloadClick(e, file)}
                      className="p-1.5 rounded hover:bg-gray-700 text-gray-400 hover:text-blue-400 transition-colors shrink-0"
                      title="Download"
                    >
                      <Download className="w-4 h-4" />
                    </button>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Transfers Section */}
        {transfers.length > 0 && (
          <div className="border-t border-gray-700 shrink-0 max-h-[40%] overflow-y-auto">
            <div className="flex items-center justify-between px-4 py-2 bg-gray-800 sticky top-0 z-10">
              <h3 className="text-sm font-medium text-gray-300">
                Transfers
                {completedCount > 0 && (
                  <span className="ml-1 text-gray-500">({completedCount} completed)</span>
                )}
              </h3>
            </div>

            <div className="divide-y divide-gray-800">
              {transfers.map((transfer) => (
                <div key={transfer.id} className="px-4 py-2.5">
                  {/* Transfer name + direction + state */}
                  <div className="flex items-center gap-2 mb-1.5">
                    {transfer.direction === 'upload' ? (
                      <Upload className="w-3.5 h-3.5 text-green-400 shrink-0" />
                    ) : (
                      <Download className="w-3.5 h-3.5 text-blue-400 shrink-0" />
                    )}
                    <span className="text-sm text-gray-200 truncate flex-1" title={transfer.name}>
                      {transfer.name}
                    </span>
                    <span className={`text-xs px-1.5 py-0.5 rounded ${
                      transfer.state === 'completed' ? 'text-green-300 bg-green-500/10' :
                      transfer.state === 'failed' ? 'text-red-300 bg-red-500/10' :
                      transfer.state === 'canceled' ? 'text-gray-400 bg-gray-500/10' :
                      transfer.state === 'paused' ? 'text-yellow-300 bg-yellow-500/10' :
                      'text-blue-300 bg-blue-500/10'
                    }`}>
                      {getTransferStateLabel(transfer.state)}
                    </span>
                  </div>

                  {/* Progress bar */}
                  <div className="w-full h-1.5 bg-gray-700 rounded-full overflow-hidden mb-1.5">
                    <div
                      className={`h-full rounded-full transition-all duration-300 ${getTransferStateColor(transfer.state)}`}
                      style={{ width: `${Math.min(100, Math.max(0, transfer.progress))}%` }}
                    />
                  </div>

                  {/* Stats + Controls */}
                  <div className="flex items-center justify-between">
                    <div className="text-xs text-gray-500">
                      {formatFileSize(transfer.transferred)} / {formatFileSize(transfer.size)}
                      {transfer.state === 'in_progress' && transfer.speed > 0 && (
                        <span className="ml-2 text-gray-400">{formatSpeed(transfer.speed)}</span>
                      )}
                    </div>

                    {/* Control buttons */}
                    <div className="flex items-center gap-1">
                      {transfer.state === 'in_progress' && (
                        <button
                          onClick={() => onPause(transfer.id)}
                          className="p-1 rounded hover:bg-gray-700 text-gray-400 hover:text-yellow-400 transition-colors"
                          title="Pause"
                        >
                          <Pause className="w-3.5 h-3.5" />
                        </button>
                      )}

                      {transfer.state === 'paused' && (
                        <button
                          onClick={() => onResume(transfer.id)}
                          className="p-1 rounded hover:bg-gray-700 text-gray-400 hover:text-green-400 transition-colors"
                          title="Resume"
                        >
                          <Play className="w-3.5 h-3.5" />
                        </button>
                      )}

                      {(transfer.state === 'in_progress' || transfer.state === 'paused' || transfer.state === 'queued') && (
                        <button
                          onClick={() => onCancel(transfer.id)}
                          className="p-1 rounded hover:bg-gray-700 text-gray-400 hover:text-red-400 transition-colors"
                          title="Cancel"
                        >
                          <XCircle className="w-3.5 h-3.5" />
                        </button>
                      )}
                    </div>
                  </div>

                  {/* Error message */}
                  {transfer.state === 'failed' && transfer.error && (
                    <div className="mt-1 text-xs text-red-400 truncate" title={transfer.error}>
                      {transfer.error}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </>
  );
}
