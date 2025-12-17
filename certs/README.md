# TLS Certificates Directory

This directory contains TLS certificates for securing Sentinel RMM communications.

## Quick Start

Generate self-signed certificates for development:

```powershell
# From project root
powershell -ExecutionPolicy Bypass -File scripts/generate-certs.ps1
```

This will create:
- `ca-cert.pem` - Certificate Authority certificate (distribute to agents)
- `ca-key.pem` - CA private key (keep secure!)
- `server-cert.pem` - Server certificate
- `server-key.pem` - Server private key (keep secure!)

## Security Warning

**NEVER commit private keys (*.key, *-key.pem) to version control!**

The `.gitignore` file is configured to exclude these files, but always verify before committing.

## Production Use

For production deployments:
1. Obtain certificates from a trusted Certificate Authority
2. Copy certificates to this directory
3. Ensure proper file permissions (private keys should be readable only by the Sentinel process)

## File Naming Convention

The following file names are recognized:

### Server Certificates
- `server-cert.pem` - Server TLS certificate
- `server-key.pem` - Server private key

### CA Certificates
- `ca-cert.pem` - Certificate Authority certificate
- `ca-key.pem` - Certificate Authority private key

## Agent Deployment

Distribute `ca-cert.pem` to all agents. See `TLS_IMPLEMENTATION.md` for deployment instructions.

## Troubleshooting

If you see "TLS certificates not found" errors:
1. Verify files exist in this directory
2. Check file permissions
3. Regenerate certificates if necessary
4. Review logs for specific error messages

For more information, see `TLS_IMPLEMENTATION.md` in the project root.
