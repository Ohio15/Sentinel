import { useState, useEffect, useCallback } from 'react';
import {
  Folder,
  File,
  ChevronRight,
  ChevronLeft,
  Home,
  RefreshCw,
  Download,
  ArrowUp,
  X,
  FileText,
  Image,
  Music,
  Video,
  Archive,
  Code,
} from 'lucide-react';
import { wsService } from '@/services/websocket';
import { Button } from '@/components/ui';
import toast from 'react-hot-toast';

interface FileInfo {
  name: string;
  path: string;
  size: number;
  is_dir: boolean;
  mode: string;
  modified_time: string;
  is_hidden: boolean;
}

interface FileBrowserProps {
  deviceId: string;
  agentId: string;
  onClose: () => void;
}

export function FileBrowser({ deviceId, agentId, onClose }: FileBrowserProps) {
  const [currentPath, setCurrentPath] = useState('~');
  const [files, setFiles] = useState<FileInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pathHistory, setPathHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const [showHidden, setShowHidden] = useState(false);
  const [selectedFile, setSelectedFile] = useState<FileInfo | null>(null);

  const requestId = useCallback(() => `files-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`, []);

  const listDirectory = useCallback((path: string) => {
    setLoading(true);
    setError(null);
    setSelectedFile(null);

    const reqId = requestId();

    const handleResponse = (data: unknown) => {
      const response = data as {
        requestId?: string;
        success?: boolean;
        data?: { files?: FileInfo[] };
        error?: string;
      };

      if (response.requestId !== reqId) return;

      setLoading(false);
      if (response.success && response.data?.files) {
        setFiles(response.data.files);
        setCurrentPath(path);
      } else {
        setError(response.error || 'Failed to list directory');
      }
    };

    const unsubscribe = wsService.on('response', handleResponse);

    wsService.send('list_files', {
      deviceId,
      agentId,
      path,
      requestId: reqId,
    });

    // Cleanup after timeout
    setTimeout(() => {
      unsubscribe();
      if (loading) {
        setLoading(false);
        setError('Request timed out');
      }
    }, 30000);

    return () => unsubscribe();
  }, [deviceId, agentId, requestId, loading]);

  useEffect(() => {
    if (wsService.isConnected) {
      listDirectory(currentPath);
    } else {
      wsService.connect();
      const unsubscribe = wsService.on('connected', () => {
        listDirectory(currentPath);
        unsubscribe();
      });
    }
  }, []);

  const navigateTo = (path: string) => {
    // Add current path to history
    const newHistory = [...pathHistory.slice(0, historyIndex + 1), currentPath];
    setPathHistory(newHistory);
    setHistoryIndex(newHistory.length);
    listDirectory(path);
  };

  const goBack = () => {
    if (historyIndex > 0) {
      const newIndex = historyIndex - 1;
      setHistoryIndex(newIndex);
      listDirectory(pathHistory[newIndex]);
    }
  };

  const goForward = () => {
    if (historyIndex < pathHistory.length - 1) {
      const newIndex = historyIndex + 1;
      setHistoryIndex(newIndex);
      listDirectory(pathHistory[newIndex]);
    }
  };

  const goUp = () => {
    const parentPath = currentPath.split(/[/\\]/).slice(0, -1).join('/') || '/';
    navigateTo(parentPath);
  };

  const goHome = () => {
    navigateTo('~');
  };

  const handleFileClick = (file: FileInfo) => {
    if (file.is_dir) {
      navigateTo(file.path);
    } else {
      setSelectedFile(file);
    }
  };

  const downloadFile = (file: FileInfo) => {
    toast.success(`Downloading ${file.name}...`);

    wsService.send('download_file', {
      deviceId,
      agentId,
      path: file.path,
    });
  };

  const formatFileSize = (bytes: number): string => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`;
  };

  const formatDate = (dateStr: string): string => {
    return new Date(dateStr).toLocaleString();
  };

  const getFileIcon = (file: FileInfo) => {
    if (file.is_dir) return <Folder className="w-5 h-5 text-yellow-500" />;

    const ext = file.name.split('.').pop()?.toLowerCase() || '';

    const imageExts = ['jpg', 'jpeg', 'png', 'gif', 'bmp', 'svg', 'webp', 'ico'];
    const audioExts = ['mp3', 'wav', 'flac', 'aac', 'ogg', 'm4a'];
    const videoExts = ['mp4', 'avi', 'mkv', 'mov', 'wmv', 'flv', 'webm'];
    const archiveExts = ['zip', 'rar', '7z', 'tar', 'gz', 'bz2', 'xz'];
    const codeExts = ['js', 'ts', 'jsx', 'tsx', 'py', 'go', 'rs', 'java', 'c', 'cpp', 'h', 'cs', 'php', 'rb', 'swift', 'kt'];
    const textExts = ['txt', 'md', 'log', 'json', 'xml', 'yaml', 'yml', 'toml', 'ini', 'cfg', 'conf'];

    if (imageExts.includes(ext)) return <Image className="w-5 h-5 text-purple-500" />;
    if (audioExts.includes(ext)) return <Music className="w-5 h-5 text-pink-500" />;
    if (videoExts.includes(ext)) return <Video className="w-5 h-5 text-red-500" />;
    if (archiveExts.includes(ext)) return <Archive className="w-5 h-5 text-orange-500" />;
    if (codeExts.includes(ext)) return <Code className="w-5 h-5 text-green-500" />;
    if (textExts.includes(ext)) return <FileText className="w-5 h-5 text-blue-500" />;

    return <File className="w-5 h-5 text-gray-500" />;
  };

  const filteredFiles = showHidden ? files : files.filter(f => !f.is_hidden);

  return (
    <div className="flex flex-col h-[500px] bg-white rounded-lg border border-border overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 bg-gray-50 border-b border-border">
        <div className="flex items-center gap-2">
          <Folder className="w-5 h-5 text-primary" />
          <span className="font-semibold text-text-primary">File Browser</span>
        </div>
        <button
          onClick={onClose}
          className="p-1 text-gray-400 hover:text-gray-600 transition-colors"
        >
          <X className="w-5 h-5" />
        </button>
      </div>

      {/* Toolbar */}
      <div className="flex items-center gap-2 px-4 py-2 bg-gray-50 border-b border-border">
        <button
          onClick={goBack}
          disabled={historyIndex <= 0}
          className="p-1.5 rounded hover:bg-gray-200 disabled:opacity-50 disabled:cursor-not-allowed"
          title="Back"
        >
          <ChevronLeft className="w-4 h-4" />
        </button>
        <button
          onClick={goForward}
          disabled={historyIndex >= pathHistory.length - 1}
          className="p-1.5 rounded hover:bg-gray-200 disabled:opacity-50 disabled:cursor-not-allowed"
          title="Forward"
        >
          <ChevronRight className="w-4 h-4" />
        </button>
        <button
          onClick={goUp}
          className="p-1.5 rounded hover:bg-gray-200"
          title="Go up"
        >
          <ArrowUp className="w-4 h-4" />
        </button>
        <button
          onClick={goHome}
          className="p-1.5 rounded hover:bg-gray-200"
          title="Home"
        >
          <Home className="w-4 h-4" />
        </button>
        <button
          onClick={() => listDirectory(currentPath)}
          disabled={loading}
          className="p-1.5 rounded hover:bg-gray-200"
          title="Refresh"
        >
          <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
        </button>

        <div className="flex-1 px-3 py-1.5 bg-white border border-border rounded text-sm font-mono truncate">
          {currentPath}
        </div>

        <label className="flex items-center gap-2 text-sm text-text-secondary cursor-pointer">
          <input
            type="checkbox"
            checked={showHidden}
            onChange={(e) => setShowHidden(e.target.checked)}
            className="rounded"
          />
          Show hidden
        </label>
      </div>

      {/* File list */}
      <div className="flex-1 overflow-auto">
        {loading && (
          <div className="flex items-center justify-center h-full">
            <RefreshCw className="w-6 h-6 animate-spin text-primary" />
          </div>
        )}

        {error && (
          <div className="flex flex-col items-center justify-center h-full text-center p-4">
            <p className="text-red-500 mb-2">{error}</p>
            <Button variant="secondary" size="sm" onClick={() => listDirectory(currentPath)}>
              Retry
            </Button>
          </div>
        )}

        {!loading && !error && filteredFiles.length === 0 && (
          <div className="flex items-center justify-center h-full text-text-secondary">
            Empty directory
          </div>
        )}

        {!loading && !error && filteredFiles.length > 0 && (
          <table className="w-full">
            <thead className="bg-gray-50 sticky top-0">
              <tr className="text-left text-xs font-medium text-text-secondary uppercase">
                <th className="px-4 py-2">Name</th>
                <th className="px-4 py-2 w-24">Size</th>
                <th className="px-4 py-2 w-48">Modified</th>
                <th className="px-4 py-2 w-24">Mode</th>
                <th className="px-4 py-2 w-20"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {filteredFiles.map((file) => (
                <tr
                  key={file.path}
                  className={`hover:bg-gray-50 cursor-pointer ${
                    selectedFile?.path === file.path ? 'bg-blue-50' : ''
                  }`}
                  onClick={() => handleFileClick(file)}
                  onDoubleClick={() => file.is_dir && navigateTo(file.path)}
                >
                  <td className="px-4 py-2">
                    <div className="flex items-center gap-2">
                      {getFileIcon(file)}
                      <span className={`text-sm ${file.is_hidden ? 'text-gray-400' : 'text-text-primary'}`}>
                        {file.name}
                      </span>
                    </div>
                  </td>
                  <td className="px-4 py-2 text-sm text-text-secondary">
                    {file.is_dir ? '-' : formatFileSize(file.size)}
                  </td>
                  <td className="px-4 py-2 text-sm text-text-secondary">
                    {formatDate(file.modified_time)}
                  </td>
                  <td className="px-4 py-2">
                    <code className="text-xs text-text-secondary">{file.mode}</code>
                  </td>
                  <td className="px-4 py-2">
                    {!file.is_dir && (
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          downloadFile(file);
                        }}
                        className="p-1 text-gray-400 hover:text-primary transition-colors"
                        title="Download"
                      >
                        <Download className="w-4 h-4" />
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Footer */}
      <div className="flex items-center justify-between px-4 py-2 bg-gray-50 border-t border-border text-sm text-text-secondary">
        <span>
          {filteredFiles.length} items
          {!showHidden && files.length !== filteredFiles.length && (
            <span className="ml-1">({files.length - filteredFiles.length} hidden)</span>
          )}
        </span>
        {selectedFile && !selectedFile.is_dir && (
          <span>Selected: {selectedFile.name} ({formatFileSize(selectedFile.size)})</span>
        )}
      </div>
    </div>
  );
}
