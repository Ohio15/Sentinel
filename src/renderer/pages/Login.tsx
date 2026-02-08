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
