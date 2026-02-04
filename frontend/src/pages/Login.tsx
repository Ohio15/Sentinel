import { useState, useEffect, FormEvent } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { Loader2, Fingerprint } from 'lucide-react';
import { browserSupportsWebAuthn } from '@simplewebauthn/browser';
import { useAuthStore } from '@/stores/authStore';
import { Input } from '@/components/ui';
import toast from 'react-hot-toast';

export function Login() {
  const navigate = useNavigate();
  const { login, loginWithPasskey, isLoading, error, clearError } = useAuthStore();
  const [identifier, setIdentifier] = useState('');
  const [password, setPassword] = useState('');
  const [passkeySupported, setPasskeySupported] = useState(false);
  const [isPasskeyLoading, setIsPasskeyLoading] = useState(false);

  useEffect(() => {
    // Check if browser supports WebAuthn/passkeys
    setPasskeySupported(browserSupportsWebAuthn());
  }, []);

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

  const handlePasskeyLogin = async () => {
    clearError();
    setIsPasskeyLoading(true);

    try {
      await loginWithPasskey();
      toast.success('Welcome back!');
      navigate('/');
    } catch (err: unknown) {
      const error = err as { name?: string };
      // Don't show error toast for user cancellation
      if (error.name !== 'NotAllowedError') {
        toast.error('Passkey authentication failed');
      }
    } finally {
      setIsPasskeyLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-black flex items-center justify-center p-4">
      <div className="w-full max-w-md">
        {/* Logo - large on black background */}
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

          {/* Passkey login button - shown first if supported */}
          {passkeySupported && (
            <>
              <button
                type="button"
                onClick={handlePasskeyLogin}
                disabled={isLoading || isPasskeyLoading}
                className="w-full flex items-center justify-center gap-2 bg-gray-800 text-white py-2.5 rounded-lg font-medium border border-gray-700 hover:bg-gray-700 hover:border-gray-600 transition-colors disabled:opacity-50 disabled:cursor-not-allowed mb-4"
              >
                {isPasskeyLoading ? (
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

              <div className="relative my-6">
                <div className="absolute inset-0 flex items-center">
                  <div className="w-full border-t border-gray-700"></div>
                </div>
                <div className="relative flex justify-center text-sm">
                  <span className="px-2 bg-gray-900 text-gray-500">or continue with password</span>
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
              autoFocus={!passkeySupported}
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
              disabled={isLoading || isPasskeyLoading}
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
