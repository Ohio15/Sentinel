#!/usr/bin/env python
content = r'''import React, { useState, useEffect } from 'react';

interface FileEntry {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  modified_time: string;
  mode: string;
  is_hidden?: boolean;
}

interface DriveInfo {
  name: string;
  path: string;
  label: string;
  drive_type: string;
  file_system: string;
  total_size: number;
  free_space: number;
  used_space: number;
}

interface FileExplorerProps {
  deviceId: string;
  isOnline: boolean;
}

type ViewMode = 'drives' | 'files';

export function FileExplorer({ deviceId, isOnline }: FileExplorerProps) {
  const [viewMode, setViewMode] = useState<ViewMode>('drives');
  const [currentPath, setCurrentPath] = useState('');
  const [files, setFiles] = useState<FileEntry[]>([]);
  const [drives, setDrives] = useState<DriveInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedFile, setSelectedFile] = useState<FileEntry | null>(null);
  const [transferProgress, setTransferProgress] = useState<{ filename: string; percentage: number } | null>(null);

  useEffect(() => {
    if (isOnline) {
      loadDrives();
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

  const loadDrives = async () => {
    setLoading(true);
    setError(null);
    try {
      const driveList = await window.api.files.drives(deviceId);
      setDrives(driveList);
      setViewMode('drives');
      setCurrentPath('');
    } catch (err: any) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  };

  const loadDirectory = async (path: string) => {
    setLoading(true);
    setError(null);
    try {
      const entries = await window.api.files.list(deviceId, path);
      setFiles(entries);
      setCurrentPath(path);
      setViewMode('files');
    } catch (err: any) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  };

  const navigateUp = () => {
    const isWindows = currentPath.includes('\\') || /^[A-Za-z]:/.test(currentPath);
    const parts = currentPath.split(/[\\\/]/).filter(Boolean);

    if (parts.length <= 1) {
      // At root or drive root - go back to drives view
      loadDrives();
      return;
    }

    parts.pop();
    let newPath: string;
    if (isWindows) {
      if (parts.length === 1) {
        // Windows drive root (e.g., C:\)
        newPath = parts[0] + '\\';
      } else {
        newPath = parts.join('\\');
      }
    } else {
      newPath = '/' + parts.join('/');
    }
    loadDirectory(newPath);
  };

  const navigateTo = (entry: FileEntry) => {
    if (entry.is_dir) {
      loadDirectory(entry.path);
    } else {
      setSelectedFile(entry);
    }
  };

  const navigateToDrive = (drive: DriveInfo) => {
    loadDirectory(drive.path);
  };

  const handleDownload = async (file: FileEntry) => {
    try {
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

  // Parse path into clickable segments
  const getPathSegments = (): { name: string; path: string }[] => {
    if (!currentPath) return [];

    const isWindows = currentPath.includes('\\') || /^[A-Za-z]:/.test(currentPath);
    const separator = isWindows ? '\\' : '/';
    const parts = currentPath.split(/[\\\/]/).filter(Boolean);

    const segments: { name: string; path: string }[] = [];
    let accumulatedPath = '';

    for (let i = 0; i < parts.length; i++) {
      if (isWindows) {
        if (i === 0) {
          // Drive letter (e.g., "C:")
          accumulatedPath = parts[i] + '\\';
          segments.push({ name: parts[i], path: accumulatedPath });
        } else {
          accumulatedPath += (i === 1 ? '' : separator) + parts[i];
          segments.push({ name: parts[i], path: accumulatedPath });
        }
      } else {
        accumulatedPath += separator + parts[i];
        segments.push({ name: parts[i], path: accumulatedPath });
      }
    }

    return segments;
  };

  const getDriveIcon = (driveType: string) => {
    switch (driveType) {
      case 'Fixed':
        return <HardDriveIcon className="w-12 h-12 text-gray-600 dark:text-gray-400" />;
      case 'Removable':
        return <UsbIcon className="w-12 h-12 text-blue-500" />;
      case 'Network':
        return <NetworkDriveIcon className="w-12 h-12 text-green-500" />;
      case 'CD-ROM':
        return <DiscIcon className="w-12 h-12 text-purple-500" />;
      default:
        return <HardDriveIcon className="w-12 h-12 text-gray-400" />;
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
      <div className="flex items-center gap-2 px-4 py-2 bg-gray-50 dark:bg-slate-800 border-b border-border">
        <button
          onClick={navigateUp}
          disabled={viewMode === 'drives'}
          className="p-1 text-text-secondary hover:text-text-primary disabled:opacity-50 transition-colors"
          title="Go up"
        >
          <UpIcon className="w-5 h-5" />
        </button>
        <button
          onClick={() => viewMode === 'drives' ? loadDrives() : loadDirectory(currentPath)}
          className="p-1 text-text-secondary hover:text-text-primary transition-colors"
          title="Refresh"
        >
          <RefreshIcon className="w-5 h-5" />
        </button>

        {/* Breadcrumb Path Bar */}
        <div className="flex-1 flex items-center gap-1 px-3 py-1 bg-white dark:bg-slate-700 border border-border rounded text-sm overflow-x-auto">
          <button
            onClick={loadDrives}
            className={`flex items-center gap-1 px-2 py-0.5 rounded hover:bg-gray-100 dark:hover:bg-slate-600 transition-colors whitespace-nowrap ${
              viewMode === 'drives' ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400' : 'text-text-primary'
            }`}
          >
            <ComputerIcon className="w-4 h-4" />
            <span>This PC</span>
          </button>

          {viewMode === 'files' && getPathSegments().map((segment, index) => (
            <React.Fragment key={segment.path}>
              <ChevronRightIcon className="w-4 h-4 text-gray-400 dark:text-gray-500 flex-shrink-0" />
              <button
                onClick={() => loadDirectory(segment.path)}
                className="px-2 py-0.5 rounded hover:bg-gray-100 dark:hover:bg-slate-600 transition-colors whitespace-nowrap font-mono text-text-primary"
              >
                {segment.name}
              </button>
            </React.Fragment>
          ))}
        </div>
      </div>

      {/* Transfer Progress */}
      {transferProgress && (
        <div className="px-4 py-2 bg-blue-50 dark:bg-blue-900/20 border-b border-blue-200 dark:border-blue-800">
          <div className="flex items-center gap-2">
            <span className="text-sm text-blue-700 dark:text-blue-400">
              Transferring: {transferProgress.filename}
            </span>
            <div className="flex-1 h-2 bg-blue-200 dark:bg-blue-800 rounded-full overflow-hidden">
              <div
                className="h-full bg-blue-600 transition-all"
                style={{ width: `${transferProgress.percentage}%` }}
              />
            </div>
            <span className="text-sm text-blue-700 dark:text-blue-400">{transferProgress.percentage}%</span>
          </div>
        </div>
      )}

      {/* Content Area */}
      <div className="flex-1 overflow-auto">
        {loading ? (
          <div className="flex items-center justify-center h-full">
            <p className="text-text-secondary">Loading...</p>
          </div>
        ) : error ? (
          <div className="flex items-center justify-center h-full">
            <p className="text-danger">{error}</p>
          </div>
        ) : viewMode === 'drives' ? (
          /* Drives View */
          <div className="p-4">
            <h3 className="text-sm font-medium text-text-secondary mb-4">Devices and drives</h3>
            <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
              {drives.map(drive => (
                <div
                  key={drive.path}
                  onClick={() => navigateToDrive(drive)}
                  className="p-4 border border-border rounded-lg hover:bg-gray-50 dark:hover:bg-slate-700 hover:border-primary cursor-pointer transition-all"
                >
                  <div className="flex items-start gap-3">
                    {getDriveIcon(drive.drive_type)}
                    <div className="flex-1 min-w-0">
                      <div className="font-medium text-text-primary truncate">
                        {drive.label || 'Local Disk'} ({drive.name})
                      </div>
                      <div className="text-xs text-text-secondary mt-1">
                        {drive.file_system} - {drive.drive_type}
                      </div>
                      {drive.total_size > 0 && (
                        <>
                          <div className="mt-2 h-1.5 bg-gray-200 dark:bg-slate-600 rounded-full overflow-hidden">
                            <div
                              className={`h-full transition-all ${
                                (drive.used_space / drive.total_size) > 0.9
                                  ? 'bg-red-500'
                                  : (drive.used_space / drive.total_size) > 0.7
                                    ? 'bg-yellow-500'
                                    : 'bg-blue-500'
                              }`}
                              style={{ width: `${(drive.used_space / drive.total_size) * 100}%` }}
                            />
                          </div>
                          <div className="text-xs text-text-secondary mt-1">
                            {formatSize(drive.free_space)} free of {formatSize(drive.total_size)}
                          </div>
                        </>
                      )}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        ) : (
          /* Files View */
          <table className="w-full">
            <thead className="sticky top-0 bg-white dark:bg-slate-800">
              <tr>
                <th className="text-left text-text-primary">Name</th>
                <th className="text-right w-24 text-text-primary">Size</th>
                <th className="text-left w-48 text-text-primary">Modified</th>
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
                    className={`cursor-pointer hover:bg-gray-50 dark:hover:bg-slate-700 ${
                      selectedFile?.path === file.path ? 'bg-primary-light dark:bg-primary/20' : ''
                    }`}
                    onClick={() => navigateTo(file)}
                  >
                    <td>
                      <div className="flex items-center gap-2">
                        {file.is_dir ? (
                          <FolderIcon className="w-5 h-5 text-yellow-500" />
                        ) : (
                          <FileIcon className="w-5 h-5 text-gray-400" />
                        )}
                        <span className={`text-text-primary ${file.is_dir ? 'font-medium' : ''}`}>
                          {file.name}
                        </span>
                      </div>
                    </td>
                    <td className="text-right text-sm text-text-secondary">
                      {formatSize(file.size)}
                    </td>
                    <td className="text-sm text-text-secondary">
                      {formatDate(file.modified_time)}
                    </td>
                    <td onClick={e => e.stopPropagation()}>
                      {!file.is_dir && (
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
      <div className="px-4 py-2 bg-gray-50 dark:bg-slate-800 border-t border-border text-sm text-text-secondary">
        {viewMode === 'drives' ? (
          <span>{drives.length} drive(s)</span>
        ) : (
          <>
            {files.length} items
            {selectedFile && !selectedFile.is_dir && (
              <span className="ml-4">
                Selected: {selectedFile.name} ({formatSize(selectedFile.size)})
              </span>
            )}
          </>
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

function ComputerIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
    </svg>
  );
}

function ChevronRightIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
    </svg>
  );
}

function HardDriveIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4m0 5c0 2.21-3.582 4-8 4s-8-1.79-8-4" />
    </svg>
  );
}

function UsbIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M12 3v12m0 0l-3-3m3 3l3-3m-6 6h6a2 2 0 002-2v-1a2 2 0 00-2-2H9a2 2 0 00-2 2v1a2 2 0 002 2z" />
    </svg>
  );
}

function NetworkDriveIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9" />
    </svg>
  );
}

function DiscIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M12 8c-2.21 0-4 1.79-4 4s1.79 4 4 4 4-1.79 4-4-1.79-4-4-4zm0 0V3m0 18v-3m0 0a9 9 0 100-18 9 9 0 000 18z" />
    </svg>
  );
}
'''

with open('D:/Projects/Sentinel/src/renderer/components/FileExplorer.tsx', 'w', encoding='utf-8') as f:
    f.write(content)

print("FileExplorer.tsx updated with dark mode!")
