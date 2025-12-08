import React, { useState, useEffect } from 'react';

interface FileEntry {
  name: string;
  path: string;
  isDirectory: boolean;
  size: number;
  modified: string;
  permissions: string;
}

interface FileExplorerProps {
  deviceId: string;
  isOnline: boolean;
}

export function FileExplorer({ deviceId, isOnline }: FileExplorerProps) {
  const [currentPath, setCurrentPath] = useState('/');
  const [files, setFiles] = useState<FileEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedFile, setSelectedFile] = useState<FileEntry | null>(null);
  const [transferProgress, setTransferProgress] = useState<{ filename: string; percentage: number } | null>(null);

  useEffect(() => {
    if (isOnline) {
      loadDirectory(currentPath);
    }

    const unsub = window.api.files.onProgress((progress) => {
      if (progress.deviceId === deviceId) {
        setTransferProgress({
          filename: progress.filename,
          percentage: progress.percentage,
        });
        if (progress.percentage >= 100) {
          setTimeout(() => setTransferProgress(null), 1000);
        }
      }
    });

    return () => unsub();
  }, [deviceId, isOnline]);

  const loadDirectory = async (path: string) => {
    setLoading(true);
    setError(null);
    try {
      const entries = await window.api.files.list(deviceId, path);
      setFiles(entries);
      setCurrentPath(path);
    } catch (err: any) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  };

  const navigateUp = () => {
    const parts = currentPath.split('/').filter(Boolean);
    parts.pop();
    const newPath = '/' + parts.join('/');
    loadDirectory(newPath || '/');
  };

  const navigateTo = (entry: FileEntry) => {
    if (entry.isDirectory) {
      loadDirectory(entry.path);
    } else {
      setSelectedFile(entry);
    }
  };

  const handleDownload = async (file: FileEntry) => {
    try {
      // In a real implementation, this would open a save dialog
      const localPath = `${file.name}`;
      await window.api.files.download(deviceId, file.path, localPath);
      alert('File downloaded successfully');
    } catch (err: any) {
      alert(`Download failed: ${err.message}`);
    }
  };

  const formatSize = (bytes: number): string => {
    if (bytes === 0) return '-';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  };

  const formatDate = (dateStr: string): string => {
    try {
      return new Date(dateStr).toLocaleString();
    } catch {
      return dateStr;
    }
  };

  if (!isOnline) {
    return (
      <div className="h-96 flex items-center justify-center">
        <p className="text-text-secondary">Device is offline. File explorer is not available.</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-96">
      {/* Toolbar */}
      <div className="flex items-center gap-2 px-4 py-2 bg-gray-50 border-b border-border">
        <button
          onClick={navigateUp}
          disabled={currentPath === '/'}
          className="p-1 text-text-secondary hover:text-text-primary disabled:opacity-50 transition-colors"
          title="Go up"
        >
          <UpIcon className="w-5 h-5" />
        </button>
        <button
          onClick={() => loadDirectory(currentPath)}
          className="p-1 text-text-secondary hover:text-text-primary transition-colors"
          title="Refresh"
        >
          <RefreshIcon className="w-5 h-5" />
        </button>
        <div className="flex-1 px-3 py-1 bg-white border border-border rounded text-sm font-mono">
          {currentPath}
        </div>
      </div>

      {/* Transfer Progress */}
      {transferProgress && (
        <div className="px-4 py-2 bg-blue-50 border-b border-blue-200">
          <div className="flex items-center gap-2">
            <span className="text-sm text-blue-700">
              Transferring: {transferProgress.filename}
            </span>
            <div className="flex-1 h-2 bg-blue-200 rounded-full overflow-hidden">
              <div
                className="h-full bg-blue-600 transition-all"
                style={{ width: `${transferProgress.percentage}%` }}
              />
            </div>
            <span className="text-sm text-blue-700">{transferProgress.percentage}%</span>
          </div>
        </div>
      )}

      {/* File List */}
      <div className="flex-1 overflow-auto">
        {loading ? (
          <div className="flex items-center justify-center h-full">
            <p className="text-text-secondary">Loading...</p>
          </div>
        ) : error ? (
          <div className="flex items-center justify-center h-full">
            <p className="text-danger">{error}</p>
          </div>
        ) : (
          <table className="w-full">
            <thead className="sticky top-0 bg-white">
              <tr>
                <th className="text-left">Name</th>
                <th className="text-right w-24">Size</th>
                <th className="text-left w-48">Modified</th>
                <th className="w-20"></th>
              </tr>
            </thead>
            <tbody>
              {files.length === 0 ? (
                <tr>
                  <td colSpan={4} className="text-center py-8 text-text-secondary">
                    Empty directory
                  </td>
                </tr>
              ) : (
                files.map(file => (
                  <tr
                    key={file.path}
                    className={`cursor-pointer ${
                      selectedFile?.path === file.path ? 'bg-primary-light' : ''
                    }`}
                    onClick={() => navigateTo(file)}
                  >
                    <td>
                      <div className="flex items-center gap-2">
                        {file.isDirectory ? (
                          <FolderIcon className="w-5 h-5 text-yellow-500" />
                        ) : (
                          <FileIcon className="w-5 h-5 text-gray-400" />
                        )}
                        <span className={file.isDirectory ? 'font-medium' : ''}>
                          {file.name}
                        </span>
                      </div>
                    </td>
                    <td className="text-right text-sm text-text-secondary">
                      {formatSize(file.size)}
                    </td>
                    <td className="text-sm text-text-secondary">
                      {formatDate(file.modified)}
                    </td>
                    <td onClick={e => e.stopPropagation()}>
                      {!file.isDirectory && (
                        <button
                          onClick={() => handleDownload(file)}
                          className="p-1 text-text-secondary hover:text-primary transition-colors"
                          title="Download"
                        >
                          <DownloadIcon className="w-4 h-4" />
                        </button>
                      )}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        )}
      </div>

      {/* Status Bar */}
      <div className="px-4 py-2 bg-gray-50 border-t border-border text-sm text-text-secondary">
        {files.length} items
        {selectedFile && !selectedFile.isDirectory && (
          <span className="ml-4">
            Selected: {selectedFile.name} ({formatSize(selectedFile.size)})
          </span>
        )}
      </div>
    </div>
  );
}

// Icons
function UpIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 10l7-7m0 0l7 7m-7-7v18" />
    </svg>
  );
}

function RefreshIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
    </svg>
  );
}

function FolderIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="currentColor" viewBox="0 0 24 24">
      <path d="M10 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z" />
    </svg>
  );
}

function FileIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z" />
    </svg>
  );
}

function DownloadIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
    </svg>
  );
}
