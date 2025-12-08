import React, { useState, useEffect } from 'react';
import { Download, RefreshCw, X, CheckCircle } from 'lucide-react';

interface UpdateInfo {
  version: string;
  releaseNotes?: string;
}

interface DownloadProgress {
  percent: number;
  bytesPerSecond: number;
  transferred: number;
  total: number;
}

type UpdateState =
  | 'idle'
  | 'checking'
  | 'available'
  | 'downloading'
  | 'downloaded'
  | 'error';

export function UpdateNotification() {
  const [state, setState] = useState<UpdateState>('idle');
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [progress, setProgress] = useState<DownloadProgress | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [dismissed, setDismissed] = useState(false);
  const [currentVersion, setCurrentVersion] = useState<string>('');

  useEffect(() => {
    // Get current version
    window.api.updater.getVersion().then(setCurrentVersion);

    // Set up event listeners
    const unsubAvailable = window.api.updater.onUpdateAvailable((info) => {
      setUpdateInfo(info);
      setState('available');
      setDismissed(false);
    });

    const unsubNotAvailable = window.api.updater.onUpdateNotAvailable(() => {
      setState('idle');
    });

    const unsubProgress = window.api.updater.onDownloadProgress((prog) => {
      setProgress(prog);
      setState('downloading');
    });

    const unsubDownloaded = window.api.updater.onUpdateDownloaded((info) => {
      setUpdateInfo(info);
      setState('downloaded');
    });

    const unsubError = window.api.updater.onError((err) => {
      setError(err.message);
      setState('error');
    });

    return () => {
      unsubAvailable();
      unsubNotAvailable();
      unsubProgress();
      unsubDownloaded();
      unsubError();
    };
  }, []);

  const handleCheckForUpdates = async () => {
    setState('checking');
    setError(null);
    try {
      await window.api.updater.checkForUpdates();
    } catch (err: any) {
      setError(err.message);
      setState('error');
    }
  };

  const handleDownload = async () => {
    setState('downloading');
    setProgress({ percent: 0, bytesPerSecond: 0, transferred: 0, total: 0 });
    try {
      await window.api.updater.downloadUpdate();
    } catch (err: any) {
      setError(err.message);
      setState('error');
    }
  };

  const handleInstall = () => {
    window.api.updater.installUpdate();
  };

  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  if (dismissed && state !== 'downloaded') {
    return null;
  }

  // Don't show anything if idle or just checking
  if (state === 'idle' || state === 'checking') {
    return null;
  }

  return (
    <div className="fixed bottom-4 right-4 max-w-sm bg-white rounded-lg shadow-lg border border-gray-200 overflow-hidden z-50">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 bg-gray-50 border-b border-gray-200">
        <div className="flex items-center gap-2">
          {state === 'downloaded' ? (
            <CheckCircle className="w-5 h-5 text-green-500" />
          ) : (
            <Download className="w-5 h-5 text-blue-500" />
          )}
          <span className="font-medium text-gray-900">
            {state === 'downloaded' ? 'Update Ready' : 'Update Available'}
          </span>
        </div>
        {state !== 'downloaded' && (
          <button
            onClick={() => setDismissed(true)}
            className="text-gray-400 hover:text-gray-600"
          >
            <X className="w-5 h-5" />
          </button>
        )}
      </div>

      {/* Content */}
      <div className="px-4 py-3">
        {state === 'available' && updateInfo && (
          <>
            <p className="text-sm text-gray-600 mb-2">
              Version <span className="font-medium">{updateInfo.version}</span> is available.
              <br />
              You are currently on version <span className="font-medium">{currentVersion}</span>.
            </p>
            <button
              onClick={handleDownload}
              className="w-full flex items-center justify-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700 transition-colors"
            >
              <Download className="w-4 h-4" />
              Download Update
            </button>
          </>
        )}

        {state === 'downloading' && progress && (
          <>
            <p className="text-sm text-gray-600 mb-2">
              Downloading update...
            </p>
            <div className="w-full bg-gray-200 rounded-full h-2.5 mb-2">
              <div
                className="bg-blue-600 h-2.5 rounded-full transition-all duration-300"
                style={{ width: `${progress.percent}%` }}
              />
            </div>
            <div className="flex justify-between text-xs text-gray-500">
              <span>{progress.percent.toFixed(1)}%</span>
              <span>{formatBytes(progress.bytesPerSecond)}/s</span>
            </div>
          </>
        )}

        {state === 'downloaded' && updateInfo && (
          <>
            <p className="text-sm text-gray-600 mb-2">
              Version <span className="font-medium">{updateInfo.version}</span> has been downloaded
              and is ready to install.
            </p>
            <button
              onClick={handleInstall}
              className="w-full flex items-center justify-center gap-2 px-4 py-2 bg-green-600 text-white rounded-md hover:bg-green-700 transition-colors"
            >
              <RefreshCw className="w-4 h-4" />
              Restart & Install
            </button>
          </>
        )}

        {state === 'error' && (
          <>
            <p className="text-sm text-red-600 mb-2">
              {error || 'An error occurred while checking for updates.'}
            </p>
            <button
              onClick={handleCheckForUpdates}
              className="w-full flex items-center justify-center gap-2 px-4 py-2 bg-gray-600 text-white rounded-md hover:bg-gray-700 transition-colors"
            >
              <RefreshCw className="w-4 h-4" />
              Try Again
            </button>
          </>
        )}
      </div>
    </div>
  );
}
