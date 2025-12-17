# TLS Quick Start Guide

Get TLS/mTLS security up and running in 5 minutes.

## Step 1: Generate Certificates (30 seconds)

```powershell
# Run from project root
powershell -ExecutionPolicy Bypass -File scripts/generate-certs.ps1
```

Output:
```
=== Sentinel Certificate Generator ===
[1/4] Generating CA private key...
  Generated CA private key: ca-key.pem
[2/4] Generating CA certificate...
  Generated CA certificate: ca-cert.pem
[3/4] Generating server private key...
  Generated server private key: server-key.pem
[4/4] Signing server certificate with CA...
  Generated server certificate: server-cert.pem
```

## Step 2: Verify Certificate Creation (10 seconds)

```powershell
# Check certificates exist
dir certs\

# Should see:
# ca-cert.pem, ca-key.pem, server-cert.pem, server-key.pem
```

## Step 3: Build and Start Server (1 minute)

```bash
# Build the project
npm run build

# Start the server
npm start
```

Check logs for successful TLS initialization:
```
[TLS] TLS certificates loaded successfully
gRPC server configured with TLS (server-side only)
gRPC DataPlane server listening on port 8082 (TLS)
```

## Step 4: Deploy CA Certificate to Agents (2 minutes)

### Windows Agent
```powershell
# Copy CA cert to agent directory
copy certs\ca-cert.pem "C:\Program Files\Sentinel\ca-cert.pem"

# Or use ProgramData location
copy certs\ca-cert.pem "C:\ProgramData\Sentinel\certs\ca-cert.pem"
```

### Linux Agent
```bash
# Create directory and copy
sudo mkdir -p /etc/sentinel/certs
sudo cp certs/ca-cert.pem /etc/sentinel/certs/ca-cert.pem
sudo chmod 644 /etc/sentinel/certs/ca-cert.pem
```

### macOS Agent
```bash
# Create directory and copy
sudo mkdir -p /usr/local/sentinel/certs
sudo cp certs/ca-cert.pem /usr/local/sentinel/certs/ca-cert.pem
sudo chmod 644 /usr/local/sentinel/certs/ca-cert.pem
```

## Step 5: Start/Restart Agents (1 minute)

Agents will automatically detect the CA certificate and connect securely.

Check agent logs:
```
[gRPC] Found CA certificate at: /path/to/ca-cert.pem
[gRPC] TLS enabled with CA certificate
[gRPC] Connected to Data Plane at <server>:8082 (TLS)
```

## Done!

Your Sentinel RMM is now secured with TLS encryption.

## What's Protected?

✅ gRPC communications (agents ↔ server)
✅ WebSocket communications (agents ↔ server)
✅ HTTPS REST API (agents ↔ server)
✅ All sensitive data in transit

## Verification

### Check Server Status
```powershell
# Server should show TLS mode
# Check console output or logs
```

### Check Agent Connection
```powershell
# Agent logs should show "TLS" not "insecure"
```

### Test HTTPS Endpoint
```powershell
# Should use HTTPS now
curl https://localhost:8081/health
```

## Troubleshooting

### "TLS certificates not found"
Run certificate generation script again:
```powershell
powershell -ExecutionPolicy Bypass -File scripts/generate-certs.ps1
```

### "Certificate verify failed" on agent
Ensure CA certificate is deployed to agent:
```powershell
# Check if file exists
Test-Path "C:\ProgramData\Sentinel\certs\ca-cert.pem"
```

### Self-signed certificate warnings in browser
This is normal for self-signed certificates. For production, use certificates from a trusted CA.

To trust the CA in your browser (development only):
```powershell
# Windows - Import CA to trusted root store
Import-Certificate -FilePath "certs\ca-cert.pem" -CertStoreLocation Cert:\LocalMachine\Root
```

## Next Steps

- Read `TLS_IMPLEMENTATION.md` for detailed documentation
- Review `HTTPS_WSS_UPGRADE.md` for WebSocket TLS integration
- Configure mTLS for stronger security
- Plan certificate rotation strategy for production

## Production Deployment

For production, replace self-signed certificates with certificates from a trusted CA:

1. Obtain certificates from CA (Let's Encrypt, DigiCert, etc.)
2. Replace files in `certs/` directory
3. Restart server
4. Distribute new CA certificate to agents (if changed)
5. Restart agents

## Security Reminders

⚠️ **NEVER** commit private keys to version control
⚠️ **ALWAYS** use TLS in production
⚠️ **ROTATE** certificates before expiration
⚠️ **MONITOR** certificate expiry dates
⚠️ **RESTRICT** access to private key files

---

**Estimated total time: 5 minutes**

For questions or issues, see `TLS_IMPLEMENTATION.md` or review server/agent logs.
