import { APIRequestContext } from '@playwright/test';

const ADMIN_EMAIL = process.env.SENTINEL_ADMIN_EMAIL || 'admin@sentinel.local';
const ADMIN_PASSWORD = process.env.SENTINEL_ADMIN_PASSWORD || 'admin';

interface LoginResponse {
  accessToken: string;
  refreshToken: string;
  user: {
    id: string;
    email: string;
    role: string;
  };
}

/**
 * Authenticates against the Sentinel API and returns the JWT access token.
 */
export async function getAuthToken(request: APIRequestContext): Promise<string> {
  const response = await request.post('/api/auth/login', {
    data: {
      email: ADMIN_EMAIL,
      password: ADMIN_PASSWORD,
    },
  });

  if (!response.ok()) {
    const body = await response.text();
    throw new Error(
      `Login failed (${response.status()}): ${body}. ` +
      `Set SENTINEL_ADMIN_EMAIL and SENTINEL_ADMIN_PASSWORD env vars.`
    );
  }

  const data: LoginResponse = await response.json();
  if (!data.accessToken) {
    throw new Error('Login response missing accessToken field');
  }

  return data.accessToken;
}

/**
 * Wrapper that adds the Authorization header to any API request.
 */
export async function authenticatedRequest(
  request: APIRequestContext,
  method: 'get' | 'post' | 'put' | 'patch' | 'delete',
  url: string,
  options?: {
    data?: unknown;
    headers?: Record<string, string>;
    params?: Record<string, string | number>;
  }
): Promise<ReturnType<APIRequestContext['get']>> {
  const token = await getAuthToken(request);
  const headers = {
    Authorization: `Bearer ${token}`,
    ...options?.headers,
  };

  const requestOptions = {
    headers,
    data: options?.data,
    params: options?.params as Record<string, string | number> | undefined,
  };

  switch (method) {
    case 'get':
      return request.get(url, requestOptions);
    case 'post':
      return request.post(url, requestOptions);
    case 'put':
      return request.put(url, requestOptions);
    case 'patch':
      return request.patch(url, requestOptions);
    case 'delete':
      return request.delete(url, requestOptions);
  }
}

/**
 * Get auth headers object for use with page.goto or manual fetches.
 */
export async function getAuthHeaders(request: APIRequestContext): Promise<Record<string, string>> {
  const token = await getAuthToken(request);
  return {
    Authorization: `Bearer ${token}`,
  };
}
