import React, { useEffect, useState } from 'react';
import { useCertificateStore, CertificateInfo, AgentCertStatus } from '../stores/certificateStore';

function ShieldIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
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

function SendIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
    </svg>
  );
}

function CheckCircleIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
    </svg>
  );
}

function ClockIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
    </svg>
  );
}

function ExclamationIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
    </svg>
  );
}

function getExpiryStatusColor(daysUntilExpiry: number): string {
  if (daysUntilExpiry <= 7) return 'text-red-500';
  if (daysUntilExpiry <= 30) return 'text-yellow-500';
  return 'text-green-500';
}

function getExpiryBadgeClass(daysUntilExpiry: number): string {
  if (daysUntilExpiry <= 7) return 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-400';
  if (daysUntilExpiry <= 30) return 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-400';
  return 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400';
}

function formatDate(dateString: string): string {
  return new Date(dateString).toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  });
}

function CertificateCard({ cert }: { cert: CertificateInfo }) {
  const daysLeft = cert.daysUntilExpiry ?? 0;
  const statusColor = getExpiryStatusColor(daysLeft);
  const badgeClass = getExpiryBadgeClass(daysLeft);

  // Handle missing certificate
  if (!cert.exists) {
    return (
      <div className="card p-6 opacity-60">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className="p-2 rounded-lg bg-gray-200 dark:bg-gray-700 text-gray-500">
              <ShieldIcon className="w-6 h-6" />
            </div>
            <div>
              <h3 className="font-semibold text-text-primary">{cert.name}</h3>
              <p className="text-sm text-text-secondary">Not generated</p>
            </div>
          </div>
          <span className="px-2 py-1 text-xs font-medium rounded-full bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-400">
            Missing
          </span>
        </div>
      </div>
    );
  }

  return (
    <div className="card p-6">
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-3">
          <div className={`p-2 rounded-lg bg-primary-light ${statusColor}`}>
            <ShieldIcon className="w-6 h-6" />
          </div>
          <div>
            <h3 className="font-semibold text-text-primary">{cert.name}</h3>
            <p className="text-sm text-text-secondary">{cert.subject || 'Unknown'}</p>
          </div>
        </div>
        <span className={`px-2 py-1 text-xs font-medium rounded-full ${badgeClass}`}>
          {daysLeft <= 0 ? 'Expired' : `${daysLeft} days left`}
        </span>
      </div>

      <div className="mt-4 grid grid-cols-2 gap-4 text-sm">
        <div>
          <span className="text-text-secondary">Issuer</span>
          <p className="text-text-primary font-medium">{cert.issuer || 'Unknown'}</p>
        </div>
        <div>
          <span className="text-text-secondary">Serial Number</span>
          <p className="text-text-primary font-mono text-xs">{cert.serialNumber || 'N/A'}</p>
        </div>
        <div>
          <span className="text-text-secondary">Valid From</span>
          <p className="text-text-primary">{cert.validFrom ? formatDate(cert.validFrom) : 'N/A'}</p>
        </div>
        <div>
          <span className="text-text-secondary">Valid To</span>
          <p className={`font-medium ${statusColor}`}>{cert.validTo ? formatDate(cert.validTo) : 'N/A'}</p>
        </div>
      </div>

      <div className="mt-4 pt-4 border-t border-border">
        <span className="text-text-secondary text-sm">Fingerprint</span>
        <p className="text-text-primary font-mono text-xs break-all">{cert.fingerprint || 'N/A'}</p>
      </div>
    </div>
  );
}

function AgentStatusTable({ statuses, currentCertHash }: { statuses: AgentCertStatus[]; currentCertHash: string | null }) {
  if (statuses.length === 0) {
    return (
      <div className="card p-6 text-center text-text-secondary">
        <p>No agent certificate data available</p>
      </div>
    );
  }

  return (
    <div className="card overflow-hidden">
      <table className="w-full">
        <thead className="bg-gray-50 dark:bg-slate-700">
          <tr>
            <th className="px-4 py-3 text-left text-xs font-medium text-text-secondary uppercase tracking-wider">Agent</th>
            <th className="px-4 py-3 text-left text-xs font-medium text-text-secondary uppercase tracking-wider">Status</th>
            <th className="px-4 py-3 text-left text-xs font-medium text-text-secondary uppercase tracking-wider">Distributed</th>
            <th className="px-4 py-3 text-left text-xs font-medium text-text-secondary uppercase tracking-wider">Confirmed</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {statuses.map((status) => {
            const isCurrent = currentCertHash && status.caCertHash === currentCertHash;
            const isConfirmed = status.confirmedAt !== null;

            return (
              <tr key={status.agentId} className="hover:bg-gray-50 dark:hover:bg-slate-700/50">
                <td className="px-4 py-3">
                  <span className="text-text-primary font-medium">{status.agentName || status.agentId}</span>
                </td>
                <td className="px-4 py-3">
                  {isCurrent ? (
                    <span className="inline-flex items-center gap-1 text-green-600 dark:text-green-400">
                      <CheckCircleIcon className="w-4 h-4" />
                      Current
                    </span>
                  ) : status.caCertHash ? (
                    <span className="inline-flex items-center gap-1 text-yellow-600 dark:text-yellow-400">
                      <ExclamationIcon className="w-4 h-4" />
                      Outdated
                    </span>
                  ) : (
                    <span className="text-text-secondary">Unknown</span>
                  )}
                </td>
                <td className="px-4 py-3 text-text-secondary text-sm">
                  {status.distributedAt ? formatDate(status.distributedAt) : '-'}
                </td>
                <td className="px-4 py-3">
                  {isConfirmed ? (
                    <span className="inline-flex items-center gap-1 text-green-600 dark:text-green-400 text-sm">
                      <CheckCircleIcon className="w-4 h-4" />
                      {formatDate(status.confirmedAt!)}
                    </span>
                  ) : status.distributedAt ? (
                    <span className="inline-flex items-center gap-1 text-yellow-600 dark:text-yellow-400 text-sm">
                      <ClockIcon className="w-4 h-4" />
                      Pending
                    </span>
                  ) : (
                    <span className="text-text-secondary text-sm">-</span>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

export function Certificates() {
  const {
    certificates,
    agentStatuses,
    currentCertHash,
    loading,
    renewing,
    distributing,
    error,
    fetchCertificates,
    fetchAgentStatuses,
    renewCertificates,
    distributeCertificates,
    subscribeToEvents,
  } = useCertificateStore();

  const [showRenewConfirm, setShowRenewConfirm] = useState(false);

  useEffect(() => {
    fetchCertificates();
    fetchAgentStatuses();
    const unsubscribe = subscribeToEvents();
    return unsubscribe;
  }, []);

  const handleRenew = async () => {
    try {
      await renewCertificates();
      setShowRenewConfirm(false);
      alert('Certificates renewed successfully!');
    } catch (err: any) {
      alert(`Failed to renew certificates: ${err.message}`);
    }
  };

  const handleDistribute = async () => {
    try {
      const result = await distributeCertificates();
      alert(`Certificate distributed to ${result.success} agents (${result.failed} failed)`);
    } catch (err: any) {
      alert(`Failed to distribute certificate: ${err.message}`);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-text-secondary">Loading certificates...</p>
      </div>
    );
  }

  const needsRenewal = certificates.some((cert) => (cert.daysUntilExpiry ?? 0) <= 30);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">Certificates</h1>
          <p className="text-text-secondary">Manage TLS certificates and distribute to agents</p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={() => setShowRenewConfirm(true)}
            disabled={renewing}
            className={`btn ${needsRenewal ? 'btn-warning' : 'btn-secondary'} flex items-center gap-2`}
          >
            <RefreshIcon className={`w-4 h-4 ${renewing ? 'animate-spin' : ''}`} />
            {renewing ? 'Renewing...' : 'Renew Certificates'}
          </button>
          <button
            onClick={handleDistribute}
            disabled={distributing || certificates.length === 0}
            className="btn btn-primary flex items-center gap-2"
          >
            <SendIcon className={`w-4 h-4 ${distributing ? 'animate-pulse' : ''}`} />
            {distributing ? 'Distributing...' : 'Distribute to Agents'}
          </button>
        </div>
      </div>

      {/* Error display */}
      {error && (
        <div className="bg-red-100 dark:bg-red-900/30 border border-red-300 dark:border-red-700 text-red-800 dark:text-red-400 px-4 py-3 rounded-lg">
          {error}
        </div>
      )}

      {/* Certificates Grid */}
      <div>
        <h2 className="text-lg font-semibold text-text-primary mb-4">TLS Certificates</h2>
        {certificates.length === 0 ? (
          <div className="card p-6 text-center text-text-secondary">
            <ShieldIcon className="w-12 h-12 mx-auto mb-2 opacity-50" />
            <p>No certificates found</p>
            <p className="text-sm">Click "Renew Certificates" to generate new certificates</p>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {certificates.map((cert, idx) => (
              <CertificateCard key={idx} cert={cert} />
            ))}
          </div>
        )}
      </div>

      {/* Agent Distribution Status */}
      <div>
        <h2 className="text-lg font-semibold text-text-primary mb-4">Agent Certificate Status</h2>
        <AgentStatusTable statuses={agentStatuses} currentCertHash={currentCertHash} />
      </div>

      {/* Renew Confirmation Modal */}
      {showRenewConfirm && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="card p-6 max-w-md mx-4">
            <h3 className="text-lg font-semibold text-text-primary mb-2">Renew Certificates?</h3>
            <p className="text-text-secondary mb-4">
              This will generate new CA and server certificates. Connected agents will need to receive the new CA certificate to maintain secure communication.
            </p>
            <div className="flex justify-end gap-2">
              <button
                onClick={() => setShowRenewConfirm(false)}
                className="btn btn-secondary"
                disabled={renewing}
              >
                Cancel
              </button>
              <button
                onClick={handleRenew}
                className="btn btn-primary"
                disabled={renewing}
              >
                {renewing ? 'Renewing...' : 'Renew'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
