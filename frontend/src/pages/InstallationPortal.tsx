import { useState, useEffect, useCallback } from 'react';
import { useParams } from 'react-router-dom';
import { publicApi } from '@/services/publicApi';

interface LinkInfo {
  valid: boolean;
  deviceName?: string;
  userName?: string;
  companyName?: string;
  expiresAt?: string;
  status?: string;
  downloadAvailable?: boolean;
  alreadyDownloaded?: boolean;
  alreadyInstalled?: boolean;
  downloadCount?: number;
  error?: string;
  message?: string;
  installInstructions?: string;
}

interface InstallStatus {
  status: string;
  agentConnected: boolean;
  connectedAt?: string;
  agentVersion?: string;
  deviceId?: number;
}

type ProgressStep = 'validated' | 'downloaded' | 'installing' | 'connected';

export default function InstallationPortal() {
  const { downloadToken } = useParams<{ downloadToken: string }>();
  const [loading, setLoading] = useState(true);
  const [linkInfo, setLinkInfo] = useState<LinkInfo | null>(null);
  const [downloading, setDownloading] = useState(false);
  const [hasDownloaded, setHasDownloaded] = useState(false);
  const [status, setStatus] = useState<InstallStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [currentStep, setCurrentStep] = useState<ProgressStep>('validated');

  // Validate the download token on mount
  useEffect(() => {
    if (!downloadToken) return;

    const validateLink = async () => {
      try {
        const data = await publicApi.validateInstallLink(downloadToken);
        setLinkInfo(data);
        if (data.alreadyDownloaded) {
          setHasDownloaded(true);
          setCurrentStep('downloaded');
        }
        if (data.alreadyInstalled || data.status === 'installed') {
          setCurrentStep('connected');
        }
      } catch (err: any) {
        if (err.response?.data) {
          setLinkInfo(err.response.data);
        } else {
          setError('Failed to validate installation link');
        }
      } finally {
        setLoading(false);
      }
    };

    validateLink();
  }, [downloadToken]);

  // Poll for installation status after download
  const pollStatus = useCallback(async () => {
    if (!downloadToken || currentStep === 'connected') return;

    try {
      const data = await publicApi.checkInstallStatus(downloadToken);
      setStatus(data);
      if (data.agentConnected) {
        setCurrentStep('connected');
      }
    } catch (err) {
      console.error('Failed to check status:', err);
    }
  }, [downloadToken, currentStep]);

  useEffect(() => {
    if (!hasDownloaded || currentStep === 'connected') return;

    const interval = setInterval(pollStatus, 10000); // Poll every 10 seconds
    return () => clearInterval(interval);
  }, [hasDownloaded, currentStep, pollStatus]);

  const handleDownload = async () => {
    if (!downloadToken) return;

    setDownloading(true);
    try {
      // Trigger the download
      const downloadUrl = publicApi.getInstallerDownloadUrl(downloadToken);
      window.location.href = downloadUrl;

      // Mark as downloaded after a short delay
      setTimeout(() => {
        setHasDownloaded(true);
        setCurrentStep('downloaded');
        setDownloading(false);
      }, 2000);
    } catch (err) {
      setError('Failed to start download');
      setDownloading(false);
    }
  };

  const formatExpirationDate = (dateStr: string) => {
    const date = new Date(dateStr);
    return date.toLocaleString(undefined, {
      weekday: 'short',
      month: 'short',
      day: 'numeric',
      year: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
    });
  };

  const getHoursRemaining = (dateStr: string) => {
    const diff = new Date(dateStr).getTime() - Date.now();
    return Math.max(0, Math.floor(diff / (1000 * 60 * 60)));
  };

  // Loading state
  if (loading) {
    return (
      <div className="min-h-screen bg-gradient-to-br from-indigo-50 to-purple-50 flex items-center justify-center">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-indigo-600"></div>
      </div>
    );
  }

  // Error states
  if (!linkInfo?.valid || error) {
    const errorType = linkInfo?.error || 'unknown';
    const errorMessages: Record<string, { title: string; message: string; icon: string }> = {
      expired: {
        title: 'Installation Link Expired',
        message: linkInfo?.message || 'This installation link has expired. Please contact your IT administrator for a new link.',
        icon: 'clock',
      },
      revoked: {
        title: 'Installation Link Revoked',
        message: linkInfo?.message || 'This installation link has been cancelled by your administrator.',
        icon: 'ban',
      },
      not_found: {
        title: 'Link Not Found',
        message: linkInfo?.message || 'This installation link was not found. Please check the URL or contact your IT administrator.',
        icon: 'question',
      },
      unknown: {
        title: 'Error',
        message: error || 'An unexpected error occurred. Please try again later.',
        icon: 'exclamation',
      },
    };

    const { title, message, icon } = errorMessages[errorType] || errorMessages.unknown;

    return (
      <div className="min-h-screen bg-gradient-to-br from-red-50 to-orange-50 flex items-center justify-center p-4">
        <div className="max-w-md w-full bg-white rounded-xl shadow-lg p-8 text-center">
          <div className="w-16 h-16 mx-auto mb-4 bg-red-100 rounded-full flex items-center justify-center">
            {icon === 'clock' && (
              <svg className="w-8 h-8 text-red-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            )}
            {icon === 'ban' && (
              <svg className="w-8 h-8 text-red-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636" />
              </svg>
            )}
            {icon === 'question' && (
              <svg className="w-8 h-8 text-red-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8.228 9c.549-1.165 2.03-2 3.772-2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            )}
            {icon === 'exclamation' && (
              <svg className="w-8 h-8 text-red-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
            )}
          </div>
          <h1 className="text-2xl font-bold text-gray-900 mb-2">{title}</h1>
          <p className="text-gray-600 mb-6">{message}</p>
          <div className="text-sm text-gray-500">
            Need help? Contact your IT administrator.
          </div>
        </div>
      </div>
    );
  }

  // Success / already installed state
  if (currentStep === 'connected' || linkInfo.alreadyInstalled) {
    return (
      <div className="min-h-screen bg-gradient-to-br from-green-50 to-emerald-50 flex items-center justify-center p-4">
        <div className="max-w-md w-full bg-white rounded-xl shadow-lg p-8 text-center">
          <div className="w-16 h-16 mx-auto mb-4 bg-green-100 rounded-full flex items-center justify-center">
            <svg className="w-10 h-10 text-green-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
            </svg>
          </div>
          <h1 className="text-2xl font-bold text-gray-900 mb-2">Installation Complete!</h1>
          <p className="text-gray-600 mb-4">
            Sentinel Agent has been successfully installed on <strong>{linkInfo.deviceName}</strong>.
          </p>
          {status?.agentVersion && (
            <p className="text-sm text-gray-500 mb-4">
              Agent Version: {status.agentVersion}
            </p>
          )}
          <p className="text-gray-500 text-sm">
            Your device is now being monitored and can receive remote support.
            You can safely close this page.
          </p>
        </div>
      </div>
    );
  }

  // Main installation portal
  return (
    <div className="min-h-screen bg-gradient-to-br from-indigo-50 to-purple-50 py-8 px-4">
      <div className="max-w-2xl mx-auto">
        {/* Header */}
        <div className="text-center mb-8">
          <div className="inline-flex items-center justify-center w-16 h-16 bg-indigo-600 rounded-2xl mb-4 shadow-lg">
            <svg className="w-8 h-8 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
            </svg>
          </div>
          <h1 className="text-3xl font-bold text-gray-900 mb-2">Install Sentinel Agent</h1>
          <p className="text-gray-600">
            {linkInfo.companyName || 'Your organization'} requires this software for remote support
          </p>
        </div>

        {/* Device Info Card */}
        <div className="bg-white rounded-xl shadow-sm border border-gray-100 p-6 mb-6">
          <div className="flex items-start gap-4">
            <div className="w-12 h-12 bg-indigo-100 rounded-xl flex items-center justify-center flex-shrink-0">
              <svg className="w-6 h-6 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
              </svg>
            </div>
            <div className="flex-1">
              <h2 className="text-lg font-semibold text-gray-900">{linkInfo.deviceName}</h2>
              {linkInfo.userName && (
                <p className="text-gray-600">For: {linkInfo.userName}</p>
              )}
              <p className="text-sm text-gray-500 mt-1">
                {linkInfo.companyName}
              </p>
            </div>
          </div>
        </div>

        {/* Progress Steps */}
        <div className="bg-white rounded-xl shadow-sm border border-gray-100 p-6 mb-6">
          <h3 className="text-lg font-semibold text-gray-900 mb-4">Installation Progress</h3>
          <div className="space-y-3">
            <ProgressItem
              label="Link validated"
              completed={true}
              active={currentStep === 'validated'}
            />
            <ProgressItem
              label="Download installer"
              completed={hasDownloaded}
              active={currentStep === 'downloaded' && !hasDownloaded}
            />
            <ProgressItem
              label="Run installation"
              completed={status?.agentConnected || false}
              active={hasDownloaded && !status?.agentConnected}
              loading={hasDownloaded && !status?.agentConnected}
            />
            <ProgressItem
              label="Agent connection"
              completed={status?.agentConnected || false}
              active={false}
            />
          </div>
        </div>

        {/* Download Section */}
        <div className="bg-white rounded-xl shadow-sm border border-gray-100 p-6 mb-6">
          <div className="text-center">
            <button
              onClick={handleDownload}
              disabled={downloading || !linkInfo.downloadAvailable}
              className="inline-flex items-center gap-3 px-8 py-4 bg-indigo-600 text-white rounded-xl font-semibold text-lg hover:bg-indigo-700 disabled:bg-indigo-300 disabled:cursor-not-allowed transition-colors shadow-lg hover:shadow-xl"
            >
              {downloading ? (
                <>
                  <svg className="animate-spin h-6 w-6" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                  </svg>
                  Preparing Download...
                </>
              ) : (
                <>
                  <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
                  </svg>
                  Download Sentinel Agent
                </>
              )}
            </button>
            <p className="text-sm text-gray-500 mt-3">
              Windows 64-bit • ~35 MB • Version 1.67.6
            </p>
            {linkInfo.downloadCount && linkInfo.downloadCount > 0 && (
              <p className="text-xs text-gray-400 mt-1">
                Downloaded {linkInfo.downloadCount} time(s)
              </p>
            )}
          </div>
        </div>

        {/* Instructions */}
        {hasDownloaded && (
          <div className="bg-amber-50 border border-amber-200 rounded-xl p-6 mb-6">
            <h4 className="font-semibold text-amber-800 mb-3 flex items-center gap-2">
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              What to do next:
            </h4>
            <ol className="list-decimal list-inside space-y-2 text-amber-700">
              <li>Open the downloaded ZIP file</li>
              <li>Extract the contents to a folder</li>
              <li>Right-click <strong>quick-install.ps1</strong> and select "Run with PowerShell"</li>
              <li>Click "Yes" when prompted for administrator permission</li>
              <li>Wait for the installation to complete</li>
            </ol>
            <p className="text-sm text-amber-600 mt-4">
              This page will automatically update when the agent connects.
            </p>
          </div>
        )}

        {/* Expiration Notice */}
        {linkInfo.expiresAt && (
          <div className="text-center text-sm text-gray-500">
            <span className="inline-flex items-center gap-1">
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              Link expires: {formatExpirationDate(linkInfo.expiresAt)} ({getHoursRemaining(linkInfo.expiresAt)} hours remaining)
            </span>
          </div>
        )}
      </div>
    </div>
  );
}

function ProgressItem({
  label,
  completed,
  active,
  loading = false,
}: {
  label: string;
  completed: boolean;
  active: boolean;
  loading?: boolean;
}) {
  return (
    <div className="flex items-center gap-3">
      <div
        className={`w-8 h-8 rounded-full flex items-center justify-center flex-shrink-0 ${
          completed
            ? 'bg-green-100 text-green-600'
            : active
            ? 'bg-indigo-100 text-indigo-600'
            : 'bg-gray-100 text-gray-400'
        }`}
      >
        {completed ? (
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
          </svg>
        ) : loading ? (
          <svg className="animate-spin w-5 h-5" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
          </svg>
        ) : (
          <div className="w-2 h-2 rounded-full bg-current"></div>
        )}
      </div>
      <span
        className={`${
          completed ? 'text-green-700' : active ? 'text-indigo-700 font-medium' : 'text-gray-500'
        }`}
      >
        {label}
      </span>
    </div>
  );
}
