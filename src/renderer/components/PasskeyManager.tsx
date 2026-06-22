import React, { useState, useEffect } from 'react';
import { browserSupportsWebAuthn, startRegistration } from '@simplewebauthn/browser';
import { passkeys as passkeysService } from '../services';

interface Passkey {
  id: string;
  name: string;
  createdAt: string;
  lastUsedAt?: string;
}

function KeyIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z" />
    </svg>
  );
}

function FingerprintIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 11c0 3.517-1.009 6.799-2.753 9.571m-3.44-2.04l.054-.09A13.916 13.916 0 008 11a4 4 0 118 0c0 1.017-.07 2.019-.203 3m-2.118 6.844A21.88 21.88 0 0015.171 17m3.839 1.132c.645-2.266.99-4.659.99-7.132A8 8 0 008 4.07M3 15.364c.64-1.319 1-2.8 1-4.364 0-1.457.39-2.823 1.07-4" />
    </svg>
  );
}

function PlusIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
    </svg>
  );
}

function TrashIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
    </svg>
  );
}

function EditIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
    </svg>
  );
}

function CheckIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
    </svg>
  );
}

function XIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
    </svg>
  );
}

function AlertIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
    </svg>
  );
}

export function PasskeyManager() {
  const [isSupported, setIsSupported] = useState<boolean | null>(null);
  const [passkeys, setPasskeys] = useState<Passkey[]>([]);
  const [loading, setLoading] = useState(true);
  const [isRegistering, setIsRegistering] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editName, setEditName] = useState('');
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setIsSupported(browserSupportsWebAuthn());
    void loadPasskeys();
  }, []);

  const loadPasskeys = async () => {
    setLoading(true);
    try {
      const data = await passkeysService.list();
      setPasskeys(data);
      setError(null);
    } catch (err) {
      console.error('Failed to load passkeys:', err);
      setError('Failed to load passkeys');
      setPasskeys([]);
    } finally {
      setLoading(false);
    }
  };

  const handleAddPasskey = async () => {
    setIsRegistering(true);
    setError(null);

    try {
      // Step 1: Begin registration
      const beginResponse = await passkeysService.beginRegistration();
      if (!beginResponse) throw new Error('Failed to start registration');

      const { sessionId, options } = beginResponse;

      // Step 2: Prompt user for biometric
      // go-webauthn returns { publicKey: { ... } }, but @simplewebauthn/browser expects just the publicKey contents
      const publicKeyOptions = (options as any)?.publicKey || options;
      const registrationResponse = await startRegistration(publicKeyOptions);

      // Step 3: Complete registration
      await passkeysService.finishRegistration({
        sessionId,
        response: registrationResponse,
        name: `Passkey ${new Date().toLocaleDateString()}`,
      });

      alert('Passkey registered successfully');
      void loadPasskeys();
    } catch (err: any) {
      if (err.name === 'NotAllowedError') {
        setError('Registration was cancelled');
      } else if (err.name === 'InvalidStateError') {
        setError('This passkey is already registered');
      } else {
        setError(err.message || 'Failed to register passkey');
      }
    } finally {
      setIsRegistering(false);
    }
  };

  const handleDelete = async (id: string, name: string) => {
    if (!window.confirm(`Delete "${name}"? This cannot be undone.`)) return;

    try {
      await passkeysService.delete(id);
      void loadPasskeys();
    } catch (err) {
      alert('Failed to delete passkey');
    }
  };

  const handleStartEdit = (passkey: Passkey) => {
    setEditingId(passkey.id);
    setEditName(passkey.name);
  };

  const handleSaveEdit = async () => {
    if (!editingId || !editName.trim()) return;

    try {
      await passkeysService.rename(editingId, editName.trim());
      setEditingId(null);
      void loadPasskeys();
    } catch (err) {
      alert('Failed to rename passkey');
    }
  };

  const handleCancelEdit = () => {
    setEditingId(null);
    setEditName('');
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
    });
  };

  // Still checking browser support
  if (isSupported === null) {
    return null;
  }

  // Browser doesn't support passkeys
  if (!isSupported) {
    return (
      <div className="card p-6">
        <div className="flex items-center gap-3 mb-4">
          <div className="p-2 bg-yellow-100 text-yellow-600 dark:bg-yellow-900/30 dark:text-yellow-400 rounded-lg">
            <AlertIcon className="w-5 h-5" />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-text-primary">Passkeys Not Supported</h3>
            <p className="text-sm text-text-secondary">
              Your browser does not support passkeys. Try Chrome, Safari, or Edge.
            </p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="card p-6">
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-indigo-100 text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-400 rounded-lg">
            <KeyIcon className="w-5 h-5" />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-text-primary">Passkeys</h3>
            <p className="text-sm text-text-secondary">
              Sign in without a password using biometrics or security keys
            </p>
          </div>
        </div>
        <button
          onClick={() => { void handleAddPasskey(); }}
          disabled={isRegistering}
          className="btn btn-primary flex items-center gap-2"
        >
          {isRegistering ? (
            <>
              <span className="animate-spin">...</span>
              Registering...
            </>
          ) : (
            <>
              <PlusIcon className="w-4 h-4" />
              Add Passkey
            </>
          )}
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
          <p className="text-sm text-red-800 dark:text-red-200">{error}</p>
        </div>
      )}

      {loading ? (
        <div className="flex items-center justify-center py-8">
          <p className="text-text-secondary">Loading...</p>
        </div>
      ) : passkeys.length === 0 ? (
        <div className="text-center py-8">
          <FingerprintIcon className="w-12 h-12 mx-auto text-gray-400 mb-3" />
          <p className="text-text-secondary mb-2">No passkeys registered</p>
          <p className="text-sm text-text-secondary">
            Add a passkey to sign in faster using your device's biometrics or a security key.
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {passkeys.map((passkey) => (
            <div
              key={passkey.id}
              className="flex items-center justify-between p-4 bg-gray-50 dark:bg-slate-800 rounded-lg border border-border"
            >
              <div className="flex items-center gap-3">
                <FingerprintIcon className="w-5 h-5 text-primary" />
                <div>
                  {editingId === passkey.id ? (
                    <input
                      type="text"
                      value={editName}
                      onChange={(e) => setEditName(e.target.value)}
                      className="input text-sm"
                      autoFocus
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') void handleSaveEdit();
                        if (e.key === 'Escape') handleCancelEdit();
                      }}
                    />
                  ) : (
                    <p className="font-medium text-text-primary">{passkey.name}</p>
                  )}
                  <p className="text-xs text-text-secondary">
                    Created {formatDate(passkey.createdAt)}
                    {passkey.lastUsedAt && ` • Last used ${formatDate(passkey.lastUsedAt)}`}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                {editingId === passkey.id ? (
                  <>
                    <button
                      onClick={() => { void handleSaveEdit(); }}
                      className="p-2 text-green-600 hover:bg-green-100 dark:hover:bg-green-900/30 rounded-lg"
                      title="Save"
                    >
                      <CheckIcon className="w-4 h-4" />
                    </button>
                    <button
                      onClick={handleCancelEdit}
                      className="p-2 text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700 rounded-lg"
                      title="Cancel"
                    >
                      <XIcon className="w-4 h-4" />
                    </button>
                  </>
                ) : (
                  <>
                    <button
                      onClick={() => handleStartEdit(passkey)}
                      className="p-2 text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700 rounded-lg"
                      title="Rename"
                    >
                      <EditIcon className="w-4 h-4" />
                    </button>
                    <button
                      onClick={() => { void handleDelete(passkey.id, passkey.name); }}
                      className="p-2 text-red-500 hover:bg-red-100 dark:hover:bg-red-900/30 rounded-lg"
                      title="Delete"
                    >
                      <TrashIcon className="w-4 h-4" />
                    </button>
                  </>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      <div className="mt-4 p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
        <p className="text-xs text-blue-800 dark:text-blue-200">
          <strong>Tip:</strong> Passkeys use your device's built-in security (Face ID, Touch ID, Windows Hello)
          to verify your identity. They're more secure than passwords.
        </p>
      </div>
    </div>
  );
}
