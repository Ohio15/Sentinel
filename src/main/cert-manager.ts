import * as fs from 'fs';
import * as path from 'path';
import * as crypto from 'crypto';
import { app } from 'electron';
import * as forge from 'node-forge';

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
 * Parse certificate information using node-forge
 */
async function parseCertificate(certPath: string): Promise<Partial<CertificateInfo>> {
  if (!fs.existsSync(certPath)) {
    return { exists: false, status: 'missing' };
  }

  try {
    const certPem = fs.readFileSync(certPath, 'utf-8');
    const cert = forge.pki.certificateFromPem(certPem);

    const info: Partial<CertificateInfo> = { exists: true };

    // Parse subject
    const subjectAttrs = cert.subject.attributes.map(attr =>
      `${attr.shortName || attr.name}=${attr.value}`
    ).join(', ');
    info.subject = subjectAttrs;

    // Parse issuer
    const issuerAttrs = cert.issuer.attributes.map(attr =>
      `${attr.shortName || attr.name}=${attr.value}`
    ).join(', ');
    info.issuer = issuerAttrs;

    // Parse dates
    info.validFrom = cert.validity.notBefore;
    info.validTo = cert.validity.notAfter;

    // Parse serial
    info.serialNumber = cert.serialNumber;

    // Calculate fingerprint (SHA-256)
    const certDer = forge.asn1.toDer(forge.pki.certificateToAsn1(cert)).getBytes();
    const md = forge.md.sha256.create();
    md.update(certDer);
    info.fingerprint = md.digest().toHex().toUpperCase().match(/.{2}/g)?.join(':');

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

    return info;
  } catch (error) {
    console.error('[CertManager] Failed to parse certificate:', error);
    return { exists: true, status: 'valid' };
  }
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
 * Generate certificates using node-forge (no external dependencies required)
 */
export async function renewCertificates(validityDays: number = 365): Promise<{ success: boolean; message: string; output?: string }> {
  try {
    const certsDir = getCertsDir();
    const hostname = require('os').hostname();

    console.log('[CertManager] Regenerating certificates...');
    console.log('[CertManager] Output directory:', certsDir);
    console.log('[CertManager] Hostname:', hostname);

    // Generate CA key pair
    console.log('[CertManager] Generating CA key pair...');
    const caKeys = forge.pki.rsa.generateKeyPair(4096);

    // Create CA certificate
    console.log('[CertManager] Creating CA certificate...');
    const caCert = forge.pki.createCertificate();
    caCert.publicKey = caKeys.publicKey;
    caCert.serialNumber = '01';
    caCert.validity.notBefore = new Date();
    caCert.validity.notAfter = new Date();
    caCert.validity.notAfter.setDate(caCert.validity.notBefore.getDate() + validityDays);

    const caAttrs = [
      { name: 'commonName', value: 'Sentinel CA' },
      { name: 'countryName', value: 'US' },
      { name: 'stateOrProvinceName', value: 'State' },
      { name: 'localityName', value: 'City' },
      { name: 'organizationName', value: 'Sentinel' },
      { shortName: 'OU', value: 'IT' },
    ];
    caCert.setSubject(caAttrs);
    caCert.setIssuer(caAttrs);
    caCert.setExtensions([
      { name: 'basicConstraints', cA: true },
      { name: 'keyUsage', keyCertSign: true, digitalSignature: true, cRLSign: true },
      { name: 'subjectKeyIdentifier' },
    ]);
    caCert.sign(caKeys.privateKey, forge.md.sha256.create());

    // Generate Server key pair
    console.log('[CertManager] Generating server key pair...');
    const serverKeys = forge.pki.rsa.generateKeyPair(4096);

    // Create Server certificate
    console.log('[CertManager] Creating server certificate...');
    const serverCert = forge.pki.createCertificate();
    serverCert.publicKey = serverKeys.publicKey;
    serverCert.serialNumber = '02';
    serverCert.validity.notBefore = new Date();
    serverCert.validity.notAfter = new Date();
    serverCert.validity.notAfter.setDate(serverCert.validity.notBefore.getDate() + validityDays);

    const serverAttrs = [
      { name: 'commonName', value: hostname },
      { name: 'countryName', value: 'US' },
      { name: 'stateOrProvinceName', value: 'State' },
      { name: 'localityName', value: 'City' },
      { name: 'organizationName', value: 'Sentinel' },
      { shortName: 'OU', value: 'IT' },
    ];
    serverCert.setSubject(serverAttrs);
    serverCert.setIssuer(caAttrs); // Signed by CA
    serverCert.setExtensions([
      { name: 'basicConstraints', cA: false },
      { name: 'keyUsage', digitalSignature: true, keyEncipherment: true },
      { name: 'extKeyUsage', serverAuth: true },
      {
        name: 'subjectAltName',
        altNames: [
          { type: 2, value: hostname }, // DNS
          { type: 2, value: 'localhost' },
          { type: 7, ip: '127.0.0.1' }, // IP
          { type: 7, ip: '::1' },
        ],
      },
    ]);
    serverCert.sign(caKeys.privateKey, forge.md.sha256.create());

    // Convert to PEM format
    const caCertPem = forge.pki.certificateToPem(caCert);
    const caKeyPem = forge.pki.privateKeyToPem(caKeys.privateKey);
    const serverCertPem = forge.pki.certificateToPem(serverCert);
    const serverKeyPem = forge.pki.privateKeyToPem(serverKeys.privateKey);

    // Write files
    console.log('[CertManager] Writing certificate files...');
    fs.writeFileSync(path.join(certsDir, 'ca-cert.pem'), caCertPem);
    fs.writeFileSync(path.join(certsDir, 'ca-key.pem'), caKeyPem);
    fs.writeFileSync(path.join(certsDir, 'server-cert.pem'), serverCertPem);
    fs.writeFileSync(path.join(certsDir, 'server-key.pem'), serverKeyPem);

    console.log('[CertManager] Certificates generated successfully');

    return {
      success: true,
      message: 'Certificates regenerated successfully',
      output: `Generated certificates in ${certsDir}:\n- ca-cert.pem\n- ca-key.pem\n- server-cert.pem\n- server-key.pem`,
    };
  } catch (error: any) {
    console.error('[CertManager] Certificate generation failed:', error);
    return {
      success: false,
      message: `Certificate generation failed: ${error.message}`,
    };
  }
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
