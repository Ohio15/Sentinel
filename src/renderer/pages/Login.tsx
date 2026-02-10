/**
 * Login Page - Web mode only
 * In Electron mode, this page is not used (auth is handled by main process)
 */
import { useState, useEffect, FormEvent } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { Loader2, Fingerprint } from 'lucide-react';
import { browserSupportsWebAuthn, startAuthentication } from '@simplewebauthn/browser';
import { useAuthStore } from '../stores/authStore';
import { Input } from '../components/ui';
import { api } from '../services/api';
import { connection } from '../services';
import toast from 'react-hot-toast';

export function Login() {
  const navigate = useNavigate();
  const { login, isLoading, error, clearError } = useAuthStore();
  const [identifier, setIdentifier] = useState('');
  const [password, setPassword] = useState('');
  const [passkeySupported, setPasskeySupported] = useState(false);
  const [passkeyLoading, setPasskeyLoading] = useState(false);
  const [passkeyError, setPasskeyError] = useState<string | null>(null);

  // Check if passkeys are supported
  useEffect(() => {
    if (browserSupportsWebAuthn()) {
      setPasskeySupported(true);
    }
  }, []);

  const handlePasskeyLogin = async () => {
    setPasskeyLoading(true);
    setPasskeyError(null);
    clearError();

    try {
      // Step 1: Begin authentication
      const beginResponse = await api!.beginPasskeyAuthentication();
      const { sessionId, options } = beginResponse;

      // go-webauthn returns { publicKey: { ... } }, but @simplewebauthn/browser expects just the publicKey contents
      const publicKeyOptions = (options as { publicKey?: unknown })?.publicKey || options;

      // Step 2: Prompt user for biometric authentication
      const authResponse = await startAuthentication(publicKeyOptions as Parameters<typeof startAuthentication>[0]);

      // Step 3: Complete authentication
      const result = await api!.finishPasskeyAuthentication({
        sessionId,
        response: authResponse,
      });

      // Step 4: Store tokens and update state
      const { accessToken, refreshToken, expiresIn, user } = result;
      const expiresAt = Date.now() + (expiresIn * 1000);

      localStorage.setItem('token', accessToken);
      localStorage.setItem('refreshToken', refreshToken);
      localStorage.setItem('tokenExpiresAt', expiresAt.toString());

      useAuthStore.setState({
        user: user as Parameters<typeof useAuthStore.setState>[0]['user'],
        token: accessToken,
        refreshToken,
        tokenExpiresAt: expiresAt,
        isAuthenticated: true,
        isLoading: false,
        error: null,
        _refreshAttempts: 0,
        _isRefreshing: false,
      });

      // Connect WebSocket
      connection.connect();

      toast.success('Welcome back!');
      navigate('/');
    } catch (err: unknown) {
      const error = err as Error & { name?: string };
      if (error.name === 'NotAllowedError') {
        setPasskeyError('Authentication was cancelled');
      } else if (error.name === 'InvalidStateError') {
        setPasskeyError('Invalid passkey state');
      } else {
        setPasskeyError(error.message || 'Passkey authentication failed');
      }
    } finally {
      setPasskeyLoading(false);
    }
  };

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    clearError();

    try {
      await login(identifier, password);
      toast.success('Welcome back!');

      // Check if user has passkeys set up, if not suggest adding one
      if (passkeySupported) {
        try {
          const passkeys = await api!.getPasskeys();
          if (!passkeys || passkeys.length === 0) {
            // Delay the suggestion toast so it doesn't overlap with welcome
            setTimeout(() => {
              toast((t) => (
                <div className="flex items-center gap-3">
                  <Fingerprint className="w-5 h-5 text-primary flex-shrink-0" />
                  <div>
                    <p className="font-medium">Enable faster sign-in</p>
                    <p className="text-sm text-gray-400">Set up a passkey in Settings → Security</p>
                  </div>
                  <button
                    onClick={() => toast.dismiss(t.id)}
                    className="text-gray-400 hover:text-white"
                  >
                    ✕
                  </button>
                </div>
              ), { duration: 8000 });
            }, 1500);
          }
        } catch {
          // Silently fail - don't block login for passkey check
        }
      }

      navigate('/');
    } catch {
      // Error is handled by the store
    }
  };

  return (
    <div className="min-h-screen bg-black flex items-center justify-center p-4 dark">
      <div className="w-full max-w-md">
        {/* Logo */}
        <div className="text-center mb-10">
          <div className="inline-flex items-center justify-center mb-6">
            <img src="/sentinel-logo.png" alt="Sentinel" className="h-64 w-auto" />
          </div>
          <h1 className="text-3xl font-bold text-white">Sentinel</h1>
          <p className="text-gray-400 mt-2">Remote Monitoring & Management</p>
        </div>

        {/* Login form */}
        <div className="bg-gray-900 rounded-xl shadow-lg border border-gray-800 p-6">
          <h2 className="text-xl font-semibold text-white mb-6">Sign in to your account</h2>

          {/* Passkey login button */}
          {passkeySupported && (
            <>
              <button
                type="button"
                onClick={handlePasskeyLogin}
                disabled={passkeyLoading || isLoading}
                className="w-full flex items-center justify-center gap-3 bg-gray-800 text-white py-3 rounded-lg font-medium hover:bg-gray-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed border border-gray-700"
              >
                {passkeyLoading ? (
                  <>
                    <Loader2 className="w-5 h-5 animate-spin" />
                    Authenticating...
                  </>
                ) : (
                  <>
                    <Fingerprint className="w-5 h-5" />
                    Sign in with Passkey
                  </>
                )}
              </button>

              {passkeyError && (
                <div className="mt-3 p-3 bg-red-900/50 border border-red-700 rounded-lg">
                  <p className="text-sm text-red-300">{passkeyError}</p>
                </div>
              )}

              <div className="relative my-6">
                <div className="absolute inset-0 flex items-center">
                  <div className="w-full border-t border-gray-700"></div>
                </div>
                <div className="relative flex justify-center text-sm">
                  <span className="px-2 bg-gray-900 text-gray-400">or continue with password</span>
                </div>
              </div>
            </>
          )}

          <form onSubmit={handleSubmit} className="space-y-4">
            <Input
              label="Username or Email"
              type="text"
              value={identifier}
              onChange={(e) => setIdentifier(e.target.value)}
              placeholder="username or email"
              required
              autoComplete="username"
              autoFocus
            />

            <Input
              label="Password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Enter your password"
              required
              autoComplete="current-password"
            />

            {error && (
              <div className="p-3 bg-red-900/50 border border-red-700 rounded-lg">
                <p className="text-sm text-red-300">{error}</p>
              </div>
            )}

            <button
              type="submit"
              disabled={isLoading}
              className="w-full flex items-center justify-center gap-2 bg-primary text-white py-2.5 rounded-lg font-medium hover:bg-primary-hover transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {isLoading ? (
                <>
                  <Loader2 className="w-4 h-4 animate-spin" />
                  Signing in...
                </>
              ) : (
                'Sign in'
              )}
            </button>
          </form>

          {/* Auth methods info */}
          <div className="mt-6 pt-4 border-t border-gray-800">
            <p className="text-xs text-gray-500 mb-3 text-center">Available sign-in methods</p>
            <div className="flex items-center justify-center gap-4">
              <div className="flex items-center gap-1.5 text-xs text-gray-400">
                <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z" />
                </svg>
                <span>Password</span>
              </div>
              {passkeySupported && (
                <>
                  <span className="text-gray-700">•</span>
                  <div className="flex items-center gap-1.5 text-xs text-gray-400">
                    <Fingerprint className="w-4 h-4" />
                    <span>Passkey</span>
                  </div>
                  <span className="text-gray-700">•</span>
                  <div className="flex items-center gap-1.5 text-xs text-gray-400">
                    <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 18h.01M8 21h8a2 2 0 002-2V5a2 2 0 00-2-2H8a2 2 0 00-2 2v14a2 2 0 002 2z" />
                    </svg>
                    <span>Phone (QR)</span>
                  </div>
                </>
              )}
            </div>
            {passkeySupported && (
              <p className="text-xs text-gray-600 text-center mt-2">
                Use a passkey from this device or scan QR with your phone
              </p>
            )}
          </div>

          {/* Invitation sign up */}
          <div className="mt-4 text-center">
            <p className="text-sm text-gray-400">
              Have an invitation?{' '}
              <Link to="/register" className="text-primary hover:text-primary-hover transition-colors">
                Create an account
              </Link>
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
