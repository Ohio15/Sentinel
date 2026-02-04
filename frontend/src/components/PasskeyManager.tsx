import { useState, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Fingerprint,
  Plus,
  Trash2,
  Edit2,
  Check,
  X,
  Loader2,
  AlertTriangle,
  Key,
} from 'lucide-react';
import { browserSupportsWebAuthn, startRegistration } from '@simplewebauthn/browser';
import { Button, Card, CardContent } from '@/components/ui';
import api from '@/services/api';
import toast from 'react-hot-toast';

interface Passkey {
  id: string;
  name: string;
  createdAt: string;
  lastUsedAt?: string;
}

export function PasskeyManager() {
  const queryClient = useQueryClient();
  const [isSupported, setIsSupported] = useState<boolean | null>(null);
  const [isRegistering, setIsRegistering] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editName, setEditName] = useState('');

  useEffect(() => {
    setIsSupported(browserSupportsWebAuthn());
  }, []);

  const { data: passkeys = [], isLoading } = useQuery<Passkey[]>({
    queryKey: ['passkeys'],
    queryFn: () => api.getPasskeys(),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deletePasskey(id),
    onSuccess: () => {
      toast.success('Passkey deleted');
      queryClient.invalidateQueries({ queryKey: ['passkeys'] });
    },
    onError: () => {
      toast.error('Failed to delete passkey');
    },
  });

  const renameMutation = useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) => api.renamePasskey(id, name),
    onSuccess: () => {
      toast.success('Passkey renamed');
      queryClient.invalidateQueries({ queryKey: ['passkeys'] });
      setEditingId(null);
    },
    onError: () => {
      toast.error('Failed to rename passkey');
    },
  });

  const handleAddPasskey = async () => {
    setIsRegistering(true);

    try {
      // Step 1: Begin registration - get challenge from server
      const beginResponse = await api.beginPasskeyRegistration();
      const { sessionId, options } = beginResponse;

      // Step 2: Prompt user for biometric/passkey creation
      const registrationResponse = await startRegistration(options);

      // Step 3: Send response to server for verification and storage
      await api.finishPasskeyRegistration({
        sessionId,
        response: registrationResponse,
        name: `Passkey ${new Date().toLocaleDateString()}`,
      });

      toast.success('Passkey registered successfully');
      queryClient.invalidateQueries({ queryKey: ['passkeys'] });
    } catch (err: unknown) {
      const error = err as { name?: string; response?: { data?: { error?: string } }; message?: string };

      // Handle user cancellation gracefully
      if (error.name === 'NotAllowedError') {
        toast.error('Passkey registration was cancelled');
      } else if (error.name === 'InvalidStateError') {
        toast.error('This passkey is already registered');
      } else {
        toast.error(error.response?.data?.error || error.message || 'Failed to register passkey');
      }
    } finally {
      setIsRegistering(false);
    }
  };

  const handleDelete = (id: string, name: string) => {
    if (window.confirm(`Are you sure you want to delete "${name}"? This cannot be undone.`)) {
      deleteMutation.mutate(id);
    }
  };

  const handleStartEdit = (passkey: Passkey) => {
    setEditingId(passkey.id);
    setEditName(passkey.name);
  };

  const handleSaveEdit = () => {
    if (editingId && editName.trim()) {
      renameMutation.mutate({ id: editingId, name: editName.trim() });
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
      <Card>
        <CardContent>
          <div className="flex items-center gap-3 mb-4">
            <div className="p-2 bg-yellow-100 text-yellow-600 dark:bg-yellow-900/30 dark:text-yellow-400 rounded-lg">
              <AlertTriangle className="w-5 h-5" />
            </div>
            <div>
              <h3 className="text-lg font-semibold text-text-primary">Passkeys Not Supported</h3>
              <p className="text-sm text-text-secondary">
                Your browser does not support passkeys. Try using a modern browser like Chrome, Safari, or Edge.
              </p>
            </div>
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardContent>
        <div className="flex items-center justify-between mb-6">
          <div className="flex items-center gap-3">
            <div className="p-2 bg-indigo-100 text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-400 rounded-lg">
              <Key className="w-5 h-5" />
            </div>
            <div>
              <h3 className="text-lg font-semibold text-text-primary">Passkeys</h3>
              <p className="text-sm text-text-secondary">
                Sign in without a password using biometrics or security keys
              </p>
            </div>
          </div>
          <Button
            onClick={handleAddPasskey}
            disabled={isRegistering}
            size="sm"
          >
            {isRegistering ? (
              <>
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                Registering...
              </>
            ) : (
              <>
                <Plus className="w-4 h-4 mr-2" />
                Add Passkey
              </>
            )}
          </Button>
        </div>

        {isLoading ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="w-6 h-6 animate-spin text-primary" />
          </div>
        ) : passkeys.length === 0 ? (
          <div className="text-center py-8">
            <Fingerprint className="w-12 h-12 mx-auto text-gray-400 mb-3" />
            <p className="text-text-secondary mb-2">No passkeys registered</p>
            <p className="text-sm text-text-secondary">
              Add a passkey to sign in faster and more securely using your device's biometrics or a security key.
            </p>
          </div>
        ) : (
          <div className="space-y-3">
            {passkeys.map((passkey) => (
              <div
                key={passkey.id}
                className="flex items-center justify-between p-4 bg-gray-50 dark:bg-gray-800/50 rounded-lg border border-gray-200 dark:border-gray-700"
              >
                <div className="flex items-center gap-3">
                  <Fingerprint className="w-5 h-5 text-primary" />
                  <div>
                    {editingId === passkey.id ? (
                      <input
                        type="text"
                        value={editName}
                        onChange={(e) => setEditName(e.target.value)}
                        className="px-2 py-1 text-sm border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
                        autoFocus
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') handleSaveEdit();
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
                        onClick={handleSaveEdit}
                        disabled={renameMutation.isPending}
                        className="p-2 text-green-600 hover:bg-green-100 dark:hover:bg-green-900/30 rounded-lg transition-colors"
                        title="Save"
                      >
                        {renameMutation.isPending ? (
                          <Loader2 className="w-4 h-4 animate-spin" />
                        ) : (
                          <Check className="w-4 h-4" />
                        )}
                      </button>
                      <button
                        onClick={handleCancelEdit}
                        className="p-2 text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700 rounded-lg transition-colors"
                        title="Cancel"
                      >
                        <X className="w-4 h-4" />
                      </button>
                    </>
                  ) : (
                    <>
                      <button
                        onClick={() => handleStartEdit(passkey)}
                        className="p-2 text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700 rounded-lg transition-colors"
                        title="Rename"
                      >
                        <Edit2 className="w-4 h-4" />
                      </button>
                      <button
                        onClick={() => handleDelete(passkey.id, passkey.name)}
                        disabled={deleteMutation.isPending}
                        className="p-2 text-red-500 hover:bg-red-100 dark:hover:bg-red-900/30 rounded-lg transition-colors"
                        title="Delete"
                      >
                        {deleteMutation.isPending ? (
                          <Loader2 className="w-4 h-4 animate-spin" />
                        ) : (
                          <Trash2 className="w-4 h-4" />
                        )}
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
            <strong>Tip:</strong> Passkeys use your device's built-in security (Face ID, Touch ID, Windows Hello, or a security key)
            to verify your identity. They're more secure than passwords and can't be phished.
          </p>
        </div>
      </CardContent>
    </Card>
  );
}
