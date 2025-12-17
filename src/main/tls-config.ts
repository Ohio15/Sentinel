import * as fs from 'fs';
import * as path from 'path';
import * as https from 'https';
import { app } from 'electron';

export interface TLSOptions {
  useTLS: boolean;
  certPath?: string;
  keyPath?: string;
  caPath?: string;
}

export interface ServerOptions {
  key?: Buffer;
  cert?: Buffer;
  ca?: Buffer;
}

/**
 * Load TLS certificates for HTTPS/WSS server
 * Falls back gracefully if certificates are not found
 */
export function loadTLSCertificates(options?: TLSOptions): ServerOptions | null {
  const useTLS = options?.useTLS !== false; // Default to true

  if (!useTLS) {
    console.warn('[TLS] TLS disabled, using HTTP/WS');
    return null;
  }

  try {
    // Determine certificate directory
    const certsDir = app.isPackaged
      ? path.join(process.resourcesPath, 'certs')
      : path.join(__dirname, '../../certs');

    const certPath = options?.certPath || path.join(certsDir, 'server-cert.pem');
    const keyPath = options?.keyPath || path.join(certsDir, 'server-key.pem');
    const caPath = options?.caPath || path.join(certsDir, 'ca-cert.pem');

    // Check if certificate files exist
    if (!fs.existsSync(certPath) || !fs.existsSync(keyPath)) {
      console.warn('[TLS] Certificates not found, falling back to HTTP/WS');
      console.warn('[TLS] Run: powershell -ExecutionPolicy Bypass -File scripts/generate-certs.ps1');
      console.warn(`[TLS] Expected paths:`);
      console.warn(`[TLS]   - ${certPath}`);
      console.warn(`[TLS]   - ${keyPath}`);
      return null;
    }

    // Load certificates
    const cert = fs.readFileSync(certPath);
    const key = fs.readFileSync(keyPath);

    const serverOptions: ServerOptions = {
      cert,
      key,
    };

    // Optional: Load CA certificate for client verification (mTLS)
    if (fs.existsSync(caPath)) {
      serverOptions.ca = fs.readFileSync(caPath);
      console.log('[TLS] Loaded CA certificate for client verification');
    }

    console.log('[TLS] TLS certificates loaded successfully');
    console.log(`[TLS]   - Certificate: ${certPath}`);
    console.log(`[TLS]   - Private key: ${keyPath}`);

    return serverOptions;
  } catch (error) {
    console.error('[TLS] Error loading TLS certificates:', error);
    console.warn('[TLS] Falling back to HTTP/WS');
    return null;
  }
}

/**
 * Create HTTPS server with TLS certificates
 * Falls back to HTTP if certificates are not available
 */
export function createSecureServer(
  app: any,
  options?: TLSOptions
): { server: https.Server | import('http').Server; protocol: 'https' | 'http' } {
  const tlsOptions = loadTLSCertificates(options);

  if (tlsOptions) {
    const server = https.createServer(tlsOptions, app);
    console.log('[Server] Created HTTPS server with TLS');
    return { server, protocol: 'https' };
  } else {
    const http = require('http');
    const server = http.createServer(app);
    console.log('[Server] Created HTTP server (no TLS)');
    return { server, protocol: 'http' };
  }
}

/**
 * Get WebSocket protocol based on TLS configuration
 */
export function getWebSocketProtocol(useTLS: boolean): 'wss' | 'ws' {
  return useTLS ? 'wss' : 'ws';
}

/**
 * Check if TLS certificates exist
 */
export function checkTLSCertificates(): boolean {
  const certsDir = app.isPackaged
    ? path.join(process.resourcesPath, 'certs')
    : path.join(__dirname, '../../certs');

  const certPath = path.join(certsDir, 'server-cert.pem');
  const keyPath = path.join(certsDir, 'server-key.pem');

  return fs.existsSync(certPath) && fs.existsSync(keyPath);
}
