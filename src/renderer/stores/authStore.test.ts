import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useAuthStore } from './authStore';

// Mock the services module
vi.mock('../services', () => ({
  auth: {
    login: vi.fn(),
    logout: vi.fn().mockResolvedValue(undefined),
    getCurrentUser: vi.fn(),
  },
  connection: {
    connect: vi.fn(),
    disconnect: vi.fn(),
  },
}));

// Mock the api module
vi.mock('../services/api', () => ({
  api: {
    refreshToken: vi.fn(),
  },
}));

import { auth, connection } from '../services';
import { api } from '../services/api';

describe('authStore', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });

    // Reset the store to a clean state
    useAuthStore.setState({
      user: null,
      token: null,
      refreshToken: null,
      tokenExpiresAt: null,
      isAuthenticated: false,
      isLoading: false,
      error: null,
      _authChecked: false,
      _refreshTimer: null,
      _refreshAttempts: 0,
      _isRefreshing: false,
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('login stores token and sets authenticated on success', async () => {
    const mockUser = { id: '1', username: 'admin', email: 'admin@test.com', role: 'admin' };
    const mockResponse = {
      accessToken: 'jwt-access-token-123',
      refreshToken: 'jwt-refresh-token-456',
      expiresIn: 3600,
      user: mockUser,
    };

    vi.mocked(auth.login).mockResolvedValue(mockResponse);

    await useAuthStore.getState().login('admin', 'password123');

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(true);
    expect(state.token).toBe('jwt-access-token-123');
    expect(state.refreshToken).toBe('jwt-refresh-token-456');
    expect(state.user).toEqual(mockUser);
    expect(state.isLoading).toBe(false);
    expect(state.error).toBeNull();

    expect(localStorage.getItem('token')).toBe('jwt-access-token-123');
    expect(localStorage.getItem('refreshToken')).toBe('jwt-refresh-token-456');
    expect(localStorage.getItem('tokenExpiresAt')).toBeTruthy();

    expect(connection.connect).toHaveBeenCalled();
  });

  it('login sets error state on failure', async () => {
    vi.mocked(auth.login).mockRejectedValue(new Error('Invalid credentials'));

    await expect(
      useAuthStore.getState().login('admin', 'wrong')
    ).rejects.toThrow('Invalid credentials');

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
    expect(state.token).toBeNull();
    expect(state.user).toBeNull();
    expect(state.error).toBe('Invalid credentials');
    expect(state.isLoading).toBe(false);
  });

  it('login sets isLoading to true during request', async () => {
    let resolveLogin: (value: unknown) => void;
    const loginPromise = new Promise((resolve) => { resolveLogin = resolve; });
    vi.mocked(auth.login).mockReturnValue(loginPromise as ReturnType<typeof auth.login>);

    const loginCall = useAuthStore.getState().login('admin', 'pass');

    // While the request is in-flight, isLoading should be true
    expect(useAuthStore.getState().isLoading).toBe(true);

    resolveLogin!({
      accessToken: 'tok',
      refreshToken: 'ref',
      expiresIn: 3600,
      user: { id: '1', username: 'a', email: 'a@a.com', role: 'admin' },
    });

    await loginCall;
    expect(useAuthStore.getState().isLoading).toBe(false);
  });

  it('logout clears all state and storage', () => {
    // Set up authenticated state
    localStorage.setItem('token', 'some-token');
    localStorage.setItem('refreshToken', 'some-refresh');
    localStorage.setItem('tokenExpiresAt', '9999999999999');
    localStorage.setItem('user', '{"id":"1"}');
    localStorage.setItem('auth-storage', '{}');

    useAuthStore.setState({
      user: { id: '1', username: 'admin', email: 'a@a.com', role: 'admin' },
      token: 'some-token',
      refreshToken: 'some-refresh',
      tokenExpiresAt: 9999999999999,
      isAuthenticated: true,
      isLoading: false,
      _refreshAttempts: 1,
      _isRefreshing: true,
    });

    useAuthStore.getState().logout();

    const state = useAuthStore.getState();
    expect(state.user).toBeNull();
    expect(state.token).toBeNull();
    expect(state.refreshToken).toBeNull();
    expect(state.tokenExpiresAt).toBeNull();
    expect(state.isAuthenticated).toBe(false);
    expect(state.isLoading).toBe(false);
    expect(state.error).toBeNull();
    expect(state._refreshAttempts).toBe(0);
    expect(state._isRefreshing).toBe(false);

    expect(localStorage.getItem('token')).toBeNull();
    expect(localStorage.getItem('refreshToken')).toBeNull();
    expect(localStorage.getItem('tokenExpiresAt')).toBeNull();
    expect(localStorage.getItem('user')).toBeNull();
    expect(localStorage.getItem('auth-storage')).toBeNull();
  });

  it('logout calls auth.logout service', () => {
    useAuthStore.getState().logout();
    expect(auth.logout).toHaveBeenCalled();
  });

  it('checkAuth returns false with no token in localStorage', async () => {
    localStorage.removeItem('token');

    await useAuthStore.getState().checkAuth();

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
    expect(state.isLoading).toBe(false);
    expect(state._authChecked).toBe(true);
  });

  it('checkAuth validates token and sets user on success', async () => {
    const mockUser = { id: '1', username: 'admin', email: 'admin@test.com', role: 'admin' };
    localStorage.setItem('token', 'valid-token');
    localStorage.setItem('tokenExpiresAt', (Date.now() + 3600000).toString());

    vi.mocked(auth.getCurrentUser).mockResolvedValue(mockUser);

    await useAuthStore.getState().checkAuth();

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(true);
    expect(state.user).toEqual(mockUser);
    expect(state.isLoading).toBe(false);
    expect(state._authChecked).toBe(true);
    expect(connection.connect).toHaveBeenCalled();
  });

  it('checkAuth clears state when token validation fails', async () => {
    localStorage.setItem('token', 'invalid-token');

    vi.mocked(auth.getCurrentUser).mockRejectedValue(new Error('Unauthorized'));

    await useAuthStore.getState().checkAuth();

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
    expect(state.user).toBeNull();
    expect(state.token).toBeNull();
    expect(state.isLoading).toBe(false);
    expect(state._authChecked).toBe(true);
    expect(localStorage.getItem('token')).toBeNull();
  });

  it('checkAuth prevents duplicate calls via _authChecked flag', async () => {
    localStorage.setItem('token', 'valid-token');
    vi.mocked(auth.getCurrentUser).mockResolvedValue({ id: '1', username: 'a', email: 'a@a.com', role: 'admin' });

    await useAuthStore.getState().checkAuth();
    expect(auth.getCurrentUser).toHaveBeenCalledTimes(1);

    // Second call should be skipped
    await useAuthStore.getState().checkAuth();
    expect(auth.getCurrentUser).toHaveBeenCalledTimes(1);
  });

  it('refreshAccessToken updates token on success', async () => {
    useAuthStore.setState({
      refreshToken: 'refresh-token-123',
      _refreshAttempts: 0,
      _isRefreshing: false,
    });

    vi.mocked(api.refreshToken).mockResolvedValue({
      accessToken: 'new-access-token',
      expiresIn: 7200,
    });

    const result = await useAuthStore.getState().refreshAccessToken();

    expect(result).toBe(true);
    const state = useAuthStore.getState();
    expect(state.token).toBe('new-access-token');
    expect(state._isRefreshing).toBe(false);
    expect(state._refreshAttempts).toBe(0); // Reset on success
    expect(localStorage.getItem('token')).toBe('new-access-token');
    expect(localStorage.getItem('tokenExpiresAt')).toBeTruthy();
  });

  it('refreshAccessToken triggers logout on failure', async () => {
    useAuthStore.setState({
      refreshToken: 'refresh-token-123',
      _refreshAttempts: 0,
      _isRefreshing: false,
      isAuthenticated: true,
      token: 'old-token',
    });

    vi.mocked(api.refreshToken).mockRejectedValue(new Error('Token expired'));

    const result = await useAuthStore.getState().refreshAccessToken();

    expect(result).toBe(false);
    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
    expect(state.token).toBeNull();
    expect(state.user).toBeNull();
  });

  it('refreshAccessToken returns false with no refresh token', async () => {
    useAuthStore.setState({
      refreshToken: null,
      _refreshAttempts: 0,
      _isRefreshing: false,
    });

    const result = await useAuthStore.getState().refreshAccessToken();

    expect(result).toBe(false);
    expect(api.refreshToken).not.toHaveBeenCalled();
  });

  it('refreshAccessToken prevents concurrent refresh attempts', async () => {
    useAuthStore.setState({
      refreshToken: 'refresh-token-123',
      _refreshAttempts: 0,
      _isRefreshing: true, // Already refreshing
    });

    const result = await useAuthStore.getState().refreshAccessToken();

    expect(result).toBe(false);
    expect(api.refreshToken).not.toHaveBeenCalled();
  });

  it('refreshAccessToken enforces max 2 attempts then logs out', async () => {
    useAuthStore.setState({
      refreshToken: 'refresh-token-123',
      _refreshAttempts: 2, // Already at max
      _isRefreshing: false,
      isAuthenticated: true,
    });

    const result = await useAuthStore.getState().refreshAccessToken();

    expect(result).toBe(false);
    expect(api.refreshToken).not.toHaveBeenCalled();
    // Should have triggered logout
    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
  });

  it('clearError resets the error state', () => {
    useAuthStore.setState({ error: 'Some error occurred' });
    expect(useAuthStore.getState().error).toBe('Some error occurred');

    useAuthStore.getState().clearError();
    expect(useAuthStore.getState().error).toBeNull();
  });

  it('_scheduleTokenRefresh sets up a timer for auto-refresh', async () => {
    useAuthStore.setState({
      refreshToken: 'refresh-token-123',
      _refreshAttempts: 0,
      _isRefreshing: false,
    });

    vi.mocked(api.refreshToken).mockResolvedValue({
      accessToken: 'refreshed-token',
      expiresIn: 3600,
    });

    // Schedule a refresh for a token expiring in 600 seconds (10 min)
    // Should refresh at 80% of lifetime = 480s or expiresIn-300 = 300s, whichever is smaller => 300s
    useAuthStore.getState()._scheduleTokenRefresh(600);

    expect(useAuthStore.getState()._refreshTimer).not.toBeNull();

    // Advance time to trigger the refresh
    await vi.advanceTimersByTimeAsync(300 * 1000 + 100);

    expect(api.refreshToken).toHaveBeenCalledWith('refresh-token-123');
  });

  it('checkAuth attempts refresh when token is expired and refresh token exists', async () => {
    const expiredTime = (Date.now() - 60000).toString(); // Expired 1 minute ago
    localStorage.setItem('token', 'expired-token');
    localStorage.setItem('refreshToken', 'valid-refresh');
    localStorage.setItem('tokenExpiresAt', expiredTime);

    vi.mocked(api.refreshToken).mockResolvedValue({
      accessToken: 'new-token-after-refresh',
      expiresIn: 3600,
    });

    const mockUser = { id: '1', username: 'admin', email: 'a@a.com', role: 'admin' };
    vi.mocked(auth.getCurrentUser).mockResolvedValue(mockUser);

    await useAuthStore.getState().checkAuth();

    expect(api.refreshToken).toHaveBeenCalledWith('valid-refresh');
  });
});
