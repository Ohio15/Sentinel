import { useState, FormEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import { Loader2 } from 'lucide-react';
import { useAuthStore } from '@/stores/authStore';
import { Input } from '@/components/ui';
import toast from 'react-hot-toast';

export function Login() {
  const navigate = useNavigate();
  const { login, isLoading, error, clearError } = useAuthStore();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    clearError();

    try {
      await login(email, password);
      toast.success('Welcome back!');
      navigate('/');
    } catch {
      // Error is handled by the store
    }
  };

  return (
    <div className="min-h-screen bg-black flex items-center justify-center p-4">
      <div className="w-full max-w-md">
        {/* Logo - large on black background */}
        <div className="text-center mb-10">
          <div className="inline-flex items-center justify-center w-48 h-48 mb-6">
            <img src="/sentinel.svg" alt="Sentinel" className="w-full h-full" />
          </div>
          <h1 className="text-3xl font-bold text-white">Sentinel</h1>
          <p className="text-gray-400 mt-2">Remote Monitoring & Management</p>
        </div>

        {/* Login form */}
        <div className="bg-gray-900 rounded-xl shadow-lg border border-gray-800 p-6">
          <h2 className="text-xl font-semibold text-white mb-6">Sign in to your account</h2>

          <form onSubmit={handleSubmit} className="space-y-4">
            <Input
              label="Email address"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="admin@example.com"
              required
              autoComplete="email"
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

          <div className="mt-6 pt-6 border-t border-gray-700">
            <p className="text-sm text-gray-400 text-center">
              Default credentials: <code className="bg-gray-800 px-1.5 py-0.5 rounded text-gray-300">admin@sentinel.local</code> /{' '}
              <code className="bg-gray-800 px-1.5 py-0.5 rounded text-gray-300">admin</code>
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
