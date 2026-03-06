import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock the env module before importing api
vi.mock('./env', () => ({
  getApiBaseUrl: () => 'http://localhost:3000/api',
}));

// We need to re-import fresh for each test because ApiService is a class instance
// Instead, we test the singleton exported instance
import { api } from './api';

describe('ApiService', () => {
  const mockFetch = vi.fn();

  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    global.fetch = mockFetch;

    // Reset _isRefreshing state by accessing private field
    (api as any)._isRefreshing = false;

    // Default: mock a valid JSON response
    mockFetch.mockResolvedValue({
      ok: true,
      status: 200,
      text: () => Promise.resolve(JSON.stringify({ success: true })),
      json: () => Promise.resolve({ success: true }),
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('attaches Bearer token from localStorage to requests', async () => {
    localStorage.setItem('token', 'my-jwt-token');

    await api.getDevices();

    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining('/devices'),
      expect.objectContaining({
        headers: expect.objectContaining({
          Authorization: 'Bearer my-jwt-token',
        }),
      })
    );
  });

  it('does not attach Authorization header when no token exists', async () => {
    localStorage.removeItem('token');

    await api.getDevices();

    const callHeaders = mockFetch.mock.calls[0][1].headers;
    expect(callHeaders.Authorization).toBeUndefined();
  });

  it('attaches CSRF token for POST requests', async () => {
    // Set csrf cookie
    Object.defineProperty(document, 'cookie', {
      writable: true,
      value: 'csrf_token=abc123; other=value',
    });

    localStorage.setItem('token', 'test-token');

    await api.login('admin', 'password');

    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining('/auth/login'),
      expect.objectContaining({
        headers: expect.objectContaining({
          'X-CSRF-Token': 'abc123',
        }),
      })
    );

    // Clean up
    Object.defineProperty(document, 'cookie', { writable: true, value: '' });
  });

  it('does not attach CSRF token for GET requests', async () => {
    Object.defineProperty(document, 'cookie', {
      writable: true,
      value: 'csrf_token=abc123',
    });

    await api.getDevices();

    const callHeaders = mockFetch.mock.calls[0][1].headers;
    expect(callHeaders['X-CSRF-Token']).toBeUndefined();

    Object.defineProperty(document, 'cookie', { writable: true, value: '' });
  });

  it('sends requests with credentials: include', async () => {
    await api.getDevices();

    expect(mockFetch).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({
        credentials: 'include',
      })
    );
  });

  it('throws error with message from response on non-ok response', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      status: 400,
      json: () => Promise.resolve({ message: 'Bad Request: invalid params' }),
      text: () => Promise.resolve(''),
    });

    await expect(api.getDevices()).rejects.toThrow('Bad Request: invalid params');
  });

  it('handles 401 on non-auth endpoint by attempting token refresh', async () => {
    localStorage.setItem('token', 'expired-token');
    localStorage.setItem('refreshToken', 'valid-refresh-token');

    // First call returns 401
    // Second call (refresh) returns success
    // Third call (retry) returns success
    let callCount = 0;
    mockFetch.mockImplementation((url: string) => {
      callCount++;
      if (callCount === 1) {
        // Original request - 401
        return Promise.resolve({
          ok: false,
          status: 401,
          json: () => Promise.resolve({ message: 'Unauthorized' }),
          text: () => Promise.resolve(''),
        });
      }
      if (callCount === 2) {
        // Refresh request
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () => Promise.resolve({ accessToken: 'new-token', expiresIn: 3600 }),
          text: () => Promise.resolve(JSON.stringify({ accessToken: 'new-token', expiresIn: 3600 })),
        });
      }
      // Retry request
      return Promise.resolve({
        ok: true,
        status: 200,
        text: () => Promise.resolve(JSON.stringify({ devices: [] })),
        json: () => Promise.resolve({ devices: [] }),
      });
    });

    // Mock window.location for the 401 handler
    const originalHref = window.location.href;
    Object.defineProperty(window, 'location', {
      writable: true,
      value: { ...window.location, href: originalHref, pathname: '/dashboard' },
    });

    const result = await api.getDevices();

    // The refresh endpoint should have been called
    expect(mockFetch).toHaveBeenCalledTimes(3);
    expect(mockFetch.mock.calls[1][0]).toContain('/auth/refresh');
    expect(localStorage.getItem('token')).toBe('new-token');
  });

  it('redirects to login when refresh fails on 401', async () => {
    localStorage.setItem('token', 'expired-token');
    localStorage.setItem('refreshToken', 'invalid-refresh');

    let callCount = 0;
    mockFetch.mockImplementation(() => {
      callCount++;
      if (callCount === 1) {
        return Promise.resolve({
          ok: false,
          status: 401,
          json: () => Promise.resolve({ message: 'Unauthorized' }),
          text: () => Promise.resolve(''),
        });
      }
      // Refresh also fails
      return Promise.resolve({
        ok: false,
        status: 401,
        json: () => Promise.resolve({ message: 'Invalid refresh token' }),
        text: () => Promise.resolve(''),
      });
    });

    const mockHref = { value: '/dashboard' };
    Object.defineProperty(window, 'location', {
      writable: true,
      value: {
        ...window.location,
        pathname: '/dashboard',
        get href() { return mockHref.value; },
        set href(v: string) { mockHref.value = v; },
      },
    });

    await expect(api.getDevices()).rejects.toThrow();

    // Should have cleared storage
    expect(localStorage.getItem('token')).toBeNull();
    expect(localStorage.getItem('refreshToken')).toBeNull();
    // Should redirect to login
    expect(mockHref.value).toBe('/login');
  });

  it('does not attempt refresh on auth endpoints returning 401', async () => {
    localStorage.setItem('token', 'some-token');

    mockFetch.mockResolvedValue({
      ok: false,
      status: 401,
      json: () => Promise.resolve({ message: 'Unauthorized' }),
      text: () => Promise.resolve(''),
    });

    Object.defineProperty(window, 'location', {
      writable: true,
      value: { ...window.location, pathname: '/dashboard' },
    });

    // getCurrentUser calls /auth/me which is an auth endpoint
    await expect(api.getCurrentUser()).rejects.toThrow('Unauthorized');

    // Should NOT have tried to refresh (only 1 fetch call)
    expect(mockFetch).toHaveBeenCalledTimes(1);
  });

  it('handles empty response body gracefully', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      status: 204,
      text: () => Promise.resolve(''),
      json: () => Promise.reject(new Error('no body')),
    });

    const result = await api.logout();
    expect(result).toBeNull();
  });

  it('appends query params for GET requests', async () => {
    await api.getDevices({ status: 'online', search: 'server' });

    const url = mockFetch.mock.calls[0][0] as string;
    expect(url).toContain('status=online');
    expect(url).toContain('search=server');
  });

  it('sends JSON body for POST requests', async () => {
    await api.login('admin', 'secret');

    const [, options] = mockFetch.mock.calls[0];
    expect(options.method).toBe('POST');
    expect(options.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(options.body)).toEqual({
      identifier: 'admin',
      password: 'secret',
    });
  });
});
