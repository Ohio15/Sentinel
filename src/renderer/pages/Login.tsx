/**
 * Login Page - Web mode only
 * Provides user-selectable authentication methods:
 * 1. Password - Traditional username/password
 * 2. This Device - Passkey with PIN/biometric on current device
 * 3. Phone - Scan QR code with phone's passkey
 */
import { useState, useEffect, FormEvent } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { Loader2, Fingerprint, Smartphone, Key, ArrowLeft } from 'lucide-react';
import { browserSupportsWebAuthn, startAuthentication } from '@simplewebauthn/browser';
import { useAuthStore } from '../stores/authStore';
import { Input } from '../components/ui';
import { api } from '../services/api';
import { connection } from '../services';
import toast from 'react-hot-toast';

type AuthMethod = 'select' | 'password' | 'passkey-device' | 'passkey-phone';

export function Login() {
  const navigate = useNavigate();
  const { login, isLoading, error, clearError } = useAuthStore();
  const [authMethod, setAuthMethod] = useState<AuthMethod>('select');
  const [identifier, setIdentifier] = useState('');
  const [password, setPassword] = useState('');
  const [passkeySupported, setPasskeySupported] = useState(false);
  const [passkeyLoading, setPasskeyLoading] = useState(false);
  const [passkeyError, setPasskeyError] = useState<string | null>(null);
  const [attemptCount, setAttemptCount] = useState(0);
  const [lockoutUntil, setLockoutUntil] = useState<number | null>(null);

  // Check if passkeys are supported
  useEffect(() => {
    if (browserSupportsWebAuthn()) {
      setPasskeySupported(true);
    }
  }, []);

  const handlePasskeyLogin = async (preferHybrid: boolean = false) => {
    setPasskeyLoading(true);
    setPasskeyError(null);
    clearError();

    try {
      // Step 1: Begin authentication
      const beginResponse = await api!.beginPasskeyAuthentication();
      const { sessionId, options } = beginResponse;

      // go-webauthn returns { publicKey: { ... } }, but @simplewebauthn/browser expects just the publicKey contents
      const publicKeyOptions = (options as { publicKey?: Record<string, unknown> })?.publicKey || options;

      // Add hints for preferred transport method
      const authOptions = {
        ...publicKeyOptions,
        // For phone/QR, hint that we prefer hybrid (cross-device) transport
        ...(preferHybrid && { hints: ['hybrid'] }),
      } as Parameters<typeof startAuthentication>[0];

      // Step 2: Prompt user for authentication
      const authResponse = await startAuthentication(authOptions);

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
      } else if (error.name === 'NotSupportedError') {
        setPasskeyError('This authentication method is not supported on your device');
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

    // Client-side rate limiting
    if (lockoutUntil && Date.now() < lockoutUntil) {
      const seconds = Math.ceil((lockoutUntil - Date.now()) / 1000);
      useAuthStore.setState({ error: `Too many attempts. Try again in ${seconds} seconds.` });
      return;
    }

    try {
      await login(identifier, password);
      // Reset rate limiting on success
      setAttemptCount(0);
      setLockoutUntil(null);
      toast.success('Welcome back!');

      // Check if user has passkeys set up, if not suggest adding one
      if (passkeySupported) {
        try {
          const passkeys = await api!.getPasskeys();
          if (!passkeys || passkeys.length === 0) {
            setTimeout(() => {
              toast((t) => (
                <div className="flex items-center gap-3">
                  <Fingerprint className="w-5 h-5 text-primary flex-shrink-0" />
                  <div>
                    <p className="font-medium">Enable faster sign-in</p>
                    <p className="text-sm text-gray-400">Set up a passkey in Settings</p>
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
          // Silently fail
        }
      }

      navigate('/');
    } catch {
      // Error is handled by the store — track failed attempts for rate limiting
      const newCount = attemptCount + 1;
      setAttemptCount(newCount);
      if (newCount >= 5) {
        setLockoutUntil(Date.now() + 60000); // 1 minute lockout
        setAttemptCount(0);
        useAuthStore.setState({ error: 'Too many failed attempts. Please wait 60 seconds before trying again.' });
      }
    }
  };

  const handleBack = () => {
    setAuthMethod('select');
    setPasskeyError(null);
    clearError();
  };

  // Method selection screen
  const renderMethodSelection = () => (
    <div className="space-y-3">
      <h2 className="text-xl font-semibold text-white mb-6 text-center">Choose sign-in method</h2>

      {/* Password option */}
      <button
        type="button"
        onClick={() => setAuthMethod('password')}
        className="w-full flex items-center gap-4 p-4 bg-gray-800 hover:bg-gray-750 border border-gray-700 hover:border-gray-600 rounded-lg transition-all group"
      >
        <div className="p-2.5 bg-gray-700 rounded-lg group-hover:bg-gray-600 transition-colors">
          <Key className="w-5 h-5 text-gray-300" />
        </div>
        <div className="text-left">
          <p className="font-medium text-white">Password</p>
          <p className="text-sm text-gray-400">Sign in with username and password</p>
        </div>
      </button>

      {/* Passkey - This Device */}
      {passkeySupported && (
        <button
          type="button"
          onClick={() => {
            setAuthMethod('passkey-device');
            handlePasskeyLogin(false);
          }}
          disabled={passkeyLoading}
          className="w-full flex items-center gap-4 p-4 bg-gray-800 hover:bg-gray-750 border border-gray-700 hover:border-gray-600 rounded-lg transition-all group disabled:opacity-50"
        >
          <div className="p-2.5 bg-primary/20 rounded-lg group-hover:bg-primary/30 transition-colors">
            <Fingerprint className="w-5 h-5 text-primary" />
          </div>
          <div className="text-left flex-1">
            <p className="font-medium text-white">This Device</p>
            <p className="text-sm text-gray-400">Use PIN or biometric on this device</p>
          </div>
          {passkeyLoading && authMethod === 'passkey-device' && (
            <Loader2 className="w-5 h-5 animate-spin text-primary" />
          )}
        </button>
      )}

      {/* Passkey - Phone QR */}
      {passkeySupported && (
        <button
          type="button"
          onClick={() => {
            setAuthMethod('passkey-phone');
            handlePasskeyLogin(true);
          }}
          disabled={passkeyLoading}
          className="w-full flex items-center gap-4 p-4 bg-gray-800 hover:bg-gray-750 border border-gray-700 hover:border-gray-600 rounded-lg transition-all group disabled:opacity-50"
        >
          <div className="p-2.5 bg-blue-500/20 rounded-lg group-hover:bg-blue-500/30 transition-colors">
            <Smartphone className="w-5 h-5 text-blue-400" />
          </div>
          <div className="text-left flex-1">
            <p className="font-medium text-white">Phone</p>
            <p className="text-sm text-gray-400">Scan QR code with your phone's passkey</p>
          </div>
          {passkeyLoading && authMethod === 'passkey-phone' && (
            <Loader2 className="w-5 h-5 animate-spin text-blue-400" />
          )}
        </button>
      )}

      {/* Error display */}
      {passkeyError && (
        <div className="p-3 bg-red-900/50 border border-red-700 rounded-lg">
          <p className="text-sm text-red-300">{passkeyError}</p>
        </div>
      )}

      {/* Passkey info */}
      {passkeySupported && (
        <div className="mt-4 p-3 bg-gray-800/50 rounded-lg border border-gray-700/50">
          <p className="text-xs text-gray-500">
            <strong className="text-gray-400">This Device</strong> uses the passkey stored on this computer.
            <br />
            <strong className="text-gray-400">Phone</strong> uses the passkey stored on your mobile device.
          </p>
        </div>
      )}
    </div>
  );

  // Password form
  const renderPasswordForm = () => (
    <>
      <div className="flex items-center gap-3 mb-6">
        <button
          type="button"
          onClick={handleBack}
          className="p-1.5 hover:bg-gray-800 rounded-lg transition-colors"
        >
          <ArrowLeft className="w-5 h-5 text-gray-400" />
        </button>
        <h2 className="text-xl font-semibold text-white">Sign in with password</h2>
      </div>

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
    </>
  );

  // Passkey loading/waiting screen
  const renderPasskeyWaiting = () => (
    <>
      <div className="flex items-center gap-3 mb-6">
        <button
          type="button"
          onClick={handleBack}
          disabled={passkeyLoading}
          className="p-1.5 hover:bg-gray-800 rounded-lg transition-colors disabled:opacity-50"
        >
          <ArrowLeft className="w-5 h-5 text-gray-400" />
        </button>
        <h2 className="text-xl font-semibold text-white">
          {authMethod === 'passkey-device' ? 'Authenticate with this device' : 'Scan with your phone'}
        </h2>
      </div>

      <div className="text-center py-8">
        {passkeyLoading ? (
          <>
            <div className="inline-flex items-center justify-center w-16 h-16 bg-gray-800 rounded-full mb-4">
              {authMethod === 'passkey-device' ? (
                <Fingerprint className="w-8 h-8 text-primary animate-pulse" />
              ) : (
                <Smartphone className="w-8 h-8 text-blue-400 animate-pulse" />
              )}
            </div>
            <p className="text-gray-300 mb-2">
              {authMethod === 'passkey-device'
                ? 'Complete authentication on your device...'
                : 'Look for the QR code option...'}
            </p>
            <p className="text-sm text-gray-500">
              {authMethod === 'passkey-device'
                ? 'Enter your PIN or use biometric to confirm'
                : 'If you see a PIN prompt, look for "Use a different device" or "Use your phone" option'}
            </p>
            {authMethod === 'passkey-phone' && (
              <div className="mt-4 p-3 bg-blue-900/20 border border-blue-800/50 rounded-lg text-left">
                <p className="text-xs text-blue-300 font-medium mb-1">Tip: Using 1Password?</p>
                <p className="text-xs text-blue-200/70">
                  In the browser dialog, click the small link that says "Use a phone or tablet"
                  or "Use a different device" to show the QR code.
                </p>
              </div>
            )}
          </>
        ) : passkeyError ? (
          <>
            <div className="inline-flex items-center justify-center w-16 h-16 bg-red-900/30 rounded-full mb-4">
              <svg className="w-8 h-8 text-red-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </div>
            <p className="text-red-300 mb-4">{passkeyError}</p>
            <button
              type="button"
              onClick={() => handlePasskeyLogin(authMethod === 'passkey-phone')}
              className="px-4 py-2 bg-gray-800 text-white rounded-lg hover:bg-gray-700 transition-colors"
            >
              Try again
            </button>
          </>
        ) : null}
      </div>
    </>
  );

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

        {/* Login card */}
        <div className="bg-gray-900 rounded-xl shadow-lg border border-gray-800 p-6">
          {authMethod === 'select' && renderMethodSelection()}
          {authMethod === 'password' && renderPasswordForm()}
          {(authMethod === 'passkey-device' || authMethod === 'passkey-phone') && renderPasskeyWaiting()}

          {/* Invitation sign up */}
          <div className="mt-6 pt-4 border-t border-gray-800 text-center">
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

export default Login;
