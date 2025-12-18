/**
 * Microsoft Authentication Library (MSAL) Integration
 * Handles M365 SSO authentication for the support portal
 */

import * as msal from '@azure/msal-node';
import * as crypto from 'crypto';

export interface MSALConfig {
  clientId: string;
  clientSecret: string;
  tenantId?: string; // 'common' for multi-tenant
  redirectUri: string;
}

export interface PortalUser {
  email: string;
  name: string;
  tenantId: string;
  accessToken: string;
  refreshToken?: string;
  idToken?: string;
  expiresAt: Date;
}

export interface TokenResponse {
  accessToken: string;
  idToken?: string;
  refreshToken?: string;
  expiresOn: Date;
  account: {
    username: string;
    name?: string;
    tenantId: string;
  };
}

// JWT payload structure from Azure AD
interface AzureADJWTPayload {
  aud: string;
  iss: string;
  iat: number;
  nbf: number;
  exp: number;
  name?: string;
  preferred_username?: string;
  email?: string;
  upn?: string;
  oid: string;
  tid: string; // Tenant ID
  sub: string;
}

export class MSALAuth {
  private confidentialClient: msal.ConfidentialClientApplication | null = null;
  private config: MSALConfig | null = null;
  private stateStore: Map<string, { timestamp: number; redirectUri: string }> = new Map();
  private readonly STATE_EXPIRY_MS = 10 * 60 * 1000; // 10 minutes

  /**
   * Initialize MSAL with configuration
   */
  initialize(config: MSALConfig): void {
    this.config = config;

    const msalConfig: msal.Configuration = {
      auth: {
        clientId: config.clientId,
        // Use 'common' endpoint to accept any Azure AD tenant
        authority: `https://login.microsoftonline.com/${config.tenantId || 'common'}`,
        clientSecret: config.clientSecret,
      },
      system: {
        loggerOptions: {
          loggerCallback: (level, message, containsPii) => {
            if (!containsPii) {
              console.log(`[MSAL ${msal.LogLevel[level]}] ${message}`);
            }
          },
          logLevel: msal.LogLevel.Warning,
          piiLoggingEnabled: false,
        },
      },
    };

    this.confidentialClient = new msal.ConfidentialClientApplication(msalConfig);
    console.log('[MSALAuth] Initialized with client ID:', config.clientId);
  }

  /**
   * Check if MSAL is configured and ready
   */
  isConfigured(): boolean {
    return this.confidentialClient !== null && this.config !== null;
  }

  /**
   * Generate a cryptographically secure state parameter
   */
  private generateState(redirectUri: string): string {
    const state = crypto.randomBytes(32).toString('hex');
    this.stateStore.set(state, {
      timestamp: Date.now(),
      redirectUri,
    });

    // Clean up expired states
    this.cleanupExpiredStates();

    return state;
  }

  /**
   * Validate and consume a state parameter
   */
  private validateState(state: string): string | null {
    const stored = this.stateStore.get(state);
    if (!stored) {
      return null;
    }

    // Check if expired
    if (Date.now() - stored.timestamp > this.STATE_EXPIRY_MS) {
      this.stateStore.delete(state);
      return null;
    }

    // Consume the state (one-time use)
    this.stateStore.delete(state);
    return stored.redirectUri;
  }

  /**
   * Clean up expired state entries
   */
  private cleanupExpiredStates(): void {
    const now = Date.now();
    for (const [state, data] of this.stateStore.entries()) {
      if (now - data.timestamp > this.STATE_EXPIRY_MS) {
        this.stateStore.delete(state);
      }
    }
  }

  /**
   * Generate the authorization URL for the OAuth flow
   */
  async getAuthCodeUrl(postLoginRedirectUri?: string): Promise<string> {
    if (!this.confidentialClient || !this.config) {
      throw new Error('MSAL not initialized');
    }

    const state = this.generateState(postLoginRedirectUri || '/portal/tickets');

    const authCodeUrlParameters: msal.AuthorizationUrlRequest = {
      scopes: ['openid', 'profile', 'email', 'User.Read'],
      redirectUri: this.config.redirectUri,
      state,
      prompt: 'select_account', // Always show account picker for multi-tenant
    };

    const url = await this.confidentialClient.getAuthCodeUrl(authCodeUrlParameters);
    return url;
  }

  /**
   * Exchange authorization code for tokens
   */
  async acquireTokenByCode(code: string, state: string): Promise<TokenResponse> {
    if (!this.confidentialClient || !this.config) {
      throw new Error('MSAL not initialized');
    }

    // Validate state
    const redirectUri = this.validateState(state);
    if (!redirectUri) {
      throw new Error('Invalid or expired state parameter');
    }

    const tokenRequest: msal.AuthorizationCodeRequest = {
      code,
      scopes: ['openid', 'profile', 'email', 'User.Read'],
      redirectUri: this.config.redirectUri,
    };

    const response = await this.confidentialClient.acquireTokenByCode(tokenRequest);

    if (!response) {
      throw new Error('Failed to acquire token');
    }

    // Extract user info from the response
    const account = response.account;
    if (!account) {
      throw new Error('No account in token response');
    }

    return {
      accessToken: response.accessToken,
      idToken: response.idToken,
      refreshToken: undefined, // MSAL Node doesn't expose refresh token directly
      expiresOn: response.expiresOn || new Date(Date.now() + 3600000),
      account: {
        username: account.username,
        name: account.name,
        tenantId: account.tenantId,
      },
    };
  }

  /**
   * Decode and validate a JWT token (basic validation)
   * Note: In production, use proper JWT validation with JWKS
   */
  decodeToken(token: string): AzureADJWTPayload | null {
    try {
      const parts = token.split('.');
      if (parts.length !== 3) {
        return null;
      }

      const payload = Buffer.from(parts[1], 'base64url').toString('utf-8');
      return JSON.parse(payload) as AzureADJWTPayload;
    } catch {
      return null;
    }
  }

  /**
   * Extract user info from tokens
   */
  extractUserInfo(accessToken: string, idToken?: string): PortalUser | null {
    // Prefer ID token for user claims
    const tokenToDecode = idToken || accessToken;
    const decoded = this.decodeToken(tokenToDecode);

    if (!decoded) {
      return null;
    }

    // Check token expiry
    const expiresAt = new Date(decoded.exp * 1000);
    if (expiresAt < new Date()) {
      return null;
    }

    return {
      email: decoded.email || decoded.preferred_username || decoded.upn || '',
      name: decoded.name || '',
      tenantId: decoded.tid,
      accessToken,
      idToken,
      expiresAt,
    };
  }

  /**
   * Validate that a token is still valid
   */
  isTokenValid(expiresAt: Date): boolean {
    // Add 5 minute buffer
    const bufferMs = 5 * 60 * 1000;
    return new Date(expiresAt.getTime() - bufferMs) > new Date();
  }

  /**
   * Get Microsoft Graph API user info
   */
  async getUserFromGraph(accessToken: string): Promise<{
    displayName?: string;
    mail?: string;
    userPrincipalName?: string;
  } | null> {
    try {
      const response = await fetch('https://graph.microsoft.com/v1.0/me', {
        headers: {
          Authorization: `Bearer ${accessToken}`,
        },
      });

      if (!response.ok) {
        console.error('[MSALAuth] Graph API error:', response.status);
        return null;
      }

      const data = await response.json() as { displayName?: string; mail?: string; userPrincipalName?: string };
      return {
        displayName: data.displayName,
        mail: data.mail || data.userPrincipalName,
        userPrincipalName: data.userPrincipalName,
      };
    } catch (error) {
      console.error('[MSALAuth] Failed to fetch user from Graph:', error);
      return null;
    }
  }

  /**
   * Get the redirect URI that should be used after login
   */
  getPostLoginRedirect(state: string): string {
    const stored = this.stateStore.get(state);
    return stored?.redirectUri || '/portal/tickets';
  }

  /**
   * Get current configuration (without secrets)
   */
  getConfig(): { clientId: string; redirectUri: string } | null {
    if (!this.config) {
      return null;
    }
    return {
      clientId: this.config.clientId,
      redirectUri: this.config.redirectUri,
    };
  }
}

// Singleton instance
export const msalAuth = new MSALAuth();
