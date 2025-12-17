import * as fs from 'fs';
import * as path from 'path';
import * as crypto from 'crypto';
import { spawn } from 'child_process';
import { app } from 'electron';

export interface CertificateInfo {
  name: string;
  type: 'ca' | 'server';
  path: string;
  exists: boolean;
  subject?: string;
  issuer?: string;
  validFrom?: Date;
  validTo?: Date;
  fingerprint?: string;
  serialNumber?: string;
  daysUntilExpiry?: number;
  status: 'valid' | 'expiring_soon' | 'expired' | 'missing';
}

export interface CertificateListResult {
  certificates: CertificateInfo[];
  certsDir: string;
  caCertHash?: string;
}

/**
 * Get the certificates directory path
 * For packaged apps, store in user data directory (writable)
 * For development, use project certs directory
 */
export function getCertsDir(): string {
  if (app.isPackaged) {
    const certsDir = path.join(app.getPath('userData'), 'certs');
    // Ensure directory exists
    if (!fs.existsSync(certsDir)) {
      fs.mkdirSync(certsDir, { recursive: true });
    }
    return certsDir;
  }
  return path.join(__dirname, '../../certs');
}

/**
 * Calculate SHA256 hash of a certificate file
 */
export function getCertHash(certPath: string): string | null {
  try {
    if (!fs.existsSync(certPath)) {
      return null;
    }
    const certData = fs.readFileSync(certPath);
    return crypto.createHash('sha256').update(certData).digest('hex');
  } catch (error) {
    console.error('[CertManager] Error calculating cert hash:', error);
    return null;
  }
}

/**
 * Parse certificate information using openssl
 */
async function parseCertificate(certPath: string): Promise<Partial<CertificateInfo>> {
  return new Promise((resolve) => {
    if (!fs.existsSync(certPath)) {
      resolve({ exists: false, status: 'missing' });
      return;
    }

    const openssl = spawn('openssl', ['x509', '-in', certPath, '-noout', '-subject', '-issuer', '-dates', '-serial', '-fingerprint']);

    let stdout = '';
    let stderr = '';

    openssl.stdout.on('data', (data) => {
      stdout += data.toString();
    });

    openssl.stderr.on('data', (data) => {
      stderr += data.toString();
    });

    openssl.on('close', (code) => {
      if (code !== 0) {
        console.error('[CertManager] OpenSSL error:', stderr);
        resolve({ exists: true, status: 'valid' });
        return;
      }

      const info: Partial<CertificateInfo> = { exists: true };

      // Parse subject
      const subjectMatch = stdout.match(/subject=(.+)/i);
      if (subjectMatch) {
        info.subject = subjectMatch[1].trim();
      }

      // Parse issuer
      const issuerMatch = stdout.match(/issuer=(.+)/i);
      if (issuerMatch) {
        info.issuer = issuerMatch[1].trim();
      }

      // Parse dates
      const notBeforeMatch = stdout.match(/notBefore=(.+)/i);
      if (notBeforeMatch) {
        info.validFrom = new Date(notBeforeMatch[1].trim());
      }

      const notAfterMatch = stdout.match(/notAfter=(.+)/i);
      if (notAfterMatch) {
        info.validTo = new Date(notAfterMatch[1].trim());
      }

      // Parse serial
      const serialMatch = stdout.match(/serial=(.+)/i);
      if (serialMatch) {
        info.serialNumber = serialMatch[1].trim();
      }

      // Parse fingerprint
      const fingerprintMatch = stdout.match(/SHA256 Fingerprint=(.+)/i) || stdout.match(/Fingerprint=(.+)/i);
      if (fingerprintMatch) {
        info.fingerprint = fingerprintMatch[1].trim();
      }

      // Calculate days until expiry and status
      if (info.validTo) {
        const now = new Date();
        const daysUntilExpiry = Math.ceil((info.validTo.getTime() - now.getTime()) / (1000 * 60 * 60 * 24));
        info.daysUntilExpiry = daysUntilExpiry;

        if (daysUntilExpiry <= 0) {
          info.status = 'expired';
        } else if (daysUntilExpiry <= 30) {
          info.status = 'expiring_soon';
        } else {
          info.status = 'valid';
        }
      } else {
        info.status = 'valid';
      }

      resolve(info);
    });

    openssl.on('error', (err) => {
      console.error('[CertManager] Failed to spawn openssl:', err);
      resolve({ exists: true, status: 'valid' });
    });
  });
}

/**
 * Get information about all certificates in the certs directory
 */
export async function listCertificates(): Promise<CertificateListResult> {
  const certsDir = getCertsDir();
  const certificates: CertificateInfo[] = [];

  // Define the certificates we track
  const certFiles = [
    { name: 'CA Certificate', type: 'ca' as const, file: 'ca-cert.pem' },
    { name: 'Server Certificate', type: 'server' as const, file: 'server-cert.pem' },
  ];

  for (const certDef of certFiles) {
    const certPath = path.join(certsDir, certDef.file);
    const parsed = await parseCertificate(certPath);

    certificates.push({
      name: certDef.name,
      type: certDef.type,
      path: certPath,
      exists: parsed.exists ?? false,
      subject: parsed.subject,
      issuer: parsed.issuer,
      validFrom: parsed.validFrom,
      validTo: parsed.validTo,
      fingerprint: parsed.fingerprint,
      serialNumber: parsed.serialNumber,
      daysUntilExpiry: parsed.daysUntilExpiry,
      status: parsed.status ?? 'missing',
    });
  }

  // Get CA cert hash for tracking distribution
  const caCertPath = path.join(certsDir, 'ca-cert.pem');
  const caCertHash = getCertHash(caCertPath) ?? undefined;

  return {
    certificates,
    certsDir,
    caCertHash,
  };
}

/**
 * Get the CA certificate content for distribution to agents
 */
export function getCACertificate(): { content: string; hash: string } | null {
  const certsDir = getCertsDir();
  const caCertPath = path.join(certsDir, 'ca-cert.pem');

  try {
    if (!fs.existsSync(caCertPath)) {
      console.error('[CertManager] CA certificate not found');
      return null;
    }

    const content = fs.readFileSync(caCertPath, 'utf-8');
    const hash = crypto.createHash('sha256').update(content).digest('hex');

    return { content, hash };
  } catch (error) {
    console.error('[CertManager] Error reading CA certificate:', error);
    return null;
  }
}

/**
 * Regenerate certificates using the generate-certs.ps1 script
 */
export async function renewCertificates(validityDays: number = 365): Promise<{ success: boolean; message: string; output?: string }> {
  return new Promise((resolve) => {
    const scriptsDir = app.isPackaged
      ? path.join(process.resourcesPath, 'scripts')
      : path.join(__dirname, '../../scripts');

    const scriptPath = path.join(scriptsDir, 'generate-certs.ps1');
    const certsDir = getCertsDir();

    if (!fs.existsSync(scriptPath)) {
      resolve({
        success: false,
        message: `Certificate generation script not found: ${scriptPath}`,
      });
      return;
    }

    console.log('[CertManager] Regenerating certificates...');
    console.log('[CertManager] Output directory:', certsDir);

    const powershell = spawn('powershell.exe', [
      '-ExecutionPolicy', 'Bypass',
      '-File', scriptPath,
      '-OutputDir', certsDir,
      '-ValidityDays', validityDays.toString(),
    ]);

    let stdout = '';
    let stderr = '';

    powershell.stdout.on('data', (data) => {
      stdout += data.toString();
      console.log('[CertManager]', data.toString().trim());
    });

    powershell.stderr.on('data', (data) => {
      stderr += data.toString();
      console.error('[CertManager]', data.toString().trim());
    });

    powershell.on('close', (code) => {
      if (code === 0) {
        resolve({
          success: true,
          message: 'Certificates regenerated successfully',
          output: stdout,
        });
      } else {
        resolve({
          success: false,
          message: `Certificate generation failed with exit code ${code}`,
          output: stderr || stdout,
        });
      }
    });

    powershell.on('error', (err) => {
      console.error('[CertManager] Failed to spawn powershell:', err);
      resolve({
        success: false,
        message: `Failed to run certificate generation script: ${err.message}`,
      });
    });
  });
}

/**
 * Check if certificates need renewal (within 30 days of expiry or missing)
 */
export async function checkCertificatesNeedRenewal(): Promise<{ needsRenewal: boolean; reason?: string }> {
  const result = await listCertificates();

  for (const cert of result.certificates) {
    if (cert.status === 'missing') {
      return { needsRenewal: true, reason: `${cert.name} is missing` };
    }
    if (cert.status === 'expired') {
      return { needsRenewal: true, reason: `${cert.name} has expired` };
    }
    if (cert.status === 'expiring_soon') {
      return { needsRenewal: true, reason: `${cert.name} expires in ${cert.daysUntilExpiry} days` };
    }
  }

  return { needsRenewal: false };
}
